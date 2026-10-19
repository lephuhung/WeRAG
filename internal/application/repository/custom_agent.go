package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// ErrCustomAgentNotFound is returned when a custom agent is not found
var ErrCustomAgentNotFound = errors.New("custom agent not found")

// customAgentRepository implements the CustomAgentRepository interface
type customAgentRepository struct {
	db *gorm.DB
}

// NewCustomAgentRepository creates a new custom agent repository
func NewCustomAgentRepository(db *gorm.DB) interfaces.CustomAgentRepository {
	return &customAgentRepository{db: db}
}

// CreateAgent creates a new custom agent
func (r *customAgentRepository) CreateAgent(ctx context.Context, agent *types.CustomAgent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

// GetAgentByID gets an agent by id for a tenant. The tenant's own row wins;
// otherwise the row of the configuration profile the tenant inherits from is
// returned, rebound to the tenant (see inheritAgent). A profile row whose
// "Tổ chức áp dụng" list leaves the tenant out is not found.
func (r *customAgentRepository) GetAgentByID(ctx context.Context, id string, tenantID uint64) (*types.CustomAgent, error) {
	return r.getAgentByID(ctx, id, tenantID, true)
}

// GetAgentByIDIgnoringTenantScope is GetAgentByID without the "Tổ chức áp
// dụng" filter, for a SuperAdmin who manages every agent.
func (r *customAgentRepository) GetAgentByIDIgnoringTenantScope(ctx context.Context, id string, tenantID uint64) (*types.CustomAgent, error) {
	return r.getAgentByID(ctx, id, tenantID, false)
}

func (r *customAgentRepository) getAgentByID(ctx context.Context, id string, tenantID uint64, scoped bool) (*types.CustomAgent, error) {
	var rows []*types.CustomAgent
	query := r.db.WithContext(ctx).
		Where("id = ?", id).
		Where(configScopeSQL("tenant_id"), configScopeArgs(tenantID)...)
	if scoped {
		query = whereAgentAppliesToTenant(query, tenantID)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	var picked *types.CustomAgent
	for _, row := range rows {
		if picked == nil || row.TenantID == tenantID {
			picked = row
		}
	}
	if picked == nil {
		return nil, ErrCustomAgentNotFound
	}
	return inheritAgent(picked, tenantID), nil
}

// ListAgentsByTenantID lists the agents a tenant sees: its own rows plus the
// rows of its configuration profile that it has not overridden and whose
// "Tổ chức áp dụng" list includes it (or is empty).
func (r *customAgentRepository) ListAgentsByTenantID(ctx context.Context, tenantID uint64) ([]*types.CustomAgent, error) {
	return r.listAgents(ctx, tenantID, true)
}

// ListAgentsByTenantIDIgnoringTenantScope is ListAgentsByTenantID without
// the "Tổ chức áp dụng" filter, for a SuperAdmin who manages every agent.
func (r *customAgentRepository) ListAgentsByTenantIDIgnoringTenantScope(ctx context.Context, tenantID uint64) ([]*types.CustomAgent, error) {
	return r.listAgents(ctx, tenantID, false)
}

func (r *customAgentRepository) listAgents(ctx context.Context, tenantID uint64, scoped bool) ([]*types.CustomAgent, error) {
	var rows []*types.CustomAgent
	query := r.db.WithContext(ctx).
		Where(configScopeSQL("tenant_id"), configScopeArgs(tenantID)...)
	if scoped {
		query = whereAgentAppliesToTenant(query, tenantID)
	}
	if err := query.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	own := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.TenantID == tenantID {
			own[row.ID] = true
		}
	}
	agents := make([]*types.CustomAgent, 0, len(rows))
	for _, row := range rows {
		if row.TenantID != tenantID && own[row.ID] {
			continue
		}
		agents = append(agents, inheritAgent(row, tenantID))
	}
	return agents, nil
}

// whereAgentAppliesToTenant keeps the rows a tenant may use: its own rows
// always (the list restricts inheritance, not ownership), and inherited rows
// whose applies_to_tenant_ids is empty or contains the tenant. Built-in rows
// are never scoped, but the API refuses a list on them, so the column is
// always [] there and needs no special case.
func whereAgentAppliesToTenant(query *gorm.DB, tenantID uint64) *gorm.DB {
	if query.Dialector.Name() == "postgres" {
		return query.Where(`(tenant_id = ? OR COALESCE(applies_to_tenant_ids, '[]'::jsonb) = '[]'::jsonb
			OR applies_to_tenant_ids @> jsonb_build_array(?::bigint))`, tenantID, tenantID)
	}
	// the SQLite test schema stores the list as JSON text
	return query.Where(`(tenant_id = ? OR json_array_length(COALESCE(applies_to_tenant_ids, '[]')) = 0
		OR EXISTS (SELECT 1 FROM json_each(applies_to_tenant_ids) AS s WHERE s.value = ?))`, tenantID, tenantID)
}

// InvalidScopeTenantIDs returns the ids in tenantIDs that cannot be in the
// "Tổ chức áp dụng" list of an agent owned by ownerTenantID: only live
// workspaces that inherit from that configuration profile qualify (any other
// tenant never sees the profile's agents, so listing it would be a no-op
// that reads like a grant).
func (r *customAgentRepository) InvalidScopeTenantIDs(
	ctx context.Context, ownerTenantID uint64, tenantIDs []uint64,
) ([]uint64, error) {
	if len(tenantIDs) == 0 {
		return nil, nil
	}
	var valid []uint64
	if err := r.db.WithContext(ctx).
		Table("tenants").
		Where("id IN ?", tenantIDs).
		Where("kind = ?", types.TenantKindWorkspace).
		Where("config_profile_id = ?", ownerTenantID).
		Where("deleted_at IS NULL").
		Pluck("id", &valid).Error; err != nil {
		return nil, err
	}
	ok := make(map[uint64]bool, len(valid))
	for _, id := range valid {
		ok[id] = true
	}
	var invalid []uint64
	for _, id := range tenantIDs {
		if !ok[id] {
			invalid = append(invalid, id)
		}
	}
	return invalid, nil
}

// inheritAgent rebinds an agent inherited from a configuration profile to the
// tenant using it. Downstream code reads agent.TenantID as the tenant the
// agent runs in — it picks the retrieval tenant from it and treats a mismatch
// with the session as a cross-tenant shared agent — so an inherited agent
// must look like the tenant's own.
func inheritAgent(agent *types.CustomAgent, tenantID uint64) *types.CustomAgent {
	if agent != nil && agent.TenantID != tenantID {
		agent.TenantID = tenantID
	}
	return agent
}

// UpdateAgent updates an agent
func (r *customAgentRepository) UpdateAgent(ctx context.Context, agent *types.CustomAgent) error {
	return r.db.WithContext(ctx).Save(agent).Error
}

// DeleteAgent deletes an agent (soft delete)
func (r *customAgentRepository) DeleteAgent(ctx context.Context, id string, tenantID uint64) error {
	return r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&types.CustomAgent{}).Error
}

