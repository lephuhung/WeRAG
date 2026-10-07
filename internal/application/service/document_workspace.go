package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	onlyOfficeCallbackTicketType = "onlyoffice_callback"
	// onlyOfficeCallbackTicketTTL outlives any realistic editor session; the
	// ticket is minted per View, so a reopened editor gets a fresh one.
	onlyOfficeCallbackTicketTTL = 12 * time.Hour
	// onlyOfficeDownloadGrantTTL bounds the capability URL the Document Server
	// downloads the current file from. DS fetches it once when the editor
	// opens, so two hours only has to cover a slow first load.
	onlyOfficeDownloadGrantTTL = 2 * time.Hour
	// onlyOfficeAssistantPluginGUID is the werag-assistant editor plugin
	// (docker/onlyoffice/plugins/werag-assistant) that reports selections.
	onlyOfficeAssistantPluginGUID = "asc.{7a4b3c2d-9e1f-4a5b-8c6d-0e1f2a3b4c5d}"
	documentWorkspaceArtifactPath = "document_workspace/"
)

// Callback statuses (ONLYOFFICE callback handler API).
const (
	onlyOfficeStatusEditing        = 1
	onlyOfficeStatusMustSave       = 2
	onlyOfficeStatusSaveError      = 3
	onlyOfficeStatusClosedNoChange = 4
	onlyOfficeStatusForceSaved     = 6
	onlyOfficeStatusForceSaveError = 7
)

// Command service result codes.
const (
	onlyOfficeCommandOK        = 0
	onlyOfficeCommandKeyAbsent = 1
	onlyOfficeCommandNoChanges = 4
)

// ErrOnlyOfficeCallbackUnauthorized is returned by HandleCallback when the
// ticket or the Document Server JWT does not verify. The HTTP layer answers
// 403 {"error":1} for it.
var ErrOnlyOfficeCallbackUnauthorized = errors.New("onlyoffice callback: unauthorized")

type documentEditorOriginKey struct{}

// WithDocumentEditorOrigin records the browser origin of the page that will
// mount the editor, passed to the assistant plugin as hostOrigin so it only
// talks to that window.
func WithDocumentEditorOrigin(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, documentEditorOriginKey{}, strings.TrimSpace(origin))
}

// DocumentEditorOrigin returns the origin set by WithDocumentEditorOrigin.
func DocumentEditorOrigin(ctx context.Context) string {
	v, _ := ctx.Value(documentEditorOriginKey{}).(string)
	return v
}

// documentWorkspaceMessageStore is the message-repository surface the final
// save needs to attach the saved file to the session's latest answer.
type documentWorkspaceMessageStore interface {
	GetRecentMessagesBySession(ctx context.Context, sessionID string, limit int) ([]*types.Message, error)
	UpdateMessage(ctx context.Context, message *types.Message) error
}

// saveWaiter is closed by the callback handler when the force-save that a
// PrepareExternalWrite asked for has landed (or failed).
type saveWaiter struct {
	ch   chan struct{}
	once sync.Once
	err  error
}

func (w *saveWaiter) done(err error) {
	w.once.Do(func() {
		w.err = err
		close(w.ch)
	})
}

type documentWorkspaceService struct {
	cfg         *config.OnlyOfficeConfig
	repo        interfaces.DocumentWorkspaceRepository
	files       interfaces.FileService
	catalog     interfaces.ResourceCatalog
	attachments interfaces.TemporaryDocumentService
	messages    documentWorkspaceMessageStore
	httpClient  *http.Client

	// waiters holds, per workspace ID, one saveWaiter per in-flight
	// PrepareExternalWrite. A save callback releases all of them; a caller
	// that times out removes only its own.
	waitersMu sync.Mutex
	waiters   map[string][]*saveWaiter
}

// NewDocumentWorkspaceService wires the document-assistant workspace service.
func NewDocumentWorkspaceService(
	cfg *config.Config,
	repo interfaces.DocumentWorkspaceRepository,
	files interfaces.FileService,
	catalog interfaces.ResourceCatalog,
	attachments interfaces.TemporaryDocumentService,
	messages interfaces.MessageRepository,
) interfaces.DocumentWorkspaceService {
	var oo *config.OnlyOfficeConfig
	if cfg != nil {
		oo = cfg.OnlyOffice
	}
	var store documentWorkspaceMessageStore
	if messages != nil {
		store = messages
	}
	return newDocumentWorkspaceService(oo, repo, files, catalog, attachments, store)
}

