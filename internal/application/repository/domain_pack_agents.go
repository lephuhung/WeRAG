package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type domainPackAgentRepository struct {
	db *gorm.DB
}

// NewDomainPackAgentRepository creates the lookup of agents by domain pack.
func NewDomainPackAgentRepository(db *gorm.DB) interfaces.DomainPackAgentRepository {
	return &domainPackAgentRepository{db: db}
}

func (r *domainPackAgentRepository) ListAgentsUsingPack(ctx context.Context, name string) ([]types.DomainPackAgent, error) {
	out := []types.DomainPackAgent{}
	if name == "" {
		return out, nil
	}
	db := r.db.WithContext(ctx)
	// config is jsonb on Postgres; the SQLite test schema stores JSON text
	contains := "a.config->'domain_packs' @> jsonb_build_array(?::text)"
	if db.Dialector.Name() != "postgres" {
		contains = "EXISTS (SELECT 1 FROM json_each(a.config, '$.domain_packs') AS p WHERE p.value = ?)"
	}
	err := db.Table("custom_agents AS a").
		Select("a.id AS id, a.name AS name, a.tenant_id AS tenant_id, COALESCE(t.name, '') AS tenant_name, "+
			"a.applies_to_tenant_ids AS applies_to_tenant_ids, a.distribution AS distribution, "+
			"uaa.price_vnd AS addon_price_vnd, uaa.is_active AS addon_active").
		Joins("LEFT JOIN tenants t ON t.id = a.tenant_id").
		Joins("LEFT JOIN usage_agent_addons uaa ON uaa.agent_id = a.id").
		Where("a.deleted_at IS NULL").
		Where(contains, name).
		Order("a.tenant_id").Order("a.name").
		Scan(&out).Error
	if err != nil {
		return out, err
	}
	if len(out) > 0 {
		ids := make([]string, 0, len(out))
		for _, a := range out {
			ids = append(ids, a.ID)
		}
		holders, err := addonHolderCounts(db, ids, time.Now())
		if err != nil {
			return out, err
		}
		for i := range out {
			out[i].AddonHolders = holders[out[i].ID]
		}
	}
	return out, r.nameScopeTenants(db, out)
}

// nameScopeTenants fills AppliesTo with the names of each agent's "Tổ chức
// áp dụng" list (an id with no tenant row keeps an empty name).
func (r *domainPackAgentRepository) nameScopeTenants(db *gorm.DB, agents []types.DomainPackAgent) error {
	var ids []uint64
	for _, a := range agents {
		ids = append(ids, a.AppliesToTenantIDs...)
	}
	names := map[uint64]string{}
	if len(ids) > 0 {
		var rows []struct {
			ID   uint64
			Name string
		}
		if err := db.Table("tenants").Select("id, name").Where("id IN ?", ids).
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			names[row.ID] = row.Name
		}
	}
	for i := range agents {
		agents[i].AppliesTo = make([]types.DomainPackAgentScope, 0, len(agents[i].AppliesToTenantIDs))
		for _, id := range agents[i].AppliesToTenantIDs {
			agents[i].AppliesTo = append(agents[i].AppliesTo, types.DomainPackAgentScope{TenantID: id, TenantName: names[id]})
		}
	}
	return nil
}
