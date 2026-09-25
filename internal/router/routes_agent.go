package router

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/embedpolicy"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// RegisterCustomAgentRoutes registers custom agent routes.
//
// Agents bind models, prompts, tools and retrieval scope, so authoring them
// is platform configuration: every mutating route (create, update, delete,
// copy) is SystemAdmin-only. Tenant members — including tenant admins —
// consume agents as response modes; they never author them. Reads stay
// Viewer+ so users can pick a mode in the composer.
func RegisterCustomAgentRoutes(r *gin.RouterGroup, agentHandler *handler.CustomAgentHandler, g *rbacGuards) {
	agents := g.apiKeyGroup(r.Group("/agents"), apiKeyFullAccess())
	// agentsRead are the agent read endpoints. They stay full-access only for
	// plain scoped keys (agent config can carry sensitive model/MCP bindings),
	// but read_agents, chat, or manage_agents may read them.
	agentsRead := agents.With(apiKeyReadAgents(apiKeyManageAgents(apiKeyChat(apiKeyFullAccess()))))
	// agentsWrite are the agent authoring endpoints. SystemAdmin-only on the
	// JWT side; on the API-key axis RequireSystemAdmin admits platform keys,
	// so manage_agents is effectively a platform capability now.
	agentsWrite := agents.With(apiKeyManageAgents(apiKeyFullAccess()))
	{
		// Get placeholder definitions (must be before /:id to avoid conflict) — Viewer+
		agentsRead.GET("/placeholders", g.Member(), agentHandler.GetPlaceholders)
		// List smart-reasoning agent type presets (rag-qa / wiki-qa / hybrid / custom) — Viewer+
		agentsRead.GET("/type-presets", g.Member(), agentHandler.GetAgentTypePresets)
		// Create custom agent — SystemAdmin
		agentsWrite.POST("", g.SystemAdmin(), agentHandler.CreateAgent)
		// List all agents (including built-in) — Viewer+
		agentsRead.GET("", g.Member(), agentHandler.ListAgents)
		// Get agent by ID — Viewer+
		agentsRead.GET("/:id", g.Member(), agentHandler.GetAgent)
		// Update agent — SystemAdmin
		agentsWrite.PUT("/:id", g.SystemAdmin(), agentHandler.UpdateAgent)
		// Delete agent — SystemAdmin
		agentsWrite.DELETE("/:id", g.SystemAdmin(), agentHandler.DeleteAgent)
		// Copy agent — SystemAdmin (a copy is still an authored agent)
		agentsWrite.POST("/:id/copy", g.SystemAdmin(), agentHandler.CopyAgent)
	}
	// Registered outside the group to avoid Gin route conflict with /agents/:id/shares in organization routes
	g.apiKeyRoute(r, http.MethodGet, "/agents/:id/suggested-questions",
		apiKeyReadAgents(apiKeyManageAgents(apiKeyChat(apiKeyFullAccess()))), g.Member(), agentHandler.GetSuggestedQuestions)
}

// RegisterUserFavoriteRoutes wires the per-user starred-resource endpoints.
//
// Authorization: the handler always derives (user_id, tenant_id) from the
// auth context — there is no admin-style "see another user's favorites"
// path — so a Viewer floor is the right gate. The endpoints intentionally
// don't follow the OwnedXOrAdmin pattern: favorites aren't owned by the
// resource's creator, they're owned by the user *doing* the starring.
func RegisterUserFavoriteRoutes(r *gin.RouterGroup, h *handler.UserResourceFavoriteHandler, g *rbacGuards) {
	// Favorites are per-user; not declared for API keys (default-deny).
	favs := r.Group("/user/favorites")
	{
		favs.GET("", g.Member(), h.ListFavorites)
		favs.POST("", g.Member(), h.AddFavorite)
		favs.DELETE("/:type/:id", g.Member(), h.RemoveFavorite)
	}
}

