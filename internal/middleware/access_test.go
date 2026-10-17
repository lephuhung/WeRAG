package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// access_test.go covers the four exported helpers in access.go:
// IsPlatformSuperAdmin, IsTenantAccessible, RequireCrossTenantAccess,
// RequirePathTenantMatch. The shapes are similar — feature-flag +
// per-user attribute combinations — so the tests are organised around
// the truth-table for each helper rather than per-handler scenarios.

// cfgCrossTenant returns a config with the cluster-wide
// EnableCrossTenantAccess flag set as requested. EnableRBAC is left at
// its zero value because none of these helpers consult it directly —
// the role middleware does.
func cfgCrossTenant(enabled bool) *config.Config {
	return &config.Config{Tenant: &config.TenantConfig{EnableCrossTenantAccess: enabled}}
}

// superAdminCtx marks ctx the way applyAuthSession does for a signed-in
// user: the user, its ID, the caller and the system-admin flag.
func superAdminCtx(ctx context.Context, user *types.User) context.Context {
	ctx = context.WithValue(ctx, types.UserContextKey, user)
	ctx = context.WithValue(ctx, types.UserIDContextKey, user.ID)
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, user.IsSystemAdmin)
	return ctx
}

// ---------- IsPlatformSuperAdmin ----------

func TestIsPlatformSuperAdmin(t *testing.T) {
	sa := &types.User{ID: "sa1", IsSystemAdmin: true}
	if !IsPlatformSuperAdmin(superAdminCtx(context.Background(), sa)) {
		t.Fatalf("explicit human SuperAdmin must qualify")
	}
	flagOnly := &types.User{ID: "u1", CanAccessAllTenants: true}
	if IsPlatformSuperAdmin(superAdminCtx(context.Background(), flagOnly)) {
		t.Fatalf("the legacy CanAccessAllTenants attribute must not qualify")
	}
	synthetic := &types.User{ID: "system-7", IsSystemAdmin: true}
	if IsPlatformSuperAdmin(superAdminCtx(context.Background(), synthetic)) {
		t.Fatalf("a synthetic identity must not qualify")
	}
	keyCtx := types.WithTenantAPIKeyScope(superAdminCtx(context.Background(), sa),
		types.TenantAPIKeyScope{FullAccess: true})
	if IsPlatformSuperAdmin(keyCtx) {
		t.Fatalf("an API-key principal must not qualify")
	}
	if IsPlatformSuperAdmin(context.Background()) {
		t.Fatalf("missing user must reject")
	}
}

// ---------- IsTenantAccessible ----------

func TestIsTenantAccessible_HomeTenantAlwaysAllows(t *testing.T) {
	user := &types.User{ID: "u1", TenantID: 1}
	// Even with no member service and the cross-tenant flag off, a user
	// reaching their own tenant is fine. Home tenant is the cheapest
	// fast path.
	if !IsTenantAccessible(context.Background(), user, 1, nil, cfgCrossTenant(false)) {
		t.Fatalf("home tenant must allow regardless of other inputs")
	}
}

func TestIsTenantAccessible_NilUserRejects(t *testing.T) {
	if IsTenantAccessible(context.Background(), nil, 1, nil, cfgCrossTenant(true)) {
		t.Fatalf("nil user must reject (defensive)")
	}
}

func TestIsTenantAccessible_ZeroTargetRejects(t *testing.T) {
	user := &types.User{ID: "u1", TenantID: 1}
	if IsTenantAccessible(context.Background(), user, 0, nil, cfgCrossTenant(true)) {
		t.Fatalf("zero target tenant must reject")
	}
}

func TestIsTenantAccessible_LegacyFlagNeverGrants(t *testing.T) {
	user := &types.User{ID: "u1", TenantID: 1, CanAccessAllTenants: true}
	// The legacy attribute grants no tenant selection, with the cluster
	// flag off or on; only SuperAdmin or an active membership does.
	if IsTenantAccessible(context.Background(), user, 99, nil, cfgCrossTenant(false)) {
		t.Fatalf("legacy flag without cluster flag must reject cross-tenant target")
	}
	if IsTenantAccessible(context.Background(), user, 99, nil, cfgCrossTenant(true)) {
		t.Fatalf("legacy flag with cluster flag must reject cross-tenant target")
	}
}

func TestIsTenantAccessible_ActiveMembershipAllows(t *testing.T) {
	user := &types.User{ID: "u1", TenantID: 1}
	ms := newFakeMemberService()
	ms.seedActive("u1", 99, types.TenantRoleMember)
	if !IsTenantAccessible(context.Background(), user, 99, ms, cfgCrossTenant(false)) {
		t.Fatalf("active membership must allow even with flag off and no superuser")
	}
}

func TestIsTenantAccessible_NoMembershipRejects(t *testing.T) {
	user := &types.User{ID: "u1", TenantID: 1}
	ms := newFakeMemberService() // empty
	if IsTenantAccessible(context.Background(), user, 99, ms, cfgCrossTenant(true)) {
		t.Fatalf("no membership and not superuser must reject")
	}
}

