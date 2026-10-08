package session

import (
	stderrors "errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/filetransport"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

const docxContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// maxOnlyOfficeCallbackBytes bounds the callback JSON. Real callbacks are a
// few KB (key, url, users, actions, token).
const maxOnlyOfficeCallbackBytes = 1 << 20

// DocumentWorkspaceHandler serves the document-assistant workspace of a chat
// session (the editable .docx shown in the embedded ONLYOFFICE editor) and
// the Document Server's save callback.
type DocumentWorkspaceHandler struct {
	sessionService interfaces.SessionService
	workspaces     interfaces.DocumentWorkspaceService
	// precheck runs the format check of an opened document in the
	// background; nil disables it.
	precheck *service.DocumentFormatPrecheck
	// frontendBaseURL (FRONTEND_BASE_URL) is the editor host origin fallback
	// when the request carries no Origin header.
	frontendBaseURL string
}

// NewDocumentWorkspaceHandler wires the document workspace routes.
func NewDocumentWorkspaceHandler(
	sessionService interfaces.SessionService,
	workspaces interfaces.DocumentWorkspaceService,
	cfg *config.Config,
	precheck *service.DocumentFormatPrecheck,
) *DocumentWorkspaceHandler {
	h := &DocumentWorkspaceHandler{sessionService: sessionService, workspaces: workspaces, precheck: precheck}
	if cfg != nil {
		h.frontendBaseURL = cfg.FrontendBaseURL
	}
	return h
}

// CreateDocumentWorkspaceRequest opens a session attachment in the editor.
type CreateDocumentWorkspaceRequest struct {
	AttachmentID string `json:"attachment_id" binding:"required"`
}

// documentIDParam is the :doc_id of the per-document routes; "" on the
// legacy /document routes, which act on the session's active document.
func documentIDParam(c *gin.Context) string {
	return strings.TrimSpace(c.Param("doc_id"))
}

// CreateDocumentWorkspace opens a .docx/.doc attachment as a new editable
// document of the session (a new editor tab), or shows the one already
// opened from it. POST /sessions/:session_id/documents (and /document)
func (h *DocumentWorkspaceHandler) CreateDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	if !h.workspaces.Enabled() {
		c.Error(apperrors.NewServiceUnavailableError("document editor is not configured"))
		return
	}
	sessionID := sessionIDParam(c)
	// Creating mutates the session, so use the strict owner scope.
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	var req CreateDocumentWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("attachment_id is required"))
		return
	}
	userID, _ := types.UserIDFromContext(ctx)
	ws, err := h.workspaces.CreateFromAttachment(
		ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, userID, strings.TrimSpace(req.AttachmentID),
	)
	if err != nil {
		h.fail(c, err, "Failed to open document")
		return
	}
	h.precheck.Start(ctx, ws.TenantID, sessionID, ws.ID)
	view, err := h.view(c, ws)
	if err != nil {
		h.fail(c, err, "Failed to build editor config")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": view})
}

// ListDocumentWorkspaces returns the session's documents in tab order,
// without editor configs (each tab loads its own), with the background
// format check of each and the active document's ID.
// GET /sessions/:id/documents
func (h *DocumentWorkspaceHandler) ListDocumentWorkspaces(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	docs, err := h.workspaces.List(ctx, tenantID, sessionID)
	if err != nil {
		h.fail(c, err, "Failed to list documents")
		return
	}
	out := make([]*types.DocumentWorkspaceView, 0, len(docs))
	for _, ws := range docs {
		out = append(out, &types.DocumentWorkspaceView{
			DocumentWorkspace: ws, EditorKey: ws.EditorKey(), Handle: ws.Handle(),
			FormatCheck: h.precheck.Status(ctx, ws.ID),
		})
	}
	activeID := ""
	if len(docs) > 0 {
		if active, err := h.workspaces.GetBySession(ctx, tenantID, sessionID); err == nil && active != nil {
			activeID = active.ID
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"documents": out, "active_id": activeID, "max_documents": types.MaxDocumentWorkspacesPerSession,
		"max_file_bytes": types.MaxDocumentWorkspaceFileBytes, "max_media_bytes": types.MaxDocumentWorkspaceMediaBytes,
	}})
}

