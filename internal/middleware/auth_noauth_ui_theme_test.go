package middleware

import (
	"net/http"
	"testing"
)

// The platform UI theme must be readable before sign-in, so the global Auth
// middleware has to treat it as a no-auth path (registered public in
// RegisterSystemRoutes). Only GET is public.
func TestIsNoAuthAPI_UITheme(t *testing.T) {
	if !isNoAuthAPI("/api/v1/system/ui-theme", http.MethodGet) {
		t.Fatal("GET /api/v1/system/ui-theme must be public")
	}
	if isNoAuthAPI("/api/v1/system/ui-theme", http.MethodPost) {
		t.Fatal("POST /api/v1/system/ui-theme must not be public")
	}
}
