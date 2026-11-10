package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"golang.org/x/text/unicode/norm"
)

// The keyword index splits a số hiệu into its parts ("15", "2026", "nđ",
// "cp"), so a document with a small number drowns among the documents that
// merely contain those words, and the vectors cannot tell Điều 1 from
// Điều 10. When the question names a document by its full số hiệu, its
// copy in the searched bases is resolved through the catalogue and its
// chunks are kept in the result: the named articles' chunks, else its best
// retrieved chunk. The amendment resolver downstream only reads documents
// that are among the results.
const (
	namedLegalMaxNumbers  = 3
	namedLegalMaxArticles = 3
	// namedLegalMaxDocs: a number shared by more documents than this (a
	// QĐ-UBND of many provinces) is not pinned: the wrong one would be cited.
	namedLegalMaxDocs = 2
	// namedLegalPinBudget bounds the kept chunks; they stay among the rows
	// shown in full (defaultSearchFullContentTop).
	namedLegalPinBudget = 4
	// namedLegalPerDocCandidates bounds a named document's rows entering rerank.
	namedLegalPerDocCandidates = 5
)

// namedLegalDoc is a named document whose copy the search covers.
type namedLegalDoc struct {
	number          string
	knowledgeID     string
	knowledgeBaseID string
}

// namedLegalMissing is a named document whose text the rows lack.
type namedLegalMissing struct {
	key, note string
}

// namedLegalPlan is what the named numbers resolved to.
type namedLegalPlan struct {
	docs     []namedLegalDoc
	articles []string
	targets  types.SearchTargets
	// notes always reach the model (an ambiguous number); missing ones only
	// when the search covered whole bases and no row holds that number.
	notes     []string
	missing   []namedLegalMissing
	wholeOnly bool
}

func (p *namedLegalPlan) holds(knowledgeID string) bool {
	for _, d := range p.docs {
		if d.knowledgeID == knowledgeID {
			return true
		}
	}
	return false
}

// namedLegalNumbers returns the normative số hiệu the texts name, each once,
// with the document type written before it ("Nghị định số 15/2026/NĐ-CP").
func namedLegalNumbers(texts []string) (numbers []string, docTypes map[string]string) {
	seen := map[string]bool{}
	docTypes = map[string]string{}
	for _, text := range texts {
		nfc := norm.NFC.String(text)
		for _, n := range vietnamese_legal.FindDocumentNumbers(nfc) {
			key := vietnamese_legal.CatalogNumberKey(n)
			if seen[key] || !vietnamese_legal.IsNormativeDocumentNumber(n) {
				continue
			}
			seen[key] = true
			numbers = append(numbers, n)
			if i := strings.Index(nfc, n); i > 0 {
				if t := LegalDocTypeBefore(nfc[:i]); t != "" {
					docTypes[key] = t
				}
			}
			if len(numbers) == namedLegalMaxNumbers {
				return numbers, docTypes
			}
		}
	}
	return numbers, docTypes
}

