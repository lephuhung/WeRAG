package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterDocumentLabelRoutes mounts the review table and JSONL export of
// the spelling and format labels. Human-only (raw group, so the API-key
// gate default-denies it): the rows hold users' paragraph text. Tenant
// Admin sees their own organization, the SuperAdmin all (the handler
// scopes it; TenantAdmin() admits both).
func RegisterDocumentLabelRoutes(r *gin.RouterGroup, h *handler.DocumentLabelHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	grp := r.Group("/document-labels", g.TenantAdmin())
	grp.GET("", h.ListDocumentLabels)
	grp.GET("/facets", h.DocumentLabelFacets)
	grp.GET("/export", h.ExportDocumentLabels)
}
