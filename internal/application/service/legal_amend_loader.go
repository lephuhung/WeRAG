package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Loading the edges that exist today (plan step 3): the vbpl converter's
// chunks.metadata.legal.amends on B's chunks, joined with the legal
// catalogue for identity (plan §4.13) and effective dates. Read-only.
//
// Tiers until the Qwen extraction exists: a phrase change is
// LegalTierLegacyParserPhrase (applied to the wording as before, labelled
// with its source); a quoted rewrite is tier 2 (one source). Tier 1 needs
// two measured, agreeing sources (§3.3, §6) — none exist yet.

const (
	// legalAmendCandidates bounds the amending chunks read per call.
	legalAmendCandidates = 300
	// legalAmendChainDocs bounds the documents amending an amender (C → B).
	legalAmendChainDocs = 20
)

// LegalAmendLoad is what the resolver needs for a set of retrieved chunks.
type LegalAmendLoad struct {
	Chunks []LegalProvisionChunk
	// Edges by A's key (legalDocKey: catalogue id, else number key).
	Edges map[string][]LegalAmendEdge
	// Catalogue relations "B amends A", by A's key.
	Catalogue map[string][]LegalCatalogAmendment
}

// legalDocKey keys a document by catalogue id, else by its number.
func legalDocKey(d LegalAmendDoc) string {
	if d.CatalogID != "" {
		return d.CatalogID
	}
	return "num:" + vietnamese_legal.CatalogNumberKey(d.Number)
}

type legalAmendRow struct {
	ID              string
	KnowledgeID     string
	KnowledgeBaseID string
	Content         string
	Metadata        []byte
	KMetadata       []byte
}

type legalCatalogRow struct {
	ID             string
	DocumentNumber string
	NumberKey      string
	DocType        string
	Title          string
	IssuedOn       *time.Time
	EffectiveOn    *time.Time
}

const legalAmendSelect = `SELECT c.id, c.knowledge_id, c.knowledge_base_id, c.content, c.metadata, k.metadata AS k_metadata
FROM chunks c JOIN knowledges k ON k.id = c.knowledge_id
WHERE c.deleted_at IS NULL AND k.deleted_at IS NULL`

// legalJSONText is the SQL text of a JSON path on col for the dialect.
func legalJSONText(db *gorm.DB, col string, path ...string) string {
	if db.Dialector.Name() == "postgres" {
		expr := col
		for i, p := range path {
			if i == len(path)-1 {
				expr += "->>'" + p + "'"
			} else {
				expr += "->'" + p + "'"
			}
		}
		return expr
	}
	return "json_extract(CAST(" + col + " AS TEXT), '$." + strings.Join(path, ".") + "')"
}

// legalAmendLoader carries one load's lookups.
type legalAmendLoader struct {
	ctx     context.Context
	db      *gorm.DB
	loc     *time.Location
	allowed map[string]bool
	catalog map[string]*legalCatalogRow   // by id
	byKey   map[string][]*legalCatalogRow // by number key
}

