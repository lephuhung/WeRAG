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
	repo    interfaces.LegalCatalogRepository
	reviews interfaces.LegalReviewRepository
}

// NewLegalCatalogService creates the legal catalogue service.
func NewLegalCatalogService(
	repo interfaces.LegalCatalogRepository, reviews interfaces.LegalReviewRepository,
) interfaces.LegalCatalogService {
	return &legalCatalogService{repo: repo, reviews: reviews}
}

// find resolves a document reference: by số ký hiệu first, else by title
// words. Newest first.
func (s *legalCatalogService) find(
	ctx context.Context, documentNumber, title string, limit int,
) ([]*types.LegalDocument, error) {
	if limit <= 0 || limit > legalCatalogMaxLookup {
		limit = legalCatalogMaxLookup
	}
	if key := vietnamese_legal.CatalogNumberKey(documentNumber); key != "" {
		docs, err := s.repo.FindByNumberKey(ctx, key, limit)
		if err != nil || len(docs) > 0 {
			return docs, err
		}
	}
	if strings.TrimSpace(title) == "" {
		return nil, nil
	}
	return s.repo.SearchByTitle(ctx, strings.Fields(title), limit)
}

// legalCandidatesScan is how many rows a bare number is matched against.
const legalCandidatesScan = 200

func (s *legalCatalogService) Candidates(
	ctx context.Context, documentNumber, docType string, limit int,
) ([]*types.LegalDocument, int, error) {
	key := vietnamese_legal.CatalogNumberKey(documentNumber)
	if key == "" {
		return nil, 0, nil
	}
	// a bare number may be a document's whole number ("53", a local
	// document) and the prefix of many more ("53/2022/NĐ-CP"): both
	docs, err := s.repo.FindByNumberPrefix(ctx, key, legalCandidatesScan)
	if err != nil {
		return nil, 0, err
	}
	if exact, err := s.repo.FindByNumberKey(ctx, key, legalCatalogMaxLookup); err == nil {
		for _, d := range exact {
			if vietnamese_legal.CatalogNumberKey(d.DocumentNumber) == key {
				docs = append(docs, d)
			}
		}
	}
	want := strings.ToLower(strings.Join(strings.Fields(docType), " "))
	out := docs[:0:0]
	for _, d := range docs {
		if want == "" || strings.ToLower(strings.Join(strings.Fields(d.DocType), " ")) == want {
			out = append(out, d)
		}
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (s *legalCatalogService) Lookup(
	ctx context.Context, documentNumber, title string, limit int,
) ([]*types.LegalStatusReport, error) {
	docs, err := s.find(ctx, documentNumber, title, limit)
	if err != nil {
		return nil, err
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