// planNamedLegal resolves the numbers the texts name to the copies the
// search covers. Only bases the search covers whole are used: a search
// narrowed to files or tags is not widened.
func (t *SearchKnowledgeTool) planNamedLegal(
	ctx context.Context, texts []string, searchTargets types.SearchTargets,
) *namedLegalPlan {
	numbers, docTypes := namedLegalNumbers(texts)
	if len(numbers) == 0 {
		return nil
	}
	wholeKB := map[string]*types.SearchTarget{}
	var kbIDs []string
	wholeOnly := true
	for _, st := range searchTargets {
		if st == nil {
			continue
		}
		if st.Type != types.SearchTargetTypeKnowledgeBase || len(st.TagIDs) > 0 {
			wholeOnly = false
			continue
		}
		if wholeKB[st.KnowledgeBaseID] == nil {
			wholeKB[st.KnowledgeBaseID] = st
			kbIDs = append(kbIDs, st.KnowledgeBaseID)
		}
	}
	copies, err := t.legalCatalog.NamedCopies(ctx, numbers, kbIDs)
	if err != nil {
		logger.Warnf(ctx, "[Tool][SearchKnowledge] Named legal documents %v: %v", numbers, err)
		return nil
	}
	byKey := map[string][]*types.LegalDocument{}
	copyOf := map[string]types.LegalNamedCopy{}
	for _, c := range copies {
		if c.Document == nil {
			continue
		}
		if _, ok := copyOf[c.Document.ID]; !ok {
			byKey[c.Document.NumberKey] = append(byKey[c.Document.NumberKey], c.Document)
			copyOf[c.Document.ID] = c
		} else if copyOf[c.Document.ID].KnowledgeID == "" && c.KnowledgeID != "" {
			copyOf[c.Document.ID] = c
		}
	}
	plan := &namedLegalPlan{wholeOnly: wholeOnly}
	for _, n := range numbers {
		key := vietnamese_legal.CatalogNumberKey(n)
		docs := byKey[key]
		if want := docTypes[key]; want != "" {
			var typed []*types.LegalDocument
			for _, d := range docs {
				if strings.EqualFold(strings.TrimSpace(d.DocType), want) {
					typed = append(typed, d)
				}
			}
			if len(typed) > 0 {
				docs = typed
			}
		}
		var live []*types.LegalDocument
		for _, d := range docs {
			if !strings.HasPrefix(strings.TrimSpace(d.Status), legalExpiredStatus) {
				live = append(live, d)
			}
		}
		if len(live) > 0 {
			docs = live
		}
		switch {
		case len(docs) == 0:
			continue // not catalogued: the legal request gate speaks for it
		case len(docs) > namedLegalMaxDocs:
			plan.notes = append(plan.notes, fmt.Sprintf("- %s: danh mục có %d văn bản cùng số hiệu này; "+
				"chưa xác định được văn bản nào (cần thêm loại văn bản, cơ quan ban hành hoặc năm).", n, len(docs)))
			continue
		}
		for _, d := range docs {
			c := copyOf[d.ID]
			if c.KnowledgeID == "" {
				reason := "kho đang tìm không có toàn văn văn bản này"
				if strings.HasPrefix(strings.TrimSpace(d.Status), legalExpiredStatus) {
					reason = "đã hết hiệu lực toàn bộ; toàn văn không còn được giữ trong kho"
				}
				plan.missing = append(plan.missing, namedLegalMissing{key: key, note: fmt.Sprintf("- %s (%s): %s.",
					legalDocLabel(d), legalOr(d.Status, "chưa rõ tình trạng"), reason)})
				continue
			}
			plan.docs = append(plan.docs, namedLegalDoc{number: d.DocumentNumber, knowledgeID: c.KnowledgeID,
				knowledgeBaseID: c.KnowledgeBaseID})
		}
	}
	for _, text := range texts {
		for _, a := range vietnamese_legal.FindArticleNumbers(text) {
			if len(plan.articles) < namedLegalMaxArticles && !containsString(plan.articles, a) {
				plan.articles = append(plan.articles, a)
			}
		}
	}
	byKB := map[string][]string{}
	var order []string
	for _, d := range plan.docs {
		if _, ok := byKB[d.knowledgeBaseID]; !ok {
			order = append(order, d.knowledgeBaseID)
		}
		byKB[d.knowledgeBaseID] = append(byKB[d.knowledgeBaseID], d.knowledgeID)
	}
	for _, kbID := range order {
		plan.targets = append(plan.targets, &types.SearchTarget{Type: types.SearchTargetTypeKnowledge,
			KnowledgeBaseID: kbID, TenantID: wholeKB[kbID].TenantID, KnowledgeIDs: byKB[kbID]})
	}
	if len(plan.docs) == 0 && len(plan.notes) == 0 && len(plan.missing) == 0 {
		return nil
	}
	return plan
}

