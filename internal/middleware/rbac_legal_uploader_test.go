package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/Tencent/WeKnora/internal/types"
)

// legalUploaderHarness seeds the identity the auth middleware would attach and
// runs the guard behind it.
func legalUploaderHarness(caller types.Caller, principal types.Principal, systemAdmin, legalUploader bool, apiKey bool) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CallerContextKey, caller.Normalize())
		ctx = context.WithValue(ctx, types.UserIDContextKey, caller.UserID)
		ctx = context.WithValue(ctx, types.TenantIDContextKey, caller.TenantID)
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, systemAdmin)
		ctx = context.WithValue(ctx, types.LegalUploaderContextKey, legalUploader)
		ctx = types.WithPrincipal(ctx, principal)
		if apiKey {
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{ScopeType: types.APIKeyScopeTenant})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/system/legal-requests", RequireLegalUploader(cfgRBAC(true)), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/system/legal-requests", nil))
	return w
}

func TestRequireLegalUploader(t *testing.T) {
	human := func(id string) (types.Caller, types.Principal) {
		return types.Caller{TenantID: types.PlatformTenantID, UserID: id},
			types.Principal{Type: types.PrincipalWebUser, ID: id}
	}

	t.Run("human SuperAdmin passes without the flag", func(t *testing.T) {
		caller, principal := human("u-admin")
		assert.Equal(t, http.StatusOK, legalUploaderHarness(caller, principal, true, false, false).Code)
	})

	t.Run("human with can_upload_legal_documents passes", func(t *testing.T) {
		caller, principal := human("u-operator")
		assert.Equal(t, http.StatusOK, legalUploaderHarness(caller, principal, false, true, false).Code)
	})

	t.Run("tenantless operator still passes", func(t *testing.T) {
		caller := types.Caller{TenantID: 0, UserID: "u-operator"}
		principal := types.Principal{Type: types.PrincipalWebUser, ID: "u-operator"}
		assert.Equal(t, http.StatusOK, legalUploaderHarness(caller, principal, false, true, false).Code)
	})

	t.Run("member without the flag is 403", func(t *testing.T) {
		caller, principal := human("u-member")
		assert.Equal(t, http.StatusForbidden, legalUploaderHarness(caller, principal, false, false, false).Code)
	})

	t.Run("tenantless human without the flag is 401", func(t *testing.T) {
		caller := types.Caller{TenantID: 0, UserID: "u-stranger"}
		principal := types.Principal{Type: types.PrincipalWebUser, ID: "u-stranger"}
		assert.Equal(t, http.StatusUnauthorized, legalUploaderHarness(caller, principal, false, false, false).Code)
	})

	t.Run("synthetic user never qualifies", func(t *testing.T) {
		caller := types.Caller{TenantID: 7, UserID: "system-7"}
		principal := types.Principal{Type: types.PrincipalWebUser, ID: "system-7"}
		assert.Equal(t, http.StatusForbidden, legalUploaderHarness(caller, principal, false, true, false).Code)
	})

	t.Run("embed session never qualifies", func(t *testing.T) {
		caller := types.Caller{TenantID: 7, UserID: "u-embed"}
		principal := types.Principal{Type: types.PrincipalEmbedSession, ID: "u-embed"}
		assert.Equal(t, http.StatusForbidden, legalUploaderHarness(caller, principal, false, true, false).Code)
	})

	t.Run("API key never qualifies through the flag", func(t *testing.T) {
		caller, principal := human("api-key-user")
		assert.Equal(t, http.StatusForbidden, legalUploaderHarness(caller, principal, false, true, true).Code)
	})
}