// CountByModelID counts active agents whose config references modelID.
func (r *customAgentRepository) CountByModelID(
	ctx context.Context, tenantID uint64, modelID string,
) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).
		Model(&types.CustomAgent{}).
		Where(configUsersSQL("tenant_id"), configScopeArgs(tenantID)...)
	query = scopeCustomAgentsByModelID(query, modelID)
	err := query.Count(&count).Error
	return count, err
}

// ListModelUsages returns active agents that reference modelID. Only id, name,
// and config are loaded; the config is decoded locally and never returned.
func (r *customAgentRepository) ListModelUsages(
	ctx context.Context, tenantID uint64, modelID string,
) ([]types.ModelUsageResource, error) {
	rows := make([]*types.CustomAgent, 0)
	query := r.db.WithContext(ctx).
		Model(&types.CustomAgent{}).
		Select("id", "name", "config").
		Where(configUsersSQL("tenant_id"), configScopeArgs(tenantID)...)
	query = scopeCustomAgentsByModelID(query, modelID)
	if err := query.Order("name ASC, id ASC").Limit(types.ModelUsageListLimit).Find(&rows).Error; err != nil {
		return nil, err
	}

	usages := make([]types.ModelUsageResource, 0, len(rows))
	for _, row := range rows {
		bindings := customAgentModelUsageBindings(row, modelID)
		if len(bindings) == 0 {
			continue
		}
		usages = append(usages, types.ModelUsageResource{
			ID:       row.ID,
			Name:     row.Name,
			Bindings: bindings,
		})
	}
	return usages, nil
}

// CountBySandboxConfigID counts agents pointing at a sandbox config.
//
// Used only to warn the admin which agents reference a config; never use it to
// refuse operations. Agent references are permanent state, so blocking on them
// would make credential rotation impossible.
func (r *customAgentRepository) CountBySandboxConfigID(
	ctx context.Context, tenantID uint64, configID string,
) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).
		Model(&types.CustomAgent{}).
		Where(configUsersSQL("tenant_id"), configScopeArgs(tenantID)...)
	query = scopeCustomAgentsBySandboxConfigID(query, configID)
	err := query.Count(&count).Error
	return count, err
}

// ListNamesBySandboxConfigID returns agent names pointing at a sandbox config.
//
// Used only to warn the admin which agents reference a config; never use it to
// refuse operations. Agent references are permanent state, so blocking on them
// would make credential rotation impossible.
func (r *customAgentRepository) ListNamesBySandboxConfigID(
	ctx context.Context, tenantID uint64, configID string,
) ([]string, error) {
	var names []string
	query := r.db.WithContext(ctx).
		Model(&types.CustomAgent{}).
		Where(configUsersSQL("tenant_id"), configScopeArgs(tenantID)...)
	query = scopeCustomAgentsBySandboxConfigID(query, configID)
	err := query.Order("name ASC").Pluck("name", &names).Error
	return names, err
}
