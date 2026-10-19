package router

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/dig"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/mcpserver"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types/interfaces"

	_ "github.com/Tencent/WeKnora/docs" // swagger docs
)

// RouterParams là tham số của router
type RouterParams struct {
	dig.In

	Config                  *config.Config
	FileService             interfaces.FileService
	UserService             interfaces.UserService
	KBService               interfaces.KnowledgeBaseService
	KnowledgeService        interfaces.KnowledgeService
	ChunkService            interfaces.ChunkService
	SessionService          interfaces.SessionService
	MessageService          interfaces.MessageService
	ModelService            interfaces.ModelService
	EvaluationService       interfaces.EvaluationService
	KBAccessGrantService    interfaces.KBAccessGrantService
	KBInvitationService     interfaces.KBInvitationService
	KBHandler               *handler.KnowledgeBaseHandler
	KnowledgeHandler        *handler.KnowledgeHandler
	TenantHandler           *handler.TenantHandler
	TenantService           interfaces.TenantService
	TenantAPIKeyService     interfaces.TenantAPIKeyService
	TenantMemberService     interfaces.TenantMemberService
	TenantMemberHandler     *handler.TenantMemberHandler
	TenantInvitationHandler *handler.TenantInvitationHandler

	AuditLogHandler             *handler.AuditLogHandler
	AuditLogService             interfaces.AuditLogService
	ChunkHandler                *handler.ChunkHandler
	SessionHandler              *session.Handler
	DocumentWorkspaceHandler    *session.DocumentWorkspaceHandler
	MessageHandler              *handler.MessageHandler
	MessageSuggestionHandler    *handler.MessageSuggestionHandler
	ModelHandler                *handler.ModelHandler
	ModelCredentialsHandler     *handler.ModelCredentialsHandler
	DocumentFormatHandler       *handler.DocumentFormatHandler
	SandboxConfigHandler        *handler.SandboxConfigHandler
	SandboxSkillHandler         *handler.SandboxSkillHandler
	MeEnvVarHandler             *handler.MeEnvVarHandler
	MeUsageHandler              *handler.MeUsageHandler
	UsageAdminHandler           *handler.UsageAdminHandler
	TenantUsageHandler          *handler.TenantUsageHandler
	UsageBillingHandler         *handler.UsageBillingHandler
	EvaluationHandler           *handler.EvaluationHandler
	AuthHandler                 *handler.AuthHandler
	InitializationHandler       *handler.InitializationHandler
	SystemHandler               *handler.SystemHandler
	StatsHandler                *handler.StatsHandler
	MCPServiceHandler           *handler.MCPServiceHandler
	MCPCredentialsHandler       *handler.MCPCredentialsHandler
	MCPOAuthHandler             *handler.MCPOAuthHandler
	WebSearchHandler            *handler.WebSearchHandler
	WebSearchProviderHandler    *handler.WebSearchProviderHandler
	WebSearchCredentialsHandler *handler.WebSearchProviderCredentialsHandler
	VectorStoreHandler          *handler.VectorStoreHandler
	StorageBackendHandler       *handler.StorageBackendHandler
	StorageBackendResolver      interfaces.StorageBackendResolver
	ResourceCatalog             interfaces.ResourceCatalog
	FAQHandler                  *handler.FAQHandler
	TagHandler                  *handler.TagHandler
	CustomAgentHandler          *handler.CustomAgentHandler
	UserFavoriteHandler         *handler.UserResourceFavoriteHandler
	SkillHandler                *handler.SkillHandler
	KBAccessGrantHandler        *handler.KBAccessGrantHandler
	KBInvitationHandler         *handler.KBInvitationHandler
	KBAccessOverviewHandler     *handler.KBAccessOverviewHandler
	IMHandler                   *handler.IMHandler
	EmbedChannelHandler         *handler.EmbedChannelHandler
	EmbedChannelService         interfaces.EmbedChannelService
	MCPEndpointHandler          *handler.MCPEndpointHandler
	MCPEndpointService          interfaces.MCPEndpointService
	MCPServer                   *mcpserver.Server
	RedisClient                 *redis.Client
	WeKnoraCloudHandler         *handler.WeKnoraCloudHandler
	WikiPageHandler             *handler.WikiPageHandler
	WikiCatalogueHandler        *handler.WikiCatalogueHandler
	MemoryHandler               *handler.MemoryHandler
	AbbreviationHandler         *handler.AbbreviationHandler
	AgencyProfileHandler        *handler.AgencyProfileHandler
	TemplateStyleHandler        *handler.TemplateStyleHandler
	LegalReviewHandler          *handler.LegalReviewHandler
	LegalRequestHandler         *handler.LegalRequestHandler
	LegalDraftHandler           *handler.LegalDraftHandler
	DomainPackReconcileHandler  *handler.DomainPackReconcileHandler
	KnowledgeGraphViewHandler   *handler.KnowledgeGraphViewHandler
}

