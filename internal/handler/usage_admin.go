package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// UsageAdminHandler is the SuperAdmin surface for usage packages: plans,
// subscriptions of workspaces and people, seats. Mounted under
// /system/admin/usage, so the group's SystemAdmin gate applies.
type UsageAdminHandler struct {
	plans interfaces.UsagePlanService
}

// NewUsageAdminHandler creates the handler.
func NewUsageAdminHandler(plans interfaces.UsagePlanService) *UsageAdminHandler {
	return &UsageAdminHandler{plans: plans}
}

func usageAdminFail(c *gin.Context, err error) {
	if appErr, ok := err.(*apperrors.AppError); ok {
		c.Error(appErr)
		return
	}
	c.Error(apperrors.NewInternalServerError("usage packages operation failed").WithDetails(err.Error()))
}

// ListPlans godoc
// @Summary      List usage plans
// @Tags         Gói sử dụng
// @Param        include_inactive query bool false "include deactivated plans"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/plans [get]
func (h *UsageAdminHandler) ListPlans(c *gin.Context) {
	includeInactive := c.Query("include_inactive") == "true" || c.Query("include_inactive") == "1"
	plans, err := h.plans.ListPlans(c.Request.Context(), includeInactive)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": plans})
}

// CreatePlan godoc
// @Summary      Create a usage plan
// @Tags         Gói sử dụng
// @Accept       json
// @Param        plan body types.UsagePlan true "plan"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/plans [post]
func (h *UsageAdminHandler) CreatePlan(c *gin.Context) {
	var plan types.UsagePlan
	if err := c.ShouldBindJSON(&plan); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid plan body").WithDetails(err.Error()))
		return
	}
	created, err := h.plans.CreatePlan(c.Request.Context(), &plan)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": created})
}

// UpdatePlan godoc
// @Summary      Update a usage plan
// @Tags         Gói sử dụng
// @Accept       json
// @Param        id path string true "plan id"
// @Param        plan body types.UsagePlan true "plan"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/plans/{id} [put]
func (h *UsageAdminHandler) UpdatePlan(c *gin.Context) {
	var plan types.UsagePlan
	if err := c.ShouldBindJSON(&plan); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid plan body").WithDetails(err.Error()))
		return
	}
	plan.ID = c.Param("id")
	updated, err := h.plans.UpdatePlan(c.Request.Context(), &plan)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

// DeletePlan godoc
// @Summary      Delete an unused usage plan
// @Tags         Gói sử dụng
// @Param        id path string true "plan id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/plans/{id} [delete]
func (h *UsageAdminHandler) DeletePlan(c *gin.Context) {
	if err := h.plans.DeletePlan(c.Request.Context(), c.Param("id")); err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListSubscriptions godoc
// @Summary      List subscriptions
// @Tags         Gói sử dụng
// @Param        scope_type query string false "tenant|user"
// @Param        scope_id   query string false "scope id"
// @Param        status     query string false "active|cancelled"
// @Param        plan_id    query string false "plan id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions [get]
func (h *UsageAdminHandler) ListSubscriptions(c *gin.Context) {
	filter := types.UsageSubscriptionFilter{
		ScopeType: strings.TrimSpace(c.Query("scope_type")),
		ScopeID:   strings.TrimSpace(c.Query("scope_id")),
		Status:    strings.TrimSpace(c.Query("status")),
		PlanID:    strings.TrimSpace(c.Query("plan_id")),
	}
	subs, err := h.plans.ListSubscriptions(c.Request.Context(), filter)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": subs})
}

// CreateSubscription godoc
// @Summary      Attach a plan to an organization or person
// @Tags         Gói sử dụng
// @Accept       json
// @Param        subscription body types.UsageSubscription true "subscription"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions [post]
func (h *UsageAdminHandler) CreateSubscription(c *gin.Context) {
	var sub types.UsageSubscription
	if err := c.ShouldBindJSON(&sub); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid subscription body").WithDetails(err.Error()))
		return
	}
	created, err := h.plans.CreateSubscription(c.Request.Context(), &sub)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": created})
}