// LoadLegalAmendments reads the chunks chunkIDs (within scopeKBs) of
// legal documents and the edges touching their documents. allowedKBs are
// the KBs whose amending text the model may read (§10.2: the question's
// KBs plus the public platform KBs); amending chunks are read from those
// and from vbpl copies anywhere — the latter only for the fact and the
// number of B, their text is withheld.
func LoadLegalAmendments(ctx context.Context, db *gorm.DB, scopeKBs, chunkIDs, allowedKBs []string, loc *time.Location) (*LegalAmendLoad, error) {
	if loc == nil {
		loc = time.UTC
	}
	l := &legalAmendLoader{ctx: ctx, db: db.WithContext(ctx), loc: loc, allowed: map[string]bool{},
		catalog: map[string]*legalCatalogRow{}, byKey: map[string][]*legalCatalogRow{}}
	for _, id := range allowedKBs {
		l.allowed[id] = true
	}
	load := &LegalAmendLoad{Edges: map[string][]LegalAmendEdge{}, Catalogue: map[string][]LegalCatalogAmendment{}}
	if len(scopeKBs) == 0 || len(chunkIDs) == 0 {
		return load, nil
	}
	var rows []legalAmendRow
	if err := l.db.Raw(legalAmendSelect+` AND c.knowledge_base_id IN ? AND c.id IN ?`, scopeKBs, chunkIDs).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load chunks: %w", err)
	}
	order := map[string]int{}
	for i, id := range chunkIDs {
		order[id] = i
	}
	sort.SliceStable(rows, func(i, j int) bool { return order[rows[i].ID] < order[rows[j].ID] })

	type aDoc struct {
		doc       LegalAmendDoc
		knowledge string
		articles  map[string]bool
	}
	docs := map[string]*aDoc{}
	var ids, keys []string
	parsed := make([]struct {
		row   legalAmendRow
		legal *vietnamese_legal.LegalMetadata
		km    map[string]interface{}
	}, 0, len(rows))
	for _, r := range rows {
		legal, km := parseLegalAmendRow(r)
		if legal == nil || len(legal.ArticleNos) == 0 || kmString(km, "doc_number") == "" {
			continue
		}
		parsed = append(parsed, struct {
			row   legalAmendRow
			legal *vietnamese_legal.LegalMetadata
			km    map[string]interface{}
		}{r, legal, km})
		if id := firstNonEmptyString(legal.SourceDocID, kmString(km, "external_id")); id != "" {
			ids = append(ids, id)
		}
		keys = append(keys, vietnamese_legal.CatalogNumberKey(kmString(km, "doc_number")))
	}
	if err := l.loadCatalog(ids, keys); err != nil {
		return nil, err
	}
	for _, p := range parsed {
		c := LegalProvisionChunk{ID: p.row.ID, KnowledgeID: p.row.KnowledgeID, KnowledgeBaseID: p.row.KnowledgeBaseID,
			Content: p.row.Content, Legal: p.legal}
		number := kmString(p.km, "doc_number")
		c.Doc = LegalAmendDoc{Number: number, Type: kmString(p.km, "doc_type"), KnowledgeID: p.row.KnowledgeID}
		src, ext := p.legal.SourceDocID, kmString(p.km, "external_id")
		switch {
		case src != "" && ext != "" && src != ext:
			c.IdentityProblem = "mã vbpl của đoạn và của văn bản khác nhau"
		case firstNonEmptyString(src, ext) == "":
			c.IdentityProblem = "văn bản không có mã vbpl"
		default:
			id := firstNonEmptyString(src, ext)
			row := l.catalog[id]
			switch {
			case row == nil:
				c.IdentityProblem = "mã vbpl " + id + " không có trong danh mục"
			case row.NumberKey != vietnamese_legal.CatalogNumberKey(number):
				c.IdentityProblem = "số hiệu " + number + " không khớp danh mục (" + row.DocumentNumber + ")"
			default:
				c.Doc.CatalogID = id
				if c.Doc.Type == "" {
					c.Doc.Type = row.DocType
				}
			}
		}
		load.Chunks = append(load.Chunks, c)
		key := legalDocKey(c.Doc)
		d := docs[key]
		if d == nil {
			d = &aDoc{doc: c.Doc, knowledge: c.KnowledgeID, articles: map[string]bool{}}
			docs[key] = d
		}
		for _, a := range p.legal.ArticleNos {
			d.articles[strings.ToLower(a)] = true
		}
	}
	if len(docs) == 0 {
		return load, nil
	}

	// catalogue relations B amends A
	var aIDs []string
	for _, d := range docs {
		if d.doc.CatalogID != "" {
			aIDs = append(aIDs, d.doc.CatalogID)
		}
	}
	amenders := map[string][]string{} // A id → B ids
	var bIDs []string
	if len(aIDs) > 0 {
		var rels []struct{ FromID, ToID string }
		if err := l.db.Raw(`SELECT from_id, to_id FROM legal_document_relations WHERE relation = ? AND to_id IN ?`,
			string(types.LegalRelationAmends), aIDs).Scan(&rels).Error; err != nil {
			return nil, fmt.Errorf("catalogue relations: %w", err)
		}
		for _, r := range rels {
			amenders[r.ToID] = append(amenders[r.ToID], r.FromID)
			bIDs = append(bIDs, r.FromID)
		}
		if err := l.loadCatalog(bIDs, nil); err != nil {
			return nil, err
		}
		for aID, bs := range amenders {
			for _, b := range bs {
				if row := l.catalog[b]; row != nil {
					load.Catalogue[aID] = append(load.Catalogue[aID], LegalCatalogAmendment{
						Amender: l.catalogDoc(row), EffFrom: l.dateOf(row.EffectiveOn)})
				}
			}
		}
	}

	// amending chunks: those of catalogue amenders, and those naming an
	// article of a retrieved document
	var articles []string
	var aKnowledge []string
	for _, d := range docs {
		aKnowledge = append(aKnowledge, d.knowledge)
		for a := range d.articles {
			articles = append(articles, a)
		}
	}
	cands, err := l.amendingChunks(bIDs, articles, aKnowledge)
	if err != nil {
		return nil, err
	}

	// structure of A, for the target checks
	structures := map[string]LegalDocStructure{}
	for key, d := range docs {
		s, err := l.structure(d.knowledge)
		if err != nil {
			return nil, err
		}
		structures[key] = s
	}

	targets := map[string]LegalAmendDoc{}
	for key, d := range docs {
		targets[key] = d.doc
	}
	all := l.edgesFrom(cands, targets, amenders, structures)
	for key, list := range all {
		load.Edges[key] = list
	}

	// one hop back: documents amending the amenders (C → B → A)
	var amenderIDs []string
	seenB := map[string]bool{}
	for _, list := range all {
		for _, e := range list {
			if id := e.Amender.CatalogID; id != "" && !seenB[id] && e.Origin.Article != "" {
				seenB[id] = true
				amenderIDs = append(amenderIDs, id)
			}
		}
	}
	if len(amenderIDs) > 0 {
		if err := l.chainHop(amenderIDs, load); err != nil {
			return nil, err
		}
	}
	return load, nil
}

