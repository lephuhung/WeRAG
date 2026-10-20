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
	sg, _ := errgroup.WithContext(l.ctx)
	legalGo(sg, func() (err error) {
		byKnowledge, err = l.structures(aKnowledge)
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
		key, short string
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
		targets = append(targets, target{key, short})
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
			for _, line := range strings.Split(r.Content, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, ">") || strings.HasPrefix(line, "#") {
					continue
				}
				for _, m := range legalCitedArticleRe.FindAllStringSubmatch(line, -1) {
					art := strings.ToLower(m[1])
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
	truncated = len(byDoc) >= legalAmendCandidates
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
func (l *legalAmendLoader) structures(kids []string) (map[string]LegalDocStructure, error) {
	out := map[string]LegalDocStructure{}
	if len(kids) == 0 {
		return out, nil
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
			'subdivision_refs', c.metadata->'legal'->'subdivision_refs')) AS metadata FROM chunks c`
	}
	if err := l.db.Raw(sel+` WHERE c.deleted_at IS NULL AND c.knowledge_id IN ?`, kids).Scan(&metas).Error; err != nil {
		return nil, fmt.Errorf("structure: %w", err)
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
	}
	return out, nil
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
	const sel = `SELECT id, document_number, number_key, doc_type, title, issued_on, effective_on, expired_on FROM legal_documents WHERE `
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
	byNumber := map[string]string{}
	for key, d := range targets {
		byNumber[vietnamese_legal.CatalogNumberKey(d.Number)] = key
	}
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

// amendTarget resolves the document an amends entry names to one of the
// targets; ok is false when it is another document. problem is set when
// the entry attaches to a target without a certain identity.
func (l *legalAmendLoader) amendTarget(a vietnamese_legal.AmendRef, legal *vietnamese_legal.LegalMetadata, groups []legalQuoteGroup,
	b LegalAmendDoc, issued time.Time, targets map[string]LegalAmendDoc, byNumber map[string]string, amenders map[string][]string,
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
		among := false
		for _, r := range rows {
			if d.CatalogID != "" && r.ID == d.CatalogID {
				among = true
			}
		}
		switch {
		case len(rows) > 1 && among:
			// a shared số hiệu, one of whose rows is this document
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
	folded := strings.ToLower(strings.Join(strings.Fields(name), " "))
	for _, d := range targets {
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
		if base != "" && (folded == base || strings.HasPrefix(folded, base+" ") || strings.HasPrefix(folded, base+",")) {
			return true
		}
	}
	return false
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
