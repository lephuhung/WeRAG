package router

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type downloadKnowledgeLookup struct {
	knowledge *types.Knowledge
}

func (s *downloadKnowledgeLookup) GetKnowledgeByIDOnly(_ context.Context, id string) (*types.Knowledge, error) {
	if s.knowledge != nil && s.knowledge.ID == id {
		return s.knowledge, nil
	}
	return nil, apprepo.ErrKnowledgeNotFound
}

type downloadKBGrantStub struct {
	interfaces.KBAccessGrantService
	permission types.KBPermission
	granted    bool
}

func (s *downloadKBGrantStub) ApprovedKBPermission(
	_ context.Context,
	_ string,
	_ uint64,
) (types.KBPermission, bool, error) {
	return s.permission, s.granted, nil
}

func (s *downloadKBGrantStub) GetKBScope(context.Context, string) (*types.KBScope, error) {
	return nil, nil
}

func newKnowledgeDownloadRouteTestEngine(
	t *testing.T,
	role types.TenantRole,
	knowledge *types.Knowledge,
	kb *types.KnowledgeBase,
	grants interfaces.KBAccessGrantService,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	enabled := true
	guards := &rbacGuards{
		cfg:              &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
		knowledgeService: &downloadKnowledgeLookup{knowledge: knowledge},
		kbService:        &stubWikiKBLookup{kbs: map[string]*types.KnowledgeBase{kb.ID: kb}},
		kbGrantService:   grants,
	}

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Next()
	})
	RegisterKnowledgeRoutes(r.Group("/api/v1"), &handler.KnowledgeHandler{}, guards)
	return r
}

func TestKnowledgeDownloadRejectsForeignKBWithoutGrant(t *testing.T) {
	engine := newKnowledgeDownloadRouteTestEngine(
		t,
		types.TenantRoleMember,
		&types.Knowledge{ID: "knowledge-foreign", KnowledgeBaseID: "kb-foreign", TenantID: 2},
		&types.KnowledgeBase{ID: "kb-foreign", TenantID: 2},
		nil,
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/knowledge-foreign/download", nil)
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

func TestKnowledgeDownloadRejectsReadOnlyGrantedKB(t *testing.T) {
	engine := newKnowledgeDownloadRouteTestEngine(
		t,
		types.TenantRoleMember,
		&types.Knowledge{ID: "knowledge-granted", KnowledgeBaseID: "kb-granted", TenantID: 2},
		&types.KnowledgeBase{ID: "kb-granted", TenantID: 2},
		&downloadKBGrantStub{permission: types.KBPermissionViewer, granted: true},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/knowledge-granted/download", nil)
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

func TestBatchKnowledgeDownloadRejectsForeignKBWithoutGrant(t *testing.T) {
	engine := newKnowledgeDownloadRouteTestEngine(
		t,
		types.TenantRoleMember,
		&types.Knowledge{ID: "knowledge-foreign", KnowledgeBaseID: "kb-foreign", TenantID: 2},
		&types.KnowledgeBase{ID: "kb-foreign", TenantID: 2},
		nil,
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/knowledge-bases/kb-foreign/knowledge/batch-download",
		bytes.NewBufferString(`{"ids":["knowledge-foreign"]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

func TestBatchKnowledgeDownloadRejectsReadOnlyGrantedKB(t *testing.T) {
	engine := newKnowledgeDownloadRouteTestEngine(
		t,
		types.TenantRoleMember,
		&types.Knowledge{ID: "knowledge-granted", KnowledgeBaseID: "kb-granted", TenantID: 2},
		&types.KnowledgeBase{ID: "kb-granted", TenantID: 2},
		&downloadKBGrantStub{permission: types.KBPermissionViewer, granted: true},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/knowledge-bases/kb-granted/knowledge/batch-download",
		bytes.NewBufferString(`{"ids":["knowledge-granted"]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}
