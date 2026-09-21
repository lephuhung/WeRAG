package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// StatsHandler serves the platform-wide usage statistics shown on the
// SystemAdmin overview page. All routes are mounted under the
// /system/admin group, which already enforces SystemAdmin.
type StatsHandler struct {
	statsSvc interfaces.StatsService
}

// NewStatsHandler creates a new stats handler.
func NewStatsHandler(statsSvc interfaces.StatsService) *StatsHandler {
	return &StatsHandler{statsSvc: statsSvc}
}

// GetSystemStats godoc
// @Summary      Platform usage statistics
// @Description  Account counters, documents grouped by parse status, and a
// @Description  per-day message activity series for the heatmap.
// @Tags         System Admin
// @Produce      json
// @Param        days query int false "Message activity window in days (1-365, default 90)"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/admin/stats [get]
func (h *StatsHandler) GetSystemStats(c *gin.Context) {
	days := 90
	if raw := c.Query("days"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 365 {
			c.Error(apperrors.NewValidationError("days must be an integer between 1 and 365"))
			return
		}
		days = v
	}
	stats, err := h.statsSvc.GetSystemStats(c.Request.Context(), days)
	if err != nil {
		c.Error(apperrors.NewInternalServerError("Failed to load system stats").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
}
