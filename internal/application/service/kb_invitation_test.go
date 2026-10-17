package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type fakeKBInviteRepo struct {
	mu   sync.Mutex
	rows map[string]*types.KBInvitation
	byID func(id string) *types.KBInvitation
	// inactive marks recipients whose recipient-tenant membership is no
	// longer active (HasLiveInviteForOwner).
	inactive map[string]bool
}

func newFakeKBInviteRepo() *fakeKBInviteRepo {
	return &fakeKBInviteRepo{rows: map[string]*types.KBInvitation{}}
}

func (f *fakeKBInviteRepo) Create(_ context.Context, inv *types.KBInvitation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *inv
	f.rows[inv.ID] = &cp
	return nil
}

func (f *fakeKBInviteRepo) GetByID(_ context.Context, id string) (*types.KBInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeKBInviteRepo) GetByTokenHash(_ context.Context, h string) (*types.KBInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.TokenHash == h {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeKBInviteRepo) GetPendingByPair(_ context.Context, kbID, userID string) (*types.KBInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.KBID == kbID && r.RecipientUserID == userID && r.Status == types.KBInvitationStatusPending {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeKBInviteRepo) ListByKB(_ context.Context, kbID string, statuses []types.KBInvitationStatus) ([]*types.KBInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*types.KBInvitation
	for _, r := range f.rows {
		if r.KBID != kbID {
			continue
		}
		if len(statuses) > 0 {
			match := false
			for _, s := range statuses {
				if r.Status == s {
					match = true
				}
			}
			if !match {
				continue
			}
		}
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeKBInviteRepo) ListByRecipient(_ context.Context, userID string, statuses []types.KBInvitationStatus) ([]*types.KBInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*types.KBInvitation
	for _, r := range f.rows {
		if r.RecipientUserID != userID {
			continue
		}
		_ = statuses
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeKBInviteRepo) MarkAccepted(_ context.Context, id string) (*types.KBInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.Status != types.KBInvitationStatusPending {
		return nil, gorm.ErrRecordNotFound
	}
	now := time.Now()
	r.Status = types.KBInvitationStatusAccepted
	r.AcceptedAt = &now
	cp := *r
	return &cp, nil
}

func (f *fakeKBInviteRepo) MarkRevoked(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || (r.Status != types.KBInvitationStatusPending && r.Status != types.KBInvitationStatusAccepted) {
		return gorm.ErrRecordNotFound
	}
	r.Status = types.KBInvitationStatusRevoked
	return nil
}

// ListLiveInvitedKBIDsByUser: the fake has no knowledge_bases table, so the
// current-owner join is covered by the repository test; here every row's
// owner is taken as current.
func (f *fakeKBInviteRepo) ListLiveInvitedKBIDsByUser(_ context.Context, userID string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.rows {
		if r.RecipientUserID != userID || r.Status != types.KBInvitationStatusAccepted {
			continue
		}
		if r.ExpiresAt != nil && !r.ExpiresAt.After(time.Now()) {
			continue
		}
		out = append(out, r.KBID)
	}
	return out, nil
}

func (f *fakeKBInviteRepo) MarkStatusIfPending(_ context.Context, id string, status types.KBInvitationStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.Status != types.KBInvitationStatusPending {
		return gorm.ErrRecordNotFound
	}
	r.Status = status
	return nil
}

func (f *fakeKBInviteRepo) HasAcceptedInvite(_ context.Context, kbID, userID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.KBID == kbID && r.RecipientUserID == userID && r.Status == types.KBInvitationStatusAccepted {
			if r.ExpiresAt != nil && !r.ExpiresAt.After(time.Now()) {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeKBInviteRepo) HasAcceptedInviteForOwner(_ context.Context, kbID, userID string, ownerTenantID uint64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.KBID == kbID && r.RecipientUserID == userID && r.OwnerTenantID == ownerTenantID &&
			r.Status == types.KBInvitationStatusAccepted {
			if r.ExpiresAt != nil && !r.ExpiresAt.After(time.Now()) {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeKBInviteRepo) HasLiveInviteForOwner(ctx context.Context, kbID, userID string, ownerTenantID uint64) (bool, error) {
	ok, err := f.HasAcceptedInviteForOwner(ctx, kbID, userID, ownerTenantID)
	if err != nil || !ok {
		return ok, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.inactive[userID], nil
}

func (f *fakeKBInviteRepo) DeleteByKBID(_ context.Context, kbID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, r := range f.rows {
		if r.KBID == kbID {
			delete(f.rows, id)
		}
	}
	return nil
}

type fakeKBRepoForInvite struct {
	kb *types.KnowledgeBase
}

func (f *fakeKBRepoForInvite) GetKnowledgeBaseByID(_ context.Context, _ string) (*types.KnowledgeBase, error) {
	return f.kb, nil
}

type fakeMemberSvcForInvite struct {
	active map[string]bool // key userID + "\x00" + tenant
}

func (f *fakeMemberSvcForInvite) GetMembership(_ context.Context, userID string, tenantID uint64) (*types.TenantMember, error) {
	if f.active[userID+"\x00"+string(rune(tenantID))] || f.active[userID] {
		return &types.TenantMember{UserID: userID, TenantID: tenantID, Status: types.TenantMemberStatusActive}, nil
	}
	return nil, nil
}

func TestKBInviteIssueAcceptRevoked(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant, Name: "KB"}}
	members := &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}
	svc := NewKBInvitationService(repo, kbRepo, members, nil, nil)

	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
	created, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-b", RecipientTenantID: 200,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if created.Token == "" {
		t.Fatal("plaintext token must be returned once at creation")
	}
	// Stored row must hold only the hash.
	stored, _ := repo.GetByTokenHash(ctx, types.HashKBInviteToken(created.Token))
	if stored == nil {
		t.Fatal("invite must be resolvable by token hash")
	}
	if stored.TokenHash == created.Token {
		t.Fatal("raw token must never be stored")
	}

	// Wrong user (same tenant) cannot redeem.
	wrong := types.Caller{TenantID: 200, UserID: "user-c", Role: types.TenantRoleMember}
	if _, err := svc.Accept(ctx, wrong, created.Token); err == nil {
		t.Fatal("wrong user must NOT accept another recipient's invite")
	}

	// Recipient without membership in intended tenant cannot redeem.
	noMember := &fakeMemberSvcForInvite{active: map[string]bool{}}
	svcNoMember := NewKBInvitationService(repo, kbRepo, noMember, nil, nil)
	recipient := types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}
	if _, err := svcNoMember.Accept(ctx, recipient, created.Token); err == nil {
		t.Fatal("recipient without intended-tenant membership must NOT accept")
	}

	// Valid recipient accepts.
	accepted, err := svc.Accept(ctx, recipient, created.Token)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if accepted.Status != types.KBInvitationStatusAccepted {
		t.Errorf("status = %q, want accepted", accepted.Status)
	}

	// Replay fails.
	if _, err := svc.Accept(ctx, recipient, created.Token); err == nil {
		t.Fatal("replayed token must NOT accept twice")
	}

	// Revoking an ACCEPTED invite succeeds and stops access immediately.
	if err := svc.Revoke(ctx, admin, created.ID); err != nil {
		t.Fatalf("revoking an accepted invite must succeed, got %v", err)
	}
	if ok, _ := svc.HasAcceptedInvite(ctx, "kb-1", "user-b"); ok {
		t.Fatal("revoked invite must stop authorizing reads immediately")
	}
	// Second revoke is a no-op failure (already terminal).
	if err := svc.Revoke(ctx, admin, created.ID); err == nil {
		t.Fatal("revoking an already-revoked invite must fail")
	}
}

func TestKBInviteRevokePending(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant, Name: "KB"}}
	members := &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}
	svc := NewKBInvitationService(repo, kbRepo, members, nil, nil)
	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
	created, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-b", RecipientTenantID: 200,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := svc.Revoke(ctx, admin, created.ID); err != nil {
		t.Fatalf("revoking a pending invite must succeed, got %v", err)
	}
	recipient := types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}
	if _, err := svc.Accept(ctx, recipient, created.Token); err == nil {
		t.Fatal("revoked pending invite must NOT accept")
	}
}

