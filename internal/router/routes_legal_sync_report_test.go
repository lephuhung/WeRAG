package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

// legalSyncReportEngine: the real API-key gate on /api/v1, a caller seeded
// like the auth middleware does (scope set: an API key of caller's tenant).
func legalSyncReportEngine(t *testing.T, caller types.Caller, systemAdmin, legalUploader bool,
	scope *types.TenantAPIKeyScope,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	g := &rbacGuards{cfg: &config.Config{}}
	gate := g.ensureAPIKeyAuthorizer()
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CallerContextKey, caller.Normalize())
		ctx = context.WithValue(ctx, types.UserIDContextKey, caller.UserID)
		ctx = context.WithValue(ctx, types.TenantIDContextKey, caller.TenantID)
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, systemAdmin)
		ctx = context.WithValue(ctx, types.LegalUploaderContextKey, legalUploader)
		if scope != nil {
			ctx = types.WithTenantAPIKeyScope(ctx, *scope)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	RegisterLegalSyncRoutes(r.Group("/api/v1", gate.Middleware()), legalSyncTestHandler(t), g)
	return r
}

func legalSyncReport(engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	engine.ServeHTTP(w, req)
	return w
}

const legalSyncStartBody = `{"worker":"dcs_sync","step":"fetch","summary":{"schedule":"15 */4 * * *"}}`

// Who may record dcs-sync runs: an API key of the platform organization
// with ingest (or full access), a human SuperAdmin. Not another
// organization's key, not a key without ingest, not the legal-document
// operator, not an organization admin.
func TestLegalSyncReportRoutesAuth(t *testing.T) {
	platform := types.Caller{TenantID: types.PlatformTenantID, UserID: "system-key"}
	ingest := &types.TenantAPIKeyScope{KeyID: 1, Capabilities: types.StringArray{string(types.APIKeyCapabilityIngest)}}
	retrieve := &types.TenantAPIKeyScope{KeyID: 2, Capabilities: types.StringArray{string(types.APIKeyCapabilityRetrieve)}}
	full := &types.TenantAPIKeyScope{KeyID: 3, FullAccess: true}
	cases := []struct {
		name   string
		engine *gin.Engine
		want   int
	}{
		{"platform ingest key", legalSyncReportEngine(t, platform, false, false, ingest), http.StatusOK},
		{"platform full-access key", legalSyncReportEngine(t, platform, false, false, full), http.StatusOK},
		{"SuperAdmin", legalSyncReportEngine(t, types.Caller{TenantID: types.PlatformTenantID, UserID: "u-admin"}, true, false, nil),
			http.StatusOK},
		{"other organization's ingest key", legalSyncReportEngine(t, types.Caller{TenantID: 7, UserID: "system-key"}, false, false,
			ingest), http.StatusForbidden},
		{"other organization's full-access key", legalSyncReportEngine(t, types.Caller{TenantID: 7, UserID: "system-key"}, false,
			false, full), http.StatusForbidden},
		{"platform key without ingest", legalSyncReportEngine(t, platform, false, false, retrieve), http.StatusForbidden},
		{"legal-document operator", legalSyncReportEngine(t, types.Caller{TenantID: types.PlatformTenantID, UserID: "u-operator"},
			false, true, nil), http.StatusForbidden},
		{"organization admin", legalSyncReportEngine(t, types.Caller{TenantID: 7, UserID: "u-member", Role: types.TenantRoleAdmin},
			false, false, nil), http.StatusForbidden},
	}
	for _, c := range cases {
		w := legalSyncReport(c.engine, http.MethodPost, "/api/v1/system/legal-sync/runs", legalSyncStartBody)
		if w.Code != c.want {
			t.Fatalf("%s: POST runs = %d, want %d (%s)", c.name, w.Code, c.want, w.Body.String())
		}
		for _, p := range []string{"/step", "/heartbeat", "/finish"} {
			if c.want == http.StatusOK {
				continue
			}
			if w := legalSyncReport(c.engine, http.MethodPost, "/api/v1/system/legal-sync/runs/1"+p, `{}`); w.Code != c.want {
				t.Fatalf("%s: POST runs/1%s = %d, want %d", c.name, p, w.Code, c.want)
			}
		}
	}
	// the key records runs; it still cannot read the monitor
	key := legalSyncReportEngine(t, platform, false, false, ingest)
	for _, p := range []string{"/api/v1/system/legal-sync/status", "/api/v1/system/legal-sync/runs"} {
		if w := legalSyncReport(key, http.MethodGet, p, ""); w.Code != http.StatusForbidden {
			t.Fatalf("ingest key GET %s = %d, want 403", p, w.Code)
		}
	}
}

