package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Two-factor authentication (TOTP per RFC 6238) for user accounts.
//
// Enrolment: BeginTwoFactorSetup mints a Base32 secret and stores it on the
// user while TwoFactorEnabled stays false; EnableTwoFactor verifies one
// live code against that secret before flipping the switch (so a stale or
// attacker-supplied setup can never lock an account), and mints one-time
// recovery codes whose bcrypt hashes are persisted. DisableTwoFactor
// requires either a live TOTP code or an unused recovery code. Login
// enforces the code through verifyTwoFactorForLogin.

const (
	// totpPeriod is the RFC 6238 default step.
	totpPeriod = 30 * time.Second
	// totpDigits is the code length the frontend prompts for.
	totpDigits = 6
	// totpWindow allows ±1 step of clock drift between server and app.
	totpWindow = 1
	// totpSecretBytes yields a 160-bit secret (32 Base32 chars).
	totpSecretBytes = 20
	// recoveryCodeCount is how many one-time codes enabling 2FA mints.
	recoveryCodeCount = 8
	// totpIssuer names the account entry in authenticator apps.
	totpIssuer = "WeRAG"
)

var (
	// ErrTwoFactorRequired is returned when login credentials are correct
	// but the account has 2FA and no valid code was supplied. Handlers
	// surface it as two_factor_required so clients prompt for a code.
	ErrTwoFactorRequired = fmt.Errorf("two-factor code required")
	// ErrTwoFactorInvalidCode is returned when a supplied TOTP or recovery
	// code does not verify.
	ErrTwoFactorInvalidCode = fmt.Errorf("invalid two-factor code")
	// ErrTwoFactorNotSetup is returned when enabling 2FA before a setup
	// secret exists for the user.
	ErrTwoFactorNotSetup = fmt.Errorf("two-factor setup not started")
	// ErrTwoFactorAlreadyEnabled is returned when starting a new setup
	// while 2FA is already enforced.
	ErrTwoFactorAlreadyEnabled = fmt.Errorf("two-factor authentication already enabled")
)

// generateTOTPSecret returns a random Base32 (RFC 4648, no padding) secret.
func generateTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// hotp computes the RFC 4226 counter-based code for the secret at counter.
func hotp(secretBase32 string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretBase32)
	if err != nil {
		return "", err
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%0*d", totpDigits, code), nil
}

// verifyTOTP checks a 6-digit code against the secret across the allowed
// drift window. Comparison is constant-time.
func verifyTOTP(secretBase32, code string) bool {
	if len(code) != totpDigits || secretBase32 == "" {
		return false
	}
	counter := uint64(time.Now().Unix() / int64(totpPeriod.Seconds()))
	for i := -int64(totpWindow); i <= totpWindow; i++ {
		want, err := hotp(secretBase32, uint64(int64(counter)+i))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// otpauthURL builds the provisioning URI authenticator apps scan.
func otpauthURL(email, secret string) string {
	label := url.PathEscape(totpIssuer + ":" + email)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(int(totpPeriod.Seconds())))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// generateRecoveryCodes mints plaintext one-time codes in xxxx-xxxx form.
func generateRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		raw := make([]byte, 4)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		codes = append(codes, fmt.Sprintf("%02x%02x-%02x%02x", raw[0], raw[1], raw[2], raw[3]))
	}
	return codes, nil
}

// hashRecoveryCodes bcrypt-hashes each plaintext code and serialises the
// hashes as a JSON array for the user row.
func hashRecoveryCodes(codes []string) (string, error) {
	hashes := make([]string, 0, len(codes))
	for _, c := range codes {
		// Hash the normalized form — verification normalizes too, so the
		// dash in the display form must not change the compared bytes.
		h, err := bcrypt.GenerateFromPassword([]byte(normalizeTwoFactorCode(c)), bcrypt.DefaultCost)
		if err != nil {
			return "", err
		}
		hashes = append(hashes, string(h))
	}
	data, err := json.Marshal(hashes)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// consumeRecoveryCode verifies code against the stored hashes; on a match
// it returns the JSON array with that hash removed (single use).
func consumeRecoveryCode(stored, code string) (string, bool) {
	code = normalizeTwoFactorCode(code)
	if stored == "" || code == "" {
		return "", false
	}
	var hashes []string
	if err := json.Unmarshal([]byte(stored), &hashes); err != nil {
		return "", false
	}
	for i, h := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(code)) == nil {
			remaining := append(hashes[:i:i], hashes[i+1:]...)
			data, err := json.Marshal(remaining)
			if err != nil {
				return "", false
			}
			return string(data), true
		}
	}
	return "", false
}