// RegisterSkillRoutes registers skill routes.
//
// PR 2 currently only exposes a read-only `ListSkills`; gated to
// Viewer+. Future skill upload / enable endpoints must use Admin+ since
// skills run sandboxed code on tenant resources.
func RegisterSkillRoutes(r *gin.RouterGroup, skillHandler *handler.SkillHandler, g *rbacGuards) {
	skills := r.Group("/skills")
	{
		// Usable skills for @ mention / chat — Viewer+
		skills.GET("", g.Member(), skillHandler.ListSkills)
		// Catalog reads are Viewer+ so the agent editor can show uninstalled skills.
		skills.GET("/catalog", g.Member(), skillHandler.ListCatalog)
	}
	// Catalog writes bake into sandbox images; scoped API keys cannot hold them.
	catalogWrite := g.apiKeyGroup(r.Group("/skills/catalog"), apiKeyFullAccess())
	{
		catalogWrite.POST("", g.Admin(), skillHandler.RegisterCatalog)
		catalogWrite.POST("/:id/install", g.Admin(), skillHandler.InstallCatalog)
		catalogWrite.GET("/:id/files", g.Admin(), skillHandler.ListCatalogFiles)
		catalogWrite.GET("/:id/files/content", g.Admin(), skillHandler.GetCatalogFile)
		catalogWrite.DELETE("/:id", g.Admin(), skillHandler.DeleteCatalog)
	}
}

// RegisterKBAccessGrantRoutes registers the retired tenant-wide KB
// grant endpoints. Mutations (request/review/revoke) are disabled: they
// return 410 Gone, and the service layer rejects them with
// ErrGrantDisabled as a second line of defence (migration 000111 revoked
// every live row, so the remaining read paths can never authorize
// access). The GET listings stay as read-only audit visibility into the
// revoked rows.
func RegisterKBAccessGrantRoutes(r *gin.RouterGroup, grantHandler *handler.KBAccessGrantHandler, g *rbacGuards) {
	if grantHandler == nil {
		return
	}
	// Grantee side: tenant-wide access requests are retired.
	grants := g.apiKeyGroup(r.Group("/knowledge-bases/:id/access-requests"), apiKeyFullAccess())
	{
		grants.POST("", g.TenantAdmin(), grantHandler.RequestAccessDisabled)
	}

	// Owner side: review/revoke are retired; incoming listing stays.
	tenantGrants := g.apiKeyGroup(r.Group("/tenants/:id/access-grants"), apiKeyFullAccess())
	{
		tenantGrants.GET("", g.TenantAdmin(), grantHandler.ListIncoming)
		tenantGrants.PUT("/:grant_id", g.TenantAdmin(), grantHandler.ReviewDisabled)
		tenantGrants.DELETE("/:grant_id", g.TenantAdmin(), grantHandler.RevokeDisabled)
	}

	// Grantee side listing stays (read-only audit visibility).
	g.apiKeyRoute(r, http.MethodGet, "/access-grants",
		apiKeyFullAccess(), g.TenantAdmin(), grantHandler.ListOutgoing)
}

// RegisterKBInvitationRoutes registers the recipient-bound, read-only KB
// invitation lifecycle (tenant/KB permission plan). This flow is DISTINCT
// from tenant join invitations (RegisterMyInvitationRoutes) and from the
// legacy tenant-wide grant requests above:
//
//   - POST/GET /knowledge-bases/:id/invites: owning-tenant Tenant Admin
//     issues/lists invites for a specific user in another tenant.
//   - DELETE .../:invite_id: owning-tenant Tenant Admin revokes (immediate).
//   - POST /kb-invites/accept + GET /kb-invites: the authenticated named
//     recipient redeems/lists their own invites.
//
// Invite management is not exposed to scoped API keys — only full-access
// keys (and JWT sessions) may change who can read a KB.
func RegisterKBInvitationRoutes(r *gin.RouterGroup, inviteHandler *handler.KBInvitationHandler, g *rbacGuards) {
	if inviteHandler == nil {
		return
	}
	invites := g.apiKeyGroup(r.Group("/knowledge-bases/:id/invites"), apiKeyFullAccess())
	{
		invites.POST("", g.TenantAdmin(), inviteHandler.Issue)
		invites.GET("", g.TenantAdmin(), inviteHandler.ListByKB)
		invites.DELETE("/:invite_id", g.TenantAdmin(), inviteHandler.Revoke)
	}
	g.apiKeyRoute(r, http.MethodPost, "/kb-invites/accept",
		apiKeyFullAccess(), g.Member(), inviteHandler.Accept)
	g.apiKeyRoute(r, http.MethodPost, "/kb-invites/:id/accept",
		apiKeyFullAccess(), g.Member(), inviteHandler.AcceptByID)
	g.apiKeyRoute(r, http.MethodGet, "/kb-invites",
		apiKeyFullAccess(), g.Member(), inviteHandler.ListMine)
}

