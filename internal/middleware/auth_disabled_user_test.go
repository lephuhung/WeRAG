package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type disabledUserService struct {
	interfaces.UserService
}

func (disabledUserService) ValidateToken(context.Context, string) (*types.User, uint64, error) {
	return nil, 0, service.ErrUserDisabled
}

// A locked account presenting a still-valid access token gets 401 with the
// login form's wording, not the generic expired-session message.
func TestAuthRejectsDisabledUserWithLoginMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Auth(nil, disabledUserService{}, nil, nil, nil))
	r.GET("/api/v1/knowledge-bases", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer still-valid-jwt")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, service.ErrUserDisabled.Error(), body["error"])
	require.Equal(t, "ACCOUNT_DISABLED", body["code"])
}
