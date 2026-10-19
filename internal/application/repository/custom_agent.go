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
// returned, rebound to the tenant (see inheritAgent). A profile row the
// tenant may not use (see whereAgentVisibleTo) is not found. It answers for
// the Tổ chức alone: add-ons on a person's own subscription need
// GetVisibleAgentByID with the person in the viewer.
func (r *customAgentRepository) GetAgentByID(ctx context.Context, id string, tenantID uint64) (*types.CustomAgent, error) {
	return r.getAgentByID(ctx, id, tenantID, &types.AgentViewer{TenantID: tenantID})
}

// GetVisibleAgentByID is GetAgentByID for a viewer: the tenant plus, for a
// human caller, the add-ons of their personal subscription.
func (r *customAgentRepository) GetVisibleAgentByID(ctx context.Context, id string, viewer types.AgentViewer) (*types.CustomAgent, error) {
	return r.getAgentByID(ctx, id, viewer.TenantID, &viewer)
}

// GetAgentByIDIgnoringTenantScope is GetAgentByID without the visibility
// filter ("Cách phân phối", "Tổ chức áp dụng"), for a SuperAdmin who manages
// every agent.
func (r *customAgentRepository) GetAgentByIDIgnoringTenantScope(ctx context.Context, id string, tenantID uint64) (*types.CustomAgent, error) {
	return r.getAgentByID(ctx, id, tenantID, nil)
}