func TestIsTenantAccessible_LookupErrorRejects(t *testing.T) {
	// A DB hiccup must NOT silently grant access; the safest behaviour
	// is "treat as no membership". The X-Tenant-ID gate in auth.go
	// relies on this — failing closed prevents tenant-bleed during
	// transient outages.
	user := &types.User{ID: "u1", TenantID: 1}
	ms := newFakeMemberService()
	ms.failGet = errors.New("db down")
	if IsTenantAccessible(context.Background(), user, 99, ms, cfgCrossTenant(true)) {
		t.Fatalf("lookup error must reject (fail closed)")
	}
}

func TestIsTenantAccessible_NilMemberServiceRejectsNonHome(t *testing.T) {
	user := &types.User{ID: "u1", TenantID: 1}
	if IsTenantAccessible(context.Background(), user, 99, nil, cfgCrossTenant(false)) {
		t.Fatalf("no member service + non-home tenant must reject")
	}
}

// ---------- RequireCrossTenantAccess ----------

func runCrossTenantHandler(cfg *config.Config, user *types.User) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// ErrorHandler renders c.Error() into the standard envelope and
	// sets the status from AppError.HTTPCode. Mounting it here mirrors
	// the production router so the tests assert what real clients see.
	r.Use(ErrorHandler())
	r.Use(func(c *gin.Context) {
		if user != nil {
			c.Request = c.Request.WithContext(superAdminCtx(c.Request.Context(), user))
		}
		c.Next()
	})
	r.GET("/cross", RequireCrossTenantAccess(cfg), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cross", nil)
	r.ServeHTTP(w, req)
	return w
}

// The legacy attribute grants nothing, with or without the cluster flag.
func TestRequireCrossTenantAccess_LegacyFlagBlocks(t *testing.T) {
	for _, on := range []bool{false, true} {
		w := runCrossTenantHandler(cfgCrossTenant(on),
			&types.User{ID: "u1", CanAccessAllTenants: true})
		if w.Code != http.StatusForbidden {
			t.Fatalf("cluster flag=%v: CanAccessAllTenants alone must 403, got %d", on, w.Code)
		}
	}
}

func TestRequireCrossTenantAccess_OrdinaryUserBlocks(t *testing.T) {
	w := runCrossTenantHandler(cfgCrossTenant(true), &types.User{ID: "u1"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("ordinary user must 403 even with flag on, got %d", w.Code)
	}
}

// Platform authority is IsSystemAdmin only; the cluster flag is not
// consulted.
func TestRequireCrossTenantAccess_SuperAdminAllows(t *testing.T) {
	for _, on := range []bool{false, true} {
		w := runCrossTenantHandler(cfgCrossTenant(on),
			&types.User{ID: "sa1", IsSystemAdmin: true})
		if w.Code != http.StatusOK {
			t.Fatalf("cluster flag=%v: SuperAdmin must pass, got %d", on, w.Code)
		}
	}
}

func TestRequireCrossTenantAccess_NoUserBlocks(t *testing.T) {
	w := runCrossTenantHandler(cfgCrossTenant(true), nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing user must 403, got %d", w.Code)
	}
}

// ---------- RequirePathTenantMatch ----------

func runPathTenantHandler(
	cfg *config.Config, ctxTenantID uint64, user *types.User, urlPath string,
) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ErrorHandler()) // see runCrossTenantHandler for rationale
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if ctxTenantID != 0 {
			ctx = context.WithValue(ctx, types.TenantIDContextKey, ctxTenantID)
		}
		if user != nil {
			ctx = superAdminCtx(ctx, user)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/tenants/:id", RequirePathTenantMatch(cfg), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, urlPath, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestRequirePathTenantMatch_MatchAllows(t *testing.T) {
	w := runPathTenantHandler(cfgCrossTenant(false), 7, nil, "/tenants/7")
	if w.Code != http.StatusOK {
		t.Fatalf("matching :id must allow, got %d", w.Code)
	}
}

func TestRequirePathTenantMatch_MismatchRejects(t *testing.T) {
	w := runPathTenantHandler(cfgCrossTenant(false), 7, nil, "/tenants/9")
	if w.Code != http.StatusForbidden {
		t.Fatalf("mismatch must 403, got %d", w.Code)
	}
}

func TestRequirePathTenantMatch_SuperAdminBypassesMismatch(t *testing.T) {
	// A SuperAdmin's authority does not depend on the active tenant; the
	// cluster flag is not consulted.
	for _, on := range []bool{false, true} {
		w := runPathTenantHandler(cfgCrossTenant(on), 7,
			&types.User{ID: "sa1", IsSystemAdmin: true},
			"/tenants/9")
		if w.Code != http.StatusOK {
			t.Fatalf("cluster flag=%v: SuperAdmin must bypass mismatch, got %d", on, w.Code)
		}
	}
}

