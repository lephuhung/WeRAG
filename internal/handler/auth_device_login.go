package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
)

// Browser sign-in for clients that should not ask for a password (the Word
// add-in), shaped like the OAuth device authorization grant (RFC 8628):
//
//  1. the client calls POST /auth/device/start and gets a secret
//     device_code plus a short user_code;
//  2. it opens <web>/word-login?code=<user_code> in the user's browser,
//     where the signed-in user approves (POST /auth/device/approve);
//  3. it polls POST /auth/device/poll with the device_code and receives a
//     token pair once, after the approval.
//
// The client never sees the password, the 2FA code or the web session; it
// keeps only the token pair it is handed. With Redis (UseDeviceLoginRedis)
// a pending request survives a backend restart and works across replicas.

const (
	deviceLoginTTL       = 10 * time.Minute
	deviceLoginInterval  = 3 // seconds between polls
	deviceLoginKeyPrefix = "werag:devicelogin:"
	// No vowels (no accidental words) and no look-alikes (0/O, 1/I).
	deviceUserCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"
	deviceUserCodeLen      = 8
)

const (
	deviceLoginPending  = "pending"
	deviceLoginApproved = "approved"
	deviceLoginDenied   = "denied"
	deviceLoginExpired  = "expired"
)

type deviceLoginEntry struct {
	UserCode  string    `json:"user_code"`
	Client    string    `json:"client"`
	Status    string    `json:"status"`
	UserID    string    `json:"user_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type deviceLoginStore struct {
	mu      sync.Mutex
	entries map[string]*deviceLoginEntry // by device key
	codes   map[string]string            // user_code → device key
	rdb     *redis.Client
}

var deviceLogins = &deviceLoginStore{
	entries: map[string]*deviceLoginEntry{},
	codes:   map[string]string{},
}

// UseDeviceLoginRedis keeps pending browser sign-ins in Redis; nil keeps
// them in process memory.
func UseDeviceLoginRedis(rdb *redis.Client) {
	deviceLogins.rdb = rdb
}

// deviceKey is what the store keeps for a device_code: its hash, so a
// leaked Redis dump does not hand out pollable codes.
func deviceKey(deviceCode string) string {
	sum := sha256.Sum256([]byte(deviceCode))
	return hex.EncodeToString(sum[:])
}

func redisDeviceKey(key string) string    { return deviceLoginKeyPrefix + "dev:" + key }
func redisUserCodeKey(code string) string { return deviceLoginKeyPrefix + "code:" + code }

func newDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func newUserCode() (string, error) {
	b := make([]byte, deviceUserCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, deviceUserCodeLen)
	for i, v := range b {
		out[i] = deviceUserCodeAlphabet[int(v)%len(deviceUserCodeAlphabet)]
	}
	return string(out), nil
}

// normalizeUserCode accepts "bcdf-ghjk", "BCDF GHJK", … and returns the
// bare upper-case code, "" when it cannot be one.
func normalizeUserCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		if strings.ContainsRune(deviceUserCodeAlphabet, r) {
			b.WriteRune(r)
		} else if r != '-' && r != ' ' {
			return ""
		}
	}
	if b.Len() != deviceUserCodeLen {
		return ""
	}
	return b.String()
}

func formatUserCode(code string) string {
	if len(code) != deviceUserCodeLen {
		return code
	}
	return code[:4] + "-" + code[4:]
}

func storeCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
}

func (s *deviceLoginStore) create(ctx context.Context, key string, e *deviceLoginEntry) (bool, error) {
	if s.rdb == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.sweepLocked()
		if _, taken := s.codes[e.UserCode]; taken {
			return false, nil
		}
		cp := *e
		s.entries[key] = &cp
		s.codes[e.UserCode] = key
		return true, nil
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	rctx, cancel := storeCtx(ctx)
	defer cancel()
	ok, err := s.rdb.SetNX(rctx, redisUserCodeKey(e.UserCode), key, deviceLoginTTL).Result()
	if err != nil || !ok {
		return false, err
	}
	if err := s.rdb.Set(rctx, redisDeviceKey(key), raw, deviceLoginTTL).Err(); err != nil {
		_ = s.rdb.Del(rctx, redisUserCodeKey(e.UserCode)).Err()
		return false, err
	}
	return true, nil
}

// sweepLocked drops expired in-memory entries.
func (s *deviceLoginStore) sweepLocked() {
	for key, e := range s.entries {
		if time.Since(e.CreatedAt) > deviceLoginTTL {
			delete(s.codes, e.UserCode)
			delete(s.entries, key)
		}
	}
}

// get returns the entry of a device key, nil when unknown or expired.
func (s *deviceLoginStore) get(ctx context.Context, key string) (*deviceLoginEntry, error) {
	if s.rdb == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.entries[key]
		if e == nil || time.Since(e.CreatedAt) > deviceLoginTTL {
			return nil, nil
		}
		cp := *e
		return &cp, nil
	}
	rctx, cancel := storeCtx(ctx)
	defer cancel()
	raw, err := s.rdb.Get(rctx, redisDeviceKey(key)).Bytes()
	if err != nil {
		if stderrors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var e deviceLoginEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// keyForUserCode returns the device key a user code points at, "" when none.
func (s *deviceLoginStore) keyForUserCode(ctx context.Context, code string) (string, error) {
	if s.rdb == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.codes[code], nil
	}
	rctx, cancel := storeCtx(ctx)
	defer cancel()
	key, err := s.rdb.Get(rctx, redisUserCodeKey(code)).Result()
	if stderrors.Is(err, redis.Nil) {
		return "", nil
	}
	return key, err
}

// decide records the user's answer on a still-pending entry; false when the
// entry is gone or was already answered.
func (s *deviceLoginStore) decide(ctx context.Context, key, status, userID string) (bool, error) {
	if s.rdb == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.entries[key]
		if e == nil || e.Status != deviceLoginPending || time.Since(e.CreatedAt) > deviceLoginTTL {
			return false, nil
		}
		e.Status, e.UserID = status, userID
		return true, nil
	}
	e, err := s.get(ctx, key)
	if err != nil || e == nil || e.Status != deviceLoginPending {
		return false, err
	}
	e.Status, e.UserID = status, userID
	raw, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	rctx, cancel := storeCtx(ctx)
	defer cancel()
	ok, err := s.rdb.SetArgs(rctx, redisDeviceKey(key), raw, redis.SetArgs{Mode: "XX", KeepTTL: true}).Result()
	if stderrors.Is(err, redis.Nil) {
		return false, nil
	}
	return err == nil && ok == "OK", err
}

// take removes a finished entry; only the caller that gets true may hand
// out tokens, so one approval yields one token pair.
func (s *deviceLoginStore) take(ctx context.Context, key string, e *deviceLoginEntry) (bool, error) {
	if s.rdb == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.entries[key]; !ok {
			return false, nil
		}
		delete(s.entries, key)
		delete(s.codes, e.UserCode)
		return true, nil
	}
	rctx, cancel := storeCtx(ctx)
	defer cancel()
	n, err := s.rdb.Del(rctx, redisDeviceKey(key)).Result()
	if err != nil || n == 0 {
		return false, err
	}
	_ = s.rdb.Del(rctx, redisUserCodeKey(e.UserCode)).Err()
	return true, nil
}

// lookupByUserCode resolves a user code typed or linked into the web page.
func (s *deviceLoginStore) lookupByUserCode(ctx context.Context, raw string) (string, *deviceLoginEntry, error) {
	code := normalizeUserCode(raw)
	if code == "" {
		return "", nil, nil
	}
	key, err := s.keyForUserCode(ctx, code)
	if err != nil || key == "" {
		return "", nil, err
	}
	e, err := s.get(ctx, key)
	if err != nil || e == nil {
		return "", nil, err
	}
	return key, e, nil
}

// DeviceLoginStart godoc
// @Summary      Start a browser sign-in for a client (Word add-in)
// @Tags         Xác thực
// @Produce      json
// @Router       /auth/device/start [post]
func (h *AuthHandler) DeviceLoginStart(c *gin.Context) {
	ctx := c.Request.Context()
	var req struct {
		Client string `json:"client"`
	}
	_ = c.ShouldBindJSON(&req)
	client := strings.TrimSpace(req.Client)
	if client == "" {
		client = "word"
	}
	if len(client) > 32 {
		client = client[:32]
	}
	deviceCode, err := newDeviceCode()
	if err != nil {
		c.Error(errors.NewInternalServerError("cannot start sign-in"))
		return
	}
	key := deviceKey(deviceCode)
	for attempt := 0; attempt < 5; attempt++ {
		userCode, err := newUserCode()
		if err != nil {
			break
		}
		ok, err := deviceLogins.create(ctx, key, &deviceLoginEntry{
			UserCode:  userCode,
			Client:    client,
			Status:    deviceLoginPending,
			CreatedAt: time.Now(),
		})
		if err != nil {
			logger.Errorf(ctx, "[DeviceLogin] store: %v", err)
			break
		}
		if !ok {
			continue
		}
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"device_code": deviceCode,
			"user_code":   formatUserCode(userCode),
			"expires_in":  int(deviceLoginTTL.Seconds()),
			"interval":    deviceLoginInterval,
		})
		return
	}
	c.Error(errors.NewServiceUnavailableError("cannot start sign-in, try again"))
}

// DeviceLoginPoll godoc
// @Summary      Poll a browser sign-in; returns the token pair once approved
// @Tags         Xác thực
// @Produce      json
// @Router       /auth/device/poll [post]
func (h *AuthHandler) DeviceLoginPoll(c *gin.Context) {
	ctx := c.Request.Context()
	var req struct {
		DeviceCode string `json:"device_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("device_code is required"))
		return
	}
	key := deviceKey(req.DeviceCode)
	e, err := deviceLogins.get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[DeviceLogin] poll: %v", err)
		c.Error(errors.NewServiceUnavailableError("sign-in store unavailable"))
		return
	}
	status := deviceLoginExpired
	if e != nil {
		status = e.Status
	}
	switch status {
	case deviceLoginPending:
		c.JSON(http.StatusOK, gin.H{"success": true, "status": status})
		return
	case deviceLoginDenied:
		_, _ = deviceLogins.take(ctx, key, e)
		c.JSON(http.StatusOK, gin.H{"success": true, "status": status})
		return
	case deviceLoginApproved:
	default:
		c.JSON(http.StatusOK, gin.H{"success": true, "status": deviceLoginExpired})
		return
	}
	won, err := deviceLogins.take(ctx, key, e)
	if err != nil || !won {
		c.JSON(http.StatusOK, gin.H{"success": true, "status": deviceLoginExpired})
		return
	}
	user, err := h.userService.GetUserByID(ctx, e.UserID)
	if err != nil || user == nil || !user.IsActive {
		c.Error(errors.NewUnauthorizedError("account is not available"))
		return
	}
	token, refresh, err := h.userService.GenerateTokens(ctx, user)
	if err != nil {
		logger.Errorf(ctx, "[DeviceLogin] tokens for %s: %v", user.ID, err)
		c.Error(errors.NewInternalServerError("cannot issue tokens"))
		return
	}
	logger.Infof(ctx, "[DeviceLogin] %s signed in through the browser (client %s)", user.ID, e.Client)
	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"status":        deviceLoginApproved,
		"token":         token,
		"refresh_token": refresh,
	})
}

