package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

// RegisterTenantRoutes 注册Tenant workspace相关的路由
//
// Tenant-internal RBAC for /tenants/:id:
//   - GET   /:id          Viewer+ (read tenant settings)
//   - PUT   /:id          Owner+ (mutate tenant config)
//   - DELETE /:id         Owner+ (also normally a CanAccessAllTenants op)
//   - GET/POST/PUT/DELETE /:id/api-keys   Owner+ (scoped API key management)
//   - GET    /:id/members            Viewer+ (any member can see who else is in)
//   - POST   /:id/members            Admin+ (membership management is the admin duty;
//     the owner role itself still requires Owner — enforced in the handler)
//   - PUT    /:id/members/:user_id   Admin+ (same owner-protection caveat)
//   - DELETE /:id/members/:user_id   Admin+ (same owner-protection caveat)
//   - POST   /:id/leave              Viewer+ (any member can quit on their own)
//
// All /tenants/:id endpoints share g.PathTenantMatch() at the group
// level: middleware/access.go enforces "URL :id == active tenant"
// (with the cross-tenant superuser carve-out) so an Owner-of-A cannot
// drive operations against tenant B by changing the URL. This used to
// be authorizeTenantAccess in tenant.go and resolveTenantIDFromPath in
// tenant_member.go; collapsing it into one route guard means the
// declaration itself documents the rule.
//
// Cross-tenant superuser endpoints (/tenants/all, /tenants/search) use
// g.CrossTenant(): RequireCrossTenantAccess in access.go combines the
// CanAccessAllTenants user attribute with the cluster-wide
// EnableCrossTenantAccess flag, replacing the 12-line if-block that
// previously opened ListAllTenants and SearchTenants.
//
// JWT behavior for POST /tenants and GET /tenants remains unchanged. Platform
// API keys may create tenants through system_tenants_manage; workspace keys
// remain default-denied on tenant-catalog operations.
func RegisterTenantRoutes(
	r *gin.RouterGroup,
	handler *handler.TenantHandler,
	memberHandler *handler.TenantMemberHandler,
	invitationHandler *handler.TenantInvitationHandler,
	auditLogHandler *handler.AuditLogHandler,
	g *rbacGuards,
) {
	// Cross-tenant superuser endpoints — promoted from handler if-blocks
	// to middleware.RequireCrossTenantAccess at the route layer.
	g.apiKeyRoute(r, http.MethodGet, "/tenants/all",
		apiKeyPlatform(types.APIKeyCapabilitySystemTenantsRead, types.APIKeyCapabilitySystemTenantsManage),
		g.CrossTenant(), handler.ListAllTenants)
	g.apiKeyRoute(r, http.MethodGet, "/tenants/search",
		apiKeyPlatform(types.APIKeyCapabilitySystemTenantsRead, types.APIKeyCapabilitySystemTenantsManage),
		g.CrossTenant(), handler.SearchTenants)

	// Tenant workspace路由组
	tenantRoutes := r.Group("/tenants")
	{
		// Create Tenant workspace对所有已登录用户开放：用户可以为自己再开一个工作区，
		// handler 内部会调 EnsureAdmin 把调用者写成新Tenant workspace的 Admin。
		// 跨Tenant workspace超管走同一个端点，但能携带 storage_quota / status 等
		// 全字段（见 handler.CreateTenant 内部分支）。
		// 安全说明：这里不挂 g.CrossTenant()，因为 self-service Create
		// 不需要跨Tenant workspace特权；handler 也不读写 X-Tenant-ID 指向的现有
		// Tenant workspace，所以越过 PathTenantMatch 守卫不会扩大攻击面。
		// Create Tenant workspace不对 API key 开放（注册在原始 group，默认拒绝）。
		g.apiKeyRoute(tenantRoutes, http.MethodPost, "",
			apiKeyPlatform(types.APIKeyCapabilitySystemTenantsManage), handler.CreateTenant)
		g.apiKeyRoute(tenantRoutes, http.MethodGet, "", apiKeyManageTenantSettings(apiKeyFullAccess()), handler.ListTenants)

		// Generic KV configuration management (tenant-level). Tenant ID
		// is obtained from authentication context; the URL :key is a
		// config key, not a tenant ID, so these stay outside the
		// PathTenantMatch group. Tenant-level surface: full-access keys may
		// call it, and scoped keys need manage_tenant_settings.
		g.apiKeyRoute(tenantRoutes, http.MethodGet, "/kv/:key", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Member(), handler.GetTenantKV)
		g.apiKeyRoute(tenantRoutes, http.MethodPut, "/kv/:key", apiKeyManageTenantSettings(apiKeyFullAccess()), g.Admin(), handler.UpdateTenantKV)

		// Per-tenant endpoints share PathTenantMatch at the group level.
		// Most /tenants/:id/* endpoints stay undeclared for API keys by
		// default — tenant lifecycle and key/principal management require
		// full tenant access or JWT ownership. Member/invitation management
		// opts in below through the manage_members capability.
		tenantByID := tenantRoutes.Group("/:id", g.PathTenantMatch())
		{
			g.apiKeyRoute(tenantByID, http.MethodGet, "",
				apiKeyPlatform(types.APIKeyCapabilitySystemTenantsRead, types.APIKeyCapabilitySystemTenantsManage),
				g.Member(), handler.GetTenant)
			g.apiKeyRoute(tenantByID, http.MethodPut, "",
				apiKeyPlatform(types.APIKeyCapabilitySystemTenantsManage), g.Admin(), handler.UpdateTenant)
			g.apiKeyRoute(tenantByID, http.MethodDelete, "",
				apiKeyPlatform(types.APIKeyCapabilitySystemTenantsManage), g.Admin(), handler.DeleteTenant)
			tenantByID.GET("/api-keys", g.Admin(), handler.ListAPIKeys)
			tenantByID.POST("/api-keys", g.Admin(), handler.CreateAPIKey)
			tenantByID.PUT("/api-keys/:key_id", g.Admin(), handler.UpdateAPIKey)
			tenantByID.DELETE("/api-keys/:key_id", g.Admin(), handler.DeleteAPIKey)
			tenantByID.GET("/api-principal-config", g.Admin(), handler.GetAPIPrincipalConfig)
			tenantByID.PUT("/api-principal-config", g.Admin(), handler.UpdateAPIPrincipalConfig)
			tenantByID.POST("/api-principal-test-token", g.Admin(), handler.CreateAPIPrincipalTestToken)

			// Tenant member management (PR 3 of #1303). Listing is
			// Viewer+ so any active member can see the roster; mutation
			// is Admin+ — managing membership is the admin's core duty.
			// The owner role itself stays protected in the handlers:
			// Membership mutations are strict TenantAdmin (enforced even
			// when the RBAC rollout flag is off): members must not
			// manage memberships. /:id/leave is Member+ — any member
			// can quit on their own; the service still rejects when
			// it would leave the tenant without an Admin.
			if memberHandler != nil {
				g.apiKeyRoute(tenantByID, http.MethodGet, "/members", apiKeyManageMembers(apiKeyFullAccess()), g.Member(), memberHandler.ListMembers)
				g.apiKeyRoute(tenantByID, http.MethodPost, "/members", apiKeyManageMembers(apiKeyFullAccess()), g.TenantAdmin(), memberHandler.AddMember)
				g.apiKeyRoute(tenantByID, http.MethodPut, "/members/:user_id", apiKeyManageMembers(apiKeyFullAccess()), g.TenantAdmin(), memberHandler.UpdateMemberRole)
				g.apiKeyRoute(tenantByID, http.MethodDelete, "/members/:user_id", apiKeyManageMembers(apiKeyFullAccess()), g.TenantAdmin(), memberHandler.RemoveMember)
				tenantByID.POST("/leave", g.Member(), memberHandler.LeaveTenant)
			}

			// Tenant invitation flow. The UI-driven "Invite Member"
			// button hits POST /invitations rather than POST /members,
			// so the invitee gets to confirm via /me/invitations
			// before any tenant_members row is written. List is
			// Viewer+ so any member can see pending invites in the
			// management view; create/revoke are strict TenantAdmin to
			// match the /members mutation gates. nil-skip pattern
			// mirrors memberHandler above for environments built
			// without the invitation dependency wired.
			if invitationHandler != nil {
				g.apiKeyRoute(tenantByID, http.MethodGet, "/invitations", apiKeyManageMembers(apiKeyFullAccess()), g.Member(), invitationHandler.ListTenantInvitations)
				g.apiKeyRoute(tenantByID, http.MethodPost, "/invitations", apiKeyManageMembers(apiKeyFullAccess()), g.TenantAdmin(), invitationHandler.CreateInvitation)
				g.apiKeyRoute(tenantByID, http.MethodDelete, "/invitations/:inv_id", apiKeyManageMembers(apiKeyFullAccess()), g.TenantAdmin(), invitationHandler.RevokeInvitation)
				// Share-link create lives under /invite-links so the URL
				// reads as "create a link" rather than another flavour
				// of /invitations; the underlying row still lives in the
				// tenant_invitations table and shows up in the GET above.
				g.apiKeyRoute(tenantByID, http.MethodPost, "/invite-links", apiKeyManageMembers(apiKeyFullAccess()), g.TenantAdmin(), invitationHandler.CreateInviteLink)
			}

			// Audit log feed (PR 6 of #1303). Admin+ so denied-action
			// histories don't surface to ordinary members; the
			// PathTenantMatch group already prevents cross-tenant
			// reads. nil-skip mirrors the memberHandler pattern above
			// for environments wired without the audit dependency.
			if auditLogHandler != nil {
				tenantByID.GET("/audit-log", g.Admin(), auditLogHandler.ListTenantAuditLog)
			}
		}
	}
}

