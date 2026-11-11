package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

const (
	// sectionSearchDocCap bounds how many documents are scanned when the call
	// names none: every document in scope is loaded chunk by chunk.
	sectionSearchDocCap = 50
	// sectionSearchMaxChunks bounds the chunks returned across documents.
	sectionSearchMaxChunks = 20
	// sectionSearchListPageSize is the page size used to enumerate scope.
	sectionSearchListPageSize = 200
	// sectionSearchAmbiguityCap bounds the candidates listed when a
	// document number matches several documents.
	sectionSearchAmbiguityCap = 10
	// sectionSearchMaxRefs bounds the sections one call reads. The pack eval
	// of 2026-11-08 showed the model walking a document one Điều per call
	// (Điều 27, 26, 25, 29, 30, 31, 32 in seven rounds).
	sectionSearchMaxRefs = 6
	// sectionNearbyEach is how many Điều headings before and after the asked
	// ones the result lists, so the model can pick the next Điều by its
	// title instead of probing numbers.
	sectionNearbyEach = 5
	// sectionNearbyTitleRunes cuts a listed Điều heading.
	sectionNearbyTitleRunes = 90
	// sectionNearbyMaxLines bounds the neighbour list: far-apart sections
	// (Điều 3 and Điều 190) get two windows, not everything between.
	sectionNearbyMaxLines = 24
)

// sectionArticleHeadingPattern reads the Điều number of a heading_path
// component ("Điều 17. Trách nhiệm").
var sectionArticleHeadingPattern = regexp.MustCompile(`(?i)^điều\s+(\d+[a-zđ]?)(?:[^\p{L}\p{N}]|$)`)

// sectionAppendixPattern matches a request for an appendix or a form
// ("Phụ lục I", "Mẫu số 02"), which is not a section: 8 calls in 30 days
// failed on "Phụ lục I".
var sectionAppendixPattern = regexp.MustCompile(`(?i)^\s*(phụ lục|mẫu số)`)

// sectionAppendixGuidance is how to reach an appendix instead, for the tool
// description and the error of an appendix request.
const sectionAppendixGuidance = "Appendices (Phụ lục) and forms (Mẫu số) are not sections: find their text with " +
	"read_document(id=dN, query=\"Phụ lục I\"); when the document's chunks list an appendix_file (an appendix " +
	"published as a separate file), give the user that file's name and link instead."

var searchDocumentSectionTool = BaseTool{
	name: ToolSearchDocumentSection,
	description: "Fetch the exact text of one section of a Vietnamese legal or administrative document by its citation: " +
		"Điều/Khoản/Điểm (\"khoản 2 Điều 17\", \"điểm a khoản 2 Điều 17\") or a Chương/Mục/Phần heading. It matches " +
		"structural metadata, so \"Điều 3\" never returns \"Điều 30\". Name the document with knowledge_id (dN) or " +
		"document_number; with neither, every document in scope is scanned. A document_number that fits several " +
		"documents lists the candidates: retry with one knowledge_id. To read several sections of the document, list " +
		"them in sections (up to 6, e.g. [\"Điều 24\", \"Điều 25\", \"khoản 2 Điều 26\"]) in one call instead of one " +
		"call each; the result also lists the titles of the neighbouring Điều so you can pick the next one by title. " +
		"Use search_knowledge for topics and read_document(id=cN, context=...) to read around a chunk.\n" +
		sectionAppendixGuidance,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "section": {
      "type": "string",
      "description": "The section citation, e.g. \"Điều 17\", \"khoản 2 Điều 17\", \"điểm a khoản 2 Điều 17\", \"Chương II\""
    },
    "sections": {
      "type": "array",
      "items": {"type": "string"},
      "maxItems": 6,
      "description": "Several citations of the same document read in one call, instead of section"
    },
    "knowledge_id": {
      "type": "string",
      "description": "dN handle of the document"
    },
    "document_number": {
      "type": "string",
      "description": "Số hiệu or short reference of the document (\"53/2022/NĐ-CP\", \"Nghị định 53/2022\", \"Thông tư 15/TT-BCA\")"
    },
    "knowledge_base_ids": {
      "type": "array",
      "items": {"type": "string"},
      "description": "bN handles to restrict the scan to (default: all in scope)"
    }
  }
}`),
}

// SearchDocumentSectionInput defines the input parameters for search_document_section.
type SearchDocumentSectionInput struct {
	Section          string   `json:"section"`
	Sections         []string `json:"sections,omitempty"`
	KnowledgeID      string   `json:"knowledge_id,omitempty"`
	DocumentNumber   string   `json:"document_number,omitempty"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids,omitempty"`
}

