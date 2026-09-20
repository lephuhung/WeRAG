package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// builtinModelAgentRepo serves one stored builtin-agent config (or
// ErrCustomAgentNotFound) and captures writes so tests can assert what a
// builtin-mode update would have persisted.
type builtinModelAgentRepo struct {
	interfaces.CustomAgentRepository
	stored *types.CustomAgent
	saved  *types.CustomAgent
}

func (r *builtinModelAgentRepo) GetAgentByID(
	_ context.Context, _ string, _ uint64,
) (*types.CustomAgent, error) {
	if r.stored == nil {
		return nil, repository.ErrCustomAgentNotFound
	}
	return r.stored, nil
}

func (r *builtinModelAgentRepo) UpdateAgent(_ context.Context, a *types.CustomAgent) error {
	r.saved = a
	return nil
}

func (r *builtinModelAgentRepo) CreateAgent(_ context.Context, a *types.CustomAgent) error {
	r.saved = a
	return nil
}

func builtinModelTestCtx(systemAdmin bool) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	if systemAdmin {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	return ctx
}

func withBuiltinModelAgent(t *testing.T, modelID string) func() {
	t.Helper()
	types.BuiltinAgentRegistry["builtin-quick-answer"] = func(tenantID uint64) *types.CustomAgent {
		return &types.CustomAgent{
			ID:        "builtin-quick-answer",
			Name:      "Quick Answer",
			IsBuiltin: true,
			TenantID:  tenantID,
			Config:    types.CustomAgentConfig{ModelID: modelID},
		}
	}
	return func() { delete(types.BuiltinAgentRegistry, "builtin-quick-answer") }
}

// A tenant admin may keep editing a builtin agent's config surface as long as
// the response-mode model assignment stays untouched.
func TestUpdateBuiltinAgentAllowsNonModelChangesForNonAdmin(t *testing.T) {
	restore := withBuiltinModelAgent(t, "mode-chat")
	defer restore()
	svc := &customAgentService{
		repo: &builtinModelAgentRepo{
			stored: &types.CustomAgent{
				ID:        "builtin-quick-answer",
				IsBuiltin: true,
				TenantID:  1,
				Config:    types.CustomAgentConfig{ModelID: "mode-chat"},
			},
		},
	}

	got, err := svc.UpdateAgent(builtinModelTestCtx(false), &types.CustomAgent{
		ID:     "builtin-quick-answer",
		Config: types.CustomAgentConfig{ModelID: "mode-chat", Temperature: 0.2},
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, "mode-chat", got.Config.ModelID)
}

// The model behind a builtin response mode is a platform assignment: without
// model-config rights a changed model_id is rejected outright.
func TestUpdateBuiltinAgentRejectsModelChangeForNonAdmin(t *testing.T) {
	restore := withBuiltinModelAgent(t, "mode-chat")
	defer restore()
	repo := &builtinModelAgentRepo{
		stored: &types.CustomAgent{
			ID:        "builtin-quick-answer",
			IsBuiltin: true,
			TenantID:  1,
			Config:    types.CustomAgentConfig{ModelID: "mode-chat"},
		},
	}
	svc := &customAgentService{repo: repo}

	_, err := svc.UpdateAgent(builtinModelTestCtx(false), &types.CustomAgent{
		ID:     "builtin-quick-answer",
		Config: types.CustomAgentConfig{ModelID: "other-chat"},
	}, nil)

	require.ErrorIs(t, err, ErrBuiltinModelManagedByAdmin)
	assert.Nil(t, repo.saved)
}

// A system admin assigns the model a builtin response mode runs on — the
// first persisted config record counts as a change too.
func TestUpdateBuiltinAgentAllowsModelChangeForSystemAdmin(t *testing.T) {
	restore := withBuiltinModelAgent(t, "")
	defer restore()
	repo := &builtinModelAgentRepo{}
	svc := &customAgentService{repo: repo}

	got, err := svc.UpdateAgent(builtinModelTestCtx(true), &types.CustomAgent{
		ID:     "builtin-quick-answer",
		Config: types.CustomAgentConfig{ModelID: "mode-chat"},
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, "mode-chat", got.Config.ModelID)
	assert.NotNil(t, repo.saved)
}
