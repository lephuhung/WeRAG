package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
)

// RegisterBillingRoutes mounts the purchase flow for signed-in people
// (human-only raw group, API keys default-denied) and the SuperAdmin side
// under /system/admin/usage next to the plan routes. Separate from
// RegisterSystemAdminRoutes so that signature stays stable.
func RegisterBillingRoutes(r *gin.RouterGroup, h *handler.UsageBillingHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	// The payment checker has no account (the path is in middleware
	// noAuthAPI): PaymentWebhookAuth checks its shared secret under per-IP
	// limits, wrong secrets counted apart.
	r.POST("/billing/payments/webhook", middleware.PaymentWebhookAuth(h.PaymentWebhookSecret), h.PaymentWebhook)

	billing := r.Group("/billing", g.Member())
	billing.GET("/info", h.Info)
	billing.GET("/plans", h.Plans)
	billing.GET("/subscription", h.Subscription)
	billing.POST("/quote", h.Quote)
	billing.GET("/orders", h.ListOrders)
	billing.POST("/orders", h.CreateOrder)
	billing.GET("/orders/:code", h.GetOrder)
	billing.POST("/orders/:code/cancel", h.CancelOrder)

	admin := r.Group("/system/admin/usage", g.SystemAdmin())
	admin.GET("/orders", h.AdminListOrders)
	admin.POST("/orders/:id/confirm", h.AdminConfirmOrder)
	admin.POST("/orders/:id/cancel", h.AdminCancelOrder)
	admin.GET("/payments", h.AdminListPayments)
	admin.GET("/promos", h.ListPromos)
	admin.POST("/promos", h.CreatePromo)
	admin.PUT("/promos/:id", h.UpdatePromo)
	admin.DELETE("/promos/:id", h.DeletePromo)
}