// NewRouter tạo router mới
func NewRouter(params RouterParams) *gin.Engine {
	r := gin.New()
	r.ContextWithFallback = true

	// Trusted proxies: gin defaults to trusting ALL proxies, which makes
	// c.ClientIP() honor a client-supplied X-Forwarded-For. Public, unauthed
	// embed endpoints rate-limit per (channel, ClientIP), so a spoofed XFF would
	// trivially bypass the limiter. Restrict to the fronting proxy network so
	// only the real client IP (appended by nginx) is returned. Configurable via
	// WEKNORA_TRUSTED_PROXIES (comma-separated CIDRs/IPs).
	if err := r.SetTrustedProxies(trustedProxies()); err != nil {
		logger.Errorf(context.Background(), "[Router] failed to set trusted proxies: %v", err)
	}

	// Middleware CORS phải đặt trước tiên.
	// Lưu ý: với AllowOrigins là wildcard, trình duyệt từ chối mọi yêu cầu cross-origin kèm thông tin xác thực (cookie)
	// (đặc tả CORS cấm kết hợp "*" với credentials), nên AllowCredentials
	// thực tế chỉ có tác dụng khi sau này đổi sang phản hồi Origin cụ thể; hiện xác thực đều qua header
	// Authorization / X-API-Key tường minh, không dựa vào thông tin xác thực ambient. Nếu đưa vào xác thực
	// bằng cookie, phải đổi AllowOrigins sang danh sách được kiểm soát trước.
	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key", "X-Request-ID", "X-Tenant-ID",
			"X-Embed-Session", "X-External-User-ID", "X-External-User-Token", "X-WeKnora-Desktop-Token",
			// Streamable HTTP MCP clients running in a browser send these on
			// the /mcp/:endpoint_id surface.
			"MCP-Protocol-Version", "Mcp-Session-Id", "Last-Event-ID",
			// Scripted clients that must stay out of long-term memory.
			middleware.MemoryOptOutHeader,
		},
		ExposeHeaders:    []string{"Content-Length", "Access-Control-Allow-Origin", "Mcp-Session-Id"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Middleware cơ bản (không cần xác thực)
	r.Use(middleware.RequestID())
	r.Use(middleware.Language())
	r.Use(middleware.MemoryOptOut())
	r.Use(middleware.Logger())
	r.Use(middleware.Recovery())
	r.Use(middleware.ErrorHandler())

	// Kiểm tra sức khoẻ (không cần xác thực)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Tài liệu Swagger API (chỉ bật ngoài môi trường production)
	// Xét theo biến môi trường GIN_MODE: chế độ release tắt Swagger
	if gin.Mode() != gin.ReleaseMode {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler,
			ginSwagger.DefaultModelsExpandDepth(-1), // mặc định thu gọn Models
			ginSwagger.DocExpansion("list"),         // chế độ mở rộng: "list" (mở các tag), "full" (mở hết), "none" (thu gọn hết)
			ginSwagger.DeepLinking(true),            // bật deep link
			ginSwagger.PersistAuthorization(true),   // lưu lại thông tin xác thực
		))
	}

	// Embed page framing policy: emit a per-channel `frame-ancestors` CSP so the
	// embed SPA page (/embed/:channelId) can only be iframed by the channel's
	// allowed origins. This is the page-level counterpart to the API Origin
	// allowlist enforced in EmbedAuth. Registered before the static handler so
	// it runs for the embed HTML response.
	if params.EmbedChannelService != nil {
		r.Use(embedFrameAncestorsMiddleware(params.EmbedChannelService))
	}

	// Route callback IM (đăng ký trước middleware xác thực, dùng xác minh chữ ký riêng của từng nền tảng)
	RegisterIMRoutes(r, params.IMHandler)

	// Route công khai của web embed (xác thực bằng publish token, không qua Auth toàn cục)
	RegisterEmbedPublicRoutes(
		r,
		params.EmbedChannelHandler,
		params.EmbedChannelService,
		params.TenantService,
		params.RedisClient,
		params.FileService,
		params.StorageBackendResolver,
		params.ResourceCatalog,
	)

	// Workspace MCP server surface (/mcp/:endpoint_id): bearer-token auth per
	// endpoint, so it must precede the global Auth middleware.
	RegisterMCPServerRoutes(r, params.MCPServer, params.MCPEndpointService, params.TenantService)

	// Short-lived capability URLs for IM and other clients that cannot attach
	// WeKnora authentication headers.
	serveResourceGrants(r, params.ResourceCatalog, params.TenantService, params.FileService, params.StorageBackendResolver)

	// ONLYOFFICE Document Server save callbacks (self-authenticated: signed
	// ticket in the path + DS JWT on the body).
	RegisterOnlyOfficeCallbackRoutes(r, params.DocumentWorkspaceHandler)

	// Sandbox terminal WebSocket (self-authenticated via a short-lived
	// query ticket — see RegisterSandboxTerminalRoutes; browsers cannot set
	// auth headers on the WS handshake, so this must precede the global Auth
	// middleware). The ticket is minted by an authenticated POST.
	RegisterSandboxTerminalRoutes(r, params.SessionHandler)
	RegisterSandboxDesktopRoutes(r, params.SessionHandler)
	r.GET("/api/v1/local-browser/extension", params.SessionHandler.BrowserSkillExtension)
	r.POST("/api/v1/local-browser/extension/authorize", params.SessionHandler.BrowserSkillAuthorize)
	r.POST("/api/v1/local-browser/internal", params.SessionHandler.BrowserSkillInternal)

	// Read-only catalogue for anonymous visitors to the landing page.
	RegisterPublicBillingRoutes(r, params.UsageBillingHandler)

	// Middleware xác thực
	r.Use(middleware.Auth(params.TenantService, params.UserService, params.TenantMemberService, params.TenantAPIKeyService, params.Config))

	// Dịch vụ tệp: proxy thống nhất cho backend lưu trữ local/MinIO/COS/TOS (cần xác thực)
	serveFilesWithKBGate(r, params.FileService, params.StorageBackendResolver, params.ResourceCatalog,
		access.NewKBFileGate(params.ResourceCatalog, params.KBService, params.KBAccessGrantService))

	// Presigned file access: no auth required, signature-verified.
	servePresignedFiles(r, params.TenantService, params.StorageBackendResolver)

	// Diagnostic preview of presigned URLs (Admin only, behind auth middleware).
	servePresignedPreview(r, params.Config, params.StorageBackendResolver, params.ResourceCatalog)

	// Langfuse observability — only active when LANGFUSE_* env vars are set.
	// The middleware is registered unconditionally; when disabled it's a no-op.
	r.Use(langfuse.GinMiddleware())

	// Audit log injection — middleware/rbac.go's reject paths and the
	// admin-only /tenants/:id/audit-log endpoint pull the service out
	// of the gin context. Provider is a no-op when AuditLogService is
	// nil (e.g. lite mode without DB), so the rbac path degrades to
	// "log to stderr only" instead of crashing.
	r.Use(middleware.AuditServiceProvider(params.AuditLogService))

	// Các route API cần xác thực
	v1 := r.Group("/api/v1")
	{
		// rbacGuards bundles the role-gating middleware factories so each
		// Register* function below can attach the right guard without
		// taking a *config.Config dependency directly. The guards honour
		// cfg.Tenant.EnableRBAC: when false, they log but pass through,
		// preserving today's behaviour during the rollout window.
		rbacGuards := newRBACGuards(
			params.Config,
			params.KBHandler,
			params.CustomAgentHandler,
			params.KnowledgeHandler,
			params.ChunkHandler,
			params.WikiPageHandler,
			params.KBService,
			params.KnowledgeService,
			params.ChunkService,
			params.KBAccessGrantService,
			params.KBInvitationService,
		)
		rbacGuards.tenantService = params.TenantService

		// API-key gate: single authority for X-API-Key principals. Runs
		// first on every /api/v1 route (JWT sessions pass straight
		// through) and denies any route not explicitly declared via the
		// apiKeyGroup helpers. Must be attached BEFORE the Register* calls
		// so that sub-groups inherit it.
		v1.Use(rbacGuards.apiKeyAuthorizer.Middleware())

		RegisterAuthRoutes(v1, params.AuthHandler, rbacGuards)
		RegisterTenantRoutes(v1, params.TenantHandler, params.TenantMemberHandler, params.TenantInvitationHandler, params.AuditLogHandler, rbacGuards)
		RegisterTenantUsageRoutes(v1, params.TenantUsageHandler, rbacGuards)
		RegisterBillingRoutes(v1, params.UsageBillingHandler, rbacGuards)
		RegisterMyInvitationRoutes(v1, params.TenantInvitationHandler)
		RegisterKnowledgeBaseRoutes(v1, params.KBHandler, rbacGuards)
		RegisterKnowledgeBaseActivityRoutes(v1, params.AuditLogHandler, rbacGuards)
		RegisterKnowledgeGraphViewRoutes(v1, params.KnowledgeGraphViewHandler, rbacGuards)
		// KB-scoped image proxy: lets tenants render images embedded in
		// org-shared / agent-visible KB content, which the tenant-scoped
		// /files route cannot serve because it enforces same-tenant paths.
		serveKBScopedFiles(
			v1,
			rbacGuards,
			params.TenantService,
			params.FileService,
			params.StorageBackendResolver,
			params.ResourceCatalog,
		)
		// Message-scoped image proxy: replies may reference resources stored
		// in a granted KB's workspace. Authorization is derived from the
		// persisted message, never from a client-provided workspace ID;
		// granted-KB evidence is the cross-tenant fallback.
		serveMessageScopedFiles(
			v1,
			rbacGuards,
			params.MessageService,
			params.TenantService,
			params.FileService,
			params.StorageBackendResolver,
			params.ResourceCatalog,
			params.KBAccessGrantService,
			params.KBService,
			params.KnowledgeService,
		)
		RegisterKnowledgeTagRoutes(v1, params.TagHandler, rbacGuards)
		RegisterKnowledgeRoutes(v1, params.KnowledgeHandler, rbacGuards)
		RegisterFAQRoutes(v1, params.FAQHandler, rbacGuards)
		RegisterChunkRoutes(v1, params.ChunkHandler, rbacGuards)
		RegisterSessionRoutes(v1, params.SessionHandler, params.MessageSuggestionHandler, rbacGuards)
		RegisterDocumentWorkspaceRoutes(v1, params.DocumentWorkspaceHandler, rbacGuards)
		RegisterChatRoutes(v1, params.SessionHandler, rbacGuards)
		RegisterMessageRoutes(v1, params.MessageHandler, rbacGuards)
		RegisterModelRoutes(v1, params.ModelHandler, params.ModelCredentialsHandler, rbacGuards)
		RegisterDocumentFormatRoutes(v1, params.DocumentFormatHandler, rbacGuards)
		RegisterSandboxConfigRoutes(v1, params.SandboxConfigHandler, params.SandboxSkillHandler, rbacGuards)
		RegisterMyEnvVarRoutes(v1, params.MeEnvVarHandler, rbacGuards)
		// Own usage counters (usage packages phase 1). No role gate: the
		// handler reads whose counters from the context only.
		if params.MeUsageHandler != nil {
			v1.GET("/me/usage", params.MeUsageHandler.Get)
		}
		v1.GET("/me/browser", params.SessionHandler.BrowserSkillAccount)
		v1.GET("/me/browser/extension", params.SessionHandler.BrowserSkillDownload)
		v1.POST("/me/browser", params.SessionHandler.BrowserSkillAccount)
		RegisterEvaluationRoutes(v1, params.EvaluationHandler, rbacGuards)
		RegisterInitializationRoutes(v1, params.InitializationHandler, rbacGuards)
		params.SystemHandler.BindDeploymentCapabilities(deploymentCapabilitiesFromRouter(params))
		RegisterSystemRoutes(v1, params.SystemHandler, rbacGuards)
		RegisterSystemAdminRoutes(v1, params.SystemHandler, params.StatsHandler, params.AuditLogHandler, params.UsageAdminHandler, rbacGuards)
		RegisterMCPServiceRoutes(v1, params.MCPServiceHandler, params.MCPCredentialsHandler, params.MCPOAuthHandler, rbacGuards)
		RegisterWebSearchRoutes(v1, params.WebSearchHandler, rbacGuards)
		RegisterWebSearchProviderRoutes(v1, params.WebSearchProviderHandler, params.WebSearchCredentialsHandler, rbacGuards)
		RegisterVectorStoreRoutes(v1, params.VectorStoreHandler, rbacGuards)
		RegisterStorageBackendRoutes(v1, params.StorageBackendHandler, rbacGuards)
		RegisterCustomAgentRoutes(v1, params.CustomAgentHandler, rbacGuards)
		RegisterUserFavoriteRoutes(v1, params.UserFavoriteHandler, rbacGuards)
		RegisterSkillRoutes(v1, params.SkillHandler, rbacGuards)
		RegisterKBAccessGrantRoutes(v1, params.KBAccessGrantHandler, rbacGuards)
		RegisterKBInvitationRoutes(v1, params.KBInvitationHandler, rbacGuards)
		RegisterKBAccessOverviewRoutes(v1, params.KBAccessOverviewHandler, rbacGuards)
		RegisterIMChannelRoutes(v1, params.IMHandler, rbacGuards)
		RegisterEmbedChannelRoutes(v1, params.EmbedChannelHandler, rbacGuards)
		RegisterMCPEndpointRoutes(v1, params.MCPEndpointHandler, rbacGuards)
		RegisterWeKnoraCloudRoutes(v1, params.WeKnoraCloudHandler, rbacGuards)
		RegisterWikiPageRoutes(v1, params.WikiPageHandler, params.WikiCatalogueHandler, rbacGuards)
		RegisterMemoryRoutes(v1, params.MemoryHandler, rbacGuards)
		RegisterAbbreviationRoutes(v1, params.AbbreviationHandler, rbacGuards)
		RegisterAgencyProfileRoutes(v1, params.AgencyProfileHandler, rbacGuards)
		RegisterTemplateStyleRoutes(v1, params.TemplateStyleHandler, rbacGuards)
		RegisterLegalReviewRoutes(v1, params.LegalReviewHandler, rbacGuards)
		RegisterLegalRequestRoutes(v1, params.LegalRequestHandler, rbacGuards)
		RegisterLegalDraftRoutes(v1, params.LegalDraftHandler, rbacGuards)
		RegisterDomainPackReconcileRoutes(v1, params.DomainPackReconcileHandler, rbacGuards)
		RegisterChunkerDebugRoutes(v1, rbacGuards)

		// Fail fast if any declared API-key policy points at a route
		// template that does not actually exist (typo / path drift). A
		// stale template would silently 403 every API key on that route,
		// so we panic at startup instead of shipping a dead policy.
		rbacGuards.assertAPIKeyPoliciesMatchRoutes(r)
	}

	return r
}

// trustedProxies returns the proxy CIDRs/IPs whose X-Forwarded-For headers
// gin should trust when resolving the client IP. Defaults to loopback and
// private ranges (covers the bundled nginx in a container network); override
// with WEKNORA_TRUSTED_PROXIES (comma-separated). An explicit empty value
// disables proxy trust entirely so ClientIP() returns the direct peer.
func trustedProxies() []string {
	raw, ok := os.LookupEnv("WEKNORA_TRUSTED_PROXIES")
	if !ok {
		return []string{
			"127.0.0.0/8",
			"::1/128",
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"fc00::/7",
		}
	}
	proxies := make([]string, 0)
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	return proxies
}
