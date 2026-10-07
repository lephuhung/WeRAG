package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/golang-jwt/jwt/v5"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

const dwTestSecret = "test-onlyoffice-secret"

// ---- fakes ---------------------------------------------------------------

type dwFakeRepo struct {
	mu   sync.Mutex
	rows map[string]types.DocumentWorkspace
}

func newDWFakeRepo() *dwFakeRepo { return &dwFakeRepo{rows: map[string]types.DocumentWorkspace{}} }

func (r *dwFakeRepo) Create(_ context.Context, ws *types.DocumentWorkspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.SessionID == ws.SessionID {
			return errors.New("UNIQUE constraint failed")
		}
	}
	_ = ws.BeforeCreate(nil)
	r.rows[ws.ID] = *ws
	return nil
}

func (r *dwFakeRepo) GetBySession(_ context.Context, tenantID uint64, sessionID string) (*types.DocumentWorkspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.TenantID == tenantID && row.SessionID == sessionID {
			cp := row
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *dwFakeRepo) GetByID(_ context.Context, id string) (*types.DocumentWorkspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (r *dwFakeRepo) Update(_ context.Context, ws *types.DocumentWorkspace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[ws.ID] = *ws
	return nil
}

func (r *dwFakeRepo) UpdateIfRevision(_ context.Context, ws *types.DocumentWorkspace, expected int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rows[ws.ID].Revision != expected {
		return false, nil
	}
	r.rows[ws.ID] = *ws
	return true, nil
}

func (r *dwFakeRepo) Delete(_ context.Context, tenantID uint64, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, row := range r.rows {
		if row.TenantID == tenantID && row.SessionID == sessionID {
			delete(r.rows, id)
		}
	}
	return nil
}

type dwFakeFiles struct {
	mu      sync.Mutex
	n       int
	blobs   map[string][]byte
	temp    map[string]bool
	deleted []string
}

func newDWFakeFiles() *dwFakeFiles {
	return &dwFakeFiles{blobs: map[string][]byte{}, temp: map[string]bool{}}
}

func (f *dwFakeFiles) CheckConnectivity(context.Context) error { return nil }
func (f *dwFakeFiles) SaveFile(context.Context, *multipart.FileHeader, uint64, string) (string, error) {
	return "", errors.New("unused")
}

func (f *dwFakeFiles) SaveBytes(_ context.Context, data []byte, _ uint64, _ string, temp bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	ref := types.ResourceScheme + fmt.Sprintf("%022d", f.n)
	f.blobs[ref] = append([]byte(nil), data...)
	f.temp[ref] = temp
	return ref, nil
}

func (f *dwFakeFiles) GetFile(_ context.Context, ref string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.blobs[ref]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *dwFakeFiles) get(ref string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blobs[ref]
}

func (f *dwFakeFiles) GetFileURL(context.Context, string) (string, error) { return "", nil }
func (f *dwFakeFiles) DeleteFile(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.blobs, ref)
	f.deleted = append(f.deleted, ref)
	return nil
}
func (f *dwFakeFiles) CopyFile(context.Context, string, uint64, string) (string, error) {
	return "", errors.New("unused")
}

type dwBinding struct{ ref, ownerType, ownerID, relation string }

type dwFakeCatalog struct {
	mu       sync.Mutex
	bindings []dwBinding
	grants   map[string]string
}

func (c *dwFakeCatalog) Register(context.Context, uint64, string, interfaces.ResourceRegistration) (string, error) {
	return "", nil
}
func (c *dwFakeCatalog) Resolve(context.Context, string) (*types.StoredResource, error) {
	return nil, nil
}
func (c *dwFakeCatalog) ResolvePath(_ context.Context, v string) (string, *types.StoredResource, error) {
	return v, nil, nil
}

func (c *dwFakeCatalog) Bind(_ context.Context, ref, ownerType, ownerID, relation string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings = append(c.bindings, dwBinding{ref, ownerType, ownerID, relation})
	return nil
}

func (c *dwFakeCatalog) bound(ref, ownerType, ownerID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range c.bindings {
		if b.ref == ref && b.ownerType == ownerType && b.ownerID == ownerID {
			return true
		}
	}
	return false
}

func (c *dwFakeCatalog) Release(context.Context, string, string, string) (int64, error) {
	return 0, nil
}
func (c *dwFakeCatalog) MarkDeleted(context.Context, string) error { return nil }

func (c *dwFakeCatalog) CreateAccessGrant(_ context.Context, ref string, _ time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.grants == nil {
		c.grants = map[string]string{}
	}
	token := fmt.Sprintf("grant%d", len(c.grants)+1)
	c.grants[token] = ref
	return token, nil
}

func (c *dwFakeCatalog) ResolveAccessGrant(context.Context, string) (*types.StoredResource, error) {
	return nil, nil
}

type dwFakeAttachments struct {
	docs  map[string]*types.TemporaryDocument
	bytes map[string][]byte
}

func (a *dwFakeAttachments) Create(context.Context, uint64, string, string, string, int64, io.Reader, types.TemporaryDocumentCreateOptions) (*types.TemporaryDocument, error) {
	return nil, errors.New("unused")
}

func (a *dwFakeAttachments) Get(_ context.Context, _ uint64, sessionID, id string) (*types.TemporaryDocument, error) {
	doc := a.docs[id]
	if doc == nil || doc.SessionID != sessionID {
		return nil, nil
	}
	return doc, nil
}

func (a *dwFakeAttachments) OpenFile(_ context.Context, _ uint64, _ string, id string) (io.ReadCloser, string, error) {
	doc := a.docs[id]
	if doc == nil {
		return nil, "", errors.New("attachment not found")
	}
	return io.NopCloser(bytes.NewReader(a.bytes[id])), doc.FileName, nil
}

func (a *dwFakeAttachments) List(context.Context, uint64, string) ([]*types.TemporaryDocument, error) {
	return nil, nil
}
func (a *dwFakeAttachments) Delete(context.Context, uint64, string, string) error { return nil }
func (a *dwFakeAttachments) ResolveForPrompt(context.Context, uint64, string, []string, string) (*types.TemporaryDocumentPromptResult, error) {
	return nil, nil
}
func (a *dwFakeAttachments) Process(context.Context, *asynq.Task) error { return nil }
func (a *dwFakeAttachments) CleanupExpired(context.Context) error       { return nil }

type dwFakeMessages struct {
	mu       sync.Mutex
	messages []*types.Message
	updated  []*types.Message
}

func (m *dwFakeMessages) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages, nil
}