// A dcs-sync run through the endpoints: start, steps, heartbeat, finish
// failed with the push failures; then visible on the monitor.
func TestLegalSyncReportRoutesLifecycle(t *testing.T) {
	platform := types.Caller{TenantID: types.PlatformTenantID, UserID: "system-key"}
	ingest := &types.TenantAPIKeyScope{KeyID: 1, Capabilities: types.StringArray{string(types.APIKeyCapabilityIngest)}}
	key := legalSyncReportEngine(t, platform, false, false, ingest)

	w := legalSyncReport(key, http.MethodPost, "/api/v1/system/legal-sync/runs", legalSyncStartBody)
	if w.Code != http.StatusOK {
		t.Fatalf("start = %d %s", w.Code, w.Body.String())
	}
	var started struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil || started.Data.ID <= 0 {
		t.Fatalf("start answer %s: %v", w.Body.String(), err)
	}
	base := "/api/v1/system/legal-sync/runs/" + jsonInt(started.Data.ID)
	for _, s := range []struct{ path, body string }{
		{"/step", `{"step":"build","summary":{"fetch":{"loaded":4}}}`},
		{"/heartbeat", ""},
		{"/step", `{"step":"upload","summary":{"build":{"text":2,"catalogue":1}}}`},
		{"/finish", `{"status":"failed","error":"0 full  01-QĐ/TW\n3 pushes failed (HTTP 404/429/5xx, transport…): retried next run","summary":{"upload":{"failed":3}}}`},
	} {
		if w := legalSyncReport(key, http.MethodPost, base+s.path, s.body); w.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", s.path, w.Code, w.Body.String())
		}
	}
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, base + "/heartbeat", "", http.StatusConflict},
		{http.MethodPost, base + "/finish", `{"status":"ok"}`, http.StatusConflict},
		{http.MethodPost, "/api/v1/system/legal-sync/runs/999/heartbeat", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/system/legal-sync/runs/abc/heartbeat", "", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/system/legal-sync/runs", `{"worker":"vbpl_sync"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/system/legal-sync/runs", `{"worker":"dcs_sync","step":"x y"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/system/legal-sync/runs", `not json`, http.StatusBadRequest},
	} {
		if w := legalSyncReport(key, c.method, c.path, c.body); w.Code != c.want {
			t.Fatalf("%s %s = %d, want %d (%s)", c.method, c.path, w.Code, c.want, w.Body.String())
		}
	}

	// a human SuperAdmin may record a run too, and reads it on the monitor
	// (each test engine has its own database)
	admin := legalSyncReportEngine(t, types.Caller{TenantID: types.PlatformTenantID, UserID: "u-admin"}, true, false, nil)
	w = legalSyncReport(admin, http.MethodPost, "/api/v1/system/legal-sync/runs", legalSyncStartBody)
	if w.Code != http.StatusOK {
		t.Fatalf("SuperAdmin start = %d", w.Code)
	}
	w = legalSyncReport(admin, http.MethodGet, "/api/v1/system/legal-sync/runs?worker=dcs_sync", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"worker":"dcs_sync"`) {
		t.Fatalf("runs = %d %s", w.Code, w.Body.String())
	}
	w = legalSyncReport(admin, http.MethodGet, "/api/v1/system/legal-sync/status", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"worker":"dcs_sync"`) ||
		!strings.Contains(w.Body.String(), `"stale_after_hours":9`) {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
