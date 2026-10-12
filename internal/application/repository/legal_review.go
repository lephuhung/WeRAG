package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type legalReviewRepository struct {
	db *gorm.DB
}

// NewLegalReviewRepository creates the legal review repository.
func NewLegalReviewRepository(db *gorm.DB) interfaces.LegalReviewRepository {
	return &legalReviewRepository{db: db}
}

// publicKnowledgeJoin restricts a knowledges query to live, parsed rows of
// platform public KBs (public visibility, no owner tenant).
func publicKnowledgeJoin(q *gorm.DB) *gorm.DB {
	return q.Joins("JOIN knowledge_bases kb ON kb.id = knowledges.knowledge_base_id").
		Where("kb.visibility = ? AND kb.owner_tenant_id = 0 AND kb.deleted_at IS NULL", types.KBVisibilityPublic).
		Where("knowledges.deleted_at IS NULL")
}

func (r *legalReviewRepository) UnlinkedPublicKnowledge(
	ctx context.Context, limit int,
) ([]interfaces.LegalPublicKnowledge, error) {
	var rows []struct {
		ID              string
		KnowledgeBaseID string
		Metadata        types.JSON
	}
	err := publicKnowledgeJoin(r.db.WithContext(ctx).Model(&types.Knowledge{})).
		Select("knowledges.id, knowledges.knowledge_base_id, knowledges.metadata").
		// finalizing: chunks are stored, only enrichment (questions,
		// summary) is still running — the header is already readable.
		Where("knowledges.parse_status IN ?", []string{types.ParseStatusCompleted, types.ParseStatusFinalizing}).
		Where("NOT EXISTS (SELECT 1 FROM legal_document_links l WHERE l.knowledge_id = knowledges.id)").
		Order("knowledges.created_at").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]interfaces.LegalPublicKnowledge, 0, len(rows))
	for _, row := range rows {
		k := interfaces.LegalPublicKnowledge{KnowledgeID: row.ID, KnowledgeBaseID: row.KnowledgeBaseID}
		var meta struct {
			ExternalID string `json:"external_id"`
		}
		if len(row.Metadata) > 0 && json.Unmarshal(row.Metadata, &meta) == nil {
			k.ExternalID = strings.TrimSpace(meta.ExternalID)
		}
		out = append(out, k)
	}
	return out, nil
}

func (r *legalReviewRepository) HeaderText(ctx context.Context, knowledgeID string, maxChars int) (string, error) {
	var contents []string
	err := r.db.WithContext(ctx).Model(&types.Chunk{}).
		Where("knowledge_id = ? AND chunk_type = ?", knowledgeID, types.ChunkTypeText).
		Order("chunk_index").Limit(6).Pluck("content", &contents).Error
	if err != nil {
		return "", err
	}
	text := strings.Join(contents, "\n")
	if len(text) > maxChars {
		text = text[:maxChars]
	}
	return text, nil
}

func (r *legalReviewRepository) SaveLink(ctx context.Context, link *types.LegalDocumentLink) error {
	return r.db.WithContext(ctx).Save(link).Error
}

