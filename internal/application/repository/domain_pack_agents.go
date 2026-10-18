package repository

import (
	"context"

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
		Select("a.id AS id, a.name AS name, a.tenant_id AS tenant_id, COALESCE(t.name, '') AS tenant_name").
		Joins("LEFT JOIN tenants t ON t.id = a.tenant_id").
		Where("a.deleted_at IS NULL").
		Where(contains, name).
		Order("a.tenant_id").Order("a.name").
		Scan(&out).Error
	return out, err
}
