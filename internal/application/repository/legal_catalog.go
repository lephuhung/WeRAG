package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const legalCatalogBatchSize = 500

type legalCatalogRepository struct {
	db *gorm.DB
}

// NewLegalCatalogRepository creates the legal catalogue repository.
func NewLegalCatalogRepository(db *gorm.DB) interfaces.LegalCatalogRepository {
	return &legalCatalogRepository{db: db}
}

func (r *legalCatalogRepository) UpsertDocuments(ctx context.Context, docs []*types.LegalDocument) error {
	if len(docs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source", "document_number", "number_key", "title", "doc_type", "issuer",
			"signer", "signer_title", "field", "issued_on", "effective_on", "expired_on",
			"status", "source_updated_at", "updated_at",
		}),
	}).CreateInBatches(docs, legalCatalogBatchSize).Error
}

func (r *legalCatalogRepository) ReplaceRelations(ctx context.Context, rels []*types.LegalDocumentRelation) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&types.LegalDocumentRelation{}).Error; err != nil {
			return err
		}
		if len(rels) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).
			CreateInBatches(rels, legalCatalogBatchSize).Error
	})
}

func (r *legalCatalogRepository) FindByNumberKey(
	ctx context.Context, key string, limit int,
) ([]*types.LegalDocument, error) {
	var docs []*types.LegalDocument
	if strings.TrimSpace(key) == "" {
		return docs, nil
	}
	err := r.db.WithContext(ctx).Where("number_key = ?", key).
		Order("issued_on DESC").Limit(limit).Find(&docs).Error
	if err != nil || len(docs) > 0 {
		return docs, err
	}
	err = r.db.WithContext(ctx).Where("number_key LIKE ? ESCAPE ?", escapeLikeKeyword(key)+"/%", likeEscapeChar).
		Order("issued_on DESC").Limit(limit).Find(&docs).Error
	return docs, err
}

func (r *legalCatalogRepository) HasNumberKey(ctx context.Context, key string) (bool, error) {
	if strings.TrimSpace(key) == "" {
		return false, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&types.LegalDocument{}).Where("number_key = ?", key).Limit(1).Count(&n).Error
	return n > 0, err
}

func (r *legalCatalogRepository) FindByNumberPrefix(
	ctx context.Context, key string, limit int,
) ([]*types.LegalDocument, error) {
	var docs []*types.LegalDocument
	if strings.TrimSpace(key) == "" {
		return docs, nil
	}
	err := r.db.WithContext(ctx).Where("number_key LIKE ? ESCAPE ?", escapeLikeKeyword(key)+"/%", likeEscapeChar).
		Order("issued_on DESC").Limit(limit).Find(&docs).Error
	return docs, err
}

func (r *legalCatalogRepository) SearchByTitle(
	ctx context.Context, words []string, limit int,
) ([]*types.LegalDocument, error) {
	var docs []*types.LegalDocument
	query := r.db.WithContext(ctx).Model(&types.LegalDocument{})
	n := 0
	for _, w := range words {
		if w = strings.TrimSpace(w); w == "" {
			continue
		}
		like := "%" + escapeLikeKeyword(w) + "%"
		if query.Dialector.Name() == "postgres" {
			query = query.Where("title ILIKE ? ESCAPE ?", like, likeEscapeChar)
		} else {
			query = query.Where("lower(title) LIKE lower(?) ESCAPE ?", like, likeEscapeChar)
		}
		n++
	}
	if n == 0 {
		return docs, nil
	}
	err := query.Order("issued_on DESC").Limit(limit).Find(&docs).Error
	return docs, err
}

func (r *legalCatalogRepository) ListLinks(
	ctx context.Context, id string, outgoing, incoming []types.LegalRelation,
) ([]types.LegalRelationLink, error) {
	var links []types.LegalRelationLink
	for _, dir := range []struct {
		rels     []types.LegalRelation
		self     string
		other    string
		incoming bool
	}{
		{outgoing, "from_id", "to_id", false},
		{incoming, "to_id", "from_id", true},
	} {
		if len(dir.rels) == 0 {
			continue
		}
		var rows []types.LegalDocumentRelation
		if err := r.db.WithContext(ctx).
			Where(dir.self+" = ? AND relation IN ?", id, dir.rels).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			if dir.incoming {
				ids = append(ids, row.FromID)
			} else {
				ids = append(ids, row.ToID)
			}
		}
		var others []*types.LegalDocument
		if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&others).Error; err != nil {
			return nil, err
		}
		byID := make(map[string]*types.LegalDocument, len(others))
		for _, o := range others {
			byID[o.ID] = o
		}
		for _, row := range rows {
			otherID := row.ToID
			if dir.incoming {
				otherID = row.FromID
			}
			if o := byID[otherID]; o != nil {
				links = append(links, types.LegalRelationLink{
					Relation: row.Relation, Incoming: dir.incoming, Other: o,
				})
			}
		}
	}
	return links, nil
}

