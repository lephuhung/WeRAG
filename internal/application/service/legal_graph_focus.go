package service

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// The focused legal graph of a KB page: one catalogue document, the
// documents it acts on and those acting on it, and the provision-level
// links in both directions (the "bảng đối chiếu"). Scope mirrors KBGraph:
// the KB's documents with an approved legal link of a live knowledge in a
// live KB (anchors) and their direct neighbours; nothing further can be
// focused through the KB. Every scope check is a query, so a KB of any
// size is covered whole (no anchor cap).

const (
	// legalFocusGroupCap bounds the neighbours kept per direction and
	// relation (KB documents, then newest, first); the totals report the
	// rest.
	legalFocusGroupCap = 60
	// legalFocusMaxDocs bounds the documents loaded per focus (provision
	// rows' documents first, then neighbours, strongest relation first).
	legalFocusMaxDocs = 1500
	// legalFocusProvisionCap bounds the provision rows per direction (the
	// most tagged document holds 380).
	legalFocusProvisionCap = 600
	// legalFocusCandidates is the picker's length.
	legalFocusCandidates = 20
)

// legalRelationRank orders a pair's relations: the strongest draws the edge.
var legalRelationRank = map[types.LegalRelation]int{
	types.LegalRelationReplaces: 0, types.LegalRelationRepeals: 1, types.LegalRelationExpires: 2,
	types.LegalRelationPartiallyExpires: 3, types.LegalRelationSuspends: 4, types.LegalRelationAmends: 5,
	types.LegalRelationCorrects: 6, types.LegalRelationConsolidates: 7, types.LegalRelationGuides: 8,
	types.LegalRelationInterprets: 9, types.LegalRelationBasedOn: 10,
}

// legalDegreeRelations are the relations a held document's degree counts
// (the picker's order, the default focus): all drawn ones but "căn cứ".
var legalDegreeRelations = func() []types.LegalRelation {
	var out []types.LegalRelation
	for _, r := range legalGraphRelations {
		if r != types.LegalRelationBasedOn {
			out = append(out, r)
		}
	}
	return out
}()

type legalGraphFocusService struct {
	repo interfaces.LegalCatalogRepository
	db   *gorm.DB
}

// NewLegalGraphFocusService creates the focused legal graph reader.
func NewLegalGraphFocusService(repo interfaces.LegalCatalogRepository, db *gorm.DB) interfaces.LegalGraphFocusService {
	return &legalGraphFocusService{repo: repo, db: db}
}

// legalAnchorSQL selects the KB's held catalogue documents: approved links
// of live knowledge in a live KB (the joins of the review repository's
// liveLinks). Two arguments: the KB id and the approved status.
const legalAnchorSQL = `SELECT l.legal_document_id FROM legal_document_links l
	JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL
	JOIN knowledge_bases kb ON kb.id = l.knowledge_base_id AND kb.deleted_at IS NULL
	WHERE l.knowledge_base_id = ? AND l.review_status = ? AND l.legal_document_id <> ''`

type legalHeld struct{ knowledgeID, title string }