// verifyTwoFactorForLogin validates a login attempt against a 2FA-enabled
// account. The code may be a live TOTP value or an unused recovery code;
// when a recovery code matches it is consumed and the user row updated.
func (s *userService) verifyTwoFactorForLogin(ctx context.Context, user *types.User, code string) error {
	code = normalizeTwoFactorCode(code)
	if verifyTOTP(user.TOTPSecret, code) {
		return nil
	}
	if remaining, ok := consumeRecoveryCode(user.TwoFactorRecoveryCodes, code); ok {
		user.TwoFactorRecoveryCodes = remaining
		if err := s.userRepo.UpdateUser(ctx, user); err != nil {
			logger.Errorf(ctx, "Failed to persist consumed recovery code: %v", err)
		}
		return nil
	}
	return ErrTwoFactorInvalidCode
}

// TwoFactorStatus reports whether the account currently enforces 2FA.
func (s *userService) TwoFactorStatus(ctx context.Context, userID string) (bool, error) {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return user.TwoFactorEnabled, nil
}

// BeginTwoFactorSetup mints a TOTP secret and stores it as pending. A
// previously enabled account must be disabled first; until EnableTwoFactor
// verifies a live code, login behaviour is unchanged.
func (s *userService) BeginTwoFactorSetup(ctx context.Context, userID string) (secret, otpauth string, err error) {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	if user.TwoFactorEnabled {
		return "", "", ErrTwoFactorAlreadyEnabled
	}
	secret, err = generateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	user.TOTPSecret = secret
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return "", "", err
	}
	return secret, otpauthURL(user.Email, secret), nil
}

// EnableTwoFactor verifies a live TOTP code against the pending secret,
// flips 2FA on, and mints one-time recovery codes (returned in plaintext
// exactly once).
func (s *userService) EnableTwoFactor(ctx context.Context, userID, code string) ([]string, error) {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.TOTPSecret == "" {
		return nil, ErrTwoFactorNotSetup
	}
	if !verifyTOTP(user.TOTPSecret, code) {
		return nil, ErrTwoFactorInvalidCode
	}
	codes, err := generateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	hashes, err := hashRecoveryCodes(codes)
	if err != nil {
		return nil, err
	}
	user.TwoFactorEnabled = true
	user.TwoFactorRecoveryCodes = hashes
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	logger.Infof(ctx, "Two-factor authentication enabled for user %s", user.Email)
	return codes, nil
}

// DisableTwoFactor turns 2FA off after verifying a live TOTP code or an
// unused recovery code, and clears the enrolment state entirely.
func (s *userService) DisableTwoFactor(ctx context.Context, userID, code string) error {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !user.TwoFactorEnabled {
		return ErrTwoFactorNotSetup
	}
	if !verifyTOTP(user.TOTPSecret, code) {
		if _, ok := consumeRecoveryCode(user.TwoFactorRecoveryCodes, code); !ok {
			return ErrTwoFactorInvalidCode
		}
	}
	user.TwoFactorEnabled = false
	user.TOTPSecret = ""
	user.TwoFactorRecoveryCodes = ""
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return err
	}
	logger.Infof(ctx, "Two-factor authentication disabled for user %s", user.Email)
	return nil
}

// normalizeTwoFactorCode trims whitespace and removes the dash from the
// xxxx-xxxx recovery-code form so "xxxx xxxx" / "xxxxxxxx" all verify.
func normalizeTwoFactorCode(code string) string {
	return strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code))
}
