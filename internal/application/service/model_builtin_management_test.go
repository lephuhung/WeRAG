package service

import (
	"context"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func builtinModelContext(systemAdmin bool) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	return context.WithValue(ctx, types.SystemAdminContextKey, systemAdmin)
}

// tenantMemberContext simulates an authenticated tenant member (any role)
// with no system-admin flag and no API-key scope — the fail-closed input
// requireModelConfigAuthority must reject.
func tenantMemberContext() context.Context {
	return context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
}

// platformKeyContext simulates a platform API key — the only non-user
// principal allowed to manage model configuration.
func platformKeyContext() context.Context {
	ctx := tenantMemberContext()
	return context.WithValue(ctx, types.TenantAPIKeyScopeContextKey,
		types.TenantAPIKeyScope{ScopeType: types.APIKeyScopePlatform})
}

// Model configuration is platform-owned: the service layer must refuse
// tenant principals on every catalog mutation, not just on built-in rows.
func TestModelMutations_RequireModelConfigAuthority(t *testing.T) {
	tenantModel := &types.Model{ID: "m-tenant", TenantID: 7}
	newKey := "sk-new"

	cases := []struct {
		name string
		run  func(svc interfaces.ModelService) error
	}{
		{"create", func(svc interfaces.ModelService) error {
			return svc.CreateModel(tenantMemberContext(), &types.Model{
				Name: "m", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceRemote,
			})
		}},
		{"update", func(svc interfaces.ModelService) error {
			return svc.UpdateModel(tenantMemberContext(), &types.Model{ID: tenantModel.ID})
		}},
		{"delete", func(svc interfaces.ModelService) error {
			return svc.DeleteModel(tenantMemberContext(), tenantModel.ID)
		}},
		{"credentials update", func(svc interfaces.ModelService) error {
			_, err := svc.UpdateModelCredentials(tenantMemberContext(), tenantModel.ID, &newKey, nil)
			return err
		}},
		{"credentials clear", func(svc interfaces.ModelService) error {
			return svc.ClearModelCredential(tenantMemberContext(), tenantModel.ID, "api_key")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewModelService(
				&stubModelRepoForDelete{model: tenantModel},
				&stubKBRepoForModelDelete{}, &stubAgentRepoForModelDelete{},
				nil, nil, nil, nil,
			)
			err := tc.run(svc)
			require.Error(t, err)
			appErr, ok := apperrors.IsAppError(err)
			require.True(t, ok)
			assert.Equal(t, apperrors.ErrForbidden, appErr.Code)
		})
	}
}

func TestCreateModel_SystemAdminAndPlatformKeyAllowed(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"system admin", builtinModelContext(true)},
		{"platform api key", platformKeyContext()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewModelService(
				&stubModelRepoForDelete{},
				&stubKBRepoForModelDelete{}, &stubAgentRepoForModelDelete{},
				nil, nil, nil, nil,
			)
			err := svc.CreateModel(tc.ctx, &types.Model{
				Name: "m", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceRemote,
			})
			require.NoError(t, err)
		})
	}
}

func TestUpdateBuiltinModel_RequiresSystemAdmin(t *testing.T) {
	stored := &types.Model{
		ID: "builtin-chat", TenantID: 10000, IsBuiltin: true,
		ManagedBy: types.BuiltinModelManagedBy,
	}
	updated := false
	svc := NewModelService(&stubModelRepoForDelete{
		model: stored,
		update: func(*types.Model) error {
			updated = true
			return nil
		},
	}, nil, nil, nil, nil, nil, nil)

	err := svc.UpdateModel(builtinModelContext(false), &types.Model{ID: stored.ID})
	require.Error(t, err)
	appErr, ok := apperrors.IsAppError(err)
	require.True(t, ok)
	assert.Equal(t, apperrors.ErrForbidden, appErr.Code)
	assert.False(t, updated)
}

func TestUpdateBuiltinModel_SystemAdminCreatesRuntimeOverride(t *testing.T) {
	stored := &types.Model{
		ID: "builtin-chat", TenantID: 10000, IsBuiltin: true,
		ManagedBy: types.BuiltinModelManagedBy,
	}
	var saved *types.Model
	svc := NewModelService(&stubModelRepoForDelete{
		model: stored,
		update: func(model *types.Model) error {
			copy := *model
			saved = &copy
			return nil
		},
	}, nil, nil, nil, nil, nil, nil)

	input := &types.Model{ID: stored.ID, Name: "edited"}
	require.NoError(t, svc.UpdateModel(builtinModelContext(true), input))
	require.NotNil(t, saved)
	assert.Equal(t, uint64(10000), saved.TenantID)
	assert.True(t, saved.IsBuiltin)
	assert.Empty(t, saved.ManagedBy, "UI edit must stop later YAML reconciliation")
}

func TestUpdateBuiltinModelCredentials_SystemAdminOnly(t *testing.T) {
	newKey := "sk-new"

	t.Run("tenant admin denied", func(t *testing.T) {
		stored := &types.Model{ID: "builtin-chat", TenantID: 10000, IsBuiltin: true}
		svc := NewModelService(&stubModelRepoForDelete{model: stored}, nil, nil, nil, nil, nil, nil)
		_, err := svc.UpdateModelCredentials(builtinModelContext(false), stored.ID, &newKey, nil)
		require.Error(t, err)
		appErr, ok := apperrors.IsAppError(err)
		require.True(t, ok)
		assert.Equal(t, apperrors.ErrForbidden, appErr.Code)
	})

	t.Run("system admin saves runtime override", func(t *testing.T) {
		stored := &types.Model{
			ID: "builtin-chat", TenantID: 10000, IsBuiltin: true,
			ManagedBy: types.BuiltinModelManagedBy,
		}
		var saved *types.Model
		svc := NewModelService(&stubModelRepoForDelete{
			model: stored,
			update: func(model *types.Model) error {
				copy := *model
				saved = &copy
				return nil
			},
		}, nil, nil, nil, nil, nil, nil)

		updated, err := svc.UpdateModelCredentials(
			builtinModelContext(true), stored.ID, &newKey, nil,
		)
		require.NoError(t, err)
		assert.Equal(t, newKey, updated.Parameters.APIKey)
		require.NotNil(t, saved)
		assert.Empty(t, saved.ManagedBy)
	})
}