func (m *dwFakeMessages) UpdateMessage(_ context.Context, msg *types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updated = append(m.updated, msg)
	return nil
}

// fakeDocumentServer stands in for ONLYOFFICE DS: /command, /converter and
// file downloads under /files/.
type fakeDocumentServer struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	files    map[string][]byte
	commands []map[string]interface{}
	// onCommand returns the DS error code for a command body.
	onCommand func(body map[string]interface{}) int
	converts  []map[string]interface{}
}

func newFakeDocumentServer(t *testing.T) *fakeDocumentServer {
	ds := &fakeDocumentServer{t: t, files: map[string][]byte{}}
	ds.srv = httptest.NewServer(http.HandlerFunc(ds.serve))
	t.Cleanup(ds.srv.Close)
	return ds
}

func (ds *fakeDocumentServer) put(name string, data []byte) string {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.files[name] = data
	return ds.srv.URL + "/files/" + name
}

// verifySigned checks both the body token and the Authorization payload.
func (ds *fakeDocumentServer) verifySigned(r *http.Request) map[string]interface{} {
	var body map[string]interface{}
	require.NoError(ds.t, json.NewDecoder(r.Body).Decode(&body))
	tokenStr, _ := body["token"].(string)
	parsed, err := jwt.Parse(tokenStr, func(*jwt.Token) (interface{}, error) { return []byte(dwTestSecret), nil })
	require.NoError(ds.t, err)
	require.True(ds.t, parsed.Valid)
	header := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	hdr, err := jwt.Parse(header, func(*jwt.Token) (interface{}, error) { return []byte(dwTestSecret), nil })
	require.NoError(ds.t, err)
	payload, ok := hdr.Claims.(jwt.MapClaims)["payload"].(map[string]interface{})
	require.True(ds.t, ok)
	require.Equal(ds.t, body["key"], payload["key"])
	return body
}

