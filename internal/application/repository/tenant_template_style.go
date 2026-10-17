package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type tenantTemplateStyleRepository struct {
	db *gorm.DB
}

// NewTenantTemplateStyleRepository stores each workspace's template font sizes.
func NewTenantTemplateStyleRepository(db *gorm.DB) interfaces.TenantTemplateStyleRepository {
	return &tenantTemplateStyleRepository{db: db}
}

func (r *tenantTemplateStyleRepository) Get(ctx context.Context, tenantID uint64) (*types.TenantTemplateStyle, error) {
	var s types.TenantTemplateStyle
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Take(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *tenantTemplateStyleRepository) Save(ctx context.Context, s *types.TenantTemplateStyle) error {
	row := *s
	row.UpdatedAt = time.Now().UTC()
	if len(row.FontSizes) == 0 {
		row.FontSizes = types.JSON("{}")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"font_sizes", "updated_by", "updated_at"}),
	}).Create(&row).Error
}