// chainHop derives C → A edges for the amenders amenderIDs of A.
func (l *legalAmendLoader) chainHop(amenderIDs []string, load *LegalAmendLoad) error {
	var rels []struct{ FromID, ToID string }
	if err := l.db.Raw(`SELECT from_id, to_id FROM legal_document_relations WHERE relation = ? AND to_id IN ? LIMIT ?`,
		string(types.LegalRelationAmends), amenderIDs, legalAmendChainDocs).Scan(&rels).Error; err != nil {
		return fmt.Errorf("chain relations: %w", err)
	}
	if len(rels) == 0 {
		return nil
	}
	var cIDs []string
	bAmenders := map[string][]string{}
	for _, r := range rels {
		cIDs = append(cIDs, r.FromID)
		bAmenders[r.ToID] = append(bAmenders[r.ToID], r.FromID)
	}
	if err := l.loadCatalog(append(cIDs, amenderIDs...), nil); err != nil {
		return err
	}
	cands, err := l.amendingChunks(cIDs, nil, nil)
	if err != nil {
		return err
	}
	bTargets := map[string]LegalAmendDoc{}
	for _, id := range amenderIDs {
		if row := l.catalog[id]; row != nil {
			bTargets[id] = l.catalogDoc(row)
		}
	}
	cEdges := l.edgesFrom(cands, bTargets, bAmenders, nil)
	for aKey, list := range load.Edges {
		for bID, onB := range cEdges {
			var fromB []LegalAmendEdge
			for _, e := range list {
				if e.Amender.CatalogID == bID {
					fromB = append(fromB, e)
				}
			}
			if len(fromB) > 0 {
				load.Edges[aKey] = append(load.Edges[aKey], TraceLegalAmendChain(onB, fromB)...)
			}
		}
	}
	return nil
}

