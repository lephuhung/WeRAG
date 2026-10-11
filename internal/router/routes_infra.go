package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
)

// Models are platform-owned infrastructure: the catalog (LLM credentials,
// embeddings, rerankers) is defined once and consumed by every tenant.
// Reads stay Viewer+ so tenants can browse the catalog and bind model IDs
// to their KBs/agents; every mutation is SystemAdmin-only, mirrored on the
// API-key axis by the platform-only system_models_manage capability.
func RegisterModelRoutes(
	r *gin.RouterGroup,
	handler *handler.ModelHandler,
	credHandler *handler.ModelCredentialsHandler,
	g *rbacGuards,
) {
	// 模型路由组。读路径：Tenant workspace级基础设施，manage_models / full-access key 可读；
	// 写路径：platform key + system_models_manage capability（见下方 With()）。
	models := g.apiKeyGroup(r.Group("/models"), apiKeyManageModels(apiKeyFullAccess()))
	// 平台级写策略：仅 platform API key 且显式持有 system_models_manage。
	modelWrites := models.With(apiKeyPlatform(types.APIKeyCapabilitySystemModelsManage))
	{
		// 获取模型厂商List  — Owner+ (infra catalog is not a member surface)
		models.GET("/providers", g.SystemAdmin(), handler.ListModelProviders)
		// Create 模型 — SystemAdmin
		modelWrites.POST("", g.SystemAdmin(), g.ConfigProfile(), handler.CreateModel)
		// 获取模型List  — Owner+
		models.GET("", g.SystemAdmin(), handler.ListModels)
		// 调试已保存模型会发起真实上游调用并产生费用 — SystemAdmin
		modelWrites.POST("/:id/debug", g.SystemAdmin(), g.ConfigProfile(), handler.DebugModel)
		// 获取单个模型 — Owner+
		models.GET("/:id", g.SystemAdmin(), handler.GetModel)
		// Update 模型 — SystemAdmin（含 tenant-owned 与 builtin）。
		modelWrites.PUT("/:id", g.SystemAdmin(), g.ConfigProfile(), handler.UpdateModel)
		// Delete 模型 — SystemAdmin
		modelWrites.DELETE("/:id", g.SystemAdmin(), g.ConfigProfile(), handler.DeleteModel)
		// Per-field credential subresource (see internal/handler/model_credentials.go) — SystemAdmin
		modelWrites.PUT("/:id/credentials", g.SystemAdmin(), g.ConfigProfile(), credHandler.Put)
		modelWrites.DELETE("/:id/credentials/:field", g.SystemAdmin(), g.ConfigProfile(), credHandler.DeleteField)
	}

	// Completion through a workspace chat model (the default one unless
	// model_id is given): integrations follow the model the workspace is set
	// to without ever receiving its credentials. Same "chat" scope as the
	// conversation flow; Member+ like the message routes.
	llm := g.apiKeyGroup(r.Group("/llm"), apiKeyChat(apiKeyFullAccess()))
	llm.POST("/chat", g.Member(), handler.LLMChat)
}

// RegisterDocumentFormatRoutes exposes the NĐ30 thể-thức checker. It runs
// the workspace chat model on the caller's own upload, like a chat turn, so
// it shares the "chat" API-key capability and the Member+ role.
func RegisterDocumentFormatRoutes(r *gin.RouterGroup, h *handler.DocumentFormatHandler, g *rbacGuards) {
	df := g.apiKeyGroup(r.Group("/document-format"), apiKeyChat(apiKeyFullAccess()))
	df.GET("/types", g.Member(), h.ListTypes)
	df.POST("/check", g.Member(), h.Check)
}

