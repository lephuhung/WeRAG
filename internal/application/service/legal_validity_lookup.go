package service

import (
	"context"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sync/singleflight"

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
	// legalLookupContentScan bounds the rows of a number and type that the
	// content words of the query are then matched against.
	legalLookupContentScan = 200
	// legalLookupSnapshotTTL is how long the catalogue's snapshot date is
	// cached (it changes once a night); a failed read is retried sooner.
	legalLookupSnapshotTTL      = 10 * time.Minute
	legalLookupSnapshotErrorTTL = 30 * time.Second
	// legalLookupMissMaxLen caps the stored query of a miss.
	legalLookupMissMaxLen = 200
	// legalLookupMissTimeout bounds the write of a miss.
	legalLookupMissTimeout = 3 * time.Second
	// Misses seen once are dropped after legalLookupMissStale; at most
	// legalLookupMissKeep rows are kept. The pruning runs on about one
	// write in legalLookupMissPruneEvery.
	legalLookupMissStale      = 30 * 24 * time.Hour
	legalLookupMissKeep       = 10000
	legalLookupMissPruneEvery = 100
	// legalLookupSnapshotTimeout bounds the snapshot query, which runs
	// detached from the request that started it (others wait on it).
	legalLookupSnapshotTimeout = 5 * time.Second
)

// legalLookupIncoming are the relations that bear on a match's validity:
// what other documents did to it.
var legalLookupIncoming = []types.LegalRelation{
	types.LegalRelationReplaces, types.LegalRelationRepeals, types.LegalRelationExpires,
	types.LegalRelationPartiallyExpires, types.LegalRelationSuspends,
	types.LegalRelationAmends, types.LegalRelationCorrects,
}

// legalLookupEnding are the incoming relations a related document's own
// status is reconciled with (types.LegalDerivedExpiry).
var legalLookupEnding = []types.LegalRelation{types.LegalRelationReplaces, types.LegalRelationExpires}

type legalValidityLookupService struct {
	repo    interfaces.LegalCatalogRepository
	reviews interfaces.LegalReviewRepository
	kbs     interfaces.KnowledgeBaseRepository
	misses  interfaces.LegalLookupMissRepository
	now     func() time.Time
	// recordMiss writes a miss off the request path (tests run it inline).
	recordMiss func(m *types.LegalLookupMiss)
	// prune reports whether this write also prunes the table.
	prune func() bool

	snapshotGroup singleflight.Group
	mu            sync.Mutex
	snapshot      string
	snapshotUntil time.Time
}

// NewLegalValidityLookupService creates the validity lookup service.
func NewLegalValidityLookupService(
	repo interfaces.LegalCatalogRepository,
	reviews interfaces.LegalReviewRepository,
	kbs interfaces.KnowledgeBaseRepository,
	misses interfaces.LegalLookupMissRepository,
) interfaces.LegalValidityLookupService {
	s := &legalValidityLookupService{repo: repo, reviews: reviews, kbs: kbs, misses: misses, now: time.Now}
	s.recordMiss = func(m *types.LegalLookupMiss) { go s.writeMiss(m) }
	s.prune = func() bool { return rand.IntN(legalLookupMissPruneEvery) == 0 }
	return s
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
	pq := parseLegalLookupQuery(query)
	docs, by, more, err := s.find(ctx, pq, limit)
	if err != nil {
		return nil, err
	}
	out.By, out.More = by, more
	switch {
	case pq.slashNumber && by != "number":
		out.NumberMissing = pq.number
	case pq.bare && len(pq.content) == 0 && by != "number":
		// "Nghị định 9999": named by number alone, and no such document
		out.NumberMissing = strings.Join(append(append([]string{}, pq.typeWords...), pq.number), " ")
	}
	switch {
	case len(docs) == 0:
		s.noteMiss(ctx, query, pq, types.LegalLookupMissNotFound)
	case out.NumberMissing != "":
		s.noteMiss(ctx, query, pq, types.LegalLookupMissNumber)
	}
	knowledge := s.readableCopies(ctx, docs)
	now := s.now()
	reports := make([]*types.LegalStatusReport, 0, len(docs))
	for _, doc := range docs {
		links, err := s.repo.ListLinks(ctx, doc.ID, nil, legalLookupIncoming)
		if err != nil {
			return nil, err
		}
		reports = append(reports, &types.LegalStatusReport{Document: doc, Links: links})
	}
	related := s.relatedStatus(ctx, reports, now)
	for _, rep := range reports {
		m := legalValidityMatch(rep, now, related)
		m.Knowledge = knowledge[rep.Document.ID]
		if m.Knowledge == nil {
			m.Knowledge = []types.LegalKnowledgeRef{}
		}
		out.Matches = append(out.Matches, m)
	}
	out.CatalogSnapshotDate = s.snapshotDate(ctx)
	return out, nil
}

