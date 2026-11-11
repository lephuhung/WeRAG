package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// memberReportStub records the reports that got past the route guards.
type memberReportStub struct {
	interfaces.UsagePlanService
	calls []string
}

func (s *memberReportStub) TenantMemberUsage(
	_ context.Context, tenantID uint64, month string, operator bool,
) (*types.UsageMemberReport, error) {
	s.calls = append(s.calls, strconv.FormatUint(tenantID, 10)+" "+month+" cost="+strconv.FormatBool(operator))
	return &types.UsageMemberReport{TenantID: tenantID, Month: month, CostVisible: operator,
		Members: []*types.UsageMemberUsage{}}, nil
}

type memberReportCaller struct {
	user   string
	tenant uint64 // the active organization
	role   types.TenantRole
	super  bool
	apiKey bool // a full-access organization API key
}

func serveMemberReport(t *testing.T, stub *memberReportStub, who memberReportCaller, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	guards := &rbacGuards{cfg: &config.Config{}}
	v1 := engine.Group("/api/v1")
	// the real API-key gate, as router.go mounts it on /api/v1
	v1.Use(guards.ensureAPIKeyAuthorizer().Middleware())
	RegisterTenantUsageRoutes(v1, handler.NewTenantUsageHandler(stub), guards)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := context.WithValue(req.Context(), types.TenantIDContextKey, who.tenant)
	ctx = context.WithValue(ctx, types.UserIDContextKey, who.user)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: who.tenant, UserID: who.user, Role: who.role})
	if who.super {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	if who.apiKey {
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 7, ScopeType: types.APIKeyScopeTenant, FullAccess: true})
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// Only the organization's admins and a SuperAdmin read the per-member
// report; a member, or the admin of another organization, never reaches
// the service. Only a SuperAdmin gets the cost.
func TestTenantMemberUsageRouteAccess(t *testing.T) {
	path := "/api/v1/tenants/10003/usage/members?month=2026-10"
	cases := []struct {
		name string
		who  memberReportCaller
		want int
		call string
	}{
		{"admin of the organization", memberReportCaller{user: "u-admin", tenant: 10003, role: types.TenantRoleAdmin}, http.StatusOK, "10003 2026-10 cost=false"},
		{"member", memberReportCaller{user: "u-member", tenant: 10003, role: types.TenantRoleMember}, http.StatusForbidden, ""},
		{"admin of another organization", memberReportCaller{user: "u-other", tenant: 10004, role: types.TenantRoleAdmin}, http.StatusForbidden, ""},
		{"SuperAdmin in the organization", memberReportCaller{user: "u-super", tenant: 10003, super: true}, http.StatusOK, "10003 2026-10 cost=true"},
		{"SuperAdmin elsewhere", memberReportCaller{user: "u-super", tenant: 10001, super: true}, http.StatusOK, "10003 2026-10 cost=true"},
		{"organization API key", memberReportCaller{user: "system-10003", tenant: 10003, role: types.TenantRoleAdmin, apiKey: true}, http.StatusForbidden, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := &memberReportStub{}
			rec := serveMemberReport(t, stub, c.who, path)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, c.want, rec.Body.String())
			}
			if c.call == "" {
				if len(stub.calls) != 0 {
					t.Fatalf("refused caller reached the service: %v", stub.calls)
				}
				return
			}
			if len(stub.calls) != 1 || stub.calls[0] != c.call {
				t.Fatalf("calls = %v, want [%s]", stub.calls, c.call)
			}
			if c.who.super != strings.Contains(rec.Body.String(), `"cost_visible":true`) {
				t.Fatalf("cost visibility wrong: %s", rec.Body.String())
			}
		})
	}
}