// RegisterEmbedPublicRoutes registers anonymous embed endpoints secured by publish tokens.
func RegisterEmbedPublicRoutes(
	r *gin.Engine,
	embedHandler *handler.EmbedChannelHandler,
	embedService interfaces.EmbedChannelService,
	tenantService interfaces.TenantService,
	redisClient *redis.Client,
	fileService interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalogs ...interfaces.ResourceCatalog,
) {
	if embedHandler == nil || embedService == nil {
		return
	}
	// Nginx uses this read-only subrequest to put the channel CSP on embed.html.
	// No token is required: framing policy must be available before JS bootstrap.
	r.GET("/api/v1/embed-frame-policy", embedFramePolicyHandler(embedService))
	embed := r.Group("/api/v1/embed/:channel_id", middleware.EmbedAuth(embedService, tenantService, redisClient))
	{
		embed.POST("/exchange", embedHandler.ExchangeEmbedSession)
		embed.GET("/config", embedHandler.GetEmbedConfig)
		embed.GET("/suggested-questions", embedHandler.GetEmbedSuggestedQuestions)
		embed.GET("/chunks/:chunk_id", embedHandler.GetEmbedChunk)
		embed.POST("/sessions", embedHandler.CreateEmbedSession)
		embed.POST("/knowledge-chat/:session_id", embedHandler.EmbedKnowledgeChat)
		embed.POST("/agent-chat/:session_id", embedHandler.EmbedAgentChat)
		embed.GET("/messages/:session_id/load", embedHandler.EmbedLoadMessages)
		embed.POST("/sessions/:session_id/stop", embedHandler.EmbedStopSession)
		embed.GET("/sessions/:session_id/messages/:message_id/suggestions", embedHandler.EmbedGetMessageSuggestions)
		embed.POST("/sessions/:session_id/messages/:message_id/suggestions", embedHandler.EmbedEnsureMessageSuggestions)
		embed.POST("/sessions/:session_id/suggestion-events", embedHandler.EmbedRecordSuggestionEvent)
		embed.POST("/sessions/:session_id/events", embedHandler.EmbedRelayWebhookEvent)
		embed.POST("/sessions/:session_id/mcp-oauth-resolutions/:pending_id", embedHandler.EmbedResolveMCPOAuth)
		embed.POST("/sessions/:session_id/mcp-oauth-resolutions/:pending_id/cancel", embedHandler.EmbedCancelMCPOAuth)
		embed.POST("/sessions/:session_id/mcp-services/:id/oauth/authorize-url", embedHandler.EmbedMCPOAuthAuthorizeURL)
		embed.GET("/sessions/:session_id/mcp-services/:id/oauth/status", embedHandler.EmbedMCPOAuthStatus)
		embed.POST("/sessions/:session_id/tool-approvals/:pending_id", embedHandler.EmbedResolveToolApproval)
		// Serve images embedded in bot replies (e.g. chart exports). EmbedAuth
		// injects the channel's tenant, and the handler enforces that the
		// requested path belongs to that tenant.
		embed.GET("/files", newFileServeHandler(fileService, storageResolver, resourceCatalogs...))
	}
}

// RegisterEmbedChannelRoutes registers authenticated embed channel management routes.
func RegisterEmbedChannelRoutes(r *gin.RouterGroup, embedHandler *handler.EmbedChannelHandler, g *rbacGuards) {
	if embedHandler == nil {
		return
	}
	agentEmbed := g.apiKeyGroup(r.Group("/agents/:id/embed-channels"), apiKeyManageChannels(apiKeyFullAccess()))
	{
		agentEmbed.POST("", g.Admin(), embedHandler.CreateEmbedChannel)
		agentEmbed.GET("", g.Admin(), embedHandler.ListEmbedChannels)
	}
	channels := g.apiKeyGroup(r.Group("/embed-channels"), apiKeyManageChannels(apiKeyFullAccess()))
	{
		channels.GET("", g.Admin(), embedHandler.ListAllEmbedChannels)
		channels.GET("/:channel_id", g.Admin(), embedHandler.GetEmbedChannel)
		channels.PUT("/:channel_id", g.Admin(), embedHandler.UpdateEmbedChannel)
		channels.DELETE("/:channel_id", g.Admin(), embedHandler.DeleteEmbedChannel)
		channels.POST("/:channel_id/rotate-token", g.Admin(), embedHandler.RotateEmbedToken)
		channels.POST("/:channel_id/preview-session", g.Admin(), embedHandler.IssuePreviewSession)
		channels.GET("/:channel_id/stats", g.Admin(), embedHandler.GetEmbedChannelStats)
	}
}

