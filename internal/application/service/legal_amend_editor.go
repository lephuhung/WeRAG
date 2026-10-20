package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// The second amendment source (plan 2026-10-19 §11): the provision
// relations vbpl.vn editors tag on the amended document A, imported into
// legal_provision_links (cmd/legal-provision-links). Behind the setting
// LegalEditorSourceSetting (default off); off, nothing here runs and the
// load is what it was.
//
// Tiers (plan §3.3):
//   - an editor edge and a parser edge on the same provision of A, from
//     the same B, with compatible operations (and the same date when both
//     know it) agree: the parser edge becomes tier 1, its wording / phrase
//     applied;
//   - an editor edge alone is tier 2 with the provision known exactly:
//     its new text is read from B's source provision (B chunk by
//     provision_ids, in a KB the question may read — §10.2), else the
//     provision is AMENDED_TEXT_MISSING;
//   - 14 giải thích / 6 đính chính change no text: an informational line;
//   - 11 tạm ngưng / 5 đình chỉ: NOT_APPLICABLE_IN_PERIOD over the
//     interval the editors date (the operation keeps the text).

// LegalEditorSourceSetting is the system setting adding the vbpl editor
// edges (legal_provision_links) to the resolver.
const LegalEditorSourceSetting = "legal.current_wording.editor_source"

// legalEditorSourceDefault is the setting's value when unset.
const legalEditorSourceDefault = false

// LegalAmendSourceEditor names edges from the vbpl editors' tags.
const LegalAmendSourceEditor = types.LegalProvisionLinkSourceEditor

const (
	// legalEditorLinksMax bounds the editor edges read per load.
	legalEditorLinksMax = 3000
	// legalEditorChunksMax bounds the B chunks read for source texts.
	legalEditorChunksMax = 400
	// legalEditorSourceRunes caps B's source text kept on an edge.
	legalEditorSourceRunes = 1200
)

// LegalAmendLoadOptions are the optional sources of a load.
type LegalAmendLoadOptions struct {
	// EditorSource adds the vbpl editor edges (LegalEditorSourceSetting).
	EditorSource bool
	// EditorLinks reads the editor edges of A's documents; nil reads
	// legal_provision_links (tests and offline probes pass their own).
	EditorLinks func(ctx context.Context, db *gorm.DB, aIDs []string) ([]types.LegalProvisionLink, error)
}

// legalEditorSourceOn reads the setting per call.
func legalEditorSourceOn(ctx context.Context, settings interfaces.SystemSettingService) bool {
	if settings == nil {
		return legalEditorSourceDefault
	}
	return settings.GetBool(ctx, LegalEditorSourceSetting, "", legalEditorSourceDefault)
}

// legalLoadOptions are the load's optional sources as the settings say.
func legalLoadOptions(ctx context.Context, settings interfaces.SystemSettingService) LegalAmendLoadOptions {
	return LegalAmendLoadOptions{EditorSource: legalEditorSourceOn(ctx, settings)}
}

// readLegalEditorLinks is the indexed read of A's editor edges.
func readLegalEditorLinks(ctx context.Context, db *gorm.DB, aIDs []string) ([]types.LegalProvisionLink, error) {
	var rows []types.LegalProvisionLink
	if len(aIDs) == 0 {
		return nil, nil
	}
	err := db.WithContext(ctx).Raw(`SELECT id, target_doc_id, target_provision_id, target_level, target_path, relation_code, op, tag_attr,
		source_doc_id, source_doc_from, source_provision_id, source_path, eff_from, eff_to, eff_from_source, source
		FROM legal_provision_links WHERE target_doc_id IN ? AND source_doc_id <> '' AND target_path <> '' ORDER BY id LIMIT ?`,
		aIDs, legalEditorLinksMax).Scan(&rows).Error
	return rows, err
}

// legalEditorTarget is what the editor merge needs of a retrieved A.
type legalEditorTarget struct {
	doc      LegalAmendDoc
	articles map[string]bool
}

