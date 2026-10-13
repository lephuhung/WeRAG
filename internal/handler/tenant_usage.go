package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// TenantUsageHandler is the workspace-facing side of usage packages: every
// member sees the unit's package and consumption; the Tenant Admin hands
// out and takes back seats when the package is in seat mode.
type TenantUsageHandler struct {
	plans interfaces.UsagePlanService
}

// NewTenantUsageHandler creates the handler.
func NewTenantUsageHandler(plans interfaces.UsagePlanService) *TenantUsageHandler {
	return &TenantUsageHandler{plans: plans}
}

// Get godoc
// @Summary      The workspace's usage package
// @Tags         Tenant workspace
// @Param        id path string true "workspace id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /tenants/{id}/usage [get]
func (h *TenantUsageHandler) Get(c *gin.Context) {
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}
	out, err := h.plans.TenantUsage(c.Request.Context(), tenantID)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// AssignSeat godoc
// @Summary      Give a member a seat of the workspace package
// @Tags         Tenant workspace
// @Accept       json
// @Param        id path string true "workspace id"
// @Param        body body usageSeatRequest true "seat"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /tenants/{id}/usage/seats [post]
func (h *TenantUsageHandler) AssignSeat(c *gin.Context) {
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}
	var req usageSeatRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.UserID) == "" {
		c.Error(apperrors.NewBadRequestError("user_id is required"))
		return
	}
	by, _ := types.UserIDFromContext(c.Request.Context())
	if err := h.plans.AssignTenantSeat(c.Request.Context(), tenantID, strings.TrimSpace(req.UserID), by); err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RevokeSeat godoc
// @Summary      Take a member's seat back
// @Tags         Tenant workspace
// @Param        id path string true "workspace id"
// @Param        user_id path string true "user id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /tenants/{id}/usage/seats/{user_id} [delete]
func (h *TenantUsageHandler) RevokeSeat(c *gin.Context) {
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}
	if err := h.plans.RevokeTenantSeat(c.Request.Context(), tenantID, c.Param("user_id")); err != nil {
		usageAdminFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
