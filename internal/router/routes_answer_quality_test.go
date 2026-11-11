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
	"github.com/Tencent/WeKnora/internal/types"
)

// stubAnswerQuality answers every call: reaching it means the guards let
// the request through.
type stubAnswerQuality struct{}

func (stubAnswerQuality) ListDisliked(context.Context, bool, types.AnswerQualityFilter) (*types.AnswerQualityPage, error) {
	return &types.AnswerQualityPage{}, nil
}
func (stubAnswerQuality) ListUngrounded(context.Context, bool, types.AnswerQualityFilter) (*types.AnswerQualityPage, error) {
	return &types.AnswerQualityPage{}, nil
}
func (stubAnswerQuality) GroupUngrounded(context.Context, bool, types.AnswerQualityFilter) (*types.UngroundedGroupPage, error) {
	return &types.UngroundedGroupPage{}, nil
}
func (stubAnswerQuality) Review(context.Context, bool, string, types.AnswerQualityReviewInput) (*types.AnswerQualityReview, error) {
	return &types.AnswerQualityReview{}, nil
}

var answerQualityTenantRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/v1/quality/disliked"},
	{http.MethodGet, "/api/v1/quality/ungrounded"},
	{http.MethodGet, "/api/v1/quality/ungrounded/groups"},
	{http.MethodPut, "/api/v1/quality/reviews/m1"},
}

var answerQualityPlatformRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/v1/system/quality/disliked"},
	{http.MethodGet, "/api/v1/system/quality/ungrounded"},
	{http.MethodGet, "/api/v1/system/quality/ungrounded/groups"},
	{http.MethodPut, "/api/v1/system/quality/reviews/m1"},
}

// answerQualityEngine mounts the routes behind the API-key gate, with the
// RBAC rollout flag off (TenantAdmin must deny Members anyway).
func answerQualityEngine(setCtx func(context.Context) context.Context) *gin.Engine {
	gin.SetMode(gin.TestMode)
	off := false
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &off}}
	g := newRBACGuards(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(setCtx(c.Request.Context()))
		c.Next()
	})
	v1 := engine.Group("/api/v1", g.ensureAPIKeyAuthorizer().Middleware())
	RegisterAnswerQualityRoutes(v1, handler.NewAnswerQualityHandler(stubAnswerQuality{}), g)
	return engine
}

func human(tenantID uint64, userID string, role types.TenantRole, superAdmin bool) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, superAdmin)
		return types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: userID, Role: role})
	}
}

func serveAnswerQuality(engine *gin.Engine, method, path string) int {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(`{"kind":"disliked"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	return w.Code
}

func TestAnswerQualityRoutesAuth(t *testing.T) {
	apiKey := func(ctx context.Context) context.Context {
		ctx = human(1, "u-admin", types.TenantRoleAdmin, false)(ctx)
		return types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, FullAccess: true})
	}
	cases := []struct {
		name             string
		ctx              func(context.Context) context.Context
		tenant, platform int
	}{
		{"member", human(1, "u-member", types.TenantRoleMember, false), http.StatusForbidden, http.StatusForbidden},
		{"guest", human(1, "u-guest", "", false), http.StatusForbidden, http.StatusForbidden},
		{"api key", apiKey, http.StatusForbidden, http.StatusForbidden},
		{"tenant admin", human(1, "u-admin", types.TenantRoleAdmin, false), http.StatusOK, http.StatusForbidden},
		{"superadmin", human(1, "u-root", "", true), http.StatusOK, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := answerQualityEngine(tc.ctx)
			for _, r := range answerQualityTenantRoutes {
				if got := serveAnswerQuality(engine, r.method, r.path); got != tc.tenant {
					t.Errorf("%s %s = %d, want %d", r.method, r.path, got, tc.tenant)
				}
			}
			for _, r := range answerQualityPlatformRoutes {
				if got := serveAnswerQuality(engine, r.method, r.path); got != tc.platform {
					t.Errorf("%s %s = %d, want %d", r.method, r.path, got, tc.platform)
				}
			}
		})
	}
}