// legalPathRef reads "dieu:5/khoan:2/diem:a"; ok is false for an empty
// path or one inside an appendix ("phuluc/…": not the main numbering).
func legalPathRef(path string) (LegalProvisionRef, bool) {
	var p LegalProvisionRef
	if path == "" || strings.HasPrefix(path, "phuluc/") {
		return p, false
	}
	for _, part := range strings.Split(path, "/") {
		k, v, ok := strings.Cut(part, ":")
		if !ok || v == "" {
			return LegalProvisionRef{}, false
		}
		switch k {
		case "dieu":
			p.Article = v
		case "khoan":
			p.Khoan = v
		case "diem":
			p.Diem = v
		default:
			return LegalProvisionRef{}, false
		}
	}
	return p.normalized(), p.Article != ""
}

// legalEditorOp is the edge operation of an editor code before B's text
// is read (sửa đổi / thay thế become a phrase change when B's provision
// is one).
func legalEditorOp(code int) (LegalAmendOp, bool) {
	switch code {
	case vbpl.EditorCodeSuaDoiBS, vbpl.EditorCodeThayThe:
		return LegalOpSuaDoi, true
	case vbpl.EditorCodeBoSung:
		return LegalOpBoSung, true
	case vbpl.EditorCodeBaiBo:
		return LegalOpBaiBo, true
	case vbpl.EditorCodeGiaiThich:
		return LegalOpGiaiThich, true
	case vbpl.EditorCodeDinhChinh:
		return LegalOpDinhChinh, true
	case vbpl.EditorCodeTamNgung:
		return LegalOpTamNgung, true
	case vbpl.EditorCodeDinhChi:
		return LegalOpDinhChi, true
	}
	return "", false
}

// legalEditorAgrees reports whether a parser operation fits an editor code.
func legalEditorAgrees(code int, op LegalAmendOp) bool {
	switch code {
	case vbpl.EditorCodeSuaDoiBS:
		return op == LegalOpSuaDoi || op == LegalOpBoSung || op == LegalOpThayCumTu || op == LegalOpBaiBoCumTu
	case vbpl.EditorCodeThayThe:
		return op == LegalOpSuaDoi || op == LegalOpThayCumTu
	case vbpl.EditorCodeBoSung:
		return op == LegalOpBoSung || op == LegalOpSuaDoi
	case vbpl.EditorCodeBaiBo:
		return op == LegalOpBaiBo || op == LegalOpBaiBoCumTu
	}
	return false
}

// legalEditorSamePlace reports whether the editor's provision of A is the
// parser edge's: the same provision; for an addition, the parent, the new
// provision or a sibling of it (editors tag the neighbour).
func legalEditorSamePlace(at LegalProvisionRef, e *LegalAmendEdge) bool {
	if e.Op == LegalOpBoSung && e.NewLabel != "" {
		if at == e.Provision || at == legalAddedProvision(e.Provision, e.NewLabel) {
			return true
		}
		parent, _ := legalParentOf(at)
		return parent == e.Provision
	}
	return at == e.Provision
}

// legalOriginsMeet reports whether two places in B are one (or one holds
// the other).
func legalOriginsMeet(a, b LegalProvisionRef) bool {
	if a.Article == "" || b.Article == "" {
		return false
	}
	return a.covers(b) || b.covers(a)
}

// LegalEditorMergeStats counts what the editor merge did, for the log.
type LegalEditorMergeStats struct {
	Links, Agreed, Explained, DateMismatch, EditorOnly, WithText int
}