// Sandbox configs are workspace infrastructure that hold provider credentials.
// Scoped API keys cannot safely receive partial authority over them yet because
// mutation can strand remote sandboxes.
func RegisterSandboxConfigRoutes(
	r *gin.RouterGroup,
	h *handler.SandboxConfigHandler,
	skills *handler.SandboxSkillHandler,
	g *rbacGuards,
) {
	configs := g.apiKeyGroup(r.Group("/sandbox-configs"), apiKeyFullAccess())
	{
		configs.GET("", g.SystemAdmin(), g.ConfigProfile(), h.List)
		configs.PUT("/workspace-policy", g.SystemAdmin(), g.ConfigProfile(), h.SetWorkspacePolicy)
		configs.POST("/templates/query", g.SystemAdmin(), g.ConfigProfile(), h.QueryTemplates)
		configs.POST("", g.SystemAdmin(), g.ConfigProfile(), h.Create)
		configs.GET("/:id", g.SystemAdmin(), g.ConfigProfile(), h.Get)
		configs.PUT("/:id", g.SystemAdmin(), g.ConfigProfile(), h.Update)
		configs.DELETE("/:id", g.SystemAdmin(), g.ConfigProfile(), h.Delete)
		configs.GET("/:id/sandboxes", g.SystemAdmin(), g.ConfigProfile(), h.Inventory)
		// Skills are SystemAdmin throughout, reads included: an upload drives a
		// root shell whose output is baked into the image every session of
		// this config boots, and the listing names what that image carries.
		configs.GET("/:id/skills", g.SystemAdmin(), g.ConfigProfile(), skills.List)
		configs.POST("/:id/skills", g.SystemAdmin(), g.ConfigProfile(), skills.Upload)
		configs.GET("/:id/skills/:skillId", g.SystemAdmin(), g.ConfigProfile(), skills.Get)
		configs.GET("/:id/skills/:skillId/files", g.SystemAdmin(), g.ConfigProfile(), skills.ListFiles)
		configs.GET("/:id/skills/:skillId/files/content", g.SystemAdmin(), g.ConfigProfile(), skills.GetFile)
		configs.POST("/:id/skills/:skillId/reinstall", g.SystemAdmin(), g.ConfigProfile(), skills.Reinstall)
		configs.GET("/:id/skills/:skillId/guidance", g.SystemAdmin(), g.ConfigProfile(), skills.InstallGuidance)
		configs.POST("/:id/skills/:skillId/guidance", g.SystemAdmin(), g.ConfigProfile(), skills.SteerInstall)
		configs.POST("/:id/skills/:skillId/stop", g.SystemAdmin(), g.ConfigProfile(), skills.Stop)
		configs.PATCH("/:id/skills/:skillId", g.SystemAdmin(), g.ConfigProfile(), skills.Patch)
		configs.DELETE("/:id/skills/:skillId", g.SystemAdmin(), g.ConfigProfile(), skills.Delete)
		configs.GET("/:id/skills/:skillId/install-events", g.SystemAdmin(), g.ConfigProfile(), skills.InstallEvents)
		configs.GET("/:id/skills/:skillId/transcript", g.SystemAdmin(), g.ConfigProfile(), skills.InstallTranscript)
	}
}

// RegisterEvaluationRoutes registers evaluation endpoints. Running an
// evaluation drives LLM calls (cost) and reads from KBs across the
// tenant; gate to SystemAdmin until product asks for a finer-grained
// matrix.
func RegisterEvaluationRoutes(r *gin.RouterGroup, handler *handler.EvaluationHandler, g *rbacGuards) {
	evaluationRoutes := g.apiKeyGroup(r.Group("/evaluation"), apiKeyRunEvaluations(apiKeyFullAccess()))
	{
		evaluationRoutes.POST("", g.SystemAdmin(), handler.Evaluation)
		evaluationRoutes.GET("", g.SystemAdmin(), handler.GetEvaluationResult)
	}
}

