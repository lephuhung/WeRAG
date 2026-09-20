package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterAbbreviationRoutes wires the global Vietnamese abbreviation
// dictionary (AIRAG port). The dictionary is shared across tenants, so there
// is no KB/tenant sub-resource scoping: reads and suggestions are Viewer+,
// while edits, activation and deletes are Admin+ (mirroring AIRAG's
// user-suggests / superadmin-approves workflow).
func RegisterAbbreviationRoutes(r *gin.RouterGroup, h *handler.AbbreviationHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	abbr := g.apiKeyGroup(r.Group("/abbreviations"), apiKeyFullAccess())
	{
		abbr.GET("", g.Viewer(), h.ListAbbreviations)
		abbr.GET("/:id", g.Viewer(), h.GetAbbreviation)
		// POST creates an inactive suggestion — safe for any signed-in user.
		abbr.POST("", g.Viewer(), h.CreateAbbreviation)
		// PATCH flips is_active (approval) or edits content — admin only.
		abbr.PATCH("/:id", g.Admin(), h.UpdateAbbreviation)
		abbr.DELETE("/:id", g.Admin(), h.DeleteAbbreviation)
	}
}