// capCandidates keeps a named document's best rows only, so its scoped
// search does not flood the rerank call.
func (p *namedLegalPlan) capCandidates(rows []*searchResultWithMeta) []*searchResultWithMeta {
	if p == nil || len(p.docs) == 0 {
		return rows
	}
	var named []*searchResultWithMeta
	seenChunk := map[string]bool{}
	for _, r := range rows {
		if r != nil && r.SearchResult != nil && p.holds(r.KnowledgeID) && !seenChunk[r.ID] {
			seenChunk[r.ID] = true
			named = append(named, r)
		}
	}
	sort.SliceStable(named, func(i, j int) bool { return named[i].Score > named[j].Score })
	keep := map[*searchResultWithMeta]bool{}
	count := map[string]int{}
	for _, r := range named {
		if count[r.KnowledgeID] < namedLegalPerDocCandidates {
			count[r.KnowledgeID]++
			keep[r] = true
		}
	}
	out := rows[:0:0]
	for _, r := range rows {
		if r == nil || r.SearchResult == nil || !p.holds(r.KnowledgeID) || keep[r] {
			out = append(out, r)
		}
	}
	return out
}

// pinNamedLegal puts the named documents' chunks first in final: one per
// document before a second one, the named articles' chunks before the best
// retrieved one, at most namedLegalPinBudget. They skip the relevance
// threshold and MMR, which judge "is this the answer", not "is this the
// document asked about".
func (t *SearchKnowledgeTool) pinNamedLegal(
	ctx context.Context, p *namedLegalPlan, candidates, final []*searchResultWithMeta, limit int,
	shownBefore map[string]bool,
) ([]*searchResultWithMeta, []string) {
	if p == nil || len(p.docs) == 0 {
		return final, nil
	}
	budget := namedLegalPinBudget
	if limit < budget {
		budget = limit
	}
	// a retrieved row of the same document carries its validity annotation
	// (AnnotateSearchResults); an article fetched by id takes it from there
	retrieved := map[string]*searchResultWithMeta{}
	for _, r := range candidates {
		if r != nil && r.SearchResult != nil && retrieved[r.KnowledgeID] == nil {
			retrieved[r.KnowledgeID] = r
		}
	}
	perDoc := map[string][]*searchResultWithMeta{}
	if len(p.articles) > 0 && t.chunkService != nil && t.knowledgeService != nil {
		ids := make([]string, 0, len(p.docs))
		for _, d := range p.docs {
			ids = append(ids, d.knowledgeID)
		}
		found, err := t.legalCatalog.ArticleChunkIDs(ctx, ids, p.articles, 1)
		if err != nil {
			logger.Warnf(ctx, "[Tool][SearchKnowledge] Named article chunks: %v", err)
		}
		var chunkIDs []string
		for _, d := range p.docs {
			for _, a := range p.articles {
				chunkIDs = append(chunkIDs, found[d.knowledgeID+"\x00"+a]...)
			}
		}
		if len(chunkIDs) > 0 {
			chunks, err := t.chunkService.GetRepository().ListChunksByIDOnly(ctx, chunkIDs)
			if err != nil {
				logger.Warnf(ctx, "[Tool][SearchKnowledge] Named article chunks: %v", err)
			}
			byID := map[string]*types.Chunk{}
			for _, c := range chunks {
				byID[c.ID] = c
			}
			knowledge := map[string]*types.Knowledge{}
			for _, id := range chunkIDs {
				c := byID[id]
				if c == nil {
					continue
				}
				k, ok := knowledge[c.KnowledgeID]
				if !ok {
					k, _ = t.knowledgeService.GetKnowledgeByIDOnly(ctx, c.KnowledgeID)
					knowledge[c.KnowledgeID] = k
				}
				if k == nil {
					continue
				}
				row := namedChunkResult(c, k)
				if got := retrieved[c.KnowledgeID]; got != nil {
					row.KnowledgeCustomMetadata, row.LegalSuperseded = got.KnowledgeCustomMetadata, got.LegalSuperseded
				}
				perDoc[c.KnowledgeID] = append(perDoc[c.KnowledgeID], &searchResultWithMeta{
					SearchResult:      row,
					QueryType:         "named_document",
					KnowledgeBaseID:   k.KnowledgeBaseID,
					KnowledgeBaseType: types.KnowledgeBaseTypeDocument,
				})
			}
		}
	}
	// no article chunk: the document's best retrieved row
	for _, d := range p.docs {
		if len(perDoc[d.knowledgeID]) > 0 {
			continue
		}
		var best *searchResultWithMeta
		for _, r := range candidates {
			if r != nil && r.SearchResult != nil && r.KnowledgeID == d.knowledgeID && (best == nil || r.Score > best.Score) {
				best = r
			}
		}
		if best != nil {
			perDoc[d.knowledgeID] = []*searchResultWithMeta{best}
		}
	}
	var pinned []*searchResultWithMeta
	seen := map[string]bool{}
	add := func(r *searchResultWithMeta, number string) {
		if len(pinned) >= budget || seen[r.ID] || shownBefore[r.ID] {
			return
		}
		seen[r.ID] = true
		note := "Văn bản câu hỏi nêu đích danh (số hiệu " + number + ")"
		if r.KnowledgeCustomMetadata != "" && !strings.HasPrefix(r.KnowledgeCustomMetadata, "Văn bản câu hỏi nêu đích danh") {
			note += "\n" + r.KnowledgeCustomMetadata
		}
		r.KnowledgeCustomMetadata = note
		pinned = append(pinned, r)
	}
	for round := 0; ; round++ {
		added := false
		for _, d := range p.docs {
			if rows := perDoc[d.knowledgeID]; round < len(rows) {
				add(rows[round], d.number)
				added = true
			}
		}
		if !added || len(pinned) >= budget {
			break
		}
	}
	if len(pinned) == 0 {
		return final, nil
	}
	out := append([]*searchResultWithMeta{}, pinned...)
	ids := make([]string, 0, len(pinned))
	for _, r := range pinned {
		ids = append(ids, r.ID)
	}
	for _, r := range final {
		if len(out) >= limit {
			break
		}
		if r == nil || r.SearchResult == nil || seen[r.ID] {
			continue
		}
		out = append(out, r)
	}
	logger.Infof(ctx, "[Tool][SearchKnowledge] Kept %d chunk(s) of %d named document(s) (articles %v)",
		len(pinned), len(p.docs), p.articles)
	return out, ids
}