func RegisterInitializationRoutes(r *gin.RouterGroup, handler *handler.InitializationHandler, g *rbacGuards) {
	// Initialization 接口
	// GetCurrentConfigByKB 是只读，Viewer+ 即可（KB 受限 key 可读其范围内的 KB）。
	g.apiKeyRoute(r, http.MethodGet, "/initialization/config/:kbId",
		apiKeyRetrieve(apiKeyFullAccess()), g.Member(), g.KBAccessRead("kbId"), handler.GetCurrentConfigByKB)
	// InitializeByKB / UpdateKBConfig 都是改 KB 的核心模型/storage Configuration  —
	// 跟 PUT /knowledge-bases/:id 同等敏感，挂同款 OwnedKB 矩阵 + KBAccessWrite
	//（API-key 主体短路 Owned* 守卫，KB allow-list 只能靠 KBAccess 兜底）。
	g.apiKeyRoute(r, http.MethodPost, "/initialization/initialize/:kbId",
		apiKeyManageKnowledgeBases(apiKeyFullAccess()), g.TenantAdmin(), g.KBAccessWrite("kbId"), handler.InitializeByKB)
	g.apiKeyRoute(r, http.MethodPut, "/initialization/config/:kbId",
		apiKeyManageKnowledgeBases(apiKeyFullAccess()), g.TenantAdmin(), g.KBAccessWrite("kbId"), handler.UpdateKBConfig)

	// Ollama / 远程 API / 抽取等模型平台级探测/下载操作。这些不绑某个 KB，
	// 会直接调用上游模型、消耗配额并探测模型基础设施 —— 一律 SystemAdmin；
	// API key 侧仅 platform key + system_models_manage capability 可过。
	modelPlatform := apiKeyPlatform(types.APIKeyCapabilitySystemModelsManage)
	g.apiKeyRoute(r, http.MethodGet, "/initialization/ollama/status", modelPlatform, g.SystemAdmin(), handler.CheckOllamaStatus)
	g.apiKeyRoute(r, http.MethodGet, "/initialization/ollama/models", modelPlatform, g.SystemAdmin(), handler.ListOllamaModels)
	g.apiKeyRoute(r, http.MethodPost, "/initialization/ollama/models/check", modelPlatform, g.SystemAdmin(), handler.CheckOllamaModels)
	g.apiKeyRoute(r, http.MethodPost, "/initialization/ollama/models/download", modelPlatform, g.SystemAdmin(), handler.DownloadOllamaModel)
	g.apiKeyRoute(r, http.MethodGet, "/initialization/ollama/download/progress/:taskId", modelPlatform, g.SystemAdmin(), handler.GetDownloadProgress)
	g.apiKeyRoute(r, http.MethodGet, "/initialization/ollama/download/tasks", modelPlatform, g.SystemAdmin(), handler.ListDownloadTasks)

	// 远程API相关接口 —— SystemAdmin（调用外部模型 provider，属于平台级探测）。
	g.apiKeyRoute(r, http.MethodPost, "/initialization/remote/check", modelPlatform, g.SystemAdmin(), handler.CheckRemoteModel)
	g.apiKeyRoute(r, http.MethodPost, "/initialization/embedding/test", modelPlatform, g.SystemAdmin(), handler.TestEmbeddingModel)
	g.apiKeyRoute(r, http.MethodPost, "/initialization/rerank/check", modelPlatform, g.SystemAdmin(), handler.CheckRerankModel)
	g.apiKeyRoute(r, http.MethodPost, "/initialization/asr/check", modelPlatform, g.SystemAdmin(), handler.CheckASRModel)
	g.apiKeyRoute(r, http.MethodPost, "/initialization/multimodal/test", modelPlatform, g.SystemAdmin(), handler.TestMultimodalFunction)

	g.apiKeyRoute(r, http.MethodPost, "/initialization/extract/text-relation", modelPlatform, g.SystemAdmin(), handler.ExtractTextRelations)
	g.apiKeyRoute(r, http.MethodPost, "/initialization/extract/fabri-tag", modelPlatform, g.SystemAdmin(), handler.FabriTag)
	g.apiKeyRoute(r, http.MethodPost, "/initialization/extract/fabri-text", modelPlatform, g.SystemAdmin(), handler.FabriText)
}

