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
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
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
	// legalAmendPhraseCandidates bounds the light phrase-entry read.
	legalAmendPhraseCandidates = 3000
	// legalAmendChainDocs bounds the documents amending an amender (C → B).
	legalAmendChainDocs = 20
)

// LegalAmendLoad is what the resolver needs for a set of retrieved chunks.
type LegalAmendLoad struct {
	Chunks []LegalProvisionChunk
	// Edges by A's key (legalDocKey: catalogue id, else number key).
	Edges map[string][]LegalAmendEdge
	// Catalogue-level facts "B amends A", by A's key: the catalogue's
	// relations and the converter's document-level entries.
	Catalogue map[string][]LegalCatalogAmendment
	// Took records the time of each phase, for the log.
	Took map[string]time.Duration
	// Editor is what the vbpl editor source did (nil when off).
	Editor *LegalEditorMergeStats
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
	ExpiredOn      *time.Time
	Status         string
}

// legalAmendIDSelect is legalAmendSelect reading ids (and KB) only.
const legalAmendIDSelect = `SELECT c.id, c.knowledge_base_id
FROM chunks c JOIN knowledges k ON k.id = c.knowledge_id
WHERE c.deleted_at IS NULL AND k.deleted_at IS NULL`

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
	// names memoises ResolveLegalDocumentName by name, date and amender
	names   map[string]legalNameMemo
	namesMu sync.Mutex
	// cites memoises FindCitations by text (headings repeat per entry)
	cites map[string][]vietnamese_legal.Citation
	// bases are the title bases of the targets, by catalogue id
	bases map[string]string
}

// citations is FindCitations, memoised for the load (not concurrent).
func (l *legalAmendLoader) citations(text string) []vietnamese_legal.Citation {
	if c, ok := l.cites[text]; ok {
		return c
	}
	c := vietnamese_legal.FindCitations(text)
	l.cites[text] = c
	return c
}

type legalNameMemo struct {
	m   LegalNameMatch
	err error
}

// LoadLegalAmendments reads the chunks chunkIDs (within scopeKBs) of
// legal documents and the edges touching their documents. allowedKBs are
// the KBs whose amending text the model may read (§10.2: the question's
// KBs plus the public platform KBs); amending chunks are read from those
// and from vbpl copies anywhere — the latter only for the fact and the
// number of B, their text is withheld.
func LoadLegalAmendments(ctx context.Context, db *gorm.DB, scopeKBs, chunkIDs, allowedKBs []string, loc *time.Location) (*LegalAmendLoad, error) {
	return LoadLegalAmendmentsWith(ctx, db, scopeKBs, chunkIDs, allowedKBs, loc, LegalAmendLoadOptions{})
}

// LoadLegalAmendmentsWith is LoadLegalAmendments with the optional sources
// of opt (the vbpl editor edges); zero options load exactly as
// LoadLegalAmendments.
func LoadLegalAmendmentsWith(ctx context.Context, db *gorm.DB, scopeKBs, chunkIDs, allowedKBs []string, loc *time.Location,
	opt LegalAmendLoadOptions,
) (*LegalAmendLoad, error) {
	if loc == nil {
		loc = time.UTC
	}
	l := &legalAmendLoader{ctx: ctx, db: db.WithContext(ctx), loc: loc, allowed: map[string]bool{},
		catalog: map[string]*legalCatalogRow{}, byKey: map[string][]*legalCatalogRow{}, names: map[string]legalNameMemo{},
		cites: map[string][]vietnamese_legal.Citation{}, bases: map[string]string{}}
	for _, id := range allowedKBs {
		l.allowed[id] = true
	}
	load := &LegalAmendLoad{Edges: map[string][]LegalAmendEdge{}, Catalogue: map[string][]LegalCatalogAmendment{}, Took: map[string]time.Duration{}}
	phase := time.Now()
	lap := func(name string) {
		load.Took[name] += time.Since(phase)
		phase = time.Now()
	}
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
	lap("chunks")
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
					if aRow := l.catalog[aID]; aRow != nil && legalRankInverted(row.DocType, aRow.DocType) {
						logger.Debugf(l.ctx, "[LegalAmend] catalogue: %s %s cannot amend %s %s: relation skipped",
							row.DocType, row.DocumentNumber, aRow.DocType, aRow.DocumentNumber)
						continue
					}
					load.Catalogue[aID] = append(load.Catalogue[aID], LegalCatalogAmendment{
						Amender: l.catalogDoc(row), EffFrom: l.dateOf(row.EffectiveOn), NeverInForce: l.neverInForce(row)})
				}
			}
		}
	}

	// amending chunks: those of catalogue amenders naming an article of a
	// retrieved document, and those naming the article and the document as
	// the converter writes it ("Nghị định số 68/2026/NĐ-CP"; a law also by
	// its title)
	var articles, aKnowledge, aKeys []string
	var probes []legalAmendProbe
	for _, d := range docs {
		aKnowledge = append(aKnowledge, d.knowledge)
		if k := vietnamese_legal.CatalogNumberKey(d.doc.Number); k != "" {
			aKeys = append(aKeys, k)
		}
		// the number as the converter writes it ("10 /2016/TT-BTNMT" → "10/2016/TT-BTNMT")
		number := d.doc.Number
		if n := vietnamese_legal.NormalizeCitationNumber(number); n != "" {
			number = n
		}
		row := l.catalog[d.doc.CatalogID]
		typ := d.doc.Type
		if typ == "" && row != nil {
			typ = row.DocType
		}
		base := ""
		if row != nil && legalNamedByTitle(row.DocType) {
			base = LegalTitleBase(row.Title, row.DocType)
		}
		for a := range d.articles {
			articles = append(articles, a)
			if typ != "" && number != "" {
				probes = append(probes, legalAmendProbe{Article: a, Document: typ + " số " + number},
					legalAmendProbe{Article: a, Document: typ + " " + number})
			}
			if base != "" {
				probes = append(probes, legalAmendProbe{Article: a, TitleBase: base})
			}
		}
	}
	lap("relations")
	// the structure of A (for the target checks) is read meanwhile
	var byKnowledge map[string]LegalDocStructure
	var repeatedBy map[string]legalRepeated
	sg, _ := errgroup.WithContext(l.ctx)
	legalGo(sg, func() (err error) {
		byKnowledge, repeatedBy, err = l.structures(aKnowledge)
		return err
	})
	cands, truncated, err := l.amendingChunks(bIDs, articles, probes, aKeys, aKnowledge)
	if werr := sg.Wait(); err == nil {
		err = werr
	}
	lap("amending")
	if err != nil {
		return nil, err
	}
	structures := map[string]LegalDocStructure{}
	for key, d := range docs {
		structures[key] = byKnowledge[d.knowledge]
	}

	targets := map[string]LegalAmendDoc{}
	for key, d := range docs {
		targets[key] = d.doc
	}
	lap("structure")
	all, facts, err := l.edgesFrom(cands, targets, amenders, structures)
	if err != nil {
		return nil, err
	}
	lap("edges")
	for key, list := range all {
		load.Edges[key] = list
	}
	// document-level entries ("sửa đổi Nghị định X" with no provision) are
	// catalogue-level facts, never a rewrite of the whole document
	for key, list := range facts {
		for _, f := range list {
			if d := docs[key]; d != nil && legalRankInverted(f.Amender.Type, d.doc.Type) {
				logger.Debugf(l.ctx, "[LegalAmend] %s %s cannot amend %s %s: document-level entry skipped",
					f.Amender.Type, f.Amender.Number, d.doc.Type, d.doc.Number)
				continue
			}
			dup := false
			for _, have := range load.Catalogue[key] {
				if have.Amender.CatalogID != "" && have.Amender.CatalogID == f.Amender.CatalogID {
					dup = true
					break
				}
			}
			if !dup {
				load.Catalogue[key] = append(load.Catalogue[key], f)
			}
		}
	}

	// how far each catalogue amender's text was read
	docArticles := map[string]map[string]bool{}
	for key, d := range docs {
		docArticles[key] = d.articles
	}
	// read meanwhile with the chain hop (it touches load.Catalogue only)
	rg, _ := errgroup.WithContext(l.ctx)
	legalGo(rg, func() error { return l.amenderReads(load, docArticles, truncated) })

	// one hop back: documents amending the amenders (C → B → A)
	var amenderIDs, origins []string
	seenB, seenO := map[string]bool{}, map[string]bool{}
	for _, list := range all {
		for _, e := range list {
			if id := e.Amender.CatalogID; id != "" && e.Origin.Article != "" {
				if !seenB[id] {
					seenB[id] = true
					amenderIDs = append(amenderIDs, id)
				}
				if !seenO[e.Origin.Article] {
					seenO[e.Origin.Article] = true
					origins = append(origins, e.Origin.Article)
				}
			}
		}
	}
	if len(amenderIDs) > 0 {
		if err := l.chainHop(amenderIDs, origins, load); err != nil {
			_ = rg.Wait()
			return nil, err
		}
	}
	if err := rg.Wait(); err != nil {
		return nil, err
	}
	lap("chain+reads")
	if opt.EditorSource || opt.FeedSource {
		targets := make(map[string]legalEditorTarget, len(docs))
		for key, d := range docs {
			targets[key] = legalEditorTarget{doc: d.doc, articles: d.articles, structure: structures[key], repeated: repeatedBy[d.knowledge]}
		}
		st, err := l.editorEdges(load, targets, opt)
		if err != nil {
			return nil, err
		}
		load.Editor = &st
		lap("editor")
		logger.Debugf(l.ctx, "[LegalAmend] editor source: %d links, %d agreed, %d explained, %d alone (%d with text), %d date mismatches, %v",
			st.Links, st.Agreed, st.Explained, st.EditorOnly, st.WithText, st.DateMismatch, load.Took["editor"])
	}
	return load, nil
}

// legalAmendCiteChunks bounds the chunks of the amenders read for their
// citations of a target's articles.
const legalAmendCiteChunks = 300

