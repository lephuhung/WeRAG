package middleware

import (
	"net/http"
	"testing"
)

// The person's own notifications are reachable without an organization (a
// Guest has none); nothing else under the prefix shape is opened.
func TestNotificationRoutesAreTenantOptional(t *testing.T) {
	cases := []struct {
		path, method string
		want         bool
	}{
		{"/api/v1/notifications", http.MethodGet, true},
		{"/api/v1/notifications/unread-count", http.MethodGet, true},
		{"/api/v1/notifications/seen", http.MethodPost, true},
		{"/api/v1/notifications/read-all", http.MethodPost, true},
		{"/api/v1/notifications/abc/read", http.MethodPost, true},
		{"/api/v1/notifications", http.MethodPost, false},
		{"/api/v1/notifications/abc", http.MethodDelete, false},
	}
	for _, tc := range cases {
		if got := isTenantOptionalAPI(tc.path, tc.method); got != tc.want {
			t.Errorf("isTenantOptionalAPI(%q, %s) = %v, want %v", tc.path, tc.method, got, tc.want)
		}
	}
}