// getAgentByID reads one agent; a nil viewer skips the visibility filter.
func (r *customAgentRepository) getAgentByID(ctx context.Context, id string, tenantID uint64, viewer *types.AgentViewer) (*types.CustomAgent, error) {
	var rows []*types.CustomAgent
	query := r.db.WithContext(ctx).
		Where("id = ?", id).
		Where(configScopeSQL("tenant_id"), configScopeArgs(tenantID)...)
	if viewer != nil {
		query = whereAgentVisibleTo(query, *viewer)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	var picked *types.CustomAgent
	var profileSwitch *bool
	for _, row := range rows {
		if row.TenantID != tenantID && row.Switch != nil {
			profileSwitch = row.Switch
		}
		if picked == nil || row.TenantID == tenantID {
			picked = row
		}
	}
	if picked == nil {
		return nil, ErrCustomAgentNotFound
	}
	applyProfileSwitch(picked, tenantID, profileSwitch)
	return inheritAgent(picked, tenantID), nil
}

// ListAgentsByTenantID lists the agents a tenant sees: its own rows plus the
// rows of its configuration profile that it has not overridden and that it
// may use (see whereAgentVisibleTo), for the Tổ chức alone.
func (r *customAgentRepository) ListAgentsByTenantID(ctx context.Context, tenantID uint64) ([]*types.CustomAgent, error) {
	return r.listAgents(ctx, tenantID, &types.AgentViewer{TenantID: tenantID})
}

// ListVisibleAgents is ListAgentsByTenantID for a viewer (see
// GetVisibleAgentByID).
func (r *customAgentRepository) ListVisibleAgents(ctx context.Context, viewer types.AgentViewer) ([]*types.CustomAgent, error) {
	return r.listAgents(ctx, viewer.TenantID, &viewer)
}

// ListAgentsByTenantIDIgnoringTenantScope is ListAgentsByTenantID without
// the visibility filter, for a SuperAdmin who manages every agent.
func (r *customAgentRepository) ListAgentsByTenantIDIgnoringTenantScope(ctx context.Context, tenantID uint64) ([]*types.CustomAgent, error) {
	return r.listAgents(ctx, tenantID, nil)
}

func (r *customAgentRepository) listAgents(ctx context.Context, tenantID uint64, viewer *types.AgentViewer) ([]*types.CustomAgent, error) {
	var rows []*types.CustomAgent
	query := r.db.WithContext(ctx).
		Where(configScopeSQL("tenant_id"), configScopeArgs(tenantID)...)
	if viewer != nil {
		query = whereAgentVisibleTo(query, *viewer)
	}
	if err := query.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	own := make(map[string]bool, len(rows))
	profileSwitch := map[string]*bool{}
	for _, row := range rows {
		if row.TenantID == tenantID {
			own[row.ID] = true
		} else if row.Switch != nil {
			profileSwitch[row.ID] = row.Switch
		}
	}
	agents := make([]*types.CustomAgent, 0, len(rows))
	for _, row := range rows {
		if row.TenantID != tenantID && own[row.ID] {
			continue
		}
		applyProfileSwitch(row, tenantID, profileSwitch[row.ID])
		agents = append(agents, inheritAgent(row, tenantID))
	}
	return agents, nil
}

// applyProfileSwitch gives a tenant's own row (a built-in it customized
// before configuration moved to the profile) the on/off switch the
// SuperAdmin set on the profile's row: the switch is a platform decision.
func applyProfileSwitch(row *types.CustomAgent, tenantID uint64, profileSwitch *bool) {
	if row != nil && row.TenantID == tenantID && profileSwitch != nil {
		v := *profileSwitch
		row.Switch = &v
	}
}

// whereAgentVisibleTo keeps the rows a viewer may use
// (docs/agent-addon-plans.md §6), in one SQL condition so the agent list and
// every chat turn cost one query:
//
//   - the tenant's own rows, always (visibility restricts inheritance, not
//     ownership);
//   - distribution "basic": everyone;
//   - "internal": applies_to_tenant_ids empty or containing the tenant;
//   - "addon": a current usage_subscription_addons row for the agent on a
//     current subscription of the tenant or of the viewer's own person;
//   - and, for a custom agent, not switched off (enabled is not false).
//
// Built-in rows are always basic (the API refuses anything else).
func whereAgentVisibleTo(query *gorm.DB, viewer types.AgentViewer) *gorm.DB {
	tenantID := viewer.TenantID
	var internal string
	if query.Dialector.Name() == "postgres" {
		internal = `(COALESCE(applies_to_tenant_ids, '[]'::jsonb) = '[]'::jsonb
			OR applies_to_tenant_ids @> jsonb_build_array(?::bigint))`
	} else {
		// the SQLite test schema stores the list as JSON text
		internal = `(json_array_length(COALESCE(applies_to_tenant_ids, '[]')) = 0
			OR EXISTS (SELECT 1 FROM json_each(applies_to_tenant_ids) AS s WHERE s.value = ?))`
	}
	args := []interface{}{tenantID, types.AgentDistributionBasic, types.AgentDistributionInternal, tenantID}
	addon, addonArgs := addonVisibleSQL(viewer)
	args = append(args, types.AgentDistributionAddon)
	args = append(args, addonArgs...)
	// a custom agent the SuperAdmin switched off is hidden from everyone
	// else (its sessions get 2104); a switched-off built-in stays
	// resolvable by id and is only left out of the pickers (ListAgents)
	return query.Where(`(tenant_id = ? OR distribution = ?
		OR (distribution = ? AND `+internal+`)
		OR (distribution = ? AND `+addon+`))`, args...).
		Where("(is_builtin = ? OR COALESCE(enabled, ?) = ?)", true, true, true)
}

// addonVisibleSQL is the "bought" half of whereAgentVisibleTo: an EXISTS on
// usage_subscription_addons (index on agent_id) joined to the subscription
// by primary key, both current at the viewer's instant. Correlated on
// custom_agents.id, so the outer query must read custom_agents unaliased.
func addonVisibleSQL(viewer types.AgentViewer) (string, []interface{}) {
	if viewer.AllAddons {
		return "1 = 1", nil
	}
	now := viewer.At()
	scope := "(us.scope_type = ? AND us.scope_id = ?)"
	args := []interface{}{now, now, types.UsageSubscriptionActive, now, now,
		types.UsageScopeTenant, types.UsageTenantScopeID(viewer.TenantID)}
	if viewer.UserID != "" && !types.IsSyntheticUserID(viewer.UserID) {
		scope = "(" + scope + " OR (us.scope_type = ? AND us.scope_id = ?))"
		args = append(args, types.UsageScopeUser, viewer.UserID)
	}
	return `EXISTS (SELECT 1 FROM usage_subscription_addons usa
			JOIN usage_subscriptions us ON us.id = usa.subscription_id
			WHERE usa.agent_id = custom_agents.id
			AND usa.starts_at <= ? AND (usa.ends_at IS NULL OR usa.ends_at > ?)
			AND us.status = ? AND us.starts_at <= ? AND (us.ends_at IS NULL OR us.ends_at > ?)
			AND ` + scope + `)`, args
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