// editorEdges adds the editor edges of the retrieved documents to load:
// it promotes the parser edges they agree with and adds the rest.
func (l *legalAmendLoader) editorEdges(load *LegalAmendLoad, targets map[string]legalEditorTarget, opt LegalAmendLoadOptions) (LegalEditorMergeStats, error) {
	var st LegalEditorMergeStats
	byID := map[string]string{} // A catalogue id → doc key
	var aIDs []string
	for key, t := range targets {
		if id := t.doc.CatalogID; id != "" && byID[id] == "" {
			byID[id] = key
			aIDs = append(aIDs, id)
		}
	}
	if len(aIDs) == 0 {
		return st, nil
	}
	read := opt.EditorLinks
	if read == nil {
		read = readLegalEditorLinks
	}
	links, err := read(l.ctx, l.db, aIDs)
	if err != nil {
		return st, fmt.Errorf("editor links: %w", err)
	}
	var bIDs []string
	type placed struct {
		link types.LegalProvisionLink
		key  string
		at   LegalProvisionRef
	}
	var list []placed
	for _, lk := range links {
		key, ok := byID[lk.TargetDocID]
		if !ok || lk.SourceDocID == "" {
			continue
		}
		at, ok := legalPathRef(lk.TargetPath)
		if !ok || !targets[key].articles[at.Article] {
			continue
		}
		list = append(list, placed{lk, key, at})
		bIDs = append(bIDs, lk.SourceDocID)
	}
	st.Links = len(list)
	if len(list) == 0 {
		return st, nil
	}
	if err := l.loadCatalog(bIDs, nil); err != nil {
		return st, err
	}
	// 1. agreement with the parser edges
	var alone []placed
	for _, p := range list {
		origin, _ := legalPathRef(p.link.SourcePath)
		eff := l.editorDate(p.link)
		edges := load.Edges[p.key]
		agreed, explained, mismatch := false, false, false
		for i := range edges {
			e := &edges[i]
			if e.Amender.CatalogID != p.link.SourceDocID || !containsString(e.Sources, LegalAmendSourceParser) {
				continue
			}
			if legalEditorAgrees(p.link.RelationCode, e.Op) && legalEditorSamePlace(p.at, e) {
				if eff.Known && e.EffFrom.Known && !eff.At.Equal(e.EffFrom.At) {
					mismatch = true
					continue
				}
				if !containsString(e.Sources, LegalAmendSourceEditor) {
					e.Sources = append(e.Sources, LegalAmendSourceEditor)
				}
				if e.Unresolved == "" && e.Rejected == "" {
					e.Tier = LegalTierVerified
				}
				agreed = true
				continue
			}
			// the parser read the same operation of B at a wider or
			// narrower place of A: it stands, no second edge
			if legalOriginsMeet(origin, e.Origin) && (p.at.covers(e.Provision) || e.Provision.covers(p.at)) {
				explained = true
			}
		}
		switch {
		case agreed:
			st.Agreed++
		case explained:
			st.Explained++
		default:
			if mismatch {
				st.DateMismatch++
			}
			alone = append(alone, p)
		}
	}
	if len(alone) == 0 {
		return st, nil
	}
	// 2. the editor-only edges, with B's source provision text
	var srcIDs []string
	for _, p := range alone {
		srcIDs = append(srcIDs, p.link.SourceProvisionID)
	}
	texts, err := l.editorSourceTexts(bIDs, srcIDs)
	if err != nil {
		return st, err
	}
	seen := map[string]int{}
	for _, p := range alone {
		e := l.editorEdge(p.link, p.at, targets[p.key].doc, texts[p.link.SourceDocID+"|"+p.link.SourceProvisionID])
		if e.QuoteText != "" || e.From != "" || e.SourceText != "" {
			st.WithText++
		}
		// one B provision tagged twice on one provision (type + new-types)
		k := strings.Join([]string{p.key, e.Amender.CatalogID, string(e.Op), e.Provision.key(), e.NewLabel, foldSpaces(e.From), p.link.SourceProvisionID}, "|")
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = len(load.Edges[p.key])
		load.Edges[p.key] = append(load.Edges[p.key], e)
		st.EditorOnly++
	}
	return st, nil
}

