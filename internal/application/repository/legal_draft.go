package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type legalDraftRepository struct {
	db *gorm.DB
}

// NewLegalDraftRepository creates the repository of legal OCR drafts.
func NewLegalDraftRepository(db *gorm.DB) interfaces.LegalDraftRepository {
	return &legalDraftRepository{db: db}
}

// legalDraftListColumns are the columns of a list row: everything but the
// OCR text, which can run to hundreds of kilobytes.
var legalDraftListColumns = []string{
	"id", "request_id", "legal_document_id", "document_number", "doc_type", "title", "pdf_file_name", "pdf_ref",
	"pdf_bytes", "ocr_pages", "ocr_engine", "ocr_report", "status", "error", "session_id", "attachment_id", "workspace_id",
	"structure", "approved_knowledge_id", "approved_kb_id", "created_by", "reviewed_by", "approved_by",
	"approved_at", "created_at", "updated_at",
}

func (r *legalDraftRepository) Create(ctx context.Context, d *types.LegalDocumentDraft) error {
	if d == nil || strings.TrimSpace(d.LegalDocumentID) == "" {
		return errors.New("legal draft: legal document id is required")
	}
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.Status == "" {
		d.Status = types.LegalDraftFetching
	}
	return r.db.WithContext(ctx).Create(d).Error
}

func (r *legalDraftRepository) take(q *gorm.DB) (*types.LegalDocumentDraft, error) {
	var d types.LegalDocumentDraft
	err := q.Take(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *legalDraftRepository) Get(ctx context.Context, id string) (*types.LegalDocumentDraft, error) {
	return r.take(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *legalDraftRepository) GetByDocumentID(ctx context.Context, legalDocumentID string) (*types.LegalDocumentDraft, error) {
	return r.take(r.db.WithContext(ctx).Where("legal_document_id = ?", legalDocumentID))
}

func (r *legalDraftRepository) ListByDocumentIDs(ctx context.Context, ids []string) ([]*types.LegalDocumentDraft, error) {
	var rows []*types.LegalDocumentDraft
	if len(ids) == 0 {
		return rows, nil
	}
	err := r.db.WithContext(ctx).Select(legalDraftListColumns).Where("legal_document_id IN ?", ids).Find(&rows).Error
	return rows, err
}

func (r *legalDraftRepository) List(
	ctx context.Context, status string, offset, limit int,
) ([]*types.LegalDocumentDraft, int64, error) {
	q := r.db.WithContext(ctx).Model(&types.LegalDocumentDraft{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	var rows []*types.LegalDocumentDraft
	err := q.Select(legalDraftListColumns).Order("updated_at DESC").Order("id").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func (r *legalDraftRepository) Update(ctx context.Context, id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	set := make(map[string]any, len(fields)+1)
	for k, v := range fields {
		set[k] = v
	}
	set["updated_at"] = time.Now()
	return r.db.WithContext(ctx).Model(&types.LegalDocumentDraft{}).Where("id = ?", id).Updates(set).Error
}

func (r *legalDraftRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.LegalDocumentDraft{}).Error
}