func TestKBInviteIssueRejectsNonMemberRecipient(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant, Name: "KB"}}
	members := &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}
	svc := NewKBInvitationService(repo, kbRepo, members, nil, nil)
	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
	// user-stranger exists nowhere: no membership in tenant 200.
	if _, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-stranger", RecipientTenantID: 200,
	}); !errors.Is(err, ErrKBInviteRecipientNotMember) {
		t.Fatalf("want ErrKBInviteRecipientNotMember, got %v", err)
	}
	// Nil member reader fails closed.
	svcNoMembers := NewKBInvitationService(repo, kbRepo, nil, nil, nil)
	if _, err := svcNoMembers.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-b", RecipientTenantID: 200,
	}); !errors.Is(err, ErrKBInviteRecipientNotMember) {
		t.Fatalf("nil member reader must fail closed, got %v", err)
	}
}

func TestKBInviteMemberCannotIssue(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant}}
	svc := NewKBInvitationService(repo, kbRepo, nil, nil, nil)
	member := types.Caller{TenantID: 100, UserID: "m-1", Role: types.TenantRoleMember}
	if _, err := svc.Issue(ctx, member, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-b", RecipientTenantID: 200,
	}); err == nil {
		t.Fatal("member must NOT issue KB invites")
	}
}

