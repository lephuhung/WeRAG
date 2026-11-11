package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// costlyMeter reports counters that carry the operator's cost.
type costlyMeter struct{ interfaces.UsageMeter }

func (costlyMeter) Summary(context.Context, uint64, string) (*types.UsageSummary, error) {
	totals := types.UsagePeriodTotals{Questions: 3, Tokens: 900, CostMicroVND: 4_200_000}
	return &types.UsageSummary{
		User:   &types.UsageScopeSummary{ScopeType: "user", Today: totals, Month: totals},
		Tenant: &types.UsageScopeSummary{ScopeType: "tenant", Today: totals, Month: totals},
	}, nil
}

func serveMeUsage(t *testing.T, user string, role types.TenantRole, super bool) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/me/usage", NewMeUsageHandler(costlyMeter{}, nil).Get)
	req := httptest.NewRequest(http.MethodGet, "/me/usage", nil)
	ctx := context.WithValue(req.Context(), types.TenantIDContextKey, uint64(10003))
	ctx = context.WithValue(ctx, types.UserIDContextKey, user)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 10003, UserID: user, Role: role})
	if super {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// What the calls cost is the operator's spend: /me/usage leaves it off the
// wire for everyone but an explicit human SuperAdmin.
func TestMeUsageHidesTheOperatorCost(t *testing.T) {
	for _, who := range []struct {
		user string
		role types.TenantRole
	}{{"u-admin", types.TenantRoleAdmin}, {"u-member", types.TenantRoleMember}} {
		body := serveMeUsage(t, who.user, who.role, false)
		if strings.Contains(body, "cost_micro_vnd") {
			t.Fatalf("%s sees the cost: %s", who.user, body)
		}
		if !strings.Contains(body, `"tokens":900`) {
			t.Fatalf("%s lost the token figures: %s", who.user, body)
		}
	}
	if body := serveMeUsage(t, "u-super", types.TenantRoleMember, true); strings.Count(body, `"cost_micro_vnd":4200000`) != 4 {
		t.Fatalf("SuperAdmin should see the cost of both scopes and periods: %s", body)
	}
	// the system-admin flag on a machine principal is not a SuperAdmin
	if body := serveMeUsage(t, "system-10003", types.TenantRoleAdmin, true); strings.Contains(body, "cost_micro_vnd") {
		t.Fatalf("synthetic principal sees the cost: %s", body)
	}
}
