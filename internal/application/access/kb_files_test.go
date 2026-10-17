package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// kbBoundCatalog resolves one registered resource of tenant 1 and reports
// its KB bindings, like the resource catalog service.
type kbBoundCatalog struct {
	fileCatalog
	kbIDs     []string
	sourceKBs []string
}

func (c kbBoundCatalog) GetMessageFileBindings(context.Context, uint64, string, string) (*types.MessageFileBindings, error) {
	return &types.MessageFileBindings{KnowledgeBaseIDs: c.kbIDs, SourceFileKBIDs: c.sourceKBs}, nil
}

type kbMapLookup map[string]*types.KnowledgeBase

func (m kbMapLookup) GetKnowledgeBasesByIDsOnly(_ context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	out := make([]*types.KnowledgeBase, 0, len(ids))
	for _, id := range ids {
		if kb := m[id]; kb != nil {
			out = append(out, kb)
		}
	}
	return out, nil
}

// grantsWithInvites is a grant lookup that also resolves invites, like the
// real grant service.
type grantsWithInvites struct {
	*grantLookup
	*stubInvites
}

func TestMessageFilesFollowDecideKBForKBContent(t *testing.T) {
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	restricted := &types.KnowledgeBase{
		ID: "kb-r", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityRestricted,
	}
	tenantKB := &types.KnowledgeBase{ID: "kb-t", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant}
	resource := &types.StoredResource{
		Handle: "AbCdEfGhIjKlMnOpQrStUv", TenantID: 1, PhysicalPath: "local://1/exports/a.png",
	}
	message := &types.Message{ID: "m", Role: "assistant", AgentTenantID: 1, Content: "![image](" + ref + ")"}
	invites := &stubInvites{accepted: map[string]bool{}}
	authorizer := MessageKBGrantAuthorizer{
		GrantGuard: grantsWithInvites{&grantLookup{grants: map[string]types.KBPermission{}}, invites},
		KBs:        kbMapLookup{"kb-r": restricted, "kb-t": tenantKB},
	}
	member := callerContext() // tenant 1, Member
	admin := context.WithValue(callerContext(), types.TenantRoleContextKey, types.TenantRoleAdmin)

	image := kbBoundCatalog{fileCatalog: fileCatalog{resource}, kbIDs: []string{"kb-r"}}
	_, err := AuthorizeMessageFile(member, message, ref, image, authorizer)
	require.ErrorIs(t, err, ErrForbidden, "same-tenant storage no longer opens a restricted KB's image")
	_, err = AuthorizeMessageFile(admin, message, ref, image, authorizer)
	require.NoError(t, err, "the owner tenant's Admin keeps reads")

	invites.accepted["kb-r\x00user"] = true
	file, err := AuthorizeMessageFile(member, message, ref, image, authorizer)
	require.NoError(t, err, "an invite opens the preview")
	require.Equal(t, uint64(1), file.OwnerTenantID)

	original := kbBoundCatalog{fileCatalog: fileCatalog{resource}, kbIDs: []string{"kb-r"}, sourceKBs: []string{"kb-r"}}
	_, err = AuthorizeMessageFile(member, message, ref, original, authorizer)
	require.ErrorIs(t, err, ErrForbidden, "an ACL viewer never fetches the original upload")
	_, err = AuthorizeMessageFile(admin, message, ref, original, authorizer)
	require.NoError(t, err)

	tenantImage := kbBoundCatalog{fileCatalog: fileCatalog{resource}, kbIDs: []string{"kb-t"}}
	delete(invites.accepted, "kb-r\x00user")
	_, err = AuthorizeMessageFile(member, message, ref, tenantImage, authorizer)
	require.NoError(t, err, "tenant-scope KB content stays readable by members")

	unbound := kbBoundCatalog{fileCatalog: fileCatalog{resource}}
	_, err = AuthorizeMessageFile(member, message, ref, unbound, authorizer)
	require.NoError(t, err, "non-KB session files keep the same-tenant rule")

	// Artifacts take the same gate before the session-ownership shortcut.
	artifactMsg := &types.Message{ID: "m", Artifacts: types.MessageArtifacts{{URL: ref, FileName: "a.png"}}}
	_, err = ResolveMessageArtifact(member, artifactMsg, 0, image, authorizer)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = ResolveMessageArtifact(member, artifactMsg, 0, unbound, authorizer)
	require.NoError(t, err)
}

func TestKBFileGateInactiveWithoutLookups(t *testing.T) {
	bound, err := KBFileGate{}.Authorize(context.Background(), 1, "resource://x")
	require.False(t, bound)
	require.NoError(t, err)
}