// RegisterMCPServiceRoutes registers MCP service routes.
//
// MCP services are tenant-level integrations (external tool servers); we
// gate reads to Viewer+ and any mutation/test to SystemAdmin. Tool-approval
// resolution is also SystemAdmin since approving a pending tool call grants
// the agent permission to execute side-effecting external commands.
// Credential subresource writes are SystemAdmin as well since secrets are
// tenant-scoped.
func RegisterMCPServiceRoutes(
	r *gin.RouterGroup,
	handler *handler.MCPServiceHandler,
	credHandler *handler.MCPCredentialsHandler,
	oauthHandler *handler.MCPOAuthHandler,
	g *rbacGuards,
) {
	// MCP OAuth provider redirect. Registered OUTSIDE the /mcp-services group
	// to avoid a static-vs-":id" route conflict, and left unauthenticated
	// (allow-listed in middleware/auth.go) because the third-party browser
	// redirect carries no WeKnora bearer — the single-use state authenticates.
	r.GET("/mcp-oauth/callback", oauthHandler.Callback)

	mcpServices := g.apiKeyGroup(r.Group("/mcp-services"), apiKeyManageMCPServices(apiKeyFullAccess()))
	{
		// Create MCP service — SystemAdmin
		mcpServices.POST("", g.SystemAdmin(), g.ConfigProfile(), handler.CreateMCPService)
		// List MCP services — Viewer+
		mcpServices.GET("", g.Member(), handler.ListMCPServices)
		// Get MCP service by ID — Viewer+
		mcpServices.GET("/:id", g.Member(), handler.GetMCPService)
		// Update MCP service — SystemAdmin
		mcpServices.PUT("/:id", g.SystemAdmin(), g.ConfigProfile(), handler.UpdateMCPService)
		// Delete MCP service — SystemAdmin
		mcpServices.DELETE("/:id", g.SystemAdmin(), g.ConfigProfile(), handler.DeleteMCPService)
		// Test MCP service connection — SystemAdmin (probes external infra)
		mcpServices.POST("/:id/test", g.SystemAdmin(), g.ConfigProfile(), handler.TestMCPService)
		// Get MCP service tools — Viewer+
		mcpServices.GET("/:id/tools", g.Member(), handler.GetMCPServiceTools)
		mcpServices.GET("/:id/metadata", g.Member(), handler.GetMCPMetadata)
		// Refresh writes a principal-scoped OAuth snapshot for the caller
		// (Viewer+), or a tenant-wide snapshot for static auth (SystemAdmin in the
		// handler). GET /tools remains Viewer+ and does not persist.
		mcpServices.POST("/:id/metadata/refresh", g.Member(), handler.RefreshMCPMetadata)
		mcpServices.POST("/:id/usage-instructions/generate", g.SystemAdmin(), g.ConfigProfile(), handler.GenerateMCPUsageInstructions)
		// Get MCP service resources — Viewer+
		mcpServices.GET("/:id/resources", g.Member(), handler.GetMCPServiceResources)
		// Per-field credential subresource: secrets never travel via the main
		// PUT body. See internal/handler/mcp_credentials.go for the contract. — SystemAdmin
		mcpServices.PUT("/:id/credentials", g.SystemAdmin(), g.ConfigProfile(), credHandler.Put)
		mcpServices.DELETE("/:id/credentials/:field", g.SystemAdmin(), g.ConfigProfile(), credHandler.DeleteField)
		// MCP tool human approval (issue #1173) — Viewer+ to read, SystemAdmin to set policy
		mcpServices.GET("/:id/tool-approvals", g.Member(), handler.ListMCPToolApprovals)
		mcpServices.PUT("/:id/tool-approvals/:tool_name", g.SystemAdmin(), g.ConfigProfile(), handler.SetMCPToolApproval)
		// Per-user OAuth authorization flow. Viewer+ may authorize/inspect/
		// revoke their own token; the callback is the separate public route
		// registered above.
		mcpServices.POST("/:id/oauth/authorize-url", g.Member(), oauthHandler.AuthorizeURL)
		mcpServices.GET("/:id/oauth/status", g.Member(), oauthHandler.Status)
		mcpServices.DELETE("/:id/oauth/token", g.Member(), oauthHandler.Revoke)
	}

	// /agent tool-approval + OAuth resolution are interactive human flows;
	// not declared for API keys (default-deny).
	agentTool := r.Group("/agent")
	{
		// Resolving a pending tool-approval is gated to tenant members
		// (Viewer+). The approval card surfaces inside an agent chat the
		// caller initiated — restricting it to SystemAdmin blocks the only
		// people who actually have context to approve, so the gate is
		// kept at "anyone in the tenant" instead.
		agentTool.POST("/tool-approvals/:pending_id", g.Member(), handler.ResolveToolApproval)
		// Resume an agent run paused on an in-conversation MCP OAuth prompt.
		// Same tenant-member (Viewer+) gating rationale as tool-approvals.
		agentTool.POST("/mcp-oauth-resolutions/:pending_id", g.Member(), oauthHandler.ResolveMCPOAuth)
		agentTool.POST("/mcp-oauth-resolutions/:pending_id/cancel", g.Member(), oauthHandler.CancelMCPOAuth)
	}
}

