package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// stubKBLookup is a tiny KBLookup stand-in for tests; satisfies the
// KBLookup interface (a single method) without dragging in the full
// KnowledgeBaseService surface.
type stubKBLookup struct {
	kbs    map[string]*types.KnowledgeBase
	getErr error
}

func (s *stubKBLookup) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if kb, ok := s.kbs[id]; ok {
		return kb, nil
	}
	return nil, apprepo.ErrKnowledgeBaseNotFound
}

// stubKBGrantForGuard implements just the methods the guard touches —
// ApprovedKBPermission plus the scope read. The embedded interface means
// any unintended new dependency nil-panics and surfaces immediately.
type stubKBGrantForGuard struct {
	interfaces.KBAccessGrantService
	permission map[string]types.KBPermission
	granted    map[string]bool
}

func (s *stubKBGrantForGuard) ApprovedKBPermission(
	_ context.Context, kbID string, _ uint64,
) (types.KBPermission, bool, error) {
	if s.granted[kbID] {
		return s.permission[kbID], true, nil
	}
	return "", false, nil
}

func (s *stubKBGrantForGuard) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return nil, nil
}

// runGuard fires a single request through the guard and returns the
// gin recorder + the kb access (if any) the guard stashed. Defaults
// to EnableRBAC=true; the guard ignores the flag, which the EnableRBAC=false
// tests further below pin down.
func runGuard(
	t *testing.T,
	tenantID uint64,
	kbID string,
	requiredPerm types.KBPermission,
	kb *types.KnowledgeBase,
	grants *stubKBGrantForGuard,
) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: kbID}}

	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), types.TenantIDContextKey, tenantID)
	c.Request = req.WithContext(ctx)

	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{}}
	if kb != nil {
		kbsvc.kbs[kbID] = kb
	}

	// Convert the concrete stub to a typed interface nil when it isn't
	// supplied — otherwise the interface wraps a nil pointer and
	// `iface != nil` evaluates true on the guard side (typed-nil trap).
	var grantSvc interfaces.KBAccessGrantService
	if grants != nil {
		grantSvc = grants
	}

	guard := RequireKBAccess(
		KBIDFromParam("id"),
		requiredPerm,
		kbsvc,
		grantSvc,
		cfgRBAC(true),
	)
	guard(c)
	return rec, c
}

func TestRequireKBAccess_OwnKB(t *testing.T) {
	rec, c := runGuard(t, 100, "kb-1",
		types.KBPermissionViewer,
		&types.KnowledgeBase{
			ID: "kb-1", TenantID: 100,
			OwnerTenantID: 100, Visibility: types.KBVisibilityTenant,
		},
		nil,
	)
	require.False(t, c.IsAborted(), "should pass through")
	require.Equal(t, 200, rec.Code) // gin's default; nothing wrote a status
	access, ok := KBAccessFromContext(c)
	require.True(t, ok)
	require.Equal(t, uint64(100), access.EffectiveTenantID)
	require.Equal(t, types.KBPermissionAdmin, access.Permission, "own KB grants admin")
	// The request context's tenant should still be the caller's own.
	got, ok := types.TenantIDFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, uint64(100), got)
}

// TestIsResourceNotFound_RecognisesKnowledgeSentinel pins that a missing
// *document* (knowledge) is treated as not-found, not a transient error.
// Regression: ErrKnowledgeNotFound was absent from the predicate, so
// GET/DELETE /knowledge/:id and chunk list resolved a missing doc into a
// raw 500 instead of a 404 — which the CLI then surfaced as a retryable
// server.error (exit 7), looping agents on a permanently-absent doc.
func TestIsResourceNotFound_RecognisesKnowledgeSentinel(t *testing.T) {
	require.True(t, isResourceNotFound(apprepo.ErrKnowledgeNotFound),
		"missing document (ErrKnowledgeNotFound) must classify as not-found")
	require.True(t, isResourceNotFound(apprepo.ErrKnowledgeBaseNotFound),
		"missing KB must still classify as not-found")
	require.True(t, isResourceNotFound(apprepo.ErrChunkNotFound),
		"missing chunk (ErrChunkNotFound) must classify as not-found — chunk view/by-id resolved a missing chunk into a raw 500 (exit 7) otherwise")
	require.True(t, isResourceNotFound(ErrResourceNotFound),
		"generic resource-not-found sentinel must still classify as not-found")
	require.False(t, isResourceNotFound(errors.New("connection refused")),
		"a genuine transient error must NOT be classified as not-found")
}

func TestRequireKBAccess_NotFound_Aborts(t *testing.T) {
	_, c := runGuard(t, 100, "kb-missing", types.KBPermissionViewer, nil, nil)
	require.True(t, c.IsAborted(), "missing KB must abort")
	require.NotEmpty(t, c.Errors)
	_, ok := KBAccessFromContext(c)
	require.False(t, ok, "no access should be stashed on failure")
}

