package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type documentFormatFeedbackRepository struct {
	db *gorm.DB
}

// NewDocumentFormatFeedbackRepository stores users' verdicts on the NĐ30
// format check reports.
func NewDocumentFormatFeedbackRepository(db *gorm.DB) interfaces.DocumentFormatFeedbackRepository {
	return &documentFormatFeedbackRepository{db: db}
}

func (r *documentFormatFeedbackRepository) Record(ctx context.Context, row *types.DocumentFormatFeedback) error {
	return r.db.WithContext(ctx).Create(row).Error
}