func (ds *fakeDocumentServer) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/command":
		body := ds.verifySigned(r)
		ds.mu.Lock()
		ds.commands = append(ds.commands, body)
		handler := ds.onCommand
		ds.mu.Unlock()
		code := 0
		if handler != nil {
			code = handler(body)
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"error": code})
	case r.URL.Path == "/converter":
		require.Equal(ds.t, "application/json", r.Header.Get("Accept"))
		body := ds.verifySigned(r)
		ds.mu.Lock()
		ds.converts = append(ds.converts, body)
		ds.mu.Unlock()
		url := ds.put("converted.docx", []byte("PK-converted-docx"))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"endConvert": true, "fileUrl": url, "fileType": "docx", "percent": 100,
		})
	case strings.HasPrefix(r.URL.Path, "/files/"):
		ds.mu.Lock()
		data, ok := ds.files[strings.TrimPrefix(r.URL.Path, "/files/")]
		ds.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

type dwFixture struct {
	svc      *documentWorkspaceService
	repo     *dwFakeRepo
	files    *dwFakeFiles
	catalog  *dwFakeCatalog
	attach   *dwFakeAttachments
	messages *dwFakeMessages
	revs     *dwFakeRevisions
	ds       *fakeDocumentServer
}

func newDWFixture(t *testing.T) *dwFixture {
	ds := newFakeDocumentServer(t)
	fx := &dwFixture{
		repo: newDWFakeRepo(), files: newDWFakeFiles(), catalog: &dwFakeCatalog{},
		attach: &dwFakeAttachments{
			docs: map[string]*types.TemporaryDocument{
				"att-docx": {ID: "att-docx", SessionID: "sess-1", FileName: "Công văn.docx", FileType: ".docx"},
				"att-doc":  {ID: "att-doc", SessionID: "sess-1", FileName: "old.doc", FileType: ".doc"},
				"att-pdf":  {ID: "att-pdf", SessionID: "sess-1", FileName: "a.pdf", FileType: ".pdf"},
			},
			bytes: map[string][]byte{
				"att-docx": []byte("PK-original-docx"),
				"att-doc":  []byte("legacy-doc-bytes"),
				"att-pdf":  []byte("%PDF"),
			},
		},
		messages: &dwFakeMessages{},
		ds:       ds,
	}
	cfg := &config.OnlyOfficeConfig{
		JWTSecret: dwTestSecret, PublicURL: "https://docs.example", InternalURL: ds.srv.URL,
		BackendURL: "http://app:8080", SaveWaitSeconds: 5,
	}
	fx.revs = &dwFakeRevisions{}
	fx.svc = newDocumentWorkspaceService(cfg, fx.repo, fx.files, fx.catalog, fx.attach, fx.messages, fx.revs)
	return fx
}

func (fx *dwFixture) create(t *testing.T) *types.DocumentWorkspace {
	ws, err := fx.svc.CreateFromAttachment(context.Background(), 7, "sess-1", "user-1", "att-docx")
	require.NoError(t, err)
	return ws
}

func (fx *dwFixture) ticket(t *testing.T, ws *types.DocumentWorkspace) string {
	ticket, err := fx.svc.issueCallbackTicket(ws)
	require.NoError(t, err)
	return ticket
}

func dwBearer(t *testing.T, secret string, cb *types.OnlyOfficeCallback) string {
	raw, err := json.Marshal(cb)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &payload))
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"payload": payload}).
		SignedString([]byte(secret))
	require.NoError(t, err)
	return "Bearer " + token
}

func requireAppCode(t *testing.T, err error, code apperrors.ErrorCode) {
	t.Helper()
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok, "expected AppError, got %v", err)
	require.Equal(t, code, appErr.Code)
}

// ---- tests ---------------------------------------------------------------