// held returns which of ids the KB holds, with the newest live knowledge
// holding each.
func (s *legalGraphFocusService) held(ctx context.Context, kbID string, ids []string) (map[string]legalHeld, error) {
	out := map[string]legalHeld{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct{ LegalDocumentID, KnowledgeID, Title string }
	err := s.db.WithContext(ctx).Raw(`SELECT l.legal_document_id, l.knowledge_id, k.title FROM legal_document_links l
		JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL
		JOIN knowledge_bases kb ON kb.id = l.knowledge_base_id AND kb.deleted_at IS NULL
		WHERE l.knowledge_base_id = ? AND l.review_status = ? AND l.legal_document_id IN ?
		ORDER BY l.updated_at DESC, l.knowledge_id`, kbID, types.LegalReviewApproved, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if _, ok := out[r.LegalDocumentID]; !ok {
			out[r.LegalDocumentID] = legalHeld{r.KnowledgeID, r.Title}
		}
	}
	return out, nil
}

// reachable returns which of ids may be focused through the KB: held, or
// joined to a held document by a drawn relation or a provision link.
func (s *legalGraphFocusService) reachable(
	ctx context.Context, kbID string, ids []string, held map[string]legalHeld,
) (map[string]bool, error) {
	out := map[string]bool{}
	var rest []string
	for _, id := range ids {
		if _, ok := held[id]; ok {
			out[id] = true
		} else if id != "" {
			rest = append(rest, id)
		}
	}
	if len(rest) == 0 {
		return out, nil
	}
	ap := types.LegalReviewApproved
	var hits []string
	err := s.db.WithContext(ctx).Raw(`SELECT r.from_id FROM legal_document_relations r
		WHERE r.from_id IN ? AND r.relation IN ? AND r.to_id IN (`+legalAnchorSQL+`)
		UNION SELECT r.to_id FROM legal_document_relations r
		WHERE r.to_id IN ? AND r.relation IN ? AND r.from_id IN (`+legalAnchorSQL+`)
		UNION SELECT p.target_doc_id FROM legal_provision_links p
		WHERE p.target_doc_id IN ? AND p.source_doc_id IN (`+legalAnchorSQL+`)
		UNION SELECT p.source_doc_id FROM legal_provision_links p
		WHERE p.source_doc_id IN ? AND p.target_doc_id IN (`+legalAnchorSQL+`)`,
		rest, legalGraphRelations, kbID, ap,
		rest, legalGraphRelations, kbID, ap,
		rest, kbID, ap,
		rest, kbID, ap).Scan(&hits).Error
	if err != nil {
		return nil, err
	}
	for _, id := range hits {
		out[id] = true
	}
	return out, nil
}

// topHeld returns the best-connected held documents (degree without "căn
// cứ"), at most limit, with their degree.
func (s *legalGraphFocusService) topHeld(ctx context.Context, kbID string, limit int) ([]string, map[string]int, error) {
	var rows []struct {
		ID     string
		Degree int
	}
	err := s.db.WithContext(ctx).Raw(`SELECT a.id,
		(SELECT COUNT(*) FROM legal_document_relations r WHERE r.from_id = a.id AND r.to_id <> a.id AND r.relation IN ?)
		+ (SELECT COUNT(*) FROM legal_document_relations r WHERE r.to_id = a.id AND r.from_id <> a.id AND r.relation IN ?) AS degree
		FROM (SELECT DISTINCT h.legal_document_id AS id FROM (`+legalAnchorSQL+`) h) a
		ORDER BY degree DESC, a.id LIMIT ?`,
		legalDegreeRelations, legalDegreeRelations, kbID, types.LegalReviewApproved, limit).Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(rows))
	deg := map[string]int{}
	for _, r := range rows {
		ids = append(ids, r.ID)
		deg[r.ID] = r.Degree
	}
	return ids, deg, nil
}

// searchHeld returns the held documents whose doc type, number and title
// hold every word of q, diacritics and case ignored: those whose type and
// number match first, then newest first. Only the KB's documents are
// read (materialized first: a semi-join would scan the whole catalogue).
// Postgres matches each folded word as a regex of its letters' accented
// forms (translate() with the whole table is ~25× slower); SQLite folds
// the text with REPLACE and matches with LIKE.
func (s *legalGraphFocusService) searchHeld(ctx context.Context, kbID, q string, limit int) ([]string, error) {
	words := strings.Fields(legalFoldText(q))
	if len(words) == 0 {
		return nil, nil
	}
	hayExpr := "(d.doc_type || ' ' || d.document_number || ' ' || d.title)"
	numExpr := "(d.doc_type || ' ' || d.document_number)"
	mat, cond := "", `LIKE ? ESCAPE '\'`
	if s.db.Dialector.Name() == "postgres" {
		mat, cond = "MATERIALIZED ", "~ ?"
		hayExpr, numExpr = "lower"+hayExpr, "lower"+numExpr
	} else {
		hayExpr, numExpr = legalFoldSQLite(hayExpr), legalFoldSQLite(numExpr)
	}
	pattern := func(w string) string {
		if mat != "" {
			return legalFoldRegex(w)
		}
		return "%" + legalLikeEscape(w) + "%"
	}
	args := []any{kbID, types.LegalReviewApproved}
	var inHay, inNum []string
	for range words {
		inHay = append(inHay, "x.hay "+cond)
		inNum = append(inNum, "x.num "+cond)
	}
	for range 2 {
		for _, w := range words {
			args = append(args, pattern(w))
		}
	}
	args = append(args, limit)
	var ids []string
	err := s.db.WithContext(ctx).Raw(`WITH a AS `+mat+`(SELECT DISTINCT h.legal_document_id AS id FROM (`+legalAnchorSQL+`) h),
		x AS `+mat+`(SELECT d.id, d.issued_on, `+hayExpr+` AS hay, `+numExpr+` AS num
			FROM a JOIN legal_documents d ON d.id = a.id)
		SELECT x.id FROM x WHERE `+strings.Join(inHay, " AND ")+`
		ORDER BY (CASE WHEN `+strings.Join(inNum, " AND ")+` THEN 0 ELSE 1 END), (x.issued_on IS NULL), x.issued_on DESC, x.id
		LIMIT ?`, args...).Scan(&ids).Error
	return ids, err
}

