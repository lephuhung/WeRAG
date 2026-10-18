package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// UsageBillingHandler serves the purchase flow (every signed-in person:
// plans, quote, orders of their own or of the workspace they administer)
// and the SuperAdmin side of it (promo codes, confirming payments).
type UsageBillingHandler struct {
	billing interfaces.UsageBillingService
}

// NewUsageBillingHandler creates the handler.
func NewUsageBillingHandler(billing interfaces.UsageBillingService) *UsageBillingHandler {
	return &UsageBillingHandler{billing: billing}
}

// billingScope resolves the scope a caller may buy for: the workspace
// (Tenant Admins and SuperAdmins) or themselves. The id never comes from
// the body, so nobody can order for another workspace or person.
func billingScope(c *gin.Context, scopeType string) (string, bool) {
	ctx := c.Request.Context()
	switch scopeType {
	case types.UsageScopeTenant:
		if !types.IsSystemAdminFromContext(ctx) && !types.CallerFromContext(ctx).Role.IsTenantAdmin() {
			c.Error(apperrors.NewForbiddenError("only an organization admin can buy a package for the organization"))
			return "", false
		}
		tenantID, ok := types.TenantIDFromContext(ctx)
		if !ok || tenantID == 0 {
			c.Error(apperrors.NewBadRequestError("no current organization"))
			return "", false
		}
		return strconv.FormatUint(tenantID, 10), true
	case types.UsageScopeUser:
		uid, _ := types.UserIDFromContext(ctx)
		if uid == "" || types.IsSyntheticUserID(uid) {
			c.Error(apperrors.NewForbiddenError("sign in with a personal account to buy a personal package"))
			return "", false
		}
		return uid, true
	default:
		c.Error(apperrors.NewBadRequestError("scope_type must be tenant or user"))
		return "", false
	}
}

// canSeeOrder: the buyer, a workspace admin for the workspace's orders,
// or a SuperAdmin.
func canSeeOrder(c *gin.Context, o *types.UsageOrder) bool {
	ctx := c.Request.Context()
	if types.IsSystemAdminFromContext(ctx) {
		return true
	}
	uid, _ := types.UserIDFromContext(ctx)
	if uid != "" && o.CreatedBy == uid {
		return true
	}
	if o.ScopeType == types.UsageScopeUser {
		return uid != "" && o.ScopeID == uid
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	return ok && o.ScopeID == strconv.FormatUint(tenantID, 10) && types.CallerFromContext(ctx).Role.IsTenantAdmin()
}

// Info godoc
// @Summary      Payment details for the purchase page
// @Tags         Thanh toán
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/info [get]
func (h *UsageBillingHandler) Info(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.billing.BillingInfo(c.Request.Context())})
}

// Plans godoc
// @Summary      Plans that can be bought online
// @Tags         Thanh toán
// @Param        scope query string false "tenant or user"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/plans [get]
func (h *UsageBillingHandler) Plans(c *gin.Context) {
	plans, err := h.billing.PurchasablePlans(c.Request.Context(), strings.TrimSpace(c.Query("scope")))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": plans})
}

// Quote godoc
// @Summary      Price a purchase (with an optional promo code)
// @Tags         Thanh toán
// @Accept       json
// @Param        body body types.UsageQuoteRequest true "what to buy"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/quote [post]
func (h *UsageBillingHandler) Quote(c *gin.Context) {
	var req types.UsageQuoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid quote body").WithDetails(err.Error()))
		return
	}
	scopeID, ok := billingScope(c, req.ScopeType)
	if !ok {
		return
	}
	q, err := h.billing.Quote(c.Request.Context(), req, scopeID)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": q})
}

// CreateOrder godoc
// @Summary      Place an order for the organization or for oneself
// @Tags         Thanh toán
// @Accept       json
// @Param        body body types.UsageQuoteRequest true "what to buy"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/orders [post]
func (h *UsageBillingHandler) CreateOrder(c *gin.Context) {
	var req types.UsageQuoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid order body").WithDetails(err.Error()))
		return
	}
	scopeID, ok := billingScope(c, req.ScopeType)
	if !ok {
		return
	}
	uid, _ := types.UserIDFromContext(c.Request.Context())
	order, err := h.billing.CreateOrder(c.Request.Context(), req, scopeID, uid)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