// editorDate is the edge's effective date: the editors' own, else B's
// catalogue date.
func (l *legalAmendLoader) editorDate(lk types.LegalProvisionLink) LegalAmendDate {
	if lk.EffFrom != nil {
		return l.dateOf(lk.EffFrom)
	}
	if row := l.catalog[lk.SourceDocID]; row != nil {
		return l.dateOf(row.EffectiveOn)
	}
	return LegalAmendDate{}
}

// legalEditorSource is B's source provision as a chunk of B holds it.
type legalEditorSource struct {
	chunkID, kbID, knowledgeID string
	text                       string // the provision's text in the chunk (quotes included)
	startRune                  int    // where text starts in the chunk content (runes)
	labelled                   bool   // the provision starts in this chunk
	headingPath                []string
}

// editorSourceTexts reads B's source provisions from B's chunks in the
// KBs the question may read, by provision id; keyed "B id|provision id".
func (l *legalAmendLoader) editorSourceTexts(bIDs, srcIDs []string) (map[string]legalEditorSource, error) {
	out := map[string]legalEditorSource{}
	allowed := make([]string, 0, len(l.allowed))
	for id := range l.allowed {
		allowed = append(allowed, id)
	}
	if len(allowed) == 0 || len(bIDs) == 0 {
		return out, nil
	}
	ext := legalJSONText(l.db, "k.metadata", "external_id")
	has := `EXISTS (SELECT 1 FROM jsonb_each_text(c.metadata->'legal'->'provision_ids') p WHERE p.value IN ?)`
	if l.db.Dialector.Name() != "postgres" {
		has = `EXISTS (SELECT 1 FROM json_each(json_extract(CAST(c.metadata AS TEXT), '$.legal.provision_ids')) p WHERE p.value IN ?)`
	}
	var rows []legalAmendRow
	if err := l.db.Raw(legalAmendSelect+` AND `+ext+` IN ? AND c.knowledge_base_id IN ? AND `+has+` ORDER BY c.id LIMIT ?`,
		bIDs, allowed, srcIDs, legalEditorChunksMax).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("editor source texts: %w", err)
	}
	want := map[string]bool{}
	for _, id := range srcIDs {
		want[id] = true
	}
	for _, r := range rows {
		legal, km := parseLegalAmendRow(r)
		if legal == nil {
			continue
		}
		bID := firstNonEmptyString(kmString(km, "external_id"), legal.SourceDocID)
		for key, id := range legal.ProvisionIDs {
			if !want[id] {
				continue
			}
			text, start, labelled, ok := legalProvisionSpan(r.Content, legal, key)
			if !ok {
				continue
			}
			k := bID + "|" + id
			// the chunk where the provision starts wins over a
			// continuation; the first allowed copy otherwise
			if have, dup := out[k]; dup && (have.labelled || !labelled) {
				continue
			}
			out[k] = legalEditorSource{chunkID: r.ID, kbID: r.KnowledgeBaseID, knowledgeID: r.KnowledgeID,
				text: text, startRune: start, labelled: labelled, headingPath: legal.HeadingPath}
		}
	}
	return out, nil
}

// legalProvisionSpan is the text of the chunk's provision key ("dieu",
// "khoan:2", "khoan:2/diem:a") with its rune offset; labelled reports that
// it starts in this chunk.
func legalProvisionSpan(content string, legal *vietnamese_legal.LegalMetadata, key string) (text string, startRune int, labelled, ok bool) {
	if len(legal.ArticleNos) != 1 {
		return "", 0, false, false
	}
	want := LegalProvisionRef{Article: strings.ToLower(legal.ArticleNos[0])}
	if key != "dieu" {
		p, ok := parseSubdivisionRef(want.Article, key)
		if !ok {
			return "", 0, false, false
		}
		want = p
	}
	units := legalUnits(content, legal)
	first, last := -1, -1
	for i, u := range units {
		if u.prov.Article != "" && want.covers(u.prov) {
			if first < 0 {
				first = i
			}
			last = i
		} else if first >= 0 {
			break
		}
	}
	if first < 0 {
		return "", 0, false, false
	}
	s, e := units[first].start, units[last].end
	raw := content[s:e]
	text = strings.TrimSpace(raw)
	lead := strings.Index(raw, text)
	return text, utf8.RuneCountInString(content[:s+lead]), units[first].labelled && units[first].prov == want, true
}

