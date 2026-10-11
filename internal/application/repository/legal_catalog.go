package repository

import (
	"context"
	"strings"

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