// RegisterWebSearchRoutes registers web search routes
func RegisterWebSearchRoutes(r *gin.RouterGroup, webSearchHandler *handler.WebSearchHandler, g *rbacGuards) {
	// Web search providers — Viewer+ (read-only listing of provider catalog).
	webSearch := r.Group("/web-search")
	{
		webSearch.GET("/providers", g.Member(), webSearchHandler.GetProviders)
	}
}

// RegisterWebSearchProviderRoutes registers CRUD routes for web search
// provider configurations.
//
// Provider rows hold external service credentials (Bing, Tavily, Google,
// etc.); reads are Viewer+, all mutations / connection tests (which
// probe external systems with stored credentials) and the per-field
// credential subresource are SystemAdmin.
func RegisterWebSearchProviderRoutes(
	r *gin.RouterGroup,
	h *handler.WebSearchProviderHandler,
	credHandler *handler.WebSearchProviderCredentialsHandler,
	g *rbacGuards,
) {
	providers := g.apiKeyGroup(r.Group("/web-search-providers"), apiKeyManageWebSearch(apiKeyFullAccess()))
	{
		// List available provider types (metadata for UI forms) — Owner+
		providers.GET("/types", g.SystemAdmin(), h.ListProviderTypes)
		// Test with raw credentials (no persistence) — Owner+
		providers.POST("/test", g.SystemAdmin(), g.ConfigProfile(), h.TestProviderRaw)
		// CRUD
		providers.POST("", g.SystemAdmin(), g.ConfigProfile(), h.CreateProvider)
		providers.GET("", g.SystemAdmin(), h.ListProviders)
		providers.GET("/:id", g.SystemAdmin(), h.GetProvider)
		providers.PUT("/:id", g.SystemAdmin(), g.ConfigProfile(), h.UpdateProvider)
		providers.DELETE("/:id", g.SystemAdmin(), g.ConfigProfile(), h.DeleteProvider)
		// Per-field credential subresource — SystemAdmin
		providers.PUT("/:id/credentials", g.SystemAdmin(), g.ConfigProfile(), credHandler.Put)
		providers.DELETE("/:id/credentials/:field", g.SystemAdmin(), g.ConfigProfile(), credHandler.DeleteField)
		// Test existing saved provider — SystemAdmin
		providers.POST("/:id/test", g.SystemAdmin(), g.ConfigProfile(), h.TestProviderByID)
	}
}

// RegisterVectorStoreRoutes registers CRUD routes for vector store configurations.
//
// Vector stores are tenant-level infrastructure; reads are Viewer+, all
// writes (and connection tests, which probe external systems with stored
// credentials) are SystemAdmin.
func RegisterVectorStoreRoutes(r *gin.RouterGroup, h *handler.VectorStoreHandler, g *rbacGuards) {
	stores := g.apiKeyGroup(r.Group("/vector-stores"), apiKeyManageVectorStores(apiKeyFullAccess()))
	{
		// List available engine types (metadata for UI forms) — Owner+
		stores.GET("/types", g.SystemAdmin(), h.ListStoreTypes)
		// Test with raw credentials (no persistence) — Owner+
		stores.POST("/test", g.SystemAdmin(), g.ConfigProfile(), h.TestStoreRaw)
		// CRUD
		stores.POST("", g.SystemAdmin(), g.ConfigProfile(), h.CreateStore)
		stores.GET("", g.SystemAdmin(), h.ListStores)
		stores.GET("/:id", g.SystemAdmin(), h.GetStore)
		stores.PUT("/:id", g.SystemAdmin(), g.ConfigProfile(), h.UpdateStore)
		stores.DELETE("/:id", g.SystemAdmin(), g.ConfigProfile(), h.DeleteStore)
		// Test existing saved or env store — SystemAdmin
		stores.POST("/:id/test", g.SystemAdmin(), g.ConfigProfile(), h.TestStoreByID)
	}
}

