package middleware

import (
	"net/http"
	"testing"
)

// Task 6 integration finding: tenantless authenticated humans must reach
// per-KB-guarded public read/download routes (auth admits; per-KB
// KBAccessRead/Download decides on the loaded owner+visibility). The table
// pins exact allow/deny: public-read shapes true; broad, mutating, and
// non-KB-guarded shapes false.
func TestTenantOptionalPublicReadSurface(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		// KB detail + content reads.
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/knowledge", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/knowledge/folders", true},
		{http.MethodGet, "/api/v1/knowledge/abc", true},
		{http.MethodGet, "/api/v1/knowledge/abc/stages", true},
		{http.MethodGet, "/api/v1/knowledge/abc/spans", true},
		{http.MethodGet, "/api/v1/knowledge/abc/preview", true},
		// Original downloads (per-KB download guard downstream).
		{http.MethodGet, "/api/v1/knowledge/abc/download", true},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/knowledge/batch-download", true},
		// Per-KB search (read-only, per-KB guard downstream).
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/hybrid-search", true},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/hybrid-search", true},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/faq/search", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/faq/entries", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/faq/entries/export", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/faq/entries/e-1", true},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/tags", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/pages", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/pages/some-slug", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/revisions/some-slug", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/folders", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/index", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/graph", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/stats", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/search", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/lint", true},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/wiki/issues", true},
		{http.MethodGet, "/api/v1/chunks/doc-1", true},
		{http.MethodGet, "/api/v1/chunks/by-id/ch-1", true},
		{http.MethodGet, "/api/v1/chunks/doc-1/ch-1/revisions", true},

		// Deny: mixed tenant list, cross-KB search/batch, activity,
		// progress, invites, copy, pins, move-targets, files serve.
		{http.MethodGet, "/api/v1/knowledge-bases", false},
		{http.MethodGet, "/api/v1/knowledge/search", false},
		{http.MethodGet, "/api/v1/knowledge/batch", false},
		{http.MethodPost, "/api/v1/knowledge/move", false},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/activity", false},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/move-targets", false},
		{http.MethodPut, "/api/v1/knowledge-bases/kb-1/pin", false},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/files", false},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/duplicate", false},
		{http.MethodPost, "/api/v1/knowledge-bases/copy", false},
		{http.MethodGet, "/api/v1/faq/import/progress/t-1", false},
		// Deny: every write under an allowlisted prefix.
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/knowledge/file", false},
		{http.MethodDelete, "/api/v1/knowledge-bases/kb-1/knowledge", false},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/faq/entries", false},
		{http.MethodPost, "/api/v1/knowledge-bases/kb-1/tags", false},
		{http.MethodPost, "/api/v1/knowledgebase/kb-1/wiki/pages", false},
		{http.MethodPut, "/api/v1/knowledgebase/kb-1/wiki/pages/some-slug", false},
		{http.MethodDelete, "/api/v1/knowledge/doc-1", false},
		{http.MethodDelete, "/api/v1/chunks/doc-1/ch-1", false},
		// Deny: unknown shapes and trailing-slash variants fail closed.
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/unknown", false},
		{http.MethodGet, "/api/v1/knowledge-bases/kb-1/knowledge/", false},
		{http.MethodGet, "/api/v1/knowledgebase/kb-1/other", false},
		{http.MethodPost, "/api/v1/knowledge/abc/download", false},
	}
	for _, tt := range tests {
		if got := isTenantOptionalAPI(tt.path, tt.method); got != tt.want {
			t.Errorf("isTenantOptionalAPI(%s %s) = %v, want %v", tt.method, tt.path, got, tt.want)
		}
	}
}
