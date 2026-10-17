package session

import (
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/gin-gonic/gin"
)

// ContinueSession godoc
// @Summary      Continue a session in a new one
// @Description  Opens a new session seeded with the session's latest context summary
// @Description  and excerpts of its newest turns: what the "open a new session" button
// @Description  of a session_limit refusal calls.
// @Tags         Cuộc trò chuyện
// @Produce      json
// @Param        session_id  path      string  true  "Session ID"
// @Success      200         {object}  map[string]interface{}
// @Failure      404         {object}  errors.AppError
// @Security     Bearer
// @Router       /sessions/{session_id}/continue [post]
func (h *Handler) ContinueSession(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		_ = c.Error(errors.NewBadRequestError("session ID is required"))
		return
	}
	// owner scope: only the session's owner may carry it on
	old, err := h.sessionService.GetOwnedSession(ctx, sessionID)
	if err != nil || old == nil {
		_ = c.Error(errors.NewNotFoundError("session not found"))
		return
	}
	next, err := h.sessionService.ContinueSession(ctx, old)
	if err != nil {
		logger.Errorf(ctx, "continue session %s failed: %v", sessionID, err)
		_ = c.Error(errors.NewInternalServerError("continue session failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": next})
}

// subChatRow is one chat of a document chat's sub-chat list.
type subChatRow struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	IsRoot    bool   `json:"is_root"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListSubChats godoc
// @Summary      List the sub-chats of a document chat
// @Description  The document-assistant chat the session belongs to and its sub-chats
// @Description  ("chat nhỏ"), oldest first; they all work on the root chat's documents.
// @Tags         Cuộc trò chuyện
// @Produce      json
// @Param        session_id  path      string  true  "Session ID"
// @Success      200         {object}  map[string]interface{}
// @Failure      404         {object}  errors.AppError
// @Security     Bearer
// @Router       /sessions/{session_id}/sub-chats [get]
func (h *Handler) ListSubChats(c *gin.Context) {
	ctx := c.Request.Context()
	// GET session routes name the wildcard :id (one name per gin tree)
	sessionID := strings.TrimSpace(c.Param("id"))
	sess, err := h.sessionService.GetOwnedSession(ctx, sessionID)
	if err != nil || sess == nil {
		_ = c.Error(errors.NewNotFoundError("session not found"))
		return
	}
	root, chats, err := h.sessionService.ListSubChats(ctx, sess)
	if err != nil {
		logger.Errorf(ctx, "list sub-chats of %s failed: %v", sessionID, err)
		_ = c.Error(errors.NewInternalServerError("list sub-chats failed"))
		return
	}
	rows := make([]subChatRow, 0, len(chats))
	for _, ch := range chats {
		rows = append(rows, subChatRow{
			ID:        ch.ID,
			Title:     ch.Title,
			IsRoot:    ch.ID == root,
			CreatedAt: ch.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: ch.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"root_id": root, "chats": rows}})
}

// OpenSubChat godoc
// @Summary      Open a new sub-chat of a document chat
// @Description  A new session on the same documents as the session's document chat,
// @Description  seeded with the session's latest context summary (like /continue).
// @Tags         Cuộc trò chuyện
// @Produce      json
// @Param        session_id  path      string  true  "Session ID"
// @Success      200         {object}  map[string]interface{}
// @Failure      404         {object}  errors.AppError
// @Security     Bearer
// @Router       /sessions/{session_id}/sub-chats [post]
func (h *Handler) OpenSubChat(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := strings.TrimSpace(c.Param("session_id"))
	from, err := h.sessionService.GetOwnedSession(ctx, sessionID)
	if err != nil || from == nil {
		_ = c.Error(errors.NewNotFoundError("session not found"))
		return
	}
	next, err := h.sessionService.OpenSubChat(ctx, from)
	if err != nil {
		logger.Errorf(ctx, "open sub-chat of %s failed: %v", sessionID, err)
		_ = c.Error(errors.NewInternalServerError("open sub-chat failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": next})
}