// editorEdge builds the edge of one editor link alone.
func (l *legalAmendLoader) editorEdge(lk types.LegalProvisionLink, at LegalProvisionRef, target LegalAmendDoc, src legalEditorSource) LegalAmendEdge {
	op, _ := legalEditorOp(lk.RelationCode)
	e := LegalAmendEdge{Op: op, Target: target, Provision: at, Sources: []string{LegalAmendSourceEditor}, Tier: LegalTierUnverified,
		EffFrom: l.editorDate(lk), EditorProvisionID: lk.SourceProvisionID}
	if lk.EffTo != nil {
		e.EffTo = l.dateOf(lk.EffTo)
	}
	e.Origin, _ = legalPathRef(lk.SourcePath)
	if row := l.catalog[lk.SourceDocID]; row != nil {
		e.Amender = l.catalogDoc(row)
		if l.neverInForce(row) {
			e.Unresolved = "danh mục ghi văn bản sửa đổi hết hiệu lực trước hoặc ngay khi có hiệu lực"
		}
	} else {
		e.Amender = LegalAmendDoc{CatalogID: lk.SourceDocID}
		e.Unresolved = "không xác định được văn bản sửa đổi trong danh mục"
	}
	switch op {
	case LegalOpGiaiThich, LegalOpDinhChinh:
		// no change to the text: an informational line
		return e
	case LegalOpTamNgung, LegalOpDinhChi:
		// keeps the text; the editors' tag is the operation (no wording
		// to verify): the interval state applies as for a tier-1 edge
		if e.Unresolved == "" {
			e.Tier = LegalTierVerified
		}
		return e
	}
	if src.chunkID != "" {
		e.Amender.ChunkID, e.Amender.KnowledgeBaseID, e.Amender.KnowledgeID = src.chunkID, src.kbID, src.knowledgeID
		l.editorText(&e, lk.RelationCode, src)
	} else {
		// B is not read in a KB the question may use: its words are not
		// available (§10.2)
		e.TextWithheld = true
	}
	// the static checks, never rejecting a human tag: a failure only
	// leaves the provision unresolved
	check := e
	ValidateLegalAmendEdge(&check, nil)
	if reason := firstNonEmptyString(check.Rejected, check.Unresolved); reason != "" && e.Unresolved == "" {
		e.Unresolved = reason
	}
	e.Tier = LegalTierUnverified
	return e
}

