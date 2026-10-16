package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterAgencyProfileRoutes wires the current workspace's issuing agency
// profile. It is the unit's own data, not platform configuration: every
// member reads it (documents drafted from chat use it), and the
// workspace's admin edits it as well as a SuperAdmin.
func RegisterAgencyProfileRoutes(r *gin.RouterGroup, h *handler.AgencyProfileHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	p := g.apiKeyGroup(r.Group("/tenant/agency-profile"), apiKeyFullAccess())
	{
		p.GET("", g.MemberOrSystemAdmin(), h.GetAgencyProfile)
		p.PUT("", g.TenantAdmin(), h.UpdateAgencyProfile)
	}
}
