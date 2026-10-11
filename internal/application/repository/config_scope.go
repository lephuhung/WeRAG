package repository

// configScopeSQL matches rows owned by a tenant or by the configuration
// profile that tenant inherits from. It takes the column name and expects the
// tenant id twice as arguments: once for the tenant itself and once to look
// up its profile. A tenant without a profile (or a profile row itself, whose
// config_profile_id is NULL) matches its own rows only.
//
// Configuration rows — models, agents, sandbox configs, MCP services, web
// search providers — are written by the SuperAdmin into the profile, and every
// workspace reads them through this scope. Data rows (knowledge bases,
// sessions, documents) never use it.
func configScopeSQL(column string) string {
	return column + " IN (?, (SELECT config_profile_id FROM tenants WHERE id = ?))"
}

// configScopeArgs returns the arguments configScopeSQL expects.
func configScopeArgs(tenantID uint64) []any {
	return []any{tenantID, tenantID}
}

// configUsersSQL matches rows owned by a tenant or, when that tenant is a
// configuration profile, by any workspace that inherits from it. Usage guards
// use it: a model or agent in a profile is in use wherever a workspace on
// that profile references it. For a plain workspace the subquery is empty and
// the match is the tenant alone. Expects the tenant id twice.
func configUsersSQL(column string) string {
	return "(" + column + " = ? OR " + column + " IN (SELECT id FROM tenants WHERE config_profile_id = ?))"
}