// namedChunkResult is a chunk fetched by its article, shaped as a search row.
func namedChunkResult(c *types.Chunk, k *types.Knowledge) *types.SearchResult {
	return &types.SearchResult{
		ID: c.ID, Content: c.Content, ContentRevision: c.ContentRevision, KnowledgeID: c.KnowledgeID,
		ChunkIndex: c.ChunkIndex, KnowledgeTitle: k.Title, StartAt: c.StartAt, EndAt: c.EndAt, Seq: c.ChunkIndex,
		Score: 1, MatchType: types.MatchTypeKeywords, Metadata: k.GetMetadata(), ChunkType: string(c.ChunkType),
		ParentChunkID: c.ParentChunkID, ImageInfo: c.ImageInfo, KnowledgeFilename: k.FileName,
		KnowledgeSource: k.Source, KnowledgeChannel: k.Channel, KnowledgeDescription: k.Description,
		KnowledgeCustomMetadata: k.CustomMetadataText(), ChunkMetadata: c.Metadata, KnowledgeBaseID: k.KnowledgeBaseID,
	}
}

// namedLegalNote is the result note on named documents not kept. "Not
// held" is said only when the search covered whole bases and no row carries
// that number: a narrowed search, or a tenant's own copy without a
// catalogue id, may hold the document all the same.
func namedLegalNote(p *namedLegalPlan, rows []*searchResultWithMeta) string {
	if p == nil {
		return ""
	}
	lines := append([]string{}, p.notes...)
	if p.wholeOnly {
		inRows := map[string]bool{}
		for _, r := range rows {
			if r != nil && r.SearchResult != nil {
				if n := r.Metadata["doc_number"]; n != "" {
					inRows[vietnamese_legal.CatalogNumberKey(n)] = true
				}
			}
		}
		for _, m := range p.missing {
			if !inRows[m.key] {
				lines = append(lines, m.note)
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Văn bản câu hỏi nêu đích danh nhưng không có trong kết quả:\n" + strings.Join(lines, "\n")
}