func newDocumentWorkspaceService(
	cfg *config.OnlyOfficeConfig,
	repo interfaces.DocumentWorkspaceRepository,
	files interfaces.FileService,
	catalog interfaces.ResourceCatalog,
	attachments interfaces.TemporaryDocumentService,
	messages documentWorkspaceMessageStore,
) *documentWorkspaceService {
	return &documentWorkspaceService{
		cfg: cfg, repo: repo, files: files, catalog: catalog,
		attachments: attachments, messages: messages,
		httpClient: &http.Client{
			Timeout: 2 * time.Minute,
			// Download URLs are allowlisted per request (see
			// documentServerFetchURL); a redirect would bypass that check.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		waiters: map[string][]*saveWaiter{},
	}
}

func (s *documentWorkspaceService) Enabled() bool { return s != nil && s.cfg.Enabled() }

func maxDocumentWorkspaceBytes() int64 { return secutils.GetMaxFileSizeMB() * 1024 * 1024 }

// ---------------------------------------------------------------------------
// Workspace lifecycle
// ---------------------------------------------------------------------------

func (s *documentWorkspaceService) CreateFromAttachment(
	ctx context.Context, tenantID uint64, sessionID, userID, attachmentID string,
) (*types.DocumentWorkspace, error) {
	if !s.Enabled() {
		return nil, apperrors.NewServiceUnavailableError("document editor is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	attachmentID = strings.TrimSpace(attachmentID)
	if tenantID == 0 || sessionID == "" || attachmentID == "" {
		return nil, apperrors.NewBadRequestError("attachment_id is required")
	}
	existing, err := s.repo.GetBySession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperrors.NewConflictError("session already has a document workspace")
	}
	doc, err := s.attachments.Get(ctx, tenantID, sessionID, attachmentID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, apperrors.NewNotFoundError("Attachment not found")
	}
	ext := strings.ToLower(filepath.Ext(doc.FileName))
	if ext != ".docx" && ext != ".doc" {
		return nil, apperrors.NewBadRequestError("only .docx and .doc files can be opened in the document editor")
	}
	reader, fileName, err := s.attachments.OpenFile(ctx, tenantID, sessionID, attachmentID)
	if err != nil {
		return nil, err
	}
	data, err := readCapped(reader, maxDocumentWorkspaceBytes())
	_ = reader.Close()
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = doc.FileName
	}
	fileName = filepath.Base(fileName)

	wsID := uuid.NewString()
	if ext == ".doc" {
		data, err = s.convertDocToDocx(ctx, tenantID, wsID, fileName, data)
		if err != nil {
			return nil, err
		}
		fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".docx"
	}

	ref, err := s.files.SaveBytes(ctx, data, tenantID, documentWorkspaceStorageName(wsID), false)
	if err != nil {
		return nil, fmt.Errorf("save document copy: %w", err)
	}
	ws := &types.DocumentWorkspace{
		ID: wsID, TenantID: tenantID, SessionID: sessionID, UserID: userID,
		OriginalRef: ref, CurrentRef: ref, FileName: fileName, FileType: "docx",
		FileSize: int64(len(data)), Status: types.DocumentWorkspaceStatusOpen,
	}
	if err := s.repo.Create(ctx, ws); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") ||
			strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, apperrors.NewConflictError("session already has a document workspace")
		}
		return nil, err
	}
	s.bind(ctx, ref, ws.ID, types.ResourceRelationSourceFile)
	s.bind(ctx, ref, ws.ID, types.ResourceRelationArtifact)
	logger.Infof(ctx, "[DocumentWorkspace] created workspace=%s session=%s file=%s size=%d",
		ws.ID, sessionID, secutils.SanitizeForLog(fileName), ws.FileSize)
	return ws, nil
}

func (s *documentWorkspaceService) GetBySession(
	ctx context.Context, tenantID uint64, sessionID string,
) (*types.DocumentWorkspace, error) {
	ws, err := s.repo.GetBySession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, apperrors.NewNotFoundError("Document workspace not found")
	}
	return ws, nil
}

