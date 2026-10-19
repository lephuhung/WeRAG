package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// addonPlansStub records the add-on calls that got past the guard.
type addonPlansStub struct {
	interfaces.UsagePlanService
	calls []string
}

func (s *addonPlansStub) ListSubscriptionAddons(_ context.Context, id string) ([]*types.UsageSubscriptionAddon, error) {
	s.calls = append(s.calls, "list "+id)
	return []*types.UsageSubscriptionAddon{}, nil
}

func (s *addonPlansStub) GrantSubscriptionAddon(
	_ context.Context, id string, g types.UsageSubscriptionAddonGrant, by string,
) (*types.UsageSubscriptionAddon, error) {
	s.calls = append(s.calls, "grant "+id+" "+g.AgentID+" by "+by)
	return &types.UsageSubscriptionAddon{SubscriptionID: id, AgentID: g.AgentID}, nil
}

func (s *addonPlansStub) RevokeSubscriptionAddon(_ context.Context, id, agentID string) error {
	s.calls = append(s.calls, "revoke "+id+" "+agentID)
	return nil
}

func serveAddonRoute(t *testing.T, stub *addonPlansStub, method, path, body string, ctxFn func(context.Context) context.Context) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	g := &rbacGuards{cfg: &config.Config{}}
	RegisterSystemAdminRoutes(engine.Group("/api/v1"), &handler.SystemHandler{}, &handler.StatsHandler{}, nil,
		handler.NewUsageAdminHandler(stub), g)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req.WithContext(ctxFn(req.Context())))
	return rec.Code
}

func TestSubscriptionAddonRoutesAreSuperAdminOnly(t *testing.T) {
	tenantAdmin := func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, types.UserIDContextKey, "admin-1")
		return types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "admin-1", Role: types.TenantRoleOwner})
	}
	superAdmin := func(ctx context.Context) context.Context {
		return context.WithValue(tenantAdmin(ctx), types.SystemAdminContextKey, true)
	}
	tenantKey := func(ctx context.Context) context.Context {
		return types.WithTenantAPIKeyScope(superAdmin(ctx), types.TenantAPIKeyScope{})
	}
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/system/admin/usage/subscriptions/sub-1/addons", ""},
		{http.MethodPost, "/api/v1/system/admin/usage/subscriptions/sub-1/addons", `{"agent_id":"thue"}`},
		{http.MethodDelete, "/api/v1/system/admin/usage/subscriptions/sub-1/addons/thue", ""},
	}
	for _, r := range routes {
		stub := &addonPlansStub{}
		if code := serveAddonRoute(t, stub, r.method, r.path, r.body, tenantAdmin); code != http.StatusForbidden {
			t.Fatalf("tenant owner %s %s = %d, want 403", r.method, r.path, code)
		}
		if code := serveAddonRoute(t, stub, r.method, r.path, r.body, tenantKey); code != http.StatusForbidden {
			t.Fatalf("tenant API key %s %s = %d, want 403", r.method, r.path, code)
		}
		if len(stub.calls) != 0 {
			t.Fatalf("refused callers reached the service: %v", stub.calls)
		}
		if code := serveAddonRoute(t, stub, r.method, r.path, r.body, superAdmin); code != http.StatusOK {
			t.Fatalf("SuperAdmin %s %s = %d, want 200", r.method, r.path, code)
		}
	}

	stub := &addonPlansStub{}
	serveAddonRoute(t, stub, http.MethodPost, routes[1].path, routes[1].body, superAdmin)
	if len(stub.calls) != 1 || stub.calls[0] != "grant sub-1 thue by admin-1" {
		t.Fatalf("grant call = %v", stub.calls)
	}
	if code := serveAddonRoute(t, &addonPlansStub{}, http.MethodPost, routes[1].path, `{}`, superAdmin); code != http.StatusBadRequest {
		t.Fatalf("grant without agent_id = %d, want 400", code)
	}
}
