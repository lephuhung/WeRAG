package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// Configuration belongs to the SuperAdmin: a Tenant Admin manages its
// members and knowledge bases and inherits everything else from the
// platform. Each route below must 403 for a Tenant Admin before any handler
// runs (handlers are empty structs — reaching one would panic on nil deps).
func TestPlatformConfigRoutesDenyTenantAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g := newRBACGuards(&config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: 1, UserID: "u-admin", Role: types.TenantRoleAdmin,
		})
		ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(1))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	v1 := engine.Group("/api/v1")
	RegisterTenantRoutes(v1, &handler.TenantHandler{}, &handler.TenantMemberHandler{},
		&handler.TenantInvitationHandler{}, nil, g)
	RegisterModelRoutes(v1, &handler.ModelHandler{}, &handler.ModelCredentialsHandler{}, g)
	RegisterSandboxConfigRoutes(v1, &handler.SandboxConfigHandler{}, &handler.SandboxSkillHandler{}, g)
	RegisterMCPServiceRoutes(v1, &handler.MCPServiceHandler{}, &handler.MCPCredentialsHandler{},
		&handler.MCPOAuthHandler{}, g)
	RegisterWebSearchProviderRoutes(v1, &handler.WebSearchProviderHandler{},
		&handler.WebSearchProviderCredentialsHandler{}, g)
	RegisterVectorStoreRoutes(v1, &handler.VectorStoreHandler{}, g)
	RegisterStorageBackendRoutes(v1, &handler.StorageBackendHandler{}, g)
	RegisterDataSourceRoutes(v1, &handler.DataSourceHandler{}, &handler.DataSourceCredentialsHandler{}, g)
	RegisterSystemRoutes(v1, &handler.SystemHandler{}, g)
	RegisterMemoryRoutes(v1, &handler.MemoryHandler{}, g)
	RegisterAbbreviationRoutes(v1, &handler.AbbreviationHandler{}, g)
	RegisterMyEnvVarRoutes(v1, &handler.MeEnvVarHandler{}, g)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPut, "/api/v1/tenants/kv/memory-config", `{"enabled":false}`},
		{http.MethodPut, "/api/v1/tenants/kv/retrieval-config", `{}`},
		{http.MethodPut, "/api/v1/tenants/kv/chat-history-config", `{}`},
		{http.MethodPut, "/api/v1/tenants/1", `{"name":"x"}`},
		{http.MethodDelete, "/api/v1/tenants/1", ""},
		{http.MethodPost, "/api/v1/tenants/1/api-keys", `{}`},
		{http.MethodPost, "/api/v1/models", `{}`},
		{http.MethodPost, "/api/v1/sandbox-configs", `{}`},
		{http.MethodPost, "/api/v1/mcp-services", `{}`},
		{http.MethodPost, "/api/v1/web-search-providers", `{}`},
		{http.MethodPost, "/api/v1/vector-stores", `{}`},
		{http.MethodPost, "/api/v1/storage-backends", `{}`},
		{http.MethodPost, "/api/v1/datasource", `{}`},
		{http.MethodPost, "/api/v1/system/storage-engine-check", `{}`},
		{http.MethodPut, "/api/v1/memory/settings", `{"enabled":false}`},
		{http.MethodDelete, "/api/v1/memory/items", ""},
		{http.MethodPatch, "/api/v1/abbreviations/1", `{}`},
		{http.MethodPut, "/api/v1/me/env-vars/skill", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var req *http.Request
			if tc.body != "" {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			}
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("tenant admin %s %s must 403, got %d body=%s",
					tc.method, tc.path, w.Code, w.Body.String())
			}
		})
	}
}
