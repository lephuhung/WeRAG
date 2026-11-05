package middleware

import (
	"net/http"
	"testing"
)

// The external-source legal feed's operator may hold no workspace of its own
// (users.can_upload_legal_documents is account-level authority). Auth must
// therefore let those shapes through the tenant gate; the route guard decides.
// These cases pin the exact shapes: sub-routes that stay SuperAdmin-only are
// excluded on purpose.
func TestIsTenantOptionalAPI_LegalFeedShapes(t *testing.T) {
	cases := []struct {
		path, method string
		want         bool
	}{
		{"/api/v1/system/legal-documents/push", http.MethodPost, true},
		{"/api/v1/system/legal-documents/push", http.MethodGet, false},
		{"/api/v1/system/legal-requests", http.MethodGet, true},
		{"/api/v1/system/legal-requests", http.MethodPut, false},
		{"/api/v1/system/legal-requests/r1", http.MethodPut, true},
		{"/api/v1/system/legal-requests/r1", http.MethodGet, false},
		{"/api/v1/system/legal-requests/r1/portal-check", http.MethodPost, false},
		{"/api/v1/system/legal-requests/r1/fetch", http.MethodPost, false},
		{"/api/v1/system/legal-requests/public-knowledge-bases", http.MethodGet, false},
		{"/api/v1/system/legal-link-candidates", http.MethodGet, false},
	}
	for _, tc := range cases {
		if got := isTenantOptionalAPI(tc.path, tc.method); got != tc.want {
			t.Errorf("isTenantOptionalAPI(%q, %s) = %v, want %v", tc.path, tc.method, got, tc.want)
		}
	}
}
