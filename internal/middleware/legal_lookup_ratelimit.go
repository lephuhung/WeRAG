package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/ratelimit"
	"github.com/Tencent/WeKnora/internal/types"
)

// Access and rate limits of the validity lookup (GET /legal-lookup): any
// signed-in person, member or Guest, never an API key or anonymous caller.
// The lookup reads the catalogue only (no LLM, no usage quota), so the
// limits only keep scripted scraping off the database:
//   - a Guest (no tenant role) per client IP — the public portal is open to
//     anyone who registers, so a new account must not mean a new budget —
//     and per account too, so one account is bounded even when many share
//     an IP (an office NAT) or the IP is not the real client's (a spoofed
//     X-Forwarded-For reaching a directly exposed port);
//   - a member or SuperAdmin per account, more generously.
//
// Redis-backed (shared by every backend instance) with the in-memory
// fallback of internal/ratelimit when Redis is unavailable.
const (
	legalLookupRateWindow    = time.Minute
	legalLookupGuestPerIP    = 30
	legalLookupGuestPerUser  = 60
	legalLookupMemberPerUser = 120
	legalLookupRateKeyPrefix = "legal_lookup:ratelimit:"
)

// LegalLookupAccess guards the validity lookup route.
func LegalLookupAccess(redisClient *redis.Client) gin.HandlerFunc {
	limiter := ratelimit.New(redisClient, legalLookupRateKeyPrefix, legalLookupRateWindow, "")
	go limiter.StartCleanup(make(chan struct{}))
	return legalLookupAccess(limiter, legalLookupLimits{
		guestPerIP: legalLookupGuestPerIP, guestPerUser: legalLookupGuestPerUser, memberPerUser: legalLookupMemberPerUser,
	})
}

type legalLookupLimits struct {
	guestPerIP, guestPerUser, memberPerUser int
}

func legalLookupAccess(limiter *ratelimit.Limiter, limits legalLookupLimits) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		caller := types.CallerFromContext(ctx)
		if !access.IsAuthenticatedHuman(ctx, caller) {
			abortWith(c, apperrors.NewForbiddenError("the legal lookup is open to signed-in people only"))
			return
		}
		var allowed bool
		if caller.HasTenantRole() || access.IsExplicitHumanSuperAdmin(ctx, caller) {
			allowed = limiter.Allow(ctx, "user:"+caller.UserID, limits.memberPerUser)
		} else {
			// both buckets count every request, so neither can be dodged
			byIP := limiter.Allow(ctx, "ip:"+c.ClientIP(), limits.guestPerIP)
			byUser := limiter.Allow(ctx, "guest:"+caller.UserID, limits.guestPerUser)
			allowed = byIP && byUser
		}
		if !allowed {
			tooManyRequests(c, legalLookupRateWindow, "too many lookups; please retry in a minute")
			return
		}
		c.Next()
	}
}