// legalLookupQuery is a query split into what the catalogue can match.
type legalLookupQuery struct {
	// number is the token naming a document number; slashNumber when it
	// is a số hiệu ("30/2020/NĐ-CP", "53/QĐ-UBND") or number/year
	// ("30/2020"), bare when only digits ("53").
	number      string
	slashNumber bool
	bare        bool
	// typeWords are the doc type phrase the query starts with ("Nghị
	// định"), docTypes the catalogue spellings of its class.
	typeWords []string
	docTypes  []string
	// content are the remaining words, stop words dropped; year a four-
	// digit year among them (taken out of content).
	content []string
	year    int
	// words are every token, punctuation trimmed (the last-resort title
	// search).
	words []string
}

// legalLookupDocTypes are the doc type phrases a query may start with,
// longest first so "nghị quyết liên tịch" wins over "nghị quyết".
var legalLookupDocTypes = []string{
	"thông tư liên tịch", "nghị quyết liên tịch", "văn bản hợp nhất",
	"bộ luật", "pháp lệnh", "nghị định", "nghị quyết", "thông tư", "quyết định", "chỉ thị",
	"hiến pháp", "công văn", "kế hoạch", "thông báo", "sắc lệnh", "sắc luật", "luật", "lệnh",
}

// legalLookupDocTypeSpellings lists the catalogue doc types of a class
// (vietnamese_legal.DocTypeClass folds "bộ luật" into "luật", …).
var legalLookupDocTypeSpellings = map[string][]string{
	"luật":       {"luật", "bộ luật"},
	"thông tư":   {"thông tư", "thông tư liên tịch"},
	"nghị quyết": {"nghị quyết", "nghị quyết liên tịch"},
}

// legalLookupStopWords carry no topic: a title need not hold them.
var legalLookupStopWords = map[string]bool{
	"về": true, "việc": true, "của": true, "và": true, "các": true, "những": true, "cho": true,
	"với": true, "theo": true, "trong": true, "tại": true, "số": true, "năm": true, "ngày": true,
	"là": true, "được": true, "một": true, "này": true, "đó": true, "đối": true,
}

// legalLookupStopPairs are two-word stop phrases.
var legalLookupStopPairs = map[string]bool{"quy định": true, "ban hành": true, "hướng dẫn": true}

func trimLookupToken(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// lookupNumberShape classifies a token: 2 = a số hiệu (letters after the
// last slash, "30/2020/NĐ-CP"), 1 = number/year ("30/2020"), 0 = neither
// (a date "5/3/2020", a word).
func lookupNumberShape(tok string) int {
	i := strings.LastIndex(tok, "/")
	if i <= 0 || !hasDigit(tok[:i]) {
		return 0
	}
	if tail := tok[i+1:]; strings.IndexFunc(tail, unicode.IsLetter) >= 0 {
		return 2
	}
	parts := strings.Split(tok, "/")
	if len(parts) == 2 && isDigits(parts[0]) && len(parts[1]) == 4 && isDigits(parts[1]) {
		return 1
	}
	return 0
}

func isDigits(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return !unicode.IsDigit(r) }) < 0
}

func hasDigit(s string) bool {
	return strings.IndexFunc(s, unicode.IsDigit) >= 0
}

