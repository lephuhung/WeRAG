package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// Task 4: dedicated human-only public catalog endpoint contract + move
// targets preserving their tenant-scoped consumer set.

// stubCatalogKBService serves the public-catalog endpoint and the list /
// move-targets paths. Embedding the interface keeps unexercised methods
// out of the stub.
type stubCatalogKBService struct {
	interfaces.KnowledgeBaseService
	kbs          []*types.KnowledgeBase
	catalogItems []*types.KnowledgeBase
	catalogTotal int64
	gotPage      int
	gotSize      int
	gotKeyword   string
}

func (s *stubCatalogKBService) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return s.kbs, nil
}

func (s *stubCatalogKBService) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	for _, kb := range s.kbs {
		if kb.ID == id {
			return kb, nil
		}
	}
	return nil, nil
}

func (s *stubCatalogKBService) ListPublicCatalog(_ context.Context, page, pageSize int, keyword string) ([]*types.KnowledgeBase, int64, error) {
	s.gotPage, s.gotSize, s.gotKeyword = page, pageSize, keyword
	return s.catalogItems, s.catalogTotal, nil
}

func newPublicCatalogRouter(t *testing.T, svc *stubCatalogKBService, ctx context.Context) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(ctx)
		if tenantID, ok := types.TenantIDFromContext(ctx); ok {
			c.Set(types.TenantIDContextKey.String(), tenantID)
		}
		if userID, ok := types.UserIDFromContext(ctx); ok {
			c.Set(types.UserIDContextKey.String(), userID)
		}
		c.Next()
	})
	h := &KnowledgeBaseHandler{service: svc}
	r.GET("/knowledge-bases/public", h.ListPublicCatalog)
	r.GET("/knowledge-bases/:id/move-targets", h.ListMoveTargets)
	return r
}

func catalogCtx(tenant uint64, user string) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant)
	return types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: user, Role: types.TenantRoleMember})
}

func TestListPublicCatalog_EnvelopeAndPagination(t *testing.T) {
	svc := &stubCatalogKBService{
		catalogItems: []*types.KnowledgeBase{
			{ID: "pub-a", Name: "pub-a", TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
		},
		catalogTotal: 42,
	}
	r := newPublicCatalogRouter(t, svc, catalogCtx(1, "user-a"))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/knowledge-bases/public?page=2&page_size=10&q=law", nil))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Equal(t, 2, svc.gotPage)
	require.Equal(t, 10, svc.gotSize)
	require.Equal(t, "law", svc.gotKeyword)

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Items    []map[string]any `json:"items"`
			Total    int64            `json:"total"`
			Page     int              `json:"page"`
			PageSize int              `json:"page_size"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.Len(t, body.Data.Items, 1)
	require.Equal(t, int64(42), body.Data.Total)
	require.Equal(t, 2, body.Data.Page)
	require.Equal(t, 10, body.Data.PageSize)
}

func TestListPublicCatalog_DeniesNonHumans(t *testing.T) {
	svc := &stubCatalogKBService{}
	// Anonymous: no identity at all.
	r := newPublicCatalogRouter(t, svc, context.Background())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/knowledge-bases/public", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())

	// API-key principal: never an implicit catalog grant.
	keyCtx := types.WithCaller(context.Background(),
		types.Caller{TenantID: 1, UserID: "system-1", Role: types.TenantRoleAdmin})
	keyCtx = types.WithTenantAPIKeyScope(keyCtx, types.TenantAPIKeyScope{
		KeyID: 7, Name: "k",
		Capabilities: types.StringArray{string(types.APIKeyCapabilityRetrieve)},
	})
	r = newPublicCatalogRouter(t, svc, keyCtx)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/knowledge-bases/public", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

func TestListMoveTargets_ExcludesPublicCatalog(t *testing.T) {
	svc := &stubCatalogKBService{
		kbs: []*types.KnowledgeBase{
			{ID: "own-a", Name: "a", TenantID: 1, OwnerTenantID: 1, EmbeddingModelID: "m"},
			{ID: "pub-a", Name: "pub", TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic, EmbeddingModelID: "m"},
		},
	}
	r := newPublicCatalogRouter(t, svc, catalogCtx(1, "user-a"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/knowledge-bases/own-a/move-targets", nil))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.NotContains(t, rec.Body.String(), "pub-a",
		"move targets preserve their tenant-scoped consumer contract")
}