var (
	// legalCitedArticleRe finds "Điều N" in a line of an amender's text.
	legalCitedArticleRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])Điều\s+(\d+[a-zđ]?)(?:[^\p{L}\p{N}]|$)`)
)

// amenderReads sets, on every catalogue fact of load, whether the
// amender's text was read whole from a KB the question may read (held
// there, parsed for operations, no read truncated) and which articles of
// the target its text cites outside quotes and headings. With the text
// read, a provision B never names is the original; one whose Điều B cites
// beyond the operations read stays unverified.
func (l *legalAmendLoader) amenderReads(load *LegalAmendLoad, docArticles map[string]map[string]bool, truncated bool) error {
	var bIDs []string
	seen := map[string]bool{}
	for _, list := range load.Catalogue {
		for _, f := range list {
			if id := f.Amender.CatalogID; id != "" && !seen[id] {
				seen[id] = true
				bIDs = append(bIDs, id)
			}
		}
	}
	if len(bIDs) == 0 {
		return nil
	}
	allowed := make([]string, 0, len(l.allowed)+1)
	for id := range l.allowed {
		allowed = append(allowed, id)
	}
	allowed = append(allowed, "")
	ext := legalJSONText(l.db, "k.metadata", "external_id")
	var held, parsed []string
	g, _ := errgroup.WithContext(l.ctx)
	legalGo(g, func() error {
		if err := l.db.Raw(`SELECT DISTINCT `+ext+` FROM knowledges k WHERE k.deleted_at IS NULL AND `+ext+` IN ? AND k.knowledge_base_id IN ?`,
			bIDs, allowed).Scan(&held).Error; err != nil {
			return fmt.Errorf("amenders held: %w", err)
		}
		return nil
	})
	hasAmends := legalJSONText(l.db, "c.metadata", "legal", "amends") + ` IS NOT NULL`
	if l.db.Dialector.Name() == "postgres" {
		hasAmends = `jsonb_exists(c.metadata->'legal', 'amends')`
	}
	legalGo(g, func() error {
		if err := l.db.Raw(`SELECT DISTINCT `+ext+` FROM chunks c JOIN knowledges k ON k.id = c.knowledge_id
			WHERE c.deleted_at IS NULL AND k.deleted_at IS NULL AND `+ext+` IN ? AND c.knowledge_base_id IN ? AND `+hasAmends,
			bIDs, allowed).Scan(&parsed).Error; err != nil {
			return fmt.Errorf("amenders parsed: %w", err)
		}
		return nil
	})
	// the amenders' chunks naming a target (in the text or the heading)
	type target struct {
		key, short, number, typ, base string
	}
	var targets []target
	var likes []string
	var likeArgs []interface{}
	heading := legalJSONText(l.db, "c.metadata", "legal", "heading_path")
	for _, c := range load.Chunks {
		key := legalDocKey(c.Doc)
		parts := strings.SplitN(vietnamese_legal.NormalizeCitationNumber(c.Doc.Number), "/", 3)
		if len(parts) < 2 {
			continue
		}
		short := parts[0] + "/" + parts[1]
		dup := false
		for _, t := range targets {
			dup = dup || t.key == key
		}
		if dup {
			continue
		}
		base := ""
		if row := l.catalog[c.Doc.CatalogID]; row != nil && legalNamedByTitle(row.DocType) {
			base = LegalTitleBase(row.Title, row.DocType)
		}
		targets = append(targets, target{key, short, c.Doc.Number, c.Doc.Type, base})
		likes = append(likes, `c.content LIKE ? OR `+heading+` LIKE ?`)
		pat := "%" + legalLikeEscaper.Replace(short) + "%"
		likeArgs = append(likeArgs, pat, pat)
	}
	var rows []struct {
		Ext, Content string
		Heading      string
	}
	if len(likes) > 0 {
		args := append([]interface{}{bIDs, allowed}, likeArgs...)
		args = append(args, legalAmendCiteChunks)
		legalGo(g, func() error {
			if err := l.db.Raw(`SELECT `+ext+` AS ext, c.content, `+heading+` AS heading FROM chunks c JOIN knowledges k ON k.id = c.knowledge_id
				WHERE c.deleted_at IS NULL AND k.deleted_at IS NULL AND `+ext+` IN ? AND c.knowledge_base_id IN ? AND (`+strings.Join(likes, " OR ")+`)
				ORDER BY c.id LIMIT ?`, args...).Scan(&rows).Error; err != nil {
				return fmt.Errorf("amenders' citations: %w", err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	citeTruncated := len(rows) >= legalAmendCiteChunks
	cited := map[string]map[string]map[string]bool{} // B → A key → articles
	for _, r := range rows {
		for _, t := range targets {
			if !strings.Contains(r.Content, t.short) && !strings.Contains(r.Heading, t.short) {
				continue
			}
			for art := range legalCitedArticlesOf(r.Content, r.Heading, t.number, t.typ, t.base) {
				if !docArticles[t.key][art] {
					continue
				}
				if cited[r.Ext] == nil {
					cited[r.Ext] = map[string]map[string]bool{}
				}
				if cited[r.Ext][t.key] == nil {
					cited[r.Ext][t.key] = map[string]bool{}
				}
				cited[r.Ext][t.key][art] = true
			}
		}
	}
	for key, list := range load.Catalogue {
		for i := range list {
			id := list[i].Amender.CatalogID
			list[i].TextRead = id != "" && containsString(held, id) && containsString(parsed, id) && !truncated && !citeTruncated
			list[i].Cited = cited[id][key]
		}
	}
	return nil
}

var (
	// legalBeforeAfterRe ends the text before "Điều N" of an insertion
	// point ("Bổ sung Điều 2a vào sau Điều 2"): Điều N is not amended.
	// legalHistoryRefRe ends the text before "Điều N" naming where an
	// earlier amendment was made ("đã được sửa đổi, bổ sung bởi khoản 5
	// Điều 1 Thông tư số 41/2020/TT-BGTVT"): a reference, not an operation.
	// "(đã) được / bị bãi bỏ bởi khoản 3 Điều 1 Thông tư …" is history too.
	legalHistoryRefRe = regexp.MustCompile(`(?i)(?:được|bị)\s+(?:sửa\s+đổi|bổ\s+sung|thay\s+thế|bãi\s+bỏ)(?:\s*,\s*bổ\s+sung)?\s+(?:bởi|tại|theo)(?:\s+quy\s+định\s+tại)?(?:\s*(?:,|và)?\s*(?:các\s+)?(?:điểm|khoản)\s+[0-9a-zđ]+)*\s*$`)
	legalParenRe      = regexp.MustCompile(`\([^()]*\)`)
	// "sau / trước" must start a word.
	legalBeforeAfterRe = regexp.MustCompile(`(?i)(?:^|\s)(?:vào\s+)?(?:sau|trước)\s*$`)
	// legalSelfRefRe starts the text after "Điều N" naming B itself
	// ("Điều 5 Nghị định này"), also at the end of a list of provisions
	// ("Điều 5 và Điều 6 Nghị định này", "Điều 5, khoản 2 Điều 6 …").
	legalSelfRefRe = regexp.MustCompile(`(?i)^(?:\s*(?:,|và|hoặc|đến|tới|-)\s*(?:(?:khoản|điểm)\s+[0-9a-zđ]+\s+(?:của\s+)?)?Điều\s+\d+[a-zđ]?)*` +
		`[\s,]*(?:(?:khoản|điểm)\s+\S+\s+)?(?:của\s+)?(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định|văn bản)\s+này\b`)
	// legalArticleHeadingRe is a line shaped as an Điều heading ("Điều 1.
	// Sửa đổi … Nghị định số …") in a copy without Markdown headings (PDF,
	// OCR): B's own Điều, never A's.
	legalArticleHeadingRe = regexp.MustCompile(`^(?:\*\*)?Điều\s+\d+[a-zđ]?\s*[.:](?:\*\*)?`)
	// legalCiteResidueRe is what a bracket holding only a citation keeps
	// besides it: the type words ("(Nghị định số 88/2025/NĐ-CP)").
	legalCiteResidueRe = regexp.MustCompile(`(?i)^[\s,.:;]*(?:(?:bộ luật|luật|pháp lệnh|nghị định|nghị quyết|thông tư liên tịch|thông tư|quyết định|chỉ thị)\s*(?:số\s*:?)?\s*)?$`)
	// legalLawWordRe is where a law may be named on a line.
	legalLawWordRe = regexp.MustCompile(`(?:^|[^\p{L}])(Bộ luật|Luật|Pháp lệnh)\s+\p{L}`)
)

// legalCitationOnly reports whether a bracket's inside is one citation and
// nothing else ("Nghị định số 88/2025/NĐ-CP", "Luật Đất đai"): it names
// the document of the Điều before it, it is no history remark.
func legalCitationOnly(inner, aBase string) bool {
	cs := vietnamese_legal.FindCitationsKnowing(inner, aBase)
	if len(cs) != 1 || (cs[0].Key == "" && cs[0].Kind != vietnamese_legal.CitationName) {
		return false
	}
	r := []rune(inner)
	return legalCiteResidueRe.MatchString(string(r[:cs[0].Start]) + " " + string(r[cs[0].End:]))
}

// legalUnreadLawBetween reports whether line names a law between the rune
// offsets from and to that FindCitations did not read (a title outside
// its lists, in lower case): a Điều before it may be that law's.
func legalUnreadLawBetween(line string, from, to int, aBase string) bool {
	r := []rune(line)
	if from >= to || to > len(r) {
		return false
	}
	seg := string(r[from:to])
	for _, m := range legalLawWordRe.FindAllStringSubmatchIndex(seg, -1) {
		rest := line[len(string(r[:from]))+m[2]:]
		cs := vietnamese_legal.FindCitationsKnowing(rest, aBase)
		// read: a name starting there, or the name of a number after it
		named := len(cs) > 0 && cs[0].Kind != vietnamese_legal.CitationName && cs[0].Name != "" &&
			strings.HasPrefix(strings.ToLower(rest), strings.ToLower(cs[0].Name))
		if len(cs) == 0 || (cs[0].Start != 0 && !named) {
			return true
		}
	}
	return false
}

// legalCitedArticlesOf lists the Điều of document A (number aNumber, type
// aType, title base aBase for a law) that an amender's chunk cites outside
// quotes and headings. A "Điều N" counts when the document cited right
// after its chain of labels ("khoản 2, khoản 4 Điều 27 Nghị định số …") is
// A, or when no document follows the chain and the subject of the chunk's
// heading is A (legalHeadingSubjectIsA). Not an insertion point ("vào sau
// Điều 2"), not B's own Điều ("Điều 5 Nghị định này", also at the end of
// a list), not an amendment history (bracketed, or "đã được sửa đổi, bổ
// sung bởi / đã bị bãi bỏ theo … Điều 1 …"), not the new wording a line
// quotes ("… như sau: “b) …”"), not the Điều of a law named in lower case
// FindCitations does not read.
func legalCitedArticlesOf(content, heading, aNumber, aType, aBase string) map[string]bool {
	isA := legalCitationIsA(aNumber, aType, aBase)
	subjectA := legalHeadingSubjectIsA(heading, aBase, isA)
	out := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ">") || strings.HasPrefix(line, "#") {
			continue
		}
		// "Điều 2. Bãi bỏ Điều 5 …" in a plain-text copy: the heading's
		// own Điều is B's; the rest of the line is read
		if loc := legalArticleHeadingRe.FindStringIndex(line); loc != nil {
			line = strings.TrimSpace(line[loc[1]:])
		}
		// the new wording quoted inline is not a citation of A
		line = legalDropInlineNewText(line)
		// a bracketed remark is history ("(đã được sửa đổi, bổ sung bởi
		// khoản 7 Điều 1 Thông tư số 41/2021/TT-BGTVT)"), not an operation;
		// a bracket holding only a citation names the Điều's document
		// ("Điều 12 (Nghị định số 88/2025/NĐ-CP) được bãi bỏ"): kept
		unbracket := func(br string) string {
			if inner := br[1 : len(br)-1]; legalCitationOnly(inner, aBase) {
				return " " + inner + " "
			}
			return " "
		}
		for stripped := legalParenRe.ReplaceAllStringFunc(line, unbracket); stripped != line; stripped = legalParenRe.ReplaceAllStringFunc(line, unbracket) {
			line = stripped // nested brackets, innermost first
		}
		var cites []vietnamese_legal.Citation
		for _, c := range vietnamese_legal.FindCitationsKnowing(line, aBase) {
			if c.Key != "" || c.Kind == vietnamese_legal.CitationName || c.Kind == vietnamese_legal.CitationPartial {
				cites = append(cites, c)
			}
		}
		// the chains of the line in order; a joined chain takes the
		// document of the next one in its clause, so they are read first
		// and settled from the last
		type chain struct {
			start, pos, kw int
			arts           []string
			attached       *vietnamese_legal.Citation
			part, joined   bool
			blocked        bool // not A's whatever comes after (B's own, an insertion point)
		}
		var chains []chain
		for _, m := range legalCitedArticleRe.FindAllStringSubmatchIndex(line, -1) {
			kw := m[0] + strings.Index(line[m[0]:m[2]], "Điều")
			if kw < m[0] {
				kw = m[0]
			}
			if legalBeforeAfterRe.MatchString(line[:kw]) || legalHistoryRefRe.MatchString(line[:kw]) || legalSelfRefRe.MatchString(line[m[3]:]) {
				chains = append(chains, chain{start: m[0], blocked: true})
				continue
			}
			// the rest of the chain of labels, then the document it names;
			// bare numbers after a Điều are Điều too ("các Điều 4, 5 và 6")
			pos, kind := m[3], "điều"
			arts := []string{strings.ToLower(line[m[2]:m[3]])}
			for {
				loc := legalChainMoreRe.FindStringSubmatchIndex(line[pos:])
				if loc == nil {
					break
				}
				if loc[8] >= 0 && legalItemLabelRe.MatchString(line[pos+loc[9]:]) {
					break
				}
				switch {
				case loc[4] >= 0:
					kind = strings.ToLower(line[pos+loc[4] : pos+loc[5]])
				case kind == "điều":
					arts = append(arts, strings.ToLower(line[pos+loc[8]:pos+loc[9]]))
				}
				pos += loc[3]
			}
			if legalSelfRefRe.MatchString(line[pos:]) {
				chains = append(chains, chain{start: m[0], blocked: true})
				continue
			}
			// listed: what follows is the next item of a list (after ", "
			// or "và"), a sibling of the Điều, not the part holding it
			listed := false
			if loc := legalChainLinkRe.FindStringIndex(line[pos:]); loc != nil {
				listed = strings.Contains(line[pos:pos+loc[1]], ",")
				pos += loc[1]
			}
			// "Điều 3 và Phụ lục số 01 của …", "Điều 4, Chương III, Phụ
			// lục 2 của …": a part or division listed beside the Điều
			if loc := legalListedPartRe.FindStringIndex(line[pos:]); loc != nil &&
				(legalDocPartRe.MatchString(line[pos+loc[1]:]) || legalDivisionRe.MatchString(line[pos+loc[1]:])) {
				pos += loc[1]
				listed = true
			}
			// a full number starts after its type words ("Nghị định số
			// 14/2016/…"), a partial or name citation with them
			at := utf8.RuneCountInString(line[:pos])
			at2 := at
			if loc := legalDocTypeLeadRe.FindStringIndex(line[pos:]); loc != nil {
				at2 = utf8.RuneCountInString(line[:pos+loc[1]])
			}
			var attached *vietnamese_legal.Citation
			for i := range cites {
				if cites[i].Start == at || cites[i].Start == at2 || legalNamedBefore(line, pos, cites[i]) ||
					cites[i].Start > at && legalTypeWords(string([]rune(line)[at:cites[i].Start])) {
					attached = &cites[i]
					break
				}
			}
			// a part of another document ("Quy chế ban hành kèm theo
			// Quyết định số …", "Phụ lục II Nghị định số …") is that
			// document's; with none cited in its clause, nobody's known. A
			// part or division listed beside the Điều names the list's
			// document when its clause cites one, else leaves it open.
			isPart := legalDocPartRe.MatchString(line[pos:]) && !legalHeadingNames(heading, line[pos:])
			part := isPart && !listed
			if (isPart || legalDivisionRe.MatchString(line[pos:])) && attached == nil {
				attached = legalClauseCitation(line, pos, cites)
			}
			// operations joined in one clause ("Bãi bỏ điểm a khoản 2 Điều
			// 3, bỏ cụm từ … tại khoản 3 Điều 4 của Thông tư số …") share
			// the document of the next chain
			joined := false
			if loc := legalJoinedOperationRe.FindStringSubmatchIndex(line[pos:]); loc != nil && attached == nil {
				joined = listed || loc[2] >= 0
			}
			chains = append(chains, chain{start: m[0], pos: pos, kw: kw, arts: arts, attached: attached, part: part, joined: joined})
		}
		for i := len(chains) - 1; i >= 0; i-- {
			ch := &chains[i]
			if ch.blocked {
				continue
			}
			if ch.joined && i+1 < len(chains) && chains[i+1].start < legalClauseEnd(line, ch.pos) {
				next := chains[i+1]
				if next.blocked {
					continue
				}
				ch.attached, ch.part = next.attached, next.part
			}
			switch {
			case ch.attached != nil && isA(*ch.attached):
			case ch.attached == nil && subjectA && !ch.part && !legalUnreadLawAt(line, ch.pos, aBase) &&
				!legalHistoryOnly(line, ch.kw, ch.pos, cites):
			default:
				continue
			}
			for _, a := range ch.arts {
				out[a] = true
			}
		}
	}
	return out
}

var (
	// legalInlineNewTextRe opens the new wording a line quotes: a quote
	// after "như sau:" or one opening a labelled provision (“b) …”, "3a.
	// …", “Điều 12. …”), or a phrase replaced or removed (cụm từ “khoản 6
	// Điều 81”: words, not a citation). A straight quote opens only after a
	// space or a colon and right before the label: in `(nếu có)." 8. Sửa
	// đổi …` it closes the wording before.
	legalInlineNewTextRe = regexp.MustCompile(`(?i)(?:như\s+sau\s*:?|cụm\s+từ)\s*[“"]|“\s*(?:\d+[a-zđ]?\.|[a-zđ]\)|Điều\s+\d)|(?:^|[\s:])"(?:\d+[a-zđ]?\.|[a-zđ]\)|Điều\s+\d)`)
	// legalChainMoreRe continues a chain of labels after "Điều N": ", Điều
	// 6", " khoản 2 Điều 10", " và điểm a", ", 7" (group 2 the label, 3
	// its value, 4 a bare number). ";" ends it: "Sửa đổi Điều 5; Điều 7
	// Thông tư … được bãi bỏ" are two operations.
	// A bare number needs its separator.
	legalChainMoreRe = regexp.MustCompile(`(?i)^(\s*(?:,\s*|\s(?:và|hoặc)\s+)?(?:các\s+)?(điểm|khoản|điều)\s+([0-9a-zđ]+)|\s*(?:,\s*|\s(?:và|hoặc)\s+)(\d+[a-zđ]?))(?:[^\p{L}\p{N}]|$)`)
	// legalItemLabelRe follows a number that labels the next item ("9.
	// Bãi bỏ", "3) Sửa đổi"), not one more in a list.
	legalItemLabelRe = regexp.MustCompile(`^[.)]\s+\p{Lu}`)
	// legalHistoryAfterRe starts the text after a chain recording an
	// earlier amendment: "đã bị bãi bỏ theo …", "đã được sửa đổi, bổ sung
	// bởi …".
	legalHistoryAfterRe = regexp.MustCompile(`(?i)^[\s,]*(?:đã\s+)?(?:(?:được|bị)\s+(?:sửa\s+đổi|bổ\s+sung|bãi\s+bỏ|thay\s+thế|đình\s+chỉ)|hết\s+hiệu\s+lực)[^.;:]*?\s(?:bởi|theo|tại)\s`)
	// legalOperationVerbRe is an operation written before a chain ("Sửa
	// đổi, bổ sung khoản 2 Điều 4, được sửa đổi … tại … Thông tư …"): the
	// history after it is a remark on the Điều the line amends.
	legalOperationVerbRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:sửa\s+đổi|bổ\s+sung|bãi\s+bỏ|thay\s+thế|thay|bỏ|đính\s+chính)(?:[^\p{L}]|$)`)
	// legalDivisionRe starts the text after a chain naming a division
	// ("Mục 5 Chương XIV của Bộ luật …", "Phần II", "chương ii", "Phần
	// thứ nhất"). Điều are numbered through a whole document, so the Điều
	// is the document's that the clause cites, else the heading's
	// subject's.
	legalDivisionRe = regexp.MustCompile(`^\s*(?:của\s+)?(?i:chương|mục|phần|tiểu\s+mục)\s+(?:(?i:thứ)\s+\p{L}+|(?i:[0-9ivxlc]+)[a-zA-Z]?)(?:[^\p{L}\p{N}]|$)`)
	// legalDocPartRe starts the text after a chain naming a part of a
	// document by its title-cased name ("Quy chế …", "Phụ lục II", "Mẫu
	// số 03", "mẫu 07-1/HĐTV"): its Điều are the part's. "quy định về" in
	// lower case is a verb ("Điều 12 quy định về nguồn vốn").
	legalDocPartRe = regexp.MustCompile(`^\s*(?:Quy\s+chế|Quy\s+định|Quy\s+trình|Điều\s+lệ|(?i:phụ\s+lục|mẫu)|Danh\s+mục|Biểu|Chương\s+trình|Chế\s+độ|Tiêu\s+chuẩn)(?:[^\p{L}]|$)`)
	// legalChainLinkRe is what may stand between a chain and the document
	// it belongs to ("Điều 16 của / tại / trong Nghị định …", "Điều 16,
	// Nghị định …").
	legalChainLinkRe = regexp.MustCompile(`(?i)^\s*(?:của\s+|tại\s+|trong\s+|,\s*)?`)
	// legalListedPartRe joins a part or division listed after a chain
	// ("và Phụ lục …", ", Chương III"); the caller checks what follows.
	legalListedPartRe = regexp.MustCompile(`(?i)^\s*(?:và|hoặc)\s+`)
	// legalJoinedOperationRe: the clause goes on with another operation
	// (", bỏ cụm từ …" after the link's comma, "và sửa đổi khoản 3 …";
	// group 1 the "và").
	legalJoinedOperationRe = regexp.MustCompile(`(?i)^\s*((?:và|hoặc)\s+)?(?:sửa\s+đổi|bổ\s+sung|bãi\s+bỏ|thay\s+thế|thay|bỏ|đính\s+chính)(?:[^\p{L}]|$)`)

	// legalHeadingStopRe marks where a heading stops naming its subject:
	// the parent law ("quy định chi tiết … của Luật …"), earlier amenders
	// ("đã được sửa đổi … theo Luật số …").
	legalHeadingStopRe = regexp.MustCompile(`(?i)quy\s+định\s+chi\s+tiết|hướng\s+dẫn|thi\s+hành|được\s+sửa\s+đổi|được\s+bổ\s+sung|sau\s+đây\s+(?:gọi|viết)`)
	// legalHeadingJoinRe: two subjects joined ("Nghị định số 88/… và
	// Nghị định số 254/…", "…, Nghị định số …").
	legalHeadingJoinRe = regexp.MustCompile(`(?i)(?:^|\s|,|;)(?:và|,|;)\s*$`)
	legalCuaRe         = regexp.MustCompile(`(?i)(?:^|\s)của\s`)
	// legalDocTypeLeadRe / legalDocTypeTailRe are the type words before a
	// number ("Nghị định số ", "Thông tư liên tịch ").
	legalDocTypeLeadRe = regexp.MustCompile(`(?i)^(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết liên tịch|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định|Chỉ thị|văn bản)(?:\s+số)?\s*:?\s*`)
	legalDocTypeTailRe = regexp.MustCompile(`(?i)(?:Bộ luật|Luật|Pháp lệnh|Nghị định|Nghị quyết liên tịch|Nghị quyết|Thông tư liên tịch|Thông tư|Quyết định|Chỉ thị|văn bản)(?:\s+số)?\s*:?\s*$`)
)

// legalTypeWordsRe is a type written in a way legalDocTypeLeadRe does not
// read ("Nghị đỉnh số " in an OCR copy): up to three words opening like
// a type, "số", then the number. Other words ("Quy chế ban hành kèm
// theo", "Phụ lục II", "và Luật") are not a type.
var legalTypeWordsRe = regexp.MustCompile(`(?i)^(?:Bộ|Luật|Pháp|Nghị|Thông|Quyết|Chỉ|Văn|Hiến|Lệnh|Sắc)\s+(?:\p{L}+\s+){0,1}(?:\p{L}+\s+)?(?:số\s*:?\s*)?$`)

func legalTypeWords(gap string) bool { return legalTypeWordsRe.MatchString(gap) }

// legalHeadingNames reports whether the heading names the part text
// starts with ("Quy chế Đại lý đổi ngoại tệ" under "Sửa đổi … của Quy
// chế Đại lý đổi ngoại tệ ban hành kèm theo …"): the subject's part.
func legalHeadingNames(heading, text string) bool {
	w := strings.Fields(strings.ToLower(text))
	if len(w) < 3 {
		return false
	}
	return strings.Contains(strings.Join(strings.Fields(strings.ToLower(heading)), " "), strings.Join(w[:3], " "))
}

// legalHistoryOnly reports whether the chain of "Điều N" (keyword at byte
// kw, chain ending at pos) is the subject of an amendment history and no
// operation: "Điều 8 đã bị bãi bỏ theo khoản 3 Điều 1 Thông tư số …"
// (the history names its author's document, no operation verb comes
// before). "Điều 7 được sửa đổi tại khoản 9 như sau" is an operation.
func legalHistoryOnly(line string, kw, pos int, cites []vietnamese_legal.Citation) bool {
	loc := legalHistoryAfterRe.FindStringIndex(line[pos:])
	if loc == nil {
		return false
	}
	// an operation verb of this clause, not of an earlier sentence
	clause := line[:kw]
	if k := max(strings.LastIndex(clause, ". "), strings.LastIndex(clause, ";")); k >= 0 {
		clause = clause[k+1:]
	}
	if legalOperationVerbRe.MatchString(clause) {
		return false
	}
	return legalClauseCitation(line, pos+loc[1], cites) != nil
}

// legalClauseEnd is the byte offset where the clause holding byte pos of
// line ends: a ";" or a sentence end.
func legalClauseEnd(line string, pos int) int {
	end := len(line)
	if k := strings.IndexAny(line[pos:], ";"); k >= 0 {
		end = pos + k
	}
	if k := strings.Index(line[pos:], ". "); k >= 0 && pos+k < end {
		end = pos + k
	}
	return end
}

// legalClauseCitation is the first of cites after byte offset pos of line
// in the same clause (before a ";" or a sentence end).
func legalClauseCitation(line string, pos int, cites []vietnamese_legal.Citation) *vietnamese_legal.Citation {
	from, to := utf8.RuneCountInString(line[:pos]), utf8.RuneCountInString(line[:legalClauseEnd(line, pos)])
	for i := range cites {
		if cites[i].Start >= from && cites[i].Start < to {
			return &cites[i]
		}
	}
	return nil
}

// legalDropInlineNewText removes the new wording a line quotes, from its
// opening quote to the closing one (to the end of the line when it does
// not close there). Only the quoted span goes: a chunk flattened to one
// line ("… như sau: "1. …" 13. Sửa đổi, bổ sung Điều 24 như sau: …")
// keeps the operations after it.
func legalDropInlineNewText(line string) string {
	var b strings.Builder
	for {
		loc := legalInlineNewTextRe.FindStringIndex(line)
		if loc == nil {
			b.WriteString(line)
			return legalDropUnquotedNewText(b.String())
		}
		b.WriteString(line[:loc[0]])
		b.WriteString(" ")
		rest := line[loc[0]:]
		q := strings.IndexAny(rest, "“\"")
		open, size := utf8.DecodeRuneInString(rest[q:])
		end := -1
		if open == '"' {
			if k := strings.IndexByte(rest[q+size:], '"'); k >= 0 {
				end = q + size + k + 1
			}
		} else {
			// a phrase (cụm từ “…”) closes at the first quote; new wording
			// may quote phrases itself
			phrase := strings.HasPrefix(strings.ToLower(rest), "cụm")
			depth := 1
			for i, r := range rest[q+size:] {
				if r == '“' && !phrase {
					depth++
				} else if r == '”' || r == '"' && depth == 1 { // “…" happens
					if depth--; depth == 0 {
						end = q + size + i + utf8.RuneLen(r)
						break
					}
				}
			}
		}
		if end < 0 {
			return legalDropUnquotedNewText(b.String())
		}
		line = rest[end:]
	}
}

var (
	legalNhuSauRe = regexp.MustCompile(`(?i)như\s+sau\s*:?`)
	// legalOperationStartRe starts a numbered operation: "13. Sửa đổi …",
	// "a) Bãi bỏ …", "12. Điểm a khoản 8 Điều 34 …", a bullet "- Bỏ …" at
	// the start or after ":", ";" or a sentence end (a dash inside a
	// sentence, "từ 2020 - bỏ mức thu", is no bullet).
	// legalOperationLeadRe: the text after "như sau:" is itself an
	// operation ("… như sau: Bãi bỏ khoản 4 Điều 21 Thông tư …").
	legalOperationLeadRe  = regexp.MustCompile(`^\s*(?:[-–•+]\s*)?(?:Sửa\s+đổi|Bổ\s+sung|Bãi\s+bỏ|Thay\s+thế|Thay|Bỏ|Đính\s+chính)\s`)
	legalOperationStartRe = regexp.MustCompile(`(?i)(?:(?:^|\s)(?:\d+[a-zđ]?\.|[a-zđ]\))|(?:^|[:;.])\s*[-–•+])\s*(?:sửa\s+đổi|bổ\s+sung|bãi\s+bỏ|thay\s+thế|thay|bỏ|đính\s+chính|(?:điểm|khoản|điều)\s+(?:\d+[a-zđ]?|[a-zđ])(?:[\s,]|$))`)
)

// legalDropUnquotedNewText removes the new wording written after "như
// sau:" without quotes, up to the next numbered operation of a line
// flattened from several ("… Điều 15 như sau: a) Sửa đổi khoản 1 …" keeps
// "a) Sửa đổi …"), else to the end of the line. What is kept on each
// side of the dropped wording is joined with " ; ", a clause end: "Sửa đổi
// một số điều như sau 1. Bãi bỏ Điều 3" never reads "điều 1".
func legalDropUnquotedNewText(line string) string {
	var b strings.Builder
	for {
		loc := legalNhuSauRe.FindStringIndex(line)
		if loc == nil {
			b.WriteString(line)
			return b.String()
		}
		b.WriteString(line[:loc[0]])
		rest := line[loc[1]:]
		if legalOperationLeadRe.MatchString(rest) {
			b.WriteString(" ; ") // "… như sau: Bãi bỏ khoản 4 Điều 21 …"
			line = rest
			continue
		}
		next := legalOperationStartRe.FindStringIndex(rest)
		if next == nil {
			return b.String()
		}
		b.WriteString(" ; ")
		line = rest[next[0]:]
	}
}

// legalNamedBefore reports whether c is a numbered law whose title starts
// at byte offset pos of line ("Luật Đất đai số 45/2013/QH13": the
// citation starts at the number, its Name at "Luật").
func legalNamedBefore(line string, pos int, c vietnamese_legal.Citation) bool {
	if c.Key == "" || c.Name == "" {
		return false
	}
	rest := strings.ToLower(line[pos:])
	name := strings.ToLower(strings.Join(strings.Fields(c.Name), " "))
	if !strings.HasPrefix(strings.Join(strings.Fields(rest), " "), name) {
		return false
	}
	// the number follows the name, nothing else cited in between
	at := utf8.RuneCountInString(line[:pos])
	return c.Start > at && c.Start-at <= utf8.RuneCountInString(c.Name)+8
}

// legalUnreadLawAt reports whether a law FindCitations did not read (a
// title outside its lists, in lower case) is named at byte offset pos of
// line: a Điều whose chain ends there is that law's.
func legalUnreadLawAt(line string, pos int, aBase string) bool {
	loc := legalLawWordRe.FindStringSubmatchIndex(line[pos:])
	if loc == nil || strings.TrimSpace(line[pos:pos+loc[2]]) != "" {
		return false
	}
	return legalUnreadLawBetween(line, utf8.RuneCountInString(line[:pos]), utf8.RuneCountInString(line[:pos+loc[1]]), aBase)
}

// legalCitationIsA matches a citation to document A by its number key, or,
// partially written ("Nghị định 14", "NĐ 14/2016"), by number, year and
// type; a law cited by title by its title base.
func legalCitationIsA(aNumber, aType, aBase string) func(vietnamese_legal.Citation) bool {
	aKey := vietnamese_legal.CatalogNumberKey(aNumber)
	parts := strings.SplitN(vietnamese_legal.NormalizeCitationNumber(aNumber), "/", 3)
	aNo, aYear := "", ""
	if len(parts) >= 2 {
		aNo, aYear = strings.TrimLeft(parts[0], "0"), parts[1]
	}
	return func(c vietnamese_legal.Citation) bool {
		switch {
		case c.Key != "":
			return c.Key == aKey
		case c.Kind == vietnamese_legal.CitationPartial:
			return aNo != "" && strings.TrimLeft(c.No, "0") == aNo && (c.Year == "" || c.Year == aYear) &&
				(c.DocType == "" || aType == "" || vietnamese_legal.DocTypeClass(c.DocType) == vietnamese_legal.DocTypeClass(aType))
		case c.Kind == vietnamese_legal.CitationName && aBase != "":
			n := strings.ToLower(strings.Join(strings.Fields(c.Name), " "))
			return n == aBase || strings.HasPrefix(n, aBase+" ") || strings.HasPrefix(n, aBase+",")
		}
		return false
	}
}

// legalHeadingSubjectIsA reports whether the subject of the chunk's heading
// (a JSON list of headings, innermost last) is A: in the innermost heading
// citing a document, the first document after "của" (else the first
// cited); citations after the parent-law or earlier-amender words do not
// count; a subject joined to another ("A và B", "A, B") is ambiguous.
func legalHeadingSubjectIsA(heading, aBase string, isA func(vietnamese_legal.Citation) bool) bool {
	var parts []string
	if json.Unmarshal([]byte(heading), &parts) != nil {
		parts = []string{heading}
	}
	for i := len(parts) - 1; i >= 0; i-- {
		h := parts[i]
		var cites []vietnamese_legal.Citation
		for _, c := range vietnamese_legal.FindCitationsKnowing(h, aBase) {
			if c.Key != "" || c.Kind == vietnamese_legal.CitationName || c.Kind == vietnamese_legal.CitationPartial {
				cites = append(cites, c)
			}
		}
		if len(cites) == 0 {
			continue
		}
		from := 0
		if loc := legalCuaRe.FindStringIndex(h); loc != nil {
			from = utf8.RuneCountInString(h[:loc[1]])
		}
		subject := -1
		for k, c := range cites {
			if c.Start >= from {
				subject = k
				break
			}
		}
		if subject < 0 {
			subject = 0
		}
		if !isA(cites[subject]) {
			return false
		}
		if subject+1 < len(cites) {
			r := []rune(h)
			between := legalDocTypeTailRe.ReplaceAllString(string(r[cites[subject].End:cites[subject+1].Start]), "")
			if !legalHeadingStopRe.MatchString(between) && legalHeadingJoinRe.MatchString(between) {
				return false // "A và B": which one is amended is not said
			}
		}
		return true
	}
	return false
}

// chainHop derives C → A edges for the amenders amenderIDs of A, reading
// only C's chunks that name an article of B that wrote into A (origins).
func (l *legalAmendLoader) chainHop(amenderIDs, origins []string, load *LegalAmendLoad) error {
	var rels []struct{ FromID, ToID string }
	if err := l.db.Raw(`SELECT from_id, to_id FROM legal_document_relations WHERE relation = ? AND to_id IN ? ORDER BY to_id, from_id LIMIT ?`,
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
	cands, _, err := l.amendingChunks(cIDs, origins, nil, nil, nil)
	if err != nil {
		return err
	}
	bTargets := map[string]LegalAmendDoc{}
	for _, id := range amenderIDs {
		if row := l.catalog[id]; row != nil {
			bTargets[id] = l.catalogDoc(row)
		}
	}
	cEdges, _, err := l.edgesFrom(cands, bTargets, bAmenders, nil)
	if err != nil {
		return err
	}
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

// legalAmendProbe is an amends entry looked for: Điều Article of the
// document written Document (an index lookup), or of a law whose entry
// holds TitleBase (a slower text match, laws only).
type legalAmendProbe struct {
	Article, Document, TitleBase string
}

// amendingChunks reads candidate amending chunks, from the allowed KBs
// first (so a copy whose text may be read wins over a withheld one), then
// vbpl copies anywhere; never from the knowledges skip (A itself):
//   - the chunks of the documents docIDs (catalogue amenders) whose amends
//     name one of articles (all of them when articles is empty);
//   - (Postgres) chunks with an entry matching one of probes; (SQLite)
//     every chunk with amends.
//
// Entries naming the document in another form ("Luật X số …" for a decree,
// a typo) are found only through the catalogue relation.
//   - (Postgres) chunks with a phrase entry for one of articles whose
//     document cites one of aKeys, however written ("Thông tư số
//     24/2019/TT-NHNN quy định về …"), read light and filtered here.
//
// truncated reports a read that hit its limit (the amenders' changes may
// not all have been read).
func (l *legalAmendLoader) amendingChunks(docIDs, articles []string, probes []legalAmendProbe, aKeys, skip []string) (out []legalAmendRow, truncated bool, err error) {
	scope := `(c.knowledge_base_id IN ? OR ` + legalJSONText(l.db, "c.metadata", "legal", "source") + ` = 'vbpl')`
	allowed := make([]string, 0, len(l.allowed)+1)
	for id := range l.allowed {
		allowed = append(allowed, id)
	}
	sort.Strings(allowed)
	allowed = append(allowed, "") // IN () is invalid SQL
	notSkip := ""
	var skipArgs []interface{}
	if len(skip) > 0 {
		notSkip = ` AND c.knowledge_id NOT IN ?`
		skipArgs = append(skipArgs, skip)
	}
	pg := l.db.Dialector.Name() == "postgres"
	hasAmends := legalJSONText(l.db, "c.metadata", "legal", "amends") + ` IS NOT NULL`
	if pg {
		hasAmends = `jsonb_exists(c.metadata->'legal', 'amends')`
	}
	// allowed copies first, then by id: a stable, preferable order
	order := ` ORDER BY CASE WHEN c.knowledge_base_id IN ? THEN 0 ELSE 1 END, c.id LIMIT ?`
	var articleConds []string
	var articleArgs []interface{}
	if pg {
		for _, a := range articles {
			filter, _ := json.Marshal([]map[string]string{{"article": a}})
			articleConds = append(articleConds, `c.metadata->'legal'->'amends' @> ?::jsonb`)
			articleArgs = append(articleArgs, string(filter))
		}
	}
	// the reads run at once
	var byDoc, byArticle []legalAmendRow
	var byPhrase []struct {
		ID     string
		Amends []byte
	}
	g, _ := errgroup.WithContext(l.ctx)
	if len(docIDs) > 0 {
		q := legalAmendIDSelect + ` AND ` + legalJSONText(l.db, "k.metadata", "external_id") + ` IN ? AND ` + scope + notSkip + ` AND ` + hasAmends
		args := append([]interface{}{docIDs, allowed}, skipArgs...)
		if len(articleConds) > 0 {
			q += ` AND (` + strings.Join(articleConds, " OR ") + `)`
			args = append(args, articleArgs...)
		}
		args = append(args, allowed, legalAmendCandidates)
		legalGo(g, func() error {
			if err := l.db.Raw(q+order, args...).Scan(&byDoc).Error; err != nil {
				return fmt.Errorf("amending chunks of amenders: %w", err)
			}
			return nil
		})
	}
	if (pg && len(probes) > 0) || (!pg && len(articles) > 0) {
		q := legalAmendIDSelect + ` AND ` + hasAmends + ` AND ` + scope + notSkip
		args := append([]interface{}{allowed}, skipArgs...)
		if pg {
			var pc []string
			for _, p := range probes {
				if p.Document != "" {
					filter, _ := json.Marshal([]map[string]string{{"article": p.Article, "document": p.Document}})
					pc = append(pc, `c.metadata->'legal'->'amends' @> ?::jsonb`)
					args = append(args, string(filter))
				} else {
					filter, _ := json.Marshal([]map[string]string{{"article": p.Article}})
					pc = append(pc, `(c.metadata->'legal'->'amends' @> ?::jsonb AND strpos(lower(c.metadata->'legal'->>'amends'), ?) > 0)`)
					args = append(args, string(filter), strings.ToLower(p.TitleBase))
				}
			}
			q += ` AND (` + strings.Join(pc, " OR ") + `)`
		}
		args = append(args, allowed, legalAmendCandidates)
		legalGo(g, func() error {
			if err := l.db.Raw(q+order, args...).Scan(&byArticle).Error; err != nil {
				return fmt.Errorf("amending chunks by article: %w", err)
			}
			return nil
		})
	}
	if pg && len(articles) > 0 && len(aKeys) > 0 {
		var pc []string
		args := append([]interface{}{allowed}, skipArgs...)
		for _, a := range articles {
			filter, _ := json.Marshal([]map[string]interface{}{{"article": a, "phrase": true}})
			pc = append(pc, `c.metadata->'legal'->'amends' @> ?::jsonb`)
			args = append(args, string(filter))
		}
		q := `SELECT c.id, c.metadata->'legal'->'amends' AS amends FROM chunks c JOIN knowledges k ON k.id = c.knowledge_id
			WHERE c.deleted_at IS NULL AND k.deleted_at IS NULL AND ` + hasAmends + ` AND ` + scope + notSkip +
			` AND (` + strings.Join(pc, " OR ") + `)`
		args = append(args, allowed, legalAmendPhraseCandidates)
		legalGo(g, func() error {
			if err := l.db.Raw(q+order, args...).Scan(&byPhrase).Error; err != nil {
				return fmt.Errorf("amending chunks by phrase: %w", err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, false, err
	}
	truncated = len(byDoc) >= legalAmendCandidates || len(byArticle) >= legalAmendCandidates || len(byPhrase) >= legalAmendPhraseCandidates
	seen := map[string]bool{}
	var ids []string
	for _, r := range append(byDoc, byArticle...) {
		if !seen[r.ID] {
			seen[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	// phrase entries naming a target by any wording of its number
	keys := map[string]bool{}
	for _, k := range aKeys {
		keys[k] = true
	}
	arts := map[string]bool{}
	for _, a := range articles {
		arts[a] = true
	}
	for _, r := range byPhrase {
		if seen[r.ID] {
			continue
		}
		if l.namesPhraseTarget(r.Amends, arts, keys) {
			seen[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	// the rows themselves, read once by id (the filters above read ids only)
	if len(ids) > 0 {
		if err := l.db.Raw(legalAmendSelect+` AND c.id IN ?`, ids).Scan(&out).Error; err != nil {
			return nil, false, fmt.Errorf("amending chunks: %w", err)
		}
	}
	pos := make(map[string]int, len(ids))
	for i, id := range ids {
		pos[id] = i
	}
	sort.SliceStable(out, func(i, j int) bool { return pos[out[i].ID] < pos[out[j].ID] })
	// the allowed copies first across both reads
	sort.SliceStable(out, func(i, j int) bool { return l.allowed[out[i].KnowledgeBaseID] && !l.allowed[out[j].KnowledgeBaseID] })
	return out, truncated, nil
}

// legalGo runs fn in g, turning a panic into the group's error (a panic
// in a goroutine would bypass the caller's recover).
func legalGo(g *errgroup.Group, fn func() error) {
	g.Go(func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()
		return fn()
	})
}

// namesPhraseTarget reports whether a chunk's amends (JSON) hold a phrase
// change of one of arts whose document cites one of keys, however the
// document is written ("Thông tư số 24/2019/TT-NHNN quy định về …").
func (l *legalAmendLoader) namesPhraseTarget(amends []byte, arts, keys map[string]bool) bool {
	var entries []vietnamese_legal.AmendRef
	if json.Unmarshal(amends, &entries) != nil {
		return false
	}
	for _, a := range entries {
		if !a.Phrase || !arts[strings.ToLower(a.Article)] {
			continue
		}
		for _, c := range l.citations(a.Document) {
			if keys[c.Key] {
				return true
			}
		}
	}
	return false
}

// legalNamedByTitle reports the types texts cite by title alone ("Luật
// Đất đai"); decrees and circulars are cited by number.
func legalNamedByTitle(docType string) bool {
	switch strings.ToLower(strings.TrimSpace(docType)) {
	case "luật", "bộ luật", "pháp lệnh", "hiến pháp", "luật tổ chức":
		return true
	}
	return false
}

// structures reads the provisions of the knowledges kids from their
// chunks, in one query.
// The second map holds each knowledge's repeated Điều numbers
// (legalRepeatedArticles).
func (l *legalAmendLoader) structures(kids []string) (map[string]LegalDocStructure, map[string]legalRepeated, error) {
	out := map[string]LegalDocStructure{}
	repeated := map[string]legalRepeated{}
	if len(kids) == 0 {
		return out, repeated, nil
	}
	var metas []struct {
		KnowledgeID string
		Metadata    []byte
	}
	// only the structure fields (the full metadata is large)
	sel := `SELECT c.knowledge_id, c.metadata FROM chunks c`
	if l.db.Dialector.Name() == "postgres" {
		sel = `SELECT c.knowledge_id, jsonb_build_object('legal', jsonb_build_object(
			'article_nos', c.metadata->'legal'->'article_nos', 'khoan_nos', c.metadata->'legal'->'khoan_nos',
			'subdivision_refs', c.metadata->'legal'->'subdivision_refs',
			'provision_ids', c.metadata->'legal'->'provision_ids')) AS metadata FROM chunks c`
	}
	if err := l.db.Raw(sel+` WHERE c.deleted_at IS NULL AND c.knowledge_id IN ?`, kids).Scan(&metas).Error; err != nil {
		return nil, nil, fmt.Errorf("structure: %w", err)
	}
	lists := map[string][]*vietnamese_legal.LegalMetadata{}
	for _, m := range metas {
		var meta types.DocumentChunkMetadata
		if json.Unmarshal(m.Metadata, &meta) == nil && meta.Legal != nil {
			lists[m.KnowledgeID] = append(lists[m.KnowledgeID], meta.Legal)
		}
	}
	for _, k := range kids {
		out[k] = legalStructureFrom(lists[k])
		repeated[k] = legalRepeatedArticles(lists[k])
	}
	return out, repeated, nil
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
	const sel = `SELECT id, document_number, number_key, doc_type, title, issued_on, effective_on, expired_on, status FROM legal_documents WHERE `
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

// neverInForce reports an amender recorded to expire on or before the day
// it takes effect: its changes never applied.
func (l *legalAmendLoader) neverInForce(r *legalCatalogRow) bool {
	return r != nil && r.EffectiveOn != nil && r.ExpiredOn != nil && !r.ExpiredOn.After(*r.EffectiveOn)
}

func (l *legalAmendLoader) dateOf(t *time.Time) LegalAmendDate {
	if t == nil || t.IsZero() {
		return LegalAmendDate{}
	}
	return legalDay(t.Format("2006-01-02"), l.loc)
}

// edgesFrom builds the edges of the amending chunks cands on the target
// documents (by legalDocKey), and the document-level facts (an entry
// naming a document without a provision). amenders lists, by target
// catalogue id, the documents the catalogue records amending it: an
// operation naming no document attaches to the target only through it.
// Copies of one amender are one source: the first (allowed) copy wins.
func (l *legalAmendLoader) edgesFrom(cands []legalAmendRow, targets map[string]LegalAmendDoc,
	amenders map[string][]string, structures map[string]LegalDocStructure,
) (map[string][]LegalAmendEdge, map[string][]LegalCatalogAmendment, error) {
	out := map[string][]LegalAmendEdge{}
	facts := map[string][]LegalCatalogAmendment{}
	seen := map[string]bool{}
	index := map[string]int{}
	byNumber := legalTargetsByNumber(targets)
	type cand struct {
		row   legalAmendRow
		legal *vietnamese_legal.LegalMetadata
		km    map[string]interface{}
	}
	var list []cand
	var bIDs, keys []string
	for _, r := range cands {
		legal, km := parseLegalAmendRow(r)
		if legal == nil || len(legal.Amends) == 0 {
			continue
		}
		list = append(list, cand{r, legal, km})
		if id := firstNonEmptyString(legal.SourceDocID, kmString(km, "external_id")); id != "" {
			bIDs = append(bIDs, id)
		}
		for _, a := range legal.Amends {
			for _, c := range l.citations(a.Document) {
				if c.Key != "" {
					keys = append(keys, c.Key)
				}
			}
		}
	}
	// one read for every amender and every cited number
	_ = l.loadCatalog(bIDs, keys)
	// the name-only citations of a target, looked up a few at once
	queries := map[string]LegalNameQuery{}
	for _, cd := range list {
		bID := firstNonEmptyString(cd.legal.SourceDocID, kmString(cd.km, "external_id"))
		issued := legalDay(kmString(cd.km, "issue_date"), l.loc).At
		if row := l.catalog[bID]; row != nil && row.IssuedOn != nil {
			issued = *row.IssuedOn
		}
		var groups []legalQuoteGroup
		for _, a := range cd.legal.Amends {
			if strings.TrimSpace(a.Document) == "" && groups == nil {
				groups = legalQuoteGroups(cd.row.Content)
			}
			name := l.amendName(a, cd.legal, groups)
			if l.hasNumberCitation(name) {
				continue
			}
			if _, _, ok := l.recordedTarget(name, LegalAmendDoc{CatalogID: bIDIfKnown(l, bID)}, targets, amenders); ok {
				continue
			}
			if issued.IsZero() {
				continue // never resolved by name: amendTarget flags it
			}
			if l.nameMayBeTarget(name, targets) {
				amenderID := ""
				if l.catalog[bID] != nil {
					amenderID = bID
				}
				queries[legalNameMemoKey(name, issued, amenderID)] = LegalNameQuery{Name: name, At: issued, AmenderID: amenderID}
			}
		}
	}
	if err := l.prefetchNames(queries); err != nil {
		return nil, nil, err
	}
	for _, cd := range list {
		r, legal, km := cd.row, cd.legal, cd.km
		b := LegalAmendDoc{Number: kmString(km, "doc_number"), Type: kmString(km, "doc_type"),
			KnowledgeID: r.KnowledgeID, KnowledgeBaseID: r.KnowledgeBaseID, ChunkID: r.ID}
		bID := firstNonEmptyString(legal.SourceDocID, kmString(km, "external_id"))
		var eff LegalAmendDate
		var issued time.Time
		bProblem := ""
		if row := l.catalog[bID]; row != nil {
			b.CatalogID = bID
			b.Number = firstNonEmptyString(b.Number, row.DocumentNumber)
			b.Type = firstNonEmptyString(b.Type, row.DocType)
			eff = l.dateOf(row.EffectiveOn)
			if row.IssuedOn != nil {
				issued = *row.IssuedOn
			}
			if l.neverInForce(row) {
				bProblem = "danh mục ghi văn bản sửa đổi hết hiệu lực trước hoặc ngay khi có hiệu lực"
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
		scope := newProvisionScope(r.Content)
		hash := sha256.Sum256([]byte(r.Content))
		for _, a := range legal.Amends {
			tKey, tDoc, problem, ok := l.amendTarget(a, legal, groups, b, issued, targets, byNumber, amenders)
			if !ok {
				continue
			}
			if a.Article == "" && !a.Phrase {
				// B amends A, no provision named: a catalogue-level fact
				k := "doc|" + legalDocKey(b) + "|" + tKey
				if !seen[k] {
					seen[k] = true
					facts[tKey] = append(facts[tKey], LegalCatalogAmendment{
						Amender: LegalAmendDoc{CatalogID: b.CatalogID, Number: b.Number, Type: b.Type}, EffFrom: eff,
						NeverInForce: l.neverInForce(l.catalog[bID])})
				}
				continue
			}
			e := l.parserEdge(r, legal, scope, a, groups, b, tDoc, eff, structures[tKey])
			e.ChunkHash = hex.EncodeToString(hash[:8])
			if p := firstNonEmptyString(problem, bProblem); p != "" && e.Unresolved == "" {
				e.Unresolved = p
				e.Tier = LegalTierUnverified
			}
			ValidateLegalAmendEdge(&e, structures[tKey])
			// identity only: two copies of B are one source; the first
			// (allowed) copy wins, unless it could not be placed and a copy
			// as readable could (converter versions differ between copies)
			k := strings.Join([]string{legalDocKey(b), tKey, string(e.Op), e.Provision.key(), foldSpaces(e.From), foldSpaces(e.To), e.NewLabel}, "|")
			if at, dup := index[k]; dup {
				have := &out[tKey][at]
				if have.Unresolved != "" && e.Unresolved == "" && (e.TextWithheld == have.TextWithheld || !e.TextWithheld) {
					*have = e
				}
				continue
			}
			index[k] = len(out[tKey])
			out[tKey] = append(out[tKey], e)
		}
	}
	return out, facts, nil
}

// legalTargetsByNumber lists the target keys by number key. A số hiệu
// is not unique: every target of a key is kept, sorted so that a pick
// does not depend on map order.
func legalTargetsByNumber(targets map[string]LegalAmendDoc) map[string][]string {
	byNumber := map[string][]string{}
	for key, d := range targets {
		k := vietnamese_legal.CatalogNumberKey(d.Number)
		byNumber[k] = append(byNumber[k], key)
	}
	for _, keys := range byNumber {
		sort.Strings(keys)
	}
	return byNumber
}

// amendTarget resolves the document an amends entry names to one of the
// targets; ok is false when it is another document. problem is set when
// the entry attaches to a target without a certain identity.
func (l *legalAmendLoader) amendTarget(a vietnamese_legal.AmendRef, legal *vietnamese_legal.LegalMetadata, groups []legalQuoteGroup,
	b LegalAmendDoc, issued time.Time, targets map[string]LegalAmendDoc, byNumber map[string][]string, amenders map[string][]string,
) (key string, doc LegalAmendDoc, problem string, ok bool) {
	name := l.amendName(a, legal, groups)
	if name == "" {
		// only a catalogue relation ties it to a target
		for k, d := range targets {
			if d.CatalogID != "" && containsString(amenders[d.CatalogID], b.CatalogID) && b.CatalogID != "" {
				return k, d, "thao tác không ghi văn bản bị sửa đổi (danh mục ghi " + b.label() + " sửa đổi văn bản này)", true
			}
		}
		return "", LegalAmendDoc{}, "", false
	}
	for _, c := range l.citations(name) {
		if c.Key == "" || c.Role == vietnamese_legal.CitationRoleInAmendingTitle {
			continue
		}
		held := byNumber[c.Key]
		if len(held) == 0 {
			return "", LegalAmendDoc{}, "", false
		}
		rows := l.byKey[c.Key]
		if _, loaded := l.byKey[c.Key]; !loaded {
			_ = l.loadCatalog(nil, []string{c.Key})
			rows = l.byKey[c.Key]
		}
		recorded := func(hk string) bool {
			id := targets[hk].CatalogID
			return id != "" && b.CatalogID != "" && containsString(amenders[id], b.CatalogID)
		}
		// a số hiệu is not unique (100/2015/QH13 is Bộ luật Hình sự and a
		// Nghị quyết): the type written with it narrows the targets and the
		// catalogue rows first
		if wt := legalWrittenType(name, c); wt != "" {
			var typed, untyped []string
			for _, hk := range held {
				t := targets[hk].Type
				if r := l.catalog[targets[hk].CatalogID]; r != nil && r.DocType != "" {
					t = r.DocType
				}
				switch legalTypeClass(t) {
				case wt:
					typed = append(typed, hk)
				case "":
					untyped = append(untyped, hk) // no type known (an upload without doc_type): not ruled out
				}
			}
			var typedRows []*legalCatalogRow
			for _, r := range rows {
				if legalTypeClass(r.DocType) == wt {
					typedRows = append(typedRows, r)
				}
			}
			switch {
			case len(typed) > 0:
				held = typed
				if len(typedRows) > 0 {
					rows = typedRows
				}
			case len(untyped) > 0:
				held = untyped
			case len(typedRows) > 0:
				return "", LegalAmendDoc{}, "", false // another document of that number
			}
		}
		k := held[0]
		if len(held) > 1 {
			// targets sharing the số hiệu: only a catalogue relation B → A
			// tells which one B amends
			var rec []string
			for _, hk := range held {
				if recorded(hk) {
					rec = append(rec, hk)
				}
			}
			switch {
			case len(rec) == 1:
				k = rec[0]
			case l.twinTargets(held, targets):
				// copies of one document (same type and issue day, both
				// ingested): one of them, the one in force first
				k = l.preferredTwin(held, targets)
			default:
				return k, targets[k], "số hiệu " + c.Number + " ứng với nhiều văn bản", true
			}
		}
		d := targets[k]
		among := false
		var self *legalCatalogRow
		for _, r := range rows {
			if d.CatalogID != "" && r.ID == d.CatalogID {
				among, self = true, r
			}
		}
		// the other rows are copies of this document (same type, same issue
		// day: a twin row), not other documents
		twins := self != nil
		for _, r := range rows {
			if twins && r != self && (legalTypeClass(r.DocType) != legalTypeClass(self.DocType) || !legalSameDay(r.IssuedOn, self.IssuedOn)) {
				twins = false
			}
		}
		switch {
		case len(rows) > 1 && among:
			// a shared số hiệu, one of whose rows is this document: sure
			// for its twins, else only when the catalogue records B amending it
			if !twins && !recorded(k) {
				return k, d, "số hiệu " + c.Number + " ứng với nhiều văn bản trong danh mục", true
			}
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
	// issued — looked up only when it starts with a target's title; a name
	// holding a number is a citation FindCitations did not read, not a title
	if k, d, ok := l.recordedTarget(name, b, targets, amenders); ok {
		return k, d, "", true
	}
	if !l.nameMayBeTarget(name, targets) {
		return "", LegalAmendDoc{}, "", false
	}
	if issued.IsZero() {
		// which law of that name B means is read at B's issue date (§4.2):
		// unknown, the name is never resolved — it attaches to the targets
		// of that title, flagged
		var named []string
		for k, d := range targets {
			if l.titleMayCite(name, d) {
				named = append(named, k)
			}
		}
		sort.Strings(named)
		problem := "không rõ ngày ban hành văn bản sửa đổi, không xác định được “" + name + "” là văn bản nào"
		if len(named) > 1 {
			problem += " (tên ứng với nhiều văn bản)"
		}
		return named[0], targets[named[0]], problem, true
	}
	m, err := l.lookupName(name, issued, b.CatalogID)
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

// legalWrittenTypeRe is a type written right before a number ("Nghị
// quyết số ", "NĐ ").
var legalWrittenTypeRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(bộ luật|luật|pháp lệnh|nghị định|nghị quyết liên tịch|nghị quyết|thông tư liên tịch|thông tư|quyết định|chỉ thị|nđ|nq|ttlt|tt|qđ)\s*(?:số\s*:?\s*)?$`)

