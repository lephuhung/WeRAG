package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// writableGrantLookup mimics ApprovedKBPermission: it reports the stored
// grant permission for foreign KBs. Tenant access grants never make a KB
// writable — even an editor grant — so the permission value is irrelevant
// to writability; it only exercises that the lookup result is ignored for
// writes.
type writableGrantLookup map[string]types.KBPermission

func (l writableGrantLookup) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return nil, nil
}

func (l writableGrantLookup) ApprovedKBPermission(
	_ context.Context, kbID string, _ uint64,
) (types.KBPermission, bool, error) {
	permission, ok := l[kbID]
	return permission, ok, nil
}

type publicWritableLookup struct{}

func (publicWritableLookup) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return &types.KBScope{TenantID: 42, Visibility: types.KBVisibilityPublic}, nil
}

func (publicWritableLookup) ApprovedKBPermission(context.Context, string, uint64) (types.KBPermission, bool, error) {
	return "", false, nil
}

func TestKBWritableIDs(t *testing.T) {
	targets := types.SearchTargets{
		{KnowledgeBaseID: "own", TenantID: 42},
		{KnowledgeBaseID: "shared-editor", TenantID: 7},
		{KnowledgeBaseID: "shared-viewer", TenantID: 7},
		{KnowledgeBaseID: "agent-scope", TenantID: 7},
	}
	grants := writableGrantLookup{"shared-editor": types.KBPermissionEditor, "shared-viewer": types.KBPermissionViewer}
	caller := func(role types.TenantRole, userID string) context.Context {
		return types.WithCaller(context.Background(), types.Caller{TenantID: 42, UserID: userID, Role: role})
	}
	apiKey := func(scope types.TenantAPIKeyScope) context.Context {
		return types.WithTenantAPIKeyScope(caller(types.TenantRoleMember, "system-42"), scope)
	}

	// Members upload into their own tenant's KBs; foreign KBs stay read-only
	// even when a grant exists — grants carry viewer semantics only.
	require.Equal(t, []string{"own"},
		kbWritableIDs(caller(types.TenantRoleMember, "u"), grants, targets, true))
	require.Equal(t, []string{"own"}, kbWritableIDs(caller(types.TenantRoleMember, ""), grants, targets, true),
		"callers without a user do not expand through tenant grants")

	require.Equal(t, []string{"own"}, kbWritableIDs(caller(types.TenantRoleMember, "u"), grants, targets, true))
	require.Equal(t, []string{"own"}, kbWritableIDs(caller(types.TenantRoleMember, "u"), grants, targets, false))
	// A stale public marker does not reduce the owning tenant Member's
	// ability to upload into their own KB.
	require.Equal(t, []string{"own"}, kbWritableIDs(
		caller(types.TenantRoleMember, "u"), publicWritableLookup{}, targets[:1], true))

	// Scoped API keys write only with the ingest capability.
	chatOnly := types.TenantAPIKeyScope{Capabilities: types.StringArray{string(types.APIKeyCapabilityChat)}}
	require.Empty(t, kbWritableIDs(apiKey(chatOnly), grants, targets, true))
	ingest := types.TenantAPIKeyScope{Capabilities: types.StringArray{string(types.APIKeyCapabilityIngest)}}
	require.Equal(t, []string{"own"}, kbWritableIDs(apiKey(ingest), grants, targets, true))
}
