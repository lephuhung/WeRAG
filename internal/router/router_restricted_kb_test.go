package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// restrictedInviteStub is the invite ACL source for the restricted-KB
// route tests: users listed in live hold a live invite on the KB.
type restrictedInviteStub struct {
	interfaces.KBInvitationService
	live map[string]bool // kbID + "/" + userID
}

func (s *restrictedInviteStub) HasAcceptedInvite(_ context.Context, kbID, userID string) (bool, error) {
	return s.live[kbID+"/"+userID], nil
}

func (s *restrictedInviteStub) HasLiveInvite(ctx context.Context, kbID, userID string) (bool, error) {
	return s.HasAcceptedInvite(ctx, kbID, userID)
}

type restrictedChunkLookup struct{ chunk *types.Chunk }

func (s *restrictedChunkLookup) GetChunkByIDOnly(_ context.Context, id string) (*types.Chunk, error) {
	if s.chunk != nil && s.chunk.ID == id {
		return s.chunk, nil
	}
	return nil, context.Canceled
}

const restrictedOwner = uint64(1)

func restrictedRouteKB() *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: "kb-r", Name: "restricted", Type: types.KnowledgeBaseTypeDocument,
		TenantID: restrictedOwner, OwnerTenantID: restrictedOwner,
		Visibility: types.KBVisibilityRestricted, MemberDefault: types.KBMemberDefaultContribute,
	}
}

// newRestrictedRouteEngine wires the real route registrations (or a
// custom register func) behind a caller of the owner tenant.
func newRestrictedRouteEngine(
	t *testing.T, userID string, role types.TenantRole, invites map[string]bool,
	register func(r *gin.RouterGroup, g *rbacGuards),
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enabled := true
	kb := restrictedRouteKB()
	guards := &rbacGuards{
		cfg:       &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
		kbService: &stubWikiKBLookup{kbs: map[string]*types.KnowledgeBase{kb.ID: kb}},
		knowledgeService: &downloadKnowledgeLookup{knowledge: &types.Knowledge{
			ID: "doc-r", KnowledgeBaseID: kb.ID, TenantID: restrictedOwner,
		}},
		chunkService: &restrictedChunkLookup{chunk: &types.Chunk{
			ID: "chunk-r", KnowledgeID: "doc-r", KnowledgeBaseID: kb.ID, TenantID: restrictedOwner,
		}},
		kbInviteService: &restrictedInviteStub{live: invites},
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: restrictedOwner, UserID: userID, Role: role,
		})
		ctx = context.WithValue(ctx, types.TenantIDContextKey, restrictedOwner)
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), restrictedOwner)
		c.Next()
	})
	register(r.Group("/api/v1"), guards)
	return r
}

// TestRestrictedKBClosedToOwnerTenantMemberWithoutACL drives the real
// route registrations: a Member of the owner tenant without an invite or
// grant gets 403 on every KB read path, before any handler runs.
func TestRestrictedKBClosedToOwnerTenantMemberWithoutACL(t *testing.T) {
	engine := newRestrictedRouteEngine(t, "member-1", types.TenantRoleMember, nil,
		func(r *gin.RouterGroup, g *rbacGuards) {
			RegisterKnowledgeBaseRoutes(r, &handler.KnowledgeBaseHandler{}, g)
			RegisterKnowledgeRoutes(r, &handler.KnowledgeHandler{}, g)
			RegisterChunkRoutes(r, &handler.ChunkHandler{}, g)
			RegisterWikiPageRoutes(r, &handler.WikiPageHandler{}, nil, g)
			serveKBScopedFiles(r, g, nil, nil, nil)
		})
	for _, path := range []string{
		"/api/v1/knowledge-bases/kb-r",                          // KB detail
		"/api/v1/knowledge-bases/kb-r/knowledge",                // document list
		"/api/v1/knowledge-bases/kb-r/move-targets",             // move targets
		"/api/v1/knowledge/doc-r",                               // document read
		"/api/v1/knowledge/doc-r/preview",                       // file preview
		"/api/v1/knowledge/doc-r/download",                      // original download
		"/api/v1/chunks/doc-r",                                  // chunk list
		"/api/v1/chunks/by-id/chunk-r",                          // chunk read
		"/api/v1/knowledgebase/kb-r/wiki/pages",                 // wiki list
		"/api/v1/knowledgebase/kb-r/wiki/search?q=x",            // wiki search
		"/api/v1/knowledge-bases/kb-r/files?file_path=resource", // KB-scoped files
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equalf(t, http.StatusForbidden, rec.Code,
			"%s must be closed, got %d body=%s", path, rec.Code, rec.Body.String())
	}
}

