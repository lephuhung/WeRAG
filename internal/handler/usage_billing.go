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

// Subscription godoc
// @Summary      The scope's current subscription and its specialist agents
// @Description  Held add-ons (with their end) and the add-ons still on sale with the subscription's plan, for a mid-term purchase (quote/order with kind "addon").
// @Tags         Thanh toán
// @Param        scope query string true "tenant or user"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/subscription [get]
func (h *UsageBillingHandler) Subscription(c *gin.Context) {
	scopeType := strings.TrimSpace(c.Query("scope"))
	scopeID, ok := billingScope(c, scopeType)
	if !ok {
		return
	}
	out, err := h.billing.ScopeAddons(c.Request.Context(), scopeType, scopeID)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
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
	updated, err := h.billing.CancelOrder(c.Request.Context(), order.ID, false)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

// InvoiceDetails godoc
// @Summary      VAT invoice details of the scope's latest order that asked for one (to prefill)
// @Tags         Thanh toán
// @Param        scope query string true "tenant or user"
// @Success      200 {object} map[string]interface{} "data: details or null"
// @Security     Bearer
// @Router       /billing/invoice-details [get]
func (h *UsageBillingHandler) InvoiceDetails(c *gin.Context) {
	scopeType := strings.TrimSpace(c.Query("scope"))
	scopeID, ok := billingScope(c, scopeType)
	if !ok {
		return
	}
	d, err := h.billing.LastInvoiceDetails(c.Request.Context(), scopeType, scopeID)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": d})
}

// UpdateOrderInvoice godoc
// @Summary      Ask for, change or withdraw the VAT invoice of an order
// @Description  Allowed on pending and paid orders until a SuperAdmin marks the invoice issued (409, reason invoice_issued); cancelled/expired orders are refused (409, reason invoice_order_closed). requested=false withdraws the request.
// @Tags         Thanh toán
// @Accept       json
// @Param        code path string true "order code"
// @Param        body body types.UsageInvoiceUpdate true "invoice details"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/orders/{code}/invoice [put]
func (h *UsageBillingHandler) UpdateOrderInvoice(c *gin.Context) {
	var req types.UsageInvoiceUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid invoice body").WithDetails(err.Error()))
		return
	}
	order, err := h.billing.GetOrderByCode(c.Request.Context(), c.Param("code"))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	if !canSeeOrder(c, order.UsageOrder) {
		c.Error(apperrors.NewNotFoundError("order not found"))
		return
	}
	updated, err := h.billing.UpdateOrderInvoice(c.Request.Context(), order.ID, req)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

// OrderReceipt godoc
// @Summary      Payment receipt of a paid order
// @Description  The order, the seller (billing.seller_* settings) and the buyer. Same access as the order; 409 (reason order_not_paid) unless paid.
// @Tags         Thanh toán
// @Param        code path string true "order code"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /billing/orders/{code}/receipt [get]
func (h *UsageBillingHandler) OrderReceipt(c *gin.Context) {
	order, err := h.billing.GetOrderByCode(c.Request.Context(), c.Param("code"))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	if !canSeeOrder(c, order.UsageOrder) {
		c.Error(apperrors.NewNotFoundError("order not found"))
		return
	}
	r, err := h.billing.OrderReceipt(c.Request.Context(), order)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": r})
}

// ---- SuperAdmin ---------------------------------------------------------------

// AdminMarkInvoiceIssued godoc
// @Summary      Mark the VAT invoice of a paid order issued (SuperAdmin)
// @Description  The e-invoice is issued in an external system; number, series and date (YYYY-MM-DD, default today) are optional. Marking again corrects them.
// @Tags         Gói sử dụng
// @Accept       json
// @Param        id   path string                  true "order id"
// @Param        body body types.UsageInvoiceIssue false "invoice number, series, date"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/orders/{id}/invoice-issued [post]
func (h *UsageBillingHandler) AdminMarkInvoiceIssued(c *gin.Context) {
	var req types.UsageInvoiceIssue
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.Error(apperrors.NewBadRequestError("invalid invoice body").WithDetails(err.Error()))
			return
		}
	}
	by, _ := types.UserIDFromContext(c.Request.Context())
	order, err := h.billing.MarkInvoiceIssued(c.Request.Context(), c.Param("id"), by, req)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

// AdminUnmarkInvoiceIssued godoc
// @Summary      Undo "invoice issued" on an order (SuperAdmin)
// @Tags         Gói sử dụng
// @Param        id path string true "order id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/orders/{id}/invoice-issued [delete]
func (h *UsageBillingHandler) AdminUnmarkInvoiceIssued(c *gin.Context) {
	order, err := h.billing.UnmarkInvoiceIssued(c.Request.Context(), c.Param("id"))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

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

// forceParam reads ?force=true|1 (SuperAdmin overrides).
func forceParam(c *gin.Context) bool {
	v := strings.TrimSpace(c.Query("force"))
	return v == "true" || v == "1"
}

// AdminCancelOrder godoc
// @Summary      Cancel a pending order (SuperAdmin)
// @Description  An order that has received money is refused (409, reason payment_received) unless force=true.
// @Tags         Gói sử dụng
// @Param        id    path  string true  "order id"
// @Param        force query bool   false "cancel although money was received"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/orders/{id}/cancel [post]
func (h *UsageBillingHandler) AdminCancelOrder(c *gin.Context) {
	order, err := h.billing.CancelOrder(c.Request.Context(), c.Param("id"), forceParam(c))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

// AdminReopenOrder godoc
// @Summary      Reopen an expired order (SuperAdmin)
// @Description  For money that arrived after the order expired: the order is pending again with a fresh payment window and its promo use reserved again; the transfers already recorded for it are counted again and confirm it when they cover the total. A plan order is refused (409, plan_order_pending) while another plan order of the scope waits for payment, unless force=true.
// @Tags         Gói sử dụng
// @Param        id    path  string true  "order id"
// @Param        force query bool   false "reopen although another plan order is pending"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/orders/{id}/reopen [post]
func (h *UsageBillingHandler) AdminReopenOrder(c *gin.Context) {
	order, err := h.billing.ReopenOrder(c.Request.Context(), c.Param("id"), forceParam(c))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": order})
}

// AdminAwaitingOrderCount godoc
// @Summary      How many orders need a SuperAdmin
// @Description  awaiting_admin: pending orders of 0 đồng or with money received without confirming them; invoices_to_issue: paid orders asking for a VAT invoice not marked issued yet.
// @Tags         Gói sử dụng
// @Success      200 {object} map[string]interface{} "data: {awaiting_admin, invoices_to_issue}"
// @Security     Bearer
// @Router       /system/admin/usage/orders/awaiting-admin-count [get]
func (h *UsageBillingHandler) AdminAwaitingOrderCount(c *gin.Context) {
	n, err := h.billing.AdminCounts(c.Request.Context())
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": n})
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
