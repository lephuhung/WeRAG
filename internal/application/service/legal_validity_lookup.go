package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

const (
	// legalLookupMaxMatches caps the documents one lookup returns.
	legalLookupMaxMatches = 10
	// legalLookupMaxPerGroup caps the documents listed per relation group
	// (a Luật can be amended by dozens).
	legalLookupMaxPerGroup = 8
	// legalLookupBareScan bounds the rows a bare number ("53") is matched
	// against before the doc-type / title filter.
	legalLookupBareScan = 200
	// legalLookupSnapshotTTL is how long the catalogue's snapshot date is
	// cached (it changes once a night).
	legalLookupSnapshotTTL = 10 * time.Minute
)

// legalLookupIncoming are the relations that bear on a match's validity:
// what other documents did to it.
var legalLookupIncoming = []types.LegalRelation{
	types.LegalRelationReplaces, types.LegalRelationRepeals, types.LegalRelationExpires,
	types.LegalRelationPartiallyExpires, types.LegalRelationSuspends,
	types.LegalRelationAmends, types.LegalRelationCorrects,
}

type legalValidityLookupService struct {
	repo    interfaces.LegalCatalogRepository
	reviews interfaces.LegalReviewRepository
	kbs     interfaces.KnowledgeBaseRepository
	now     func() time.Time

	mu         sync.Mutex
	snapshot   string
	snapshotAt time.Time
}

// NewLegalValidityLookupService creates the validity lookup service.
func NewLegalValidityLookupService(
	repo interfaces.LegalCatalogRepository,
	reviews interfaces.LegalReviewRepository,
	kbs interfaces.KnowledgeBaseRepository,
) interfaces.LegalValidityLookupService {
	return &legalValidityLookupService{repo: repo, reviews: reviews, kbs: kbs, now: time.Now}
}

