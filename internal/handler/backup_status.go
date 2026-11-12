package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
)

// BackupStatusHandler serves the SuperAdmin view of the backup jobs
// (service.BackupMonitor): the latest status file of each job.
type BackupStatusHandler struct {
	monitor *service.BackupMonitor
}

// NewBackupStatusHandler creates the handler; monitor nil = backups not
// configured (the page says so).
func NewBackupStatusHandler(monitor *service.BackupMonitor) *BackupStatusHandler {
	return &BackupStatusHandler{monitor: monitor}
}

// backupStatusTimeout bounds the reads of one page view.
const backupStatusTimeout = 20 * time.Second

// Get godoc
// @Summary      Backup job status (SuperAdmin)
// @Description  Latest status file of every watched backup job: state, finish time, object, size, note.
// @Tags         system
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Router       /system/admin/backups [get]
func (h *BackupStatusHandler) Get(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), backupStatusTimeout)
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.monitor.Report(ctx)})
}
