package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// Profile 9 holds "group" (applies to workspace 1 only); workspaces 1 and 2
// inherit from the profile. The router puts the caller in the tenant named
// by X-Test-Tenant, as a SuperAdmin when X-Test-Super is set.
func agentScopeRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.CustomAgent{}, &types.UsageSubscription{}, &types.UsageSubscriptionAddon{}))
	require.NoError(t, db.Exec(`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT,
		kind TEXT NOT NULL DEFAULT 'workspace', config_profile_id INTEGER, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, name, kind, config_profile_id) VALUES
		(9, 'Cấu hình chung', 'config_profile', NULL), (1, 'Nhóm thuế', 'workspace', 9), (2, 'Phòng khác', 'workspace', 9)`).Error)
	repo := repository.NewCustomAgentRepository(db)
	require.NoError(t, repo.CreateAgent(context.Background(), &types.CustomAgent{
		ID: "group", Name: "Thuế", TenantID: 9, Distribution: types.AgentDistributionInternal,
		AppliesToTenantIDs: types.AgentTenantScope{1},
		Config:             types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning},
	}))

	h := &CustomAgentHandler{service: service.NewCustomAgentService(repo, nil, nil, nil, nil, nil, nil)}
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		var tenantID uint64 = 2
		if c.GetHeader("X-Test-Tenant") == "1" {
			tenantID = 1
		} else if c.GetHeader("X-Test-Tenant") == "9" {
			tenantID = 9
		}
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenantID)
		caller := types.Caller{TenantID: tenantID, UserID: "user-1", Role: types.TenantRoleAdmin}
		if c.GetHeader("X-Test-Super") != "" {
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
			caller = types.Caller{TenantID: tenantID, UserID: "super-1"}
		}
		c.Request = c.Request.WithContext(types.WithCaller(ctx, caller))
		c.Next()
	})
	router.GET("/agents", h.ListAgents)
	router.GET("/agents/:id", h.GetAgent)
	router.PUT("/agents/:id", h.UpdateAgent)
	router.POST("/agents", h.CreateAgent)
	router.POST("/agents/:id/copy", h.CopyAgent)
	router.PUT("/agents/:id/enabled", h.SetAgentEnabled)
	return router
}

func agentScopeDo(router *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAgentTenantScopeHandlerHidesAgentOutsideList(t *testing.T) {
	router := agentScopeRouter(t)
	outside := map[string]string{"X-Test-Tenant": "2"}
	inside := map[string]string{"X-Test-Tenant": "1"}

	missing := agentScopeDo(router, http.MethodGet, "/agents/no-such-agent", "", outside)
	require.Equal(t, http.StatusNotFound, missing.Code)
	hidden := agentScopeDo(router, http.MethodGet, "/agents/group", "", outside)
	require.Equal(t, http.StatusNotFound, hidden.Code)
	require.JSONEq(t, missing.Body.String(), hidden.Body.String(), "same answer as an id that never existed")

	rec := agentScopeDo(router, http.MethodGet, "/agents/group", "", inside)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"applies_to_tenant_ids":[1]`)

	rec = agentScopeDo(router, http.MethodGet, "/agents", "", outside)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), `"id":"group"`)

	rec = agentScopeDo(router, http.MethodPost, "/agents/group/copy", "", outside)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// a SuperAdmin sees it from any workspace
	rec = agentScopeDo(router, http.MethodGet, "/agents", "", map[string]string{"X-Test-Tenant": "2", "X-Test-Super": "1"})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"id":"group"`)
	rec = agentScopeDo(router, http.MethodGet, "/agents/group", "", map[string]string{"X-Test-Tenant": "2", "X-Test-Super": "1"})
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAgentTenantScopeHandlerValidatesList(t *testing.T) {
	router := agentScopeRouter(t)
	super := map[string]string{"X-Test-Tenant": "9", "X-Test-Super": "1"}
	cfg := `"config":{"agent_mode":"smart-reasoning"}`

	rec := agentScopeDo(router, http.MethodPut, "/agents/group", `{"name":"Thuế",`+cfg+`,"applies_to_tenant_ids":[1,42]}`, super)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "#42", "the message names the offending ids")
	require.NotContains(t, rec.Body.String(), "#1,")
	rec = agentScopeDo(router, http.MethodPost, "/agents", `{"name":"Mới",`+cfg+`,"applies_to_tenant_ids":[9]}`, super)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// absent keeps the list; a sent list replaces it
	rec = agentScopeDo(router, http.MethodPut, "/agents/group", `{"name":"Thuế",`+cfg+`}`, super)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"applies_to_tenant_ids":[1]`)
	rec = agentScopeDo(router, http.MethodPut, "/agents/group", `{"name":"Thuế",`+cfg+`,"applies_to_tenant_ids":[2]}`, super)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = agentScopeDo(router, http.MethodGet, "/agents/group", "", map[string]string{"X-Test-Tenant": "1"})
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec = agentScopeDo(router, http.MethodPost, "/agents", `{"name":"Mới",`+cfg+`}`, super)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created struct {
		Data types.CustomAgent `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Contains(t, rec.Body.String(), `"applies_to_tenant_ids":[]`, "an empty list is an array, not null")
}