// UpdateSubscription godoc
// @Summary      Update a subscription
// @Tags         Gói sử dụng
// @Accept       json
// @Param        id path string true "subscription id"
// @Param        subscription body types.UsageSubscription true "subscription"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions/{id} [put]
func (h *UsageAdminHandler) UpdateSubscription(c *gin.Context) {
	var sub types.UsageSubscription
	if err := c.ShouldBindJSON(&sub); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid subscription body").WithDetails(err.Error()))
		return
	}
	sub.ID = c.Param("id")
	updated, err := h.plans.UpdateSubscription(c.Request.Context(), &sub)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

// DeleteSubscription godoc
// @Summary      Delete a subscription
// @Tags         Gói sử dụng
// @Param        id path string true "subscription id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions/{id} [delete]
func (h *UsageAdminHandler) DeleteSubscription(c *gin.Context) {
	if err := h.plans.DeleteSubscription(c.Request.Context(), c.Param("id")); err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// TenantOverview godoc
// @Summary      Every organization with its subscription and usage
// @Tags         Gói sử dụng
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/tenants [get]
func (h *UsageAdminHandler) TenantOverview(c *gin.Context) {
	rows, err := h.plans.TenantOverview(c.Request.Context())
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// ListSeats godoc
// @Summary      Seats of a seat-mode subscription
// @Tags         Gói sử dụng
// @Param        id path string true "subscription id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions/{id}/seats [get]
func (h *UsageAdminHandler) ListSeats(c *gin.Context) {
	seats, err := h.plans.ListSeats(c.Request.Context(), c.Param("id"))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": seats})
}

type usageSeatRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

// AssignSeat godoc
// @Summary      Give a person a seat
// @Tags         Gói sử dụng
// @Accept       json
// @Param        id path string true "subscription id"
// @Param        body body usageSeatRequest true "seat"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions/{id}/seats [post]
func (h *UsageAdminHandler) AssignSeat(c *gin.Context) {
	var req usageSeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("user_id is required"))
		return
	}
	by, _ := types.UserIDFromContext(c.Request.Context())
	if err := h.plans.AssignSeat(c.Request.Context(), c.Param("id"), strings.TrimSpace(req.UserID), by); err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RevokeSeat godoc
// @Summary      Take a seat back
// @Tags         Gói sử dụng
// @Param        id path string true "subscription id"
// @Param        user_id path string true "user id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions/{id}/seats/{user_id} [delete]
func (h *UsageAdminHandler) RevokeSeat(c *gin.Context) {
	if err := h.plans.RevokeSeat(c.Request.Context(), c.Param("id"), c.Param("user_id")); err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListSubscriptionAddons godoc
// @Summary      Specialist agents a subscription may use
// @Tags         Gói sử dụng
// @Param        id path string true "subscription id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions/{id}/addons [get]
func (h *UsageAdminHandler) ListSubscriptionAddons(c *gin.Context) {
	addons, err := h.plans.ListSubscriptionAddons(c.Request.Context(), c.Param("id"))
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": addons})
}

// GrantSubscriptionAddon godoc
// @Summary      Grant a specialist (addon) agent to a subscription
// @Description  Manual grant by a SuperAdmin (tests, negotiated deals). Re-granting replaces the period and price.
// @Tags         Gói sử dụng
// @Accept       json
// @Param        id path string true "subscription id"
// @Param        body body types.UsageSubscriptionAddonGrant true "add-on"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions/{id}/addons [post]
func (h *UsageAdminHandler) GrantSubscriptionAddon(c *gin.Context) {
	var req types.UsageSubscriptionAddonGrant
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid add-on body").WithDetails(err.Error()))
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		c.Error(apperrors.NewBadRequestError("agent_id is required"))
		return
	}
	by, _ := types.UserIDFromContext(c.Request.Context())
	addon, err := h.plans.GrantSubscriptionAddon(c.Request.Context(), c.Param("id"), req, by)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": addon})
}

// RevokeSubscriptionAddon godoc
// @Summary      Take a specialist agent back from a subscription
// @Tags         Gói sử dụng
// @Param        id path string true "subscription id"
// @Param        agent_id path string true "agent id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/usage/subscriptions/{id}/addons/{agent_id} [delete]
func (h *UsageAdminHandler) RevokeSubscriptionAddon(c *gin.Context) {
	if err := h.plans.RevokeSubscriptionAddon(c.Request.Context(), c.Param("id"), c.Param("agent_id")); err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