func TestKBInviteExpiredTokenRejected(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant}}
	members := &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}
	svc := NewKBInvitationService(repo, kbRepo, members, nil, nil)
	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
	past := time.Now().Add(-time.Hour)
	created, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-b", RecipientTenantID: 200, ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	recipient := types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}
	if _, err := svc.Accept(ctx, recipient, created.Token); err == nil {
		t.Fatal("expired invite must NOT accept")
	}
}

func TestKBInvitePublicKBOwnerSemantics(t *testing.T) {
	publicKB := &types.KnowledgeBase{
		ID: "kb-pub", TenantID: 100,
		OwnerTenantID: 0, Visibility: types.KBVisibilityPublic, Name: "Public",
	}
	newSvc := func() (*fakeKBInviteRepo, interfaces.KBInvitationService) {
		repo := newFakeKBInviteRepo()
		members := &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}
		svc := NewKBInvitationService(repo, &fakeKBRepoForInvite{kb: publicKB}, members, nil, nil)
		return repo, svc
	}
	issue := &types.CreateKBInviteRequest{RecipientUserID: "user-b", RecipientTenantID: 200}

	t.Run("data-scope tenant admin cannot invite on platform-owned rows", func(t *testing.T) {
		_, svc := newSvc()
		admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
		if _, err := svc.Issue(context.Background(), admin, "kb-pub", issue); err == nil {
			t.Fatal("data-scope tenant admin must NOT manage invites on a platform-owned KB")
		}
		if _, err := svc.ListByKB(context.Background(), admin, "kb-pub"); err == nil {
			t.Fatal("data-scope tenant admin must NOT list invites on a platform-owned KB")
		}
	})

	t.Run("cross-tenant member cannot invite on platform-owned rows", func(t *testing.T) {
		_, svc := newSvc()
		member := types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}
		if _, err := svc.Issue(context.Background(), member, "kb-pub", issue); err == nil {
			t.Fatal("cross-tenant member must NOT manage invites on a platform-owned KB")
		}
	})

	t.Run("no-active-tenant explicit SuperAdmin may manage invites", func(t *testing.T) {
		_, svc := newSvc()
		ctx := context.WithValue(context.Background(), types.SystemAdminContextKey, true)
		super := types.Caller{TenantID: 0, UserID: "super-1", Role: types.TenantRoleMember}
		created, err := svc.Issue(ctx, super, "kb-pub", issue)
		if err != nil {
			t.Fatalf("no-tenant SuperAdmin issue: %v", err)
		}
		if created.OwnerTenantID != 0 {
			t.Fatalf("invite owner = %d, want platform owner 0", created.OwnerTenantID)
		}
		if _, err := svc.ListByKB(ctx, super, "kb-pub"); err != nil {
			t.Fatalf("no-tenant SuperAdmin list: %v", err)
		}
		if err := svc.Revoke(ctx, super, created.ID); err != nil {
			t.Fatalf("no-tenant SuperAdmin revoke: %v", err)
		}
	})
}