func (s *documentWorkspaceService) View(
	ctx context.Context, ws *types.DocumentWorkspace, userID, userName, lang string,
) (*types.DocumentWorkspaceView, error) {
	if ws == nil {
		return nil, apperrors.NewNotFoundError("Document workspace not found")
	}
	view := &types.DocumentWorkspaceView{DocumentWorkspace: ws, EditorKey: ws.EditorKey()}
	if !s.Enabled() {
		return view, nil
	}
	if s.cfg.BackendURL == "" {
		return nil, apperrors.NewServiceUnavailableError("ONLYOFFICE_BACKEND_URL (or APP_EXTERNAL_URL) is not configured")
	}
	grant, err := s.catalog.CreateAccessGrant(ctx, ws.CurrentRef, onlyOfficeDownloadGrantTTL)
	if err != nil {
		return nil, fmt.Errorf("create document download grant: %w", err)
	}
	ticket, err := s.issueCallbackTicket(ws)
	if err != nil {
		return nil, err
	}
	if lang == "" {
		lang = "vi"
	}
	if strings.TrimSpace(userName) == "" {
		userName = userID
	}
	cfg := map[string]interface{}{
		"documentType": "word",
		"type":         "desktop",
		"document": map[string]interface{}{
			"fileType": "docx",
			"key":      ws.EditorKey(),
			"title":    ws.FileName,
			"url":      s.cfg.BackendURL + "/r/" + grant,
			"permissions": map[string]interface{}{
				"edit": true, "review": true, "comment": true,
				"download": true, "print": true, "chat": false,
			},
		},
		"editorConfig": map[string]interface{}{
			"callbackUrl": s.cfg.BackendURL + "/onlyoffice/callback/" + ticket,
			"lang":        lang,
			"mode":        "edit",
			"user":        map[string]interface{}{"id": userID, "name": userName},
			"customization": map[string]interface{}{
				"forcesave":     true,
				"autosave":      true,
				"compactHeader": true,
				"hideRightMenu": false,
				"review":        map[string]interface{}{"trackChanges": true, "showReviewChanges": true, "reviewDisplay": "markup"},
				"features":      map[string]interface{}{"spellcheck": false},
				"plugins":       true,
			},
			"plugins": map[string]interface{}{
				"autostart": []string{onlyOfficeAssistantPluginGUID},
				// ONLYOFFICE merges options under "all" and under the plugin's
				// own guid into window.Asc.plugin.info.options; emit both so the
				// assistant plugin finds hostOrigin whichever rule applies.
				"options": map[string]interface{}{
					"all":                         map[string]interface{}{"hostOrigin": DocumentEditorOrigin(ctx)},
					onlyOfficeAssistantPluginGUID: map[string]interface{}{"hostOrigin": DocumentEditorOrigin(ctx)},
				},
			},
		},
	}
	token, err := s.sign(jwt.MapClaims(cfg))
	if err != nil {
		return nil, err
	}
	cfg["token"] = token
	view.Editor = &types.DocumentEditorConfig{DocumentServerURL: s.cfg.PublicURL, Config: cfg}
	return view, nil
}

func (s *documentWorkspaceService) OpenCurrent(
	ctx context.Context, tenantID uint64, sessionID string,
) (io.ReadCloser, *types.DocumentWorkspace, error) {
	ws, err := s.GetBySession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	reader, err := s.files.GetFile(ctx, ws.CurrentRef)
	if err != nil {
		return nil, nil, fmt.Errorf("open document: %w", err)
	}
	return reader, ws, nil
}

// ---------------------------------------------------------------------------
// Saves and external writes
// ---------------------------------------------------------------------------

func (s *documentWorkspaceService) ForceSave(ctx context.Context, tenantID uint64, sessionID string) error {
	if !s.Enabled() {
		return apperrors.NewServiceUnavailableError("document editor is not configured")
	}
	ws, err := s.GetBySession(ctx, tenantID, sessionID)
	if err != nil {
		return err
	}
	// The browser fires this from beforeunload/pagehide with keepalive and
	// may drop the connection before the Document Server answers; detach
	// from the request context so the command still reaches the server.
	cmdCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	_, err = s.forceSave(cmdCtx, ws)
	return err
}

// forceSave sends the forcesave command. willCallback reports whether a
// status-6 callback will follow (false when there was nothing to save or the
// document is not open in any editor).
func (s *documentWorkspaceService) forceSave(ctx context.Context, ws *types.DocumentWorkspace) (bool, error) {
	body := map[string]interface{}{
		"c":        "forcesave",
		"key":      ws.EditorKey(),
		"userdata": "werag:" + ws.ID,
	}
	var res struct {
		Error int `json:"error"`
	}
	if err := s.postDocumentServer(ctx, "/command", body, &res); err != nil {
		return false, fmt.Errorf("onlyoffice forcesave: %w", err)
	}
	switch res.Error {
	case onlyOfficeCommandOK:
		return true, nil
	case onlyOfficeCommandNoChanges, onlyOfficeCommandKeyAbsent:
		return false, nil
	default:
		return false, fmt.Errorf("onlyoffice forcesave failed: error %d", res.Error)
	}
}

