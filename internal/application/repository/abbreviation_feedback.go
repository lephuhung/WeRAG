package repository

import (
	"context"

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