// SearchDocumentSectionTool resolves a Điều/Khoản/Điểm citation to the chunks
// that hold it, using the legal metadata the chunker records per chunk.
type SearchDocumentSectionTool struct {
	BaseTool
	knowledgeService interfaces.KnowledgeService
	chunkService     interfaces.ChunkService
	searchTargets    types.SearchTargets
	// currentWording returns legal chunks with in-place amendments applied.
	currentWording CurrentWordingFunc
}

// WithCurrentWording sets the current-wording lookup, as for read_document.
func (t *SearchDocumentSectionTool) WithCurrentWording(fn CurrentWordingFunc) *SearchDocumentSectionTool {
	t.currentWording = fn
	return t
}

// NewSearchDocumentSectionTool creates a new search_document_section tool.
func NewSearchDocumentSectionTool(
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	searchTargets types.SearchTargets,
) *SearchDocumentSectionTool {
	return &SearchDocumentSectionTool{
		BaseTool:         searchDocumentSectionTool,
		knowledgeService: knowledgeService,
		chunkService:     chunkService,
		searchTargets:    searchTargets,
	}
}

// sectionHit is one document's share of the result.
type sectionHit struct {
	knowledge *types.Knowledge
	chunks    []*types.Chunk
	// mode is "metadata" when the chunks carry legal metadata, "amendment"
	// when the Điều is not the document's own but its quoted text rewrites
	// that Điều of the amended document, "content" when the document
	// predates the metadata and only the Điều heading was matched in the
	// body text, "none" when the document has neither.
	mode string
}

// Execute parses the citation, resolves the document(s) and returns the
// matching chunks in document order.
func (t *SearchDocumentSectionTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input SearchDocumentSectionInput
	if err := json.Unmarshal(args, &input); err != nil {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf("Failed to parse args: %v", err)}, err
	}
	if t.knowledgeService == nil || t.chunkService == nil {
		return &types.ToolResult{Success: false, Error: "document services are unavailable"},
			fmt.Errorf("services unavailable")
	}
	refs, refNote, err := parseSectionRefs(input)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}

	docs, ambiguous, err := t.resolveDocuments(ctx, input)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}
	if ambiguous {
		return t.ambiguousResult(input.DocumentNumber, refs, docs), nil
	}
	if len(docs) == 0 {
		msg := "no document in scope"
		if strings.TrimSpace(input.DocumentNumber) != "" {
			msg = fmt.Sprintf("no document in scope matches document_number %q; use list_documents to find "+
				"its dN handle", input.DocumentNumber)
		}
		return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
	}

	// Each document's chunks are loaded once for all the asked sections;
	// only a single named document keeps them, for the neighbour list.
	lookups := make([]sectionLookup, len(refs))
	for i, ref := range refs {
		lookups[i] = sectionLookup{ref: ref, hits: make([]sectionHit, 0, len(docs))}
	}
	var docChunks []*types.Chunk
	for _, k := range docs {
		chunks, err := t.chunkService.GetRepository().ListChunksByKnowledgeID(ctx, t.tenantFor(k), k.ID)
		if err != nil {
			err = fmt.Errorf("failed to list chunks of %s: %w", k.ID, err)
			return &types.ToolResult{Success: false, Error: err.Error()}, err
		}
		for i := range lookups {
			lookups[i].hits = append(lookups[i].hits, sectionChunks(k, chunks, lookups[i].ref))
		}
		if len(docs) == 1 {
			docChunks = chunks
		}
	}
	var extraNotes []string
	if refNote != "" {
		extraNotes = append(extraNotes, refNote)
	}
	if len(docs) == 1 {
		if note := nearbySectionsNote(docChunks, refs); note != "" {
			extraNotes = append(extraNotes, note)
		}
	}
	result := t.render(ctx, input, lookups, extraNotes)
	// match_snippet holds the heading path here, not a text match: no query
	rows, _ := result.Data["chunks"].([]map[string]interface{})
	if n := applyCurrentWordingRows(ctx, t.currentWording, rows, ""); n > 0 {
		logger.Infof(ctx, "[Tool][SearchDocumentSection] Current wording applied to %d chunk(s)", n)
	}
	return result, nil
}

