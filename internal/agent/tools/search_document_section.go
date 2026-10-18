package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

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
)

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
	description: "Fetch the exact text of one section of a Vietnamese legal or administrative document by its " +
		"citation: Điều, Khoản and Điểm (\"Điều 17\", \"khoản 2 Điều 17\", \"điểm a khoản 2 Điều 17\") or a " +
		"Chương/Mục/Phần heading (\"Chương II\"). Matching uses the structural metadata recorded when the " +
		"document was indexed, so \"Điều 3\" never returns \"Điều 30\".\n" +
		"Name the document with knowledge_id (its dN handle) or document_number (số hiệu such as " +
		"\"53/2022/NĐ-CP\", or \"Nghị định 53/2022\"); with neither, every document in scope is scanned and " +
		"the chunks come back grouped by document. When a document_number fits several documents the " +
		"candidates are listed instead: retry with one knowledge_id.\n" +
		"One section per call: ask for \"khoản 2 Điều 17\" and \"khoản 3 Điều 17\" separately. Use " +
		"search_knowledge for questions by topic, and read_document(id=cN, context=...) to read around a " +
		"returned chunk.\n" + sectionAppendixGuidance,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "section": {
      "type": "string",
      "description": "The section citation, e.g. \"Điều 17\", \"khoản 2 Điều 17\", \"điểm a khoản 2 Điều 17\", \"Chương II\"",
      "minLength": 1
    },
    "knowledge_id": {
      "type": "string",
      "description": "dN handle of the document to read the section from"
    },
    "document_number": {
      "type": "string",
      "description": "Số hiệu or short reference of the document (\"53/2022/NĐ-CP\", \"Nghị định 53/2022\", \"Thông tư 15/TT-BCA\")"
    },
    "knowledge_base_ids": {
      "type": "array",
      "items": {"type": "string"},
      "description": "bN handles to restrict the scan to (default: every knowledge base in scope)"
    }
  },
  "required": ["section"]
}`),
}

// SearchDocumentSectionInput defines the input parameters for search_document_section.
type SearchDocumentSectionInput struct {
	Section          string   `json:"section"`
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
	section := strings.TrimSpace(input.Section)
	if section == "" {
		return &types.ToolResult{Success: false, Error: "section is required (e.g. \"khoản 2 Điều 17\")"},
			fmt.Errorf("missing section")
	}
	ref, err := vietnamese_legal.ParseSectionRef(section)
	if err != nil {
		msg := fmt.Sprintf("section %q names no Điều, Khoản, Điểm, Chương, Mục or Phần", section)
		if sectionAppendixPattern.MatchString(section) {
			msg += ". " + sectionAppendixGuidance
		}
		return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
	}
	if ref.Multi {
		msg := fmt.Sprintf("section %q names more than one part; ask for one Điều/Khoản/Điểm per call", section)
		return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
	}

	docs, ambiguous, err := t.resolveDocuments(ctx, input)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, err
	}
	if ambiguous {
		return t.ambiguousResult(input.DocumentNumber, ref, docs), nil
	}
	if len(docs) == 0 {
		msg := "no document in scope"
		if strings.TrimSpace(input.DocumentNumber) != "" {
			msg = fmt.Sprintf("no document in scope matches document_number %q; use list_documents to find "+
				"its dN handle", input.DocumentNumber)
		}
		return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
	}

	hits := make([]sectionHit, 0, len(docs))
	for _, k := range docs {
		hit, err := t.sectionChunks(ctx, k, ref)
		if err != nil {
			return &types.ToolResult{Success: false, Error: err.Error()}, err
		}
		hits = append(hits, hit)
	}
	return t.render(ctx, ref, input, hits), nil
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

// sectionChunks loads the document's text chunks and keeps those holding the
// section. Documents indexed before the legal chunker recorded metadata fall
// back to a heading match in the body text (Điều / Chương level only).
func (t *SearchDocumentSectionTool) sectionChunks(
	ctx context.Context, knowledge *types.Knowledge, ref vietnamese_legal.SectionRef,
) (sectionHit, error) {
	hit := sectionHit{knowledge: knowledge, mode: "none"}
	chunks, err := t.chunkService.GetRepository().ListChunksByKnowledgeID(ctx, t.tenantFor(knowledge), knowledge.ID)
	if err != nil {
		return hit, fmt.Errorf("failed to list chunks of %s: %w", knowledge.ID, err)
	}
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
		return hit, nil
	}
	re := ref.ContentPattern()
	if re == nil {
		return hit, nil
	}
	hit.mode = "content"
	for _, c := range chunks {
		if c != nil && c.IsEnabled && re.MatchString(c.Content) {
			hit.chunks = append(hit.chunks, c)
		}
	}
	return hit, nil
}

// legalChunkMetadata returns the chunk's legal metadata, nil when absent.
func legalChunkMetadata(c *types.Chunk) *vietnamese_legal.LegalMetadata {
	meta, err := c.DocumentMetadata()
	if err != nil || meta == nil {
		return nil
	}
	return meta.Legal
}

// render builds the model/UI payload: chunks grouped by document in document
// order, bounded by the chunk cap and the output budget.
func (t *SearchDocumentSectionTool) render(
	ctx context.Context, ref vietnamese_legal.SectionRef, input SearchDocumentSectionInput, hits []sectionHit,
) *types.ToolResult {
	label := ref.String()
	budget := OutputBudget(ctx) * 4 / 5
	used, matchCount, emitted := 0, 0, 0
	truncated := false
	rows := make([]map[string]interface{}, 0)
	docRows := make([]map[string]interface{}, 0, len(hits))
	var b strings.Builder
	var withChunks []sectionHit
	var notes []string

	for _, hit := range hits {
		matchCount += len(hit.chunks)
		if len(hit.chunks) > 0 {
			withChunks = append(withChunks, hit)
		}
	}
	fmt.Fprintf(&b, "<sections ref=\"%s\" documents=\"%d\" matches=\"%d\">\n", xmlEscape(label), len(withChunks), matchCount)
	for _, hit := range withChunks {
		k := hit.knowledge
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
		fmt.Fprintf(&b, "<document knowledge_id=\"%s\" title=\"%s\" match=\"%s\" chunks=\"%d\"",
			xmlEscape(k.ID), xmlEscape(strings.TrimSpace(k.Title)), hit.mode, len(hit.chunks))
		if num, ok := docRow["document_number"]; ok {
			fmt.Fprintf(&b, " document_number=\"%s\"", xmlEscape(fmt.Sprint(num)))
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
		for _, c := range hit.chunks {
			if emitted >= sectionSearchMaxChunks || (used >= budget && emitted > 0) {
				truncated = true
				break
			}
			emitted++
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
		b.WriteString("</document>\n")
		if truncated {
			break
		}
	}
	if truncated {
		b.WriteString("<truncated>true</truncated>\n")
	}
	if matchCount == 0 {
		b.WriteString(emptySectionStatement(label, hits, input))
	}
	b.WriteString("</sections>")

	data := map[string]interface{}{
		"display_type":   "knowledge_chunks_list",
		"query":          label,
		"section":        sectionData(ref),
		"match_count":    matchCount,
		"fetched_chunks": len(rows),
		"total_chunks":   len(rows),
		"truncated":      truncated,
		"chunks":         rows,
		"documents":      docRows,
	}
	if len(notes) > 0 {
		data["model_note"] = strings.Join(notes, "\n")
	}
	if len(hits) == 1 {
		k := hits[0].knowledge
		data["knowledge_id"] = k.ID
		data["knowledge_title"] = strings.TrimSpace(k.Title)
		data["document"] = documentInfo(k, int64(len(hits[0].chunks)))
		data["match_mode"] = hits[0].mode
	}
	return &types.ToolResult{Success: true, Output: b.String(), Data: data}
}

// emptySectionStatement tells the model why nothing came back and what to try.
func emptySectionStatement(label string, hits []sectionHit, input SearchDocumentSectionInput) string {
	var b strings.Builder
	noMeta := 0
	for _, hit := range hits {
		if hit.mode != "metadata" {
			noMeta++
		}
	}
	switch {
	case len(hits) == 1:
		fmt.Fprintf(&b, "<empty>%s was not found in %q.", xmlEscape(label), xmlEscape(strings.TrimSpace(hits[0].knowledge.Title)))
	default:
		fmt.Fprintf(&b, "<empty>%s was not found in any of the %d documents scanned.", xmlEscape(label), len(hits))
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
	b.WriteString("</empty>\n")
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
	number string, ref vietnamese_legal.SectionRef, docs []*types.Knowledge,
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
	fmt.Fprintf(&b, "<hint>document_number %q fits %d documents. Retry search_document_section(section=%q, "+
		"knowledge_id=dN) with the intended document.</hint>\n</documents>", number, len(docs), ref.String())
	return &types.ToolResult{
		Success: true,
		Output:  b.String(),
		Data: map[string]interface{}{
			"display_type":    "document_info",
			"ambiguous":       true,
			"document_number": number,
			"section":         sectionData(ref),
			"documents":       rows,
			"total_docs":      int64(len(rows)),
		},
	}
}