// anchorCount counts the KB's held catalogue documents.
func (s *legalGraphFocusService) anchorCount(ctx context.Context, kbID string) (int, error) {
	var n int64
	err := s.db.WithContext(ctx).Raw(`SELECT COUNT(DISTINCT h.legal_document_id) FROM (`+legalAnchorSQL+`) h`,
		kbID, types.LegalReviewApproved).Scan(&n).Error
	return int(n), err
}

// KBFocus returns the ego graph of docID seen from kbID. With docID empty
// the best-connected held document is focused, unless q is set: a picker
// search returns the candidates only (Focus nil). q filters the candidates.
func (s *legalGraphFocusService) KBFocus(ctx context.Context, kbID, docID, q string) (*types.LegalGraphFocus, error) {
	out := &types.LegalGraphFocus{
		Neighbours: []*types.LegalGraphNeighbour{}, NeighbourTotals: map[string]int{},
		ProvisionsIn: []*types.LegalGraphProvisionRow{}, ProvisionsOut: []*types.LegalGraphProvisionRow{},
		Documents: map[string]*types.LegalGraphDoc{}, Candidates: []*types.LegalGraphDoc{},
	}
	if s.db == nil {
		return out, nil
	}
	count, err := s.anchorCount(ctx, kbID)
	if err != nil {
		return nil, err
	}
	out.AnchorCount = count
	docID, q = strings.TrimSpace(docID), strings.TrimSpace(q)
	if count == 0 {
		if docID != "" {
			return nil, types.ErrLegalGraphFocusNotFound
		}
		return out, nil
	}
	if docID != "" {
		h, err := s.held(ctx, kbID, []string{docID})
		if err != nil {
			return nil, err
		}
		reach, err := s.reachable(ctx, kbID, []string{docID}, h)
		if err != nil {
			return nil, err
		}
		if !reach[docID] {
			return nil, types.ErrLegalGraphFocusNotFound
		}
	}

	// Picker candidates: the search hits, else the best connected.
	var candIDs []string
	degree := map[string]int{}
	if q != "" {
		if candIDs, err = s.searchHeld(ctx, kbID, q, legalFocusCandidates); err != nil {
			return nil, err
		}
	} else if candIDs, degree, err = s.topHeld(ctx, kbID, legalFocusCandidates); err != nil {
		return nil, err
	}
	if err := s.fillCandidates(ctx, kbID, candIDs, degree, out); err != nil {
		return nil, err
	}
	if docID == "" {
		if q != "" || len(candIDs) == 0 {
			// a picker search: the candidates only
			return out, nil
		}
		docID = candIDs[0]
	}

	// Neighbours from the catalogue relations.
	type pairKey struct{ id, dir string }
	rels := map[pairKey][]types.LegalRelation{}
	var order []pairKey
	add := func(k pairKey, r types.LegalRelation) {
		if _, ok := rels[k]; !ok {
			order = append(order, k)
		}
		for _, have := range rels[k] {
			if have == r {
				return
			}
		}
		rels[k] = append(rels[k], r)
	}
	for _, incoming := range []bool{false, true} {
		edges, err := s.repo.ListEdges(ctx, []string{docID}, legalGraphRelations, incoming)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if e.FromID == e.ToID {
				continue
			}
			if incoming {
				add(pairKey{e.FromID, types.LegalGraphIn}, e.Relation)
			} else {
				add(pairKey{e.ToID, types.LegalGraphOut}, e.Relation)
			}
		}
	}

	// Provision rows, both directions.
	in, inTotal, err := s.provisionRows(ctx, "target_doc_id", docID)
	if err != nil {
		return nil, err
	}
	outRows, outTotal, err := s.provisionRows(ctx, "source_doc_id", docID)
	if err != nil {
		return nil, err
	}
	out.ProvisionsIn, out.ProvisionsOut = in, outRows
	out.ProvisionsInTotal, out.ProvisionsOutTotal = inTotal, outTotal
	out.ProvisionsTruncated = int64(len(in)) < inTotal || int64(len(outRows)) < outTotal
	allRows := append(append([]*types.LegalGraphProvisionRow{}, in...), outRows...)
	derived := map[pairKey]bool{}
	for _, list := range []struct {
		rows []*types.LegalGraphProvisionRow
		dir  string
	}{{in, types.LegalGraphIn}, {outRows, types.LegalGraphOut}} {
		for _, r := range list.rows {
			if r.DocID == "" || r.DocID == docID {
				continue
			}
			k := pairKey{r.DocID, list.dir}
			if _, ok := rels[k]; ok && !derived[k] {
				continue
			}
			derived[k] = true
			add(k, legalOpRelation(r.Op))
		}
	}

	// Each pair's relations, strongest first; the group totals count every
	// pair before any cap.
	for _, k := range order {
		list := rels[k]
		sort.SliceStable(list, func(i, j int) bool { return legalRelationRank[list[i]] < legalRelationRank[list[j]] })
		out.NeighbourTotals[k.dir+":"+string(list[0])]++
	}
	sort.SliceStable(order, func(i, j int) bool {
		return legalRelationRank[rels[order[i]][0]] < legalRelationRank[rels[order[j]][0]]
	})

	// Load the focus, the provision rows' documents, then the neighbours
	// (strong relations first, so "căn cứ" is what the cap drops).
	ids := []string{docID}
	idSeen := map[string]bool{docID: true}
	push := func(id string) {
		if id != "" && !idSeen[id] && len(ids) < legalFocusMaxDocs {
			idSeen[id] = true
			ids = append(ids, id)
		}
	}
	for _, r := range allRows {
		push(r.DocID)
	}
	for _, k := range order {
		push(k.id)
	}
	docs, err := s.repo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	heldDocs, err := s.held(ctx, kbID, ids)
	if err != nil {
		return nil, err
	}
	reach, err := s.reachable(ctx, kbID, ids, heldDocs)
	if err != nil {
		return nil, err
	}
	byID := map[string]*types.LegalGraphDoc{}
	for _, d := range docs {
		byID[d.ID] = legalGraphDocOf(d, heldDocs, reach[d.ID], 0)
	}
	focus := byID[docID]
	if focus == nil {
		return nil, types.ErrLegalGraphFocusNotFound
	}
	out.Focus = focus

	groups := map[string][]*types.LegalGraphNeighbour{}
	var groupOrder []string
	for _, k := range order {
		d := byID[k.id]
		if d == nil {
			// beyond the load cap (a catalogue miss is not a truncation)
			out.NeighboursTruncated = out.NeighboursTruncated || !idSeen[k.id]
			continue
		}
		names := make([]string, len(rels[k]))
		for i, r := range rels[k] {
			names[i] = string(r)
		}
		g := k.dir + ":" + names[0]
		if _, ok := groups[g]; !ok {
			groupOrder = append(groupOrder, g)
		}
		groups[g] = append(groups[g], &types.LegalGraphNeighbour{Doc: d, Direction: k.dir, Relations: names, FromProvisions: derived[k]})
	}
	for _, g := range groupOrder {
		list := groups[g]
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Doc.InKB != list[j].Doc.InKB {
				return list[i].Doc.InKB
			}
			return list[i].Doc.IssuedOn > list[j].Doc.IssuedOn
		})
		if len(list) > legalFocusGroupCap {
			list, out.NeighboursTruncated = list[:legalFocusGroupCap], true
		}
		out.Neighbours = append(out.Neighbours, list...)
	}
	for _, r := range allRows {
		if d := byID[r.DocID]; d != nil {
			out.Documents[r.DocID] = d
		}
	}
	return out, nil
}

