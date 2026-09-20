package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type abbreviationRepository struct {
	db *gorm.DB
}

// NewAbbreviationRepository creates the abbreviation repository.
func NewAbbreviationRepository(db *gorm.DB) interfaces.AbbreviationRepository {
	return &abbreviationRepository{db: db}
}

func (r *abbreviationRepository) Create(ctx context.Context, abbr *types.Abbreviation) error {
	return r.db.WithContext(ctx).Create(abbr).Error
}

func (r *abbreviationRepository) GetByID(ctx context.Context, id string) (*types.Abbreviation, error) {
	var abbr types.Abbreviation
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&abbr).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &abbr, nil
}

func (r *abbreviationRepository) List(
	ctx context.Context, search string, isActive *bool, offset, limit int,
) ([]*types.Abbreviation, int64, error) {
	query := r.db.WithContext(ctx).Model(&types.Abbreviation{})

	if s := strings.TrimSpace(search); s != "" {
		like := "%" + s + "%"
		if query.Dialector.Name() == "postgres" {
			query = query.Where("short_form ILIKE ? OR full_form ILIKE ?", like, like)
		} else {
			query = query.Where(
				"lower(short_form) LIKE lower(?) OR lower(full_form) LIKE lower(?)", like, like,
			)
		}
	}
	if isActive != nil {
		query = query.Where("is_active = ?", *isActive)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []*types.Abbreviation
	err := query.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func (r *abbreviationRepository) ListActive(ctx context.Context) ([]*types.Abbreviation, error) {
	var rows []*types.Abbreviation
	err := r.db.WithContext(ctx).Where("is_active = ?", true).Find(&rows).Error
	return rows, err
}

func (r *abbreviationRepository) ListByShortForm(
	ctx context.Context, shortForm string,
) ([]*types.Abbreviation, error) {
	var rows []*types.Abbreviation
	err := r.db.WithContext(ctx).
		Where("lower(short_form) = lower(?)", strings.TrimSpace(shortForm)).
		Find(&rows).Error
	return rows, err
}

func (r *abbreviationRepository) Update(ctx context.Context, abbr *types.Abbreviation) error {
	return r.db.WithContext(ctx).Save(abbr).Error
}

func (r *abbreviationRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&types.Abbreviation{}, "id = ?", id).Error
}