func TestDocumentWorkspaceCreateFromDocxCopiesBytes(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)

	require.Equal(t, "Công văn.docx", ws.FileName)
	require.Equal(t, "docx", ws.FileType)
	require.Equal(t, types.DocumentWorkspaceStatusOpen, ws.Status)
	require.Equal(t, ws.OriginalRef, ws.CurrentRef)
	require.Equal(t, []byte("PK-original-docx"), fx.files.get(ws.CurrentRef))
	require.False(t, fx.files.temp[ws.CurrentRef], "workspace copy must outlive the 24h attachment")
	require.True(t, fx.catalog.bound(ws.OriginalRef, types.ResourceOwnerDocumentWorkspace, ws.ID))
	require.Equal(t, ws.ID+"-0", ws.EditorKey())

	_, err := fx.svc.CreateFromAttachment(context.Background(), 7, "sess-1", "user-1", "att-docx")
	requireAppCode(t, err, apperrors.ErrConflict)

	_, err = fx.svc.CreateFromAttachment(context.Background(), 7, "sess-2", "user-1", "att-pdf")
	require.Error(t, err)
}

func TestDocumentWorkspaceCreateFromDocConverts(t *testing.T) {
	fx := newDWFixture(t)
	ws, err := fx.svc.CreateFromAttachment(context.Background(), 7, "sess-1", "user-1", "att-doc")
	require.NoError(t, err)

	require.Equal(t, "old.docx", ws.FileName)
	require.Equal(t, []byte("PK-converted-docx"), fx.files.get(ws.CurrentRef))
	require.Len(t, fx.ds.converts, 1)
	conv := fx.ds.converts[0]
	require.Equal(t, "doc", conv["filetype"])
	require.Equal(t, "docx", conv["outputtype"])
	require.Equal(t, false, conv["async"])
	require.True(t, strings.HasPrefix(conv["url"].(string), "http://app:8080/r/grant"))

	// The staged .doc copy is removed once the converter has fetched it.
	stagedRef := fx.catalog.grants["grant1"]
	require.Contains(t, fx.files.deleted, stagedRef)
	require.True(t, fx.files.temp[stagedRef])
	require.NotEqual(t, stagedRef, ws.CurrentRef)
}

func TestDocumentWorkspaceViewSignsEditorConfig(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	ctx := WithDocumentEditorOrigin(context.Background(), "https://chat.example")

	view, err := fx.svc.View(ctx, ws, "user-1", "Nguyễn Văn A", "")
	require.NoError(t, err)
	require.Equal(t, ws.EditorKey(), view.EditorKey)
	require.NotNil(t, view.Editor)
	require.Equal(t, "https://docs.example", view.Editor.DocumentServerURL)

	cfg := view.Editor.Config
	token, _ := cfg["token"].(string)
	parsed, err := jwt.Parse(token, func(*jwt.Token) (interface{}, error) { return []byte(dwTestSecret), nil },
		jwt.WithValidMethods([]string{"HS256"}))
	require.NoError(t, err)
	claims := parsed.Claims.(jwt.MapClaims)
	doc := claims["document"].(map[string]interface{})
	require.Equal(t, ws.EditorKey(), doc["key"])
	require.Equal(t, "http://app:8080/r/grant1", doc["url"])
	require.Equal(t, ws.CurrentRef, fx.catalog.grants["grant1"])
	editor := claims["editorConfig"].(map[string]interface{})
	require.Equal(t, "vi", editor["lang"])
	require.Equal(t, "Nguyễn Văn A", editor["user"].(map[string]interface{})["name"])
	plugins := editor["plugins"].(map[string]interface{})
	options := plugins["options"].(map[string]interface{})
	require.Equal(t, "https://chat.example", options["all"].(map[string]interface{})["hostOrigin"])
	require.Equal(t, "https://chat.example", options[onlyOfficeAssistantPluginGUID].(map[string]interface{})["hostOrigin"])
	review := editor["customization"].(map[string]interface{})["review"].(map[string]interface{})
	require.Equal(t, false, review["trackChanges"], "AI edits are undone via snapshots, not tracked changes")

	callbackURL := editor["callbackUrl"].(string)
	ticket, ok := strings.CutPrefix(callbackURL, "http://app:8080/onlyoffice/callback/")
	require.True(t, ok)
	parsedTicket, err := fx.svc.parseCallbackTicket(ticket)
	require.NoError(t, err)
	require.Equal(t, ws.ID, parsedTicket.workspaceID)
	require.Equal(t, uint64(7), parsedTicket.tenantID)

	// The signed config must match what the frontend receives.
	require.Equal(t, cfg["document"].(map[string]interface{})["key"], doc["key"])

	en, err := fx.svc.View(context.Background(), ws, "user-1", "", "en")
	require.NoError(t, err)
	require.Equal(t, "en", en.Editor.Config["editorConfig"].(map[string]interface{})["lang"])
}