// sectionLookup is one asked section and each document's share of it.
type sectionLookup struct {
	ref  vietnamese_legal.SectionRef
	hits []sectionHit
}

// parseSectionRefs reads section and sections into distinct references, at
// most sectionSearchMaxRefs; note says what was left out.
func parseSectionRefs(input SearchDocumentSectionInput) ([]vietnamese_legal.SectionRef, string, error) {
	var raw []string
	if s := strings.TrimSpace(input.Section); s != "" {
		raw = append(raw, s)
	}
	for _, s := range input.Sections {
		if s = strings.TrimSpace(s); s != "" {
			raw = append(raw, s)
		}
	}
	if len(raw) == 0 {
		return nil, "", fmt.Errorf("section is required (e.g. \"khoản 2 Điều 17\"), or sections for several")
	}
	seen := make(map[string]bool, len(raw))
	refs := make([]vietnamese_legal.SectionRef, 0, len(raw))
	for _, s := range raw {
		ref, err := vietnamese_legal.ParseSectionRef(s)
		if err != nil {
			msg := fmt.Sprintf("section %q names no Điều, Khoản, Điểm, Chương, Mục or Phần", s)
			if sectionAppendixPattern.MatchString(s) {
				msg += ". " + sectionAppendixGuidance
			}
			return nil, "", fmt.Errorf("%s", msg)
		}
		if ref.Multi {
			return nil, "", fmt.Errorf("section %q names more than one part; list each one as its own "+
				"entry of sections (e.g. [\"khoản 2 Điều 17\", \"khoản 3 Điều 17\"])", s)
		}
		if key := ref.String(); !seen[key] {
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	if len(refs) <= sectionSearchMaxRefs {
		return refs, "", nil
	}
	left := make([]string, 0, len(refs)-sectionSearchMaxRefs)
	for _, ref := range refs[sectionSearchMaxRefs:] {
		left = append(left, ref.String())
	}
	note := fmt.Sprintf("Only the first %d sections were read; not read: %s.", sectionSearchMaxRefs, strings.Join(left, ", "))
	return refs[:sectionSearchMaxRefs], note, nil
}

// resolveDocuments returns the documents the section is read from. With a
// knowledge_id that one document; with a document_number the documents whose
// số hiệu / title fit it (ambiguous when several fit equally well and none
// exactly); otherwise every document in the scanned scope, capped.
func (t *SearchDocumentSectionTool) resolveDocuments(
	ctx context.Context, input SearchDocumentSectionInput,
) (docs []*types.Knowledge, ambiguous bool, err error) {
	if id := strings.TrimSpace(input.KnowledgeID); id != "" {
		knowledge, kerr := t.knowledgeService.GetKnowledgeByIDOnly(ctx, id)
		if kerr != nil || knowledge == nil {
			return nil, false, fmt.Errorf("knowledge_id %s is not a document in the current scope", id)
		}
		authorized, aerr := authorizeLoadedKnowledge(ctx, t.searchTargets, knowledge, t.knowledgeService)
		if aerr != nil {
			return nil, false, fmt.Errorf("document is not accessible: %w", aerr)
		}
		return []*types.Knowledge{authorized}, false, nil
	}

	kbIDs := dedupNonEmptyStrings(input.KnowledgeBaseIDs)
	if len(kbIDs) > 0 {
		if verr := validateKnowledgeBaseIDsInSearchTargets(t.searchTargets, kbIDs); verr != nil {
			return nil, false, verr
		}
	} else {
		kbIDs = t.searchTargets.GetAllKnowledgeBaseIDs()
	}
	if len(kbIDs) == 0 {
		return nil, false, fmt.Errorf("no knowledge base in scope")
	}

	number := strings.TrimSpace(input.DocumentNumber)
	parsed := vietnamese_legal.ParseReference(number)
	seen := make(map[string]bool)
	var all []*types.Knowledge
	add := func(rows []*types.Knowledge) {
		for _, k := range rows {
			if k != nil && !seen[k.ID] {
				seen[k.ID] = true
				all = append(all, k)
			}
		}
	}
	for _, kbID := range kbIDs {
		rows, _, lerr := listKnowledgeInScope(ctx, t.knowledgeService, t.searchTargets, kbID, "", 1, sectionSearchListPageSize)
		if lerr != nil {
			return nil, false, fmt.Errorf("failed to list documents of knowledge base %s: %w", kbID, lerr)
		}
		add(rows)
		// A large base is enumerated only partially above; a title / file
		// name search by the bare number reaches the rest.
		if number != "" && parsed.NumberRaw != "" {
			rows, _, lerr = listKnowledgeInScope(ctx, t.knowledgeService, t.searchTargets, kbID, parsed.NumberRaw, 1, sectionSearchAmbiguityCap*5)
			if lerr != nil {
				return nil, false, fmt.Errorf("failed to list documents of knowledge base %s: %w", kbID, lerr)
			}
			add(rows)
		}
	}
	if number == "" {
		if len(all) > sectionSearchDocCap {
			all = all[:sectionSearchDocCap]
		}
		return all, false, nil
	}

	best := 0
	var matched []*types.Knowledge
	for _, k := range all {
		score := documentNumberScore(k, parsed, number)
		switch {
		case score == 0:
		case score > best:
			best, matched = score, []*types.Knowledge{k}
		case score == best:
			matched = append(matched, k)
		}
	}
	if len(matched) <= 1 || best >= documentNumberExact {
		return matched, false, nil
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Title < matched[j].Title })
	if len(matched) > sectionSearchAmbiguityCap {
		matched = matched[:sectionSearchAmbiguityCap]
	}
	return matched, true, nil
}

// Document-number match strengths, strongest first.
const (
	documentNumberExact   = 3 // profile số hiệu equals the full number given
	documentNumberPartial = 2 // the full number given occurs in the số hiệu or title
	documentNumberBare    = 1 // only the bare number (plus year / type when given) fits
)

// documentNumberScore rates how well a document fits a document reference:
// 0 when it does not fit at all.
func documentNumberScore(k *types.Knowledge, ref vietnamese_legal.ParsedReference, raw string) int {
	profileNumber, typeCode := "", ""
	if k.Profile != nil {
		profileNumber = k.Profile.DocumentNumber
		typeCode = k.Profile.DocTypeCode
	}
	haystacks := []string{profileNumber, k.Title, k.FileName}
	// "số 53" parses as DocumentNumber "53": a bare number, not a số hiệu,
	// so it takes the bare path below (year and type then disambiguate).
	if want := vietnamese_legal.NormalizeDocumentNumber(ref.DocumentNumber); strings.Contains(want, "/") {
		if profileNumber != "" && vietnamese_legal.NormalizeDocumentNumber(profileNumber) == want {
			return documentNumberExact
		}
		for _, h := range haystacks {
			if containsFoldingSpace(h, want) {
				return documentNumberPartial
			}
		}
	}
	if ref.NumberRaw == "" {
		// A reference with no number at all ("Luật An ninh mạng") falls
		// back to a title match on the whole text.
		if raw != "" && containsFoldingSpace(k.Title, raw) {
			return documentNumberPartial
		}
		return 0
	}
	if ref.DocTypeSlug != "" && typeCode != "" && typeCode != ref.DocTypeSlug {
		return 0
	}
	for _, h := range haystacks {
		if !vietnamese_legal.NumberTokenPresent(h, ref.NumberRaw) {
			continue
		}
		if ref.Year != "" && !strings.Contains(h, ref.Year) && !strings.Contains(k.Title, ref.Year) {
			continue
		}
		return documentNumberBare
	}
	return 0
}

// containsFoldingSpace reports whether needle occurs in haystack ignoring
// case and whitespace ("45 / KH-UBND" holds "45/KH-UBND").
func containsFoldingSpace(haystack, needle string) bool {
	fold := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if !unicode.IsSpace(r) {
				b.WriteRune(unicode.ToLower(r))
			}
		}
		return b.String()
	}
	n := fold(needle)
	return n != "" && strings.Contains(fold(haystack), n)
}

