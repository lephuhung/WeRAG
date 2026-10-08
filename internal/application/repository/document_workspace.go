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

type documentWorkspaceRepository struct{ db *gorm.DB }

// NewDocumentWorkspaceRepository persists the editable document of a chat
// session (document assistant).
func NewDocumentWorkspaceRepository(db *gorm.DB) interfaces.DocumentWorkspaceRepository {
	return &documentWorkspaceRepository{db: db}
}

func (r *documentWorkspaceRepository) Create(ctx context.Context, ws *types.DocumentWorkspace) error {
	return r.db.WithContext(ctx).Create(ws).Error
}

// GetBySession returns the session's active document — the target whose
// tab was activated last, else the latest opened — or (nil, nil) when the
// session has no target. Sources have no tab and are never active.
func (r *documentWorkspaceRepository) GetBySession(
	ctx context.Context, tenantID uint64, sessionID string,
) (*types.DocumentWorkspace, error) {
	var ws types.DocumentWorkspace
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ? AND role <> ?", tenantID, sessionID, types.DocumentWorkspaceRoleSource).
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

// ListBySession returns the session's documents of both roles in handle
// order (position).
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
		"role":          ws.Role,
		"text_status":   ws.TextStatus,
		"last_saved_at": ws.LastSavedAt,
		"closed_at":     ws.ClosedAt,
		"updated_at":    ws.UpdatedAt,
	}
}

// SaveText stores (or replaces) the text of a source document.
func (r *documentWorkspaceRepository) SaveText(ctx context.Context, text *types.DocumentWorkspaceText) error {
	if len(text.Chunks) == 0 {
		text.Chunks = types.JSON(`[]`)
	}
	now := time.Now()
	if text.CreatedAt.IsZero() {
		text.CreatedAt = now
	}
	text.UpdatedAt = now
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"content", "chunks", "token_count", "chunk_count", "updated_at"}),
	}).Create(text).Error
}

// GetText returns the stored text of a source, or (nil, nil) when none.
func (r *documentWorkspaceRepository) GetText(ctx context.Context, workspaceID string) (*types.DocumentWorkspaceText, error) {
	var text types.DocumentWorkspaceText
	err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).First(&text).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &text, nil
}
