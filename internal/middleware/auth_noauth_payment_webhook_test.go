package middleware

import (
	"net/http"
	"testing"
)

// The payment checker has no account: the webhook is a no-auth path and
// its handler checks BILLING_WEBHOOK_SECRET. Only POST is public.
func TestIsNoAuthAPI_PaymentWebhook(t *testing.T) {
	if !isNoAuthAPI("/api/v1/billing/payments/webhook", http.MethodPost) {
		t.Fatal("POST /api/v1/billing/payments/webhook must be public")
	}
	if isNoAuthAPI("/api/v1/billing/payments/webhook", http.MethodGet) {
		t.Fatal("GET /api/v1/billing/payments/webhook must not be public")
	}
	if isNoAuthAPI("/api/v1/billing/orders", http.MethodPost) {
		t.Fatal("the rest of /billing stays authenticated")
	}
}