func (t *SearchDocumentSectionTool) tenantFor(knowledge *types.Knowledge) uint64 {
	if knowledge.TenantID != 0 {
		return knowledge.TenantID
	}
	return t.searchTargets.GetTenantIDForKB(knowledge.KnowledgeBaseID)
}

// sectionChunks keeps the document's chunks holding the section. Documents
// indexed before the legal chunker recorded metadata fall back to a heading
// match in the body text (Điều / Chương level only).
func sectionChunks(knowledge *types.Knowledge, chunks []*types.Chunk, ref vietnamese_legal.SectionRef) sectionHit {
	hit := sectionHit{knowledge: knowledge, mode: "none"}
	hasMeta := false
	var amending []*types.Chunk
	for _, c := range chunks {
		if c == nil || !c.IsEnabled {
			continue
		}
		meta := legalChunkMetadata(c)
		if meta == nil {
			continue
		}
		hasMeta = true
		if ref.Matches(meta) {
			hit.chunks = append(hit.chunks, c)
		} else if ref.MatchesAmendment(meta) {
			amending = append(amending, c)
		}
	}
	if hasMeta {
		hit.mode = "metadata"
		// An amending document has no Điều 9 of its own: its text that
		// rewrites Điều 9 of the amended document is what was asked for.
		if len(hit.chunks) == 0 && len(amending) > 0 {
			hit.mode, hit.chunks = "amendment", amending
		}
		return hit
	}
	re := ref.ContentPattern()
	if re == nil {
		return hit
	}
	hit.mode = "content"
	for _, c := range chunks {
		if c != nil && c.IsEnabled && re.MatchString(c.Content) {
			hit.chunks = append(hit.chunks, c)
		}
	}
	return hit
}

