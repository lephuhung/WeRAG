package session

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/gin-gonic/gin"
)

// ContinueSession godoc
// @Summary      Continue a session in a new one
// @Description  Opens a new session seeded with the session's latest context summary
// @Description  and excerpts of its newest turns: what the "open a new session" button
// @Description  of a session_limit refusal calls.
// @Tags         会话
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
