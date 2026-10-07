package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBGrantLookup resolves KB scopes and owner-issued tenant-wide grants. A
// live grant confers Viewer on one tenant-owned KB to every human member of
// the grantee tenant (see HasTenantGrant); it never confers writes.
type KBGrantLookup interface {
	// GetKBScope returns the scope of one KB; nil when it does not exist.
	GetKBScope(ctx context.Context, kbID string) (*types.KBScope, error)
	// ApprovedKBPermission returns the permission an approved, unexpired
	// grant gives granteeTenantID on kbID — false when no live grant exists.
	ApprovedKBPermission(ctx context.Context, kbID string, granteeTenantID uint64) (types.KBPermission, bool, error)
}

// KBGrantPermissions is a compatibility wrapper for the retired tenant-wide
// grant lookup. Its Check method always denies; cross-tenant human reads must
// be established as recipient-bound exact context grants.
type KBGrantPermissions struct{}

// NewKBGrantPermissions constructs a retired-grant checker. The parameters
// are retained for compatibility but no database lookup authorizes access.
func NewKBGrantPermissions(_ context.Context, _ KBGrantLookup, _ uint64) *KBGrantPermissions {
	return &KBGrantPermissions{}
}

// Check always denies: tenant-wide grants are retired and can never
// authorize cross-tenant reads or writes, even if an approved legacy row
// remains in storage.
func (p *KBGrantPermissions) Check(_ string, _ types.KBPermission) (bool, error) {
	return false, nil
}
