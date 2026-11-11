package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterAnswerQualityRoutes mounts the answer-quality inbox (disliked
// answers, ungrounded questions). Human-only: raw groups, declared to no
// API-key policy, so the API-key gate refuses every key (the service checks
// again). Privacy (product owner): a Tenant Admin reads their own
// organization (/quality, TenantAdmin — strict, Members and guests 403
// whatever the rollout flag); a SuperAdmin reads every organization
// (/system/quality, PlatformSuperAdmin).
func RegisterAnswerQualityRoutes(r *gin.RouterGroup, h *handler.AnswerQualityHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	tenant := r.Group("/quality", g.TenantAdmin())
	{
		tenant.GET("/disliked", h.ListDisliked)
		tenant.GET("/ungrounded", h.ListUngrounded)
		tenant.GET("/ungrounded/groups", h.GroupUngrounded)
		tenant.PUT("/reviews/:message_id", h.Review)
	}
	platform := r.Group("/system/quality", g.PlatformSuperAdmin())
	{
		platform.GET("/disliked", h.PlatformListDisliked)
		platform.GET("/ungrounded", h.PlatformListUngrounded)
		platform.GET("/ungrounded/groups", h.PlatformGroupUngrounded)
		platform.PUT("/reviews/:message_id", h.PlatformReview)
	}
}
