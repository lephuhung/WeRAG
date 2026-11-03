package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterEmailAdminRoutes registers the SuperAdmin email routes (the SMTP
// account is edited as email.* system settings). Human SuperAdmin only:
// never an API key (PlatformSuperAdmin).
func RegisterEmailAdminRoutes(r *gin.RouterGroup, h *handler.EmailHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	email := r.Group("/system/admin/email", g.PlatformSuperAdmin())
	email.POST("/test", h.SendTest)
}
