package access

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

var (
	// ErrResourceNotFound means the ownership lookup found no resource visible
	// in the caller's tenant. HTTP adapters decide how to produce the 404.
	ErrResourceNotFound = errors.New("rbac: resource not found")
	// ErrOwnershipForbidden indicates neither ownership nor the required role.
	ErrOwnershipForbidden = errors.New("rbac: ownership or role insufficient")
)

// OwnershipRequest carries server-resolved identity and rollout policy. The
// cross-tenant bypass must already account for the cluster configuration.
// This gate does not replace API-key capabilities or resource-scope checks.
type OwnershipRequest struct {
	UserID               string
	Role                 types.TenantRole
	APIKey               bool
	CrossTenantSuperuser bool
	Enforce              bool
}

// OwnershipDecision contains the details adapters need for logs and audits.
// The error returned with it determines whether access was allowed.
type OwnershipDecision struct {
	CreatorID          string
	EnforcementSkipped bool
	LookupFailed       bool
}

// CheckOwnershipOrRole performs no resource lookup when API-key authorization,
// role, superuser status, or the RBAC rollout policy already settles the gate.
// The lookup must enforce tenant visibility before returning a creator ID.
func CheckOwnershipOrRole(
	request OwnershipRequest,
	required types.TenantRole,
	lookup func() (string, error),
) (OwnershipDecision, error) {
	var decision OwnershipDecision
	if request.APIKey || request.Role.HasPermission(required) || request.CrossTenantSuperuser {
		return decision, nil
	}
	if !request.Enforce {
		decision.EnforcementSkipped = true
		return decision, nil
	}
	if lookup == nil {
		decision.LookupFailed = true
		return decision, errors.New("rbac: creator lookup unavailable")
	}
	creator, err := lookup()
	decision.CreatorID = creator
	if errors.Is(err, ErrResourceNotFound) {
		return decision, ErrResourceNotFound
	}
	if err != nil {
		decision.LookupFailed = true
		return decision, err
	}
	if creator != "" && creator == request.UserID {
		return decision, nil
	}
	return decision, ErrOwnershipForbidden
}

// CanManageKBSharing reports whether caller may share a KB owned by
// ownerTenantID with other tenants or users: a Tenant Admin of the owning
// tenant, or an explicit human SuperAdmin for any KB (including
// platform-owned rows). API-key principals never qualify.
func CanManageKBSharing(ctx context.Context, caller types.Caller, ownerTenantID uint64) bool {
	caller = caller.Normalize()
	if IsExplicitHumanSuperAdmin(ctx, caller) {
		return true
	}
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return false
	}
	return ownerTenantID != 0 && caller.TenantID == ownerTenantID &&
		caller.Role.IsTenantAdmin() && IsHumanCaller(caller)
}
