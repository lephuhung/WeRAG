package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func publicDownloadKB(id string, dataTenant uint64) *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: id, TenantID: dataTenant,
		OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}
}

// runPublicBatchDownload exercises BatchDownloadKnowledge as a cross-tenant
// authenticated human against a platform-owned public KB.
func runPublicBatchDownload(
	t *testing.T,
	kb *types.KnowledgeBase,
	items []*types.Knowledge,
	names map[string]string,
	dataTenant uint64,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: 7, UserID: "user-1", Role: types.TenantRoleMember,
		})
		ctx = types.WithExecutionTenant(ctx, 7)
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Set(types.UserIDContextKey.String(), "user-1")
		c.Next()
	})
	svc := &downloadKnowledgeStub{items: items, names: names, expectedTenant: dataTenant}
	h := &KnowledgeHandler{
		kgService: svc,
		kbService: &downloadKBStub{kb: kb},
	}
	body, err := json.Marshal(BatchDownloadKnowledgeRequest{IDs: []string{"a"}})
	require.NoError(t, err)
	router.POST("/knowledge-bases/:id/knowledge/batch-download", h.BatchDownloadKnowledge)
	req := httptest.NewRequest(
		http.MethodPost, "/knowledge-bases/pub/knowledge/batch-download", bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestBatchDownloadPublicKBThroughDataScope(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	kb := publicDownloadKB("pub", 8)
	w := runPublicBatchDownload(t, kb,
		[]*types.Knowledge{{ID: "a", TenantID: 8, KnowledgeBaseID: "pub", FilePath: "stored-a"}},
		map[string]string{"a": "public-law.txt"}, 8)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "application/zip", w.Header().Get("Content-Type"))
}

func TestBatchDownloadPrivateCrossTenantDenied(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	kb := &types.KnowledgeBase{ID: "priv", TenantID: 8, OwnerTenantID: 8, Visibility: types.KBVisibilityTenant}
	w := runPublicBatchDownload(t, kb,
		[]*types.Knowledge{{ID: "a", TenantID: 8, KnowledgeBaseID: "priv", FilePath: "stored-a"}},
		map[string]string{"a": "private.txt"}, 8)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestSingleDownloadPublicKBThroughDataScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kb := publicDownloadKB("pub", 8)
	knowledge := &types.Knowledge{ID: "doc-1", TenantID: 8, KnowledgeBaseID: "pub", FilePath: "stored"}
	kgSvc := &singleDownloadKnowledgeStub{knowledge: knowledge}
	kgSvc.expectedTenant = 8
	h := &KnowledgeHandler{kgService: kgSvc, kbService: &downloadKBStub{kb: kb}}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: 7, UserID: "user-1", Role: types.TenantRoleMember,
		})
		ctx = types.WithExecutionTenant(ctx, 7)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.GET("/knowledge/:id/download", h.DownloadKnowledgeFile)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/knowledge/doc-1/download", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
}

func TestSingleDownloadAnonymousDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kb := publicDownloadKB("pub", 8)
	knowledge := &types.Knowledge{ID: "doc-1", TenantID: 8, KnowledgeBaseID: "pub", FilePath: "stored"}
	kgSvc := &singleDownloadKnowledgeStub{knowledge: knowledge}
	kgSvc.expectedTenant = 8
	h := &KnowledgeHandler{kgService: kgSvc, kbService: &downloadKBStub{kb: kb}}
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.GET("/knowledge/:id/download", h.DownloadKnowledgeFile)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/knowledge/doc-1/download", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Empty(t, kgSvc.opened, "anonymous callers must not reach storage")
}

type singleDownloadKnowledgeStub struct {
	downloadKnowledgeStub
	knowledge *types.Knowledge
}

func (s *singleDownloadKnowledgeStub) GetKnowledgeByIDOnly(_ context.Context, id string) (*types.Knowledge, error) {
	if s.knowledge != nil && s.knowledge.ID == id {
		return s.knowledge, nil
	}
	return nil, apprepo.ErrKnowledgeNotFound
}

func (s *singleDownloadKnowledgeStub) GetKnowledgeFile(ctx context.Context, id string) (io.ReadCloser, string, error) {
	if types.MustTenantIDFromContext(ctx) != s.expectedTenant {
		return nil, "", fmt.Errorf("wrong tenant context")
	}
	s.opened = append(s.opened, id)
	return io.NopCloser(strings.NewReader("原文-" + id)), "public-law.txt", nil
}
