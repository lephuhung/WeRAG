package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// callbackOnlyWorkspaces implements only what the callback route uses.
type callbackOnlyWorkspaces struct {
	gotTicket string
	gotAuth   string
	gotBody   *types.OnlyOfficeCallback
}

func (f *callbackOnlyWorkspaces) Enabled() bool { return true }
func (f *callbackOnlyWorkspaces) CreateFromAttachment(context.Context, uint64, string, string, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) GetBySession(context.Context, uint64, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) View(context.Context, *types.DocumentWorkspace, string, string, string) (*types.DocumentWorkspaceView, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) OpenCurrent(context.Context, uint64, string) (io.ReadCloser, *types.DocumentWorkspace, error) {
	return nil, nil, nil
}
func (f *callbackOnlyWorkspaces) ForceSave(context.Context, uint64, string) error { return nil }
func (f *callbackOnlyWorkspaces) PrepareExternalWrite(context.Context, uint64, string, time.Duration) (*types.DocumentWorkspace, []byte, error) {
	return nil, nil, nil
}
func (f *callbackOnlyWorkspaces) CommitExternalWrite(context.Context, uint64, string, int, []byte) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) HandleCallback(_ context.Context, ticket, auth string, body *types.OnlyOfficeCallback) error {
	f.gotTicket, f.gotAuth, f.gotBody = ticket, auth, body
	if ticket == "bad" {
		return service.ErrOnlyOfficeCallbackUnauthorized
	}
	return nil
}

func TestOnlyOfficeCallbackRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &callbackOnlyWorkspaces{}
	h := NewDocumentWorkspaceHandler(nil, fake, nil)
	r := gin.New()
	r.POST("/onlyoffice/callback/:ticket", h.OnlyOfficeCallback)

	post := func(ticket, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/onlyoffice/callback/"+ticket, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ds-jwt")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	decode := func(rec *httptest.ResponseRecorder) map[string]int {
		var out map[string]int
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}

	rec := post("good", `{"key":"ws-1","status":6,"url":"http://ds/file","forcesavetype":0}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, map[string]int{"error": 0}, decode(rec))
	require.Equal(t, "good", fake.gotTicket)
	require.Equal(t, "Bearer ds-jwt", fake.gotAuth)
	require.Equal(t, 6, fake.gotBody.Status)
	require.NotNil(t, fake.gotBody.ForceSaveType)

	rec = post("bad", `{"key":"ws-1","status":2}`)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, map[string]int{"error": 1}, decode(rec))

	rec = post("good", `not json`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestEditorLang(t *testing.T) {
	require.Equal(t, "vi", editorLang(""))
	require.Equal(t, "vi", editorLang("vi-VN,vi;q=0.9,en;q=0.8"))
	require.Equal(t, "en", editorLang("en-US,en;q=0.9"))
	require.Equal(t, "vi", editorLang("zh-CN"))
}

func TestBuildQARequestCarriesDocumentSelection(t *testing.T) {
	sel := (&types.DocumentSelection{Text: "  đoạn cần sửa  "}).Normalized()
	rc := &qaRequestContext{assistantMessage: &types.Message{ID: "a1"}, documentSelection: sel}
	req := rc.buildQARequest()
	require.NotNil(t, req.DocumentSelection)
	require.Equal(t, "đoạn cần sửa", req.DocumentSelection.Text)
}

// ---- authenticated endpoints ---------------------------------------------

type ownerOnlySessions struct {
	interfaces.SessionService
	owned map[string]bool // session id -> caller owns it
}

func (s *ownerOnlySessions) GetSession(_ context.Context, id string) (*types.Session, error) {
	if _, ok := s.owned[id]; !ok {
		return nil, errors.New("not found")
	}
	return &types.Session{ID: id}, nil
}

func (s *ownerOnlySessions) GetOwnedSession(_ context.Context, id string) (*types.Session, error) {
	if !s.owned[id] {
		return nil, errors.New("not owner")
	}
	return &types.Session{ID: id}, nil
}

type routeWorkspaces struct {
	callbackOnlyWorkspaces
	enabled   bool
	bySess    map[string]*types.DocumentWorkspace
	gotOrigin string
}

func (f *routeWorkspaces) Enabled() bool { return f.enabled }

func (f *routeWorkspaces) CreateFromAttachment(_ context.Context, tenantID uint64, sessionID, userID, _ string) (*types.DocumentWorkspace, error) {
	if f.bySess[sessionID] != nil {
		return nil, apperrors.NewConflictError("session already has a document workspace")
	}
	ws := &types.DocumentWorkspace{ID: "ws-" + sessionID, TenantID: tenantID, SessionID: sessionID, UserID: userID, FileName: "a.docx"}
	f.bySess[sessionID] = ws
	return ws, nil
}

func (f *routeWorkspaces) GetBySession(_ context.Context, _ uint64, sessionID string) (*types.DocumentWorkspace, error) {
	if ws := f.bySess[sessionID]; ws != nil {
		return ws, nil
	}
	return nil, apperrors.NewNotFoundError("Document workspace not found")
}

func (f *routeWorkspaces) View(ctx context.Context, ws *types.DocumentWorkspace, _, _, _ string) (*types.DocumentWorkspaceView, error) {
	f.gotOrigin = service.DocumentEditorOrigin(ctx)
	return &types.DocumentWorkspaceView{DocumentWorkspace: ws, EditorKey: ws.EditorKey()}, nil
}

func newDocumentRoutes(t *testing.T, ws *routeWorkspaces, frontendBase string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	sessions := &ownerOnlySessions{owned: map[string]bool{"mine": true, "admin-view": false}}
	h := NewDocumentWorkspaceHandler(sessions, ws, &config.Config{FrontendBaseURL: frontendBase})
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)) })
	r.POST("/sessions/:session_id/document", h.CreateDocumentWorkspace)
	r.GET("/sessions/:id/document", h.GetDocumentWorkspace)
	return r
}

func doJSON(r *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDocumentWorkspaceRoutes(t *testing.T) {
	disabled := &routeWorkspaces{bySess: map[string]*types.DocumentWorkspace{}}
	r := newDocumentRoutes(t, disabled, "")
	rec := doJSON(r, http.MethodPost, "/sessions/mine/document", `{"attachment_id":"a1"}`, nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	ws := &routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{}}
	r = newDocumentRoutes(t, ws, "")

	rec = doJSON(r, http.MethodGet, "/sessions/mine/document", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, "no workspace yet")

	// A session the caller can read but does not own cannot get a document.
	rec = doJSON(r, http.MethodPost, "/sessions/admin-view/document", `{"attachment_id":"a1"}`, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = doJSON(r, http.MethodPost, "/sessions/someone-else/document", `{"attachment_id":"a1"}`, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = doJSON(r, http.MethodGet, "/sessions/someone-else/document", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec = doJSON(r, http.MethodPost, "/sessions/mine/document", `{}`, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = doJSON(r, http.MethodPost, "/sessions/mine/document", `{"attachment_id":"a1"}`,
		map[string]string{"Origin": "https://chat.example"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		Success bool `json:"success"`
		Data    struct {
			ID        string `json:"id"`
			EditorKey string `json:"editor_key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.True(t, created.Success)
	require.Equal(t, "ws-mine", created.Data.ID)
	require.Equal(t, "ws-mine-0", created.Data.EditorKey)
	require.Equal(t, "https://chat.example", ws.gotOrigin)

	rec = doJSON(r, http.MethodPost, "/sessions/mine/document", `{"attachment_id":"a1"}`, nil)
	require.Equal(t, http.StatusConflict, rec.Code)

	rec = doJSON(r, http.MethodGet, "/sessions/mine/document", "", nil)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestDocumentWorkspaceHostOriginFallback(t *testing.T) {
	ws := &routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{
		"mine": {ID: "ws-mine", SessionID: "mine"},
	}}

	// Origin header wins.
	r := newDocumentRoutes(t, ws, "https://frontend.example/app")
	doJSON(r, http.MethodGet, "/sessions/mine/document", "", map[string]string{
		"Origin": "https://origin.example", "Referer": "https://referer.example/chat/1",
	})
	require.Equal(t, "https://origin.example", ws.gotOrigin)

	// Same-origin GET: the Referer names the embedding page (reduced to its
	// origin) and ranks above the configured base URL, which may differ by
	// host (localhost vs IP) and would make postMessage drop the message.
	doJSON(r, http.MethodGet, "/sessions/mine/document", "", map[string]string{
		"Referer": "http://localhost:3000/chat/1?x=1",
	})
	require.Equal(t, "http://localhost:3000", ws.gotOrigin)

	// No Referer: FRONTEND_BASE_URL.
	doJSON(r, http.MethodGet, "/sessions/mine/document", "", nil)
	require.Equal(t, "https://frontend.example", ws.gotOrigin)

	r = newDocumentRoutes(t, ws, "")

	// Nothing known.
	doJSON(r, http.MethodGet, "/sessions/mine/document", "", nil)
	require.Equal(t, "", ws.gotOrigin)
}