func TestKBInviteOwnerAdminExplicitHumanSuperAdmin(t *testing.T) {
	publicKB := &types.KnowledgeBase{
		ID: "kb-pub", TenantID: 100,
		OwnerTenantID: 0, Visibility: types.KBVisibilityPublic, Name: "Public",
	}
	tenantKB := &types.KnowledgeBase{
		ID: "kb-1", TenantID: 100,
		OwnerTenantID: 100, Visibility: types.KBVisibilityTenant, Name: "KB",
	}
	newSvcFor := func(kb *types.KnowledgeBase) interfaces.KBInvitationService {
		members := &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}
		return NewKBInvitationService(newFakeKBInviteRepo(), &fakeKBRepoForInvite{kb: kb}, members, nil, nil)
	}
	issue := &types.CreateKBInviteRequest{RecipientUserID: "user-b", RecipientTenantID: 200}
	sysFlag := func(ctx context.Context) context.Context {
		return context.WithValue(ctx, types.SystemAdminContextKey, true)
	}

	t.Run("nonzero-tenant explicit human SuperAdmin manages platform-owned rows", func(t *testing.T) {
		svc := newSvcFor(publicKB)
		ctx := sysFlag(context.Background())
		super := types.Caller{TenantID: 3, UserID: "super-1", Role: types.TenantRoleMember}
		created, err := svc.Issue(ctx, super, "kb-pub", issue)
		if err != nil {
			t.Fatalf("nonzero-tenant SuperAdmin issue: %v", err)
		}
		if _, err := svc.ListByKB(ctx, super, "kb-pub"); err != nil {
			t.Fatalf("nonzero-tenant SuperAdmin list: %v", err)
		}
		if err := svc.Revoke(ctx, super, created.ID); err != nil {
			t.Fatalf("nonzero-tenant SuperAdmin revoke: %v", err)
		}
	})

	t.Run("synthetic identity with system-admin flag denied on platform-owned rows", func(t *testing.T) {
		svc := newSvcFor(publicKB)
		ctx := sysFlag(context.Background())
		synth := types.Caller{TenantID: 3, UserID: "system-3", Role: types.TenantRoleAdmin}
		if _, err := svc.Issue(ctx, synth, "kb-pub", issue); err == nil {
			t.Fatal("synthetic identity must NOT manage invites even with the system-admin flag")
		}
	})

	t.Run("API-key identity with system-admin flag denied on platform-owned rows", func(t *testing.T) {
		svc := newSvcFor(publicKB)
		ctx := sysFlag(types.WithTenantAPIKeyScope(context.Background(),
			types.TenantAPIKeyScope{FullAccess: true}))
		key := types.Caller{TenantID: 3, UserID: "super-1", Role: types.TenantRoleAdmin}
		if _, err := svc.Issue(ctx, key, "kb-pub", issue); err == nil {
			t.Fatal("API-key principal must NOT manage invites even with the system-admin flag")
		}
		if _, err := svc.ListByKB(ctx, key, "kb-pub"); err == nil {
			t.Fatal("API-key principal must NOT list invites even with the system-admin flag")
		}
	})

	t.Run("tenant rows keep strict same-tenant TenantAdmin without platform override", func(t *testing.T) {
		svc := newSvcFor(tenantKB)
		admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
		if _, err := svc.Issue(context.Background(), admin, "kb-1", issue); err != nil {
			t.Fatalf("ordinary tenant Admin issue unchanged: %v", err)
		}
		flaggedMember := types.Caller{TenantID: 100, UserID: "m-1", Role: types.TenantRoleMember}
		if _, err := svc.Issue(sysFlag(context.Background()), flaggedMember, "kb-1", issue); err == nil {
			t.Fatal("system-admin flag must NOT override tenant ownership for non-admin members")
		}
		foreignSuper := types.Caller{TenantID: 300, UserID: "super-1", Role: types.TenantRoleAdmin}
		if _, err := svc.Issue(sysFlag(context.Background()), foreignSuper, "kb-1", issue); err == nil {
			t.Fatal("platform-admin status must NOT override another tenant's ownership")
		}
	})
}