func TestRequirePathTenantMatch_LegacyFlagDoesNotBypass(t *testing.T) {
	for _, on := range []bool{false, true} {
		w := runPathTenantHandler(cfgCrossTenant(on), 7,
			&types.User{ID: "su1", CanAccessAllTenants: true},
			"/tenants/9")
		if w.Code != http.StatusForbidden {
			t.Fatalf("cluster flag=%v: CanAccessAllTenants alone must 403, got %d", on, w.Code)
		}
	}
}

func TestRequirePathTenantMatch_MissingCtxTenantRejects(t *testing.T) {
	// If the auth middleware never set TenantIDContextKey we fail
	// closed — silently treating "no ctx" as a match would be a footgun
	// the next time someone refactors the auth chain.
	w := runPathTenantHandler(cfgCrossTenant(true), 0, nil, "/tenants/7")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing tenant ctx must 401, got %d", w.Code)
	}
}

func TestRequirePathTenantMatch_NonNumericIDRejects(t *testing.T) {
	w := runPathTenantHandler(cfgCrossTenant(true), 7, nil, "/tenants/abc")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric :id must 400, got %d", w.Code)
	}
}

func TestRequirePathTenantMatch_ZeroIDRejects(t *testing.T) {
	w := runPathTenantHandler(cfgCrossTenant(true), 7, nil, "/tenants/0")
	if w.Code != http.StatusBadRequest {
		t.Fatalf(":id=0 must 400, got %d", w.Code)
	}
}

// TestAccessMiddleware_ResponseEnvelope pins the wire format produced
// by RequireCrossTenantAccess and RequirePathTenantMatch when they
// reject. Both used to live in handlers that called
// c.Error(apperrors.NewForbiddenError(...)) and rendered through
// ErrorHandler — clients (frontend axios interceptor, Go SDK) key off
// `success` and `error.code`. If a future change drops c.Error in
// favour of a raw c.JSON, this test catches the regression.
func TestAccessMiddleware_ResponseEnvelope(t *testing.T) {
	t.Run("RequireCrossTenantAccess_NotSuperAdmin", func(t *testing.T) {
		w := runCrossTenantHandler(cfgCrossTenant(false),
			&types.User{ID: "u1", CanAccessAllTenants: true})
		assertEnvelope(t, w, http.StatusForbidden, apperrors.ErrForbidden)
	})
	t.Run("RequirePathTenantMatch_Mismatch", func(t *testing.T) {
		w := runPathTenantHandler(cfgCrossTenant(false), 7, nil, "/tenants/9")
		assertEnvelope(t, w, http.StatusForbidden, apperrors.ErrForbidden)
	})
	t.Run("RequirePathTenantMatch_NonNumeric", func(t *testing.T) {
		w := runPathTenantHandler(cfgCrossTenant(true), 7, nil, "/tenants/abc")
		// NewValidationError uses ErrValidation, not ErrBadRequest — both
		// surface as HTTP 400 but the code in the body is ErrValidation.
		assertEnvelope(t, w, http.StatusBadRequest, apperrors.ErrValidation)
	})
	t.Run("RequirePathTenantMatch_NoCtxTenant", func(t *testing.T) {
		w := runPathTenantHandler(cfgCrossTenant(true), 0, nil, "/tenants/7")
		assertEnvelope(t, w, http.StatusUnauthorized, apperrors.ErrUnauthorized)
	})
}

func assertEnvelope(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode apperrors.ErrorCode) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("status: got %d, want %d (body=%s)", w.Code, wantStatus, w.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code    apperrors.ErrorCode `json:"code"`
			Message string              `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the standard envelope: %v (body=%s)", err, w.Body.String())
	}
	if body.Success {
		t.Fatalf("envelope success must be false on rejection, got body=%s", w.Body.String())
	}
	if body.Error.Code != wantCode {
		t.Fatalf("envelope error.code: got %d, want %d (body=%s)",
			body.Error.Code, wantCode, w.Body.String())
	}
	if body.Error.Message == "" {
		t.Fatalf("envelope error.message must be non-empty, got body=%s", w.Body.String())
	}
}

// F1: choosing a foreign tenant is a SuperAdmin scope selection; the
// legacy CanAccessAllTenants attribute no longer qualifies, even with the
// cluster-wide cross-tenant flag on.
func TestIsTenantAccessible_LegacyFlagWithoutMembershipRejects(t *testing.T) {
	user := &types.User{ID: "legacy", TenantID: 1, CanAccessAllTenants: true}
	if IsTenantAccessible(context.Background(), user, 99, newFakeMemberService(), cfgCrossTenant(true)) {
		t.Fatalf("legacy flag without membership must not reach a foreign tenant")
	}
}

func TestIsTenantAccessible_SystemAdminWithoutMembershipAllows(t *testing.T) {
	user := &types.User{ID: "root", IsSystemAdmin: true}
	if !IsTenantAccessible(context.Background(), user, 99, newFakeMemberService(), cfgCrossTenant(false)) {
		t.Fatalf("SuperAdmin may select any tenant as scope")
	}
}