// ActivateDocumentWorkspace records the tab the user switched to: the agent
// treats it as the document a request without @ is about.
// POST /sessions/:session_id/documents/:doc_id/activate
func (h *DocumentWorkspaceHandler) ActivateDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	ws, err := h.workspaces.Activate(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, documentIDParam(c))
	if err != nil {
		h.fail(c, err, "Failed to switch document")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": ws.ID}})
}

// DeleteDocumentWorkspace closes a document's tab (after a snapshot of its
// current state). DELETE /sessions/:id/documents/:doc_id
func (h *DocumentWorkspaceHandler) DeleteDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	if err := h.workspaces.Remove(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, documentIDParam(c)); err != nil {
		h.fail(c, err, "Failed to close document")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetDocumentWorkspace returns one document (the active one on the legacy
// route) and a signed editor config.
// GET /sessions/:id/documents/:doc_id (and /document)
func (h *DocumentWorkspaceHandler) GetDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	ws, err := h.workspaces.Get(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, documentIDParam(c))
	if err != nil {
		h.fail(c, err, "Failed to load document")
		return
	}
	if ws.Status == types.DocumentWorkspaceStatusOpen {
		// a document opened before a server restart has no check yet
		h.precheck.Start(ctx, ws.TenantID, sessionID, ws.ID)
		// a save since the check: keep it or check again (format changed)
		h.precheck.Refresh(ctx, ws)
	}
	view, err := h.view(c, ws)
	if err != nil {
		h.fail(c, err, "Failed to build editor config")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// ForceSaveDocumentWorkspace asks the Document Server to flush unsaved edits.
// POST /sessions/:session_id/documents/:doc_id/forcesave (and /document/forcesave)
func (h *DocumentWorkspaceHandler) ForceSaveDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if err := h.workspaces.ForceSave(ctx, tenantID, sessionID, documentIDParam(c)); err != nil {
		h.fail(c, err, "Failed to save document")
		return
	}
	ws, err := h.workspaces.Get(ctx, tenantID, sessionID, documentIDParam(c))
	if err != nil {
		h.fail(c, err, "Failed to load document")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"revision": ws.Revision}})
}

// DownloadDocumentWorkspace streams the latest version of the document.
// GET /sessions/:id/documents/:doc_id/download (and /document/download)
func (h *DocumentWorkspaceHandler) DownloadDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	reader, ws, err := h.workspaces.OpenCurrent(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, documentIDParam(c))
	if err != nil {
		h.fail(c, err, "Failed to open document")
		return
	}
	if err := filetransport.Serve(c.Writer, c.Request, reader, filetransport.Options{
		Filename: ws.FileName, Download: true, ContentType: docxContentType,
		CacheControl: "private, no-store", Size: ws.FileSize,
	}); err != nil {
		logger.Errorf(ctx, "Failed to stream document workspace: %v", err)
	}
}

// DocumentRevisionResponse is one entry of the snapshot timeline.
type DocumentRevisionResponse struct {
	Seq       int       `json:"seq"`
	Label     string    `json:"label"`
	Source    string    `json:"source"`
	FileSize  int64     `json:"file_size"`
	CreatedAt time.Time `json:"created_at"`
}

// ListDocumentRevisions returns the snapshot timeline, newest first.
// GET /sessions/:id/documents/:doc_id/revisions (and /document/revisions)
func (h *DocumentWorkspaceHandler) ListDocumentRevisions(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	revisions, err := h.workspaces.ListRevisions(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, documentIDParam(c))
	if err != nil {
		h.fail(c, err, "Failed to list document revisions")
		return
	}
	out := make([]DocumentRevisionResponse, 0, len(revisions))
	for _, rev := range revisions {
		out = append(out, DocumentRevisionResponse{
			Seq: rev.Seq, Label: rev.Label, Source: rev.Source, FileSize: rev.FileSize, CreatedAt: rev.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// RestoreDocumentRevision makes a snapshot the current version; the new
// editor_key makes the open editor reload.
// POST /sessions/:session_id/documents/:doc_id/revisions/:seq/restore (and /document/…)
func (h *DocumentWorkspaceHandler) RestoreDocumentRevision(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	seq, err := strconv.Atoi(c.Param("seq"))
	if err != nil || seq <= 0 {
		c.Error(apperrors.NewBadRequestError("invalid revision number"))
		return
	}
	ws, err := h.workspaces.Restore(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, documentIDParam(c), seq)
	if err != nil {
		h.fail(c, err, "Failed to restore document revision")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"revision": ws.Revision, "editor_key": ws.EditorKey()}})
}

// SnapshotDocumentWorkspaceRequest is a manual snapshot from the UI.
type SnapshotDocumentWorkspaceRequest struct {
	Label string `json:"label"`
}

// SnapshotDocumentWorkspace saves the editor and records a manual snapshot.
// POST /sessions/:session_id/documents/:doc_id/snapshot (and /document/snapshot)
func (h *DocumentWorkspaceHandler) SnapshotDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	var req SnapshotDocumentWorkspaceRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.Error(apperrors.NewBadRequestError("invalid snapshot request"))
			return
		}
	}
	rev, err := h.workspaces.Snapshot(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, documentIDParam(c),
		req.Label, types.DocumentRevisionSourceManual, 0)
	if err != nil {
		h.fail(c, err, "Failed to snapshot document")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"seq": rev.Seq}})
}