// PrepareExternalWrite force-saves the open editor and returns the bytes the
// AI edit must start from.
//
// Known window, by design: keystrokes typed after the force-save callback
// and before CommitExternalWrite are still under the old editor key. The
// commit rotates the key, the editor reloads the AI's version, and those few
// keystrokes are dropped (a later save under the old key is ignored as stale
// rather than allowed to overwrite the AI edit). Closing the window would
// need an editor lock, which ONLYOFFICE does not offer to the backend.
func (s *documentWorkspaceService) PrepareExternalWrite(
	ctx context.Context, tenantID uint64, sessionID string, wait time.Duration,
) (*types.DocumentWorkspace, []byte, error) {
	ws, err := s.GetBySession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if s.Enabled() {
		// Register before asking: the callback may land before the command
		// response does.
		waiter := s.waiterFor(ws.ID)
		willCallback, fsErr := s.forceSave(ctx, ws)
		switch {
		case fsErr != nil:
			s.dropWaiter(ws.ID, waiter)
			logger.Warnf(ctx, "[DocumentWorkspace] force-save before external write failed, proceeding: workspace=%s err=%v", ws.ID, fsErr)
		case !willCallback:
			s.dropWaiter(ws.ID, waiter)
		default:
			if wait <= 0 {
				wait = s.cfg.SaveWait()
			}
			timer := time.NewTimer(wait)
			select {
			case <-waiter.ch:
				if waiter.err != nil {
					logger.Warnf(ctx, "[DocumentWorkspace] force-save reported an error, proceeding: workspace=%s err=%v", ws.ID, waiter.err)
				}
			case <-timer.C:
				s.dropWaiter(ws.ID, waiter)
				logger.Warnf(ctx, "[DocumentWorkspace] force-save callback did not arrive within %s, proceeding: workspace=%s", wait, ws.ID)
			case <-ctx.Done():
				timer.Stop()
				s.dropWaiter(ws.ID, waiter)
				return nil, nil, ctx.Err()
			}
			timer.Stop()
			// The callback replaced CurrentRef; read the row again.
			if fresh, err := s.repo.GetByID(ctx, ws.ID); err == nil && fresh != nil {
				ws = fresh
			}
		}
	}
	reader, err := s.files.GetFile(ctx, ws.CurrentRef)
	if err != nil {
		return nil, nil, fmt.Errorf("open document: %w", err)
	}
	defer reader.Close()
	data, err := readCapped(reader, maxDocumentWorkspaceBytes())
	if err != nil {
		return nil, nil, fmt.Errorf("read document: %w", err)
	}
	return ws, data, nil
}

func (s *documentWorkspaceService) CommitExternalWrite(
	ctx context.Context, tenantID uint64, sessionID string, expectedRevision int, data []byte,
) (*types.DocumentWorkspace, error) {
	if len(data) == 0 {
		return nil, apperrors.NewBadRequestError("document is empty")
	}
	ws, err := s.GetBySession(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	if ws.Revision != expectedRevision {
		return nil, apperrors.NewConflictError("document changed since it was read; read it again")
	}
	ref, err := s.files.SaveBytes(ctx, data, tenantID, documentWorkspaceStorageName(ws.ID), false)
	if err != nil {
		return nil, fmt.Errorf("save document: %w", err)
	}
	s.bind(ctx, ref, ws.ID, types.ResourceRelationArtifact)
	ws.CurrentRef = ref
	ws.FileSize = int64(len(data))
	ws.Revision = expectedRevision + 1
	ok, err := s.repo.UpdateIfRevision(ctx, ws, expectedRevision)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperrors.NewConflictError("document changed since it was read; read it again")
	}
	logger.Infof(ctx, "[DocumentWorkspace] external write committed: workspace=%s revision=%d size=%d",
		ws.ID, ws.Revision, ws.FileSize)
	return ws, nil
}

// ---------------------------------------------------------------------------
// Document Server callback
// ---------------------------------------------------------------------------

