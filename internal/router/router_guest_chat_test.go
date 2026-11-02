package router

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const guestPortalID uint64 = 10050

// newGuestChatEngine wires the real chat, session, message and agent routes
// behind a fixed auth context: tenant is the tenant row the caller runs in
// and role its tenant role ("" for none).
func newGuestChatEngine(t *testing.T, tenant *types.Tenant, role types.TenantRole, apiKey bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enabled := true
	guards := &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}}
	r := gin.New()
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		c.AbortWithStatus(handlerReachedStatus)
	}))
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: tenant.ID, UserID: "guest-1", Role: role})
		ctx = context.WithValue(ctx, types.UserIDContextKey, "guest-1")
		ctx = context.WithValue(ctx, types.TenantIDContextKey, tenant.ID)
		ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "guest-1"})
		if apiKey {
			ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{})
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	g := r.Group("/api/v1")
	RegisterSessionRoutes(g, &session.Handler{}, nil, guards)
	RegisterDocumentWorkspaceRoutes(g, &session.DocumentWorkspaceHandler{}, guards)
	RegisterChatRoutes(g, &session.Handler{}, guards)
	RegisterMessageRoutes(g, &handler.MessageHandler{}, guards)
	RegisterCustomAgentRoutes(g, &handler.CustomAgentHandler{}, guards)
	return r
}

func portalTenant() *types.Tenant {
	return &types.Tenant{ID: guestPortalID, Kind: types.TenantKindPublicPortal}
}

// guestChatCases are the routes auth serves a Guest in the public portal
// (middleware.IsGuestChatAPI), one concrete request each.
var guestChatCases = []struct{ method, path string }{
	{http.MethodPost, "/api/v1/sessions"},
	{http.MethodGet, "/api/v1/sessions"},
	{http.MethodGet, "/api/v1/sessions/s1"},
	{http.MethodPut, "/api/v1/sessions/s1"},
	{http.MethodDelete, "/api/v1/sessions/s1"},
	{http.MethodGet, "/api/v1/sessions/s1/context-usage"},
	{http.MethodPost, "/api/v1/sessions/s1/generate_title"},
	{http.MethodPost, "/api/v1/sessions/s1/stop"},
	{http.MethodPost, "/api/v1/sessions/s1/continue"},
	{http.MethodGet, "/api/v1/sessions/continue-stream/s1"},
	{http.MethodPost, "/api/v1/agent-chat/s1"},
	{http.MethodPost, "/api/v1/knowledge-chat/s1"},
	{http.MethodGet, "/api/v1/messages/s1/load"},
	{http.MethodDelete, "/api/v1/messages/s1/m1"},
	{http.MethodPut, "/api/v1/messages/s1/m1/feedback"},
	{http.MethodGet, "/api/v1/agents"},
}

// memberOnlyChatCases stay Member+: a Guest never reaches them.
var memberOnlyChatCases = []struct{ method, path string }{
	{http.MethodDelete, "/api/v1/sessions/batch"},
	{http.MethodDelete, "/api/v1/sessions/s1/messages"},
	{http.MethodPost, "/api/v1/sessions/s1/attachments"},
	{http.MethodGet, "/api/v1/sessions/s1/attachments"},
	{http.MethodPost, "/api/v1/sessions/s1/fork"},
	{http.MethodGet, "/api/v1/sessions/s1/sub-chats"},
	{http.MethodPost, "/api/v1/sessions/s1/sub-chats"},
	{http.MethodPost, "/api/v1/sessions/s1/steer"},
	{http.MethodPost, "/api/v1/sessions/s1/pin"},
	{http.MethodPost, "/api/v1/sessions/s1/sandbox/terminal-ticket"},
	{http.MethodGet, "/api/v1/sessions/s1/artifacts"},
	{http.MethodPost, "/api/v1/sessions/s1/documents"},
	{http.MethodGet, "/api/v1/sessions/s1/documents"},
	{http.MethodPost, "/api/v1/sessions/s1/documents/d1/content"},
	{http.MethodPost, "/api/v1/sessions/s1/documents/d1/word-file"},
	{http.MethodPost, "/api/v1/knowledge-search"},
	{http.MethodPost, "/api/v1/messages/search"},
	{http.MethodGet, "/api/v1/agents/a1"},
	{http.MethodGet, "/api/v1/agents/type-presets"},
}

