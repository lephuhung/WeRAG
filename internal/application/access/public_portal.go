package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// IsPublicPortalGuest reports a Guest chatting in the public portal: an
// authenticated human (never an API key or synthetic identity) with no
// tenant role whose request runs in the public portal tenant. Auth attaches
// that context only on the guest chat routes (middleware.IsGuestChatAPI);
// the GuestChat route guard admits it there and nowhere else.
func IsPublicPortalGuest(ctx context.Context, caller types.Caller) bool {
	tenant, ok := types.TenantInfoFromContext(ctx)
	if !ok || !tenant.IsPublicPortal() {
		return false
	}
	caller = caller.Normalize()
	if caller.TenantID != tenant.ID || caller.HasTenantRole() {
		return false
	}
	return IsAuthenticatedHuman(ctx, caller)
}