// RegisterMyInvitationRoutes wires the per-user invitation inbox under
// /me/invitations. The v1 group already applies middleware.Auth so we
// don't need a role gate here — the service enforces "only the invitee
// can accept/decline". The list endpoint mounts under /me to make the
// "show me MY invitations" semantics obvious in URLs and logs (vs the
// tenant-scoped /tenants/:id/invitations which lists ALL invitations
// for the tenant). pending-count is a separate, ultra-light endpoint
// the avatar-row badge polls; splitting it off so polling doesn't
// transfer the full list every cycle.
//
// invitationHandler may be nil in environments built without the
// invitation dependency wired; that's a no-op registration which is
// preferable to a startup crash.
func RegisterMyInvitationRoutes(r *gin.RouterGroup, invitationHandler *handler.TenantInvitationHandler) {
	if invitationHandler == nil {
		return
	}
	me := r.Group("/me")
	{
		me.GET("/invitations", invitationHandler.ListMyInvitations)
		me.GET("/invitations/pending-count", invitationHandler.CountMyPendingInvitations)
		me.POST("/invitations/:inv_id/accept", invitationHandler.AcceptMyInvitation)
		me.POST("/invitations/:inv_id/decline", invitationHandler.DeclineMyInvitation)
		// 已登录用户用共享链接 token 加入Tenant workspace（对应 register-by-invite，但不建新账号）。
		me.POST("/invitations/accept-by-token", invitationHandler.AcceptMyInvitationByToken)
	}
}