// sectionArticle is one Điều of a document: its number and heading.
type sectionArticle struct {
	no      string
	heading string
}

// documentArticles lists the document's own Điều in reading order, from the
// chunks' heading paths. Quoted amending text and appendices are skipped:
// their Điều belong to another text.
func documentArticles(chunks []*types.Chunk) []sectionArticle {
	var out []sectionArticle
	seen := make(map[string]bool)
	for _, c := range chunks {
		if c == nil || !c.IsEnabled {
			continue
		}
		meta := legalChunkMetadata(c)
		if meta == nil || meta.Quoted || meta.Appendix != "" {
			continue
		}
		for _, comp := range meta.HeadingPath {
			comp = strings.TrimSpace(comp)
			m := sectionArticleHeadingPattern.FindStringSubmatch(comp)
			if m == nil {
				continue
			}
			no := strings.ToLower(m[1])
			if seen[no] {
				continue
			}
			seen[no] = true
			out = append(out, sectionArticle{no: no, heading: comp})
		}
	}
	return out
}

// articleNumber is the numeric part of a Điều number ("5a" → 5), -1 when
// it has none.
func articleNumber(no string) int {
	n, digits := 0, 0
	for _, r := range no {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		digits++
	}
	if digits == 0 {
		return -1
	}
	return n
}

// nearbySectionsNote lists the headings of the Điều around the asked ones
// in a single document, so the next section is chosen by its title rather
// than by trying neighbouring numbers. Empty when no Điều was asked or the
// document has no Điều structure.
func nearbySectionsNote(chunks []*types.Chunk, refs []vietnamese_legal.SectionRef) string {
	articles := documentArticles(chunks)
	if len(articles) < 2 {
		return ""
	}
	asked := make(map[string]bool)
	inWindow := make([]bool, len(articles))
	hasArticle := false
	for _, ref := range refs {
		if ref.Article == "" {
			continue
		}
		asked[ref.Article] = true
		hasArticle = true
		// position of the asked Điều, or where it would sit by number
		pos := len(articles) - 1
		want := articleNumber(ref.Article)
		for i, a := range articles {
			if a.no == ref.Article || (want >= 0 && articleNumber(a.no) >= want) {
				pos = i
				break
			}
		}
		for i := max(0, pos-sectionNearbyEach); i <= min(len(articles)-1, pos+sectionNearbyEach); i++ {
			inWindow[i] = true
		}
	}
	if !hasArticle {
		return ""
	}
	var lines []string
	gap := false
	for i, a := range articles {
		if !inWindow[i] {
			gap = len(lines) > 0
			continue
		}
		if asked[a.no] {
			continue
		}
		if len(lines) >= sectionNearbyMaxLines {
			lines = append(lines, "…")
			break
		}
		if gap {
			lines = append(lines, "…")
			gap = false
		}
		heading := a.heading
		if r := []rune(heading); len(r) > sectionNearbyTitleRunes {
			heading = strings.TrimSpace(string(r[:sectionNearbyTitleRunes])) + "…"
		}
		lines = append(lines, heading)
	}
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf("Nearby Điều of this document (it has %d); read the ones you need together with "+
		"sections=[...]:\n%s", len(articles), strings.Join(lines, "\n"))
}