// The highlighted passage is persisted on the user message so chat history
// shows which passage the question referred to.
func TestPersistTurnMessagesStoresDocumentSelection(t *testing.T) {
	h, _, msgs, _ := newAbbrevGateHandler()
	reqCtx := newAbbrevReqCtx("Sửa đoạn này cho đúng thể thức")
	reqCtx.documentSelection = (&types.DocumentSelection{
		Text: "  Kính gửi: Phòng PV01  ", ParagraphHint: "Kính gửi: Phòng PV01 Công an tỉnh",
	}).Normalized()

	require.NoError(t, h.persistTurnMessages(context.Background(), reqCtx))

	var user *types.Message
	for _, m := range msgs.created {
		if m.Role == "user" {
			user = m
		}
	}
	require.NotNil(t, user)
	require.NotNil(t, user.DocumentSelection)
	require.Equal(t, "Kính gửi: Phòng PV01", user.DocumentSelection.Text)
	require.Equal(t, "Kính gửi: Phòng PV01 Công an tỉnh", user.DocumentSelection.ParagraphHint)

	raw, err := json.Marshal(user)
	require.NoError(t, err)
	require.Contains(t, string(raw),
		`"document_selection":{"text":"Kính gửi: Phòng PV01","paragraph_hint":"Kính gửi: Phòng PV01 Công an tỉnh"}`)
}

func TestPersistTurnMessagesWithoutSelectionLeavesItNil(t *testing.T) {
	h, _, msgs, _ := newAbbrevGateHandler()
	reqCtx := newAbbrevReqCtx("Câu hỏi thường")
	require.NoError(t, h.persistTurnMessages(context.Background(), reqCtx))
	for _, m := range msgs.created {
		require.Nil(t, m.DocumentSelection)
	}
	raw, err := json.Marshal(msgs.created[0])
	require.NoError(t, err)
	require.NotContains(t, string(raw), "document_selection")
}
