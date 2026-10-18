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

// RegisterLegalRequestRoutes wires the SuperAdmin queue of requests to add
// legal documents users asked about that no knowledge base holds.
func RegisterLegalRequestRoutes(r *gin.RouterGroup, h *handler.LegalRequestHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	requests := r.Group("/system/legal-requests", g.PlatformSuperAdmin())
	{
		requests.GET("", h.ListLegalRequests)
		requests.GET("/public-knowledge-bases", h.ListLegalRequestKnowledgeBases)
		requests.PUT("/:id", h.DecideLegalRequest)
		requests.POST("/:id/portal-check", h.CheckLegalRequestPortal)
	}
}

// RegisterLegalDraftRoutes wires the SuperAdmin review of OCR drafts of
// documents vbpl.vn publishes only as a scanned PDF.
func RegisterLegalDraftRoutes(r *gin.RouterGroup, h *handler.LegalDraftHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	r.POST("/system/legal-requests/:id/fetch", g.PlatformSuperAdmin(), h.FetchLegalDraft)
	drafts := r.Group("/system/legal-drafts", g.PlatformSuperAdmin())
	{
		drafts.GET("", h.ListLegalDrafts)
		drafts.GET("/:id", h.GetLegalDraft)
		drafts.POST("/:id/open", h.OpenLegalDraft)
		drafts.GET("/:id/structure", h.LegalDraftStructure)
		drafts.POST("/:id/approve", h.ApproveLegalDraft)
		drafts.DELETE("/:id", h.DiscardLegalDraft)
	}
}

// RegisterDomainPackReconcileRoutes wires the SuperAdmin check of a domain
// pack's catalogue.yaml against the legal catalogue
// (docs/domain-pack-standard.md §4 layer b).
func RegisterDomainPackReconcileRoutes(r *gin.RouterGroup, h *handler.DomainPackReconcileHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	r.POST("/system/domain-packs/:name/reconcile", g.PlatformSuperAdmin(), h.ReconcileDomainPack)
}