// legalChunkMetadata returns the chunk's legal metadata, nil when absent.
func legalChunkMetadata(c *types.Chunk) *vietnamese_legal.LegalMetadata {
	meta, err := c.DocumentMetadata()
	if err != nil || meta == nil {
		return nil
	}
	return meta.Legal
}

// render builds the model/UI payload: per asked section, chunks grouped by
// document in document order, all sections sharing the chunk cap and the
// output budget. A chunk already shown for an earlier section is not
// repeated. extraNotes go to the model after the chunks.
func (t *SearchDocumentSectionTool) render(
	ctx context.Context, input SearchDocumentSectionInput, lookups []sectionLookup, extraNotes []string,
) *types.ToolResult {
	budget := OutputBudget(ctx) * 4 / 5
	used, matchCount, emitted := 0, 0, 0
	truncated := false
	rows := make([]map[string]interface{}, 0)
	var docRows []map[string]interface{}
	docSeen := make(map[string]bool)
	shown := make(map[string]string) // chunk id → label of the section that showed it
	var b strings.Builder
	var notes []string
	labels := make([]string, 0, len(lookups))
	sections := make([]map[string]interface{}, 0, len(lookups))

	for _, lk := range lookups {
		ref, hits := lk.ref, lk.hits
		label := ref.String()
		labels = append(labels, label)
		sections = append(sections, sectionData(ref))
		refMatches := 0
		var withChunks []sectionHit
		for _, hit := range hits {
			refMatches += len(hit.chunks)
			if len(hit.chunks) > 0 {
				withChunks = append(withChunks, hit)
			}
		}
		matchCount += refMatches
		if truncated {
			// the budget is spent: later sections are only named, unless
			// everything they hold was already shown
			fmt.Fprintf(&b, "<sections ref=\"%s\" matches=\"%d\" truncated=\"true\" />\n", xmlEscape(label), refMatches)
			if unshownChunks(withChunks, shown) > 0 {
				notes = append(notes, fmt.Sprintf("%s was not shown (output budget spent); ask for it alone.", label))
			}
			continue
		}
		fmt.Fprintf(&b, "<sections ref=\"%s\" documents=\"%d\" matches=\"%d\">\n", xmlEscape(label), len(withChunks), refMatches)
		for _, hit := range withChunks {
			k := hit.knowledge
			if !docSeen[k.ID] {
				docSeen[k.ID] = true
				docRow := map[string]interface{}{
					"knowledge_id": k.ID,
					"title":        strings.TrimSpace(k.Title),
					"match_mode":   hit.mode,
					"chunk_count":  len(hit.chunks),
				}
				if k.Profile != nil && k.Profile.DocumentNumber != "" {
					docRow["document_number"] = k.Profile.DocumentNumber
				}
				docRows = append(docRows, docRow)
			}
			fmt.Fprintf(&b, "<document knowledge_id=\"%s\" title=\"%s\" match=\"%s\" chunks=\"%d\"",
				xmlEscape(k.ID), xmlEscape(strings.TrimSpace(k.Title)), hit.mode, len(hit.chunks))
			if k.Profile != nil && k.Profile.DocumentNumber != "" {
				fmt.Fprintf(&b, " document_number=\"%s\"", xmlEscape(k.Profile.DocumentNumber))
			}
			b.WriteString(">\n")
			if hit.mode == "amendment" {
				note := fmt.Sprintf("%s is not a provision of %q. The chunks with role=amendment are its amending "+
					"text that rewrites %s of the amended document named in their cite; cite them by their own "+
					"location (cite) and say which document they amend.", label, strings.TrimSpace(k.Title), label)
				notes = append(notes, note)
				fmt.Fprintf(&b, "<note>%s</note>\n", xmlEscape(note))
			}
			if hit.mode == "content" && (ref.Khoan != "" || ref.Diem != "") {
				b.WriteString("<note>This document carries no Khoản/Điểm metadata (indexed before structure " +
					"recording); the whole Điều is returned, locate the Khoản/Điểm in its text.</note>\n")
			}
			enrichChunkImageInfo(ctx, t.chunkService.GetRepository(), t.tenantFor(k), hit.chunks)
			var repeated []string
			for _, c := range hit.chunks {
				if prev, ok := shown[c.ID]; ok {
					repeated = append(repeated, prev)
					continue
				}
				if emitted >= sectionSearchMaxChunks || (used >= budget && emitted > 0) {
					truncated = true
					break
				}
				emitted++
				shown[c.ID] = label
				used += len([]rune(c.Content)) + readDocumentChunkOverhead
				row := chunkDataMap(emitted, c)
				row["knowledge_id"] = k.ID
				row["knowledge_title"] = strings.TrimSpace(k.Title)
				role := "match"
				if hit.mode == "amendment" {
					role = "amendment"
				}
				row["role"] = role
				extra := fmt.Sprintf(" role=\"%s\"", role)
				if meta := legalChunkMetadata(c); meta != nil && len(meta.HeadingPath) > 0 {
					path := strings.Join(meta.HeadingPath, " > ")
					row["match_snippet"] = path
					extra += fmt.Sprintf(" heading_path=\"%s\"", xmlEscape(path))
				}
				// a lookup of one khoản / điểm points at it, not at the whole chunk
				if meta := legalChunkMetadata(c); hit.mode == "metadata" && meta != nil && len(meta.ArticleNos) == 1 {
					if u := meta.SectionURL(ref.Khoan, ref.Diem); u != "" {
						row["source_url"] = u
						cite := vietnamese_legal.AmendRef{Article: meta.ArticleNos[0], Khoan: ref.Khoan, Diem: ref.Diem}.String()
						if meta.Appendix != "" {
							cite += " – " + meta.Appendix
						}
						row["legal_cite"] = cite
					}
				}
				if cite, ok := row["legal_cite"].(string); ok {
					extra += fmt.Sprintf(" cite=\"%s\"", xmlEscape(cite))
				}
				rows = append(rows, row)
				writeChunkXML(&b, c, extra)
			}
			if len(repeated) > 0 {
				note := fmt.Sprintf("%s is in the chunk(s) already shown above for %s.", label, strings.Join(dedupNonEmptyStrings(repeated), ", "))
				notes = append(notes, note)
				fmt.Fprintf(&b, "<note>%s</note>\n", xmlEscape(note))
			}
			b.WriteString("</document>\n")
			if truncated {
				break
			}
		}
		if truncated {
			b.WriteString("<truncated>true</truncated>\n")
			notes = append(notes, fmt.Sprintf("%s: %d chunk(s) not shown (output budget spent); ask for it alone "+
				"to read the rest.", label, unshownChunks(withChunks, shown)))
		}
		if refMatches == 0 {
			text := emptySectionText(label, hits, input)
			fmt.Fprintf(&b, "<empty>%s</empty>\n", xmlEscape(text))
			if len(lookups) > 1 || len(hits) == 1 {
				// the model reads the chunks, not this Output: say it there
				notes = append(notes, text)
			}
		}
		b.WriteString("</sections>\n")
	}
	notes = append(notes, extraNotes...)
	for _, note := range extraNotes {
		fmt.Fprintf(&b, "<note>%s</note>\n", xmlEscape(note))
	}

	data := map[string]interface{}{
		"display_type":   "knowledge_chunks_list",
		"query":          strings.Join(labels, "; "),
		"section":        sections[0],
		"match_count":    matchCount,
		"fetched_chunks": len(rows),
		"total_chunks":   len(rows),
		"truncated":      truncated,
		"chunks":         rows,
		"documents":      docRows,
	}
	if docRows == nil {
		data["documents"] = make([]map[string]interface{}, 0)
	}
	if len(sections) > 1 {
		data["sections"] = sections
	}
	if len(notes) > 0 {
		data["model_note"] = strings.Join(notes, "\n")
	}
	if hits := lookups[0].hits; len(hits) == 1 {
		k := hits[0].knowledge
		data["knowledge_id"] = k.ID
		data["knowledge_title"] = strings.TrimSpace(k.Title)
		data["document"] = documentInfo(k, int64(len(hits[0].chunks)))
		data["match_mode"] = hits[0].mode
	}
	return &types.ToolResult{Success: true, Output: strings.TrimSuffix(b.String(), "\n"), Data: data}
}

