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

func (s *addonPlansStub) ListAgentAddons(context.Context, bool) (*types.UsageAgentAddonCatalogue, error) {
	s.calls = append(s.calls, "catalogue")
	return &types.UsageAgentAddonCatalogue{}, nil
}

func (s *addonPlansStub) CreateAgentAddon(_ context.Context, row *types.UsageAgentAddon) (*types.UsageAgentAddon, error) {
	s.calls = append(s.calls, "create "+row.AgentID)
	return row, nil
}

func (s *addonPlansStub) UpdateAgentAddon(_ context.Context, row *types.UsageAgentAddon) (*types.UsageAgentAddon, error) {
	s.calls = append(s.calls, "update "+row.AgentID)
	return row, nil
}

func (s *addonPlansStub) DeleteAgentAddon(_ context.Context, agentID string) error {
	s.calls = append(s.calls, "delete "+agentID)
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
		{http.MethodGet, "/api/v1/system/admin/usage/agent-addons", ""},
		{http.MethodPost, "/api/v1/system/admin/usage/agent-addons", `{"agent_id":"thue","price_vnd":49000}`},
		{http.MethodPut, "/api/v1/system/admin/usage/agent-addons/thue", `{"price_vnd":59000}`},
		{http.MethodDelete, "/api/v1/system/admin/usage/agent-addons/thue", ""},
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

func TestAgentAddonCatalogueUpdateTakesAgentFromPath(t *testing.T) {
	stub := &addonPlansStub{}
	superAdmin := func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, types.UserIDContextKey, "super-1")
		ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "super-1", Role: types.TenantRoleOwner})
		return context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	code := serveAddonRoute(t, stub, http.MethodPut, "/api/v1/system/admin/usage/agent-addons/thue",
		`{"agent_id":"other","price_vnd":1}`, superAdmin)
	if code != http.StatusOK || len(stub.calls) != 1 || stub.calls[0] != "update thue" {
		t.Fatalf("update = %d %v, want the path's agent", code, stub.calls)
	}
}

// billingPlansStub answers GET /billing/plans with one offer.
type billingPlansStub struct {
	interfaces.UsageBillingService
}

func (billingPlansStub) PurchasablePlans(context.Context, string) ([]*types.UsagePlanOffer, error) {
	return []*types.UsagePlanOffer{{
		UsagePlan:      &types.UsagePlan{ID: "go", Name: "Go", Mode: types.UsageModeSeat, PriceVND: 49000},
		IncludedAgents: []types.UsagePlanAgent{{ID: types.BuiltinDocumentAssistantID, Name: "Soạn thảo văn bản"}},
		Addons:         []types.UsagePlanAddonOffer{{AgentID: "thue", Name: "Thuế", PriceVND: 99000, PerSeat: true}},
	}}, nil
}

// Buyers (any member) read the plans with their basic agents and add-ons;
// the plan's own fields stay at the top level for older clients.
func TestBillingPlansCarryAgents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	RegisterBillingRoutes(engine.Group("/api/v1"), handler.NewUsageBillingHandler(billingPlansStub{}),
		&rbacGuards{cfg: &config.Config{}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/plans?scope=tenant", nil)
	ctx := types.WithCaller(req.Context(), types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleMember})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /billing/plans = %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"id":"go"`, `"price_vnd":49000`, `"included_agents":[{"id":"builtin-document-assistant"`,
		`"addons":[{"agent_id":"thue","name":"Thuế","description":"","price_vnd":99000,"per_seat":true}]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %s: %s", want, body)
		}
	}
}

func (billingPlansStub) ScopeAddons(_ context.Context, scopeType, scopeID string) (*types.UsageScopeAddons, error) {
	return &types.UsageScopeAddons{Held: []*types.UsageSubscriptionAddon{{AgentID: scopeType + ":" + scopeID}}}, nil
}

// The buy page reads its scope's add-ons: the Tổ chức needs a Tenant Admin,
// the personal scope is the caller.
func TestBillingSubscriptionScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	RegisterBillingRoutes(engine.Group("/api/v1"), handler.NewUsageBillingHandler(billingPlansStub{}),
		&rbacGuards{cfg: &config.Config{}})
	get := func(path string, role types.TenantRole) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		ctx := context.WithValue(req.Context(), types.TenantIDContextKey, uint64(10003))
		ctx = context.WithValue(ctx, types.UserIDContextKey, "u-7")
		ctx = types.WithCaller(ctx, types.Caller{TenantID: 10003, UserID: "u-7", Role: role})
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req.WithContext(ctx))
		return rec
	}
	if rec := get("/api/v1/billing/subscription?scope=tenant", types.TenantRoleMember); rec.Code != http.StatusForbidden {
		t.Fatalf("member, tenant scope = %d", rec.Code)
	}
	rec := get("/api/v1/billing/subscription?scope=tenant", types.TenantRoleAdmin)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"agent_id":"tenant:10003"`) {
		t.Fatalf("admin, tenant scope = %d %s", rec.Code, rec.Body.String())
	}
	rec = get("/api/v1/billing/subscription?scope=user", types.TenantRoleMember)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"agent_id":"user:u-7"`) {
		t.Fatalf("member, user scope = %d %s", rec.Code, rec.Body.String())
	}
}