// amendingChunks reads candidate amending chunks: those of the documents
// docIDs, and (Postgres) those whose amends name one of articles; never
// from the knowledges skip (A itself). Only allowed KBs or vbpl copies.
func (l *legalAmendLoader) amendingChunks(docIDs, articles, skip []string) ([]legalAmendRow, error) {
	scope := `(c.knowledge_base_id IN ? OR ` + legalJSONText(l.db, "c.metadata", "legal", "source") + ` = 'vbpl')`
	allowed := make([]string, 0, len(l.allowed)+1)
	for id := range l.allowed {
		allowed = append(allowed, id)
	}
	allowed = append(allowed, "") // IN () is invalid SQL
	notSkip := ""
	var skipArgs []interface{}
	if len(skip) > 0 {
		notSkip = ` AND c.knowledge_id NOT IN ?`
		skipArgs = append(skipArgs, skip)
	}
	seen := map[string]bool{}
	var out []legalAmendRow
	add := func(rows []legalAmendRow) {
		for _, r := range rows {
			if !seen[r.ID] {
				seen[r.ID] = true
				out = append(out, r)
			}
		}
	}
	pg := l.db.Dialector.Name() == "postgres"
	hasAmends := legalJSONText(l.db, "c.metadata", "legal", "amends") + ` IS NOT NULL`
	if pg {
		hasAmends = `jsonb_exists(c.metadata->'legal', 'amends')`
	}
	if len(docIDs) > 0 {
		var rows []legalAmendRow
		args := append([]interface{}{docIDs, allowed}, skipArgs...)
		args = append(args, legalAmendCandidates)
		if err := l.db.Raw(legalAmendSelect+` AND `+legalJSONText(l.db, "k.metadata", "external_id")+` IN ? AND `+scope+notSkip+` AND `+hasAmends+`
			ORDER BY c.knowledge_id, c.chunk_index LIMIT ?`, args...).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("amending chunks of amenders: %w", err)
		}
		add(rows)
	}
	if len(articles) > 0 {
		var conds []string
		var args []interface{}
		if pg {
			for _, a := range articles {
				filter, _ := json.Marshal([]map[string]string{{"article": a}})
				conds = append(conds, `c.metadata->'legal'->'amends' @> ?::jsonb`)
				args = append(args, string(filter))
			}
		} else {
			conds = append(conds, hasAmends)
		}
		var rows []legalAmendRow
		all := append(args, allowed)
		all = append(all, skipArgs...)
		all = append(all, legalAmendCandidates)
		if err := l.db.Raw(legalAmendSelect+` AND `+hasAmends+` AND (`+strings.Join(conds, " OR ")+`) AND `+scope+notSkip+`
			ORDER BY c.id LIMIT ?`, all...).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("amending chunks by article: %w", err)
		}
		add(rows)
	}
	return out, nil
}

// structure reads the provisions of knowledge kid from its chunks.
func (l *legalAmendLoader) structure(kid string) (LegalDocStructure, error) {
	var metas []struct{ Metadata []byte }
	if err := l.db.Raw(`SELECT c.metadata FROM chunks c WHERE c.deleted_at IS NULL AND c.knowledge_id = ?`, kid).Scan(&metas).Error; err != nil {
		return nil, fmt.Errorf("structure: %w", err)
	}
	var list []*vietnamese_legal.LegalMetadata
	for _, m := range metas {
		var meta types.DocumentChunkMetadata
		if json.Unmarshal(m.Metadata, &meta) == nil && meta.Legal != nil {
			list = append(list, meta.Legal)
		}
	}
	return legalStructureFrom(list), nil
}

// loadCatalog reads catalogue rows by id and by number key into l.
func (l *legalAmendLoader) loadCatalog(ids, keys []string) error {
	var needIDs []string
	for _, id := range ids {
		if _, ok := l.catalog[id]; !ok && id != "" {
			needIDs = append(needIDs, id)
		}
	}
	var needKeys []string
	for _, k := range keys {
		if _, ok := l.byKey[k]; !ok && k != "" {
			needKeys = append(needKeys, k)
			l.byKey[k] = nil
		}
	}
	var rows []legalCatalogRow
	const sel = `SELECT id, document_number, number_key, doc_type, title, issued_on, effective_on FROM legal_documents WHERE `
	if len(needIDs) > 0 {
		if err := l.db.Raw(sel+`id IN ?`, needIDs).Scan(&rows).Error; err != nil {
			return fmt.Errorf("catalogue by id: %w", err)
		}
	}
	if len(needKeys) > 0 {
		var more []legalCatalogRow
		if err := l.db.Raw(sel+`number_key IN ?`, needKeys).Scan(&more).Error; err != nil {
			return fmt.Errorf("catalogue by number: %w", err)
		}
		rows = append(rows, more...)
	}
	for i := range rows {
		r := &rows[i]
		if _, ok := l.catalog[r.ID]; ok {
			continue
		}
		l.catalog[r.ID] = r
		l.byKey[r.NumberKey] = append(l.byKey[r.NumberKey], r)
	}
	for _, id := range needIDs {
		if _, ok := l.catalog[id]; !ok {
			l.catalog[id] = nil
		}
	}
	return nil
}

func (l *legalAmendLoader) catalogDoc(r *legalCatalogRow) LegalAmendDoc {
	return LegalAmendDoc{CatalogID: r.ID, Number: r.DocumentNumber, Type: r.DocType}
}

func (l *legalAmendLoader) dateOf(t *time.Time) LegalAmendDate {
	if t == nil || t.IsZero() {
		return LegalAmendDate{}
	}
	return legalDay(t.Format("2006-01-02"), l.loc)
}