// unshownChunks counts the chunks of hits not shown for an earlier section.
func unshownChunks(hits []sectionHit, shown map[string]string) int {
	n := 0
	for _, hit := range hits {
		for _, c := range hit.chunks {
			if _, ok := shown[c.ID]; !ok {
				n++
			}
		}
	}
	return n
}

// emptySectionText tells the model why nothing came back and what to try.
func emptySectionText(label string, hits []sectionHit, input SearchDocumentSectionInput) string {
	var b strings.Builder
	noMeta := 0
	for _, hit := range hits {
		if hit.mode != "metadata" {
			noMeta++
		}
	}
	switch {
	case len(hits) == 1:
		fmt.Fprintf(&b, "%s was not found in %q.", label, strings.TrimSpace(hits[0].knowledge.Title))
	default:
		fmt.Fprintf(&b, "%s was not found in any of the %d documents scanned.", label, len(hits))
	}
	if noMeta == len(hits) {
		b.WriteString(" The document carries no legal structure metadata (not a Điều-structured document, or " +
			"indexed before structure recording), so Khoản/Điểm cannot be resolved; " +
			"use read_document(id=dN, query=...) instead.")
	} else {
		b.WriteString(" Check the number, or use read_document(id=dN, query=\"Điều N\") to read nearby text.")
	}
	if input.DocumentNumber == "" && input.KnowledgeID == "" && len(hits) >= sectionSearchDocCap {
		fmt.Fprintf(&b, " Only the first %d documents in scope were scanned; name the document with "+
			"document_number or knowledge_id.", sectionSearchDocCap)
	}
	return b.String()
}