// RegisterMyEnvVarRoutes wires the caller's own environment variables under
// /me/env-vars. The v1 group already applies middleware.Auth, and no role gate
// is added on purpose: these are the caller's own values, and the service
// derives whose they are from the context rather than the request.
//
// This deliberately does not reuse /sandbox-configs/:id/skills*, which is
// Admin+ even for reads (see routes_infra.go): an upload there drives a root
// shell whose output is baked into the image, and the listing names what that
// image carries. This endpoint returns declarations and set/unset status only.
//
// h may be nil in environments built without the dependency wired; a no-op
// registration is preferable to a startup crash, as with the invitation inbox.
func RegisterMyEnvVarRoutes(r *gin.RouterGroup, h *handler.MeEnvVarHandler) {
	if h == nil {
		return
	}
	me := r.Group("/me/env-vars")
	{
		me.GET("", h.List)
		me.PUT("/skill", h.SetSkill)
		me.DELETE("/skill", h.DeleteSkill)
		me.PUT("/sandbox", h.SetSandbox)
		me.DELETE("/sandbox", h.DeleteSandbox)
	}
}

// RegisterAuthRoutes registers authentication routes
func RegisterAuthRoutes(r *gin.RouterGroup, handler *handler.AuthHandler, g *rbacGuards) {
	r.POST("/auth/register", handler.Register)
	// Share-link surfaces are unauthenticated and accept a plaintext
	// token from the caller; rate-limit by IP to bound brute-force /
	// enumeration / abuse traffic. Limiter is shared across both
	// endpoints (see middleware/auth_public_ratelimit.go) so total
	// budget per IP is intuitive.
	publicAuthRL := middleware.PublicAuthRateLimit()
	r.POST("/auth/register-by-invite", publicAuthRL, handler.RegisterByInvite)
	r.POST("/auth/invitations/lookup", publicAuthRL, handler.LookupInvitationByToken)
	r.POST("/auth/login", handler.Login)
	r.POST("/auth/auto-setup", handler.AutoSetup)
	r.GET("/auth/config", handler.GetAuthConfig)
	r.POST("/auth/switch-tenant", handler.SwitchTenant)
	r.GET("/auth/oidc/config", handler.GetOIDCConfig)
	r.GET("/auth/oidc/url", handler.GetOIDCAuthorizationURL)
	r.GET("/auth/oidc/callback", handler.OIDCRedirectCallback)
	// /auth/oidc/start：直连 302 跳转到 OIDC 提供方，供前端无法走 JS 拉取 URL 的场景直接发起登录
	r.GET("/auth/oidc/start", handler.OIDCStart)
	r.POST("/auth/refresh", handler.RefreshToken)
	r.GET("/auth/validate", handler.ValidateToken)
	r.POST("/auth/logout", handler.Logout)
	// auth/me returns only the caller's own identity/profile, so it is safe
	// for any valid API key. Chat clients / MCP call it to discover "who am I";
	// leaving it default-deny was why scoped keys got a 403 here.
	g.apiKeyRoute(r, http.MethodGet, "/auth/me", apiKeyAny(), handler.GetCurrentUser)
	r.PUT("/auth/me/preferences", handler.UpdateMyPreferences)
	r.POST("/auth/change-password", handler.ChangePassword)
}

