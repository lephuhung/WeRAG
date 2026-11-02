package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

// Catalogue of specialist agents sold with service plans
// (usage_agent_addons, migration 000167). Methods of usagePlanRepository.

func (r *usagePlanRepository) ListAgentAddons(ctx context.Context, includeInactive bool) ([]*types.UsageAgentAddon, error) {
	rows := []*types.UsageAgentAddon{}
	q := r.conn(ctx).Order("sort_order ASC, agent_id ASC")
	if !includeInactive {
		q = q.Where("is_active = ?", true)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if err := fillAgentAddons(r.conn(ctx), rows, time.Now()); err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *usagePlanRepository) GetAgentAddon(ctx context.Context, agentID string) (*types.UsageAgentAddon, error) {
	var row types.UsageAgentAddon
	if err := r.conn(ctx).Where("agent_id = ?", agentID).First(&row).Error; err != nil {
		return nil, err
	}
	if err := fillAgentAddons(r.conn(ctx), []*types.UsageAgentAddon{&row}, time.Now()); err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *usagePlanRepository) CreateAgentAddon(ctx context.Context, row *types.UsageAgentAddon) error {
	now := time.Now()
	row.CreatedAt, row.UpdatedAt = now, now
	return r.conn(ctx).Create(row).Error
}

func (r *usagePlanRepository) UpdateAgentAddon(ctx context.Context, row *types.UsageAgentAddon) error {
	row.UpdatedAt = time.Now()
	return r.conn(ctx).Model(row).Select("price_vnd", "plan_ids", "description", "is_active", "sort_order", "updated_at").
		Updates(row).Error
}

func (r *usagePlanRepository) DeleteAgentAddon(ctx context.Context, agentID string) (bool, error) {
	res := r.conn(ctx).Where("agent_id = ?", agentID).Delete(&types.UsageAgentAddon{})
	return res.RowsAffected > 0, res.Error
}

// DeactivateUnsellableAgentAddon switches the catalogue row of agentID off
// when no live custom agent with that id is an "addon" agent any more
// (deleted, or converted to basic/internal). Idempotent; called after every
// agent update or delete. Converting back to addon does not re-activate it:
// the SuperAdmin decides when it is sold again.
func (r *usagePlanRepository) DeactivateUnsellableAgentAddon(ctx context.Context, agentID string) error {
	return r.conn(ctx).Model(&types.UsageAgentAddon{}).
		Where("agent_id = ? AND is_active = ?", agentID, true).
		Where(`NOT EXISTS (SELECT 1 FROM custom_agents ca WHERE ca.id = usage_agent_addons.agent_id
			AND ca.deleted_at IS NULL AND ca.is_builtin = ? AND ca.distribution = ?)`,
			false, types.AgentDistributionAddon).
		Updates(map[string]interface{}{"is_active": false, "updated_at": time.Now()}).Error
}

// fillAgentAddons sets the agent name, availability and holder count of
// catalogue rows (two grouped queries, not one per row).
func fillAgentAddons(db *gorm.DB, rows []*types.UsageAgentAddon, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.AgentID)
	}
	var agents []struct {
		ID           string
		Name         string
		Distribution string
		Enabled      *bool
	}
	if err := db.Table("custom_agents").Select("id, name, distribution, enabled").
		Where("id IN ? AND deleted_at IS NULL AND is_builtin = ?", ids, false).
		Order("tenant_id ASC").Scan(&agents).Error; err != nil {
		return err
	}
	names := map[string]string{}
	available := map[string]bool{}
	for _, a := range agents {
		if _, ok := names[a.ID]; !ok {
			names[a.ID] = a.Name
		}
		// a switched-off agent is hidden from buyers, so it is not sold
		// (the row stays active: switching the agent back on resumes sales)
		if a.Distribution == types.AgentDistributionAddon && (a.Enabled == nil || *a.Enabled) {
			available[a.ID] = true
		}
	}
	holders, err := addonHolderCounts(db, ids, now)
	if err != nil {
		return err
	}
	for _, row := range rows {
		row.AgentName = names[row.AgentID]
		row.AgentAvailable = available[row.AgentID]
		row.Holders = holders[row.AgentID]
	}
	return nil
}

// addonHolderCounts counts, per agent, the current subscriptions holding a
// current add-on row for it (the same validity as the visibility rule).
func addonHolderCounts(db *gorm.DB, agentIDs []string, now time.Time) (map[string]int64, error) {
	out := map[string]int64{}
	if len(agentIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		AgentID string
		Holders int64
	}
	err := db.Table("usage_subscription_addons AS usa").
		Select("usa.agent_id AS agent_id, COUNT(*) AS holders").
		Joins("JOIN usage_subscriptions us ON us.id = usa.subscription_id").
		Where("usa.agent_id IN ?", agentIDs).
		Where("usa.starts_at <= ? AND (usa.ends_at IS NULL OR usa.ends_at > ?)", now, now).
		Where("us.status = ? AND us.starts_at <= ? AND (us.ends_at IS NULL OR us.ends_at > ?)",
			types.UsageSubscriptionActive, now, now).
		Group("usa.agent_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.AgentID] = row.Holders
	}
	return out, nil
}

// ProfileBuiltinAgents returns the stored built-in rows of the configuration
// profile(s) for ids (name customised by a SuperAdmin, on/off switch).
func (r *usagePlanRepository) ProfileBuiltinAgents(ctx context.Context, ids []string) ([]*types.CustomAgent, error) {
	rows := []*types.CustomAgent{}
	if len(ids) == 0 {
		return rows, nil
	}
	err := r.conn(ctx).
		Where("id IN ? AND is_builtin = ?", ids, true).
		Where("tenant_id IN (SELECT id FROM tenants WHERE kind = ? AND deleted_at IS NULL)", types.TenantKindConfigProfile).
		Order("tenant_id ASC").Find(&rows).Error
	return rows, err
}

// PendingOrderAddons maps each pending order of a scope to the add-on agents
// it buys.
func (r *usagePlanRepository) PendingOrderAddons(ctx context.Context, scopeType, scopeID string) (map[string][]string, error) {
	var orders []*types.UsageOrder
	if err := r.conn(ctx).Select("id", "addons").
		Where("scope_type = ? AND scope_id = ? AND status = ?", scopeType, scopeID, types.UsageOrderPending).
		Find(&orders).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(orders))
	for _, o := range orders {
		for _, l := range o.Addons {
			out[o.ID] = append(out[o.ID], l.AgentID)
		}
	}
	return out, nil
}