// parseLegalLookupQuery splits query: the number token (a số hiệu
// preferred over number/year, then digits; a date is never one), a
// leading doc type phrase, and the content words.
func parseLegalLookupQuery(query string) legalLookupQuery {
	var pq legalLookupQuery
	var toks []string
	for _, f := range strings.Fields(query) {
		if t := trimLookupToken(f); t != "" {
			toks = append(toks, t)
		}
	}
	pq.words = toks
	pick, best := -1, 0
	for i, t := range toks {
		if shape := lookupNumberShape(t); shape > best {
			pick, best = i, shape
		}
	}
	if pick >= 0 {
		pq.number, pq.slashNumber = toks[pick], true
	}
	rest := make([]string, 0, len(toks))
	for i, t := range toks {
		if i != pick {
			rest = append(rest, t)
		}
	}
	// leading doc type phrase ("Nghị định", "số" before or after it)
	lower := make([]string, len(rest))
	for i, w := range rest {
		lower[i] = strings.ToLower(w)
	}
	start := 0
	for start < len(lower) && lower[start] == "số" {
		start++
	}
	for _, dt := range legalLookupDocTypes {
		n := len(strings.Fields(dt))
		if start+n <= len(lower) && strings.Join(lower[start:start+n], " ") == dt {
			pq.typeWords = rest[start : start+n]
			class := vietnamese_legal.DocTypeClass(dt)
			if sp, ok := legalLookupDocTypeSpellings[class]; ok {
				pq.docTypes = sp
			} else {
				pq.docTypes = []string{class}
			}
			start += n
			break
		}
	}
	rest, lower = rest[start:], lower[start:]
	// a bare number: digits, when no số hiệu was found
	if pick < 0 {
		for i, w := range rest {
			if isDigits(w) {
				pq.number, pq.bare = w, true
				rest = append(append([]string{}, rest[:i]...), rest[i+1:]...)
				lower = append(append([]string{}, lower[:i]...), lower[i+1:]...)
				break
			}
		}
	}
	for i := 0; i < len(rest); i++ {
		if i+1 < len(lower) && legalLookupStopPairs[lower[i]+" "+lower[i+1]] {
			i++
			continue
		}
		if legalLookupStopWords[lower[i]] {
			continue
		}
		if y, err := strconv.Atoi(rest[i]); err == nil && len(rest[i]) == 4 && y >= 1945 && y <= 2100 && pq.year == 0 {
			pq.year = y
			continue
		}
		pq.content = append(pq.content, rest[i])
	}
	// a bare number with words that are no doc type ("chương trình 135",
	// "Luật Đất đai 2024" keeps the type) is part of a title
	if pq.bare && len(pq.typeWords) == 0 && len(pq.content) > 0 {
		if y, _ := strconv.Atoi(pq.number); len(pq.number) == 4 && y >= 1945 && y <= 2100 && pq.year == 0 {
			pq.year = y
		} else {
			pq.content = append(pq.content, pq.number)
		}
		pq.number, pq.bare = "", false
	}
	// "Luật Đất đai 2024": the digits were a year, not a number
	if pq.bare && len(pq.content) > 0 && pq.year == 0 {
		if y, _ := strconv.Atoi(pq.number); len(pq.number) == 4 && y >= 1945 && y <= 2100 {
			pq.year = y
		}
	}
	return pq
}

// find resolves the query to at most limit documents, newest first:
//   - a số hiệu or number/year by number key — every catalogue row of it,
//     since số hiệu is not unique;
//   - a bare number ("53", "Nghị định 53", "Nghị định 100 về xử phạt giao
//     thông") by number prefix, of the doc type when one leads the query,
//     kept to titles holding most content words when there are any;
//   - otherwise, or when that finds nothing, by title: the content words
//     (stop words dropped), of the doc type and year when given; last, every
//     word of the query in the title.
//
// by says which matched ("number", "title", ""); more that there are
// further matches.
func (s *legalValidityLookupService) find(
	ctx context.Context, pq legalLookupQuery, limit int,
) ([]*types.LegalDocument, string, bool, error) {
	if pq.number != "" {
		docs, more, err := s.byNumber(ctx, pq, limit)
		if err != nil {
			return nil, "", false, err
		}
		if len(docs) > 0 {
			return docs, "number", more, nil
		}
		if pq.bare && len(pq.content) == 0 {
			// a number alone names one document: titles that merely hold
			// the digits would be unrelated
			return nil, "", false, nil
		}
	}
	for _, f := range s.titleFilters(pq) {
		f.Limit = limit + 1
		docs, err := s.repo.FilterDocuments(ctx, f)
		if err != nil {
			return nil, "", false, err
		}
		if len(docs) > 0 {
			rankTitleMatches(docs, pq)
			docs, more := cutLookup(docs, limit)
			return docs, "title", more, nil
		}
	}
	return nil, "", false, nil
}