func TestDocumentWorkspaceForceSaveNothingToSave(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	fx.ds.onCommand = func(body map[string]interface{}) int {
		require.Equal(t, "forcesave", body["c"])
		require.Equal(t, ws.EditorKey(), body["key"])
		return onlyOfficeCommandNoChanges
	}
	require.NoError(t, fx.svc.ForceSave(context.Background(), 7, "sess-1"))

	start := time.Now()
	got, data, err := fx.svc.PrepareExternalWrite(context.Background(), 7, "sess-1", 5*time.Second)
	require.NoError(t, err)
	require.Less(t, time.Since(start), 2*time.Second, "error 4 means no callback is coming")
	require.Equal(t, []byte("PK-original-docx"), data)
	require.Equal(t, 0, got.Revision)

	fx.ds.onCommand = func(map[string]interface{}) int { return 5 }
	require.Error(t, fx.svc.ForceSave(context.Background(), 7, "sess-1"))
}

func TestDocumentWorkspaceForceSavedCallbackSignalsWaiter(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	ticket := fx.ticket(t, ws)
	savedURL := fx.ds.put("saved.docx", []byte("PK-human-edits"))

	callbackErr := make(chan error, 1)
	fx.ds.onCommand = func(body map[string]interface{}) int {
		go func() {
			time.Sleep(50 * time.Millisecond)
			cb := &types.OnlyOfficeCallback{Key: body["key"].(string), Status: 6, URL: savedURL}
			callbackErr <- fx.svc.HandleCallback(context.Background(), ticket, dwBearer(t, dwTestSecret, cb), cb)
		}()
		return onlyOfficeCommandOK
	}

	got, data, err := fx.svc.PrepareExternalWrite(context.Background(), 7, "sess-1", 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, <-callbackErr)
	require.Equal(t, []byte("PK-human-edits"), data)
	require.Equal(t, 0, got.Revision, "a force-save must not rotate the key")
	require.Equal(t, 1, got.SaveCount)
	require.NotNil(t, got.LastSavedAt)
	require.NotEqual(t, ws.OriginalRef, got.CurrentRef)
	require.Equal(t, types.DocumentWorkspaceStatusOpen, got.Status)

	// AI edit on top of the saved bytes.
	committed, err := fx.svc.CommitExternalWrite(context.Background(), 7, "sess-1", got.Revision, []byte("PK-ai-edit"))
	require.NoError(t, err)
	require.Equal(t, 1, committed.Revision)
	require.Equal(t, ws.ID+"-1", committed.EditorKey())
	require.Equal(t, []byte("PK-ai-edit"), fx.files.get(committed.CurrentRef))
}

