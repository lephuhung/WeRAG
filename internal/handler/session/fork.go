package session

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// sessionForker is the fork surface the handler needs. Declaring it here
// rather than depending on *service.SessionForkService keeps the handler
// testable with a stub.
type sessionForker interface {
	Fork(
		ctx context.Context,
		tenantID uint64,
		userID, sourceSessionID, messageID, title string,
	) (*service.ForkResult, error)
}

// ForkSessionRequest is the fork endpoint's body.
type ForkSessionRequest struct {
	// MessageID is the message to branch at. A user message copies history
	// strictly before it (the client prefills that question). An assistant
	// message copies history through that answer so the branch continues after it.
	MessageID string `json:"message_id" binding:"required"`
	// Title is optional. Empty falls back to the source title plus a suffix.
	Title string `json:"title"`
}

// ForkSession godoc
// @Summary      Rẽ nhánh cuộc trò chuyện
// @Description  Tạo cuộc trò chuyện mới rẽ nhánh từ một tin nhắn người dùng hoặc trợ lý. Tin nhắn người dùng: sao chép lịch sử trước đó và điền sẵn câu hỏi; tin nhắn trợ lý: sao chép lịch sử gồm cả câu trả lời đó và tiếp tục từ trạng thái sandbox của lượt đó.
// @Tags         Cuộc trò chuyện
// @Accept       json
// @Produce      json
// @Param        session_id  path      string              true  "ID cuộc trò chuyện nguồn"
// @Param        request     body      ForkSessionRequest  true  "Yêu cầu rẽ nhánh"
// @Success      200         {object}  map[string]interface{}  "Cuộc trò chuyện mới"
// @Failure      400         {object}  errors.AppError         "Tham số không hợp lệ / vai trò tại điểm rẽ nhánh không được hỗ trợ"
// @Failure      404         {object}  errors.AppError         "Không tìm thấy cuộc trò chuyện hoặc tin nhắn"
// @Failure      409         {object}  errors.AppError         "Cuộc trò chuyện nguồn đang sinh câu trả lời"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /sessions/{session_id}/fork [post]
func (h *Handler) ForkSession(c *gin.Context) {
	ctx := c.Request.Context()

	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(c.Param("id"))
	}
	if sessionID == "" {
		_ = c.Error(errors.NewBadRequestError("session ID is required"))
		return
	}

	var req ForkSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequestError("message_id is required"))
		return
	}
	if strings.TrimSpace(req.MessageID) == "" {
		_ = c.Error(errors.NewBadRequestError("message_id is required"))
		return
	}

	if h.forkService == nil {
		_ = c.Error(errors.NewBadRequestError("session fork is not available"))
		return
	}

	tenantID, _ := types.TenantIDFromContext(ctx)
	userID := types.SessionOwnerIDFromContext(ctx)

	result, err := h.forkService.Fork(
		ctx, tenantID, userID, sessionID,
		strings.TrimSpace(req.MessageID), strings.TrimSpace(req.Title),
	)
	if err != nil {
		// A busy source is a retryable, user-actionable state, not a fault:
		// snapshotting would pause the sandbox and interrupt the running turn.
		if stderrors.Is(err, service.ErrForkSourceBusy) {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "source session has an active turn",
				"code":    "FORK_SOURCE_BUSY",
			})
			return
		}
		if stderrors.Is(err, service.ErrForkSessionNotFound) {
			_ = c.Error(errors.NewNotFoundError("session not found"))
			return
		}
		if stderrors.Is(err, service.ErrForkMessageNotFound) {
			_ = c.Error(errors.NewNotFoundError("message not found"))
			return
		}
		if stderrors.Is(err, service.ErrForkMessageNotUser) {
			_ = c.Error(errors.NewBadRequestError("fork point must be a user or assistant message"))
			return
		}
		logger.Errorf(ctx, "fork session %s failed: %v", sessionID, err)
		_ = c.Error(errors.NewInternalServerError("fork session failed"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