// RegisterSystemRoutes registers system information routes
//
// Reads (GetSystemInfo / ListParserEngines / GetStorageEngineStatus)
// are gated to Viewer+ — any tenant member can see "is the parser
// reachable". The /*-check / /reconnect endpoints actively probe
// remote services with tenant credentials and could trigger network
// fanout, so they're Admin+.
func RegisterSystemRoutes(
	r *gin.RouterGroup,
	handler *handler.SystemHandler,
	g *rbacGuards,
) {
	systemRoutes := g.apiKeyGroup(r.Group("/system"), apiKeyManageVectorStores(apiKeyFullAccess()))
	{
		systemRoutes.With(apiKeyAny()).GET("/capabilities", g.Member(), handler.GetDeploymentCapabilities)
		systemRoutes.GET("/info", g.Member(), handler.GetSystemInfo)
		systemRoutes.GET("/parser-engines", g.Member(), handler.ListParserEngines)
		// Parser-engine probes and infra reconnects exercise provider
		// endpoints/credentials — platform-only, same as model probes.
		systemRoutes.With(apiKeyPlatform(types.APIKeyCapabilitySystemModelsManage)).POST(
			"/parser-engines/check", g.SystemAdmin(), handler.CheckParserEngines)
		systemRoutes.With(apiKeyPlatform(types.APIKeyCapabilitySystemModelsManage)).POST(
			"/docreader/reconnect", g.SystemAdmin(), handler.ReconnectDocReader)
		systemRoutes.GET("/storage-engine-status", g.Member(), handler.GetStorageEngineStatus)
		// Member-readable: upload UIs poll this to decide whether the
		// parse-settings dialog can be skipped (SystemAdmin-locked defaults).
		systemRoutes.GET("/parse-defaults", g.Member(), handler.GetSystemParseDefaults)
		systemRoutes.POST("/storage-engine-check", g.Admin(), handler.CheckStorageEngine)
		systemRoutes.POST("/sandbox-check", g.Admin(), handler.CheckSandboxConfig)
	}
}

