package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// readableFixture seeds one owner tenant (t1), one other tenant (t2) and
// the ACL rows that ListReadableKnowledgeBases must honour exactly like
// access.DecideKB: live invites (current owner, unexpired, recipient
// membership active), approved unexpired tenant grants, scope rules.
type readableFixture struct {
	db     *gorm.DB
	repo   *knowledgeBaseRepository
	t1, t2 uint64
}

func newReadableFixture(t *testing.T) *readableFixture {
	t.Helper()
	db := setupTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.KBInvitation{}, &types.KBAccessGrant{}))
	return seedReadableFixture(t, db)
}

func seedReadableFixture(t *testing.T, db *gorm.DB) *readableFixture {
	t.Helper()
	f := &readableFixture{db: db, repo: NewKnowledgeBaseRepository(db).(*knowledgeBaseRepository)}
	f.t1 = seedCatalogTenant(t, db, "t1")
	f.t2 = seedCatalogTenant(t, db, "t2")
	t3 := seedCatalogTenant(t, db, "t3")

	kb := func(id string, owner uint64, vis types.KBVisibility) {
		seedCatalogKB(t, db, id, owner, owner, vis, false)
	}
	kb("t1-tenant", f.t1, types.KBVisibilityTenant)
	kb("t1-published", f.t1, types.KBVisibilityPublished)
	kb("t1-restricted", f.t1, types.KBVisibilityRestricted)
	kb("t1-restricted-invite", f.t1, types.KBVisibilityRestricted)
	kb("t1-restricted-stale-member", f.t1, types.KBVisibilityRestricted)
	kb("t1-restricted-expired", f.t1, types.KBVisibilityRestricted)
	kb("t1-restricted-old-owner", f.t1, types.KBVisibilityRestricted)
	kb("t1-restricted-grant", f.t1, types.KBVisibilityRestricted)
	kb("t1-tenant-grant", f.t1, types.KBVisibilityTenant)
	kb("t1-restricted-revoked-grant", f.t1, types.KBVisibilityRestricted)
	kb("t1-restricted-expired-grant", f.t1, types.KBVisibilityRestricted)
	kb("t2-tenant", f.t2, types.KBVisibilityTenant)
	kb("t2-published", f.t2, types.KBVisibilityPublished)
	seedCatalogKB(t, db, "platform-public", 0, 0, types.KBVisibilityPublic, false)
	seedCatalogKB(t, db, "t1-temporary", f.t1, f.t1, types.KBVisibilityTenant, true)
	kb("t1-deleted", f.t1, types.KBVisibilityTenant)
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET deleted_at = ? WHERE id = ?", time.Now(), "t1-deleted").Error)

	member := func(user string, tenant uint64, role types.TenantRole, status types.TenantMemberStatus) {
		require.NoError(t, db.Create(&types.TenantMember{
			UserID: user, TenantID: tenant, Role: role, Status: status, JoinedAt: time.Now(),
		}).Error)
	}
	member("m1", f.t1, types.TenantRoleMember, types.TenantMemberStatusActive)
	member("a1", f.t1, types.TenantRoleAdmin, types.TenantMemberStatusActive)
	member("m2", f.t2, types.TenantRoleMember, types.TenantMemberStatusActive)
	member("m1", t3, types.TenantRoleMember, types.TenantMemberStatusSuspended)

	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	invite := func(id, kbID string, owner, recipientTenant uint64, expires *time.Time) {
		require.NoError(t, db.Create(&types.KBInvitation{
			ID: id, KBID: kbID, OwnerTenantID: owner, RecipientUserID: "m1",
			RecipientTenantID: recipientTenant, InviterUserID: "a1",
			TokenHash: types.HashKBInviteToken(id), Status: types.KBInvitationStatusAccepted,
			ExpiresAt: expires,
		}).Error)
	}
	invite("inv-live", "t1-restricted-invite", f.t1, f.t1, &future)
	invite("inv-live-dup", "t1-restricted-invite", f.t1, f.t1, nil)
	invite("inv-stale", "t1-restricted-stale-member", f.t1, t3, nil)
	invite("inv-expired", "t1-restricted-expired", f.t1, f.t1, &past)
	invite("inv-old-owner", "t1-restricted-old-owner", 999, f.t1, nil)

	grant := func(id, kbID string, status types.GrantStatus, expires *time.Time) {
		require.NoError(t, db.Create(&types.KBAccessGrant{
			ID: id, KBID: kbID, OwnerTenantID: f.t1, GranteeTenantID: f.t2,
			Permission: types.KBPermissionViewer, Status: status, ExpiresAt: expires,
		}).Error)
	}
	grant("g-live", "t1-restricted-grant", types.GrantStatusApproved, nil)
	grant("g-tenant", "t1-tenant-grant", types.GrantStatusApproved, &future)
	grant("g-revoked", "t1-restricted-revoked-grant", types.GrantStatusRevoked, nil)
	grant("g-expired", "t1-restricted-expired-grant", types.GrantStatusApproved, &past)
	return f
}

func (f *readableFixture) list(t *testing.T, reader types.KBReader) map[string]string {
	t.Helper()
	kbs, err := f.repo.ListReadableKnowledgeBases(context.Background(), reader)
	require.NoError(t, err)
	out := make(map[string]string, len(kbs))
	for _, kb := range kbs {
		_, dup := out[kb.ID]
		require.False(t, dup, "one row per KB: %s", kb.ID)
		out[kb.ID] = kb.AccessSource
	}
	return out
}