func TestRequireKBAccess_LegacyTenantGrantDenied(t *testing.T) {
	grants := &stubKBGrantForGuard{
		permission: map[string]types.KBPermission{"kb-granted": types.KBPermissionViewer},
		granted:    map[string]bool{"kb-granted": true},
	}
	_, c := runGuard(t, 100, "kb-granted",
		types.KBPermissionViewer,
		&types.KnowledgeBase{ID: "kb-granted", TenantID: 200},
		grants,
	)
	require.True(t, c.IsAborted(), "legacy tenant-wide grants must not authorize access")
	_, ok := KBAccessFromContext(c)
	require.False(t, ok)
}

func TestRequireKBAccess_GrantedKB_PermissionBelowMin_Aborts(t *testing.T) {
	grants := &stubKBGrantForGuard{
		permission: map[string]types.KBPermission{"kb-granted": types.KBPermissionViewer},
		granted:    map[string]bool{"kb-granted": true},
	}
	_, c := runGuard(t, 100, "kb-granted",
		types.KBPermissionEditor, // require Editor
		&types.KnowledgeBase{ID: "kb-granted", TenantID: 200},
		grants,
	)
	require.True(t, c.IsAborted(), "Viewer grant must reject when Editor required")
}

func TestRequireKBAccess_LegacyPublicKBForeignTenantDenied(t *testing.T) {
	for _, required := range []types.KBPermission{types.KBPermissionViewer, types.KBPermissionEditor} {
		_, c := runGuard(t, 100, "kb-pub", required,
			&types.KnowledgeBase{ID: "kb-pub", TenantID: 200, Visibility: types.KBVisibilityPublic}, nil)
		require.True(t, c.IsAborted(), "legacy public visibility must never grant cross-tenant access")
	}
}

func TestRequireKBAccess_NoGrant_ForeignTenant_Aborts(t *testing.T) {
	_, c := runGuard(t, 100, "kb-private",
		types.KBPermissionViewer,
		&types.KnowledgeBase{ID: "kb-private", TenantID: 200},
		&stubKBGrantForGuard{
			permission: map[string]types.KBPermission{},
			granted:    map[string]bool{},
		},
	)
	require.True(t, c.IsAborted(), "no approved grant -> deny")
}

func TestRequireKBAccess_NoTenant_Aborts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "kb-x"}}
	c.Request = httptest.NewRequest("GET", "/", nil) // no tenant in context
	guard := RequireKBAccess(
		KBIDFromParam("id"),
		types.KBPermissionViewer,
		&stubKBLookup{},
		nil,
		cfgRBAC(true),
	)
	guard(c)
	require.True(t, c.IsAborted())
}

// ---------- EnableRBAC=false rollout window ----------

func TestRequireKBAccess_Forbidden_EnforcedEvenWhenRBACDisabled(t *testing.T) {
	// The EnableRBAC=false rollout window relaxes roles inside a workspace;
	// it must not open another workspace's KB, since handlers behind this
	// guard load by ID.
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "kb-granted"}}
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req.WithContext(context.WithValue(req.Context(), types.TenantIDContextKey, uint64(100)))

	grants := &stubKBGrantForGuard{
		permission: map[string]types.KBPermission{"kb-granted": types.KBPermissionViewer},
		granted:    map[string]bool{"kb-granted": true},
	}
	kbsvc := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{
		"kb-granted": {ID: "kb-granted", TenantID: 200},
	}}

	guard := RequireKBAccess(
		KBIDFromParam("id"),
		types.KBPermissionEditor, // would-deny
		kbsvc, grants,
		cfgRBAC(false), // enforcement off
	)
	guard(c)
	require.True(t, c.IsAborted(), "cross-workspace access stays denied when EnableRBAC is off")
	_ = rec
}

func TestRequireKBAccess_NotFound_FiresEvenWhenRBACDisabled(t *testing.T) {
	// Not-found is not an authorisation event; the client asked for a
	// resource that genuinely isn't there. We surface 404 regardless of
	// the rollout flag (matches the comment in RequireKBAccess).
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "kb-missing"}}
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req.WithContext(context.WithValue(req.Context(), types.TenantIDContextKey, uint64(100)))

	guard := RequireKBAccess(
		KBIDFromParam("id"),
		types.KBPermissionViewer,
		&stubKBLookup{kbs: map[string]*types.KnowledgeBase{}},
		nil,
		cfgRBAC(false),
	)
	guard(c)
	require.True(t, c.IsAborted(), "404 still fires with enforcement off")
	_ = rec
}
