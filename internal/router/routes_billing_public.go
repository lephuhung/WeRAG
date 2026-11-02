package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterPublicBillingRoutes runs before global Auth; purchases retain their guards.
func RegisterPublicBillingRoutes(r *gin.Engine, h *handler.UsageBillingHandler) {
	if h != nil {
		r.GET("/api/v1/public/billing/plans", h.PublicPlans)
	}
}