// edgesFrom builds the edges of the amending chunks cands on the target
// documents (by legalDocKey). amenders lists, by target catalogue id, the
// documents the catalogue records amending it: an operation naming no
// document attaches to the target only through it.
func (l *legalAmendLoader) edgesFrom(cands []legalAmendRow, targets map[string]LegalAmendDoc,
	amenders map[string][]string, structures map[string]LegalDocStructure,
) map[string][]LegalAmendEdge {
	out := map[string][]LegalAmendEdge{}
	seen := map[string]bool{}
	byNumber := map[string]string{}
	for key, d := range targets {
		byNumber[vietnamese_legal.CatalogNumberKey(d.Number)] = key
	}
	for _, r := range cands {
		legal, km := parseLegalAmendRow(r)
		if legal == nil || len(legal.Amends) == 0 {
			continue
		}
		b := LegalAmendDoc{Number: kmString(km, "doc_number"), Type: kmString(km, "doc_type"),
			KnowledgeID: r.KnowledgeID, KnowledgeBaseID: r.KnowledgeBaseID, ChunkID: r.ID}
		bID := firstNonEmptyString(legal.SourceDocID, kmString(km, "external_id"))
		var eff LegalAmendDate
		var issued time.Time
		bProblem := ""
		if bID != "" {
			_ = l.loadCatalog([]string{bID}, nil)
		}
		if row := l.catalog[bID]; row != nil {
			b.CatalogID = bID
			b.Number = firstNonEmptyString(b.Number, row.DocumentNumber)
			b.Type = firstNonEmptyString(b.Type, row.DocType)
			eff = l.dateOf(row.EffectiveOn)
			if row.IssuedOn != nil {
				issued = *row.IssuedOn
			}
		} else {
			bProblem = "không xác định được văn bản sửa đổi trong danh mục"
		}
		if !eff.Known {
			eff = legalDay(kmString(km, "eff_from"), l.loc)
		}
		if issued.IsZero() {
			issued = legalDay(kmString(km, "issue_date"), l.loc).At
		}
		groups := legalQuoteGroups(r.Content)
		hash := sha256.Sum256([]byte(r.Content))
		for _, a := range legal.Amends {
			tKey, tDoc, problem, ok := l.amendTarget(a, legal, groups, b, issued, targets, byNumber, amenders)
			if !ok {
				continue
			}
			e := l.parserEdge(r, a, groups, b, tDoc, eff, structures[tKey])
			e.ChunkHash = hex.EncodeToString(hash[:8])
			if p := firstNonEmptyString(problem, bProblem); p != "" && e.Unresolved == "" {
				e.Unresolved = p
				e.Tier = LegalTierUnverified
			}
			ValidateLegalAmendEdge(&e, structures[tKey])
			k := strings.Join([]string{legalDocKey(b), tKey, string(e.Op), e.Provision.key(), foldSpaces(e.From), foldSpaces(e.To), e.NewLabel, foldSpaces(e.QuoteText)}, "|")
			if seen[k] {
				continue
			}
			seen[k] = true
			out[tKey] = append(out[tKey], e)
		}
	}
	return out
}

