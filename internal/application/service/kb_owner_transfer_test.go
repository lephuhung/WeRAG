package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 4: POST /knowledge-bases/:id/transfer-owner — explicit human
// SuperAdmin only, never through public, revokes every share.

func newTransferFixture(kb *types.KnowledgeBase, tenants ...uint64) (*fakeKBRepo, *lifecycleTenantRepo,
	*lifecycleAudit, *knowledgeBaseService,
) {
	repo := newFakeKBRepo()
	repo.rows[kb.ID] = kb
	tr := newLifecycleTenantRepo(tenants...)
	audit := &lifecycleAudit{}
	return repo, tr, audit, newLifecycleService(repo, tr, audit)
}

func TestTransferOwner_SuperAdminMovesTenantKB(t *testing.T) {
	repo, _, audit, svc := newTransferFixture(&types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
		MemberDefault: types.KBMemberDefaultView,
	}, 1, 2)
	repo.scopeRevoked = types.KBShareRevocation{Invitations: 3, Grants: 2}

	res, err := svc.TransferKnowledgeBaseOwner(superAdminNoTenantLifecycleCtx(), "kb-1", 2)
	require.NoError(t, err)
	kb := res.KnowledgeBase
	assert.Equal(t, uint64(2), kb.OwnerTenantID)
	assert.Equal(t, types.KBVisibilityTenant, kb.Visibility)
	assert.Equal(t, uint64(1), kb.TenantID, "data scope tenant_id is unchanged")
	assert.Equal(t, types.KBMemberDefaultView, kb.MemberDefault, "member_default is unchanged")
	assert.Equal(t, uint64(1), res.PreviousOwnerTenantID)
	assert.Equal(t, int64(3), res.RevokedInvitations)
	assert.Equal(t, int64(2), res.RevokedGrants)
	assert.False(t, res.WithdrawnFromPublished)
	assert.Equal(t, []string{types.KBInviteRevokedReasonOwnerChanged}, repo.scopeRevokeReasons)

	require.Len(t, audit.entries, 1)
	entry := audit.entries[0]
	assert.Equal(t, types.AuditActionKBOwnerTransferred, entry.Action)
	assert.Equal(t, uint64(2), entry.TenantID)
	details := decodeScopeDetails(t, entry)
	assert.Equal(t, float64(1), details["old_owner_tenant"])
	assert.Equal(t, float64(2), details["new_owner_tenant"])
	assert.Equal(t, float64(2), details["target_tenant_id"])
	assert.Equal(t, float64(3), details["revoked_invitations"])
	assert.Equal(t, float64(2), details["revoked_grants"])
}

func TestTransferOwner_PublishedIsWithdrawnToTenant(t *testing.T) {
	_, _, _, svc := newTransferFixture(&types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityPublished,
	}, 1, 2)
	res, err := svc.TransferKnowledgeBaseOwner(superAdminLifecycleCtx(1), "kb-1", 2)
	require.NoError(t, err)
	assert.Equal(t, types.KBVisibilityTenant, res.KnowledgeBase.Visibility)
	assert.Equal(t, types.KBVisibilityPublished, res.PreviousVisibility)
	assert.True(t, res.WithdrawnFromPublished)
}

// A restricted KB must not widen to the new owner's whole tenant: it stays
// restricted, every ACL is revoked, so only the new owner's Admins (and
// SuperAdmins) read it until they share it.
func TestTransferOwner_RestrictedStaysRestricted(t *testing.T) {
	repo, _, _, svc := newTransferFixture(&types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityRestricted,
		MemberDefault: types.KBMemberDefaultContribute,
	}, 1, 2)
	repo.scopeRevoked = types.KBShareRevocation{Invitations: 2}
	res, err := svc.TransferKnowledgeBaseOwner(superAdminLifecycleCtx(1), "kb-1", 2)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), res.KnowledgeBase.OwnerTenantID)
	assert.Equal(t, types.KBVisibilityRestricted, res.KnowledgeBase.Visibility)
	assert.Equal(t, types.KBVisibilityRestricted, res.PreviousVisibility)
	assert.False(t, res.WithdrawnFromPublished)
	assert.Equal(t, int64(2), res.RevokedInvitations)
	assert.Equal(t, []string{types.KBInviteRevokedReasonOwnerChanged}, repo.scopeRevokeReasons)
	assert.Equal(t, types.KBVisibilityRestricted, repo.rows["kb-1"].Visibility, "persisted scope")
}

