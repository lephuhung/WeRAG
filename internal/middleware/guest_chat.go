package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// Guest chat: a Guest (a signed-in human with no active membership anywhere)
// chats with the public-lookup agent in the public portal tenant. Auth puts
// the Guest there, with no tenant role, on exactly the routes below; every
// other tenant route still answers TENANT_REQUIRED. The GuestChat route
// guard (RequireMemberOrPortalGuest) admits the portal session on the same
// routes, and the chat handler forces the public-lookup agent.
//
// Left out on purpose: attachments, fork, steer, pin, sub-chats, documents,
// sandbox, artifacts, suggestions and the knowledge search — a Guest reads
// public knowledge bases through their own tenant-optional read routes.

// guestChatRoute is one guest chat shape. Segments after /api/v1 are matched
// one by one; "*" matches any single non-empty segment.
type guestChatRoute struct {
	method string
	segs   []string
}

var guestChatRoutes = []guestChatRoute{
	{http.MethodPost, []string{"sessions"}},
	{http.MethodGet, []string{"sessions"}},
	{http.MethodGet, []string{"sessions", "*"}},
	{http.MethodPut, []string{"sessions", "*"}},
	{http.MethodDelete, []string{"sessions", "*"}},
	{http.MethodGet, []string{"sessions", "*", "context-usage"}},
	{http.MethodPost, []string{"sessions", "*", "generate_title"}},
	{http.MethodPost, []string{"sessions", "*", "stop"}},
	{http.MethodPost, []string{"sessions", "*", "continue"}},
	{http.MethodGet, []string{"sessions", "continue-stream", "*"}},
	{http.MethodPost, []string{"agent-chat", "*"}},
	{http.MethodPost, []string{"knowledge-chat", "*"}},
	{http.MethodGet, []string{"messages", "*", "load"}},
	{http.MethodDelete, []string{"messages", "*", "*"}},
	{http.MethodPut, []string{"messages", "*", "*", "feedback"}},
	{http.MethodGet, []string{"agents"}},
	{http.MethodGet, []string{"me", "usage"}},
}

// guestChatReservedSessionSegs are /sessions/<x> words that name another
// route, never a session id: they must not match the "*" of /sessions/:id.
var guestChatReservedSessionSegs = map[string]bool{
	"batch":           true,
	"continue-stream": true,
}

// IsGuestChatAPI reports whether method+path is one of the guest chat routes.
// Trailing slashes and empty segments fail closed.
func IsGuestChatAPI(path, method string) bool {
	rest, ok := strings.CutPrefix(path, "/api/v1/")
	if !ok || rest == "" {
		return false
	}
	segs := strings.Split(rest, "/")
	for _, s := range segs {
		if s == "" {
			return false
		}
	}
	for _, r := range guestChatRoutes {
		if r.method != method || len(r.segs) != len(segs) {
			continue
		}
		if matchGuestChatSegs(r.segs, segs) {
			return true
		}
	}
	return false
}

func matchGuestChatSegs(pattern, segs []string) bool {
	for i, p := range pattern {
		if p == "*" {
			// /sessions/batch (DELETE) and friends are not /sessions/:id
			if i == 1 && segs[0] == "sessions" && guestChatReservedSessionSegs[segs[1]] {
				return false
			}
			continue
		}
		if p != segs[i] {
			return false
		}
	}
	return true
}

// isGuestUser reports whether user is a Guest: a real human account that is
// not a SuperAdmin and holds no active membership in any tenant. Lookup
// errors fail closed (not a guest).
func isGuestUser(ctx context.Context, user *types.User, memberService interfaces.TenantMemberService) bool {
	if user == nil || user.IsSystemAdmin || types.IsSyntheticUserID(user.ID) || memberService == nil {
		return false
	}
	members, err := memberService.ListByUser(ctx, user.ID)
	if err != nil {
		logger.Warnf(ctx, "[auth] guest check: memberships of %s unavailable: %v", user.ID, err)
		return false
	}
	for _, m := range members {
		if m != nil && m.TenantID != 0 && m.Status == types.TenantMemberStatusActive {
			return false
		}
	}
	return true
}

// attachPublicPortalGuest places a Guest in the public portal for a guest
// chat route: the portal as tenant, no tenant role. Returns false when no
// portal exists (the caller then answers TENANT_REQUIRED as before).
func attachPublicPortalGuest(c *gin.Context, tenantService interfaces.TenantService, user *types.User) bool {
	ctx := c.Request.Context()
	resolver, ok := tenantService.(interfaces.PublicPortalResolver)
	if !ok {
		return false
	}
	portal, err := resolver.GetPublicPortal(ctx)
	if err != nil || portal == nil {
		logger.Warnf(ctx, "[auth] public portal unavailable for guest %s: %v", user.ID, err)
		return false
	}
	logger.Infof(ctx, "[auth] guest %s served %s %s in the public portal (tenant %d)",
		user.ID, c.Request.Method, c.Request.URL.Path, portal.ID)
	applyAuthSession(c, authSession{
		User:      user,
		Principal: types.Principal{Type: types.PrincipalWebUser, ID: user.ID},
		TenantID:  portal.ID,
		Tenant:    portal,
		Role:      "",
	})
	return true
}

// tryGuestChat serves a guest chat route in the public portal when the caller
// is a Guest. Returns true when the session was attached.
func tryGuestChat(
	c *gin.Context,
	tenantService interfaces.TenantService,
	memberService interfaces.TenantMemberService,
	user *types.User,
) bool {
	if !IsGuestChatAPI(c.Request.URL.Path, c.Request.Method) {
		return false
	}
	if !isGuestUser(c.Request.Context(), user, memberService) {
		return false
	}
	return attachPublicPortalGuest(c, tenantService, user)
}

// RequireMemberOrPortalGuest is the guard of the guest chat routes: Member+
// as RequireRole, or a Guest that auth placed in the public portal
// (access.IsPublicPortalGuest). Use it only on routes IsGuestChatAPI lists.
func RequireMemberOrPortalGuest(cfg *config.Config) gin.HandlerFunc {
	member := RequireRole(types.TenantRoleMember, cfg)
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if access.IsPublicPortalGuest(ctx, types.CallerFromContext(ctx)) {
			c.Next()
			return
		}
		member(c)
	}
}
