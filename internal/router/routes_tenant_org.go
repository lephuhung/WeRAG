package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterTenantOrgRoutes registers tenant-org management endpoints.
//
// Tenant orgs are user groups inside one tenant (NOT the cross-tenant
// Organization used for KB/agent sharing). Route matrix:
//
//	GET    /orgs                    Viewer+  — any member sees the org list
//	POST   /orgs                    Admin+   — creating orgs is a tenant-admin op
//	GET    /orgs/:id                Viewer+
//	PUT    /orgs/:id                Viewer+  — service enforces org-manager-or-admin
//	DELETE /orgs/:id                Admin+   — deleting detaches KB scope
//	GET    /orgs/:id/members        Viewer+
//	POST   /orgs/:id/members        Viewer+  — service enforces org-manager-or-admin
//	PUT    /orgs/:id/members/:uid   Viewer+  — service enforces org-manager-or-admin
//	DELETE /orgs/:id/members/:uid   Viewer+  — service enforces org-manager-or-admin
//	POST   /orgs/:id/invite-links   Viewer+  — service enforces org-manager-or-admin
//
// The low route gates on member/invite endpoints are deliberate: an org
// manager may hold only Viewer at tenant level, and the service
// (requireOrgManager) is the authoritative check. All org endpoints
// stay JWT-only — no API-key capability is declared, so X-API-Key
// principals are default-denied.
func RegisterTenantOrgRoutes(r *gin.RouterGroup, h *handler.TenantOrgHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	orgs := r.Group("/orgs")
	{
		orgs.GET("", g.Viewer(), h.ListOrgs)
		orgs.POST("", g.Admin(), h.CreateOrg)
		orgs.GET("/:id", g.Viewer(), h.GetOrg)
		orgs.PUT("/:id", g.Viewer(), h.UpdateOrg)
		orgs.DELETE("/:id", g.Admin(), h.DeleteOrg)

		orgs.GET("/:id/members", g.Viewer(), h.ListMembers)
		orgs.POST("/:id/members", g.Viewer(), h.AddMember)
		orgs.PUT("/:id/members/:user_id", g.Viewer(), h.UpdateMemberRole)
		orgs.DELETE("/:id/members/:user_id", g.Viewer(), h.RemoveMember)

		orgs.POST("/:id/invite-links", g.Viewer(), h.CreateInviteLink)
	}
}
