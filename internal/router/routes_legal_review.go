package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterLegalReviewRoutes wires the SuperAdmin review of the legal
// identity of public-KB documents. Public KBs are platform-owned, so every
// route needs an explicit human SuperAdmin; none is reachable by API key.
func RegisterLegalReviewRoutes(r *gin.RouterGroup, h *handler.LegalReviewHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	reviews := r.Group("/system/legal-reviews", g.PlatformSuperAdmin())
	{
		reviews.GET("", h.ListLegalReviews)
		reviews.POST("/scan", h.ScanLegalReviews)
		reviews.PUT("/:knowledge_id", h.DecideLegalReview)
		reviews.POST("/:knowledge_id/spellcheck", h.SpellcheckLegalReview)
	}
	r.GET("/system/legal-catalog/search", g.PlatformSuperAdmin(), h.SearchLegalCatalog)
}
