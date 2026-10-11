package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

const legalCatalogMaxLookup = 5

// Relations reported for a looked-up document. Outgoing: what it does to
// others (including what it is based on). Incoming: what was done to it —
// the edges that decide whether it still applies. Incoming based_on and
// references are left out: a Luật is cited by thousands of documents and
// that says nothing about its own validity.
var (
	legalStatusOutgoing = []types.LegalRelation{
		types.LegalRelationReplaces, types.LegalRelationRepeals, types.LegalRelationExpires,
		types.LegalRelationPartiallyExpires, types.LegalRelationSuspends, types.LegalRelationAmends,
		types.LegalRelationGuides, types.LegalRelationConsolidates, types.LegalRelationBasedOn,
	}
	legalStatusIncoming = []types.LegalRelation{
		types.LegalRelationReplaces, types.LegalRelationRepeals, types.LegalRelationExpires,
		types.LegalRelationPartiallyExpires, types.LegalRelationSuspends, types.LegalRelationAmends,
		types.LegalRelationGuides, types.LegalRelationInterprets, types.LegalRelationConsolidates,
		types.LegalRelationCorrects,
	}
)

type legalCatalogService struct {
	repo interfaces.LegalCatalogRepository
}

// NewLegalCatalogService creates the legal catalogue service.
func NewLegalCatalogService(repo interfaces.LegalCatalogRepository) interfaces.LegalCatalogService {
	return &legalCatalogService{repo: repo}
}

func (s *legalCatalogService) Lookup(
	ctx context.Context, documentNumber, title string, limit int,
) ([]*types.LegalStatusReport, error) {
	if limit <= 0 || limit > legalCatalogMaxLookup {
		limit = legalCatalogMaxLookup
	}
	var docs []*types.LegalDocument
	var err error
	if key := vietnamese_legal.CatalogNumberKey(documentNumber); key != "" {
		if docs, err = s.repo.FindByNumberKey(ctx, key, limit); err != nil {
			return nil, err
		}
	}
	if len(docs) == 0 && strings.TrimSpace(title) != "" {
		if docs, err = s.repo.SearchByTitle(ctx, strings.Fields(title), limit); err != nil {
			return nil, err
		}
	}
	reports := make([]*types.LegalStatusReport, 0, len(docs))
	for _, doc := range docs {
		links, err := s.repo.ListLinks(ctx, doc.ID, legalStatusOutgoing, legalStatusIncoming)
		if err != nil {
			return nil, err
		}
		reports = append(reports, &types.LegalStatusReport{Document: doc, Links: links})
	}
	return reports, nil
}
