package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// Profile 9 holds the agents; workspaces 1, 2 and 3 inherit from it.
// "group" applies to workspace 1 only.
func newAgentScopeService(t *testing.T) (*customAgentService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.CustomAgent{}, &types.UsageSubscription{}, &types.UsageSubscriptionAddon{}))
	require.NoError(t, db.Exec(`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT,
		kind TEXT NOT NULL DEFAULT 'workspace', config_profile_id INTEGER, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, name, kind, config_profile_id) VALUES
		(9, 'Cấu hình chung', 'config_profile', NULL), (1, 'Nhóm thuế', 'workspace', 9),
		(2, 'Phòng khác', 'workspace', 9), (3, 'Phòng ba', 'workspace', 9)`).Error)
	repo := repository.NewCustomAgentRepository(db)
	require.NoError(t, repo.CreateAgent(context.Background(), &types.CustomAgent{
		ID: "group", Name: "Thuế", TenantID: 9, Distribution: types.AgentDistributionInternal,
		AppliesToTenantIDs: types.AgentTenantScope{1},
		Config:             types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning},
	}))
	require.NoError(t, repo.CreateAgent(context.Background(), &types.CustomAgent{
		ID: "everyone", Name: "Chung", TenantID: 9,
		Config: types.CustomAgentConfig{AgentMode: types.AgentModeSmartReasoning},
	}))
	return &customAgentService{repo: repo}, db
}

func scopeMemberCtx(tenantID uint64) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	return types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: "user-1", Role: types.TenantRoleAdmin})
}

func scopeSuperAdminCtx(tenantID uint64) context.Context {
	ctx := context.WithValue(scopeMemberCtx(tenantID), types.SystemAdminContextKey, true)
	return types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: "super-1"})
}

func customAgentIDs(agents []*types.CustomAgent) []string {
	var ids []string
	for _, a := range agents {
		if !a.IsBuiltin {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

func TestAgentTenantScopeServiceReads(t *testing.T) {
	svc, _ := newAgentScopeService(t)

	// in the list: visible; outside: not found, the same error as a bad id
	got, err := svc.GetAgentByID(scopeMemberCtx(1), "group")
	require.NoError(t, err)
	require.Equal(t, uint64(1), got.TenantID)
	_, err = svc.GetAgentByID(scopeMemberCtx(2), "group")
	require.ErrorIs(t, err, ErrAgentNotFound)
	_, err = svc.GetAgentByIDAndTenant(scopeMemberCtx(2), "group", 2)
	require.ErrorIs(t, err, ErrAgentNotFound)

	list, err := svc.ListAgents(scopeMemberCtx(2))
	require.NoError(t, err)
	require.Equal(t, []string{"everyone"}, customAgentIDs(list))
	list, err = svc.ListAgents(scopeMemberCtx(1))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"group", "everyone"}, customAgentIDs(list))

	// suggested questions resolve the agent the same way
	_, err = svc.GetSuggestedQuestions(scopeMemberCtx(2), "group", nil, nil, nil, 3)
	require.ErrorIs(t, err, ErrAgentNotFound)

	// a SuperAdmin sees and opens every agent from any workspace
	got, err = svc.GetAgentByID(scopeSuperAdminCtx(2), "group")
	require.NoError(t, err)
	require.Equal(t, types.AgentTenantScope{1}, got.AppliesToTenantIDs)
	list, err = svc.ListAgents(scopeSuperAdminCtx(2))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"group", "everyone"}, customAgentIDs(list))

	// the system-admin flag on an API key is not a SuperAdmin
	keyCtx := types.WithTenantAPIKeyScope(scopeSuperAdminCtx(2), types.TenantAPIKeyScope{})
	_, err = svc.GetAgentByID(keyCtx, "group")
	require.ErrorIs(t, err, ErrAgentNotFound)
}