// fillCandidates loads the picker's documents in the order of ids.
func (s *legalGraphFocusService) fillCandidates(
	ctx context.Context, kbID string, ids []string, degree map[string]int, out *types.LegalGraphFocus,
) error {
	if len(ids) == 0 {
		return nil
	}
	docs, err := s.repo.GetByIDs(ctx, ids)
	if err != nil {
		return err
	}
	h, err := s.held(ctx, kbID, ids)
	if err != nil {
		return err
	}
	byID := map[string]*types.LegalDocument{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	for _, id := range ids {
		if d := byID[id]; d != nil {
			out.Candidates = append(out.Candidates, legalGraphDocOf(d, h, true, degree[id]))
		}
	}
	return nil
}

func legalGraphDocOf(d *types.LegalDocument, held map[string]legalHeld, focusable bool, degree int) *types.LegalGraphDoc {
	g := &types.LegalGraphDoc{
		ID: d.ID, DocumentNumber: d.DocumentNumber, DocType: d.DocType, Title: d.Title, Status: d.Status,
		Issuer: d.Issuer, IssuedOn: isoDate(d.IssuedOn), EffectiveOn: isoDate(d.EffectiveOn),
		Focusable: focusable, Degree: degree,
	}
	if h, ok := held[d.ID]; ok {
		g.InKB, g.KnowledgeID, g.KnowledgeTitle = true, h.knowledgeID, h.title
	}
	return g
}

// provisionRows reads the provision links whose col is docID.
func (s *legalGraphFocusService) provisionRows(
	ctx context.Context, col, docID string,
) ([]*types.LegalGraphProvisionRow, int64, error) {
	var total int64
	db := s.db.WithContext(ctx)
	if err := db.Model(&types.LegalProvisionLink{}).Where(col+" = ?", docID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var links []types.LegalProvisionLink
	if total > 0 {
		if err := db.Where(col+" = ?", docID).Order("id").Limit(legalFocusProvisionCap).Find(&links).Error; err != nil {
			return nil, 0, err
		}
	}
	rows := make([]*types.LegalGraphProvisionRow, 0, len(links))
	for _, lk := range links {
		other := lk.SourceDocID
		if col == "source_doc_id" {
			other = lk.TargetDocID
		}
		rows = append(rows, &types.LegalGraphProvisionRow{
			ID: lk.ID, TargetPath: lk.TargetPath, TargetLevel: lk.TargetLevel, Op: lk.Op,
			SourcePath: lk.SourcePath, DocID: other, EffFrom: isoDate(lk.EffFrom), EffTo: isoDate(lk.EffTo),
		})
	}
	return rows, total, nil
}

// legalOpRelation is the document relation a provision operation implies
// when the catalogue records none.
func legalOpRelation(op string) types.LegalRelation {
	switch op {
	case "bai_bo":
		return types.LegalRelationRepeals
	case "dinh_chinh":
		return types.LegalRelationCorrects
	case "giai_thich":
		return types.LegalRelationInterprets
	case "tam_ngung", "dinh_chi":
		return types.LegalRelationSuspends
	}
	return types.LegalRelationAmends
}

var legalFoldTransform = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// legalFoldText lowercases and drops diacritics ("Nghị Định" → "nghi dinh";
// đ, Đ and the look-alike Ð/ð → d).
func legalFoldText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("đ", "d", "ð", "d").Replace(s)
	if folded, _, err := transform.String(legalFoldTransform, s); err == nil {
		return folded
	}
	return s
}

// legalFoldPairs maps every accented Vietnamese letter, both cases, and
// đ/Đ/ð/Ð to its folded ASCII letter: legalFoldText as a table, for SQL.
var legalFoldPairs = func() [][2]string {
	var pairs [][2]string
	tones := []rune{0, 0x0300, 0x0301, 0x0309, 0x0303, 0x0323}
	for _, base := range "aăâeêioôơuưy" {
		for _, tone := range tones {
			s := string(base)
			if tone != 0 {
				s += string(tone)
			}
			lower := norm.NFC.String(s)
			if len(lower) == 1 { // plain ASCII vowel
				continue
			}
			folded := legalFoldText(lower)
			pairs = append(pairs, [2]string{lower, folded}, [2]string{strings.ToUpper(lower), folded})
		}
	}
	for _, d := range []string{"đ", "Đ", "ð", "Ð"} {
		pairs = append(pairs, [2]string{d, "d"})
	}
	return pairs
}()

// legalFoldClasses maps a folded letter to the regex class of the
// lowercase letters folding to it ("d" → "[dđð]").
var legalFoldClasses = func() map[rune]string {
	sets := map[rune][]string{}
	for _, p := range legalFoldPairs {
		if strings.ToLower(p[0]) != p[0] && p[0] != "ð" {
			continue // lower() has run on the text
		}
		r := []rune(p[1])[0]
		sets[r] = append(sets[r], p[0])
	}
	out := map[rune]string{}
	for r, forms := range sets {
		out[r] = "[" + string(r) + strings.Join(forms, "") + "]"
	}
	return out
}()

// legalFoldRegex is a Postgres regex matching a folded word in lowercased
// text written with or without diacritics.
func legalFoldRegex(word string) string {
	var b strings.Builder
	for _, r := range word {
		switch {
		case legalFoldClasses[r] != "":
			b.WriteString(legalFoldClasses[r])
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte('\\')
			b.WriteRune(r)
		}
	}
	return b.String()
}

// legalFoldSQLite is legalFoldText as an SQLite expression over expr:
// nested REPLACE of the accented letters, then lower (ASCII only there).
func legalFoldSQLite(expr string) string {
	out := expr
	for _, p := range legalFoldPairs {
		out = "REPLACE(" + out + ", '" + p[0] + "', '" + p[1] + "')"
	}
	return "lower(" + out + ")"
}

// legalLikeEscape escapes LIKE wildcards (ESCAPE '\').
func legalLikeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
