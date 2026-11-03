package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// EmailHandler serves the SuperAdmin side of outgoing email: the SMTP
// account itself is edited through the email.* system settings.
type EmailHandler struct {
	notifier *service.EmailNotifier
}

// NewEmailHandler creates the handler.
func NewEmailHandler(notifier *service.EmailNotifier) *EmailHandler {
	return &EmailHandler{notifier: notifier}
}

// emailTestResponse is the result of a test send: sent, or the SMTP error.
type emailTestResponse struct {
	Sent  bool   `json:"sent"`
	To    string `json:"to"`
	Error string `json:"error,omitempty"`
}

// SendTest godoc
// @Summary      Gửi email thử
// @Description  Gửi một email thử tới địa chỉ email của SuperAdmin đang đăng nhập bằng cấu hình SMTP đã lưu. Lỗi SMTP được trả về trong trường error.
// @Tags         system
// @Produce      json
// @Success      200  {object}  emailTestResponse
// @Router       /system/admin/email/test [post]
func (h *EmailHandler) SendTest(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	user, _ := ctx.Value(types.UserContextKey).(*types.User)
	if user == nil {
		c.Error(apperrors.NewUnauthorizedError("sign in as a SuperAdmin"))
		return
	}
	resp := emailTestResponse{To: user.Email}
	if err := h.notifier.SendTest(ctx, user.Email); err != nil {
		logger.Warnf(ctx, "[Email] test send to %s failed: %v", user.Email, err)
		resp.Error = err.Error()
	} else {
		resp.Sent = true
	}
	c.JSON(http.StatusOK, resp)
}