func TestAgentTenantScopeServiceWrites(t *testing.T) {
	svc, _ := newAgentScopeService(t)
	// SuperAdmin writes run in the profile (ConfigProfile middleware)
	profile := scopeSuperAdminCtx(9)

	created, err := svc.CreateAgent(profile, &types.CustomAgent{
		Name: "Mới", AppliesToTenantIDs: types.AgentTenantScope{3, 1, 3, 0},
	})
	require.NoError(t, err)
	require.Equal(t, types.AgentTenantScope{1, 3}, created.AppliesToTenantIDs, "sorted, deduplicated")

	_, err = svc.CreateAgent(profile, &types.CustomAgent{Name: "Sai", AppliesToTenantIDs: types.AgentTenantScope{9}})
	require.ErrorIs(t, err, ErrAgentTenantScopeInvalid, "the profile itself is not a workspace")
	_, err = svc.CreateAgent(profile, &types.CustomAgent{Name: "Sai", AppliesToTenantIDs: types.AgentTenantScope{42}})
	require.ErrorIs(t, err, ErrAgentTenantScopeInvalid)

	// update: absent list keeps it, a sent list replaces it, [] clears it
	updated, err := svc.UpdateAgent(profile, &types.CustomAgent{ID: "group", Name: "Thuế 2"}, nil)
	require.NoError(t, err)
	require.Equal(t, types.AgentTenantScope{1}, updated.AppliesToTenantIDs)
	updated, err = svc.UpdateAgent(profile, &types.CustomAgent{
		ID: "group", Name: "Thuế 2", AppliesToTenantIDs: types.AgentTenantScope{2},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, types.AgentTenantScope{2}, updated.AppliesToTenantIDs)
	_, err = svc.GetAgentByID(scopeMemberCtx(1), "group")
	require.ErrorIs(t, err, ErrAgentNotFound, "workspace 1 lost the agent")
	_, err = svc.GetAgentByID(scopeMemberCtx(2), "group")
	require.NoError(t, err)
	_, err = svc.UpdateAgent(profile, &types.CustomAgent{
		ID: "group", Name: "Thuế 2", AppliesToTenantIDs: types.AgentTenantScope{42},
	}, nil)
	require.ErrorIs(t, err, ErrAgentTenantScopeInvalid)
	updated, err = svc.UpdateAgent(profile, &types.CustomAgent{
		ID: "group", Name: "Thuế 2", AppliesToTenantIDs: types.AgentTenantScope{},
	}, nil)
	require.NoError(t, err)
	require.Empty(t, updated.AppliesToTenantIDs)
	_, err = svc.GetAgentByID(scopeMemberCtx(3), "group")
	require.NoError(t, err, "an empty list applies to every workspace")
}

func TestAgentTenantScopeRefusedOnBuiltin(t *testing.T) {
	restore := withBuiltinModelAgent(t, "mode-chat")
	defer restore()
	svc, _ := newAgentScopeService(t)
	_, err := svc.UpdateAgent(scopeSuperAdminCtx(9), &types.CustomAgent{
		ID: "builtin-quick-answer", Config: types.CustomAgentConfig{ModelID: "mode-chat"},
		AppliesToTenantIDs: types.AgentTenantScope{1},
	}, nil)
	require.ErrorIs(t, err, ErrBuiltinAgentNotScopable)
	// an empty list is the normal built-in save
	_, err = svc.UpdateAgent(scopeSuperAdminCtx(9), &types.CustomAgent{
		ID: "builtin-quick-answer", Config: types.CustomAgentConfig{ModelID: "mode-chat"},
		AppliesToTenantIDs: types.AgentTenantScope{},
	}, nil)
	require.NoError(t, err)
}

func TestCopyAgentKeepsTenantScope(t *testing.T) {
	svc, _ := newAgentScopeService(t)
	copied, err := svc.CopyAgent(scopeSuperAdminCtx(9), "group")
	require.NoError(t, err)
	require.Equal(t, types.AgentTenantScope{1}, copied.AppliesToTenantIDs)
	_, err = svc.GetAgentByID(scopeMemberCtx(2), copied.ID)
	require.ErrorIs(t, err, ErrAgentNotFound, "the copy stays with the work group")
	_, err = svc.GetAgentByID(scopeMemberCtx(1), copied.ID)
	require.NoError(t, err)

	// a caller outside the list cannot copy what it cannot see
	_, err = svc.CopyAgent(scopeMemberCtx(2), "group")
	require.ErrorIs(t, err, ErrAgentNotFound)
}
