package session

import (
	"net/http"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/gin-gonic/gin"
)

// GetContextUsage godoc
// @Summary      Session context usage
// @Description  How full the history of the session's next turn is, next to the plan's caps (chat UI context indicator)
// @Tags         Cuộc trò chuyện
// @Produce      json
// @Param        id   path      string  true  "Session ID"
// @Success      200  {object}  github_com_Tencent_WeKnora_internal_types.SessionContextUsage
// @Failure      404  {object}  github_com_Tencent_WeKnora_internal_errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /sessions/{id}/context-usage [get]
func (h *Handler) GetContextUsage(c *gin.Context) {
	ctx := c.Request.Context()
	sessionID := sessionIDParam(c)
	// Another user's session, or another workspace's, is not found.
	session, err := h.sessionService.GetOwnedSession(ctx, sessionID)
	if err != nil || session == nil || h.contextUsage == nil {
		c.Error(apperrors.NewNotFoundError("Session not found"))
		return
	}
	usage, err := h.contextUsage.Usage(ctx, session)
	if err != nil {
		logger.Warnf(ctx, "context usage of session %s failed: %v", sessionID, err)
		c.Error(apperrors.NewInternalServerError("Failed to read the context usage"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": usage})
}