func (r *legalReviewRepository) GetLink(ctx context.Context, knowledgeID string) (*types.LegalDocumentLink, error) {
	var link types.LegalDocumentLink
	err := r.db.WithContext(ctx).Where("knowledge_id = ?", knowledgeID).First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

type legalLinkScan struct {
	types.LegalDocumentLink
	KnowledgeTitle    string
	KnowledgeBaseName string
}

func (r *legalReviewRepository) linkRows(q *gorm.DB, offset, limit int) ([]interfaces.LegalLinkRow, int64, error) {
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var scans []legalLinkScan
	err := q.Select("l.*, k.title AS knowledge_title, kb.name AS knowledge_base_name").
		Order("l.updated_at DESC").Offset(offset).Limit(limit).Scan(&scans).Error
	if err != nil {
		return nil, 0, err
	}
	rows := make([]interfaces.LegalLinkRow, 0, len(scans))
	for i := range scans {
		link := scans[i].LegalDocumentLink
		rows = append(rows, interfaces.LegalLinkRow{
			Link: &link, KnowledgeTitle: scans[i].KnowledgeTitle, KnowledgeBaseName: scans[i].KnowledgeBaseName,
		})
	}
	return rows, total, nil
}

// liveLinks selects links whose knowledge is still alive, joined to the
// knowledge title and KB name; kbID "" keeps every KB.
func (r *legalReviewRepository) liveLinks(ctx context.Context, kbID string) *gorm.DB {
	q := r.db.WithContext(ctx).Table("legal_document_links AS l").
		Joins("JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL").
		Joins("JOIN knowledge_bases kb ON kb.id = l.knowledge_base_id AND kb.deleted_at IS NULL")
	if kbID != "" {
		q = q.Where("l.knowledge_base_id = ?", kbID)
	}
	return q
}

func (r *legalReviewRepository) ListLinks(
	ctx context.Context, status types.LegalReviewStatus, kbID string, offset, limit int,
) ([]interfaces.LegalLinkRow, int64, error) {
	q := r.liveLinks(ctx, kbID)
	if status != "" {
		q = q.Where("l.review_status = ?", status)
	}
	return r.linkRows(q, offset, limit)
}

func (r *legalReviewRepository) ListSupersededLinks(
	ctx context.Context, kbID string, offset, limit int,
) ([]interfaces.LegalLinkRow, int64, error) {
	q := r.liveLinks(ctx, kbID).
		Joins("JOIN legal_documents d ON d.id = l.legal_document_id").
		Where("l.review_status = ?", types.LegalReviewApproved).
		Where("(d.status LIKE ? OR EXISTS (SELECT 1 FROM legal_document_relations r WHERE r.to_id = d.id AND r.relation IN ?))",
			"Hết hiệu lực%", []types.LegalRelation{
				types.LegalRelationReplaces, types.LegalRelationRepeals, types.LegalRelationExpires,
			})
	return r.linkRows(q, offset, limit)
}

func (r *legalReviewRepository) ApprovedDocumentIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var found []string
	err := r.liveLinks(ctx, "").
		Where("l.review_status = ? AND l.legal_document_id IN ?", types.LegalReviewApproved, ids).
		Distinct().Pluck("l.legal_document_id", &found).Error
	for _, id := range found {
		out[id] = true
	}
	return out, err
}

func (r *legalReviewRepository) ApprovedLinksForKnowledge(
	ctx context.Context, knowledgeIDs []string,
) (map[string]*types.LegalDocumentLink, error) {
	out := map[string]*types.LegalDocumentLink{}
	if len(knowledgeIDs) == 0 {
		return out, nil
	}
	var links []*types.LegalDocumentLink
	err := r.db.WithContext(ctx).
		Where("knowledge_id IN ? AND review_status = ?", knowledgeIDs, types.LegalReviewApproved).
		Find(&links).Error
	for _, l := range links {
		out[l.KnowledgeID] = l
	}
	return out, err
}

func (r *legalReviewRepository) KnowledgeForDocuments(
	ctx context.Context, documentIDs []string,
) (map[string][]types.LegalKnowledgeRef, error) {
	out := map[string][]types.LegalKnowledgeRef{}
	if len(documentIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		LegalDocumentID   string
		KnowledgeID       string
		KnowledgeBaseID   string
		KnowledgeBaseName string
		Title             string
	}
	err := r.liveLinks(ctx, "").
		Select("l.legal_document_id, l.knowledge_id, l.knowledge_base_id, kb.name AS knowledge_base_name, k.title").
		Where("l.review_status = ? AND l.legal_document_id IN ?", types.LegalReviewApproved, documentIDs).
		Order("k.created_at").Scan(&rows).Error
	for _, row := range rows {
		out[row.LegalDocumentID] = append(out[row.LegalDocumentID], types.LegalKnowledgeRef{
			KnowledgeID: row.KnowledgeID, KnowledgeBaseID: row.KnowledgeBaseID,
			KnowledgeBaseName: row.KnowledgeBaseName, Title: row.Title,
		})
	}
	return out, err
}
