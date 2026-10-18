package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
)

// Authentication and rate limits of the public payment webhook (POST
// /billing/payments/webhook), per client IP:
//   - every request: generous, so a checker pushing a backlog of
//     transfers one by one is not slowed down, but a flood is cut off;
//   - wrong secrets: a presented secret is compared under the IP's
//     failure bucket lock and a wrong one takes a slot, so at most a few
//     wrong secrets per window are ever compared, even in a parallel
//     burst, while right ones never take a slot (any number may be in
//     flight). Once the slots are used up every request from the IP that
//     presents a secret is refused until the window passes. A request
//     with no secret at all (a probe, a mis-pointed service) gets 401
//     without costing the IP its slots.
const (
	paymentWebhookWindow     = time.Minute
	paymentWebhookMax        = 300
	paymentWebhookFailWindow = 15 * time.Minute
	paymentWebhookFailMax    = 10
)

var (
	paymentWebhookLimiter     = newIPRateLimiter(paymentWebhookWindow, paymentWebhookMax)
	paymentWebhookFailLimiter = newIPRateLimiter(paymentWebhookFailWindow, paymentWebhookFailMax)
)

// PaymentWebhookAuth checks the shared secret that secret returns (503
// while it is empty) under the per-IP limits above. secret is read on
// every request, so a changed setting applies at once.
func PaymentWebhookAuth(secret func(ctx context.Context) string) gin.HandlerFunc {
	return paymentWebhookAuth(paymentWebhookLimiter, paymentWebhookFailLimiter, secret)
}

func paymentWebhookAuth(all, fails *ipRateLimiter, secret func(ctx context.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !all.allow(ip) {
			tooManyRequests(c, paymentWebhookWindow, "too many requests; please retry shortly")
			return
		}
		want := secret(c.Request.Context())
		if want == "" {
			abortWith(c, apperrors.NewServiceUnavailableError("the payment webhook is not configured"))
			return
		}
		got := paymentWebhookSecret(c)
		if got == "" {
			abortWith(c, apperrors.NewUnauthorizedError("invalid webhook secret"))
			return
		}
		refused, passed := fails.check(ip, func() bool {
			return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
		})
		switch {
		case refused:
			tooManyRequests(c, paymentWebhookFailWindow, "too many failed attempts; retry later")
			return
		case !passed:
			abortWith(c, apperrors.NewUnauthorizedError("invalid webhook secret"))
			return
		}
		c.Next()
	}
}

// paymentWebhookSecret is what the request carries: X-Webhook-Secret, or
// Authorization as "Bearer <secret>" / "Apikey <secret>" (SePay's form).
func paymentWebhookSecret(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("X-Webhook-Secret")); v != "" {
		return v
	}
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	for _, scheme := range []string{"Bearer ", "Apikey "} {
		if len(auth) > len(scheme) && strings.EqualFold(auth[:len(scheme)], scheme) {
			return strings.TrimSpace(auth[len(scheme):])
		}
	}
	return ""
}

func abortWith(c *gin.Context, err *apperrors.AppError) {
	c.Error(err)
	c.Abort()
}

func tooManyRequests(c *gin.Context, retryAfter time.Duration, msg string) {
	c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	abortWith(c, &apperrors.AppError{
		Code:     apperrors.ErrTooManyRequests,
		Message:  msg,
		HTTPCode: http.StatusTooManyRequests,
	})
}