func (r *legalCatalogRepository) ListEdges(
	ctx context.Context, ids []string, rels []types.LegalRelation, incoming bool,
) ([]types.LegalDocumentRelation, error) {
	var rows []types.LegalDocumentRelation
	if len(ids) == 0 || len(rels) == 0 {
		return rows, nil
	}
	col := "from_id"
	if incoming {
		col = "to_id"
	}
	err := r.db.WithContext(ctx).Where(col+" IN ? AND relation IN ?", ids, rels).Find(&rows).Error
	return rows, err
}

func (r *legalCatalogRepository) FilterDocuments(
	ctx context.Context, f types.LegalCatalogFilter,
) ([]*types.LegalDocument, error) {
	var docs []*types.LegalDocument
	pg := r.db.Dialector.Name() == "postgres"
	q := r.db.WithContext(ctx).Model(&types.LegalDocument{})
	n := 0
	if k := strings.TrimSpace(f.NumberKey); k != "" {
		q = q.Where("number_key = ?", k)
		n++
	}
	if k := strings.TrimSpace(f.NumberPrefix); k != "" {
		q = q.Where("number_key LIKE ? ESCAPE ?", escapeLikeKeyword(k)+"/%", likeEscapeChar)
		n++
	}
	for _, w := range f.TitleWords {
		if w = strings.TrimSpace(w); w == "" {
			continue
		}
		like := "%" + escapeLikeKeyword(w) + "%"
		if pg {
			q = q.Where("title ILIKE ? ESCAPE ?", like, likeEscapeChar)
		} else {
			q = q.Where("lower(title) LIKE lower(?) ESCAPE ?", like, likeEscapeChar)
		}
		n++
	}
	if len(f.DocTypes) > 0 {
		// SQLite's lower() folds ASCII only: match the spellings instead
		var lowered, spellings []string
		for _, t := range f.DocTypes {
			t = strings.Join(strings.Fields(t), " ")
			if t == "" {
				continue
			}
			l := strings.ToLower(t)
			lowered = append(lowered, l)
			r := []rune(l)
			spellings = append(spellings, l, strings.ToUpper(string(r[:1]))+string(r[1:]))
		}
		if pg {
			q = q.Where("lower(doc_type) IN ?", lowered)
		} else {
			q = q.Where("doc_type IN ?", spellings)
		}
	}
	if f.IssuedYear > 0 {
		year := fmt.Sprintf("%04d", f.IssuedYear)
		yearExpr := "strftime('%Y', issued_on) = ?"
		if pg {
			yearExpr = "to_char(issued_on, 'YYYY') = ?"
		}
		q = q.Where("("+yearExpr+" OR number_key LIKE ? ESCAPE ?)", year, "%/"+year+"/%", likeEscapeChar)
	}
	if n == 0 && f.IssuedYear == 0 {
		return docs, nil // a type alone would list the whole catalogue
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 10
	}
	err := q.Order("issued_on DESC NULLS LAST").Order("id").Limit(limit).Find(&docs).Error
	return docs, err
}

func (r *legalCatalogRepository) ExternalCopies(
	ctx context.Context, documentIDs, kbIDs []string,
) (map[string][]types.LegalKnowledgeRef, error) {
	out := map[string][]types.LegalKnowledgeRef{}
	if len(documentIDs) == 0 || len(kbIDs) == 0 {
		return out, nil
	}
	ext := "k.metadata->>'external_id'"
	if r.db.Dialector.Name() != "postgres" {
		ext = "json_extract(k.metadata, '$.external_id')"
	}
	var rows []struct {
		ExternalID        string
		KnowledgeID       string
		KnowledgeBaseID   string
		KnowledgeBaseName string
		Title             string
	}
	err := r.db.WithContext(ctx).Table("knowledges AS k").
		Select(ext+" AS external_id, k.id AS knowledge_id, k.knowledge_base_id, kb.name AS knowledge_base_name, k.title").
		Joins("JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.deleted_at IS NULL").
		Joins("LEFT JOIN legal_document_links l ON l.knowledge_id = k.id").
		Where("k.knowledge_base_id IN ? AND k.deleted_at IS NULL AND "+ext+" IN ?", kbIDs, documentIDs).
		Where("k.parse_status IN ?", []string{types.ParseStatusCompleted, types.ParseStatusFinalizing}).
		Where("(l.knowledge_id IS NULL OR l.review_status = ?)", types.LegalReviewApproved).
		Order("k.created_at").Order("k.id").Scan(&rows).Error
	for _, row := range rows {
		out[row.ExternalID] = append(out[row.ExternalID], types.LegalKnowledgeRef{
			KnowledgeID: row.KnowledgeID, KnowledgeBaseID: row.KnowledgeBaseID,
			KnowledgeBaseName: row.KnowledgeBaseName, Title: row.Title,
		})
	}
	return out, err
}