func TestTransferOwner_PlatformPublicToTenant(t *testing.T) {
	repo, _, audit, svc := newTransferFixture(&types.KnowledgeBase{
		ID: "pub-1", TenantID: 5, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}, 2)
	res, err := svc.TransferKnowledgeBaseOwner(superAdminNoTenantLifecycleCtx(), "pub-1", 2)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), res.KnowledgeBase.OwnerTenantID)
	assert.Equal(t, types.KBVisibilityTenant, res.KnowledgeBase.Visibility)
	assert.Equal(t, uint64(5), res.KnowledgeBase.TenantID, "data scope tenant_id is unchanged")
	assert.False(t, res.WithdrawnFromPublished)
	assert.Equal(t, types.KBVisibilityPublic, res.PreviousVisibility)
	require.NoError(t, res.KnowledgeBase.ValidateOwnership())
	assert.Equal(t, []string{types.KBInviteRevokedReasonOwnerChanged}, repo.scopeRevokeReasons)
	require.Len(t, audit.entries, 1)
	assert.Equal(t, types.AuditActionKBOwnerTransferred, audit.entries[0].Action)
}

func TestTransferOwner_Denied(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"owner tenant admin":            tenantAdminLifecycleCtx(1),
		"target tenant admin":           tenantAdminLifecycleCtx(2),
		"api key manage_kbs + sys flag": apiKeyManageLifecycleCtx(),
		"can-access-all-tenants only":   crossTenantOnlyLifecycleCtx(),
	} {
		repo, _, audit, svc := newTransferFixture(&types.KnowledgeBase{
			ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
		}, 1, 2)
		_, err := svc.TransferKnowledgeBaseOwner(ctx, "kb-1", 2)
		requireHTTPCode(t, err, http.StatusForbidden)
		assert.Equal(t, uint64(1), repo.rows["kb-1"].OwnerTenantID, name)
		assert.Empty(t, repo.scopeRevokeReasons, name)
		assert.Empty(t, audit.entries, name)
	}
	_, _, _, svc := newTransferFixture(&types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant,
	}, 1, 2)
	tenantless := types.WithCaller(context.Background(), types.Caller{UserID: "u"})
	_, err := svc.TransferKnowledgeBaseOwner(tenantless, "kb-1", 2)
	requireHTTPCode(t, err, http.StatusUnauthorized)
}

func TestTransferOwner_RejectsBadTargets(t *testing.T) {
	setup := func() (*fakeKBRepo, *lifecycleTenantRepo, *knowledgeBaseService) {
		repo, tr, _, svc := newTransferFixture(&types.KnowledgeBase{
			ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityPublished,
		}, 1, 2)
		tr.tenants[3] = &types.Tenant{ID: 3, Name: "profile", Kind: types.TenantKindConfigProfile}
		tr.tenants[4] = &types.Tenant{ID: 4, Name: "off", Status: "inactive"}
		return repo, tr, svc
	}
	ctx := superAdminNoTenantLifecycleCtx()
	cases := []struct {
		name   string
		kbID   string
		target uint64
		code   int
	}{
		{"missing target tenant", "kb-1", 99, http.StatusNotFound},
		{"config profile", "kb-1", 3, http.StatusBadRequest},
		{"inactive tenant", "kb-1", 4, http.StatusBadRequest},
		{"same owner", "kb-1", 1, http.StatusBadRequest},
		{"no target", "kb-1", 0, http.StatusBadRequest},
		{"missing kb", "nope", 2, http.StatusNotFound},
	}
	for _, tc := range cases {
		repo, _, svc := setup()
		_, err := svc.TransferKnowledgeBaseOwner(ctx, tc.kbID, tc.target)
		requireHTTPCode(t, err, tc.code)
		stored := repo.rows["kb-1"]
		assert.Equal(t, uint64(1), stored.OwnerTenantID, tc.name)
		assert.Equal(t, types.KBVisibilityPublished, stored.Visibility, tc.name)
		assert.Empty(t, repo.scopeRevokeReasons, tc.name)
	}
}