var legalTypeAbbrev = map[string]string{"nđ": "nghị định", "nq": "nghị quyết", "ttlt": "thông tư liên tịch", "tt": "thông tư", "qđ": "quyết định"}

// legalWrittenType is the type class (legalTypeClass) written with a
// numbered citation in text — by its name ("Bộ luật Hình sự số …") or
// the words right before the number — or "" when none is written (a type
// read from the number's code is a guess: QH is a law or a resolution).
func legalWrittenType(text string, c vietnamese_legal.Citation) string {
	if c.Name != "" {
		return legalTypeClass(c.DocType)
	}
	r := []rune(text)
	if c.Start > len(r) {
		return ""
	}
	m := legalWrittenTypeRe.FindStringSubmatch(string(r[max(0, c.Start-40):c.Start]))
	if m == nil {
		return ""
	}
	t := strings.ToLower(m[1])
	if full, ok := legalTypeAbbrev[t]; ok {
		t = full
	}
	return legalTypeClass(t)
}

// twinTargets reports whether the targets keys are catalogue twins: rows
// of one type class issued the same day.
func (l *legalAmendLoader) twinTargets(keys []string, targets map[string]LegalAmendDoc) bool {
	var first *legalCatalogRow
	for _, k := range keys {
		r := l.catalog[targets[k].CatalogID]
		if r == nil {
			return false
		}
		if first == nil {
			first = r
			continue
		}
		if legalTypeClass(r.DocType) != legalTypeClass(first.DocType) || !legalSameDay(r.IssuedOn, first.IssuedOn) {
			return false
		}
	}
	return first != nil
}

