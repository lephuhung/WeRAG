package session

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/filetransport"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// UploadTemporaryDocument accepts one multipart file and immediately returns
// a session-scoped document ID. Parsing continues in the document worker.
func (h *Handler) UploadTemporaryDocument(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := c.Param("session_id")
	// Uploading attaches content to the session, so use the strict owner scope:
	// a tenant admin may read an API-key session but must not add attachments.
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	maxBytes := secutils.GetMaxFileSizeMB()*1024*1024 + 1024*1024
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.Error(apperrors.NewBadRequestError(fmt.Sprintf("invalid attachment upload: %v", err)))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.Error(apperrors.NewBadRequestError("failed to open attachment"))
		return
	}
	defer file.Close()

	agent, resourceTenantID := h.resolveAgent(ctx, c, c.PostForm("agent_id"))
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileHeader.Filename)), ".")
	options := types.TemporaryDocumentCreateOptions{ParserEngine: strings.TrimSpace(c.PostForm("parser_engine"))}
	if agent != nil {
		options.ResourceTenantID = resourceTenantID
		// A shared agent parses in its owner's workspace, with the parser
		// engines (and credentials) configured there: only the agent's own
		// rules may pick one, not the caller.
		if resourceTenantID != 0 && resourceTenantID != c.GetUint64(types.TenantIDContextKey.String()) {
			options.ParserEngine = ""
		}
		if len(agent.Config.SupportedFileTypes) > 0 && !containsFileType(agent.Config.SupportedFileTypes, ext) {
			c.Error(apperrors.NewBadRequestError("file type is not supported by this agent"))
			return
		}
		if isAudioExtension(ext) {
			if !agent.Config.AudioUploadEnabled || agent.Config.ASRModelID == "" {
				c.Error(apperrors.NewBadRequestError("audio upload is not enabled or no ASR model is configured"))
				return
			}
			options.ASRModelID = agent.Config.ASRModelID
		}
		// Resolve the parser engine from the agent's chat rules when the caller
		// did not pass an explicit engine. Tenant-level rules remain the final
		// fallback and are applied in the parse worker.
		if options.ParserEngine == "" || options.ParserEngine == "auto" {
			if engine := agent.Config.ResolveChatParserEngine(ext); engine != "" {
				options.ParserEngine = engine
			}
		}
		// Image understanding (caption/OCR) uses the agent's VLM model. Images
		// always benefit; scanned/image-only documents only OCR when the agent
		// enabled AttachmentImageUnderstanding (see parse worker threshold gate).
		if agent.Config.ImageUploadEnabled && agent.Config.VLMModelID != "" {
			options.VLMModelID = agent.Config.VLMModelID
			options.ImageUnderstanding = agent.Config.AttachmentImageUnderstanding
			options.OCRMaxPages = agent.Config.AttachmentOCRMaxPages
		}
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	// In the document assistant a chat upload becomes a source document of
	// the session; check the source limit before storing and parsing it.
	asSource := h.uploadBecomesSource(agent, ext, c.PostForm("document_role"))
	if asSource {
		if docs, err := h.documentWorkspaces.List(ctx, tenantID, sessionID); err == nil {
			if capErr := service.SourceCapacityError(docs); capErr != nil {
				c.Error(capErr)
				return
			}
		}
	}
	document, err := h.temporaryDocuments.Create(
		ctx, tenantID, sessionID,
		fileHeader.Filename, fileHeader.Header.Get("Content-Type"), fileHeader.Size, file, options,
	)
	if err != nil {
		c.Error(apperrors.NewBadRequestError(err.Error()))
		return
	}
	resp := gin.H{"success": true, "data": document}
	if asSource && document != nil && document.Status != types.TemporaryDocumentStatusFailed {
		userID, _ := types.UserIDFromContext(ctx)
		ws, err := h.documentWorkspaces.CreateSourceFromAttachment(ctx, tenantID, sessionID, userID, document.ID)
		switch {
		case err == nil:
			resp["document"] = &types.DocumentWorkspaceView{DocumentWorkspace: ws, EditorKey: ws.EditorKey(), Handle: ws.Handle()}
		case isConflict(err):
			// another upload took the last source slot meanwhile
			_ = h.temporaryDocuments.Delete(ctx, tenantID, sessionID, document.ID)
			c.Error(err)
			return
		default:
			// the file still works as a plain attachment of this turn
			logger.Warnf(ctx, "Upload %s of session %s not recorded as a source document: %v",
				document.ID, sessionID, err)
		}
	}
	c.JSON(http.StatusAccepted, resp)
}

