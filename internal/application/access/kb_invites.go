package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBInviteLookup resolves recipient-bound KB read invitations. The invite
// service implements it; a nil lookup fails closed (no invite access).
type KBInviteLookup interface {
	// HasAcceptedInvite reports whether userID holds a live (accepted,
	// unexpired) invite on kbID issued by the KB's current owner tenant;
	// invites from a previous owner never count.
	HasAcceptedInvite(ctx context.Context, kbID, userID string) (bool, error)
	// HasLiveInvite is HasAcceptedInvite plus the recipient's membership:
	// the invite's recipient_tenant_id membership must still be active
	// (a suspended or removed member loses the invite's access). This is
	// the check DecideKB uses.
	HasLiveInvite(ctx context.Context, kbID, userID string) (bool, error)
}

// ResolveKBWithInvite is ResolveKB plus recipient-bound invitations as
// an ACL source (DecideKB with Grants and Invites). An invite confers
// Viewer only, never download or writes, and only when it was issued by
// the KB's current owner, is unexpired, and the recipient's membership in
// recipient_tenant_id is still active. API-key principals never reach the
// invite lookup.
func ResolveKBWithInvite(
	ctx context.Context,
	request KBRequest,
	kb *types.KnowledgeBase,
	required types.KBPermission,
	grants KBGrantLookup,
	invites KBInviteLookup,
) (*KBAccess, error) {
	return resolveKBAction(ctx, request, kb, ActionForPermission(required), required,
		KBLookups{Grants: grants, Invites: invites})
}
