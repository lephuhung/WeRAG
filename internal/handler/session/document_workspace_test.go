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

func (f *callbackOnlyWorkspaces) Enabled() bool          { return true }
func (f *callbackOnlyWorkspaces) DocumentsEnabled() bool { return true }
func (f *callbackOnlyWorkspaces) CreateFromAttachment(context.Context, uint64, string, string, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) CreateFromAttachmentFor(context.Context, uint64, string, string, string, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) StoreClientSave(context.Context, uint64, string, string, int, []byte) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) GetBySession(context.Context, uint64, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) CreateSourceFromAttachment(context.Context, uint64, string, string, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) SetRole(context.Context, uint64, string, string, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) SourceText(context.Context, uint64, string, string) (*types.DocumentWorkspaceText, *types.DocumentWorkspace, error) {
	return nil, nil, nil
}
func (f *callbackOnlyWorkspaces) View(context.Context, *types.DocumentWorkspace, string, string, string) (*types.DocumentWorkspaceView, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) OpenCurrent(context.Context, uint64, string, string) (io.ReadCloser, *types.DocumentWorkspace, error) {
	return nil, nil, nil
}
func (f *callbackOnlyWorkspaces) ForceSave(context.Context, uint64, string, string) error { return nil }
func (f *callbackOnlyWorkspaces) Get(context.Context, uint64, string, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) List(context.Context, uint64, string) ([]*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) Activate(context.Context, uint64, string, string) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) Remove(context.Context, uint64, string, string) error { return nil }
func (f *callbackOnlyWorkspaces) PrepareExternalWrite(context.Context, uint64, string, string, time.Duration) (*types.DocumentWorkspace, []byte, error) {
	return nil, nil, nil
}
func (f *callbackOnlyWorkspaces) CommitExternalWrite(context.Context, uint64, string, string, int, []byte) (*types.DocumentWorkspace, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) Snapshot(context.Context, uint64, string, string, string, string, time.Duration) (*types.DocumentRevision, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) ListRevisions(context.Context, uint64, string, string) ([]*types.DocumentRevision, error) {
	return nil, nil
}
func (f *callbackOnlyWorkspaces) Restore(context.Context, uint64, string, string, int) (*types.DocumentWorkspace, error) {
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
	h := NewDocumentWorkspaceHandler(nil, fake, nil, nil)
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

// DocumentsEnabled does not depend on the editor: sources and Word targets
// work without ONLYOFFICE.
func (f *routeWorkspaces) DocumentsEnabled() bool { return true }

func (f *routeWorkspaces) CreateFromAttachmentFor(ctx context.Context, tenantID uint64, sessionID, userID, attachmentID, _ string) (*types.DocumentWorkspace, error) {
	return f.CreateFromAttachment(ctx, tenantID, sessionID, userID, attachmentID)
}
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

// Get resolves "" to the session's document like the service does.
func (f *routeWorkspaces) Get(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.DocumentWorkspace, error) {
	ws, err := f.GetBySession(ctx, tenantID, sessionID)
	if err != nil || (documentID != "" && ws.ID != documentID) {
		return nil, apperrors.NewNotFoundError("Document workspace not found")
	}
	return ws, nil
}

func (f *routeWorkspaces) List(_ context.Context, _ uint64, sessionID string) ([]*types.DocumentWorkspace, error) {
	if ws := f.bySess[sessionID]; ws != nil {
		return []*types.DocumentWorkspace{ws}, nil
	}
	return nil, nil
}

func (f *routeWorkspaces) View(ctx context.Context, ws *types.DocumentWorkspace, _, _, _ string) (*types.DocumentWorkspaceView, error) {
	f.gotOrigin = service.DocumentEditorOrigin(ctx)
	return &types.DocumentWorkspaceView{DocumentWorkspace: ws, EditorKey: ws.EditorKey()}, nil
}

func newDocumentRoutes(t *testing.T, ws *routeWorkspaces, frontendBase string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	sessions := &ownerOnlySessions{owned: map[string]bool{"mine": true, "admin-view": false}}
	h := NewDocumentWorkspaceHandler(sessions, ws, &config.Config{FrontendBaseURL: frontendBase}, nil)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)) })
	r.POST("/sessions/:session_id/document", h.CreateDocumentWorkspace)
	r.GET("/sessions/:id/document", h.GetDocumentWorkspace)
	r.GET("/sessions/:id/documents", h.ListDocumentWorkspaces)
	r.GET("/sessions/:id/documents/:doc_id", h.GetDocumentWorkspace)
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