// rankTitleMatches moves first, keeping the newest-first order otherwise,
// the documents whose title is (or starts with) what the query names —
// "Luật Đất đai 2024" puts the Luật Đất đai before the laws amending it.
func rankTitleMatches(docs []*types.LegalDocument, pq legalLookupQuery) {
	phrase := strings.ToLower(strings.Join(append(append([]string{}, pq.typeWords...), pq.content...), " "))
	if phrase == "" {
		return
	}
	sort.SliceStable(docs, func(i, j int) bool {
		return titleNames(docs[i].Title, phrase) && !titleNames(docs[j].Title, phrase)
	})
}

func titleNames(title, phrase string) bool {
	return strings.HasPrefix(strings.ToLower(strings.Join(strings.Fields(title), " ")), phrase)
}

// titleFilters are the title searches tried in turn.
func (s *legalValidityLookupService) titleFilters(pq legalLookupQuery) []types.LegalCatalogFilter {
	var out []types.LegalCatalogFilter
	words := append([]string{}, pq.content...)
	if pq.slashNumber {
		// documents mentioning the number the catalogue lacks
		words = append(words, pq.number)
	}
	if len(words) > 0 || pq.year > 0 {
		out = append(out, types.LegalCatalogFilter{TitleWords: words, DocTypes: pq.docTypes, IssuedYear: pq.year})
	}
	if len(pq.words) > 0 {
		out = append(out, types.LegalCatalogFilter{TitleWords: pq.words})
	}
	return out
}

// byNumber looks a number token up (see find).
func (s *legalValidityLookupService) byNumber(
	ctx context.Context, pq legalLookupQuery, limit int,
) ([]*types.LegalDocument, bool, error) {
	key := vietnamese_legal.CatalogNumberKey(pq.number)
	if key == "" {
		return nil, false, nil
	}
	if pq.slashNumber {
		docs, err := s.repo.FindByNumberKey(ctx, key, limit+1)
		if err != nil {
			return nil, false, err
		}
		docs, more := cutLookup(docs, limit)
		return docs, more, nil
	}
	scan := limit + 1
	if len(pq.content) > 0 {
		scan = legalLookupContentScan
	}
	// "Nghị định 13 2023": the year narrows the number
	prefixed, err := s.repo.FilterDocuments(ctx, types.LegalCatalogFilter{
		NumberPrefix: key, DocTypes: pq.docTypes, IssuedYear: pq.year, Limit: scan,
	})
	if err != nil {
		return nil, false, err
	}
	exact, err := s.repo.FilterDocuments(ctx, types.LegalCatalogFilter{
		NumberKey: key, DocTypes: pq.docTypes, IssuedYear: pq.year, Limit: scan,
	})
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	var all []*types.LegalDocument
	for _, d := range append(exact, prefixed...) {
		if d == nil || seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		if legalLookupTitleFits(d, pq.content) {
			all = append(all, d)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return legalDateAfter(all[i].IssuedOn, all[j].IssuedOn) })
	docs, more := cutLookup(all, limit)
	return docs, more || len(prefixed) == scan || len(exact) == scan, nil
}

// legalLookupTitleFits reports whether doc's title holds most (at least
// half, rounded up) of the content words; true without any.
func legalLookupTitleFits(doc *types.LegalDocument, content []string) bool {
	if len(content) == 0 {
		return true
	}
	title := strings.ToLower(doc.Title)
	hits := 0
	for _, w := range content {
		if strings.Contains(title, strings.ToLower(w)) {
			hits++
		}
	}
	return hits*2 >= len(content)
}

func cutLookup(docs []*types.LegalDocument, limit int) ([]*types.LegalDocument, bool) {
	if len(docs) > limit {
		return docs[:limit], true
	}
	return docs, false
}

// noteMiss records a lookup that found nothing (or only documents
// mentioning a missing số hiệu). Never fails the lookup.
func (s *legalValidityLookupService) noteMiss(ctx context.Context, query string, pq legalLookupQuery, reason string) {
	if s.misses == nil || s.recordMiss == nil {
		return
	}
	norm := capRunes(strings.ToLower(query), legalLookupMissMaxLen)
	if norm == "" {
		return
	}
	caller := types.CallerFromContext(ctx)
	if access.IsExplicitHumanSuperAdmin(ctx, caller) {
		return // re-checking a gap ("Tra lại") must not count as a user's miss
	}
	now := s.now()
	number := ""
	if pq.slashNumber {
		number = capRunes(pq.number, 128)
	}
	s.recordMiss(&types.LegalLookupMiss{
		Query: norm, Sample: capRunes(query, legalLookupMissMaxLen), Number: number, Reason: reason,
		Count: 1, FirstAt: now, LastAt: now, LastTenantID: caller.TenantID, LastUserID: capRunes(caller.UserID, 36),
	})
}

