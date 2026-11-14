package handler

import (
	"context"
	stderrors "errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
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
// @Description  dcs_sync, auto_ingest, expired_retention or empty for all; limit 1–200
// @Description  (default 30).
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        worker  query  string  false  "vbpl_sync, dcs_sync, auto_ingest, expired_retention"
// @Param        limit   query  int     false  "1–200, default 30"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /system/legal-sync/runs [get]
func (h *LegalSyncHandler) ListLegalSyncRuns(c *gin.Context) {
	worker := strings.TrimSpace(c.Query("worker"))
	switch worker {
	case "", types.LegalSyncWorkerVBPL, types.LegalSyncWorkerDCS, types.LegalSyncWorkerAutoIngest,
		types.LegalSyncWorkerExpiredRetention:
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

// legalSyncReportBodyMax bounds a run report (the error is kept to its
// last 4000 bytes anyway).
const legalSyncReportBodyMax = 256 << 10

// legalSyncReportAllowed: who may record the runs of a scheduled sync
// without database credentials (deploy/dcs-sync). An API key of the
// platform organization with full access or the ingest capability (the
// route's API-key policy requires one of them; checked again here), or an
// explicit human SuperAdmin. Not the legal-document operators, not another
// organization's key.
func legalSyncReportAllowed(ctx context.Context) error {
	caller := types.CallerFromContext(ctx)
	if scope, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		if caller.TenantID != types.PlatformTenantID {
			return errors.NewForbiddenError("only an API key of the platform organization may record sync runs")
		}
		if !scope.FullAccess && !scope.HasCapability(types.APIKeyCapabilityIngest) {
			return errors.NewForbiddenError("the API key needs the ingest capability to record sync runs")
		}
		return nil
	}
	if access.IsExplicitHumanSuperAdmin(ctx, caller) {
		return nil
	}
	return errors.NewForbiddenError("Forbidden: system administrator required")
}

// legalSyncRunReport is the body of the run endpoints.
type legalSyncRunReport struct {
	Worker  string         `json:"worker"`
	Step    string         `json:"step"`
	Status  string         `json:"status"`
	Error   string         `json:"error"`
	Summary map[string]any `json:"summary"`
}

// legalSyncReportStart checks the caller and reads the body (an empty
// body is an empty report) and the run id (the :id route parameter, 0 for
// a start).
func legalSyncReportStart(c *gin.Context, withID bool) (legalSyncRunReport, int64, bool) {
	var body legalSyncRunReport
	if err := legalSyncReportAllowed(c.Request.Context()); err != nil {
		c.Error(err)
		return body, 0, false
	}
	var id int64
	if withID {
		var err error
		if id, err = strconv.ParseInt(c.Param("id"), 10, 64); err != nil || id <= 0 {
			c.Error(errors.NewBadRequestError("invalid run id"))
			return body, 0, false
		}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, legalSyncReportBodyMax)
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil && !stderrors.Is(err, io.EOF) {
			c.Error(errors.NewBadRequestError("invalid run report").WithDetails(err.Error()))
			return body, 0, false
		}
	}
	return body, id, true
}

func legalSyncReportError(c *gin.Context, err error) {
	switch {
	case stderrors.Is(err, service.ErrLegalSyncReportWorker), stderrors.Is(err, service.ErrLegalSyncReportInput):
		c.Error(errors.NewBadRequestError(err.Error()))
	case stderrors.Is(err, service.ErrLegalSyncReportRun):
		c.Error(errors.NewNotFoundError(err.Error()))
	case stderrors.Is(err, service.ErrLegalSyncReportClosed), stderrors.Is(err, service.ErrLegalSyncReportBusy):
		c.Error(errors.NewConflictError(err.Error()))
	default:
		c.Error(errors.NewInternalServerError("legal sync run").WithDetails(err.Error()))
	}
}

// StartLegalSyncRun godoc
// @Summary      Open a run of a scheduled sync
// @Description  For the syncs without database credentials (worker
// @Description  dcs_sync). JSON {worker, step, summary}; closes the worker's
// @Description  runs left open as interrupted and returns the new run's id;
// @Description  409 while another run of the worker is alive (written to
// @Description  within 2 minutes). summary: schedule, untracked, and the
// @Description  counts of a step (fetch, build, upload); at most 32 KB.
// @Description  API key of the platform organization (ingest) or a human
// @Description  SuperAdmin.
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Security     ApiKeyAuth
// @Router       /system/legal-sync/runs [post]
func (h *LegalSyncHandler) StartLegalSyncRun(c *gin.Context) {
	body, _, ok := legalSyncReportStart(c, false)
	if !ok {
		return
	}
	id, err := h.monitor.StartReportedRun(c.Request.Context(), strings.TrimSpace(body.Worker), body.Step, body.Summary)
	if err != nil {
		legalSyncReportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": id}})
}

// UpdateLegalSyncRun godoc
// @Summary      Move a scheduled sync's run to a step
// @Description  JSON {step, summary}: step "" keeps the step; summary is
// @Description  merged (one level deep). 409 when the run is closed.
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "run id"
// @Success      200  {object}  map[string]interface{}
// @Security     ApiKeyAuth
// @Router       /system/legal-sync/runs/{id}/step [post]
func (h *LegalSyncHandler) UpdateLegalSyncRun(c *gin.Context) {
	body, id, ok := legalSyncReportStart(c, true)
	if !ok {
		return
	}
	if err := h.monitor.UpdateReportedRun(c.Request.Context(), id, body.Step, body.Summary); err != nil {
		legalSyncReportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// HeartbeatLegalSyncRun godoc
// @Summary      Sign of life of a scheduled sync's run
// @Description  A running run silent for 30 minutes is closed as
// @Description  interrupted by the monitor; a write brings it back.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        id   path  int  true  "run id"
// @Success      200  {object}  map[string]interface{}
// @Security     ApiKeyAuth
// @Router       /system/legal-sync/runs/{id}/heartbeat [post]
func (h *LegalSyncHandler) HeartbeatLegalSyncRun(c *gin.Context) {
	_, id, ok := legalSyncReportStart(c, true)
	if !ok {
		return
	}
	if err := h.monitor.TouchReportedRun(c.Request.Context(), id); err != nil {
		legalSyncReportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// FinishLegalSyncRun godoc
// @Summary      Close a scheduled sync's run
// @Description  JSON {status: ok|failed, step, error, summary}; a failed
// @Description  run keeps the last 4000 bytes of error (control characters
// @Description  and colour codes dropped).
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "run id"
// @Success      200  {object}  map[string]interface{}
// @Security     ApiKeyAuth
// @Router       /system/legal-sync/runs/{id}/finish [post]
func (h *LegalSyncHandler) FinishLegalSyncRun(c *gin.Context) {
	body, id, ok := legalSyncReportStart(c, true)
	if !ok {
		return
	}
	if err := h.monitor.FinishReportedRun(c.Request.Context(), id, strings.TrimSpace(body.Status), body.Step, body.Error,
		body.Summary); err != nil {
		legalSyncReportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