func TestTransferOwner_TemporaryKBRejected(t *testing.T) {
	_, _, _, svc := newTransferFixture(&types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityTenant, IsTemporary: true,
	}, 1, 2)
	_, err := svc.TransferKnowledgeBaseOwner(superAdminNoTenantLifecycleCtx(), "kb-1", 2)
	requireHTTPCode(t, err, http.StatusBadRequest)
}

// transferRevokingRepo stands in for the repository transaction: an
// owner-changing scope write revokes the KB's invitations in the shared
// fake invite store, so HasLiveInvite can be checked end to end.
type transferRevokingRepo struct {
	*fakeKBRepo
	invites *fakeKBInviteRepo
}

func (r *transferRevokingRepo) UpdateKnowledgeBaseScope(
	ctx context.Context, kb *types.KnowledgeBase, reason string,
) (types.KBShareRevocation, error) {
	if _, err := r.fakeKBRepo.UpdateKnowledgeBaseScope(ctx, kb, reason); err != nil || reason == "" {
		return types.KBShareRevocation{}, err
	}
	r.invites.mu.Lock()
	defer r.invites.mu.Unlock()
	var n int64
	for _, inv := range r.invites.rows {
		if inv.KBID == kb.ID && (inv.Status == types.KBInvitationStatusPending ||
			inv.Status == types.KBInvitationStatusAccepted) {
			inv.Status = types.KBInvitationStatusRevoked
			inv.RevokedReason = reason
			n++
		}
	}
	return types.KBShareRevocation{Invitations: n}, nil
}

func TestTransferOwner_LiveInviteGoneAfterTransfer(t *testing.T) {
	base := newFakeKBRepo()
	base.rows["kb-1"] = &types.KnowledgeBase{
		ID: "kb-1", TenantID: 1, OwnerTenantID: 1, Visibility: types.KBVisibilityRestricted,
	}
	invites := newFakeKBInviteRepo()
	now := time.Now()
	invites.rows["inv-1"] = &types.KBInvitation{
		ID: "inv-1", KBID: "kb-1", OwnerTenantID: 1, RecipientUserID: "u-b", RecipientTenantID: 1,
		Status: types.KBInvitationStatusAccepted, AcceptedAt: &now,
	}
	repo := &transferRevokingRepo{fakeKBRepo: base, invites: invites}
	inviteSvc := NewKBInvitationService(invites, repo, nil, nil, nil)
	ctx := context.Background()
	live, err := inviteSvc.HasLiveInvite(ctx, "kb-1", "u-b")
	require.NoError(t, err)
	require.True(t, live, "fixture: the invite is live before the transfer")

	svc := &knowledgeBaseService{repo: repo, tenantRepo: newLifecycleTenantRepo(1, 2), audit: &lifecycleAudit{}}
	res, err := svc.TransferKnowledgeBaseOwner(superAdminNoTenantLifecycleCtx(), "kb-1", 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.RevokedInvitations)
	assert.Equal(t, types.KBInviteRevokedReasonOwnerChanged, invites.rows["inv-1"].RevokedReason)
	live, err = inviteSvc.HasLiveInvite(ctx, "kb-1", "u-b")
	require.NoError(t, err)
	assert.False(t, live, "a transferred KB keeps no share of the previous owner")
}

// The visibility endpoint no longer changes owners.
func TestSetVisibility_PublicToTenantPointsToTransfer(t *testing.T) {
	repo, _, audit, svc := newTransferFixture(&types.KnowledgeBase{
		ID: "pub-1", TenantID: 0, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic,
	}, 2)
	_, err := svc.SetKnowledgeBaseVisibility(superAdminNoTenantLifecycleCtx(), "pub-1", types.KBVisibilityTenant, 2)
	requireHTTPCode(t, err, http.StatusBadRequest)
	assert.Contains(t, err.Error(), "transfer-owner")
	assert.Equal(t, types.KBVisibilityPublic, repo.rows["pub-1"].Visibility)
	assert.Equal(t, uint64(0), repo.rows["pub-1"].OwnerTenantID)
	assert.Empty(t, audit.entries)
}
