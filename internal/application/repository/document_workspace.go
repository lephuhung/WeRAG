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

// GetBySession returns the session's active document — the one whose tab
// was activated last, else the latest opened — or (nil, nil) when the
// session has none.
func (r *documentWorkspaceRepository) GetBySession(
	ctx context.Context, tenantID uint64, sessionID string,
) (*types.DocumentWorkspace, error) {
	var ws types.DocumentWorkspace
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		Order("active_at DESC NULLS LAST").Order("position DESC").
		First(&ws).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// ListBySession returns the session's documents in tab order (position).
func (r *documentWorkspaceRepository) ListBySession(
	ctx context.Context, tenantID uint64, sessionID string,
) ([]*types.DocumentWorkspace, error) {
	var list []*types.DocumentWorkspace
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		Order("position ASC").
		Find(&list).Error
	return list, err
}

// NextPosition returns one more than the highest position the session ever
// used, deleted documents included, so a handle is never reused.
func (r *documentWorkspaceRepository) NextPosition(ctx context.Context, tenantID uint64, sessionID string) (int, error) {
	var maxPos *int
	err := r.db.WithContext(ctx).Unscoped().Model(&types.DocumentWorkspace{}).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		Select("MAX(position)").Scan(&maxPos).Error
	if err != nil {
		return 0, err
	}
	if maxPos == nil {
		return 1, nil
	}
	return *maxPos + 1, nil
}

// SetActive marks the document as the session's active one.
func (r *documentWorkspaceRepository) SetActive(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&types.DocumentWorkspace{}).
		Where("id = ?", id).
		UpdateColumn("active_at", at).Error
}

// DeleteByID soft-deletes one document of the session.
func (r *documentWorkspaceRepository) DeleteByID(ctx context.Context, tenantID uint64, sessionID, id string) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ? AND id = ?", tenantID, sessionID, id).
		Delete(&types.DocumentWorkspace{}).Error
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