func (s *legalValidityLookupService) LookupValidity(
	ctx context.Context, query string, limit int,
) (*types.LegalValidityLookup, error) {
	if limit <= 0 || limit > legalLookupMaxMatches {
		limit = legalLookupMaxMatches
	}
	query = strings.Join(strings.Fields(query), " ")
	out := &types.LegalValidityLookup{Query: query, Matches: []types.LegalValidityMatch{}}
	if query == "" {
		return out, nil
	}
	docs, by, err := s.find(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out.By = by
	knowledge := s.readableCopies(ctx, docs)
	now := s.now()
	for _, doc := range docs {
		links, err := s.repo.ListLinks(ctx, doc.ID, nil, legalLookupIncoming)
		if err != nil {
			return nil, err
		}
		m := legalValidityMatch(&types.LegalStatusReport{Document: doc, Links: links}, now)
		m.Knowledge = knowledge[doc.ID]
		if m.Knowledge == nil {
			m.Knowledge = []types.LegalKnowledgeRef{}
		}
		out.Matches = append(out.Matches, m)
	}
	out.CatalogSnapshotDate = s.snapshotDate(ctx)
	return out, nil
}

// find resolves the query: a token holding a số hiệu ("30/2020/NĐ-CP",
// with or without the words around it) by number key — every catalogue
// row of that number, since số hiệu is not unique; a bare number ("53",
// "Nghị định 53") by number prefix, narrowed by the words around it; else
// title words. by says which matched.
func (s *legalValidityLookupService) find(
	ctx context.Context, query string, limit int,
) ([]*types.LegalDocument, string, error) {
	if num, rest := legalLookupNumberToken(query); num != "" {
		key := vietnamese_legal.CatalogNumberKey(num)
		var docs []*types.LegalDocument
		var err error
		if strings.Contains(num, "/") {
			docs, err = s.repo.FindByNumberKey(ctx, key, limit)
		} else {
			docs, err = s.bareNumber(ctx, key, rest, limit)
		}
		if err != nil {
			return nil, "", err
		}
		if len(docs) > 0 {
			return docs, "number", nil
		}
	}
	docs, err := s.repo.SearchByTitle(ctx, strings.Fields(query), limit)
	if err != nil || len(docs) == 0 {
		return docs, "", err
	}
	return docs, "title", nil
}

// bareNumber lists the documents a number without year or suffix fits —
// the document numbered exactly so (a local one) and every key/… — kept to
// those whose type or title fits the other words of the query, if any.
func (s *legalValidityLookupService) bareNumber(
	ctx context.Context, key string, rest []string, limit int,
) ([]*types.LegalDocument, error) {
	docs, err := s.repo.FindByNumberPrefix(ctx, key, legalLookupBareScan)
	if err != nil {
		return nil, err
	}
	exact, err := s.repo.FindByNumberKey(ctx, key, limit)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var all []*types.LegalDocument
	for _, d := range append(exact, docs...) {
		if d == nil || seen[d.ID] {
			continue
		}
		if d.NumberKey != key && !strings.HasPrefix(d.NumberKey, key+"/") {
			continue // the prefix fallback of FindByNumberKey
		}
		seen[d.ID] = true
		if legalLookupFits(d, rest) {
			all = append(all, d)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return legalDateAfter(all[i].IssuedOn, all[j].IssuedOn) })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// legalLookupFits reports whether the words around a bare number fit doc:
// they name its type ("Nghị định"), or its title holds each of them.
func legalLookupFits(doc *types.LegalDocument, rest []string) bool {
	if len(rest) == 0 {
		return true
	}
	words := strings.Join(rest, " ")
	if vietnamese_legal.DocTypeClass(words) == vietnamese_legal.DocTypeClass(doc.DocType) {
		return true
	}
	title := strings.ToLower(doc.Title + " " + doc.DocType)
	for _, w := range rest {
		if !strings.Contains(title, strings.ToLower(w)) {
			return false
		}
	}
	return true
}

// legalLookupNumberToken picks the token of query that holds a number:
// the first with a digit and a slash, else — when there is no such token —
// the first with a digit. rest are the other tokens, "số" dropped.
// Punctuation around a token is trimmed ("(30/2020/NĐ-CP),").
func legalLookupNumberToken(query string) (string, []string) {
	trim := func(s string) string {
		return strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	fields := strings.Fields(query)
	pick := -1
	for i, f := range fields {
		if hasDigit(f) && strings.Contains(f, "/") {
			pick = i
			break
		}
	}
	if pick < 0 {
		for i, f := range fields {
			if hasDigit(f) {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		return "", nil
	}
	var rest []string
	for i, f := range fields {
		if i == pick {
			continue
		}
		if t := trim(f); t != "" && !strings.EqualFold(t, "số") {
			rest = append(rest, t)
		}
	}
	return trim(fields[pick]), rest
}

func hasDigit(s string) bool {
	return strings.IndexFunc(s, unicode.IsDigit) >= 0
}

// readableCopies returns, per catalogue document, its copies in knowledge
// bases every signed-in person may read (platform public or published to
// all), which the lookup page links to. Errors only drop the links.
func (s *legalValidityLookupService) readableCopies(
	ctx context.Context, docs []*types.LegalDocument,
) map[string][]types.LegalKnowledgeRef {
	if s.reviews == nil || len(docs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	copies, err := s.reviews.KnowledgeForDocuments(ctx, ids)
	if err != nil {
		logger.Warnf(ctx, "[LegalLookup] knowledge copies unavailable: %v", err)
		return nil
	}
	var kbIDs []string
	seen := map[string]bool{}
	for _, refs := range copies {
		for _, r := range refs {
			if !seen[r.KnowledgeBaseID] {
				seen[r.KnowledgeBaseID] = true
				kbIDs = append(kbIDs, r.KnowledgeBaseID)
			}
		}
	}
	if len(kbIDs) == 0 || s.kbs == nil {
		return nil
	}
	kbs, err := s.kbs.GetKnowledgeBaseByIDs(ctx, kbIDs)
	if err != nil {
		logger.Warnf(ctx, "[LegalLookup] knowledge bases unavailable: %v", err)
		return nil
	}
	readable := map[string]bool{}
	for _, kb := range kbs {
		if kb != nil && kb.DeletedAt.Time.IsZero() && (access.IsPlatformPublicKB(kb) || kb.IsTenantPublished()) {
			readable[kb.ID] = true
		}
	}
	out := map[string][]types.LegalKnowledgeRef{}
	for docID, refs := range copies {
		for _, r := range refs {
			if readable[r.KnowledgeBaseID] {
				out[docID] = append(out[docID], r)
			}
		}
	}
	return out
}

// snapshotDate is the date of the catalogue's newest vbpl.vn snapshot,
// cached for a few minutes; "" when unknown.
func (s *legalValidityLookupService) snapshotDate(ctx context.Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.snapshotAt.IsZero() && s.now().Sub(s.snapshotAt) < legalLookupSnapshotTTL {
		return s.snapshot
	}
	latest, err := s.repo.LatestSnapshot(ctx)
	if err != nil {
		logger.Warnf(ctx, "[LegalLookup] snapshot date unavailable: %v", err)
		return s.snapshot
	}
	s.snapshot = types.LegalDateString(latest)
	s.snapshotAt = s.now()
	return s.snapshot
}

// legalValidityMatch renders one report for the lookup page; the status is
// reconciled with the links as the chat tool does (types.LegalDerivedExpiry).
func legalValidityMatch(rep *types.LegalStatusReport, now time.Time) types.LegalValidityMatch {
	doc := rep.Document
	m := types.LegalValidityMatch{
		LegalValidityRef: legalValidityRef(doc, ""),
		Issuer:           doc.Issuer,
		StatusText:       doc.Status,
		ExpiredOn:        types.LegalDateString(doc.ExpiredOn),
		Source:           doc.Source,
		SourceLabel:      types.LegalSourceLabel(doc.Source),
		SourceUpdatedOn:  types.LegalDateString(doc.SourceUpdatedAt),
		Unverified:       doc.Unverified(),
		ReplacedBy:       []types.LegalValidityRef{},
		EndedBy:          []types.LegalValidityRef{},
		AmendedBy:        []types.LegalValidityRef{},
	}
	if m.Unverified {
		m.UnverifiedNote = types.LegalUnverifiedNote(doc)
	}
	if by, on := types.LegalDerivedExpiry(rep, now); by != nil {
		rel := types.LegalDerivedExpiryRelation(rep, by)
		m.Derived = &types.LegalDerivedValidity{
			StatusText: types.LegalDerivedStatusText(doc),
			ExpiredOn:  types.LegalDateString(on),
			Relation:   rel,
			By:         legalValidityRef(by, rel),
		}
		if !types.LegalPartlyExpired(doc) {
			m.Status = types.LegalStatusKindExpired
		}
	}
	var links []types.LegalRelationLink
	for _, l := range rep.Links {
		if l.Incoming && l.Other != nil {
			links = append(links, l)
		}
	}
	sort.SliceStable(links, func(i, j int) bool {
		return legalDateAfter(links[i].Other.IssuedOn, links[j].Other.IssuedOn)
	})
	add := func(list *[]types.LegalValidityRef, total *int, l types.LegalRelationLink) {
		*total++
		if len(*list) < legalLookupMaxPerGroup {
			*list = append(*list, legalValidityRef(l.Other, l.Relation))
		}
	}
	for _, l := range links {
		switch l.Relation {
		case types.LegalRelationReplaces:
			add(&m.ReplacedBy, &m.ReplacedByTotal, l)
		case types.LegalRelationRepeals, types.LegalRelationExpires,
			types.LegalRelationPartiallyExpires, types.LegalRelationSuspends:
			add(&m.EndedBy, &m.EndedByTotal, l)
		case types.LegalRelationAmends, types.LegalRelationCorrects:
			add(&m.AmendedBy, &m.AmendedByTotal, l)
		}
	}
	return m
}

func legalValidityRef(doc *types.LegalDocument, rel types.LegalRelation) types.LegalValidityRef {
	return types.LegalValidityRef{
		ID:             doc.ID,
		DocumentNumber: doc.DocumentNumber,
		Title:          doc.Title,
		DocType:        doc.DocType,
		IssuedOn:       types.LegalDateString(doc.IssuedOn),
		EffectiveOn:    types.LegalDateString(doc.EffectiveOn),
		Status:         types.ClassifyLegalStatus(doc.Status),
		Relation:       rel,
	}
}

func legalDateAfter(a, b *time.Time) bool {
	if a == nil {
		return false
	}
	return b == nil || a.After(*b)
}