// TestRestrictedKBACLViewerPreviewsButNeverDownloads: a live invite opens
// reads (preview) but never the original download; the owner tenant's
// Admin keeps both.
func TestRestrictedKBACLViewerPreviewsButNeverDownloads(t *testing.T) {
	register := func(r *gin.RouterGroup, g *rbacGuards) {
		k := g.apiKeyGroup(r.Group("/knowledge"), apiKeyRetrieve(apiKeyFullAccess()))
		k.GET("/:id/preview", g.Member(), g.KBAccessReadFromKnowledgeIDParam("id"), okStub)
		k.GET("/:id/download", g.Member(), g.KBAccessDownloadFromKnowledgeIDParam("id"), okStub)
		w := g.apiKeyGroup(r.Group("/knowledgebase/:kb_id/wiki"), apiKeyRetrieve(apiKeyFullAccess()))
		w.GET("/pages", g.Member(), g.KBAccessRead("kb_id"), okStub)
		c := g.apiKeyGroup(r.Group("/chunks"), apiKeyRetrieve(apiKeyFullAccess()))
		c.GET("/by-id/:id", g.Member(), g.KBAccessReadFromChunkIDParam("id"), okStub)
	}
	for _, tt := range []struct {
		name         string
		user         string
		role         types.TenantRole
		invites      map[string]bool
		readCode     int
		downloadCode int
	}{
		{"invited member", "member-1", types.TenantRoleMember, map[string]bool{"kb-r/member-1": true},
			http.StatusNoContent, http.StatusForbidden},
		{"owner tenant admin", "admin-1", types.TenantRoleAdmin, nil,
			http.StatusNoContent, http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			engine := newRestrictedRouteEngine(t, tt.user, tt.role, tt.invites, register)
			for _, path := range []string{
				"/api/v1/knowledge/doc-r/preview",
				"/api/v1/knowledgebase/kb-r/wiki/pages",
				"/api/v1/chunks/by-id/chunk-r",
			} {
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equalf(t, tt.readCode, rec.Code, "%s body=%s", path, rec.Body.String())
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/doc-r/download", nil))
			require.Equal(t, tt.downloadCode, rec.Code, "body=%s", rec.Body.String())
		})
	}
}

// restrictedBindingLookup reports the resource as an image of kb-r.
type restrictedBindingLookup struct{}

func (restrictedBindingLookup) GetMessageFileBindings(context.Context, uint64, string, string) (*types.MessageFileBindings, error) {
	return &types.MessageFileBindings{KnowledgeBaseIDs: []string{"kb-r"}}, nil
}

type restrictedKBLoader struct{}

func (restrictedKBLoader) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{restrictedRouteKB()}, nil
}

// TestTenantFileProxyClosesRestrictedKBContent: the raw /files proxy no
// longer serves a restricted KB's registered resources to an owner-tenant
// Member just because the storage path is in their tenant.
func TestTenantFileProxyClosesRestrictedKBContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("STORAGE_TYPE", "local")
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	gate := access.KBFileGate{Bindings: restrictedBindingLookup{}, KBs: restrictedKBLoader{}}
	for _, tt := range []struct {
		role types.TenantRole
		want int
	}{
		{types.TenantRoleMember, http.StatusForbidden},
		{types.TenantRoleAdmin, http.StatusOK},
	} {
		engine := gin.New()
		serveFilesWithKBGate(engine, &stubFileService{getFile: func(context.Context, string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("image")), nil
		}}, nil, &stubResourceCatalog{resource: &types.StoredResource{
			TenantID: restrictedOwner, PhysicalPath: "local://1/exports/a.png",
		}}, gate)
		req := httptest.NewRequest(http.MethodGet, "/files?file_path="+url.QueryEscape(ref), nil)
		ctx := context.WithValue(req.Context(), types.TenantInfoContextKey, &types.Tenant{ID: restrictedOwner})
		ctx = types.WithCaller(ctx, types.Caller{TenantID: restrictedOwner, UserID: "u-1", Role: tt.role})
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req.WithContext(ctx))
		require.Equal(t, tt.want, rec.Code, "role=%s body=%s", tt.role, rec.Body.String())
	}
}
