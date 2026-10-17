package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// MeUsageHandler serves GET /me/usage: what the caller and their current
// workspace used today and this month. Identity comes from the context
// only; there is no way to ask for someone else's counters here.
type MeUsageHandler struct {
	meter interfaces.UsageMeter
	guard interfaces.UsageGuard
}

// NewMeUsageHandler creates the handler.
func NewMeUsageHandler(meter interfaces.UsageMeter, guard interfaces.UsageGuard) *MeUsageHandler {
	return &MeUsageHandler{meter: meter, guard: guard}
}

// Get godoc
// @Summary      My usage counters
// @Description  Questions and LLM tokens used today and this month, for the
// @Description  caller and for the caller's current workspace.
// @Tags         Tài khoản của tôi
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /me/usage [get]
func (h *MeUsageHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, _ := types.TenantIDFromContext(ctx)
	userID, _ := types.UserIDFromContext(ctx)
	summary, err := h.meter.Summary(ctx, tenantID, userID)
	if err != nil {
		c.Error(apperrors.NewInternalServerError("Failed to load usage").WithDetails(err.Error()))
		return
	}
	if h.guard != nil {
		summary.Enforced = h.guard.Enforced(ctx)
		ent, err := h.guard.Entitlement(ctx, tenantID, userID)
		if err != nil {
			c.Error(apperrors.NewInternalServerError("Failed to load usage package").WithDetails(err.Error()))
			return
		}
		summary.Entitlement = ent
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": summary})
}
