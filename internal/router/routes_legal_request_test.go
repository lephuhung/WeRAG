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

// stubLegalRequestService answers every queue call; the tests only care about
// which requests reach the handler at all.
type stubLegalRequestService struct{}

func (stubLegalRequestService) List(context.Context, string, int, int) ([]*types.LegalRequestItem, int64, map[string]int64, error) {
	return []*types.LegalRequestItem{}, 0, map[string]int64{}, nil
}

func (stubLegalRequestService) Decide(_ context.Context, id string, d *types.LegalRequestDecision) (*types.LegalRequestItem, error) {
	return &types.LegalRequestItem{LegalDocumentRequest: &types.LegalDocumentRequest{ID: id, Status: types.LegalRequestAdded}}, nil
}

func (stubLegalRequestService) Claim(_ context.Context, id, action, _ string) (*types.LegalRequestItem, error) {
	return &types.LegalRequestItem{LegalDocumentRequest: &types.LegalDocumentRequest{ID: id, ClaimedBy: "u"}}, nil
}

func (stubLegalRequestService) PublicKnowledgeBases(context.Context) ([]types.LegalPublicKB, error) {
	return []types.LegalPublicKB{}, nil
}

func (stubLegalRequestService) CheckPortal(_ context.Context, id string) (*types.LegalRequestItem, error) {
	return &types.LegalRequestItem{LegalDocumentRequest: &types.LegalDocumentRequest{ID: id}}, nil
}

func (stubLegalRequestService) Get(_ context.Context, id string) (*types.LegalRequestItem, error) {
	return &types.LegalRequestItem{LegalDocumentRequest: &types.LegalDocumentRequest{ID: id}}, nil
}

// legalRequestTestEngine seeds a caller the way applyAuthSession does, so the
// real guards (not stubs) decide. The flag values come from the caller spec.
func legalRequestTestEngine(caller types.Caller, systemAdmin, legalUploader bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CallerContextKey, caller.Normalize())
		ctx = context.WithValue(ctx, types.UserIDContextKey, caller.UserID)
		ctx = context.WithValue(ctx, types.TenantIDContextKey, caller.TenantID)
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, systemAdmin)
		ctx = context.WithValue(ctx, types.LegalUploaderContextKey, legalUploader)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	g := &rbacGuards{cfg: &config.Config{}}
	RegisterLegalRequestRoutes(r.Group("/api/v1"), handler.NewLegalRequestHandler(stubLegalRequestService{}), g)
	return r
}

func serve(engine *gin.Engine, method, path, body string) int {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	engine.ServeHTTP(w, req)
	return w.Code
}

func TestLegalRequestRoutes_OperatorMayReadAndClose(t *testing.T) {
	operator := types.Caller{TenantID: types.PlatformTenantID, UserID: "u-operator"}
	engine := legalRequestTestEngine(operator, false, true)

	if code := serve(engine, http.MethodGet, "/api/v1/system/legal-requests", ""); code != http.StatusOK {
		t.Fatalf("operator GET = %d, want 200", code)
	}
	body := `{"action":"added","knowledge_id":"k1"}`
	if code := serve(engine, http.MethodPut, "/api/v1/system/legal-requests/r1", body); code != http.StatusOK {
		t.Fatalf("operator PUT added = %d, want 200", code)
	}
	// The routes that stay SuperAdmin-only must not be reachable by the operator.
	if code := serve(engine, http.MethodGet, "/api/v1/system/legal-requests/public-knowledge-bases", ""); code != http.StatusForbidden {
		t.Fatalf("operator public-knowledge-bases = %d, want 403", code)
	}
	if code := serve(engine, http.MethodPost, "/api/v1/system/legal-requests/r1/portal-check", ""); code != http.StatusForbidden {
		t.Fatalf("operator portal-check = %d, want 403", code)
	}
}

func TestLegalRequestRoutes_MemberWithoutFlagIsRefused(t *testing.T) {
	member := types.Caller{TenantID: 7, UserID: "u-member", Role: types.TenantRoleMember}
	engine := legalRequestTestEngine(member, false, false)

	if code := serve(engine, http.MethodGet, "/api/v1/system/legal-requests", ""); code != http.StatusForbidden {
		t.Fatalf("member GET = %d, want 403", code)
	}
	if code := serve(engine, http.MethodPut, "/api/v1/system/legal-requests/r1", `{"action":"added"}`); code != http.StatusForbidden {
		t.Fatalf("member PUT = %d, want 403", code)
	}
}

func TestLegalRequestRoutes_SuperAdminKeepsEverything(t *testing.T) {
	admin := types.Caller{TenantID: types.PlatformTenantID, UserID: "u-admin"}
	engine := legalRequestTestEngine(admin, true, false)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/system/legal-requests", ""},
		{http.MethodPut, "/api/v1/system/legal-requests/r1", `{"action":"reject","note":"no"}`},
		{http.MethodGet, "/api/v1/system/legal-requests/public-knowledge-bases", ""},
	} {
		if code := serve(engine, tc.method, tc.path, tc.body); code != http.StatusOK {
			t.Fatalf("%s %s = %d, want 200", tc.method, tc.path, code)
		}
	}
}
