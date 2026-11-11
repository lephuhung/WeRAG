package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type legalLookupMissRepository struct {
	db *gorm.DB
}

// NewLegalLookupMissRepository creates the repository of lookup misses.
func NewLegalLookupMissRepository(db *gorm.DB) interfaces.LegalLookupMissRepository {
	return &legalLookupMissRepository{db: db}
}

func (r *legalLookupMissRepository) Record(ctx context.Context, m *types.LegalLookupMiss) error {
	if m.Count <= 0 {
		m.Count = 1
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "query"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"count":          gorm.Expr("legal_lookup_misses.count + 1"),
			"sample":         m.Sample,
			"number":         m.Number,
			"reason":         m.Reason,
			"last_at":        m.LastAt,
			"last_tenant_id": m.LastTenantID,
			"last_user_id":   m.LastUserID,
		}),
	}).Create(m).Error
}

func (r *legalLookupMissRepository) List(
	ctx context.Context, byCount bool, offset, limit int,
) ([]*types.LegalLookupMiss, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&types.LegalLookupMiss{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	q := r.db.WithContext(ctx).Model(&types.LegalLookupMiss{})
	if byCount {
		q = q.Order("count DESC").Order("last_at DESC")
	} else {
		q = q.Order("last_at DESC")
	}
	var rows []*types.LegalLookupMiss
	err := q.Order("id").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func (r *legalLookupMissRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&types.LegalLookupMiss{}, id).Error
}

func (r *legalLookupMissRepository) Prune(ctx context.Context, staleBefore time.Time, keep int) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("count <= 1 AND last_at < ?", staleBefore).Delete(&types.LegalLookupMiss{}).Error; err != nil {
		return err
	}
	if keep <= 0 {
		return nil
	}
	// the keep-th most recent row is the cut-off; older rows go
	var cut []types.LegalLookupMiss
	if err := db.Select("id", "last_at").Order("last_at DESC").Order("id DESC").
		Offset(keep - 1).Limit(1).Find(&cut).Error; err != nil || len(cut) == 0 {
		return err
	}
	c := cut[0]
	return db.Where("last_at < ? OR (last_at = ? AND id < ?)", c.LastAt, c.LastAt, c.ID).
		Delete(&types.LegalLookupMiss{}).Error
}