func (s *documentWorkspaceService) HandleCallback(
	ctx context.Context, ticket string, authorization string, payload *types.OnlyOfficeCallback,
) error {
	if !s.Enabled() {
		return ErrOnlyOfficeCallbackUnauthorized
	}
	claims, err := s.parseCallbackTicket(ticket)
	if err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] callback ticket rejected: %v", err)
		return ErrOnlyOfficeCallbackUnauthorized
	}
	cb, err := s.verifyCallbackBody(authorization, payload)
	if err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] callback JWT rejected: workspace=%s err=%v", claims.workspaceID, err)
		return ErrOnlyOfficeCallbackUnauthorized
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, claims.tenantID)

	ws, err := s.repo.GetByID(ctx, claims.workspaceID)
	if err != nil {
		return err
	}
	if ws == nil || ws.TenantID != claims.tenantID || ws.SessionID != claims.sessionID {
		logger.Warnf(ctx, "[DocumentWorkspace] callback for missing workspace=%s ignored", claims.workspaceID)
		return nil
	}
	keyRevision, ok := parseEditorKeyRevision(ws.ID, cb.Key)
	if !ok {
		logger.Warnf(ctx, "[DocumentWorkspace] callback key %q does not belong to workspace=%s, ignored",
			secutils.SanitizeForLog(cb.Key), ws.ID)
		return nil
	}

	switch cb.Status {
	case onlyOfficeStatusEditing:
		logger.Debugf(ctx, "[DocumentWorkspace] editing: workspace=%s users=%v", ws.ID, cb.Users)
		if ws.Status != types.DocumentWorkspaceStatusOpen && keyRevision == ws.Revision {
			ws.Status = types.DocumentWorkspaceStatusOpen
			ws.ClosedAt = nil
			if err := s.repo.Update(ctx, ws); err != nil {
				logger.Warnf(ctx, "[DocumentWorkspace] reopen workspace=%s failed: %v", ws.ID, err)
			}
		}
		return nil

	case onlyOfficeStatusMustSave, onlyOfficeStatusForceSaved:
		final := cb.Status == onlyOfficeStatusMustSave
		if keyRevision != ws.Revision {
			// The editor was working on a revision an AI edit has already
			// replaced; storing it would silently undo that edit.
			logger.Warnf(ctx, "[DocumentWorkspace] stale save ignored: workspace=%s key_revision=%d current=%d status=%d",
				ws.ID, keyRevision, ws.Revision, cb.Status)
			s.signal(ws.ID, nil)
			return nil
		}
		err := s.storeEditorSave(ctx, ws, cb, final)
		s.signal(ws.ID, err)
		return err

	case onlyOfficeStatusClosedNoChange:
		if keyRevision == ws.Revision {
			now := time.Now()
			ws.Status = types.DocumentWorkspaceStatusClosed
			ws.ClosedAt = &now
			if err := s.repo.Update(ctx, ws); err != nil {
				s.signal(ws.ID, nil)
				return err
			}
		}
		s.signal(ws.ID, nil)
		return nil

	case onlyOfficeStatusSaveError, onlyOfficeStatusForceSaveError:
		logger.Errorf(ctx, "[DocumentWorkspace] document server save error: workspace=%s status=%d", ws.ID, cb.Status)
		s.signal(ws.ID, fmt.Errorf("document server reported save error (status %d)", cb.Status))
		return nil

	default:
		logger.Infof(ctx, "[DocumentWorkspace] callback status %d ignored: workspace=%s", cb.Status, ws.ID)
		return nil
	}
}

// storeEditorSave downloads the editor's file and makes it CurrentRef.
func (s *documentWorkspaceService) storeEditorSave(
	ctx context.Context, ws *types.DocumentWorkspace, cb *types.OnlyOfficeCallback, final bool,
) error {
	if strings.TrimSpace(cb.URL) == "" {
		return fmt.Errorf("callback status %d without url", cb.Status)
	}
	// The DS link expires (~15 minutes): fetch it right away.
	data, err := s.download(ctx, cb.URL)
	if err != nil {
		return fmt.Errorf("download saved document: %w", err)
	}
	ref, err := s.files.SaveBytes(ctx, data, ws.TenantID, documentWorkspaceStorageName(ws.ID), false)
	if err != nil {
		return fmt.Errorf("store saved document: %w", err)
	}
	s.bind(ctx, ref, ws.ID, types.ResourceRelationArtifact)

	expected := ws.Revision
	now := time.Now()
	ws.CurrentRef = ref
	ws.FileSize = int64(len(data))
	ws.SaveCount++
	ws.LastSavedAt = &now
	if final {
		// Every editor has left. The next editor session must use a new key:
		// the Document Server may still cache the closed one.
		ws.Status = types.DocumentWorkspaceStatusClosed
		ws.ClosedAt = &now
		ws.Revision = expected + 1
	}
	ok, err := s.repo.UpdateIfRevision(ctx, ws, expected)
	if err != nil {
		return err
	}
	if !ok {
		logger.Warnf(ctx, "[DocumentWorkspace] editor save lost the race to an external write: workspace=%s", ws.ID)
		return nil
	}
	logger.Infof(ctx, "[DocumentWorkspace] editor save stored: workspace=%s final=%t size=%d save_count=%d",
		ws.ID, final, ws.FileSize, ws.SaveCount)
	if final {
		s.attachToLatestAnswer(ctx, ws, ref, data)
	}
	return nil
}

