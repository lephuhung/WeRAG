package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAgentsInheritFromConfigProfile(t *testing.T) {
	ctx := context.Background()
	db := setupModelUsageTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, kind, config_profile_id) VALUES
		(9, 'config_profile', NULL), (1, 'workspace', 9), (2, 'workspace', 9), (3, 'workspace', NULL)`).Error)
	repo := NewCustomAgentRepository(db)
	for _, a := range []*types.CustomAgent{
		{ID: "shared", Name: "from profile", TenantID: 9},
		{ID: "overridden", Name: "profile copy", TenantID: 9},
		{ID: "overridden", Name: "workspace copy", TenantID: 1},
	} {
		require.NoError(t, repo.CreateAgent(ctx, a))
	}

	// An inherited agent is returned as the workspace's own: downstream code
	// reads agent.TenantID as the tenant the agent runs in.
	got, err := repo.GetAgentByID(ctx, "shared", 2)
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.TenantID)
	require.Equal(t, "from profile", got.Name)

	// The workspace's own row wins over the profile's.
	got, err = repo.GetAgentByID(ctx, "overridden", 1)
	require.NoError(t, err)
	require.Equal(t, "workspace copy", got.Name)
	got, err = repo.GetAgentByID(ctx, "overridden", 2)
	require.NoError(t, err)
	require.Equal(t, "profile copy", got.Name)

	// A workspace without a profile sees nothing it does not own.
	_, err = repo.GetAgentByID(ctx, "shared", 3)
	require.ErrorIs(t, err, ErrCustomAgentNotFound)

	list, err := repo.ListAgentsByTenantID(ctx, 1)
	require.NoError(t, err)
	names := map[string]string{}
	for _, a := range list {
		require.Equal(t, uint64(1), a.TenantID)
		names[a.ID] = a.Name
	}
	require.Equal(t, map[string]string{"shared": "from profile", "overridden": "workspace copy"}, names)
}

// A model in a profile is in use wherever a workspace on that profile
// references it, so the delete guard run in the profile must count them.
func TestModelUsageCountsWorkspacesOnProfile(t *testing.T) {
	ctx := context.Background()
	db := setupModelUsageTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, kind, config_profile_id) VALUES
		(9, 'config_profile', NULL), (1, 'workspace', 9), (3, 'workspace', NULL)`).Error)
	kbRepo := NewKnowledgeBaseRepository(db)

	inProfileWorkspace := makeKB(nil)
	inProfileWorkspace.TenantID = 1
	inProfileWorkspace.EmbeddingModelID = "shared-embed"
	require.NoError(t, kbRepo.CreateKnowledgeBase(ctx, inProfileWorkspace))
	elsewhere := makeKB(nil)
	elsewhere.TenantID = 3
	elsewhere.EmbeddingModelID = "shared-embed"
	require.NoError(t, kbRepo.CreateKnowledgeBase(ctx, elsewhere))

	count, err := kbRepo.CountByModelID(ctx, 9, "shared-embed")
	require.NoError(t, err)
	require.Equal(t, int64(1), count, "profile 9 counts its workspace 1, not unrelated workspace 3")

	count, err = kbRepo.CountByModelID(ctx, 3, "shared-embed")
	require.NoError(t, err)
	require.Equal(t, int64(1), count, "a workspace still counts only itself")
}

func TestModelsInheritFromConfigProfile(t *testing.T) {
	ctx := context.Background()
	db := setupModelUsageTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.Model{}))
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, kind, config_profile_id) VALUES
		(9, 'config_profile', NULL), (1, 'workspace', 9), (3, 'workspace', NULL)`).Error)
	repo := NewModelRepository(db)
	require.NoError(t, repo.Create(ctx, &types.Model{ID: "profile-chat", TenantID: 9, Name: "chat", Type: types.ModelTypeKnowledgeQA}))
	require.NoError(t, repo.Create(ctx, &types.Model{ID: "builtin", TenantID: 1, Name: "b", Type: types.ModelTypeKnowledgeQA, IsBuiltin: true}))

	got, err := repo.GetByID(ctx, 1, "profile-chat")
	require.NoError(t, err)
	require.NotNil(t, got)

	got, err = repo.GetByID(ctx, 3, "profile-chat")
	require.NoError(t, err)
	require.Nil(t, got, "a workspace off the profile does not see its models")

	list, err := repo.List(ctx, 3, "", "")
	require.NoError(t, err)
	require.Len(t, list, 1, "built-in models stay visible everywhere")
}