// RegisterStorageBackendRoutes manages concrete object/file storage instances.
func RegisterStorageBackendRoutes(r *gin.RouterGroup, h *handler.StorageBackendHandler, g *rbacGuards) {
	backends := g.apiKeyGroup(r.Group("/storage-backends"), apiKeyManageStorageBackends(apiKeyFullAccess()))
	{
		backends.GET("/types", g.SystemAdmin(), h.Types)
		backends.POST("/test", g.SystemAdmin(), g.ConfigProfile(), h.TestRaw)
		backends.POST("", g.SystemAdmin(), g.ConfigProfile(), h.Create)
		backends.GET("", g.SystemAdmin(), h.List)
		backends.GET("/:id", g.SystemAdmin(), h.Get)
		backends.PUT("/:id", g.SystemAdmin(), g.ConfigProfile(), h.Update)
		backends.DELETE("/:id", g.SystemAdmin(), g.ConfigProfile(), h.Delete)
		backends.POST("/:id/test", g.SystemAdmin(), g.ConfigProfile(), h.TestByID)
		backends.PUT("/:id/default", g.SystemAdmin(), g.ConfigProfile(), h.SetDefault)
	}
}

// RegisterDataSourceRoutes 注册数据源相关的路由
//
// Data sources hold external service credentials (Feishu/Notion/Yuque)
// and trigger sync jobs that mutate KB content tenant-wide. Reads are
// Viewer+; everything else (CRUD, validation, sync control, credential
// subresource) is SystemAdmin.
func RegisterDataSourceRoutes(
	r *gin.RouterGroup,
	handler *handler.DataSourceHandler,
	credHandler *handler.DataSourceCredentialsHandler,
	g *rbacGuards,
) {
	// Data source routes
	ds := g.apiKeyGroup(r.Group("/datasource"), apiKeyManageDataSources(apiKeyFullAccess()))
	{
		// Get available connector types — Owner+
		ds.GET("/types", g.SystemAdmin(), handler.GetAvailableConnectors)

		// Validate credentials without persistence (for "Test Connection" button) — SystemAdmin
		ds.POST("/validate-credentials", g.SystemAdmin(), handler.ValidateCredentials)

		// CRUD operations
		ds.POST("", g.SystemAdmin(), handler.CreateDataSource)
		ds.GET("", g.SystemAdmin(), handler.ListDataSources)
		ds.GET("/:id", g.SystemAdmin(), handler.GetDataSource)
		ds.PUT("/:id", g.SystemAdmin(), handler.UpdateDataSource)
		ds.DELETE("/:id", g.SystemAdmin(), handler.DeleteDataSource)

		// Credential subresource. Single logical field "credentials" because
		// connector credentials are a per-connector atomic map (see
		// internal/handler/datasource_credentials.go). — SystemAdmin
		ds.PUT("/:id/credentials", g.SystemAdmin(), credHandler.Put)
		ds.DELETE("/:id/credentials/:field", g.SystemAdmin(), credHandler.DeleteField)

		// Connection and resource management — SystemAdmin
		ds.POST("/:id/validate", g.SystemAdmin(), handler.ValidateConnection)
		ds.GET("/:id/resources", g.SystemAdmin(), handler.ListAvailableResources)
		ds.POST("/:id/resource-ancestors", g.SystemAdmin(), handler.ResolveResourceAncestors)

		// Sync management — SystemAdmin
		ds.POST("/:id/sync", g.SystemAdmin(), handler.ManualSync)
		ds.POST("/:id/pause", g.SystemAdmin(), handler.PauseDataSource)
		ds.POST("/:id/resume", g.SystemAdmin(), handler.ResumeDataSource)

		// Sync logs — Owner+ (read-only audit trail)
		ds.GET("/:id/logs", g.SystemAdmin(), handler.GetSyncLogs)
		ds.GET("/logs/:log_id", g.SystemAdmin(), handler.GetSyncLog)
	}
}

// RegisterWeKnoraCloudRoutes 注册 WeKnoraCloud Initialization 路由
// RegisterWeKnoraCloudRoutes registers the WeKnoraCloud credential
// management endpoints. SaveCredentials persists external SaaS model
// credentials — platform-owned, SystemAdmin-only like /models writes.
// Status is a low-risk readiness probe (Viewer+).
func RegisterWeKnoraCloudRoutes(r *gin.RouterGroup, handler *handler.WeKnoraCloudHandler, g *rbacGuards) {
	g.apiKeyRoute(r, http.MethodPost, "/weknoracloud/credentials", apiKeyPlatform(types.APIKeyCapabilitySystemModelsManage), g.SystemAdmin(), g.ConfigProfile(), handler.SaveCredentials)
	g.apiKeyRoute(r, http.MethodGet, "/models/weknoracloud/status", apiKeyManageModels(apiKeyFullAccess()), g.SystemAdmin(), handler.Status)
}