// amendTarget resolves the document an amends entry names to one of the
// targets; ok is false when it is another document. problem is set when
// the entry attaches to a target without a certain identity.
func (l *legalAmendLoader) amendTarget(a vietnamese_legal.AmendRef, legal *vietnamese_legal.LegalMetadata, groups []legalQuoteGroup,
	b LegalAmendDoc, issued time.Time, targets map[string]LegalAmendDoc, byNumber map[string]string, amenders map[string][]string,
) (key string, doc LegalAmendDoc, problem string, ok bool) {
	name := strings.TrimSpace(a.Document)
	if name == "" {
		// the lead-in or a heading may name it where the parser did not
		var texts []string
		for _, g := range groups {
			if a.Article != "" && legalMentions(g.leadIn, "điều", a.Article) {
				texts = append(texts, g.leadIn)
			}
		}
		for i := len(legal.HeadingPath) - 1; i >= 0; i-- {
			texts = append(texts, legal.HeadingPath[i])
		}
		name = legalSingleCitation(texts...)
	}
	if name == "" {
		// only a catalogue relation ties it to a target
		for k, d := range targets {
			if d.CatalogID != "" && containsString(amenders[d.CatalogID], b.CatalogID) && b.CatalogID != "" {
				return k, d, "thao tác không ghi văn bản bị sửa đổi (danh mục ghi " + b.label() + " sửa đổi văn bản này)", true
			}
		}
		return "", LegalAmendDoc{}, "", false
	}
	for _, c := range vietnamese_legal.FindCitations(name) {
		if c.Key == "" || c.Role == vietnamese_legal.CitationRoleInAmendingTitle {
			continue
		}
		k, held := byNumber[c.Key]
		if !held {
			return "", LegalAmendDoc{}, "", false
		}
		d := targets[k]
		rows := l.byKey[c.Key]
		if _, loaded := l.byKey[c.Key]; !loaded {
			_ = l.loadCatalog(nil, []string{c.Key})
			rows = l.byKey[c.Key]
		}
		switch {
		case len(rows) > 1:
			return k, d, "số hiệu " + c.Number + " ứng với nhiều văn bản trong danh mục", true
		case len(rows) == 0:
			return k, d, "số hiệu " + c.Number + " không có trong danh mục", true
		case d.CatalogID != "" && rows[0].ID != d.CatalogID:
			return "", LegalAmendDoc{}, "", false
		}
		return k, d, "", true
	}
	// a name only: the catalogue law of that title in force when B was
	// issued — looked up only when it starts with a target's title
	folded := strings.ToLower(strings.Join(strings.Fields(name), " "))
	maybe := false
	for _, d := range targets {
		if row := l.catalog[d.CatalogID]; row != nil {
			if base := LegalTitleBase(row.Title, row.DocType); base != "" && (folded == base || strings.HasPrefix(folded, base+" ") || strings.HasPrefix(folded, base+",")) {
				maybe = true
			}
		}
	}
	if !maybe {
		return "", LegalAmendDoc{}, "", false
	}
	m, err := ResolveLegalDocumentName(l.ctx, l.db, LegalNameQuery{Name: name, At: issued, AmenderID: b.CatalogID})
	if err != nil || m.Chosen == nil {
		for k, d := range targets {
			if err == nil && m.Resolution == LegalNameAmbiguous && d.CatalogID != "" && containsLegalCandidate(m, d.CatalogID) {
				return k, d, "tên “" + name + "” ứng với nhiều văn bản", true
			}
		}
		return "", LegalAmendDoc{}, "", false
	}
	for k, d := range targets {
		if d.CatalogID == m.Chosen.ID || (d.CatalogID == "" && vietnamese_legal.CatalogNumberKey(d.Number) == m.Chosen.NumberKey) {
			if m.Trimmed {
				return k, d, "tên “" + name + "” chỉ khớp sau khi bỏ bớt chữ (" + m.Dropped + ")", true
			}
			return k, d, "", true
		}
	}
	return "", LegalAmendDoc{}, "", false
}

func containsLegalCandidate(m LegalNameMatch, id string) bool {
	for _, c := range m.Candidates {
		if c.Document != nil && c.Document.ID == id {
			return true
		}
	}
	return false
}

// legalSingleCitation is the số hiệu of the first text naming exactly one
// document (an amending title's own words aside).
func legalSingleCitation(texts ...string) string {
	for _, t := range texts {
		var found []string
		for _, c := range vietnamese_legal.FindCitations(t) {
			if c.Key != "" && c.Role != vietnamese_legal.CitationRoleInAmendingTitle && !containsString(found, c.Number) {
				found = append(found, c.Number)
			}
		}
		if len(found) == 1 {
			return found[0]
		}
	}
	return ""
}

var (
	legalBoSungOnlyRe = regexp.MustCompile(`(?i)bổ sung`)
	legalSuaDoiRe     = regexp.MustCompile(`(?i)sửa đổi|được sửa|thay thế`)
)

