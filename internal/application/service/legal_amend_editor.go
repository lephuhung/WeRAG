package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/legalcatalog"
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

// LegalFeedSourceSetting is the system setting adding the provision links
// pushed by other feeds (POST /system/legal-documents/push) and approved
// by a SuperAdmin; they are read as the editors' tags are, named by their
// feed (legal_provision_links.source, e.g. "tvpl").
const LegalFeedSourceSetting = "legal.current_wording.feed_source"

// legalFeedSourceDefault is the setting's value when unset.
const legalFeedSourceDefault = false

// legalLinkSource is the edge source of a link: its feed, the editors'
// tags when unnamed.
func legalLinkSource(lk types.LegalProvisionLink) string {
	if lk.Source == "" {
		return LegalAmendSourceEditor
	}
	return lk.Source
}

// legalIsTagSource reports whether an edge source is a human tag (the vbpl
// editors or an approved feed), not one of the system's own readings.
func legalIsTagSource(s string) bool {
	switch s {
	case "", LegalAmendSourceParser, LegalAmendSourceCatalogue, LegalAmendSourceChain:
		return false
	}
	return true
}

// legalHasTagSource reports whether sources hold a human tag.
func legalHasTagSource(sources []string) bool {
	for _, s := range sources {
		if legalIsTagSource(s) {
			return true
		}
	}
	return false
}

// legalLinkWanted reports whether the load reads a link of this source.
func legalLinkWanted(lk types.LegalProvisionLink, opt LegalAmendLoadOptions) bool {
	if legalLinkSource(lk) == LegalAmendSourceEditor {
		return opt.EditorSource
	}
	return opt.FeedSource
}

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
	// FeedSource adds the approved feed links (LegalFeedSourceSetting).
	FeedSource bool
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
	opt := LegalAmendLoadOptions{EditorSource: legalEditorSourceOn(ctx, settings)}
	if settings != nil {
		opt.FeedSource = settings.GetBool(ctx, LegalFeedSourceSetting, "", legalFeedSourceDefault)
	}
	return opt
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
	// structure is A's provisions as its chunks record them (nil: unknown)
	structure LegalDocStructure
	// repeated: the Điều numbers A's chunks record under more than one
	// vbpl id (a Quy chế's "Điều 4" next to the decree's own)
	repeated legalRepeated
}

// editorTarget is the provision of A an editor link tags.
func (t legalEditorTarget) editorTarget(lk types.LegalProvisionLink, at LegalProvisionRef) LegalEditorTargetRef {
	ref := LegalEditorTargetRef{ID: lk.TargetProvisionID, Key: legalChunkKey(at), Article: at.Article, Repeated: t.repeated.articles[at.Article]}
	if legalcatalog.IsSyntheticProvisionID(ref.ID) {
		// a made-up id (a feed's link, a provision no portal copy named)
		// is no chunk's id: the provision is matched by its number only
		ref.ID = ""
		return ref
	}
	if ref.Repeated {
		ref.DieuID = t.repeated.dieuOf[lk.TargetProvisionID]
		if ref.Key == "dieu" {
			ref.DieuID = ref.ID
		}
	}
	return ref
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
	case vbpl.EditorCodeSuaDoiBS, vbpl.EditorCodeThayThe, vbpl.AmendCodeSuaDoi:
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
	case vbpl.AmendCodeSuaDoi:
		return op == LegalOpSuaDoi || op == LegalOpThayCumTu || op == LegalOpBaiBoCumTu
	case vbpl.EditorCodeThayThe:
		return op == LegalOpSuaDoi || op == LegalOpThayCumTu
	case vbpl.EditorCodeBoSung:
		return op == LegalOpBoSung || op == LegalOpSuaDoi
	case vbpl.EditorCodeBaiBo:
		return op == LegalOpBaiBo || op == LegalOpBaiBoCumTu
	}
	return false
}

