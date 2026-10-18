package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type temporaryDocumentRepository struct{ db *gorm.DB }

func NewTemporaryDocumentRepository(db *gorm.DB) interfaces.TemporaryDocumentRepository {
	return &temporaryDocumentRepository{db: db}
}

func (r *temporaryDocumentRepository) Create(ctx context.Context, document *types.TemporaryDocument) error {
	return r.db.WithContext(ctx).Create(document).Error
}

func (r *temporaryDocumentRepository) GetByID(ctx context.Context, tenantID uint64, documentID string) (*types.TemporaryDocument, error) {
	var document types.TemporaryDocument
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, documentID).First(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &document, err
}

func (r *temporaryDocumentRepository) GetScoped(ctx context.Context, tenantID uint64, sessionID, documentID string) (*types.TemporaryDocument, error) {
	var document types.TemporaryDocument
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND session_id = ? AND id = ?", tenantID, sessionID, documentID).First(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &document, err
}

func (r *temporaryDocumentRepository) ListScoped(ctx context.Context, tenantID uint64, sessionID string) ([]*types.TemporaryDocument, error) {
	var documents []*types.TemporaryDocument
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).Order("created_at ASC").Find(&documents).Error
	return documents, err
}

// CountProcessingForSessionOwner counts the uploads of the owner of
// sessionID that are being parsed, across their sessions.
func (r *temporaryDocumentRepository) CountProcessingForSessionOwner(ctx context.Context, tenantID uint64, sessionID string, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&types.TemporaryDocument{}).
		Where("tenant_id = ? AND status = ? AND started_at > ?", tenantID, types.TemporaryDocumentStatusProcessing, since.UTC()).
		Where("session_id IN (SELECT id FROM sessions WHERE tenant_id = ? AND user_id = (SELECT user_id FROM sessions WHERE id = ?))", tenantID, sessionID).
		Count(&n).Error
	return n, err
}

func (r *temporaryDocumentRepository) MarkProcessing(ctx context.Context, tenantID uint64, documentID string, startedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&types.TemporaryDocument{}).
		Where("tenant_id = ? AND id = ?", tenantID, documentID).
		Updates(map[string]interface{}{"status": types.TemporaryDocumentStatusProcessing, "started_at": startedAt.UTC(), "error_message": ""}).Error
}

func (r *temporaryDocumentRepository) MarkReady(ctx context.Context, tenantID uint64, documentID, content string, chunks, imageRefs, metadata types.JSON, tokenCount, chunkCount int, readyAt time.Time) error {
	return r.db.WithContext(ctx).Model(&types.TemporaryDocument{}).
		Where("tenant_id = ? AND id = ?", tenantID, documentID).
		Updates(map[string]interface{}{
			"status": types.TemporaryDocumentStatusReady, "content": content, "chunks": chunks,
			"image_refs": imageRefs, "metadata": metadata, "token_count": tokenCount,
			"chunk_count": chunkCount, "ready_at": readyAt.UTC(), "error_message": "",
		}).Error
}

func (r *temporaryDocumentRepository) MarkFailed(ctx context.Context, tenantID uint64, documentID, message string) error {
	return r.db.WithContext(ctx).Model(&types.TemporaryDocument{}).
		Where("tenant_id = ? AND id = ?", tenantID, documentID).
		Updates(map[string]interface{}{"status": types.TemporaryDocumentStatusFailed, "error_message": message}).Error
}

func (r *temporaryDocumentRepository) DeleteScoped(ctx context.Context, tenantID uint64, sessionID, documentID string) error {
	return r.db.WithContext(ctx).Where("tenant_id = ? AND session_id = ? AND id = ?", tenantID, sessionID, documentID).Delete(&types.TemporaryDocument{}).Error
}

// ListExpired lists the uploads whose expires_at is at or before before.
func (r *temporaryDocumentRepository) ListExpired(ctx context.Context, before time.Time, limit int) ([]*types.TemporaryDocument, error) {
	var documents []*types.TemporaryDocument
	err := r.db.WithContext(ctx).Where("expires_at <= ?", before.UTC()).Order("expires_at ASC").Limit(limit).Find(&documents).Error
	return documents, err
}
