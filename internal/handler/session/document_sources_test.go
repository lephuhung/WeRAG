package session

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUploadBecomesSource(t *testing.T) {
	assistant := &types.CustomAgent{ID: types.BuiltinDocumentAssistantID}
	h := &Handler{documentWorkspaces: &routeWorkspaces{enabled: true}}
	cases := []struct {
		name  string
		agent *types.CustomAgent
		ext   string
		role  string
		want  bool
	}{
		{"pdf in the document assistant", assistant, "pdf", "", true},
		{"docx at chat", assistant, "docx", "source", true},
		{"spreadsheet", assistant, "xlsx", "", true},
		{"the editor pane's own upload", assistant, "docx", "target", false},
		{"image for vision", assistant, "png", "", false},
		{"audio", assistant, "mp3", "", false},
		{"another agent", &types.CustomAgent{ID: "builtin-smart-reasoning"}, "pdf", "", false},
		{"no agent", nil, "pdf", "", false},
	}
	for _, c := range cases {
		if got := h.uploadBecomesSource(c.agent, c.ext, c.role); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if (&Handler{documentWorkspaces: &routeWorkspaces{}}).uploadBecomesSource(assistant, "pdf", "") {
		t.Error("editor disabled: no source")
	}
}

func TestPinDocumentAssistantForASessionHoldingOnlySources(t *testing.T) {
	ws := &routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{
		"sources": {SessionID: "sources", FileName: "a.pdf", Role: types.DocumentWorkspaceRoleSource},
	}}
	h := &Handler{documentWorkspaces: ws}
	got := h.pinDocumentAssistant(context.Background(), &types.Session{ID: "sources", TenantID: 1}, "builtin-smart-reasoning")
	require.Equal(t, types.BuiltinDocumentAssistantID, got)
}

// roleWorkspaces answers SetRole like the service: a pdf cannot become a
// target, a full session refuses with 409.
type roleWorkspaces struct {
	routeWorkspaces
	gotRole string
}

func (f *roleWorkspaces) SetRole(_ context.Context, tenantID uint64, sessionID, documentID, role string) (*types.DocumentWorkspace, error) {
	f.gotRole = role
	switch documentID {
	case "pdf":
		return nil, apperrors.NewBadRequestError("Chỉ văn bản Word (.docx, .doc) mở được trong trình soạn thảo")
	case "full":
		return nil, apperrors.NewConflictError("Cuộc hội thoại đã mở tối đa 4 văn bản")
	}
	return &types.DocumentWorkspace{ID: documentID, TenantID: tenantID, SessionID: sessionID, Position: 2,
		FileName: "a.docx", Role: role}, nil
}

func TestSetDocumentRoleRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ws := &roleWorkspaces{routeWorkspaces: routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{}}}
	sessions := &ownerOnlySessions{owned: map[string]bool{"mine": true, "admin-view": false}}
	h := NewDocumentWorkspaceHandler(sessions, ws, &config.Config{}, nil)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)) })
	r.POST("/sessions/:session_id/documents/:doc_id/role", h.SetDocumentRole)

	rec := doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-a/role", `{"role":"source"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Data struct {
			ID     string `json:"id"`
			Role   string `json:"role"`
			Handle string `json:"handle"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ws-a", body.Data.ID)
	require.Equal(t, types.DocumentWorkspaceRoleSource, body.Data.Role)
	require.Equal(t, "vb2", body.Data.Handle)

	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/full/role", `{"role":"target"}`, nil)
	require.Equal(t, http.StatusConflict, rec.Code)
	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/pdf/role", `{"role":"target"}`, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-a/role", `{}`, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = doJSON(r, http.MethodPost, "/sessions/admin-view/documents/ws-a/role", `{"role":"source"}`, nil)
	require.Equal(t, http.StatusNotFound, rec.Code, "only the owner switches roles")
}

// GET /documents and /documents/:doc_id carry each document's profile; a
// document without one gets it started (here it fails: no assistant
// model), and the hash never leaves the server.
func TestDocumentRoutesIncludeTheProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	src := &types.DocumentWorkspace{ID: "ws-prof-src", TenantID: 7, SessionID: "mine", Position: 1, FileName: "bao-cao.pdf",
		FileType: "pdf", Role: types.DocumentWorkspaceRoleSource, TextStatus: types.DocumentSourceTextReady}
	ws := &routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{"mine": src}}
	sessions := &ownerOnlySessions{owned: map[string]bool{"mine": true}}
	h := NewDocumentWorkspaceHandler(sessions, ws, &config.Config{}, service.NewDocumentFormatPrecheck(ws, nil, nil))
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)) })
	r.GET("/sessions/:session_id/documents", h.ListDocumentWorkspaces)
	r.GET("/sessions/:session_id/documents/:doc_id", h.GetDocumentWorkspace)

	type profileView struct {
		Status   string `json:"status"`
		Error    string `json:"error"`
		TextHash string `json:"text_hash"`
	}
	var list struct {
		Data struct {
			Documents []struct {
				ID      string       `json:"id"`
				Profile *profileView `json:"profile"`
			} `json:"documents"`
		} `json:"data"`
	}
	require.Eventually(t, func() bool {
		rec := doJSON(r, http.MethodGet, "/sessions/mine/documents", "", nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
		require.Len(t, list.Data.Documents, 1)
		p := list.Data.Documents[0].Profile
		return p != nil && p.Status == types.DocumentProfileFailed
	}, 5*time.Second, 20*time.Millisecond)
	require.Equal(t, "no model", list.Data.Documents[0].Profile.Error)

	rec := doJSON(r, http.MethodGet, "/sessions/mine/documents/ws-prof-src", "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var one struct {
		Data struct {
			Profile *profileView `json:"profile"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &one))
	require.NotNil(t, one.Data.Profile)
	require.Equal(t, types.DocumentProfileFailed, one.Data.Profile.Status)
	require.Empty(t, one.Data.Profile.TextHash)
}
