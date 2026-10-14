package repository

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type abbreviationFeedbackRepository struct {
	db *gorm.DB
}

// NewAbbreviationFeedbackRepository stores answers to the chat gate's
// "unclear word" question.
func NewAbbreviationFeedbackRepository(db *gorm.DB) interfaces.AbbreviationFeedbackRepository {
	return &abbreviationFeedbackRepository{db: db}
}

func (r *abbreviationFeedbackRepository) Record(ctx context.Context, rows []types.AbbreviationTypoFeedback) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&rows).Error
}

// Latest returns, per word (lower case), the tenant's latest typo, keep or
// abbreviation answer for it.
func (r *abbreviationFeedbackRepository) Latest(ctx context.Context, tenantID uint64, words []string) (map[string]types.AbbreviationTypoFeedback, error) {
	out := map[string]types.AbbreviationTypoFeedback{}
	if len(words) == 0 {
		return out, nil
	}
	lower := make([]string, len(words))
	for i, w := range words {
		lower[i] = strings.ToLower(w)
	}
	var rows []types.AbbreviationTypoFeedback
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND LOWER(word) IN ? AND choice IN ?", tenantID, lower,
			[]string{types.AbbreviationFeedbackTypo, types.AbbreviationFeedbackKeep, types.AbbreviationFeedbackAbbreviation}).
		Order("id DESC").Limit(200).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		k := strings.ToLower(row.Word)
		if _, seen := out[k]; !seen {
			out[k] = row
		}
	}
	return out, nil
}
