package session

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// PUT and DELETE /documents/scope set and clear the session's scope, which
// GET /documents carries; only the owner may change it.
func TestDocumentScopeRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	doc := &types.DocumentWorkspace{ID: "ws-scope", TenantID: 7, SessionID: "mine", Position: 1, FileName: "a.docx"}
	ws := &routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{"mine": doc, "admin-view": doc}}
	sessions := &ownerOnlySessions{owned: map[string]bool{"mine": true, "admin-view": false}}
	h := NewDocumentWorkspaceHandler(sessions, ws, &config.Config{}, nil)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)) })
	r.GET("/sessions/:id/documents", h.ListDocumentWorkspaces)
	r.PUT("/sessions/:id/documents/scope", h.SetDocumentScope)
	r.DELETE("/sessions/:id/documents/scope", h.ClearDocumentScope)
	r.DELETE("/sessions/:id/documents/:doc_id", h.DeleteDocumentWorkspace)

	type listResp struct {
		Data struct {
			Scope *types.DocumentScope `json:"scope"`
		} `json:"data"`
	}
	list := func() *types.DocumentScope {
		rec := doJSON(r, http.MethodGet, "/sessions/mine/documents", "", nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out listResp
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out.Data.Scope
	}
	require.Nil(t, list())

	rec := doJSON(r, http.MethodPut, "/sessions/mine/documents/scope",
		`{"document_ids":["vb1"],"sections":[{"document_id":"vb1","from":3,"to":8,"title":"Điều 2"}],"task":"compare"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	scope := list()
	require.NotNil(t, scope)
	require.Equal(t, []string{"ws-scope"}, scope.DocumentIDs)
	require.Equal(t, types.DocumentScopeSetByUser, scope.SetBy)
	require.Equal(t, 3, scope.Sections[0].From)

	rec = doJSON(r, http.MethodPut, "/sessions/mine/documents/scope", `{"document_ids":["vb5"]}`, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = doJSON(r, http.MethodPut, "/sessions/mine/documents/scope", `{"document_ids":["vb1"],"task":"dance"}`, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = doJSON(r, http.MethodPut, "/sessions/admin-view/documents/scope", `{"document_ids":["vb1"]}`, nil)
	require.Equal(t, http.StatusNotFound, rec.Code, "only the owner sets the scope")
	rec = doJSON(r, http.MethodDelete, "/sessions/admin-view/documents/scope", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec = doJSON(r, http.MethodDelete, "/sessions/mine/documents/scope", "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Nil(t, list())
}