func TestDocumentWorkspaceFinalSaveClosesAndAttachesArtifact(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	fx.messages.messages = []*types.Message{
		{ID: "m-user", SessionID: "sess-1", Role: "user"},
		{ID: "m-answer", SessionID: "sess-1", Role: "assistant"},
	}
	url := fx.ds.put("final.docx", []byte("PK-final"))
	cb := &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 2, URL: url}

	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))

	got, _ := fx.repo.GetByID(context.Background(), ws.ID)
	require.Equal(t, types.DocumentWorkspaceStatusClosed, got.Status)
	require.NotNil(t, got.ClosedAt)
	require.Equal(t, 1, got.SaveCount)
	require.Equal(t, 1, got.Revision, "the next editor session needs a fresh key")
	require.Equal(t, []byte("PK-final"), fx.files.get(got.CurrentRef))

	require.Len(t, fx.messages.updated, 1)
	msg := fx.messages.updated[0]
	require.Equal(t, "m-answer", msg.ID)
	require.Len(t, msg.Artifacts, 1)
	require.Equal(t, got.CurrentRef, msg.Artifacts[0].URL)
	require.Equal(t, "Công văn.docx", msg.Artifacts[0].FileName)
	require.True(t, fx.catalog.bound(got.CurrentRef, types.ResourceOwnerMessage, "m-answer"))

	// Status 1 from a re-opened editor flips it back to open.
	reopen := &types.OnlyOfficeCallback{Key: got.EditorKey(), Status: 1}
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, reopen), reopen))
	got, _ = fx.repo.GetByID(context.Background(), ws.ID)
	require.Equal(t, types.DocumentWorkspaceStatusOpen, got.Status)
}

func TestDocumentWorkspaceStaleKeySaveIgnored(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	staleKey := ws.EditorKey()
	committed, err := fx.svc.CommitExternalWrite(context.Background(), 7, "sess-1", 0, []byte("PK-ai"))
	require.NoError(t, err)

	url := fx.ds.put("stale.docx", []byte("PK-stale"))
	for _, status := range []int{6, 2} {
		cb := &types.OnlyOfficeCallback{Key: staleKey, Status: status, URL: url}
		require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))
	}
	got, _ := fx.repo.GetByID(context.Background(), ws.ID)
	require.Equal(t, committed.CurrentRef, got.CurrentRef)
	require.Equal(t, 1, got.Revision)
	require.Equal(t, 0, got.SaveCount)
	require.Equal(t, types.DocumentWorkspaceStatusOpen, got.Status)
}

func TestDocumentWorkspaceCommitConflict(t *testing.T) {
	fx := newDWFixture(t)
	fx.create(t)
	_, err := fx.svc.CommitExternalWrite(context.Background(), 7, "sess-1", 0, []byte("PK-a"))
	require.NoError(t, err)
	_, err = fx.svc.CommitExternalWrite(context.Background(), 7, "sess-1", 0, []byte("PK-b"))
	requireAppCode(t, err, apperrors.ErrConflict)
}

func TestDocumentWorkspaceCallbackVerification(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	cb := &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 4}

	err := fx.svc.HandleCallback(context.Background(), "not-a-ticket", dwBearer(t, dwTestSecret, cb), cb)
	require.ErrorIs(t, err, ErrOnlyOfficeCallbackUnauthorized)

	// A ticket signed with the DS secret itself is not a callback ticket.
	forged, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"workspace_id": ws.ID, "tenant_id": "7", "session_id": "sess-1",
		"type": onlyOfficeCallbackTicketType, "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(dwTestSecret))
	err = fx.svc.HandleCallback(context.Background(), forged, dwBearer(t, dwTestSecret, cb), cb)
	require.ErrorIs(t, err, ErrOnlyOfficeCallbackUnauthorized)

	ticket := fx.ticket(t, ws)
	err = fx.svc.HandleCallback(context.Background(), ticket, dwBearer(t, "wrong-secret", cb), cb)
	require.ErrorIs(t, err, ErrOnlyOfficeCallbackUnauthorized)

	err = fx.svc.HandleCallback(context.Background(), ticket, "", cb)
	require.ErrorIs(t, err, ErrOnlyOfficeCallbackUnauthorized, "unsigned callbacks are rejected")

	// Token-in-body form: the claims are the callback itself.
	bodyToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"key": ws.EditorKey(), "status": 4,
	}).SignedString([]byte(dwTestSecret))
	require.NoError(t, err)
	require.NoError(t, fx.svc.HandleCallback(context.Background(), ticket, "",
		&types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 4, Token: bodyToken}))
	got, _ := fx.repo.GetByID(context.Background(), ws.ID)
	require.Equal(t, types.DocumentWorkspaceStatusClosed, got.Status)
}

