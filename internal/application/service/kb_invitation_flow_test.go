package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// fakeInviteUserRepo resolves users by ID or email from an in-memory map.
type fakeInviteUserRepo struct {
	byID    map[string]*types.User
	byEmail map[string]*types.User
}

func (f *fakeInviteUserRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (f *fakeInviteUserRepo) GetUserByEmail(_ context.Context, email string) (*types.User, error) {
	if u, ok := f.byEmail[email]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

// fakeInviteAudit captures audit entries for lifecycle assertions.
type fakeInviteAudit struct {
	entries []*types.AuditLog
}

func (f *fakeInviteAudit) Log(_ context.Context, entry *types.AuditLog) error {
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeInviteAudit) actions() []types.AuditAction {
	out := make([]types.AuditAction, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e.Action)
	}
	return out
}

func TestKBInviteIssueByEmail(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant, Name: "KB"}}
	members := &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}
	users := &fakeInviteUserRepo{
		byID:    map[string]*types.User{"user-b": {ID: "user-b", Email: "b@x.com"}},
		byEmail: map[string]*types.User{"b@x.com": {ID: "user-b", Email: "b@x.com"}},
	}
	audit := &fakeInviteAudit{}
	svc := NewKBInvitationService(repo, kbRepo, members, users, audit)

	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
	created, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientEmail: "b@x.com", RecipientTenantID: 200,
	})
	if err != nil {
		t.Fatalf("Issue by email: %v", err)
	}
	if created.RecipientUserID != "user-b" {
		t.Errorf("recipient = %q, want user-b", created.RecipientUserID)
	}
	if len(audit.entries) != 1 || audit.entries[0].Action != types.AuditActionKBInviteIssued {
		t.Errorf("expected one kb.invite_issued audit row, got %v", audit.actions())
	}

	// Unknown email fails closed.
	if _, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientEmail: "ghost@x.com", RecipientTenantID: 200,
	}); err == nil {
		t.Fatal("unknown email must fail")
	}
	// Conflicting user ID + email fail closed.
	if _, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-other", RecipientEmail: "b@x.com", RecipientTenantID: 200,
	}); err == nil {
		t.Fatal("mismatched id+email must fail")
	}
}

func TestKBInviteAcceptByID(t *testing.T) {
	ctx := context.Background()
	repo := newFakeKBInviteRepo()
	kbRepo := &fakeKBRepoForInvite{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 100, OwnerTenantID: 100, Visibility: types.KBVisibilityTenant, Name: "KB"}}
	members := &fakeMemberSvcForInvite{active: map[string]bool{"user-b": true}}
	audit := &fakeInviteAudit{}
	svc := NewKBInvitationService(repo, kbRepo, members, nil, audit)

	admin := types.Caller{TenantID: 100, UserID: "admin-1", Role: types.TenantRoleAdmin}
	created, err := svc.Issue(ctx, admin, "kb-1", &types.CreateKBInviteRequest{
		RecipientUserID: "user-b", RecipientTenantID: 200,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	recipient := types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}
	accepted, err := svc.AcceptByID(ctx, recipient, created.ID)
	if err != nil {
		t.Fatalf("AcceptByID: %v", err)
	}
	if accepted.Status != types.KBInvitationStatusAccepted {
		t.Errorf("status = %q, want accepted", accepted.Status)
	}
	found := false
	for _, a := range audit.actions() {
		if a == types.AuditActionKBInviteAccepted {
			found = true
		}
	}
	if !found {
		t.Errorf("expected kb.invite_accepted audit row, got %v", audit.actions())
	}

	// Wrong user cannot accept by ID (non-enumerating forbidden, not leak).
	wrong := types.Caller{TenantID: 200, UserID: "user-c", Role: types.TenantRoleMember}
	if _, err := svc.AcceptByID(ctx, wrong, created.ID); err == nil {
		t.Fatal("wrong user must NOT accept by ID")
	}
}

func TestKBInviteInvitedKBIDs(t *testing.T) {
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
	ids, err := svc.InvitedKBIDs(ctx, "user-b")
	if err != nil || len(ids) != 0 {
		t.Fatalf("pending invite must not appear in discovery, got %v %v", ids, err)
	}
	recipient := types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}
	if _, err := svc.AcceptByID(ctx, recipient, created.ID); err != nil {
		t.Fatalf("AcceptByID: %v", err)
	}
	ids, err = svc.InvitedKBIDs(ctx, "user-b")
	if err != nil || len(ids) != 1 || ids[0] != "kb-1" {
		t.Fatalf("accepted invite must appear in discovery, got %v %v", ids, err)
	}
	if err := svc.Revoke(ctx, admin, created.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	ids, err = svc.InvitedKBIDs(ctx, "user-b")
	if err != nil || len(ids) != 0 {
		t.Fatalf("revoked invite must vanish from discovery, got %v %v", ids, err)
	}
}

func TestAppendInvitedKBsDedupes(t *testing.T) {
	own := []*types.KnowledgeBase{{ID: "kb-1"}, {ID: "kb-2"}}
	invited := []*types.KnowledgeBase{{ID: "kb-2"}, nil, {ID: ""}, {ID: "kb-3"}}
	got := appendInvitedKBs(own, invited)
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3 (kb-1, kb-2, kb-3)", len(got))
	}
	ids := map[string]bool{}
	for _, kb := range got {
		ids[kb.ID] = true
	}
	if !ids["kb-1"] || !ids["kb-2"] || !ids["kb-3"] {
		t.Fatalf("unexpected merge result: %v", ids)
	}
}