// ---- revision routes -----------------------------------------------------

type revisionWorkspaces struct {
	routeWorkspaces
	revs         []*types.DocumentRevision
	snapLabel    string
	snapSource   string
	restoredSeqs []int
}

func (f *revisionWorkspaces) ListRevisions(ctx context.Context, tenantID uint64, sessionID, _ string) ([]*types.DocumentRevision, error) {
	if _, err := f.GetBySession(ctx, tenantID, sessionID); err != nil {
		return nil, err
	}
	return f.revs, nil
}

func (f *revisionWorkspaces) Snapshot(ctx context.Context, tenantID uint64, sessionID, _, label, source string, _ time.Duration) (*types.DocumentRevision, error) {
	if _, err := f.GetBySession(ctx, tenantID, sessionID); err != nil {
		return nil, err
	}
	f.snapLabel, f.snapSource = label, source
	rev := &types.DocumentRevision{Seq: len(f.revs) + 1, Label: label, Source: source}
	f.revs = append([]*types.DocumentRevision{rev}, f.revs...)
	return rev, nil
}

func (f *revisionWorkspaces) Restore(ctx context.Context, tenantID uint64, sessionID, _ string, seq int) (*types.DocumentWorkspace, error) {
	ws, err := f.GetBySession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	if seq > len(f.revs) {
		return nil, apperrors.NewNotFoundError("Revision not found")
	}
	f.restoredSeqs = append(f.restoredSeqs, seq)
	ws.Revision++
	return ws, nil
}

func TestDocumentRevisionRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	created := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	ws := &revisionWorkspaces{
		routeWorkspaces: routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{
			"mine": {ID: "ws-mine", SessionID: "mine"},
		}},
		revs: []*types.DocumentRevision{{Seq: 1, Label: "Gốc", Source: "manual", FileSize: 42, CreatedAt: created, Ref: "resource://secret"}},
	}
	sessions := &ownerOnlySessions{owned: map[string]bool{"mine": true, "admin-view": false, "empty": true}}
	h := NewDocumentWorkspaceHandler(sessions, ws, nil, nil)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)) })
	r.GET("/sessions/:id/document/revisions", h.ListDocumentRevisions)
	r.POST("/sessions/:session_id/document/revisions/:seq/restore", h.RestoreDocumentRevision)
	r.POST("/sessions/:session_id/document/snapshot", h.SnapshotDocumentWorkspace)

	// List: fields exposed, storage ref hidden.
	rec := doJSON(r, http.MethodGet, "/sessions/mine/document/revisions", "", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var listed struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.True(t, listed.Success)
	require.Len(t, listed.Data, 1)
	require.Equal(t, float64(1), listed.Data[0]["seq"])
	require.Equal(t, "Gốc", listed.Data[0]["label"])
	require.Equal(t, "manual", listed.Data[0]["source"])
	require.Equal(t, float64(42), listed.Data[0]["file_size"])
	require.Contains(t, listed.Data[0], "created_at")
	require.NotContains(t, rec.Body.String(), "resource://")

	// Snapshot: manual source, label passed through.
	rec = doJSON(r, http.MethodPost, "/sessions/mine/document/snapshot", `{"label":"Trước khi gửi"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"success":true,"data":{"seq":2}}`, rec.Body.String())
	require.Equal(t, "Trước khi gửi", ws.snapLabel)
	require.Equal(t, types.DocumentRevisionSourceManual, ws.snapSource)
	rec = doJSON(r, http.MethodPost, "/sessions/mine/document/snapshot", "", nil)
	require.Equal(t, http.StatusOK, rec.Code, "empty body means default label")

	// Restore: returns the rotated key.
	rec = doJSON(r, http.MethodPost, "/sessions/mine/document/revisions/1/restore", "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"success":true,"data":{"revision":1,"editor_key":"ws-mine-1"}}`, rec.Body.String())
	require.Equal(t, []int{1}, ws.restoredSeqs)

	rec = doJSON(r, http.MethodPost, "/sessions/mine/document/revisions/abc/restore", "", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = doJSON(r, http.MethodPost, "/sessions/mine/document/revisions/99/restore", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Ownership: not-owned sessions cannot snapshot or restore; unknown
	// sessions cannot list.
	for _, path := range []string{
		"/sessions/admin-view/document/snapshot",
		"/sessions/admin-view/document/revisions/1/restore",
		"/sessions/someone-else/document/snapshot",
	} {
		rec = doJSON(r, http.MethodPost, path, `{}`, nil)
		require.Equal(t, http.StatusNotFound, rec.Code, path)
	}
	rec = doJSON(r, http.MethodGet, "/sessions/someone-else/document/revisions", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, []int{1}, ws.restoredSeqs, "rejected requests never reach the service")

	// Session without a workspace: 404 on all three.
	rec = doJSON(r, http.MethodGet, "/sessions/empty/document/revisions", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = doJSON(r, http.MethodPost, "/sessions/empty/document/snapshot", `{}`, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = doJSON(r, http.MethodPost, "/sessions/empty/document/revisions/1/restore", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// proposalWorkspaces records the document, label, source and wait of the
// snapshot the apply route takes.
type proposalWorkspaces struct {
	revisionWorkspaces
	snapDocID string
	snapWait  time.Duration
}

func (f *proposalWorkspaces) Snapshot(ctx context.Context, tenantID uint64, sessionID, documentID, label, source string, wait time.Duration) (*types.DocumentRevision, error) {
	f.snapDocID, f.snapWait = documentID, wait
	return f.revisionWorkspaces.Snapshot(ctx, tenantID, sessionID, documentID, label, source, wait)
}

func TestApplyRewriteProposalSnapshotsAsAI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ws := &proposalWorkspaces{revisionWorkspaces: revisionWorkspaces{
		routeWorkspaces: routeWorkspaces{enabled: true, bySess: map[string]*types.DocumentWorkspace{
			"mine": {ID: "ws-mine", SessionID: "mine"},
		}},
		revs: []*types.DocumentRevision{{Seq: 1, Label: "Gốc", Source: "manual"}},
	}}
	sessions := &ownerOnlySessions{owned: map[string]bool{"mine": true, "admin-view": false}}
	h := NewDocumentWorkspaceHandler(sessions, ws, nil, nil)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)) })
	r.POST("/sessions/:session_id/documents/:doc_id/proposals/apply", h.ApplyRewriteProposal)

	rec := doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-mine/proposals/apply", `{"batch_id":"b-1","variant_id":"v2"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"success":true,"data":{"snapshot_seq":2}}`, rec.Body.String())
	require.Equal(t, "ai: viết lại (phương án 2)", ws.snapLabel)
	require.Equal(t, types.DocumentRevisionSourceAI, ws.snapSource)
	require.Equal(t, "ws-mine", ws.snapDocID)
	require.Equal(t, proposalSnapshotWait, ws.snapWait, "the snapshot waits for the editor to flush")

	for name, body := range map[string]string{
		"no body":     "",
		"no batch":    `{"variant_id":"v1"}`,
		"bad variant": `{"batch_id":"b-1","variant_id":"x"}`,
		"no variant":  `{"batch_id":"b-1"}`,
	} {
		rec = doJSON(r, http.MethodPost, "/sessions/mine/documents/ws-mine/proposals/apply", body, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, name)
	}
	for _, sess := range []string{"admin-view", "someone-else"} {
		rec = doJSON(r, http.MethodPost, "/sessions/"+sess+"/documents/ws-mine/proposals/apply", `{"batch_id":"b","variant_id":"v1"}`, nil)
		require.Equal(t, http.StatusNotFound, rec.Code, sess)
	}
	require.Len(t, ws.revs, 2, "rejected requests take no snapshot")
}
