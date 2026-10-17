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

// RegisterTemplateStyleRoutes wires the workspace's template font sizes:
// every member reads them, the workspace's admin saves its override. The
// platform default is the system setting document_template.font_sizes.
func RegisterTemplateStyleRoutes(r *gin.RouterGroup, h *handler.TemplateStyleHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	p := g.apiKeyGroup(r.Group("/tenant/document-template-style"), apiKeyFullAccess())
	{
		p.GET("", g.MemberOrSystemAdmin(), h.GetTemplateStyle)
		p.PUT("", g.TenantAdmin(), h.UpdateTemplateStyle)
	}
}
