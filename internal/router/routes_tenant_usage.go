package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterTenantUsageRoutes mounts the workspace-facing usage-package
// routes. Human-only (raw group, so API keys are default-denied): any
// member reads the unit's package; seat changes are Tenant Admin work.
// Lives apart from RegisterTenantRoutes so its signature stays stable.
func RegisterTenantUsageRoutes(r *gin.RouterGroup, h *handler.TenantUsageHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	grp := r.Group("/tenants/:id/usage", g.PathTenantMatch())
	grp.GET("", g.Member(), h.Get)
	grp.POST("/seats", g.TenantAdmin(), h.AssignSeat)
	grp.DELETE("/seats/:user_id", g.TenantAdmin(), h.RevokeSeat)
}