// legalEditorExact reports whether a parser operation agreeing with an
// editor code is the same operation, not a narrower one: a bãi bỏ tag on
// a phrase removal, a bổ sung tag on a rewrite. Only an exact agreement
// makes the edge tier 1; a loose one leaves its tier.
func legalEditorExact(code int, op LegalAmendOp) bool {
	switch code {
	case vbpl.EditorCodeBoSung:
		return op == LegalOpBoSung
	case vbpl.EditorCodeBaiBo:
		return op == LegalOpBaiBo
	}
	return legalEditorAgrees(code, op)
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
	if load.Took == nil {
		load.Took = map[string]time.Duration{}
	}
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
	t0 := time.Now()
	links, err := read(l.ctx, l.db, aIDs)
	load.Took["editor_links"] += time.Since(t0)
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
		if !ok || lk.SourceDocID == "" || !legalLinkWanted(lk, opt) {
			continue
		}
		if _, known := legalEditorOp(lk.RelationCode); !known {
			continue // e.g. 9 hướng dẫn: kept for review pages, no operation here
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
				if src := legalLinkSource(p.link); !containsString(e.Sources, src) {
					e.Sources = append(e.Sources, src)
				}
				if e.Unresolved == "" && e.Rejected == "" && legalEditorExact(p.link.RelationCode, e.Op) {
					switch {
					case e.Tier != LegalTierVerified:
						e.preEditorTier, e.Tier = e.Tier, LegalTierVerified
						e.EditorTargets = append(e.EditorTargets, targets[p.key].editorTarget(p.link, p.at))
					case e.preEditorTier != "":
						// verified by an earlier link: this one tags another
						// provision under the same number, which holds it too
						e.EditorTargets = append(e.EditorTargets, targets[p.key].editorTarget(p.link, p.at))
					}
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
	t0 = time.Now()
	texts, err := l.editorSourceTexts(bIDs, srcIDs)
	if err == nil {
		var misses []types.LegalProvisionLink
		for _, p := range alone {
			if _, ok := texts[p.link.SourceDocID+"|"+p.link.SourceProvisionID]; !ok {
				misses = append(misses, p.link)
			}
		}
		err = l.editorSourceTextsByPath(misses, texts)
	}
	load.Took["editor_texts"] += time.Since(t0)
	if err != nil {
		return st, err
	}
	t0 = time.Now()
	defer func() { load.Took["editor_build"] += time.Since(t0) }()
	seen := map[string]int{}
	for _, p := range alone {
		e := l.editorEdge(p.link, p.at, targets[p.key], texts[p.link.SourceDocID+"|"+p.link.SourceProvisionID])
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
	// B's knowledges first (index on KB + external id), then their chunks
	// (index on knowledge): one join lets the planner scan chunks
	var kids []string
	if err := l.db.Raw(`SELECT k.id FROM knowledges k WHERE k.deleted_at IS NULL AND `+ext+` IN ? AND k.knowledge_base_id IN ?`,
		bIDs, allowed).Scan(&kids).Error; err != nil {
		return nil, fmt.Errorf("editor source documents: %w", err)
	}
	if len(kids) == 0 {
		return out, nil
	}
	var rows []legalAmendRow
	if err := l.db.Raw(legalAmendSelect+` AND c.knowledge_id IN ? AND c.knowledge_base_id IN ? AND `+has+` ORDER BY c.id LIMIT ?`,
		kids, allowed, srcIDs, legalEditorChunksMax).Scan(&rows).Error; err != nil {
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

// editorSourceTextsByPath finds, by source_path, the B provisions
// editorSourceTexts could not find by id, in copies of B whose chunks carry
// no provision ids (a legal feed's text: processVBPLFile keeps no ids for
// it); added to out under the link's "B id|provision id" key. A copy with
// portal ids is never read by path: there another id means another
// provision.
func (l *legalAmendLoader) editorSourceTextsByPath(links []types.LegalProvisionLink, out map[string]legalEditorSource) error {
	type want struct{ key, article, chunkKey string }
	byDoc := map[string][]want{}
	var bIDs []string
	for _, lk := range links {
		at, ok := legalPathRef(lk.SourcePath)
		if !ok || lk.SourceDocID == "" {
			continue
		}
		if _, seen := byDoc[lk.SourceDocID]; !seen {
			bIDs = append(bIDs, lk.SourceDocID)
		}
		byDoc[lk.SourceDocID] = append(byDoc[lk.SourceDocID],
			want{lk.SourceDocID + "|" + lk.SourceProvisionID, at.Article, legalChunkKey(at)})
	}
	allowed := make([]string, 0, len(l.allowed))
	for id := range l.allowed {
		allowed = append(allowed, id)
	}
	if len(bIDs) == 0 || len(allowed) == 0 {
		return nil
	}
	ext := legalJSONText(l.db, "k.metadata", "external_id")
	src := legalJSONText(l.db, "k.metadata", "source")
	var kids []string
	if err := l.db.Raw(`SELECT k.id FROM knowledges k WHERE k.deleted_at IS NULL AND `+ext+` IN ? AND k.knowledge_base_id IN ? AND `+
		`COALESCE(`+src+`, '') NOT IN ('', 'vbpl')`, bIDs, allowed).Scan(&kids).Error; err != nil {
		return fmt.Errorf("editor source documents by path: %w", err)
	}
	if len(kids) == 0 {
		return nil
	}
	var rows []legalAmendRow
	if err := l.db.Raw(legalAmendSelect+` AND c.knowledge_id IN ? AND c.knowledge_base_id IN ? ORDER BY c.id LIMIT ?`,
		kids, allowed, legalEditorChunksMax).Scan(&rows).Error; err != nil {
		return fmt.Errorf("editor source texts by path: %w", err)
	}
	for _, r := range rows {
		legal, km := parseLegalAmendRow(r)
		if legal == nil || len(legal.ProvisionIDs) > 0 || len(legal.ArticleNos) != 1 {
			continue
		}
		bID := firstNonEmptyString(kmString(km, "external_id"), legal.SourceDocID)
		for _, w := range byDoc[bID] {
			if !strings.EqualFold(legal.ArticleNos[0], w.article) || w.chunkKey == "" {
				continue
			}
			text, start, labelled, ok := legalProvisionSpan(r.Content, legal, w.chunkKey)
			if !ok {
				continue
			}
			if have, dup := out[w.key]; dup && (have.labelled || !labelled) {
				continue
			}
			out[w.key] = legalEditorSource{chunkID: r.ID, kbID: r.KnowledgeBaseID, knowledgeID: r.KnowledgeID,
				text: text, startRune: start, labelled: labelled, headingPath: legal.HeadingPath}
		}
	}
	return nil
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
func (l *legalAmendLoader) editorEdge(lk types.LegalProvisionLink, at LegalProvisionRef, t legalEditorTarget, src legalEditorSource) LegalAmendEdge {
	op, _ := legalEditorOp(lk.RelationCode)
	e := LegalAmendEdge{Op: op, Target: t.doc, Provision: at, Sources: []string{legalLinkSource(lk)}, Tier: LegalTierUnverified,
		EffFrom: l.editorDate(lk), EditorProvisionID: lk.SourceProvisionID,
		EditorTargets: []LegalEditorTargetRef{t.editorTarget(lk, at)}}
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
		// a "new" label A already has is a rewrite of it (the editors tag
		// the Điều whose khoản B rewrites)
		if e.Op == LegalOpBoSung && e.NewLabel != "" && len(t.structure) > 0 {
			if added := legalAddedProvision(e.Provision, e.NewLabel); t.structure.has(added) {
				e.Op, e.Provision, e.NewLabel = LegalOpSuaDoi, added, ""
			}
		}
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
	for _, a := range vbpl.PhraseAmendsInChunk(src.text, src.headingPath, "", "") {
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
