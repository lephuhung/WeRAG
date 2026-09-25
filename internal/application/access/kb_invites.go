package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBInviteLookup resolves recipient-bound KB read invitations. The invite
// service implements it; a nil lookup fails closed (no invite access).
type KBInviteLookup interface {
	// HasAcceptedInvite reports whether userID holds a live (accepted,
	// unexpired) invite on kbID.
	HasAcceptedInvite(ctx context.Context, kbID, userID string) (bool, error)
}

// ResolveKBWithInvite extends ResolveKB with recipient-bound invitation
// access: when the caller's tenant has no grant, a live invite accepted
// by THIS user (not just any member of their tenant) confers read-only
// access. ResolveKB itself is untouched so existing callers keep their
// behavior; new cross-tenant read paths should prefer this function.
//
// Rules:
//   - Same-tenant access is identical to ResolveKB; public visibility has
//     no cross-tenant effect.
//   - Legacy tenant-level grants are ignored and never authorize access.
//   - Invite access requires: required == Viewer (read-only), a live
//     accepted invite bound to request.Caller.UserID on this KB, and no
//     lookup error. Any invite error fails closed.
//   - Invite access never satisfies Editor/Admin (no write upgrade).
func ResolveKBWithInvite(
	ctx context.Context,
	request KBRequest,
	kb *types.KnowledgeBase,
	required types.KBPermission,
	grants KBGrantLookup,
	invites KBInviteLookup,
) (*KBAccess, error) {
	access, err := ResolveKB(ctx, request, kb, required, grants)
	if err == nil {
		return access, nil
	}
	// Only a forbidden cross-tenant read may fall through to invites;
	// never upgrade not-found/unauthorized into invite probing, and
	// never satisfy a write with a read-only invite.
	if err != ErrForbidden || required != types.KBPermissionViewer {
		return nil, err
	}
	if kb == nil || kb.ID == "" {
		return nil, ErrNotFound
	}
	if invites == nil {
		return nil, ErrForbidden
	}
	caller := request.Caller.Normalize()
	if caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) {
		return nil, ErrForbidden
	}
	// API-key principals never hold recipient-bound invites: a platform
	// key's api_platform:<id> identity (or any scoped key) must not reach
	// the invite lookup even though it is non-synthetic.
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return nil, ErrForbidden
	}
	// The invite must belong to the KB-owning tenant's KB and the
	// recipient's own user ID — HasAcceptedInvite is keyed by
	// (kbID, userID), never by tenant, so a colleague in the same
	// tenant cannot ride on another member's invite.
	ok, lookupErr := invites.HasAcceptedInvite(ctx, kb.ID, caller.UserID)
	if lookupErr != nil || !ok {
		return nil, ErrForbidden
	}
	return &KBAccess{
		KnowledgeBase: kb, Caller: caller,
		EffectiveTenantID: kb.TenantID, Permission: types.KBPermissionViewer,
		operationPermission: required,
	}, nil
}
