package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBGrantLookup is the KB-access lookup bundle: the visibility-scope read
// plus the approved tenant-to-tenant access grant check the two-scope model
// (tenant/public) needs. The kb access grant service implements all of it;
// a nil lookup disables scope and grant resolution alike (fail-closed).
type KBGrantLookup interface {
	// GetKBScope returns the scope of one KB; nil when it does not exist.
	GetKBScope(ctx context.Context, kbID string) (*types.KBScope, error)
	// ApprovedKBPermission returns the permission an approved, unexpired
	// grant gives granteeTenantID on kbID — false when no live grant exists.
	ApprovedKBPermission(ctx context.Context, kbID string, granteeTenantID uint64) (types.KBPermission, bool, error)
}

// KBGrantPermissions caches approved-grant lookups for one caller and
// operation so a fan-out search does not hit the store per KB.
type KBGrantPermissions struct {
	ctx      context.Context
	grants   KBGrantLookup
	tenantID uint64
	cache    map[string]grantResult
}

type grantResult struct {
	permission types.KBPermission
	ok         bool
}

// NewKBGrantPermissions resolves cross-tenant grants for tenantID.
// A nil grants lookup fails closed.
func NewKBGrantPermissions(ctx context.Context, grants KBGrantLookup, tenantID uint64) *KBGrantPermissions {
	return &KBGrantPermissions{
		ctx:      ctx,
		grants:   grants,
		tenantID: tenantID,
		cache:    make(map[string]grantResult),
	}
}

// Check reports whether the caller's tenant holds a live grant on kbID that
// satisfies the required permission level.
func (p *KBGrantPermissions) Check(kbID string, required types.KBPermission) (bool, error) {
	if p == nil || p.grants == nil || p.tenantID == 0 || kbID == "" {
		return false, nil
	}
	if r, ok := p.cache[kbID]; ok {
		return r.ok && r.permission.HasPermission(required), nil
	}
	permission, ok, err := p.grants.ApprovedKBPermission(p.ctx, kbID, p.tenantID)
	if err != nil {
		return false, err
	}
	p.cache[kbID] = grantResult{permission: permission, ok: ok}
	return ok && permission.HasPermission(required), nil
}
