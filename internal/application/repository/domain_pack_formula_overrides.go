package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type domainPackFormulaOverrideRepository struct {
	db *gorm.DB
}

// NewDomainPackFormulaOverrideRepository creates the store of the
// SuperAdmin's edits of domain pack formulas.
func NewDomainPackFormulaOverrideRepository(db *gorm.DB) interfaces.DomainPackFormulaOverrideRepository {
	return &domainPackFormulaOverrideRepository{db: db}
}

func (r *domainPackFormulaOverrideRepository) List(ctx context.Context) ([]*types.DomainPackFormulaOverride, error) {
	var out []*types.DomainPackFormulaOverride
	err := r.db.WithContext(ctx).Order("pack_name, formula_id").Find(&out).Error
	return out, err
}

func (r *domainPackFormulaOverrideRepository) Upsert(ctx context.Context, o *types.DomainPackFormulaOverride) error {
	now := time.Now()
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	}
	o.UpdatedAt = now
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "pack_name"}, {Name: "formula_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"formula", "updated_by", "updated_at"}),
	}).Create(o).Error
}

func (r *domainPackFormulaOverrideRepository) Delete(ctx context.Context, packName, formulaID string) error {
	return r.db.WithContext(ctx).
		Where("pack_name = ? AND formula_id = ?", packName, formulaID).
		Delete(&types.DomainPackFormulaOverride{}).Error
}