// OnlyOfficeCallback receives Document Server save callbacks. It is public
// (registered before the auth middleware): the :ticket path segment is a JWT
// we issued for one workspace, and the body is verified against the DS JWT
// secret. DS expects {"error":0}; anything else makes it retry or report.
// POST /onlyoffice/callback/:ticket
func (h *DocumentWorkspaceHandler) OnlyOfficeCallback(c *gin.Context) {
	ctx := c.Request.Context()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOnlyOfficeCallbackBytes)
	var payload types.OnlyOfficeCallback
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": 1})
		return
	}
	err := h.workspaces.HandleCallback(ctx, c.Param("ticket"), c.GetHeader("Authorization"), &payload)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"error": 0})
	case stderrors.Is(err, service.ErrOnlyOfficeCallbackUnauthorized):
		c.JSON(http.StatusForbidden, gin.H{"error": 1})
	default:
		logger.Errorf(ctx, "[DocumentWorkspace] callback failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": 1})
	}
}

func (h *DocumentWorkspaceHandler) view(c *gin.Context, ws *types.DocumentWorkspace) (*types.DocumentWorkspaceView, error) {
	ctx := service.WithDocumentEditorOrigin(c.Request.Context(), h.editorHostOrigin(c))
	userID, _ := types.UserIDFromContext(ctx)
	userName := ""
	if user, ok := ctx.Value(types.UserContextKey).(*types.User); ok && user != nil {
		if userID == "" {
			userID = user.ID
		}
		userName = user.Username
	}
	view, err := h.workspaces.View(ctx, ws, userID, userName, editorLang(c.GetHeader("Accept-Language")))
	if err == nil && view != nil {
		view.Handle = ws.Handle()
		view.FormatCheck = h.precheck.Status(ctx, ws.ID)
	}
	return view, err
}

func (h *DocumentWorkspaceHandler) fail(c *gin.Context, err error, message string) {
	if appErr, ok := apperrors.IsAppError(err); ok {
		c.Error(appErr)
		return
	}
	logger.ErrorWithFields(c.Request.Context(), err, nil)
	c.Error(apperrors.NewInternalServerError(message).WithDetails(err.Error()))
}

// editorHostOrigin is the origin of the page that mounts the editor, handed
// to the assistant plugin as hostOrigin. Same-origin GETs (Next.js rewrite)
// carry no Origin header, so fall back to the Referer, then FRONTEND_BASE_URL.
func (h *DocumentWorkspaceHandler) editorHostOrigin(c *gin.Context) string {
	// The Referer names the page that actually embeds the editor (it may be
	// reached by IP, hostname or localhost), so it ranks above the configured
	// base URL: postMessage silently drops a mismatched target origin.
	for _, candidate := range []string{c.GetHeader("Origin"), c.GetHeader("Referer"), h.frontendBaseURL, os.Getenv("FRONTEND_BASE_URL")} {
		if origin := urlOrigin(candidate); origin != "" {
			return origin
		}
	}
	return ""
}

// urlOrigin returns scheme://host[:port] of an absolute http(s) URL, or "".
func urlOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// editorLang maps Accept-Language to the editor UI language: "en" when the
// browser's first preference is English, "vi" otherwise.
func editorLang(acceptLanguage string) string {
	first := strings.TrimSpace(strings.SplitN(acceptLanguage, ",", 2)[0])
	if strings.HasPrefix(strings.ToLower(first), "en") {
		return "en"
	}
	return "vi"
}