// parserEdge turns one converter amends entry into an edge.
func (l *legalAmendLoader) parserEdge(r legalAmendRow, a vietnamese_legal.AmendRef, groups []legalQuoteGroup,
	b LegalAmendDoc, target LegalAmendDoc, eff LegalAmendDate, structure LegalDocStructure,
) LegalAmendEdge {
	prov := LegalProvisionRef{Article: a.Article, Khoan: a.Khoan, Diem: a.Diem}.normalized()
	e := LegalAmendEdge{Amender: b, Target: target, Provision: prov, EffFrom: eff,
		Sources: []string{LegalAmendSourceParser}, Tier: LegalTierUnverified}
	if !l.allowed[b.KnowledgeBaseID] {
		e.TextWithheld = true
	}
	scope := newProvisionScope(r.Content)
	article := ""
	if len(r.Metadata) > 0 {
		if legal, _ := parseLegalAmendRow(r); legal != nil && len(legal.ArticleNos) == 1 {
			article = strings.ToLower(legal.ArticleNos[0])
		}
	}
	origin := func(byteAt int) LegalProvisionRef {
		k, d := scope.at(byteAt)
		return LegalProvisionRef{Article: article, Khoan: k, Diem: d}
	}
	if a.Phrase {
		e.Op, e.From, e.To = LegalOpThayCumTu, a.From, a.To
		if a.To == "" {
			e.Op = LegalOpBaiBoCumTu
		}
		e.Tier = LegalTierLegacyParserPhrase
		if i := strings.Index(r.Content, a.From); i >= 0 {
			start := strings.LastIndexByte(r.Content[:i], '\n') + 1
			end := strings.IndexByte(r.Content[i:], '\n')
			if end < 0 {
				end = len(r.Content)
			} else {
				end += i
			}
			e.LeadInText = strings.TrimSpace(r.Content[start:end])
			e.LeadIn = &LegalRuneSpan{utf8.RuneCountInString(r.Content[:start]), utf8.RuneCountInString(r.Content[:end])}
			e.Origin = origin(start)
		}
		return e
	}
	// a quoted rewrite: sửa đổi, or bổ sung of a provision A lacks
	e.Op = LegalOpSuaDoi
	g, blk, _, ok := legalQuoteFor(groups, prov, "")
	if (ok && leadInAdds(g.leadIn)) || (!ok && structure != nil && !structure.has(prov)) {
		parent, label := legalParentOf(prov)
		if label != "" {
			if g2, blk2, _, ok2 := legalQuoteFor(groups, parent, label); ok2 {
				e.Op, e.Provision, e.NewLabel = LegalOpBoSung, parent, label
				g, blk, ok = g2, blk2, true
			}
		}
	}
	if ok {
		e.Quote = &blk.span
		e.QuoteText = blk.text
		e.LeadInText = g.leadIn
		e.LeadIn = &g.leadInSpan
		e.Origin = origin(g.leadInByte)
		if e.TextWithheld {
			e.QuoteText = ""
		}
	}
	return e
}

// leadInAdds reports a lead-in that only adds ("Bổ sung khoản 15 …").
func leadInAdds(leadIn string) bool {
	return leadIn != "" && legalBoSungOnlyRe.MatchString(leadIn) && !legalSuaDoiRe.MatchString(leadIn)
}

// legalParentOf splits the deepest label off p.
func legalParentOf(p LegalProvisionRef) (LegalProvisionRef, string) {
	switch {
	case p.Diem != "":
		return LegalProvisionRef{Article: p.Article, Khoan: p.Khoan}, p.Diem
	case p.Khoan != "":
		return LegalProvisionRef{Article: p.Article}, p.Khoan
	case p.Article != "":
		return LegalProvisionRef{}, p.Article
	}
	return p, ""
}

func parseLegalAmendRow(r legalAmendRow) (*vietnamese_legal.LegalMetadata, map[string]interface{}) {
	var meta types.DocumentChunkMetadata
	if json.Unmarshal(r.Metadata, &meta) != nil {
		return nil, nil
	}
	var km map[string]interface{}
	_ = json.Unmarshal(r.KMetadata, &km)
	return meta.Legal, km
}

func kmString(km map[string]interface{}, k string) string {
	v, _ := km[k].(string)
	return strings.TrimSpace(v)
}

// Resolve runs the resolver on every loaded chunk as of asOf. allowedKBs
// is the §10.2 guard (nil: the load's own withholding only).
func (load *LegalAmendLoad) Resolve(asOf time.Time, allowedKBs map[string]bool) map[string]LegalResolution {
	out := make(map[string]LegalResolution, len(load.Chunks))
	for _, c := range load.Chunks {
		key := legalDocKey(c.Doc)
		edges := append([]LegalAmendEdge(nil), load.Edges[key]...)
		out[c.ID] = ResolveLegalProvisions(c, edges, asOf, LegalResolveOptions{
			AllowedKBs: allowedKBs, Catalogue: load.Catalogue[c.Doc.CatalogID],
		})
	}
	return out
}
