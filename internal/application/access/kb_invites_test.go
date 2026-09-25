package access

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type stubGrants struct {
	perm map[string]types.KBPermission
	err  error
}

func (s *stubGrants) GetKBScope(_ context.Context, _ string) (*types.KBScope, error) {
	return nil, nil
}

func (s *stubGrants) ApprovedKBPermission(_ context.Context, kbID string, _ uint64) (types.KBPermission, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	p, ok := s.perm[kbID]
	return p, ok, nil
}

type stubInvites struct {
	accepted map[string]bool // key: kbID + "\x00" + userID
	err      error
}

func (s *stubInvites) HasAcceptedInvite(_ context.Context, kbID, userID string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.accepted[kbID+"\x00"+userID], nil
}

func testKB(id string, tenant uint64) *types.KnowledgeBase {
	return &types.KnowledgeBase{
		ID: id, TenantID: tenant,
		OwnerTenantID: tenant, Visibility: types.KBVisibilityTenant,
	}
}

func TestResolveKBWithInviteRecipientBound(t *testing.T) {
	ctx := context.Background()
	kb := testKB("kb-1", 100)
	grants := &stubGrants{}
	invites := &stubInvites{accepted: map[string]bool{"kb-1\x00user-b": true}}

	recipient := KBRequest{Caller: types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}}
	got, err := ResolveKBWithInvite(ctx, recipient, kb, types.KBPermissionViewer, grants, invites)
	if err != nil {
		t.Fatalf("recipient with accepted invite should read: %v", err)
	}
	if got.Permission != types.KBPermissionViewer {
		t.Errorf("invite access must be viewer, got %q", got.Permission)
	}

	// Another member of the SAME tenant must NOT ride on user-b's invite.
	other := KBRequest{Caller: types.Caller{TenantID: 200, UserID: "user-c", Role: types.TenantRoleMember}}
	if _, err := ResolveKBWithInvite(ctx, other, kb, types.KBPermissionViewer, grants, invites); !errors.Is(err, ErrForbidden) {
		t.Errorf("uninvited tenant-mate must be forbidden, got %v", err)
	}

	// Non-invited foreign member with no invite at all.
	stranger := KBRequest{Caller: types.Caller{TenantID: 300, UserID: "user-z", Role: types.TenantRoleMember}}
	if _, err := ResolveKBWithInvite(ctx, stranger, kb, types.KBPermissionViewer, grants, invites); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger must be forbidden, got %v", err)
	}
}

func TestResolveKBWithInviteReadOnly(t *testing.T) {
	ctx := context.Background()
	kb := testKB("kb-1", 100)
	grants := &stubGrants{}
	invites := &stubInvites{accepted: map[string]bool{"kb-1\x00user-b": true}}
	recipient := KBRequest{Caller: types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}}

	if _, err := ResolveKBWithInvite(ctx, recipient, kb, types.KBPermissionEditor, grants, invites); !errors.Is(err, ErrForbidden) {
		t.Errorf("invite must NOT satisfy editor/write, got %v", err)
	}
}

func TestResolveKBWithInviteLookupErrorFailsClosed(t *testing.T) {
	ctx := context.Background()
	kb := testKB("kb-1", 100)
	grants := &stubGrants{}
	invites := &stubInvites{err: errors.New("db down")}
	recipient := KBRequest{Caller: types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}}
	if _, err := ResolveKBWithInvite(ctx, recipient, kb, types.KBPermissionViewer, grants, invites); !errors.Is(err, ErrForbidden) {
		t.Errorf("lookup error must fail closed, got %v", err)
	}
}

func TestResolveKBWithInviteNilLookupFailsClosed(t *testing.T) {
	ctx := context.Background()
	kb := testKB("kb-1", 100)
	grants := &stubGrants{}
	recipient := KBRequest{Caller: types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}}
	if _, err := ResolveKBWithInvite(ctx, recipient, kb, types.KBPermissionViewer, grants, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("nil invite lookup must fail closed, got %v", err)
	}
}

func TestResolveKBWithInviteLegacyTenantGrantFailsClosed(t *testing.T) {
	ctx := context.Background()
	kb := testKB("kb-1", 100)
	grants := &stubGrants{perm: map[string]types.KBPermission{"kb-1": types.KBPermissionViewer}}
	invites := &stubInvites{}
	recipient := KBRequest{Caller: types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}}
	if _, err := ResolveKBWithInvite(ctx, recipient, kb, types.KBPermissionViewer, grants, invites); !errors.Is(err, ErrForbidden) {
		t.Errorf("legacy tenant grant must not authorize cross-tenant read, got %v", err)
	}
}

func TestInviteGrantNeverAuthorizesWrite(t *testing.T) {
	ctx := context.Background()
	kb := testKB("kb-1", 100)
	grants := &stubGrants{}
	invites := &stubInvites{accepted: map[string]bool{"kb-1\x00user-b": true}}
	recipient := KBRequest{Caller: types.Caller{TenantID: 200, UserID: "user-b", Role: types.TenantRoleMember}}

	// The invite resolves a read grant...
	got, err := ResolveKBWithInvite(ctx, recipient, kb, types.KBPermissionViewer, grants, invites)
	if err != nil {
		t.Fatalf("recipient read must resolve: %v", err)
	}
	// ...but that grant never satisfies a write: RequireKBWrite demands
	// an Editor operation grant, and invite resolution caps at Viewer.
	if err := RequireKBWrite(got.WithGrant(ctx), kb); !errors.Is(err, ErrForbidden) {
		t.Errorf("invited foreign member must be denied write, got %v", err)
	}
	// Same for an Editor requirement at resolve time.
	if _, err := ResolveKBWithInvite(ctx, recipient, kb, types.KBPermissionEditor, grants, invites); !errors.Is(err, ErrForbidden) {
		t.Errorf("invite must not satisfy editor, got %v", err)
	}
}