func TestListReadableKnowledgeBases(t *testing.T) {
	runReadableCases(t, newReadableFixture(t))
}

func runReadableCases(t *testing.T, f *readableFixture) {
	t1, t2 := f.t1, f.t2
	allT1 := []string{
		"t1-tenant", "t1-published", "t1-restricted", "t1-restricted-invite",
		"t1-restricted-stale-member", "t1-restricted-expired", "t1-restricted-old-owner",
		"t1-restricted-grant", "t1-tenant-grant", "t1-restricted-revoked-grant",
		"t1-restricted-expired-grant",
	}
	with := func(source string, ids []string, extra map[string]string) map[string]string {
		out := map[string]string{}
		for _, id := range ids {
			out[id] = source
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	for _, tt := range []struct {
		name   string
		reader types.KBReader
		want   map[string]string
	}{
		{
			name:   "owner-tenant member: tenant/published scope, restricted only with a live invite",
			reader: types.KBReader{TenantID: t1, UserID: "m1", Human: true, TenantMember: true},
			want: map[string]string{
				"t1-tenant": "tenant_scope", "t1-published": "tenant_scope", "t1-tenant-grant": "tenant_scope",
				"t1-restricted-invite": "acl_user",
				"platform-public":      "public", "t2-published": "published",
			},
		},
		{
			name:   "owner-tenant admin: every KB of the tenant",
			reader: types.KBReader{TenantID: t1, UserID: "a1", Human: true, TenantMember: true, TenantAdmin: true},
			want: with("tenant_admin", allT1, map[string]string{
				"platform-public": "public", "t2-published": "published",
			}),
		},
		{
			name:   "other-tenant member: ACL rows from approved live grants only",
			reader: types.KBReader{TenantID: t2, UserID: "m2", Human: true, TenantMember: true},
			want: map[string]string{
				"t2-tenant": "tenant_scope", "t2-published": "tenant_scope",
				"t1-published": "published", "platform-public": "public",
				"t1-restricted-grant": "acl_tenant", "t1-tenant-grant": "acl_tenant",
			},
		},
		{
			name:   "guest (no tenant role): public and published only, no tenant grant",
			reader: types.KBReader{TenantID: t2, UserID: "g", Human: true},
			want: map[string]string{
				"t1-published": "published", "t2-published": "published", "platform-public": "public",
			},
		},
		{
			name:   "superadmin: every non-temporary live KB",
			reader: types.KBReader{UserID: "root", Human: true, SuperAdmin: true},
			want: with("superadmin", append(append([]string{}, allT1...),
				"t2-tenant", "t2-published", "platform-public"), nil),
		},
		{
			name:   "machine principal: own tenant only, never public/published/ACL",
			reader: types.KBReader{TenantID: t1, UserID: "system-1"},
			want:   with("tenant_scope", allT1, nil),
		},
		{
			name:   "tenantless machine: nothing",
			reader: types.KBReader{},
			want:   map[string]string{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, f.list(t, tt.reader))
		})
	}
}

func TestListReadableKnowledgeBases_RevocationIsImmediate(t *testing.T) {
	f := newReadableFixture(t)
	m1 := types.KBReader{TenantID: f.t1, UserID: "m1", Human: true, TenantMember: true}
	m2 := types.KBReader{TenantID: f.t2, UserID: "m2", Human: true, TenantMember: true}
	require.Contains(t, f.list(t, m1), "t1-restricted-invite")
	require.Contains(t, f.list(t, m2), "t1-restricted-grant")

	require.NoError(t, f.db.Model(&types.KBInvitation{}).Where("kb_id = ?", "t1-restricted-invite").
		Update("status", types.KBInvitationStatusRevoked).Error)
	require.NoError(t, f.db.Model(&types.KBAccessGrant{}).Where("id = ?", "g-live").
		Update("status", types.GrantStatusRevoked).Error)
	assert.NotContains(t, f.list(t, m1), "t1-restricted-invite")
	assert.NotContains(t, f.list(t, m2), "t1-restricted-grant")

	// Switching a tenant KB to restricted drops it for plain members.
	require.Contains(t, f.list(t, m1), "t1-tenant")
	require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET visibility = ? WHERE id = ?",
		types.KBVisibilityRestricted, "t1-tenant").Error)
	assert.NotContains(t, f.list(t, m1), "t1-tenant")
}

func TestListReadableKnowledgeBases_LoadsFullRows(t *testing.T) {
	f := newReadableFixture(t)
	kbs, err := f.repo.ListReadableKnowledgeBases(context.Background(),
		types.KBReader{TenantID: f.t1, UserID: "a1", Human: true, TenantMember: true, TenantAdmin: true})
	require.NoError(t, err)
	require.NotEmpty(t, kbs)
	for _, kb := range kbs {
		require.NotEmpty(t, kb.Name)
		require.Equal(t, kb.OwnerTenantID, kb.TenantID)
		require.True(t, kb.Visibility.IsValid())
	}
}

func TestListForeignKnowledgeBasesSkipsRestricted(t *testing.T) {
	f := newReadableFixture(t)
	kbs, err := f.repo.ListForeignKnowledgeBasesByTenantID(context.Background(), f.t1)
	require.NoError(t, err)
	for _, kb := range kbs {
		require.NotEqual(t, types.KBVisibilityRestricted, kb.Visibility, kb.ID)
	}
	ids := map[string]bool{}
	for _, kb := range kbs {
		ids[kb.ID] = true
	}
	require.True(t, ids["t1-tenant"])
	require.True(t, ids["t1-published"])
	require.False(t, ids["t1-restricted"])
}
