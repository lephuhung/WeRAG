package types

import "context"

// Caller is the authenticated identity used for resource authorization. Its
// tenant and role do not change when queries execute in a resource's tenant.
// Machine capabilities remain in TenantAPIKeyScope; Principal is separate.
type Caller struct {
	TenantID uint64
	UserID   string
	Role     TenantRole
}

// Normalize maps an empty or invalid tenant role to "" (no tenant role).
// It never invents a role: a caller without an active membership (a Guest,
// or a SuperAdmin visiting a tenant) stays role-less, and "" satisfies no
// role check (TenantRole("").HasPermission(TenantRoleMember) is false).
func (caller Caller) Normalize() Caller {
	if !caller.Role.IsValid() {
		caller.Role = ""
	}
	return caller
}

// HasTenantRole reports whether the caller holds a tenant role (an active
// membership) in its current tenant.
func (caller Caller) HasTenantRole() bool {
	return caller.Role.IsValid()
}

// WithCaller captures the authenticated identity independently of execution scope.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, CallerContextKey, caller.Normalize())
}

// CallerFromContext supports legacy, unscoped service contexts. Always capture
// the caller before changing execution tenants, including a missing identity.
func CallerFromContext(ctx context.Context) Caller {
	if caller, ok := ctx.Value(CallerContextKey).(Caller); ok {
		return caller
	}
	tenantID, _ := TenantIDFromContext(ctx)
	userID, _ := UserIDFromContext(ctx)
	return Caller{TenantID: tenantID, UserID: userID, Role: TenantRoleFromContext(ctx)}
}

// WithExecutionTenant changes repository/model scope, never authorization.
// It does not grant access to any resources in the destination tenant.
func WithExecutionTenant(ctx context.Context, tenantID uint64) context.Context {
	ctx = WithCaller(ctx, CallerFromContext(ctx))
	return context.WithValue(ctx, TenantIDContextKey, tenantID)
}
