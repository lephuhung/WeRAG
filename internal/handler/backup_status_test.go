package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Without BACKUP_S3_* the monitor is nil and the page is told backups are
// not configured.
func TestBackupStatusNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/backups", NewBackupStatusHandler(nil).Get)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/backups", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Enabled bool              `json:"enabled"`
			Jobs    []json.RawMessage `json:"jobs"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.False(t, body.Data.Enabled)
	require.NotNil(t, body.Data.Jobs)
}