func TestGuestChatGuardAdmitsPortalGuest(t *testing.T) {
	engine := newGuestChatEngine(t, portalTenant(), "", false)
	for _, tc := range guestChatCases {
		rec := serveGuest(engine, tc.method, tc.path, "")
		require.NotContainsf(t, []int{http.StatusForbidden, http.StatusNotFound}, rec.Code,
			"%s %s must reach the handler, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
	}
}

func TestGuestChatGuardKeepsMemberRoutesClosed(t *testing.T) {
	engine := newGuestChatEngine(t, portalTenant(), "", false)
	for _, tc := range memberOnlyChatCases {
		rec := serveGuest(engine, tc.method, tc.path, "")
		require.Equalf(t, http.StatusForbidden, rec.Code,
			"%s %s must stay Member-only, body=%s", tc.method, tc.path, rec.Body.String())
	}
}

// Outside the portal a caller without a tenant role is refused on the guest
// chat routes too: only the portal context opens them.
func TestGuestChatGuardRefusesRolelessOutsidePortal(t *testing.T) {
	engine := newGuestChatEngine(t, &types.Tenant{ID: 5, Kind: types.TenantKindWorkspace}, "", false)
	for _, tc := range guestChatCases {
		rec := serveGuest(engine, tc.method, tc.path, "")
		require.Equalf(t, http.StatusForbidden, rec.Code, "%s %s body=%s", tc.method, tc.path, rec.Body.String())
	}
}

// A member keeps every chat route.
func TestGuestChatGuardMemberUnchanged(t *testing.T) {
	engine := newGuestChatEngine(t, &types.Tenant{ID: 5, Kind: types.TenantKindWorkspace}, types.TenantRoleMember, false)
	for _, tc := range append(append([]struct{ method, path string }{}, guestChatCases...), memberOnlyChatCases...) {
		rec := serveGuest(engine, tc.method, tc.path, "")
		require.NotEqualf(t, http.StatusForbidden, rec.Code, "%s %s body=%s", tc.method, tc.path, rec.Body.String())
	}
}

// The guarded routes and auth's allow-list are the same set: every
// GuestChat route is one auth serves a Guest, and every allow-listed shape
// is a registered route.
func TestGuestChatRoutesMatchAuthAllowList(t *testing.T) {
	for _, tc := range guestChatCases {
		require.Truef(t, middleware.IsGuestChatAPI(tc.path, tc.method), "%s %s", tc.method, tc.path)
	}
	for _, tc := range memberOnlyChatCases {
		require.Falsef(t, middleware.IsGuestChatAPI(tc.path, tc.method), "%s %s", tc.method, tc.path)
	}
	// every registered route admitted by the portal guard is allow-listed
	engine := newGuestChatEngine(t, portalTenant(), "", false)
	for _, route := range engine.Routes() {
		path := strings.NewReplacer(":session_id", "s1", ":id", "s1", ":message_id", "m1",
			":attachment_id", "a1", ":doc_id", "d1", ":steer_id", "x1", ":seq", "1", ":index", "0",
			":request_id", "r1").Replace(route.Path)
		rec := serveGuest(engine, route.Method, path, "")
		admitted := rec.Code != http.StatusForbidden
		require.Equalf(t, admitted, middleware.IsGuestChatAPI(path, route.Method),
			"%s %s: guard admits=%v but auth allow-list disagrees (code %d)", route.Method, route.Path, admitted, rec.Code)
	}
}