// preferredTwin picks one of twin targets (keys sorted): the first whose
// catalogue status says in force, else the first.
func (l *legalAmendLoader) preferredTwin(keys []string, targets map[string]LegalAmendDoc) string {
	for _, k := range keys {
		if r := l.catalog[targets[k].CatalogID]; r != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Status)), "còn hiệu lực") {
			return k
		}
	}
	return keys[0]
}

// legalSameDay reports whether two dates are the same, known day.
func legalSameDay(a, b *time.Time) bool {
	return a != nil && b != nil && a.Format("2006-01-02") == b.Format("2006-01-02")
}

// legalTypeClass folds a document type for comparison
// (vietnamese_legal.DocTypeClass: bộ luật = luật, liên tịch = its type).
func legalTypeClass(t string) string { return vietnamese_legal.DocTypeClass(t) }

// amendName is the document an amends entry names: as written, else the
// one document its lead-in or a heading cites.
func (l *legalAmendLoader) amendName(a vietnamese_legal.AmendRef, legal *vietnamese_legal.LegalMetadata, groups []legalQuoteGroup) string {
	if name := strings.TrimSpace(a.Document); name != "" {
		return name
	}
	var texts []string
	for _, g := range groups {
		if a.Article != "" && legalMentions(g.leadIn, "điều", a.Article) {
			texts = append(texts, g.leadIn)
		}
	}
	for i := len(legal.HeadingPath) - 1; i >= 0; i-- {
		texts = append(texts, legal.HeadingPath[i])
	}
	for _, t := range texts {
		var found []string
		for _, c := range l.citations(t) {
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

// hasNumberCitation reports whether text cites a document by number.
func (l *legalAmendLoader) hasNumberCitation(text string) bool {
	for _, c := range l.citations(text) {
		if c.Key != "" {
			return true
		}
	}
	return false
}

// nameMayBeTarget reports whether a name without a number may cite one of
// the targets (it starts with a target's title): only then is the
// catalogue asked. A name holding a number is a citation FindCitations
// did not read, not a title.
func (l *legalAmendLoader) nameMayBeTarget(name string, targets map[string]LegalAmendDoc) bool {
	if name == "" || strings.IndexFunc(name, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0 {
		return false
	}
	for _, d := range targets {
		if l.titleMayCite(name, d) {
			return true
		}
	}
	return false
}

// titleMayCite reports whether a name starts with the title of target d.
func (l *legalAmendLoader) titleMayCite(name string, d LegalAmendDoc) bool {
	base, ok := l.bases[d.CatalogID]
	if !ok {
		if row := l.catalog[d.CatalogID]; row != nil {
			base = LegalTitleBase(row.Title, row.DocType)
			// a title that is only its type ("pháp lệnh") names nothing
			if typ := strings.ToLower(strings.Join(strings.Fields(row.DocType), " ")); base == typ {
				base = ""
			}
		}
		l.bases[d.CatalogID] = base
	}
	folded := strings.ToLower(strings.Join(strings.Fields(name), " "))
	return base != "" && (folded == base || strings.HasPrefix(folded, base+" ") || strings.HasPrefix(folded, base+","))
}

// recordedTarget resolves a name without asking the catalogue when B is
// recorded to amend a target of exactly that title (the relation wins in
// ResolveLegalDocumentName too).
func (l *legalAmendLoader) recordedTarget(name string, b LegalAmendDoc, targets map[string]LegalAmendDoc, amenders map[string][]string) (string, LegalAmendDoc, bool) {
	if b.CatalogID == "" || name == "" {
		return "", LegalAmendDoc{}, false
	}
	folded := strings.TrimRight(strings.ToLower(strings.Join(strings.Fields(name), " ")), " ,.;:")
	for k, d := range targets {
		if d.CatalogID == "" || !containsString(amenders[d.CatalogID], b.CatalogID) {
			continue
		}
		row := l.catalog[d.CatalogID]
		if row != nil && LegalTitleBase(row.Title, row.DocType) == folded {
			return k, d, true
		}
	}
	return "", LegalAmendDoc{}, false
}

// bIDIfKnown is bID when the catalogue holds it, else "".
func bIDIfKnown(l *legalAmendLoader, bID string) string {
	if l.catalog[bID] != nil {
		return bID
	}
	return ""
}

func legalNameMemoKey(name string, issued time.Time, amenderID string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " ")) + "|" + issued.Format("2006-01-02") + "|" + amenderID
}

// lookupName is ResolveLegalDocumentName, memoised for the load and, for
// legalNameCacheTTL, for the process (the catalogue changes rarely; a
// review that changes it is picked up after the TTL).
func (l *legalAmendLoader) lookupName(name string, issued time.Time, amenderID string) (LegalNameMatch, error) {
	key := legalNameMemoKey(name, issued, amenderID)
	l.namesMu.Lock()
	memo, ok := l.names[key]
	l.namesMu.Unlock()
	if ok {
		return memo.m, memo.err
	}
	if m, hit := legalNameCache.get(key); hit {
		memo.m = m
	} else {
		memo.m, memo.err = ResolveLegalDocumentName(l.ctx, l.db, LegalNameQuery{Name: name, At: issued, AmenderID: amenderID})
		if memo.err == nil {
			legalNameCache.put(key, memo.m)
		}
	}
	l.namesMu.Lock()
	l.names[key] = memo
	l.namesMu.Unlock()
	return memo.m, memo.err
}

const (
	legalNameCacheTTL  = 10 * time.Minute
	legalNameCacheSize = 4096
)

// legalNameCache is the process-wide cache of catalogue name lookups.
var legalNameCache = &legalNameTTLCache{items: map[string]legalNameCached{}}

type legalNameCached struct {
	m  LegalNameMatch
	at time.Time
}

type legalNameTTLCache struct {
	mu    sync.Mutex
	items map[string]legalNameCached
}

func (c *legalNameTTLCache) get(key string) (LegalNameMatch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok || time.Since(it.at) > legalNameCacheTTL {
		return LegalNameMatch{}, false
	}
	return it.m, true
}

func (c *legalNameTTLCache) put(key string, m LegalNameMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= legalNameCacheSize {
		// drop the expired, else everything: simple and bounded
		for k, it := range c.items {
			if time.Since(it.at) > legalNameCacheTTL {
				delete(c.items, k)
			}
		}
		if len(c.items) >= legalNameCacheSize {
			c.items = map[string]legalNameCached{}
		}
	}
	c.items[key] = legalNameCached{m: m, at: time.Now()}
}

// legalNameLookups bounds the catalogue name lookups run at once.
const legalNameLookups = 4

// prefetchNames runs the catalogue name lookups edgesFrom will need, a
// few at once; a panic in one comes back as the error.
func (l *legalAmendLoader) prefetchNames(queries map[string]LegalNameQuery) error {
	if len(queries) == 0 {
		return nil
	}
	g, _ := errgroup.WithContext(l.ctx)
	g.SetLimit(legalNameLookups)
	for _, q := range queries {
		q := q
		legalGo(g, func() error {
			_, _ = l.lookupName(q.Name, q.At, q.AmenderID)
			return nil
		})
	}
	return g.Wait()
}

func containsLegalCandidate(m LegalNameMatch, id string) bool {
	for _, c := range m.Candidates {
		if c.Document != nil && c.Document.ID == id {
			return true
		}
	}
	return false
}

var (
	legalBoSungOnlyRe = regexp.MustCompile(`(?i)bổ sung`)
	legalSuaDoiRe     = regexp.MustCompile(`(?i)sửa đổi|được sửa|thay thế`)
)

// parserEdge turns one converter amends entry into an edge.
func (l *legalAmendLoader) parserEdge(r legalAmendRow, legal *vietnamese_legal.LegalMetadata, scope provisionScope, a vietnamese_legal.AmendRef, groups []legalQuoteGroup,
	b LegalAmendDoc, target LegalAmendDoc, eff LegalAmendDate, structure LegalDocStructure,
) LegalAmendEdge {
	prov := LegalProvisionRef{Article: a.Article, Khoan: a.Khoan, Diem: a.Diem}.normalized()
	e := LegalAmendEdge{Amender: b, Target: target, Provision: prov, EffFrom: eff,
		Sources: []string{LegalAmendSourceParser}, Tier: LegalTierUnverified}
	if !l.allowed[b.KnowledgeBaseID] {
		e.TextWithheld = true
	}
	article := ""
	if len(legal.ArticleNos) == 1 {
		article = strings.ToLower(legal.ArticleNos[0])
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
		// the line quoting the words of this very change ("cụm từ “X”
		// bằng “Y”", "bãi bỏ cụm từ “X”"), not the first place the words
		// occur (a heading, another change "bổ sung … sau từ “X”"); none
		// sure: no lead-in, so no verb check
		if start, end, ok := legalPhraseLeadIn(r.Content, a.From, a.To); ok {
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

// legalPhraseLeadIn finds the line of content that writes the phrase
// change from → to: it quotes from and, after it, to (a replacement) or
// says "bãi bỏ" (a removal).
func legalPhraseLeadIn(content, from, to string) (start, end int, ok bool) {
	for _, q := range [][2]string{{"“", "”"}, {`"`, `"`}} {
		quotedFrom := q[0] + from + q[1]
		for off := 0; ; {
			i := strings.Index(content[off:], quotedFrom)
			if i < 0 {
				break
			}
			i += off
			off = i + len(quotedFrom)
			start = strings.LastIndexByte(content[:i], '\n') + 1
			end = strings.IndexByte(content[i:], '\n')
			if end < 0 {
				end = len(content)
			} else {
				end += i
			}
			rest := content[off:end]
			if to != "" && strings.Contains(rest, q[0]+to+q[1]) {
				return start, end, true
			}
			if to == "" && strings.Contains(strings.ToLower(content[start:end]), "bãi bỏ") {
				return start, end, true
			}
		}
	}
	return 0, 0, false
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
			AllowedKBs: allowedKBs, Catalogue: load.Catalogue[key],
		})
	}
	return out
}
