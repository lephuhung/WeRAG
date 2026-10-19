package repository

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

// agentScopeFixture: profile 9 with workspaces 1, 2, 3 on it and workspace 4
// on no profile. "everyone" has no list, "group" applies to 1 and 3 only,
// "only-two" to 2 only, and workspace 1 overrides "only-two" with its own row.
func agentScopeFixture(t *testing.T) (*gorm.DB, *customAgentRepository) {
	t.Helper()
	db := setupModelUsageTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, kind, config_profile_id, deleted_at) VALUES
		(9, 'config_profile', NULL, NULL), (1, 'workspace', 9, NULL), (2, 'workspace', 9, NULL),
		(3, 'workspace', 9, NULL), (4, 'workspace', NULL, NULL), (5, 'workspace', 9, CURRENT_TIMESTAMP)`).Error)
	repo := NewCustomAgentRepository(db).(*customAgentRepository)
	ctx := context.Background()
	for _, a := range []*types.CustomAgent{
		{ID: "everyone", Name: "everyone", TenantID: 9},
		{ID: "group", Name: "group", TenantID: 9, Distribution: types.AgentDistributionInternal, AppliesToTenantIDs: types.AgentTenantScope{1, 3}},
		{ID: "only-two", Name: "only-two (profile)", TenantID: 9, Distribution: types.AgentDistributionInternal, AppliesToTenantIDs: types.AgentTenantScope{2}},
		{ID: "only-two", Name: "only-two (workspace 1)", TenantID: 1},
	} {
		require.NoError(t, repo.CreateAgent(ctx, a))
	}
	return db, repo
}

func agentIDs(agents []*types.CustomAgent) []string {
	ids := make([]string, 0, len(agents))
	for _, a := range agents {
		ids = append(ids, a.ID+"="+a.Name)
	}
	sort.Strings(ids)
	return ids
}

func TestAgentTenantScopeFiltersInheritedAgents(t *testing.T) {
	_, repo := agentScopeFixture(t)
	ctx := context.Background()

	cases := map[uint64][]string{
		// workspace 1 is in "group"; its own "only-two" row stays visible
		// (the list restricts inheritance, not ownership)
		1: {"everyone=everyone", "group=group", "only-two=only-two (workspace 1)"},
		2: {"everyone=everyone", "only-two=only-two (profile)"},
		3: {"everyone=everyone", "group=group"},
		// the profile owns every row and sees them all
		9: {"everyone=everyone", "group=group", "only-two=only-two (profile)"},
	}
	for tenantID, want := range cases {
		list, err := repo.ListAgentsByTenantID(ctx, tenantID)
		require.NoError(t, err)
		require.Equal(t, want, agentIDs(list), "tenant %d", tenantID)
		for _, a := range list {
			require.Equal(t, tenantID, a.TenantID, "rows are rebound to the reader")
		}
	}

	// Get answers not-found outside the list, the same error as a missing id.
	_, err := repo.GetAgentByID(ctx, "group", 2)
	require.ErrorIs(t, err, ErrCustomAgentNotFound)
	_, err = repo.GetAgentByID(ctx, "only-two", 3)
	require.ErrorIs(t, err, ErrCustomAgentNotFound)
	got, err := repo.GetAgentByID(ctx, "group", 3)
	require.NoError(t, err)
	require.Equal(t, types.AgentTenantScope{1, 3}, got.AppliesToTenantIDs)
	got, err = repo.GetAgentByID(ctx, "only-two", 1)
	require.NoError(t, err)
	require.Equal(t, "only-two (workspace 1)", got.Name)

	// The SuperAdmin reads skip the list.
	all, err := repo.ListAgentsByTenantIDIgnoringTenantScope(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, []string{"everyone=everyone", "group=group", "only-two=only-two (profile)"}, agentIDs(all))
	got, err = repo.GetAgentByIDIgnoringTenantScope(ctx, "group", 2)
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.TenantID)
}

func TestAgentTenantScopeEmptyListMeansEveryone(t *testing.T) {
	db, repo := agentScopeFixture(t)
	ctx := context.Background()
	// a row written before the column existed reads back as an empty list
	require.NoError(t, db.Exec(`INSERT INTO custom_agents (id, name, tenant_id, config) VALUES ('legacy', 'legacy', 9, '{}')`).Error)
	got, err := repo.GetAgentByID(ctx, "legacy", 2)
	require.NoError(t, err)
	require.Empty(t, got.AppliesToTenantIDs)

	// clearing the list widens the agent back to every workspace
	group, err := repo.GetAgentByID(ctx, "group", 9)
	require.NoError(t, err)
	group.AppliesToTenantIDs = types.AgentTenantScope{}
	require.NoError(t, repo.UpdateAgent(ctx, group))
	_, err = repo.GetAgentByID(ctx, "group", 2)
	require.NoError(t, err)
}

func TestInvalidScopeTenantIDs(t *testing.T) {
	_, repo := agentScopeFixture(t)
	ctx := context.Background()
	// 1-3 inherit from profile 9; 4 has no profile, 5 is deleted, 9 is the
	// profile itself, 77 does not exist
	invalid, err := repo.InvalidScopeTenantIDs(ctx, 9, []uint64{1, 2, 3, 4, 5, 9, 77})
	require.NoError(t, err)
	require.Equal(t, []uint64{4, 5, 9, 77}, invalid)
	// a workspace that is not a profile has no inheritors
	invalid, err = repo.InvalidScopeTenantIDs(ctx, 1, []uint64{2})
	require.NoError(t, err)
	require.Equal(t, []uint64{2}, invalid)
	invalid, err = repo.InvalidScopeTenantIDs(ctx, 9, nil)
	require.NoError(t, err)
	require.Empty(t, invalid)
}

// The Postgres branch renders the jsonb containment the migration's GIN index
// serves and the add-on EXISTS; the SQLite tests above only exercise the
// json_each branch.
func TestAgentTenantScopePostgresSQL(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=x dbname=x sslmode=disable"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)
	stmt := whereAgentVisibleTo(
		db.Model(&types.CustomAgent{}).Where(configScopeSQL("tenant_id"), configScopeArgs(7)...),
		types.AgentViewer{TenantID: 7, UserID: "u-1"},
	).Find(&[]*types.CustomAgent{}).Statement
	sql := stmt.SQL.String()
	require.Contains(t, sql, "COALESCE(applies_to_tenant_ids, '[]'::jsonb) = '[]'::jsonb")
	require.Contains(t, sql, "applies_to_tenant_ids @> jsonb_build_array($6::bigint)")
	require.Contains(t, sql, "(tenant_id = $3 OR distribution = $4")
	require.Contains(t, sql, "WHERE usa.agent_id = custom_agents.id")
	require.Contains(t, sql, "OR (us.scope_type = $15 AND us.scope_id = $16)")
	require.Equal(t, "u-1", stmt.Vars[len(stmt.Vars)-4])
	require.Contains(t, sql, "(is_builtin = $17 OR COALESCE(enabled, $18) = $19)")
}