func TestAgentDistributionHandler(t *testing.T) {
	router := agentScopeRouter(t)
	super := map[string]string{"X-Test-Tenant": "9", "X-Test-Super": "1"}
	cfg := `"config":{"agent_mode":"smart-reasoning"}`

	// the default on create is answered back
	rec := agentScopeDo(router, http.MethodPost, "/agents", `{"name":"Mới",`+cfg+`}`, super)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"distribution":"basic"`)
	rec = agentScopeDo(router, http.MethodPost, "/agents", `{"name":"Nhóm",`+cfg+`,"applies_to_tenant_ids":[1]}`, super)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"distribution":"internal"`)

	// a list on a basic agent, an unknown value: 400 with the reason
	rec = agentScopeDo(router, http.MethodPost, "/agents",
		`{"name":"Sai",`+cfg+`,"distribution":"basic","applies_to_tenant_ids":[1]}`, super)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "Nội bộ")
	rec = agentScopeDo(router, http.MethodPost, "/agents", `{"name":"Sai",`+cfg+`,"distribution":"all"}`, super)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// switching "group" to addon hides it from workspace 1 (not bought)
	rec = agentScopeDo(router, http.MethodPut, "/agents/group", `{"name":"Thuế",`+cfg+`,"distribution":"addon"}`, super)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"distribution":"addon"`)
	require.Contains(t, rec.Body.String(), `"applies_to_tenant_ids":[]`)
	rec = agentScopeDo(router, http.MethodGet, "/agents/group", "", map[string]string{"X-Test-Tenant": "1"})
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAgentSwitchHandler(t *testing.T) {
	router := agentScopeRouter(t)
	super := map[string]string{"X-Test-Tenant": "9", "X-Test-Super": "1"}
	member := map[string]string{"X-Test-Tenant": "1"}

	rec := agentScopeDo(router, http.MethodPut, "/agents/group/enabled", `{"enabled":false}`, member)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	rec = agentScopeDo(router, http.MethodPut, "/agents/group/enabled", `{}`, super)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	rec = agentScopeDo(router, http.MethodPut, "/agents/nope/enabled", `{"enabled":false}`, super)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	rec = agentScopeDo(router, http.MethodPut, "/agents/group/enabled", `{"enabled":false}`, super)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"enabled":false`)
	rec = agentScopeDo(router, http.MethodGet, "/agents/group", "", member)
	require.Equal(t, http.StatusNotFound, rec.Code, "switched off: hidden from members")

	// only the SuperAdmin's include_disabled lists it
	rec = agentScopeDo(router, http.MethodGet, "/agents?include_disabled=true", "", member)
	require.NotContains(t, rec.Body.String(), `"id":"group"`)
	rec = agentScopeDo(router, http.MethodGet, "/agents", "", map[string]string{"X-Test-Tenant": "1", "X-Test-Super": "1"})
	require.NotContains(t, rec.Body.String(), `"id":"group"`)
	rec = agentScopeDo(router, http.MethodGet, "/agents?include_disabled=true", "", map[string]string{"X-Test-Tenant": "1", "X-Test-Super": "1"})
	require.Contains(t, rec.Body.String(), `"id":"group"`)
}