// RegisterSystemAdminRoutes registers system administration routes.
//
// All endpoints under this group are gated to SystemAdmin users (i.e.
// User.IsSystemAdmin == true). These are platform-wide operations
// independent of per-tenant Owner/Admin/Contributor/Viewer roles —
// they let org-level superusers grant/revoke system-admin status and,
// in later milestones, will host global settings, built-in models, and
// cross-tenant observability.
//
// Mounted under /api/v1/system/admin/* so the URL scheme stays aligned
// with the existing /api/v1/system/info family. Front-end clients live
// in frontend/src/api/system/index.ts.
//
// auditLogHandler may be nil in environments wired without the audit
// dependency; the /audit-log subroute is then omitted. This mirrors
// the optional wiring in RegisterTenantRoutes.
func RegisterSystemAdminRoutes(
	r *gin.RouterGroup,
	handler *handler.SystemHandler,
	statsHandler *handler.StatsHandler,
	auditLogHandler *handler.AuditLogHandler,
	g *rbacGuards,
) {
	// Apply SystemAdmin() at the group level — every route below inherits
	// the guard, so adding new endpoints can't accidentally drop the gate.
	adminRoutes := r.Group("/system/admin", g.SystemAdmin())
	{
		// P0: SystemAdmin role management
		adminRoutes.POST("/promote", handler.PromoteUserToSystemAdmin)
		adminRoutes.POST("/revoke", handler.RevokeSystemAdmin)
		adminRoutes.GET("/list", handler.ListSystemAdmins)
		// GET /users lists every account (admins + regular users) for the
		// user-management UI; /list above is the admins-only subset kept
		// for the compact admin-picker views.
		adminRoutes.GET("/users", handler.ListSystemUsers)
		// Platform-wide usage stats (accounts, documents by status, message
		// activity heatmap) for the SystemAdmin overview dashboard.
		// Human-admin only — deliberately not declared as an API-key route.
		adminRoutes.GET("/stats", statsHandler.GetSystemStats)
		adminRoutes.POST("/users/reset-password", handler.ResetUserPassword)
		adminRoutes.POST("/users/create", handler.CreateSystemUser)
		// Workspace-role management for any tenant — the SystemAdmin
		// group gate replaces the per-tenant Owner requirement of the
		// /tenants/:id/members/:user_id route.
		adminRoutes.PUT("/tenants/:tenant_id/members/:user_id", handler.UpdateSystemUserRole)
		adminRoutes.GET("/api-keys", handler.ListPlatformAPIKeys)
		adminRoutes.POST("/api-keys", handler.CreatePlatformAPIKey)
		adminRoutes.DELETE("/api-keys/:key_id", handler.DeletePlatformAPIKey)

		// P1: platform-wide system settings (DB-backed runtime tunables).
		// Reads return raw model rows / arrays (no `gin.H{"data":...}`
		// wrapping), matching the project's axios interceptor convention
		// — see frontend/src/utils/request.ts:97.
		g.apiKeyRoute(adminRoutes, http.MethodGet, "/settings",
			apiKeyPlatform(types.APIKeyCapabilitySystemSettingsRead, types.APIKeyCapabilitySystemSettingsManage),
			handler.ListSystemSettings)
		g.apiKeyRoute(adminRoutes, http.MethodGet, "/settings/:key",
			apiKeyPlatform(types.APIKeyCapabilitySystemSettingsRead, types.APIKeyCapabilitySystemSettingsManage),
			handler.GetSystemSetting)
		g.apiKeyRoute(adminRoutes, http.MethodPut, "/settings/:key",
			apiKeyPlatform(types.APIKeyCapabilitySystemSettingsManage), handler.UpdateSystemSetting)
		g.apiKeyRoute(adminRoutes, http.MethodDelete, "/settings/:key",
			apiKeyPlatform(types.APIKeyCapabilitySystemSettingsManage), handler.ResetSystemSetting)

		// Runtime operations: live asynq queue depths, safe task projections,
		// and state-checked task actions for the SystemAdmin dashboard. Lite
		// mode returns available=false.
		g.apiKeyRoute(adminRoutes, http.MethodGet, "/runtime/queues",
			apiKeyPlatform(types.APIKeyCapabilitySystemRuntimeRead, types.APIKeyCapabilitySystemRuntimeManage),
			handler.GetRuntimeQueues)
		g.apiKeyRoute(adminRoutes, http.MethodGet, "/runtime/queues/:queue/tasks",
			apiKeyPlatform(types.APIKeyCapabilitySystemRuntimeRead, types.APIKeyCapabilitySystemRuntimeManage),
			handler.ListRuntimeTasks)
		g.apiKeyRoute(adminRoutes, http.MethodPost, "/runtime/queues/:queue/tasks/:task_id/actions/:action",
			apiKeyPlatform(types.APIKeyCapabilitySystemRuntimeManage), handler.MutateRuntimeTask)
		g.apiKeyRoute(adminRoutes, http.MethodDelete, "/runtime/queues/:queue/archived",
			apiKeyPlatform(types.APIKeyCapabilitySystemRuntimeManage), handler.PurgeArchivedRuntimeTasks)

		// Bulk action — write the current default-quota setting onto
		// every existing tenant. Lives under /tenants instead of
		// /settings because it changes tenants, not the setting row.
		g.apiKeyRoute(adminRoutes, http.MethodPost, "/tenants/apply-default-storage-quota",
			apiKeyPlatform(types.APIKeyCapabilitySystemTenantsManage),
			handler.ApplyDefaultStorageQuotaToAllTenants)

		// Platform-wide audit feed (tenant_id=0 rows). Covers
		// system.setting_changed / system.admin_promoted /
		// system.admin_revoked etc. — events written by the routes
		// above. Without this endpoint those audit rows would have
		// no UI surface (per-tenant ListTenantAuditLog filters them
		// out by tenant_id). Optional: skip when audit deps are
		// absent, matching RegisterTenantRoutes' /audit-log handling.
		if auditLogHandler != nil {
			g.apiKeyRoute(adminRoutes, http.MethodGet, "/audit-log",
				apiKeyPlatform(types.APIKeyCapabilitySystemAuditRead), auditLogHandler.ListSystemAuditLog)
		}
	}
}
