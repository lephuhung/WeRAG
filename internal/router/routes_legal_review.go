package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
)

// RegisterLegalLookupRoutes wires the validity lookup page. One route
// serves members and Guests: auth admits it tenantless
// (tenantOptionalRoutes), so a Guest reaches it with no organization, and
// LegalLookupAccess refuses anonymous and API-key callers and rate-limits
// a Guest per IP, a member per account. No API-key policy is declared, so
// the API-key gate denies it to keys.
//
// The SuperAdmin list of lookups that found nothing (catalogue gaps) is
// human SuperAdmin only.
func RegisterLegalLookupRoutes(
	r *gin.RouterGroup, h *handler.LegalLookupHandler, redisClient *redis.Client, g *rbacGuards,
) {
	if h == nil {
		return
	}
	r.GET("/legal-lookup", middleware.LegalLookupAccess(redisClient), h.LookupLegalValidity)
	misses := r.Group("/system/legal-lookup-misses", g.PlatformSuperAdmin())
	{
		misses.GET("", h.ListLegalLookupMisses)
		misses.DELETE("/:id", h.DeleteLegalLookupMiss)
	}
}

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

// RegisterLegalRequestRoutes wires the queue of requests to add legal
// documents users asked about that no knowledge base holds. Reading the queue
// and closing a request as "added" are open to the external-source feed's
// operator account (users.can_upload_legal_documents); ingesting from vbpl.vn,
// rejecting a request and the portal check stay with the SuperAdmin.
func RegisterLegalRequestRoutes(r *gin.RouterGroup, h *handler.LegalRequestHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	requests := r.Group("/system/legal-requests")
	{
		requests.GET("", g.LegalUploader(), h.ListLegalRequests)
		requests.GET("/public-knowledge-bases", g.PlatformSuperAdmin(), h.ListLegalRequestKnowledgeBases)
		requests.PUT("/:id", g.LegalUploader(), h.DecideLegalRequest)
		requests.POST("/:id/portal-check", g.PlatformSuperAdmin(), h.CheckLegalRequestPortal)
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
	packs := r.Group("/system/domain-packs", g.PlatformSuperAdmin())
	{
		packs.GET("", h.ListSystemDomainPacks)
		packs.GET("/:name", h.GetSystemDomainPack)
		packs.POST("/:name/reconcile", h.ReconcileDomainPack)
		packs.PUT("/:name/formulas/:id", h.UpdateDomainPackFormula)
		packs.DELETE("/:name/formulas/:id", h.ResetDomainPackFormula)
	}
}

// RegisterLegalPushRoutes wires the push of legal documents from sources
// other than vbpl.vn (docs/legal-document-feed-spec.md §9) and the
// SuperAdmin review of the provision links they carry. The push takes an
// API key of the platform organization with ingest, a human SuperAdmin, or
// a human account flagged users.can_upload_legal_documents (all re-checked
// in the handler, legalPushAllowed); the review is human SuperAdmin only.
func RegisterLegalPushRoutes(r *gin.RouterGroup, h *handler.LegalPushHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	g.apiKeyRoute(r, http.MethodPost, "/system/legal-documents/push", apiKeyIngest(apiKeyFullAccess()), h.PushLegalDocument)
	g.apiKeyRoute(r, http.MethodPost, "/system/legal-documents/push-scan", apiKeyIngest(apiKeyFullAccess()), h.PushLegalScan)
	cands := r.Group("/system/legal-link-candidates", g.PlatformSuperAdmin())
	{
		cands.GET("", h.ListLegalLinkCandidates)
		cands.PUT("/:id", h.DecideLegalLinkCandidate)
	}
}

// RegisterLegalSyncRoutes wires the SuperAdmin monitor of the legal data
// workers (read only).
func RegisterLegalSyncRoutes(r *gin.RouterGroup, h *handler.LegalSyncHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	sync := r.Group("/system/legal-sync", g.PlatformSuperAdmin())
	{
		sync.GET("/status", h.GetLegalSyncStatus)
		sync.GET("/runs", h.ListLegalSyncRuns)
	}
}