// RegisterIMRoutes registers IM callback routes.
// These are registered BEFORE auth middleware since IM platforms use their own signature verification.
func RegisterIMRoutes(r *gin.Engine, imHandler *handler.IMHandler) {
	im := r.Group("/api/v1/im")
	{
		im.GET("/callback/:channel_id", imHandler.IMCallback)
		im.POST("/callback/:channel_id", imHandler.IMCallback)
	}
}

// RegisterIMChannelRoutes registers IM channel CRUD routes (requires authentication).
//
// IM channels carry external bot credentials (WeChat/Feishu/Slack/...);
// listing is Viewer+ but any mutation, toggle, or QR-code login flow
// (which can hijack a personal WeChat session) is Admin+.
func RegisterIMChannelRoutes(r *gin.RouterGroup, imHandler *handler.IMHandler, g *rbacGuards) {
	// Channel CRUD under agents
	agentChannels := g.apiKeyGroup(r.Group("/agents/:id/im-channels"), apiKeyManageChannels(apiKeyFullAccess()))
	{
		agentChannels.POST("", g.Admin(), imHandler.CreateIMChannel)
		agentChannels.GET("", g.Admin(), imHandler.ListIMChannels)
	}

	// Channel operations by channel ID
	channels := g.apiKeyGroup(r.Group("/im-channels"), apiKeyManageChannels(apiKeyFullAccess()))
	{
		channels.GET("", g.Admin(), imHandler.ListAllIMChannels)
		channels.PUT("/:id", g.Admin(), imHandler.UpdateIMChannel)
		channels.DELETE("/:id", g.Admin(), imHandler.DeleteIMChannel)
		channels.POST("/:id/toggle", g.Admin(), imHandler.ToggleIMChannel)
	}

	// WeChat QR code login (requires authentication) — Admin+: a successful
	// scan binds a personal WeChat account to the tenant.
	wechatGroup := g.apiKeyGroup(r.Group("/wechat"), apiKeyManageChannels(apiKeyFullAccess()))
	{
		wechatGroup.POST("/qrcode", g.Admin(), imHandler.WeChatGetQRCode)
		wechatGroup.POST("/qrcode/status", g.Admin(), imHandler.WeChatPollQRCodeStatus)
	}
}

// embedChannelIDFromPath extracts the channel id from an /embed/:channelID path.
func embedChannelIDFromPath(path string) string {
	const prefix = "/embed/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/")
	// Never authorize the first segment of a path that the browser/API can
	// normalize to a different channel (including encoded slashes or dot paths).
	if rest == "" || rest == "." || rest == ".." ||
		strings.TrimSpace(rest) != rest || strings.ContainsAny(rest, "/\\%?#") {
		return ""
	}
	return rest
}

// embedFramePolicyHandler serves only framing policy, never channel config.
func embedFramePolicyHandler(svc interfaces.EmbedChannelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Security-Policy", "frame-ancestors 'none'")
		u, err := url.ParseRequestURI(c.GetHeader("X-Embed-Page-URI"))
		if err != nil || u.IsAbs() || u.Host != "" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		channelID := embedChannelIDFromPath(u.Path)
		if channelID == "" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		ch, err := svc.LookupEnabledChannel(c.Request.Context(), channelID)
		if err != nil || ch == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		policy := embedpolicy.FrameAncestors(ch.AllowedOriginsList())
		c.Header("Content-Security-Policy", policy)
		if policy == "frame-ancestors 'none'" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// embedFrameAncestorsMiddleware applies the same policy to Lite's HTML response.
func embedFrameAncestorsMiddleware(svc interfaces.EmbedChannelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		if !strings.HasPrefix(c.Request.URL.Path, "/embed/") {
			c.Next()
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Security-Policy", "frame-ancestors 'none'")
		channelID := embedChannelIDFromPath(c.Request.URL.Path)
		if channelID == "" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		ch, err := svc.LookupEnabledChannel(c.Request.Context(), channelID)
		if err != nil || ch == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		policy := embedpolicy.FrameAncestors(ch.AllowedOriginsList())
		c.Header("Content-Security-Policy", policy)
		if policy == "frame-ancestors 'none'" {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