// editorText reads the change from B's source provision: the quoted new
// text for A's provision, a phrase change, else the provision's own words.
func (l *legalAmendLoader) editorText(e *LegalAmendEdge, code int, src legalEditorSource) {
	groups := legalQuoteGroups(src.text)
	setQuote := func(g legalQuoteGroup, b legalQuoteBlock) {
		e.QuoteText = b.text
		e.Quote = &LegalRuneSpan{src.startRune + b.span.Start, src.startRune + b.span.End}
		e.LeadInText = g.leadIn
		e.LeadIn = &LegalRuneSpan{src.startRune + g.leadInSpan.Start, src.startRune + g.leadInSpan.End}
	}
	if e.Op != LegalOpBaiBo {
		if g, b, _, ok := legalQuoteFor(groups, e.Provision, ""); ok {
			setQuote(g, b)
			return
		}
		// an addition: the quote's label is a child or a sibling of the
		// tagged provision
		if len(groups) == 1 && len(groups[0].blocks) == 1 && (code == vbpl.EditorCodeBoSung || code == vbpl.EditorCodeSuaDoiBS) {
			g, b := groups[0], groups[0].blocks[0]
			if parent, label, ok := legalAddedPlace(e.Provision, b.label); ok {
				e.Op, e.Provision, e.NewLabel = LegalOpBoSung, parent, label
				setQuote(g, b)
				return
			}
		}
	}
	// a phrase change written in B's provision
	var phrases []vbpl.Amend
	for _, a := range vbpl.PhraseAmendsInChunk(src.text, src.headingPath, "") {
		if a.Phrase {
			phrases = append(phrases, a)
		}
	}
	if len(phrases) > 0 {
		pick := -1
		for i, a := range phrases {
			if (LegalProvisionRef{Article: a.Article, Khoan: a.Khoan, Diem: a.Diem}).normalized() == e.Provision {
				pick = i
				break
			}
		}
		same := true
		for _, a := range phrases {
			same = same && a.From == phrases[0].From && a.To == phrases[0].To
		}
		if pick < 0 && same {
			pick = 0
		}
		if pick >= 0 && (e.Op != LegalOpBaiBo || phrases[pick].To == "") {
			a := phrases[pick]
			e.Op, e.From, e.To = LegalOpThayCumTu, a.From, a.To
			if a.To == "" {
				e.Op = LegalOpBaiBoCumTu
			}
			return
		}
	}
	if e.Op == LegalOpBaiBo {
		e.LeadInText = legalFirstLine(src.text)
		return
	}
	// no quote, no phrase: B's own words, shown as such
	e.SourceText = legalCapRunes(src.text, legalEditorSourceRunes)
	e.LeadInText = legalFirstLine(src.text)
}

// legalAddedPlace places a quoted label ("2a.", "c)") relative to the
// tagged provision p: a child of p, or a sibling (the editors tag the
// neighbour); ok is false otherwise.
func legalAddedPlace(p LegalProvisionRef, quoteLabel string) (parent LegalProvisionRef, label string, ok bool) {
	label = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(quoteLabel, "điều "), "."), ")")
	if label == "" {
		return p, "", false
	}
	article := strings.HasPrefix(quoteLabel, "điều ")
	khoan := !article && strings.HasSuffix(quoteLabel, ".")
	diem := strings.HasSuffix(quoteLabel, ")")
	switch {
	case p.Khoan == "" && khoan, p.Khoan != "" && p.Diem == "" && diem:
		return p, label, true // child
	case p.Khoan == "" && article:
		return LegalProvisionRef{}, label, true // a new Điều next to p
	case p.Khoan != "" && p.Diem == "" && khoan, p.Diem != "" && diem:
		parent, own := legalParentOf(p)
		return parent, label, label != own
	}
	return p, "", false
}

func legalFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// legalCapRunes cuts s to n runes at a space.
func legalCapRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if k := strings.LastIndexByte(cut, ' '); k > len(cut)/2 {
		cut = cut[:k]
	}
	return cut + " …"
}

// legalEditorLinksFromJSON reads importer rows (cmd/legal-provision-links
// -out) as an EditorLinks source, for offline probes.
func legalEditorLinksFromJSON(lines [][]byte) func(ctx context.Context, db *gorm.DB, aIDs []string) ([]types.LegalProvisionLink, error) {
	by := map[string][]types.LegalProvisionLink{}
	for _, l := range lines {
		var r types.LegalProvisionLink
		if json.Unmarshal(l, &r) == nil {
			by[r.TargetDocID] = append(by[r.TargetDocID], r)
		}
	}
	return func(_ context.Context, _ *gorm.DB, aIDs []string) ([]types.LegalProvisionLink, error) {
		var out []types.LegalProvisionLink
		for _, id := range aIDs {
			for _, r := range by[id] {
				if r.SourceDocID != "" && r.TargetPath != "" {
					out = append(out, r)
				}
			}
		}
		return out, nil
	}
}
