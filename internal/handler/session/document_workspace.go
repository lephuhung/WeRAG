package session

import (
	stderrors "errors"
	"net/http"
	"net/url"
	"strings"

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
	// frontendBaseURL (FRONTEND_BASE_URL) is the editor host origin fallback
	// when the request carries no Origin header.
	frontendBaseURL string
}

// NewDocumentWorkspaceHandler wires the document workspace routes.
func NewDocumentWorkspaceHandler(
	sessionService interfaces.SessionService,
	workspaces interfaces.DocumentWorkspaceService,
	cfg *config.Config,
) *DocumentWorkspaceHandler {
	h := &DocumentWorkspaceHandler{sessionService: sessionService, workspaces: workspaces}
	if cfg != nil {
		h.frontendBaseURL = cfg.FrontendBaseURL
	}
	return h
}

// CreateDocumentWorkspaceRequest opens a session attachment in the editor.
type CreateDocumentWorkspaceRequest struct {
	AttachmentID string `json:"attachment_id" binding:"required"`
}

// CreateDocumentWorkspace copies a .docx/.doc attachment into the session's
// document workspace. POST /sessions/:session_id/document
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
	view, err := h.view(c, ws)
	if err != nil {
		h.fail(c, err, "Failed to build editor config")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": view})
}

// GetDocumentWorkspace returns the workspace and a signed editor config.
// GET /sessions/:id/document
func (h *DocumentWorkspaceHandler) GetDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	ws, err := h.workspaces.GetBySession(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID)
	if err != nil {
		h.fail(c, err, "Failed to load document")
		return
	}
	view, err := h.view(c, ws)
	if err != nil {
		h.fail(c, err, "Failed to build editor config")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// ForceSaveDocumentWorkspace asks the Document Server to flush unsaved edits.
// POST /sessions/:session_id/document/forcesave
func (h *DocumentWorkspaceHandler) ForceSaveDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if err := h.workspaces.ForceSave(ctx, tenantID, sessionID); err != nil {
		h.fail(c, err, "Failed to save document")
		return
	}
	ws, err := h.workspaces.GetBySession(ctx, tenantID, sessionID)
	if err != nil {
		h.fail(c, err, "Failed to load document")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"revision": ws.Revision}})
}

// DownloadDocumentWorkspace streams the latest version of the document.
// GET /sessions/:id/document/download
func (h *DocumentWorkspaceHandler) DownloadDocumentWorkspace(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	reader, ws, err := h.workspaces.OpenCurrent(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID)
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
	return h.workspaces.View(ctx, ws, userID, userName, editorLang(c.GetHeader("Accept-Language")))
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
// carry no Origin header, so fall back to FRONTEND_BASE_URL, then Referer.
func (h *DocumentWorkspaceHandler) editorHostOrigin(c *gin.Context) string {
	for _, candidate := range []string{c.GetHeader("Origin"), h.frontendBaseURL, c.GetHeader("Referer")} {
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