// F5: an invite issued by a previous owner must stop authorizing reads
// once the KB has a new owner, and the new owner's admin must be able to
// revoke it.
func TestKBInviteStaleAfterOwnerChange(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	_ = repo.Create(ctx, &types.KBInvitation{
		ID: "inv-old", KBID: "kb-1", OwnerTenantID: 100,
		RecipientUserID: "user-b", RecipientTenantID: 200,
		InviterUserID: "admin-old", TokenHash: types.HashKBInviteToken("t"),
		Status: types.KBInvitationStatusAccepted,
	})
	// The KB has since moved to tenant 300 (data scope stays 100).
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{
		ID: "kb-1", TenantID: 100, OwnerTenantID: 300, Visibility: types.KBVisibilityTenant, Name: "KB",
	}}
	svc := NewKBInvitationService(repo, kbRepo, &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}, nil, nil)

	if ok, err := svc.HasAcceptedInvite(ctx, "kb-1", "user-b"); err != nil || ok {
		t.Fatalf("stale invite from a previous owner must not authorize reads, got (%v, %v)", ok, err)
	}

	oldAdmin := types.Caller{TenantID: 100, UserID: "admin-old", Role: types.TenantRoleAdmin}
	if err := svc.Revoke(ctx, oldAdmin, "inv-old"); err == nil {
		t.Fatal("the previous owner's admin must not manage the KB's invites")
	}

	newAdmin := types.Caller{TenantID: 300, UserID: "admin-new", Role: types.TenantRoleAdmin}
	if err := svc.Revoke(ctx, newAdmin, "inv-old"); err != nil {
		t.Fatalf("current owner's admin must revoke a stale invite, got %v", err)
	}
	if got, _ := repo.GetByID(ctx, "inv-old"); got == nil || got.Status != types.KBInvitationStatusRevoked {
		t.Fatalf("stale invite must be revoked, got %+v", got)
	}
}

// guestMemberReader reports a non-active (suspended / invited) row for
// every lookup: the recipient is a Guest with no active membership.
type guestMemberReader struct{ status types.TenantMemberStatus }

func (g guestMemberReader) GetMembership(_ context.Context, userID string, tenantID uint64) (*types.TenantMember, error) {
	return &types.TenantMember{UserID: userID, TenantID: tenantID, Status: g.status}, nil
}

