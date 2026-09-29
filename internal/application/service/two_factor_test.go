package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHotpRFC4226 verifies the TOTP core against the RFC 4226 appendix-D
// test vectors (secret = ASCII "12345678901234567890", Base32-encoded).
func TestHotpRFC4226(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	want := []string{
		"755224", "287082", "359152", "969429", "338314",
		"254676", "287922", "162583", "399871", "520489",
	}
	for counter, expected := range want {
		got, err := hotp(secret, uint64(counter))
		if err != nil {
			t.Fatalf("counter %d: %v", counter, err)
		}
		if got != expected {
			t.Errorf("counter %d: got %s, want %s", counter, got, expected)
		}
	}
}

func TestVerifyTOTPRejectsGarbage(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, code := range []string{"", "12345", "1234567", "abcdef", "12 456"} {
		if verifyTOTP(secret, code) {
			t.Errorf("expected rejection of %q", code)
		}
	}
	if verifyTOTP("", "123456") {
		t.Error("expected rejection of empty secret")
	}
}

func TestRecoveryCodesRoundTrip(t *testing.T) {
	codes, err := generateRecoveryCodes()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(codes), recoveryCodeCount)
	}
	hashes, err := hashRecoveryCodes(codes)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if strings.Contains(hashes, codes[0]) {
		t.Error("plaintext code must not appear in stored hashes")
	}

	// Consume the first code in its plain and dashed forms; the second
	// consumption of the same code must fail (single use).
	remaining, ok := consumeRecoveryCode(hashes, codes[0])
	if !ok {
		t.Fatal("first consumption failed")
	}
	if _, ok := consumeRecoveryCode(remaining, codes[0]); ok {
		t.Error("consumed code must be single use")
	}
	dashed := codes[1][:2] + "-" + codes[1][2:]
	remaining2, ok := consumeRecoveryCode(remaining, dashed)
	if !ok {
		t.Fatalf("dashed form %q must verify", dashed)
	}
	var left []string
	if err := json.Unmarshal([]byte(remaining2), &left); err != nil {
		t.Fatalf("remaining is not JSON: %v", err)
	}
	if len(left) != recoveryCodeCount-2 {
		t.Errorf("got %d remaining codes, want %d", len(left), recoveryCodeCount-2)
	}
}

func TestNormalizeTwoFactorCode(t *testing.T) {
	cases := map[string]string{
		" 123456 ": "123456",
		"ab-cd-ef": "abcdef",
		"ab cd":    "abcd",
		"123456":   "123456",
	}
	for in, want := range cases {
		if got := normalizeTwoFactorCode(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
