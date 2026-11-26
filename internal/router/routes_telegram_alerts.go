package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterTelegramAlertRoutes registers the SuperAdmin routes of the
// system alert bot (its settings are telegram_alerts.* system settings).
// Human SuperAdmin only: never an API key (PlatformSuperAdmin).
func RegisterTelegramAlertRoutes(r *gin.RouterGroup, h *handler.TelegramAlertHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	tg := r.Group("/system/admin/telegram", g.PlatformSuperAdmin())
	tg.GET("", h.Status)
	tg.POST("/link-code", h.CreateLinkCode)
	tg.GET("/chats", h.ListChats)
	tg.DELETE("/chats/:id", h.DeleteChat)
	tg.POST("/test", h.SendTest)
}