func (s *legalValidityLookupService) writeMiss(m *types.LegalLookupMiss) {
	ctx, cancel := context.WithTimeout(context.Background(), legalLookupMissTimeout)
	defer cancel()
	if err := s.misses.Record(ctx, m); err != nil {
		logger.Warnf(ctx, "[LegalLookup] miss %q not recorded: %v", m.Query, err)
		return
	}
	if s.prune != nil && s.prune() {
		if err := s.misses.Prune(ctx, s.now().Add(-legalLookupMissStale), legalLookupMissKeep); err != nil {
			logger.Warnf(ctx, "[LegalLookup] misses not pruned: %v", err)
		}
	}
}

// readableCopies returns, per catalogue document, the copies the caller
// may open, in the knowledge bases the caller may read (the same listing
// and rule as the knowledge base list and default retrieval scope:
// ListReadableKnowledgeBases + access.InDefaultRetrievalScope): copies with
// an approved legal link in any of them, and vbpl.vn imports (metadata
// source + external_id, which an uploader can set) only in the caller's own
// organization's KBs. A guest therefore sees approved copies only. Errors
// only drop the links.
func (s *legalValidityLookupService) readableCopies(
	ctx context.Context, docs []*types.LegalDocument,
) map[string][]types.LegalKnowledgeRef {
	out := map[string][]types.LegalKnowledgeRef{}
	if len(docs) == 0 || s.kbs == nil {
		return out
	}
	caller := types.CallerFromContext(ctx)
	kbs, err := s.kbs.ListReadableKnowledgeBases(ctx, access.KBReaderFor(ctx, caller))
	if err != nil {
		logger.Warnf(ctx, "[LegalLookup] readable knowledge bases unavailable: %v", err)
		return out
	}
	readable := map[string]bool{}
	var ownKBIDs []string
	for _, kb := range kbs {
		if kb == nil || !access.InDefaultRetrievalScope(caller, kb) || readable[kb.ID] {
			continue
		}
		readable[kb.ID] = true
		// upload metadata (external_id) is nobody's review: trusted only in
		// the caller's own organization; elsewhere an approved link is needed
		if caller.TenantID != 0 && kb.OwnerTenantID == caller.TenantID {
			ownKBIDs = append(ownKBIDs, kb.ID)
		}
	}
	if len(readable) == 0 {
		return out
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	seen := map[string]bool{}
	add := func(docID string, r types.LegalKnowledgeRef) {
		if readable[r.KnowledgeBaseID] && !seen[docID+"\x00"+r.KnowledgeID] {
			seen[docID+"\x00"+r.KnowledgeID] = true
			out[docID] = append(out[docID], r)
		}
	}
	if s.reviews != nil {
		if linked, err := s.reviews.KnowledgeForDocuments(ctx, ids); err != nil {
			logger.Warnf(ctx, "[LegalLookup] linked copies unavailable: %v", err)
		} else {
			for docID, refs := range linked {
				for _, r := range refs {
					add(docID, r)
				}
			}
		}
	}
	if imported, err := s.repo.ExternalCopies(ctx, ids, ownKBIDs); err != nil {
		logger.Warnf(ctx, "[LegalLookup] imported copies unavailable: %v", err)
	} else {
		for docID, refs := range imported {
			for _, r := range refs {
				add(docID, r)
			}
		}
	}
	return out
}

// relatedStatus reconciles the status of the documents acting on the
// matches as the matches' own (types.LegalDerivedExpiry), in two queries:
// returns the kind to show per related document ID, for those it changes.
func (s *legalValidityLookupService) relatedStatus(
	ctx context.Context, reports []*types.LegalStatusReport, now time.Time,
) map[string]types.LegalStatusKind {
	others := map[string]*types.LegalDocument{}
	var ids []string
	for _, rep := range reports {
		for _, l := range rep.Links {
			o := l.Other
			if o == nil || others[o.ID] != nil || o.ExpiredOn != nil || !types.LegalDerivableStatus(o.Status) {
				continue
			}
			others[o.ID] = o
			ids = append(ids, o.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	edges, err := s.repo.ListEdges(ctx, ids, legalLookupEnding, true)
	if err != nil || len(edges) == 0 {
		return nil
	}
	var fromIDs []string
	for _, e := range edges {
		fromIDs = append(fromIDs, e.FromID)
	}
	enders, err := s.repo.GetByIDs(ctx, fromIDs)
	if err != nil {
		return nil
	}
	byID := map[string]*types.LegalDocument{}
	for _, d := range enders {
		byID[d.ID] = d
	}
	links := map[string][]types.LegalRelationLink{}
	for _, e := range edges {
		if d := byID[e.FromID]; d != nil {
			links[e.ToID] = append(links[e.ToID], types.LegalRelationLink{Relation: e.Relation, Incoming: true, Other: d})
		}
	}
	out := map[string]types.LegalStatusKind{}
	for id, ls := range links {
		o := others[id]
		if by, _ := types.LegalDerivedExpiry(&types.LegalStatusReport{Document: o, Links: ls}, now); by != nil && !types.LegalPartlyExpired(o) {
			out[id] = types.LegalStatusKindExpired
		}
	}
	return out
}

// snapshotDate is the date of the catalogue's newest vbpl.vn snapshot,
// cached for a few minutes (a failure for less, keeping the last value);
// concurrent misses share one query. "" when unknown.
func (s *legalValidityLookupService) snapshotDate(ctx context.Context) string {
	s.mu.Lock()
	if s.now().Before(s.snapshotUntil) {
		v := s.snapshot
		s.mu.Unlock()
		return v
	}
	s.mu.Unlock()
	v, _, _ := s.snapshotGroup.Do("snapshot", func() (interface{}, error) {
		// not the first caller's ctx: its cancellation must not fail the
		// others waiting on this query
		qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), legalLookupSnapshotTimeout)
		defer cancel()
		latest, err := s.repo.LatestSnapshot(qctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			logger.Warnf(ctx, "[LegalLookup] snapshot date unavailable: %v", err)
			s.snapshotUntil = s.now().Add(legalLookupSnapshotErrorTTL)
			return s.snapshot, nil
		}
		s.snapshot = types.LegalDateString(latest)
		s.snapshotUntil = s.now().Add(legalLookupSnapshotTTL)
		return s.snapshot, nil
	})
	str, _ := v.(string)
	return str
}

// legalValidityMatch renders one report for the lookup page; the status is
// reconciled with the links as the chat tool does (types.LegalDerivedExpiry),
// and related documents get the kinds relatedStatus derived.
func legalValidityMatch(
	rep *types.LegalStatusReport, now time.Time, related map[string]types.LegalStatusKind,
) types.LegalValidityMatch {
	doc := rep.Document
	m := types.LegalValidityMatch{
		LegalValidityRef: legalValidityRef(doc, "", nil),
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
			By:         legalValidityRef(by, rel, related),
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
			*list = append(*list, legalValidityRef(l.Other, l.Relation, related))
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

func legalValidityRef(
	doc *types.LegalDocument, rel types.LegalRelation, related map[string]types.LegalStatusKind,
) types.LegalValidityRef {
	status := types.ClassifyLegalStatus(doc.Status)
	if k, ok := related[doc.ID]; ok {
		status = k
	}
	return types.LegalValidityRef{
		ID:             doc.ID,
		DocumentNumber: doc.DocumentNumber,
		Title:          doc.Title,
		DocType:        doc.DocType,
		IssuedOn:       types.LegalDateString(doc.IssuedOn),
		EffectiveOn:    types.LegalDateString(doc.EffectiveOn),
		Status:         status,
		Relation:       rel,
	}
}

func legalDateAfter(a, b *time.Time) bool {
	if a == nil {
		return false
	}
	return b == nil || a.After(*b)
}

type legalLookupMissService struct {
	repo interfaces.LegalLookupMissRepository
}

// NewLegalLookupMissService creates the SuperAdmin service of lookup misses.
func NewLegalLookupMissService(repo interfaces.LegalLookupMissRepository) interfaces.LegalLookupMissService {
	return &legalLookupMissService{repo: repo}
}

func (s *legalLookupMissService) ListMisses(
	ctx context.Context, byCount bool, offset, limit int,
) ([]*types.LegalLookupMiss, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.List(ctx, byCount, offset, limit)
}

func (s *legalLookupMissService) DeleteMiss(ctx context.Context, id uint64) error {
	return s.repo.Delete(ctx, id)
}
