package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// LegalSyncHandler exposes the SuperAdmin monitor of the legal data
// workers: the nightly vbpl.vn sync, the auto-ingest and expired-retention
// workers, the queues they drain and the push activity of the legal feeds.
type LegalSyncHandler struct {
	monitor *service.LegalSyncMonitor
}

// NewLegalSyncHandler creates the handler.
func NewLegalSyncHandler(monitor *service.LegalSyncMonitor) *LegalSyncHandler {
	return &LegalSyncHandler{monitor: monitor}
}

// GetLegalSyncStatus godoc
// @Summary      Legal data workers status
// @Description  Each worker's last run, last successful run and health
// @Description  (ok, unknown, warning, error with reasons), the queues and
// @Description  the push activity of the legal feeds over 30 days.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /system/legal-sync/status [get]
func (h *LegalSyncHandler) GetLegalSyncStatus(c *gin.Context) {
	st, err := h.monitor.Status(c.Request.Context())
	if err != nil {
		c.Error(errors.NewInternalServerError("legal sync status").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": st})
}

// ListLegalSyncRuns godoc
// @Summary      Legal data worker runs
// @Description  The latest runs, newest first. worker is vbpl_sync,
// @Description  auto_ingest, expired_retention or empty for all; limit 1–200
// @Description  (default 30).
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        worker  query  string  false  "vbpl_sync, auto_ingest, expired_retention"
// @Param        limit   query  int     false  "1–200, default 30"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /system/legal-sync/runs [get]
func (h *LegalSyncHandler) ListLegalSyncRuns(c *gin.Context) {
	worker := strings.TrimSpace(c.Query("worker"))
	switch worker {
	case "", types.LegalSyncWorkerVBPL, types.LegalSyncWorkerAutoIngest, types.LegalSyncWorkerExpiredRetention:
	default:
		c.Error(errors.NewBadRequestError("unknown worker " + worker))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	runs, err := h.monitor.Runs(c.Request.Context(), worker, limit)
	if err != nil {
		c.Error(errors.NewInternalServerError("legal sync runs").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": runs}})
}