func TestDocumentWorkspaceDisabled(t *testing.T) {
	svc := newDocumentWorkspaceService(&config.OnlyOfficeConfig{}, newDWFakeRepo(), newDWFakeFiles(), &dwFakeCatalog{}, &dwFakeAttachments{}, nil, nil)
	require.False(t, svc.Enabled())
	_, err := svc.CreateFromAttachment(context.Background(), 7, "s", "u", "a")
	requireAppCode(t, err, apperrors.ErrServiceUnavailable)
}

func TestDocumentWorkspaceDownloadRewritesPublicURL(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	fx.ds.put("public.docx", []byte("PK-via-public-host"))
	// DS builds callback URLs from the browser-facing address; the app only
	// reaches DS on InternalURL.
	cb := &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6,
		URL: "https://docs.example/files/public.docx?md5=x&expires=1"}
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))
	got, _ := fx.repo.GetByID(context.Background(), ws.ID)
	require.Equal(t, []byte("PK-via-public-host"), fx.files.get(got.CurrentRef))
	require.Equal(t, 1, got.SaveCount)
}

func TestDocumentWorkspaceFetchURLAllowlist(t *testing.T) {
	fx := newDWFixture(t)
	fx.svc.cfg.PublicURL = "https://docs.example/ds"
	fx.svc.cfg.InternalURL = "http://documentserver"

	got, err := fx.svc.documentServerFetchURL("https://docs.example/ds/cache/files/a/output.docx?md5=1")
	require.NoError(t, err)
	require.Equal(t, "http://documentserver/cache/files/a/output.docx?md5=1", got)

	got, err = fx.svc.documentServerFetchURL("http://documentserver/cache/files/b.docx")
	require.NoError(t, err)
	require.Equal(t, "http://documentserver/cache/files/b.docx", got)

	for _, bad := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://evil.example/cache/files/a.docx",
		"file:///etc/passwd",
		"http://docs.example.evil.com/x",
		"",
	} {
		_, err := fx.svc.documentServerFetchURL(bad)
		require.Error(t, err, bad)
	}
}

func TestDocumentWorkspaceCallbackRejectsForeignHost(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("foreign host must never be fetched")
		_, _ = w.Write([]byte("PK-evil"))
	}))
	defer foreign.Close()
	// Same machine, different hostname than the DS (localhost vs 127.0.0.1).
	evilURL := strings.Replace(foreign.URL, "127.0.0.1", "localhost", 1) + "/x.docx"
	cb := &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6, URL: evilURL}
	err := fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb)
	require.Error(t, err)
	got, _ := fx.repo.GetByID(context.Background(), ws.ID)
	require.Equal(t, ws.CurrentRef, got.CurrentRef)
	require.Equal(t, 0, got.SaveCount)
}

func TestDocumentWorkspaceDownloadRefusesRedirects(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	target := fx.ds.put("target.docx", []byte("PK-redirected"))
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}))
	defer redirector.Close()
	cb := &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6, URL: redirector.URL + "/redirect"}
	err := fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb)
	require.Error(t, err)
	got, _ := fx.repo.GetByID(context.Background(), ws.ID)
	require.Equal(t, 0, got.SaveCount)
}

func TestDocumentWorkspaceConcurrentPrepareAllReleased(t *testing.T) {
	fx := newDWFixture(t)
	ws := fx.create(t)
	fx.ds.onCommand = func(map[string]interface{}) int { return onlyOfficeCommandOK }

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, _, err := fx.svc.PrepareExternalWrite(context.Background(), 7, "sess-1", 5*time.Second)
			results <- err
		}()
	}
	require.Eventually(t, func() bool {
		fx.svc.waitersMu.Lock()
		defer fx.svc.waitersMu.Unlock()
		return len(fx.svc.waiters[ws.ID]) == 2
	}, 2*time.Second, 5*time.Millisecond)
	url := fx.ds.put("both.docx", []byte("PK-both"))
	cb := &types.OnlyOfficeCallback{Key: ws.EditorKey(), Status: 6, URL: url}
	start := time.Now()
	require.NoError(t, fx.svc.HandleCallback(context.Background(), fx.ticket(t, ws), dwBearer(t, dwTestSecret, cb), cb))
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.Less(t, time.Since(start), 2*time.Second)
}
