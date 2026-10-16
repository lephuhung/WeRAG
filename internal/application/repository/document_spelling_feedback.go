package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type documentSpellingFeedbackRepository struct {
	db *gorm.DB
}

// NewDocumentSpellingFeedbackRepository stores verdicts on the document
// assistant's spelling underlines.
func NewDocumentSpellingFeedbackRepository(db *gorm.DB) interfaces.DocumentSpellingFeedbackRepository {
	return &documentSpellingFeedbackRepository{db: db}
}

func (r *documentSpellingFeedbackRepository) Record(ctx context.Context, rows []types.DocumentSpellingFeedback) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&rows).Error
}

// Dismissed returns the tenant's "not a mistake" verdicts on the
// paragraphs with these hashes (paragraph_hash and wrong only).
func (r *documentSpellingFeedbackRepository) Dismissed(ctx context.Context, tenantID uint64, paragraphHashes []string) ([]types.DocumentSpellingFeedback, error) {
	if len(paragraphHashes) == 0 {
		return nil, nil
	}
	var rows []types.DocumentSpellingFeedback
	err := r.db.WithContext(ctx).
		Select("paragraph_hash", "wrong").
		Where("tenant_id = ? AND paragraph_hash IN ? AND source = ? AND verdict = ?", tenantID, paragraphHashes,
			types.SpellingFeedbackSourceUser, types.SpellingVerdictNotError).
		Limit(5000).Find(&rows).Error
	return rows, err
}