// DeviceLoginLookup godoc
// @Summary      Show a pending browser sign-in to the signed-in user
// @Tags         Xác thực
// @Produce      json
// @Security     Bearer
// @Router       /auth/device/lookup [get]
func (h *AuthHandler) DeviceLoginLookup(c *gin.Context) {
	ctx := c.Request.Context()
	if _, err := h.userService.GetCurrentUser(ctx); err != nil {
		c.Error(errors.NewUnauthorizedError("sign in first"))
		return
	}
	_, e, err := deviceLogins.lookupByUserCode(ctx, c.Query("user_code"))
	if err != nil {
		c.Error(errors.NewServiceUnavailableError("sign-in store unavailable"))
		return
	}
	if e == nil || e.Status != deviceLoginPending {
		c.JSON(http.StatusOK, gin.H{"success": true, "status": deviceLoginExpired})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"status":     e.Status,
		"client":     e.Client,
		"user_code":  formatUserCode(e.UserCode),
		"expires_at": e.CreatedAt.Add(deviceLoginTTL),
	})
}

// DeviceLoginApprove godoc
// @Summary      Approve or deny a pending browser sign-in
// @Tags         Xác thực
// @Accept       json
// @Produce      json
// @Security     Bearer
// @Router       /auth/device/approve [post]
func (h *AuthHandler) DeviceLoginApprove(c *gin.Context) {
	ctx := c.Request.Context()
	user, err := h.userService.GetCurrentUser(ctx)
	if err != nil || user == nil {
		c.Error(errors.NewUnauthorizedError("sign in first"))
		return
	}
	var req struct {
		UserCode string `json:"user_code" binding:"required"`
		Approve  bool   `json:"approve"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("user_code is required"))
		return
	}
	key, e, err := deviceLogins.lookupByUserCode(ctx, req.UserCode)
	if err != nil {
		c.Error(errors.NewServiceUnavailableError("sign-in store unavailable"))
		return
	}
	if e == nil {
		c.Error(errors.NewNotFoundError("the code is wrong or has expired"))
		return
	}
	status := deviceLoginDenied
	if req.Approve {
		status = deviceLoginApproved
	}
	ok, err := deviceLogins.decide(ctx, key, status, user.ID)
	if err != nil {
		c.Error(errors.NewServiceUnavailableError("sign-in store unavailable"))
		return
	}
	if !ok {
		c.Error(errors.NewConflictError("this sign-in was already answered or has expired"))
		return
	}
	logger.Infof(ctx, "[DeviceLogin] %s %s a %s sign-in", user.ID, status, e.Client)
	c.JSON(http.StatusOK, gin.H{"success": true, "status": status})
}