// attachToLatestAnswer records the final file as an artifact of the
// session's latest assistant message, so it shows up in the artifact list.
// Best-effort: the save itself already succeeded.
func (s *documentWorkspaceService) attachToLatestAnswer(
	ctx context.Context, ws *types.DocumentWorkspace, ref string, data []byte,
) {
	if s.messages == nil {
		return
	}
	messages, err := s.messages.GetRecentMessagesBySession(ctx, ws.SessionID, 20)
	if err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] load messages for artifact failed: %v", err)
		return
	}
	var target *types.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && messages[i].Role == "assistant" {
			target = messages[i]
			break
		}
	}
	if target == nil {
		return
	}
	now := time.Now().UTC()
	sum := sha256.Sum256(data)
	artifact := types.MessageArtifact{
		URL: ref, FileName: ws.FileName, FileType: ".docx", FileSize: int64(len(data)),
		ContentHash: hex.EncodeToString(sum[:]), SourcePath: documentWorkspaceArtifactPath + ws.ID,
		ModTime: now, CreatedAt: now,
	}
	artifacts := make(types.MessageArtifacts, 0, len(target.Artifacts)+1)
	for _, a := range target.Artifacts {
		if a.SourcePath != artifact.SourcePath {
			artifacts = append(artifacts, a)
		}
	}
	target.Artifacts = append(artifacts, artifact)
	if err := s.messages.UpdateMessage(ctx, target); err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] attach artifact to message=%s failed: %v", target.ID, err)
		return
	}
	s.bindOwner(ctx, ref, types.ResourceOwnerMessage, target.ID, types.ResourceRelationArtifact)
}

// waiterFor registers a new waiter for this caller only.
func (s *documentWorkspaceService) waiterFor(id string) *saveWaiter {
	w := &saveWaiter{ch: make(chan struct{})}
	s.waitersMu.Lock()
	s.waiters[id] = append(s.waiters[id], w)
	s.waitersMu.Unlock()
	return w
}