func (r *legalCatalogRepository) LatestSnapshot(ctx context.Context) (*time.Time, error) {
	// ORDER BY … LIMIT 1 rather than MAX(): SQLite (unit tests) loses the
	// column type on an aggregate and returns text.
	var docs []types.LegalDocument
	err := r.db.WithContext(ctx).Select("source_updated_at").
		Where("source = ? AND source_updated_at IS NOT NULL", types.LegalDocumentSourceVBPL).
		Order("source_updated_at DESC").Limit(1).Find(&docs).Error
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	return docs[0].SourceUpdatedAt, nil
}

func (r *legalCatalogRepository) GetByIDs(ctx context.Context, ids []string) ([]*types.LegalDocument, error) {
	var docs []*types.LegalDocument
	if len(ids) == 0 {
		return docs, nil
	}
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&docs).Error
	return docs, err
}

func (r *legalCatalogRepository) SaveDocument(ctx context.Context, doc *types.LegalDocument) error {
	return r.db.WithContext(ctx).Save(doc).Error
}

func (r *legalCatalogRepository) SetSourcePending(ctx context.Context, id string, pending types.JSON) error {
	var value interface{}
	if len(pending) > 0 {
		value = pending
	}
	return r.db.WithContext(ctx).Model(&types.LegalDocument{}).
		Where("id = ?", id).UpdateColumn("source_pending", value).Error
}

func (r *legalCatalogRepository) NamedCopies(
	ctx context.Context, keys, kbIDs []string, limit int,
) ([]types.LegalNamedCopy, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	// per key: a number shared by many provinces must not crowd out another
	var docs []*types.LegalDocument
	for _, k := range keys {
		var part []*types.LegalDocument
		if err := r.db.WithContext(ctx).Where("number_key = ?", k).Order("issued_on DESC NULLS LAST, id").
			Limit(limit).Find(&part).Error; err != nil {
			return nil, err
		}
		docs = append(docs, part...)
	}
	if len(docs) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	type copyRow struct {
		ID              string
		KnowledgeBaseID string
		ExternalID      string
	}
	var copies []copyRow
	if len(kbIDs) > 0 {
		ext := "metadata->>'external_id'"
		if r.db.Dialector.Name() != "postgres" {
			ext = "json_extract(metadata, '$.external_id')"
		}
		// knowledge_base_id first: the partial index on (kb, external_id)
		if err := r.db.WithContext(ctx).Table("knowledges").
			Select("id, knowledge_base_id, "+ext+" AS external_id").
			Where("knowledge_base_id IN ? AND deleted_at IS NULL AND "+ext+" IN ?", kbIDs, ids).
			Where("parse_status IN ?", []string{types.ParseStatusCompleted, types.ParseStatusFinalizing}).
			Order("created_at, id").Scan(&copies).Error; err != nil {
			return nil, err
		}
	}
	byDoc := map[string][]copyRow{}
	for _, c := range copies {
		byDoc[c.ExternalID] = append(byDoc[c.ExternalID], c)
	}
	var out []types.LegalNamedCopy
	for _, d := range docs {
		if len(byDoc[d.ID]) == 0 {
			out = append(out, types.LegalNamedCopy{Document: d})
			continue
		}
		for _, c := range byDoc[d.ID] {
			out = append(out, types.LegalNamedCopy{Document: d, KnowledgeID: c.ID, KnowledgeBaseID: c.KnowledgeBaseID})
		}
	}
	return out, nil
}

func (r *legalCatalogRepository) ArticleChunkIDs(
	ctx context.Context, knowledgeIDs, articles []string, perArticle int,
) (map[string][]string, error) {
	out := map[string][]string{}
	if len(knowledgeIDs) == 0 || len(articles) == 0 {
		return out, nil
	}
	has := "jsonb_exists(metadata->'legal'->'article_nos', ?)"
	if r.db.Dialector.Name() != "postgres" {
		has = "EXISTS (SELECT 1 FROM json_each(metadata, '$.legal.article_nos') WHERE value = ?)"
	}
	for _, a := range articles {
		var rows []struct {
			ID          string
			KnowledgeID string
		}
		if err := r.db.WithContext(ctx).Table("chunks").Select("id, knowledge_id").
			Where("knowledge_id IN ? AND deleted_at IS NULL AND is_enabled AND chunk_type = ?", knowledgeIDs, types.ChunkTypeText).
			// an edit whose index is not synchronized yet is not shown (isSearchableChunk)
			Where("(index_status IS NULL OR index_status NOT IN ?)", []string{"processing", "failed"}).
			Where(has, a).Order("knowledge_id, chunk_index").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			k := row.KnowledgeID + "\x00" + a
			if len(out[k]) < perArticle {
				out[k] = append(out[k], row.ID)
			}
		}
	}
	return out, nil
}
