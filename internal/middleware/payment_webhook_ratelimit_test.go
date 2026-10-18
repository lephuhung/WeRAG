package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
)

// paymentWebhookTestRouter mounts the webhook guard with secret "good" in
// front of next (200 when nil), behind a minimal error renderer.
func paymentWebhookTestRouter(all, fails *ipRateLimiter, secret string, next gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	if next == nil {
		next = func(c *gin.Context) { c.Status(http.StatusOK) }
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			if e, ok := c.Errors.Last().Err.(*apperrors.AppError); ok {
				c.AbortWithStatus(e.HTTPCode)
			}
		}
	})
	r.POST("/hook", paymentWebhookAuth(all, fails, func(context.Context) string { return secret }), next)
	return r
}

func postHook(r *gin.Engine, secret string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/hook", nil)
	req.RemoteAddr = "203.0.113.7:4000"
	if secret != "" {
		req.Header.Set("X-Webhook-Secret", secret)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// postHookParallel sends n requests at once and counts the status codes.
func postHookParallel(r *gin.Engine, secret string, n int) map[int]int {
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code := postHook(r, secret).Code
			mu.Lock()
			codes[code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	return codes
}

func TestPaymentWebhookOffWithoutSecret(t *testing.T) {
	r := paymentWebhookTestRouter(newIPRateLimiter(time.Minute, 100), newIPRateLimiter(time.Minute, 3), "", nil)
	if w := postHook(r, "anything"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestPaymentWebhookWrongSecretsLockTheIPOut(t *testing.T) {
	r := paymentWebhookTestRouter(newIPRateLimiter(time.Minute, 100), newIPRateLimiter(time.Minute, 3), "good", nil)
	for i := 0; i < 3; i++ {
		if w := postHook(r, "bad"); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d, want 401", i, w.Code)
		}
	}
	w := postHook(r, "good")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("after 3 wrong secrets even the right one is refused: got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 carries Retry-After")
	}
}

func TestPaymentWebhookParallelWrongSecretsComparedAtMostMax(t *testing.T) {
	r := paymentWebhookTestRouter(newIPRateLimiter(time.Minute, 100), newIPRateLimiter(time.Minute, 3), "good", nil)
	codes := postHookParallel(r, "bad", 20)
	if codes[http.StatusUnauthorized] != 3 || codes[http.StatusTooManyRequests] != 17 {
		t.Fatalf("got %v, want 3×401 and 17×429", codes)
	}
}

func TestPaymentWebhookRightSecretsInFlightAreNotCapped(t *testing.T) {
	release := make(chan struct{})
	var inFlight sync.WaitGroup
	inFlight.Add(20)
	slow := func(c *gin.Context) {
		inFlight.Done()
		<-release // all 20 requests inside the handler at once
		c.Status(http.StatusOK)
	}
	r := paymentWebhookTestRouter(newIPRateLimiter(time.Minute, 100), newIPRateLimiter(time.Minute, 1), "good", slow)
	done := make(chan map[int]int)
	go func() { done <- postHookParallel(r, "good", 20) }()
	inFlight.Wait()
	close(release)
	if codes := <-done; codes[http.StatusOK] != 20 {
		t.Fatalf("got %v, want 20×200: right secrets must not use up failure slots", codes)
	}
}

func TestPaymentWebhookMissingSecretDoesNotLockOut(t *testing.T) {
	r := paymentWebhookTestRouter(newIPRateLimiter(time.Minute, 100), newIPRateLimiter(time.Minute, 1), "good", nil)
	for i := 0; i < 5; i++ {
		if w := postHook(r, ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("probe %d: got %d, want 401", i, w.Code)
		}
	}
	if w := postHook(r, "good"); w.Code != http.StatusOK {
		t.Fatalf("probes without a secret must not lock the checker out: got %d", w.Code)
	}
}

func TestPaymentWebhookOverallBudget(t *testing.T) {
	r := paymentWebhookTestRouter(newIPRateLimiter(time.Minute, 5), newIPRateLimiter(time.Minute, 1), "good", nil)
	for i := 0; i < 5; i++ {
		if w := postHook(r, "good"); w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i, w.Code)
		}
	}
	if w := postHook(r, "good"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("over the overall budget: got %d, want 429", w.Code)
	}
}
