package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// PublicPlans exposes the active, priced checkout catalogue without account data.
func (h *UsageBillingHandler) PublicPlans(c *gin.Context) {
	scope := strings.TrimSpace(c.Query("scope"))
	if scope != "" && scope != types.UsageScopeUser && scope != types.UsageScopeTenant {
		c.Error(apperrors.NewBadRequestError("scope must be tenant or user"))
		return
	}
	plans, err := h.billing.PurchasablePlans(c.Request.Context(), scope)
	if err != nil {
		usageAdminFail(c, err)
		return
	}
	data := make([]gin.H, 0, len(plans))
	for _, p := range plans {
		if p == nil || p.UsagePlan == nil || !p.IsActive || p.PriceVND <= 0 {
			continue
		}
		included := p.IncludedAgents
		if included == nil {
			included = []types.UsagePlanAgent{}
		}
		addons := p.Addons
		if addons == nil {
			addons = []types.UsagePlanAddonOffer{}
		}
		// The per-turn and per-session caps buyers compare plans on, typed so
		// unknown keys in the stored features never leave the server.
		caps, err := types.ParseUsagePlanFeatures(p.Features)
		if err != nil {
			caps = types.UsagePlanFeatures{}
		}
		data = append(data, gin.H{
			"id": p.ID, "name": p.Name, "description": p.Description,
			"scope_kind": p.ScopeKind, "mode": p.Mode, "limits": p.Limits,
			"price_vnd": p.PriceVND, "price_label": p.PriceLabel, "sort_order": p.SortOrder,
			"included_agents": included, "addons": addons, "caps": caps,
		})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