// dropWaiter unregisters one caller's waiter (timeout, cancellation, or no
// callback coming).
func (s *documentWorkspaceService) dropWaiter(id string, w *saveWaiter) {
	s.waitersMu.Lock()
	defer s.waitersMu.Unlock()
	list := s.waiters[id]
	for i, cur := range list {
		if cur == w {
			list = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(s.waiters, id)
	} else {
		s.waiters[id] = list
	}
}

// signal releases every caller waiting on this workspace's save.
func (s *documentWorkspaceService) signal(id string, err error) {
	s.waitersMu.Lock()
	list := s.waiters[id]
	delete(s.waiters, id)
	s.waitersMu.Unlock()
	for _, w := range list {
		w.done(err)
	}
}

// ---------------------------------------------------------------------------
// .doc conversion
// ---------------------------------------------------------------------------

func (s *documentWorkspaceService) convertDocToDocx(
	ctx context.Context, tenantID uint64, wsID, fileName string, data []byte,
) ([]byte, error) {
	if s.cfg.BackendURL == "" {
		return nil, apperrors.NewServiceUnavailableError("ONLYOFFICE_BACKEND_URL (or APP_EXTERNAL_URL) is not configured")
	}
	srcRef, err := s.files.SaveBytes(ctx, data, tenantID, "document_workspace_"+wsID+"_source.doc", true)
	if err != nil {
		return nil, fmt.Errorf("stage .doc for conversion: %w", err)
	}
	// The converter runs synchronously (async=false) and has fetched the
	// staged copy by the time we return; it is never needed again. The
	// catalog-backed DeleteFile also marks the resource deleted.
	defer func() {
		if err := s.files.DeleteFile(ctx, srcRef); err != nil {
			logger.Warnf(ctx, "[DocumentWorkspace] delete staged .doc %s failed: %v", srcRef, err)
		}
	}()
	grant, err := s.catalog.CreateAccessGrant(ctx, srcRef, onlyOfficeDownloadGrantTTL)
	if err != nil {
		return nil, fmt.Errorf("create conversion grant: %w", err)
	}
	body := map[string]interface{}{
		"async":      false,
		"filetype":   "doc",
		"outputtype": "docx",
		"key":        "conv-" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"title":      fileName,
		"url":        s.cfg.BackendURL + "/r/" + grant,
	}
	var res struct {
		EndConvert bool   `json:"endConvert"`
		FileURL    string `json:"fileUrl"`
		FileType   string `json:"fileType"`
		Error      int    `json:"error"`
	}
	if err := s.postDocumentServer(ctx, "/converter", body, &res); err != nil {
		return nil, fmt.Errorf("convert .doc: %w", err)
	}
	if res.Error != 0 {
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("could not convert .doc to .docx (error %d)", res.Error))
	}
	if !res.EndConvert || res.FileURL == "" {
		return nil, fmt.Errorf("convert .doc: conversion did not finish")
	}
	out, err := s.download(ctx, res.FileURL)
	if err != nil {
		return nil, fmt.Errorf("download converted document: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// HTTP + JWT helpers
// ---------------------------------------------------------------------------

// postDocumentServer POSTs a signed JSON body: the body carries a "token"
// field, and the Authorization header carries {"payload": body}, so DS
// accepts it whichever of the two it is configured to read.
func (s *documentWorkspaceService) postDocumentServer(
	ctx context.Context, path string, body map[string]interface{}, out interface{},
) error {
	if s.cfg.InternalURL == "" {
		return fmt.Errorf("ONLYOFFICE_INTERNAL_URL is not configured")
	}
	bodyToken, err := s.sign(jwt.MapClaims(body))
	if err != nil {
		return err
	}
	headerToken, err := s.sign(jwt.MapClaims{"payload": body})
	if err != nil {
		return err
	}
	signed := make(map[string]interface{}, len(body)+1)
	for k, v := range body {
		signed[k] = v
	}
	signed["token"] = bodyToken
	raw, err := json.Marshal(signed)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.InternalURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+headerToken)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("document server %s returned HTTP %d", path, resp.StatusCode)
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode document server %s response: %w", path, err)
	}
	return nil
}

// documentServerFetchURL maps a file URL handed out by the Document Server
// (callback "url", converter "fileUrl") to one this backend can reach. DS
// builds those links from the browser-facing address, so a URL on the
// PublicURL origin is rewritten onto InternalURL. Any host other than the
// public or internal DS host is rejected: the URL is attacker-shaped input
// as far as SSRF is concerned.
func (s *documentWorkspaceService) documentServerFetchURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid document server file url")
	}
	pub, _ := url.Parse(s.cfg.PublicURL)
	internal, _ := url.Parse(s.cfg.InternalURL)
	if pub != nil && internal != nil && internal.Host != "" && sameURLOrigin(u, pub) {
		u.Scheme = internal.Scheme
		u.Host = internal.Host
		if prefix := strings.TrimRight(pub.Path, "/"); prefix != "" && strings.HasPrefix(u.Path, prefix+"/") {
			u.Path = strings.TrimRight(internal.Path, "/") + strings.TrimPrefix(u.Path, prefix)
			u.RawPath = ""
		}
		return u.String(), nil
	}
	for _, allowed := range []*url.URL{pub, internal} {
		if allowed != nil && allowed.Hostname() != "" && strings.EqualFold(u.Hostname(), allowed.Hostname()) {
			return u.String(), nil
		}
	}
	return "", fmt.Errorf("document server file url host %q is not allowed", u.Hostname())
}

func sameURLOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func (s *documentWorkspaceService) download(ctx context.Context, rawURL string) ([]byte, error) {
	fetchURL, err := s.documentServerFetchURL(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := readCapped(resp.Body, maxDocumentWorkspaceBytes())
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty file")
	}
	return data, nil
}

