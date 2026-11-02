package handler

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func domainPackRouter(t *testing.T, repo *agentAvatarRepository) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	registry, err := domainpacks.Load(os.DirFS("../agent/domainpacks/testdata"))
	require.NoError(t, err)
	h := &CustomAgentHandler{service: service.NewCustomAgentService(repo, nil, nil, nil, nil, nil, nil), domainPacks: registry}
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.GET("/agents/domain-packs", h.ListDomainPacks)
	router.POST("/agents", h.CreateAgent)
	router.PUT("/agents/:id", h.UpdateAgent)
	return router
}

func TestCustomAgentDomainPackValidationOnSave(t *testing.T) {
	for _, tc := range []struct {
		name, method, config, want string
		status                     int
	}{
		{"create ok", "POST", `{"agent_mode":"smart-reasoning","allowed_tools":["thinking"],"domain_packs":["sample-pack"]}`, "", 201},
		{"update ok", "PUT", `{"agent_mode":"smart-reasoning","allowed_tools":["thinking"],"domain_packs":["sample-pack"]}`, "", 200},
		{"unknown pack", "POST", `{"agent_mode":"smart-reasoning","allowed_tools":["thinking"],"domain_packs":["gone"]}`, "không tồn tại", 400},
		{"duplicate pack", "PUT", `{"agent_mode":"smart-reasoning","allowed_tools":["thinking"],"domain_packs":["sample-pack","sample-pack"]}`, "hai lần", 400},
		{"quick answer", "POST", `{"agent_mode":"quick-answer","domain_packs":["sample-pack"]}`, "smart-reasoning", 400},
		{"missing tool", "PUT", `{"agent_mode":"smart-reasoning","allowed_tools":["search_knowledge"],"domain_packs":["sample-pack"]}`, `cần công cụ \"thinking\"`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &agentAvatarRepository{agent: &types.CustomAgent{ID: "custom-1", Name: "old", TenantID: 1}}
			path := "/agents/custom-1"
			if tc.method == http.MethodPost {
				path = "/agents"
			}
			body := `{"name":"agent","config":` + tc.config + `}`
			req := httptest.NewRequest(tc.method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), types.TenantIDContextKey, uint64(1)))
			rec := httptest.NewRecorder()
			domainPackRouter(t, repo).ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.want != "" {
				require.Contains(t, rec.Body.String(), tc.want)
				require.Zero(t, repo.writes, "a refused selection is not stored")
			} else {
				require.Equal(t, []string{"sample-pack"}, repo.agent.Config.DomainPacks)
			}
		})
	}
}

func TestListDomainPacks(t *testing.T) {
	rec := httptest.NewRecorder()
	domainPackRouter(t, &agentAvatarRepository{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/domain-packs", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Data []DomainPackInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	got := response.Data[0]
	require.Equal(t, "sample-pack", got.Name)
	require.Equal(t, "1.0.0", got.Version)
	require.Equal(t, "2026-10-17", got.AsOf)
	require.Equal(t, "2027-04-17", got.ReviewBy)
	require.Equal(t, []string{"thinking"}, got.RequiredTools)
	require.Equal(t, "doanh-nghiep-nho", got.Domain)
	require.NotEmpty(t, got.Attribution)
	registry, err := domainpacks.Load(os.DirFS("../agent/domainpacks/testdata"))
	require.NoError(t, err)
	pack, _ := registry.Get("sample-pack")
	require.Equal(t, pack.Overdue(types.PromptNow()), got.Overdue)
}

// searchPackRegistry ships the sample pack changed to require search_knowledge.
func searchPackRegistry(t *testing.T) *domainpacks.Registry {
	t.Helper()
	src := os.DirFS("../agent/domainpacks/testdata")
	fsys := fstest.MapFS{}
	for _, rel := range []string{"SKILL.md", "catalogue.yaml", "ATTRIBUTION.md", "references/a.md", "assets/checklist.md"} {
		data, err := fs.ReadFile(src, "sample-pack/"+rel)
		require.NoError(t, err)
		if rel == "SKILL.md" {
			data = []byte(strings.Replace(string(data), "required_tools: [thinking]", "required_tools: [search_knowledge]", 1))
		}
		fsys["sample-pack/"+rel] = &fstest.MapFile{Data: data, Mode: 0o644}
	}
	registry, err := domainpacks.Load(fsys)
	require.NoError(t, err)
	return registry
}

func TestDomainPackSaveCheckMatchesRunTime(t *testing.T) {
	h := &CustomAgentHandler{domainPacks: searchPackRegistry(t)}
	cfg := types.CustomAgentConfig{
		AgentMode:       types.AgentModeSmartReasoning,
		KBSelectionMode: "selected",
		KnowledgeBases:  []string{"kb-1"},
		// a retired name the run time reads as search_knowledge
		AllowedTools: []string{"grep_chunks"},
		DomainPacks:  []string{"sample-pack"},
	}
	require.NoError(t, h.validateDomainPacks(cfg))
	require.Equal(t, []string{"grep_chunks"}, cfg.AllowedTools, "the stored list is not rewritten")

	cfg.KnowledgeBases = nil
	err := h.validateDomainPacks(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "kho tri thức")
}

func TestCopyAgentDropsUnshippedDomainPacks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry, err := domainpacks.Load(os.DirFS("../agent/domainpacks/testdata"))
	require.NoError(t, err)
	repo := &agentAvatarRepository{agent: &types.CustomAgent{ID: "custom-1", Name: "src", TenantID: 1,
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning, DomainPacks: []string{"gone", "sample-pack"}}}}
	svc := service.NewCustomAgentService(repo, nil, nil, nil, nil, nil, nil)
	svc.(interface{ SetDomainPacks(*domainpacks.Registry) }).SetDomainPacks(registry)
	h := &CustomAgentHandler{service: svc, domainPacks: registry}
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.POST("/agents/:id/copy", h.CopyAgent)
	req := httptest.NewRequest(http.MethodPost, "/agents/custom-1/copy", nil)
	req = req.WithContext(context.WithValue(req.Context(), types.TenantIDContextKey, uint64(1)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, []string{"sample-pack"}, repo.agent.Config.DomainPacks)
}
