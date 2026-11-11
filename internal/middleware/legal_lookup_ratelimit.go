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
//   - a Guest (no tenant role) per client IP: the public portal is open to
//     anyone who registers, so a new account must not mean a new budget;
//   - a member or SuperAdmin per account, more generously.
//
// Redis-backed (shared by every backend instance) with the in-memory
// fallback of internal/ratelimit when Redis is unavailable.
const (
	legalLookupRateWindow    = time.Minute
	legalLookupGuestPerIP    = 30
	legalLookupMemberPerUser = 120
	legalLookupRateKeyPrefix = "legal_lookup:ratelimit:"
)

// LegalLookupAccess guards the validity lookup route.
func LegalLookupAccess(redisClient *redis.Client) gin.HandlerFunc {
	limiter := ratelimit.New(redisClient, legalLookupRateKeyPrefix, legalLookupRateWindow, "")
	go limiter.StartCleanup(make(chan struct{}))
	return legalLookupAccess(limiter, legalLookupGuestPerIP, legalLookupMemberPerUser)
}

func legalLookupAccess(limiter *ratelimit.Limiter, guestPerIP, memberPerUser int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		caller := types.CallerFromContext(ctx)
		if !access.IsAuthenticatedHuman(ctx, caller) {
			abortWith(c, apperrors.NewForbiddenError("the legal lookup is open to signed-in people only"))
			return
		}
		key, max := "ip:"+c.ClientIP(), guestPerIP
		if caller.HasTenantRole() || access.IsExplicitHumanSuperAdmin(ctx, caller) {
			key, max = "user:"+caller.UserID, memberPerUser
		}
		if !limiter.Allow(ctx, key, max) {
			tooManyRequests(c, legalLookupRateWindow, "too many lookups; please retry in a minute")
			return
		}
		c.Next()
	}
}
