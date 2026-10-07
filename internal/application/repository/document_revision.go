package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type documentRevisionRepository struct{ db *gorm.DB }

// NewDocumentRevisionRepository persists the snapshot timeline of document
// workspaces.
func NewDocumentRevisionRepository(db *gorm.DB) interfaces.DocumentRevisionRepository {
	return &documentRevisionRepository{db: db}
}

// Create numbers the revision after the workspace's latest one. Two writers
// racing for the same seq hit the (workspace_id, seq) unique index; the loser
// retries with the next number.
func (r *documentRevisionRepository) Create(ctx context.Context, rev *types.DocumentRevision) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var maxSeq int
			if err := tx.Model(&types.DocumentRevision{}).
				Where("workspace_id = ?", rev.WorkspaceID).
				Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
				return err
			}
			rev.Seq = maxSeq + 1
			return tx.Create(rev).Error
		})
		if err == nil || !isDocumentRevisionSeqConflict(err) {
			return err
		}
		rev.ID = ""
	}
	return err
}

func isDocumentRevisionSeqConflict(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

func (r *documentRevisionRepository) ListByWorkspace(
	ctx context.Context, workspaceID string,
) ([]*types.DocumentRevision, error) {
	var revisions []*types.DocumentRevision
	err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).
		Order("seq DESC").Find(&revisions).Error
	return revisions, err
}

func (r *documentRevisionRepository) GetBySeq(
	ctx context.Context, workspaceID string, seq int,
) (*types.DocumentRevision, error) {
	var rev types.DocumentRevision
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND seq = ?", workspaceID, seq).First(&rev).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

func (r *documentRevisionRepository) Latest(ctx context.Context, workspaceID string) (*types.DocumentRevision, error) {
	var rev types.DocumentRevision
	err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("seq DESC").First(&rev).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rev, nil
}
