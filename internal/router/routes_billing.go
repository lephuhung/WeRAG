package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterBillingRoutes mounts the purchase flow for signed-in people
// (human-only raw group, API keys default-denied) and the SuperAdmin side
// under /system/admin/usage next to the plan routes. Separate from
// RegisterSystemAdminRoutes so that signature stays stable.
func RegisterBillingRoutes(r *gin.RouterGroup, h *handler.UsageBillingHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	billing := r.Group("/billing", g.Member())
	billing.GET("/info", h.Info)
	billing.GET("/plans", h.Plans)
	billing.POST("/quote", h.Quote)
	billing.GET("/orders", h.ListOrders)
	billing.POST("/orders", h.CreateOrder)
	billing.GET("/orders/:code", h.GetOrder)
	billing.POST("/orders/:code/cancel", h.CancelOrder)

	admin := r.Group("/system/admin/usage", g.SystemAdmin())
	admin.GET("/orders", h.AdminListOrders)
	admin.POST("/orders/:id/confirm", h.AdminConfirmOrder)
	admin.POST("/orders/:id/cancel", h.AdminCancelOrder)
	admin.GET("/promos", h.ListPromos)
	admin.POST("/promos", h.CreatePromo)
	admin.PUT("/promos/:id", h.UpdatePromo)
	admin.DELETE("/promos/:id", h.DeletePromo)
}