// uploadBecomesSource reports whether a chat upload is recorded as a source
// document: in a document-assistant session (no Document Server needed:
// a source is only looked up), for any parsed document (not an image sent for vision, not audio).
// The editor pane's own uploads pass document_role=target: they are opened
// as an editor tab right after the upload.
func (h *Handler) uploadBecomesSource(agent *types.CustomAgent, ext, role string) bool {
	if agent == nil || agent.ID != types.BuiltinDocumentAssistantID ||
		h.documentWorkspaces == nil || !h.documentWorkspaces.DocumentsEnabled() {
		return false
	}
	if strings.TrimSpace(role) == types.DocumentWorkspaceRoleTarget {
		return false
	}
	return !docparser.IsImageFormat(ext) && !isAudioExtension(ext)
}

func isConflict(err error) bool {
	appErr, ok := apperrors.IsAppError(err)
	return ok && appErr.Code == apperrors.ErrConflict
}

func (h *Handler) ListTemporaryDocuments(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	documents, err := h.temporaryDocuments.List(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID)
	if err != nil {
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": documents})
}

func (h *Handler) GetTemporaryDocument(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	document, err := h.temporaryDocuments.Get(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, c.Param("attachment_id"))
	if err != nil {
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	if document == nil {
		c.Error(apperrors.NewNotFoundError("Attachment not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": document})
}

func (h *Handler) PreviewTemporaryDocument(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	attachmentID := secutils.SanitizeForLog(c.Param("attachment_id"))
	if attachmentID == "" {
		c.Error(apperrors.NewBadRequestError("Attachment ID cannot be empty"))
		return
	}
	file, filename, err := h.temporaryDocuments.OpenFile(
		ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, attachmentID,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			c.Error(apperrors.NewNotFoundError("Attachment not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(apperrors.NewInternalServerError("Failed to retrieve attachment").WithDetails(err.Error()))
		return
	}
	if err := filetransport.Serve(c.Writer, c.Request, file, filetransport.Options{
		Filename: filename, CacheControl: "private, no-store",
	}); err != nil {
		logger.Errorf(ctx, "Failed to stream attachment preview: %v", err)
	}
}

func (h *Handler) DeleteTemporaryDocument(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	// Deleting mutates the session's attachments, so use the strict owner scope:
	// a tenant admin may read an API-key session but must not remove attachments.
	if _, err := h.sessionService.GetOwnedSession(ctx, sessionID); err != nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	if err := h.temporaryDocuments.Delete(ctx, c.GetUint64(types.TenantIDContextKey.String()), sessionID, c.Param("attachment_id")); err != nil {
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	c.Status(http.StatusNoContent)
}

func sessionIDParam(c *gin.Context) string {
	if value := c.Param("session_id"); value != "" {
		return value
	}
	return c.Param("id")
}

func containsFileType(supported []string, ext string) bool {
	for _, item := range supported {
		if strings.TrimPrefix(strings.ToLower(strings.TrimSpace(item)), ".") == ext {
			return true
		}
	}
	return false
}

func isAudioExtension(ext string) bool {
	switch ext {
	case "mp3", "wav", "m4a", "flac", "ogg", "aac":
		return true
	default:
		return false
	}
}
