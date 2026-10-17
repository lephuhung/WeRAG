package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type deviceTestUsers struct {
	interfaces.UserService
	minted int
}

func (s *deviceTestUsers) GetCurrentUser(ctx context.Context) (*types.User, error) {
	return &types.User{ID: "approver", IsActive: true}, nil
}

func (s *deviceTestUsers) GetUserByID(_ context.Context, id string) (*types.User, error) {
	return &types.User{ID: id, IsActive: true}, nil
}

func (s *deviceTestUsers) GenerateTokens(context.Context, *types.User) (string, string, error) {
	s.minted++
	return "access", "refresh", nil
}

func callDevice(t *testing.T, fn gin.HandlerFunc, method, target string, body any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(method, target, &buf)
	ctx.Request.Header.Set("Content-Type", "application/json")
	fn(ctx)
	if len(ctx.Errors) > 0 {
		return map[string]any{"error": ctx.Errors.Last().Error()}
	}
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func TestDeviceLoginFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &deviceTestUsers{}
	h := &AuthHandler{userService: users}

	start := callDevice(t, h.DeviceLoginStart, "POST", "/", map[string]string{"client": "word"})
	device, _ := start["device_code"].(string)
	code, _ := start["user_code"].(string)
	require.NotEmpty(t, device)
	require.Len(t, code, 9)

	poll := callDevice(t, h.DeviceLoginPoll, "POST", "/", map[string]string{"device_code": device})
	require.Equal(t, "pending", poll["status"])
	require.Nil(t, poll["token"])

	look := callDevice(t, h.DeviceLoginLookup, "GET", "/?user_code="+code, nil)
	require.Equal(t, "pending", look["status"])

	// lower case and no dash still match
	lower := []byte(code[:4] + code[5:])
	for i := range lower {
		lower[i] |= 0x20
	}
	ok := callDevice(t, h.DeviceLoginApprove, "POST", "/", map[string]any{"user_code": string(lower), "approve": true})
	require.Equal(t, "approved", ok["status"])

	again := callDevice(t, h.DeviceLoginApprove, "POST", "/", map[string]any{"user_code": code, "approve": false})
	require.Contains(t, again["error"], "already answered")

	got := callDevice(t, h.DeviceLoginPoll, "POST", "/", map[string]string{"device_code": device})
	require.Equal(t, "approved", got["status"])
	require.Equal(t, "access", got["token"])
	require.Equal(t, "refresh", got["refresh_token"])

	// one approval, one token pair
	replay := callDevice(t, h.DeviceLoginPoll, "POST", "/", map[string]string{"device_code": device})
	require.Equal(t, "expired", replay["status"])
	require.Equal(t, 1, users.minted)
}

func TestDeviceLoginDenyAndUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &deviceTestUsers{}
	h := &AuthHandler{userService: users}

	start := callDevice(t, h.DeviceLoginStart, "POST", "/", nil)
	device := start["device_code"].(string)
	callDevice(t, h.DeviceLoginApprove, "POST", "/", map[string]any{"user_code": start["user_code"], "approve": false})
	poll := callDevice(t, h.DeviceLoginPoll, "POST", "/", map[string]string{"device_code": device})
	require.Equal(t, "denied", poll["status"])

	unknown := callDevice(t, h.DeviceLoginPoll, "POST", "/", map[string]string{"device_code": "nope"})
	require.Equal(t, "expired", unknown["status"])
	bad := callDevice(t, h.DeviceLoginApprove, "POST", "/", map[string]any{"user_code": "AAAA-0000", "approve": true})
	require.Contains(t, bad["error"], "expired")
	require.Zero(t, users.minted)
}

func TestNormalizeUserCode(t *testing.T) {
	require.Equal(t, "BCDFGHJK", normalizeUserCode(" bcdf-ghjk "))
	require.Equal(t, "", normalizeUserCode("BCDF-GHJ"))
	require.Equal(t, "", normalizeUserCode("BCDF-GHJA"))
}