func sectionData(ref vietnamese_legal.SectionRef) map[string]interface{} {
	out := map[string]interface{}{}
	if ref.Article != "" {
		out["article"] = ref.Article
	}
	if ref.Khoan != "" {
		out["khoan"] = ref.Khoan
	}
	if ref.Diem != "" {
		out["diem"] = ref.Diem
	}
	if ref.Heading != "" {
		out["heading"] = ref.Heading
	}
	return out
}

// ambiguousResult lists the documents a document_number fits so the model
// can retry with one knowledge_id.
func (t *SearchDocumentSectionTool) ambiguousResult(
	number string, refs []vietnamese_legal.SectionRef, docs []*types.Knowledge,
) *types.ToolResult {
	rows := make([]map[string]interface{}, 0, len(docs))
	var b strings.Builder
	fmt.Fprintf(&b, "<documents ambiguous=\"true\" document_number=\"%s\" total=\"%d\">\n", xmlEscape(number), len(docs))
	for _, k := range docs {
		row := map[string]interface{}{
			"knowledge_id": k.ID,
			"title":        strings.TrimSpace(k.Title),
			"file_name":    k.FileName,
			"parse_status": k.ParseStatus,
			"is_faq":       false,
		}
		if k.Profile != nil && k.Profile.DocumentNumber != "" {
			row["document_number"] = k.Profile.DocumentNumber
		}
		rows = append(rows, row)
		fmt.Fprintf(&b, "<document knowledge_id=\"%s\" title=\"%s\"", xmlEscape(k.ID), xmlEscape(strings.TrimSpace(k.Title)))
		if num, ok := row["document_number"]; ok {
			fmt.Fprintf(&b, " document_number=\"%s\"", xmlEscape(fmt.Sprint(num)))
		}
		b.WriteString(" />\n")
	}
	arg := fmt.Sprintf("section=%q", refs[0].String())
	if len(refs) > 1 {
		labels := make([]string, len(refs))
		for i, ref := range refs {
			labels[i] = fmt.Sprintf("%q", ref.String())
		}
		arg = "sections=[" + strings.Join(labels, ", ") + "]"
	}
	hint := fmt.Sprintf("document_number %q fits %d documents. Retry search_document_section(%s, "+
		"knowledge_id=dN) with the intended document.", number, len(docs), arg)
	fmt.Fprintf(&b, "<hint>%s</hint>\n</documents>", xmlEscape(hint))
	data := map[string]interface{}{
		// the model reads the rows, not Output: the retry instruction rides along
		"model_note":      hint,
		"display_type":    "document_info",
		"ambiguous":       true,
		"document_number": number,
		"section":         sectionData(refs[0]),
		"documents":       rows,
		"total_docs":      int64(len(rows)),
	}
	if len(refs) > 1 {
		sections := make([]map[string]interface{}, len(refs))
		for i, ref := range refs {
			sections[i] = sectionData(ref)
		}
		data["sections"] = sections
	}
	return &types.ToolResult{Success: true, Output: b.String(), Data: data}
}
