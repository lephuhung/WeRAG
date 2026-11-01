package database

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The 000172 rollback removes every usage counter guest chat wrote — the
// Guests' own scope, the portal sessions' and the portal tenant's — before
// the portal sessions and tenant they point at are deleted.
func TestMigration000172DownRemovesGuestChatCounters(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(sqliteRepoRoot(t), "migrations", "versioned", "000172_guest_chat.down.sql"))
	require.NoError(t, err)
	sql := regexp.MustCompile(`--[^\n]*`).ReplaceAllString(string(raw), "")
	sql = strings.Join(strings.Fields(sql), " ")
	for _, scope := range []string{"guest", "session", "tenant"} {
		require.Containsf(t, sql, "DELETE FROM usage_counters WHERE scope_type = '"+scope+"'", "scope %s", scope)
	}
	counters := strings.Index(sql, "DELETE FROM usage_counters WHERE scope_type = 'session'")
	sessions := strings.Index(sql, "DELETE FROM sessions")
	tenants := strings.Index(sql, "DELETE FROM tenants")
	require.Less(t, counters, sessions, "session counters go before their sessions")
	require.Less(t, strings.Index(sql, "DELETE FROM usage_counters WHERE scope_type = 'tenant'"), tenants)
}