func (s *documentWorkspaceService) sign(claims jwt.MapClaims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

// parseDocumentServerJWT verifies an HS256 token signed with the DS secret.
func (s *documentWorkspaceService) parseDocumentServerJWT(raw string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(strings.TrimSpace(raw), func(token *jwt.Token) (interface{}, error) {
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || token == nil || !token.Valid {
		return nil, fmt.Errorf("invalid token: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// verifyCallbackBody returns the callback as signed by the Document Server.
// The signed copy is authoritative: unsigned body fields are never trusted.
func (s *documentWorkspaceService) verifyCallbackBody(
	authorization string, payload *types.OnlyOfficeCallback,
) (*types.OnlyOfficeCallback, error) {
	var signed interface{}
	if bearer, ok := strings.CutPrefix(strings.TrimSpace(authorization), "Bearer "); ok && strings.TrimSpace(bearer) != "" {
		claims, err := s.parseDocumentServerJWT(bearer)
		if err != nil {
			return nil, err
		}
		inner, ok := claims["payload"]
		if !ok {
			return nil, errors.New("authorization token has no payload")
		}
		signed = inner
	} else if payload != nil && strings.TrimSpace(payload.Token) != "" {
		claims, err := s.parseDocumentServerJWT(payload.Token)
		if err != nil {
			return nil, err
		}
		signed = map[string]interface{}(claims)
	} else {
		return nil, errors.New("callback is not signed")
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		return nil, err
	}
	var cb types.OnlyOfficeCallback
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, fmt.Errorf("decode signed callback: %w", err)
	}
	return &cb, nil
}

type documentCallbackTicket struct {
	workspaceID string
	tenantID    uint64
	sessionID   string
}

// callbackTicketKey derives the ticket signing key from the DS secret, so a
// ticket never verifies as a DS token (or vice versa) and survives restarts.
func (s *documentWorkspaceService) callbackTicketKey() []byte {
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	mac.Write([]byte("werag/onlyoffice-callback-ticket/v1"))
	return mac.Sum(nil)
}

func (s *documentWorkspaceService) issueCallbackTicket(ws *types.DocumentWorkspace) (string, error) {
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"workspace_id": ws.ID,
		"tenant_id":    strconv.FormatUint(ws.TenantID, 10),
		"session_id":   ws.SessionID,
		"type":         onlyOfficeCallbackTicketType,
		"iat":          now.Unix(),
		"exp":          now.Add(onlyOfficeCallbackTicketTTL).Unix(),
	}).SignedString(s.callbackTicketKey())
}

func (s *documentWorkspaceService) parseCallbackTicket(raw string) (*documentCallbackTicket, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("missing ticket")
	}
	token, err := jwt.Parse(raw, func(token *jwt.Token) (interface{}, error) {
		return s.callbackTicketKey(), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || token == nil || !token.Valid {
		return nil, fmt.Errorf("invalid or expired ticket: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid ticket claims")
	}
	if t, _ := claims["type"].(string); t != onlyOfficeCallbackTicketType {
		return nil, errors.New("not a callback ticket")
	}
	wsID, _ := claims["workspace_id"].(string)
	sessionID, _ := claims["session_id"].(string)
	tenantRaw, _ := claims["tenant_id"].(string)
	tenantID, _ := strconv.ParseUint(tenantRaw, 10, 64)
	if wsID == "" || sessionID == "" || tenantID == 0 {
		return nil, errors.New("incomplete ticket claims")
	}
	return &documentCallbackTicket{workspaceID: wsID, tenantID: tenantID, sessionID: sessionID}, nil
}

// parseEditorKeyRevision splits "<workspaceID>-<revision>".
func parseEditorKeyRevision(workspaceID, key string) (int, bool) {
	rest, ok := strings.CutPrefix(key, workspaceID+"-")
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func (s *documentWorkspaceService) bind(ctx context.Context, ref, workspaceID, relation string) {
	s.bindOwner(ctx, ref, types.ResourceOwnerDocumentWorkspace, workspaceID, relation)
}

func (s *documentWorkspaceService) bindOwner(ctx context.Context, ref, ownerType, ownerID, relation string) {
	if s.catalog == nil {
		return
	}
	if _, ok := types.ParseResourcePath(ref); !ok {
		return
	}
	if err := s.catalog.Bind(ctx, ref, ownerType, ownerID, relation); err != nil {
		logger.Warnf(ctx, "[DocumentWorkspace] bind resource failed: owner=%s/%s err=%v", ownerType, ownerID, err)
	}
}

func documentWorkspaceStorageName(workspaceID string) string {
	return "document_workspace_" + workspaceID + "_" + uuid.NewString()[:8] + ".docx"
}

func readCapped(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds size limit of %d bytes", limit)
	}
	return data, nil
}
