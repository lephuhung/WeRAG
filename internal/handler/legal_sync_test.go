package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

// legalSyncTestDB has the tables the monitor reads (the legal ones in the
// minimal shape the queries need).
func legalSyncTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalSyncRun{}, &types.LegalDocumentRequest{}))
	for _, stmt := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, metadata TEXT, parse_status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE legal_documents (id TEXT PRIMARY KEY, source TEXT, status TEXT)`,
		`CREATE TABLE legal_feed_log (id INTEGER PRIMARY KEY, action TEXT, vbpl_id TEXT, detail TEXT, created_at DATETIME)`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	return db
}

func legalSyncTestEngine(t *testing.T) (*gin.Engine, *repository.LegalSyncRunRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := legalSyncTestDB(t)
	runs := repository.NewLegalSyncRunRepository(db)
	h := NewLegalSyncHandler(service.NewLegalSyncMonitor(db, runs, nil))
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/status", h.GetLegalSyncStatus)
	r.GET("/runs", h.ListLegalSyncRuns)
	return r, runs
}

func TestLegalSyncHandlerStatusAndRuns(t *testing.T) {
	r, runs := legalSyncTestEngine(t)
	ctx := context.Background()
	id, err := runs.Start(ctx, types.LegalSyncWorkerVBPL, "crawl", map[string]any{"schedule": "30 2 * * *"})
	require.NoError(t, err)
	require.NoError(t, runs.Finish(ctx, id, types.LegalSyncOK, "", "", nil))
	require.NoError(t, runs.Record(ctx, types.LegalSyncWorkerAutoIngest, time.Now(), types.LegalSyncOK, "", map[string]any{"added": 1}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var st struct {
		Success bool `json:"success"`
		Data    struct {
			Workers []struct {
				Worker   string `json:"worker"`
				Health   string `json:"health"`
				Schedule string `json:"schedule"`
			} `json:"workers"`
			Queues struct {
				RetentionDays int `json:"retention_days"`
			} `json:"queues"`
			Feeds []json.RawMessage `json:"feeds"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	require.True(t, st.Success)
	require.Len(t, st.Data.Workers, 4)
	require.Equal(t, types.LegalSyncWorkerVBPL, st.Data.Workers[0].Worker)
	require.Equal(t, service.LegalSyncHealthOK, st.Data.Workers[0].Health)
	require.Equal(t, "30 2 * * *", st.Data.Workers[0].Schedule)
	require.Equal(t, int(types.LegalExpiredRetentionDefaultDays), st.Data.Queues.RetentionDays)
	require.NotNil(t, st.Data.Feeds, "an empty list, not null")

	for _, c := range []struct {
		query string
		want  int
	}{{"", 2}, {"?worker=vbpl_sync", 1}, {"?worker=auto_ingest&limit=5", 1}, {"?worker=expired_retention", 0}} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/runs"+c.query, nil))
		require.Equal(t, http.StatusOK, w.Code, c.query)
		var body struct {
			Data struct {
				Items []types.LegalSyncRun `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Len(t, body.Data.Items, c.want, c.query)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/runs?worker=bogus", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
}
