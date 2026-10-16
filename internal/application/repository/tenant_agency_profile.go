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

type tenantAgencyProfileRepository struct {
	db *gorm.DB
}

// NewTenantAgencyProfileRepository stores each workspace's issuing agency.
func NewTenantAgencyProfileRepository(db *gorm.DB) interfaces.TenantAgencyProfileRepository {
	return &tenantAgencyProfileRepository{db: db}
}

func (r *tenantAgencyProfileRepository) Get(ctx context.Context, tenantID uint64) (*types.TenantAgencyProfile, error) {
	var p types.TenantAgencyProfile
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *tenantAgencyProfileRepository) Save(ctx context.Context, p *types.TenantAgencyProfile, expectedVersion int) (*types.TenantAgencyProfile, error) {
	now := time.Now().UTC()
	row := *p
	row.UpdatedAt = now
	if row.DefaultRecipients == nil {
		row.DefaultRecipients = types.StringArray{}
	}
	if expectedVersion == 0 {
		row.Version, row.CreatedAt = 1, now
		res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			return nil, interfaces.ErrAgencyProfileVersion
		}
		return &row, nil
	}
	res := r.db.WithContext(ctx).Model(&types.TenantAgencyProfile{}).
		Where("tenant_id = ? AND version = ?", row.TenantID, expectedVersion).
		Updates(map[string]interface{}{
			"parent_agency": row.ParentAgency, "agency_name": row.AgencyName, "agency_abbr": row.AgencyAbbr,
			"locality": row.Locality, "head_title": row.HeadTitle, "collective_name": row.CollectiveName,
			"default_recipients": row.DefaultRecipients, "version": expectedVersion + 1,
			"updated_by": row.UpdatedBy, "updated_at": now,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, interfaces.ErrAgencyProfileVersion
	}
	return r.Get(ctx, row.TenantID)
}
