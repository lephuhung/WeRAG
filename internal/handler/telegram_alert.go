package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// TelegramAlertHandler serves the SuperAdmin side of the system alert bot:
// its state, the link codes, the linked chats and a test message. The
// token, the switch and the muted kinds are telegram_alerts.* system
// settings.
type TelegramAlertHandler struct {
	alerts *service.TelegramAlerts
}

// NewTelegramAlertHandler creates the handler.
func NewTelegramAlertHandler(alerts *service.TelegramAlerts) *TelegramAlertHandler {
	return &TelegramAlertHandler{alerts: alerts}
}

func telegramAdmin(c *gin.Context) *types.User {
	user, _ := c.Request.Context().Value(types.UserContextKey).(*types.User)
	if user == nil {
		c.Error(apperrors.NewUnauthorizedError("sign in as a SuperAdmin"))
	}
	return user
}

// Status godoc
// @Summary      Trạng thái bot cảnh báo Telegram
// @Description  Bot (tên, lỗi nếu có), số tin chờ gửi / gửi lỗi trong ngày, các loại cảnh báo và loại đang tắt.
// @Tags         system
// @Produce      json
// @Success      200  {object}  types.TelegramAlertStatus
// @Router       /system/admin/telegram [get]
func (h *TelegramAlertHandler) Status(c *gin.Context) {
	st, err := h.alerts.Status(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": st})
}

// CreateLinkCode godoc
// @Summary      Tạo mã liên kết Telegram
// @Description  Mã dùng một lần trong 10 phút: gửi /start <mã> cho bot (chat riêng) hoặc thêm bot vào nhóm bằng liên kết nhóm. Body {"personal": true}: mã liên kết Telegram của chính SuperAdmin (chỉ chat riêng; chat đó bấm được nút duyệt / hủy đơn).
// @Tags         system
// @Produce      json
// @Success      200  {object}  types.TelegramLinkCode
// @Router       /system/admin/telegram/link-code [post]
func (h *TelegramAlertHandler) CreateLinkCode(c *gin.Context) {
	user := telegramAdmin(c)
	if user == nil {
		return
	}
	// personal: link the caller's own Telegram account (may act on orders)
	var req struct {
		Personal bool `json:"personal"`
	}
	_ = c.ShouldBindJSON(&req)
	code, err := h.alerts.CreateLinkCode(c.Request.Context(), user.ID, req.Personal)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": code})
}

// ListChats godoc
// @Summary      Các chat nhận cảnh báo Telegram
// @Tags         system
// @Produce      json
// @Success      200  {array}  types.TelegramAlertChatView
// @Router       /system/admin/telegram/chats [get]
func (h *TelegramAlertHandler) ListChats(c *gin.Context) {
	chats, err := h.alerts.ListChats(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": chats})
}

// DeleteChat godoc
// @Summary      Gỡ một chat khỏi bot cảnh báo
// @Tags         system
// @Param        id  path  string  true  "chat id"
// @Success      200
// @Router       /system/admin/telegram/chats/{id} [delete]
func (h *TelegramAlertHandler) DeleteChat(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	if err := h.alerts.DeleteChat(ctx, c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	logger.Infof(ctx, "[TelegramAlerts] chat %s unlinked by a SuperAdmin", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SendTest godoc
// @Summary      Gửi tin thử qua bot cảnh báo
// @Description  Xếp một tin thử cho mọi chat đang nhận cảnh báo; trả về số chat.
// @Tags         system
// @Produce      json
// @Success      200
// @Router       /system/admin/telegram/test [post]
func (h *TelegramAlertHandler) SendTest(c *gin.Context) {
	user := telegramAdmin(c)
	if user == nil {
		return
	}
	n, err := h.alerts.SendTest(c.Request.Context(), user)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"chats": n}})
}