// Phase 2: a Guest (signed-in user with no active membership anywhere)
// gets no ACL. Issue refuses the recipient whoever issues it — a Tenant
// Admin or an explicit SuperAdmin — and a Guest cannot accept.
func TestKBInviteGuestGetsNoACL(t *testing.T) {
	ctx := context.Background()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant, Name: "KB"}}
	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
	superCtx := context.WithValue(ctx, types.SystemAdminContextKey, true)
	superAdmin := types.Caller{TenantID: 0, UserID: "root"}
	for _, status := range []types.TenantMemberStatus{
		types.TenantMemberStatusSuspended, types.TenantMemberStatusInvited,
	} {
		svc := NewKBInvitationService(newFakeKBInviteRepo(), kbRepo, guestMemberReader{status: status}, nil, nil)
		req := &types.CreateKBInviteRequest{RecipientUserID: "guest-1", RecipientTenantID: 200}
		if _, err := svc.Issue(ctx, admin, "kb-1", req); !errors.Is(err, ErrKBInviteRecipientNotMember) {
			t.Fatalf("status=%s: tenant admin inviting a Guest: want ErrKBInviteRecipientNotMember, got %v", status, err)
		}
		if _, err := svc.Issue(superCtx, superAdmin, "kb-1", req); !errors.Is(err, ErrKBInviteRecipientNotMember) {
			t.Fatalf("status=%s: SuperAdmin inviting a Guest: want ErrKBInviteRecipientNotMember, got %v", status, err)
		}
	}

	// An invite issued while the recipient was a member cannot be accepted
	// once they are a Guest (tenantless caller, no active membership).
	repo := newFakeKBInviteRepo()
	svc := NewKBInvitationService(repo, kbRepo, &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}, nil, nil)
	created, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-b", RecipientTenantID: 200,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	guestSvc := NewKBInvitationService(repo, kbRepo, guestMemberReader{status: types.TenantMemberStatusSuspended}, nil, nil)
	if _, err := guestSvc.Accept(ctx, types.Caller{UserID: "user-b"}, created.Token); err == nil {
		t.Fatal("a Guest must not accept a KB invite")
	}
}

// Phase 3: HasLiveInvite (the DecideKB lookup) checks the KB's current owner
// and the recipient's membership in recipient_tenant_id.
func TestKBInviteHasLiveInviteRequiresCurrentOwnerAndActiveMembership(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	repo.rows["inv-1"] = &types.KBInvitation{
		ID: "inv-1", KBID: "kb-1", OwnerTenantID: 100, RecipientUserID: "user-b",
		RecipientTenantID: 200, Status: types.KBInvitationStatusAccepted,
	}
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityRestricted}
	svc := NewKBInvitationService(repo, &fakeKBRepoForInvite{kb: kb}, nil, nil, nil)

	ok, err := svc.HasLiveInvite(ctx, "kb-1", "user-b")
	if err != nil || !ok {
		t.Fatalf("live invite with an active membership must count: ok=%v err=%v", ok, err)
	}
	repo.inactive = map[string]bool{"user-b": true}
	if ok, _ := svc.HasLiveInvite(ctx, "kb-1", "user-b"); ok {
		t.Fatal("a recipient whose membership is no longer active loses the invite's access")
	}
	repo.inactive = nil
	kb.OwnerTenantID = 300
	if ok, _ := svc.HasLiveInvite(ctx, "kb-1", "user-b"); ok {
		t.Fatal("an invite from a previous owner never counts")
	}
}

// Phase 3: a same-tenant invite is the per-user ACL of a restricted KB. The
// recipient still needs an active membership in recipient_tenant_id.
func TestKBInviteIssueSameTenant(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{
		ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityRestricted, Name: "KB",
	}}
	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}

	members := &fakeMemberSvcForInvite{active: map[string]bool{"colleague": true}}
	svc := NewKBInvitationService(repo, kbRepo, members, nil, nil)
	created, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "colleague", RecipientTenantID: 100,
	})
	if err != nil {
		t.Fatalf("same-tenant invite must be allowed: %v", err)
	}
	if created.RecipientTenantID != 100 || created.OwnerTenantID != 100 {
		t.Fatalf("unexpected invite %+v", created)
	}

	noMember := NewKBInvitationService(repo, kbRepo, &fakeMemberSvcForInvite{active: map[string]bool{}}, nil, nil)
	if _, err := noMember.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "guest", RecipientTenantID: 100,
	}); err == nil {
		t.Fatal("a recipient without an active membership (a Guest) gets no ACL")
	}
	member := types.Caller{TenantID: 100, UserID: "member-1", Role: types.TenantRoleMember}
	if _, err := svc.Issue(ctx, member, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "colleague", RecipientTenantID: 100,
	}); err == nil {
		t.Fatal("only the owner tenant's admin issues invites")
	}
}