// ListOrders godoc
// @Summary      My orders and, for an organization admin, the organization's
// @Tags         Thanh toán
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/orders [get]
func (h *UsageBillingHandler) ListOrders(c *gin.Context) {
	ctx := c.Request.Context()
	uid, _ := types.UserIDFromContext(ctx)
	seen := map[string]bool{}
	out := []*types.UsageOrderView{}
	add := func(list []*types.UsageOrderView) {
		for _, o := range list {
			if !seen[o.ID] {
				seen[o.ID] = true
				out = append(out, o)
			}
		}
	}
	if uid != "" {
		mine, err := h.billing.ListOrders(ctx, types.UsageOrderFilter{CreatedBy: uid, Limit: 100})
		if err != nil {
			usageAdminFail(c, err)
			return
		}
		add(mine)
	}
	if tenantID, ok := types.TenantIDFromContext(ctx); ok && tenantID != 0 &&
		(types.IsSystemAdminFromContext(ctx) || types.CallerFromContext(ctx).Role.IsTenantAdmin()) {
		ws, err := h.billing.ListOrders(ctx, types.UsageOrderFilter{
			ScopeType: types.UsageScopeTenant, ScopeID: strconv.FormatUint(tenantID, 10), Limit: 100,
		})
		if err != nil {
			usageAdminFail(c, err)
			return
		}
		add(ws)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// GetOrder godoc
// @Summary      One order by its code
// @Tags         Thanh toán
// @Param        code path string true "order code"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/orders/{code} [get]
func (h *UsageBillingHandler) GetOrder(c *gin.Context) {
	order, err := h.billing.GetOrderByCode(c.Request.Context(), c.Param("code"))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	if !canSeeOrder(c, order.UsageOrder) {
		c.Error(apperrors.NewNotFoundError("order not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

// CancelOrder godoc
// @Summary      Cancel a pending order of mine / of my organization
// @Tags         Thanh toán
// @Param        code path string true "order code"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/orders/{code}/cancel [post]
func (h *UsageBillingHandler) CancelOrder(c *gin.Context) {
	order, err := h.billing.GetOrderByCode(c.Request.Context(), c.Param("code"))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	if !canSeeOrder(c, order.UsageOrder) {
		c.Error(apperrors.NewNotFoundError("order not found"))
		return
	}
	updated, err := h.billing.CancelOrder(c.Request.Context(), order.ID)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

// ---- SuperAdmin ---------------------------------------------------------------

// AdminListOrders godoc
// @Summary      Every order (SuperAdmin)
// @Tags         Gói sử dụng
// @Param        status query string false "pending|paid|cancelled|expired"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/orders [get]
func (h *UsageBillingHandler) AdminListOrders(c *gin.Context) {
	orders, err := h.billing.ListOrders(c.Request.Context(), types.UsageOrderFilter{
		Status: strings.TrimSpace(c.Query("status")), Limit: 500,
	})
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": orders})
}

// AdminConfirmOrder godoc
// @Summary      Mark an order paid and apply it (SuperAdmin)
// @Tags         Gói sử dụng
// @Param        id path string true "order id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/orders/{id}/confirm [post]
func (h *UsageBillingHandler) AdminConfirmOrder(c *gin.Context) {
	by, _ := types.UserIDFromContext(c.Request.Context())
	order, err := h.billing.ConfirmOrder(c.Request.Context(), c.Param("id"), by)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

// AdminCancelOrder godoc
// @Summary      Cancel a pending order (SuperAdmin)
// @Tags         Gói sử dụng
// @Param        id path string true "order id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/orders/{id}/cancel [post]
func (h *UsageBillingHandler) AdminCancelOrder(c *gin.Context) {
	order, err := h.billing.CancelOrder(c.Request.Context(), c.Param("id"))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

// ListPromos godoc
// @Summary      Promo codes (SuperAdmin)
// @Tags         Gói sử dụng
// @Param        include_inactive query bool false "include deactivated codes"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/promos [get]
func (h *UsageBillingHandler) ListPromos(c *gin.Context) {
	includeInactive := c.Query("include_inactive") == "true" || c.Query("include_inactive") == "1"
	promos, err := h.billing.ListPromos(c.Request.Context(), includeInactive)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": promos})
}

// CreatePromo godoc
// @Summary      Create a promo code (SuperAdmin)
// @Tags         Gói sử dụng
// @Accept       json
// @Param        promo body types.UsagePromoCode true "promo"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/promos [post]
func (h *UsageBillingHandler) CreatePromo(c *gin.Context) {
	var p types.UsagePromoCode
	if err := c.ShouldBindJSON(&p); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid promo body").WithDetails(err.Error()))
		return
	}
	created, err := h.billing.CreatePromo(c.Request.Context(), &p)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": created})
}

// UpdatePromo godoc
// @Summary      Update a promo code (SuperAdmin)
// @Tags         Gói sử dụng
// @Accept       json
// @Param        id path string true "promo id"
// @Param        promo body types.UsagePromoCode true "promo"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/promos/{id} [put]
func (h *UsageBillingHandler) UpdatePromo(c *gin.Context) {
	var p types.UsagePromoCode
	if err := c.ShouldBindJSON(&p); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid promo body").WithDetails(err.Error()))
		return
	}
	p.ID = c.Param("id")
	updated, err := h.billing.UpdatePromo(c.Request.Context(), &p)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

// DeletePromo godoc
// @Summary      Delete a promo code (SuperAdmin)
// @Tags         Gói sử dụng
// @Param        id path string true "promo id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/promos/{id} [delete]
func (h *UsageBillingHandler) DeletePromo(c *gin.Context) {
	if err := h.billing.DeletePromo(c.Request.Context(), c.Param("id")); err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ---- payment checker -----------------------------------------------------------

// paymentWebhookMaxBody bounds one pushed transaction.
const paymentWebhookMaxBody = 64 << 10

// PaymentWebhookSecret feeds middleware.PaymentWebhookAuth on the route.
func (h *UsageBillingHandler) PaymentWebhookSecret(ctx context.Context) string {
	return h.billing.PaymentWebhookSecret(ctx)
}

// PaymentWebhook godoc
// @Summary      Push one incoming bank transfer (payment checker)
// @Description  Public route authenticated by the billing.webhook_secret setting (fallback BILLING_WEBHOOK_SECRET)
// @Description  (header X-Webhook-Secret, or Authorization: Bearer|Apikey <secret>).
// @Description  A transfer whose content carries a pending order's code and covers
// @Description  its total confirms the order. A resend of the same source +
// @Description  transaction_id returns the stored result with duplicate=true.
// @Tags         Thanh toán
// @Accept       json
// @Param        body body types.UsagePaymentWebhookRequest true "transaction"
// @Success      200 {object} map[string]interface{}
// @Router       /billing/payments/webhook [post]
//
// The secret and the per-IP limits are checked by
// middleware.PaymentWebhookAuth on the route, before this handler.
func (h *UsageBillingHandler) PaymentWebhook(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, paymentWebhookMaxBody+1))
	if err != nil {
		c.Error(apperrors.NewBadRequestError("cannot read the body"))
		return
	}
	if len(raw) > paymentWebhookMaxBody {
		c.Error(apperrors.NewBadRequestError("body too large"))
		return
	}
	// Postgres text and jsonb reject NUL; refuse it here (400, not worth a
	// resend) rather than fail the insert with a 500 the checker retries.
	if bytes.IndexByte(raw, 0) >= 0 || bytes.Contains(raw, []byte(`\u0000`)) {
		c.Error(apperrors.NewBadRequestError("the body must not contain NUL characters"))
		return
	}
	var req types.UsagePaymentWebhookRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid transaction body").WithDetails(err.Error()))
		return
	}
	res, err := h.billing.RecordPayment(c.Request.Context(), req, raw)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// AdminListPayments godoc
// @Summary      Transfers pushed by the payment checker (SuperAdmin)
// @Tags         Gói sử dụng
// @Description  Newest first. page_size defaults to 50, at most 200.
// @Param        status    query string false "matched|unmatched|underpaid|order_closed|confirm_failed|ignored"
// @Param        page      query int    false "Page (from 1)"
// @Param        page_size query int    false "Page size"
// @Success      200 {object} map[string]interface{} "data, total, page, page_size"
// @Security     Bearer
// @Router       /system/admin/usage/payments [get]
func (h *UsageBillingHandler) AdminListPayments(c *gin.Context) {
	pageNo, size := 1, 50
	if v := strings.TrimSpace(c.Query("page")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			c.Error(apperrors.NewBadRequestError("page must be a number from 1"))
			return
		}
		pageNo = n
	}
	if v := strings.TrimSpace(c.Query("page_size")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			c.Error(apperrors.NewBadRequestError("page_size must be a number from 1"))
			return
		}
		size = min(n, 200)
	}
	list, total, err := h.billing.ListPayments(c.Request.Context(), types.UsagePaymentFilter{
		Status: strings.TrimSpace(c.Query("status")),
		Offset: (pageNo - 1) * size,
		Limit:  size,
	})
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true, "data": list, "total": total, "page": pageNo, "page_size": size,
	})
}
