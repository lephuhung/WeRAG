package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type documentWorkspaceRepository struct{ db *gorm.DB }

// NewDocumentWorkspaceRepository persists the editable document of a chat
// session (document assistant).
func NewDocumentWorkspaceRepository(db *gorm.DB) interfaces.DocumentWorkspaceRepository {
	return &documentWorkspaceRepository{db: db}
}

func (r *documentWorkspaceRepository) Create(ctx context.Context, ws *types.DocumentWorkspace) error {
	return r.db.WithContext(ctx).Create(ws).Error
}

// GetBySession returns (nil, nil) when the session has no workspace.
func (r *documentWorkspaceRepository) GetBySession(
	ctx context.Context, tenantID uint64, sessionID string,
) (*types.DocumentWorkspace, error) {
	var ws types.DocumentWorkspace
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		First(&ws).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// GetByID returns (nil, nil) when no live workspace has this id.
func (r *documentWorkspaceRepository) GetByID(ctx context.Context, id string) (*types.DocumentWorkspace, error) {
	var ws types.DocumentWorkspace
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&ws).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

func (r *documentWorkspaceRepository) Update(ctx context.Context, ws *types.DocumentWorkspace) error {
	ws.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(&types.DocumentWorkspace{}).
		Where("id = ?", ws.ID).
		Updates(documentWorkspaceColumns(ws)).Error
}

func (r *documentWorkspaceRepository) UpdateIfRevision(
	ctx context.Context, ws *types.DocumentWorkspace, expectedRevision int,
) (bool, error) {
	ws.UpdatedAt = time.Now()
	res := r.db.WithContext(ctx).Model(&types.DocumentWorkspace{}).
		Where("id = ? AND revision = ?", ws.ID, expectedRevision).
		Updates(documentWorkspaceColumns(ws))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *documentWorkspaceRepository) Delete(ctx context.Context, tenantID uint64, sessionID string) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		Delete(&types.DocumentWorkspace{}).Error
}

// documentWorkspaceColumns lists every mutable column explicitly so zero
// values (status reset, save_count 0, nil timestamps) are written too.
func documentWorkspaceColumns(ws *types.DocumentWorkspace) map[string]interface{} {
	return map[string]interface{}{
		"user_id":       ws.UserID,
		"original_ref":  ws.OriginalRef,
		"current_ref":   ws.CurrentRef,
		"file_name":     ws.FileName,
		"file_type":     ws.FileType,
		"file_size":     ws.FileSize,
		"revision":      ws.Revision,
		"status":        ws.Status,
		"save_count":    ws.SaveCount,
		"last_saved_at": ws.LastSavedAt,
		"closed_at":     ws.ClosedAt,
		"updated_at":    ws.UpdatedAt,
	}
}
