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
// the KB's documents with an approved legal link (anchors) and their
// direct neighbours; nothing further can be focused through the KB.

const (
	// legalFocusGroupCap bounds the neighbours kept per direction and
	// relation (newest first); the totals report the rest.
	legalFocusGroupCap = 60
	// legalFocusMaxNeighbours bounds the neighbour documents loaded.
	legalFocusMaxNeighbours = 1500
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

type legalGraphFocusService struct {
	repo    interfaces.LegalCatalogRepository
	reviews interfaces.LegalReviewRepository
	db      *gorm.DB
}

// NewLegalGraphFocusService creates the focused legal graph reader.
func NewLegalGraphFocusService(
	repo interfaces.LegalCatalogRepository, reviews interfaces.LegalReviewRepository, db *gorm.DB,
) interfaces.LegalGraphFocusService {
	return &legalGraphFocusService{repo: repo, reviews: reviews, db: db}
}

// legalKBScope is what a KB may show of the catalogue.
type legalKBScope struct {
	held    map[string]struct{ knowledgeID, title string }
	anchors []string
	// degree: document relations of each anchor, "căn cứ" not counted
	degree map[string]int
	// reach: anchors and their direct neighbours (document relations or
	// provision links)
	reach map[string]bool
}

func (s *legalGraphFocusService) scope(ctx context.Context, kbID string) (*legalKBScope, error) {
	sc := &legalKBScope{held: map[string]struct{ knowledgeID, title string }{}, degree: map[string]int{}, reach: map[string]bool{}}
	rows, _, err := s.reviews.ListLinks(ctx, types.LegalReviewApproved, kbID, 0, legalGraphMaxAnchors)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		id := row.Link.LegalDocumentID
		if id == "" {
			continue
		}
		if _, ok := sc.held[id]; !ok {
			sc.anchors = append(sc.anchors, id)
		}
		sc.held[id] = struct{ knowledgeID, title string }{row.Link.KnowledgeID, row.KnowledgeTitle}
		sc.reach[id] = true
	}
	if len(sc.anchors) == 0 {
		return sc, nil
	}
	seen := map[types.LegalDocumentRelation]bool{}
	for _, incoming := range []bool{false, true} {
		edges, err := s.repo.ListEdges(ctx, sc.anchors, legalGraphRelations, incoming)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if e.FromID == e.ToID || seen[e] {
				continue
			}
			seen[e] = true
			sc.reach[e.FromID], sc.reach[e.ToID] = true, true
			if e.Relation == types.LegalRelationBasedOn {
				continue
			}
			for _, id := range []string{e.FromID, e.ToID} {
				if _, ok := sc.held[id]; ok {
					sc.degree[id]++
				}
			}
		}
	}
	var pairs []struct{ TargetDocID, SourceDocID string }
	err = s.db.WithContext(ctx).Raw(`SELECT DISTINCT target_doc_id, source_doc_id FROM legal_provision_links
		WHERE source_doc_id <> '' AND (target_doc_id IN ? OR source_doc_id IN ?)`, sc.anchors, sc.anchors).Scan(&pairs).Error
	if err != nil {
		return nil, err
	}
	for _, p := range pairs {
		sc.reach[p.TargetDocID], sc.reach[p.SourceDocID] = true, true
	}
	return sc, nil
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
	if s.reviews == nil {
		return out, nil
	}
	sc, err := s.scope(ctx, kbID)
	if err != nil {
		return nil, err
	}
	out.AnchorCount = len(sc.anchors)
	if len(sc.anchors) == 0 {
		if docID != "" {
			return nil, types.ErrLegalGraphFocusNotFound
		}
		return out, nil
	}
	ranked := append([]string{}, sc.anchors...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if sc.degree[ranked[i]] != sc.degree[ranked[j]] {
			return sc.degree[ranked[i]] > sc.degree[ranked[j]]
		}
		return ranked[i] < ranked[j]
	})
	docID = strings.TrimSpace(docID)
	if docID != "" && !sc.reach[docID] {
		return nil, types.ErrLegalGraphFocusNotFound
	}
	if err := s.candidates(ctx, sc, ranked, q, out); err != nil {
		return nil, err
	}
	if docID == "" {
		if strings.TrimSpace(q) != "" {
			// a picker search: the candidates only
			return out, nil
		}
		docID = ranked[0]
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

	// Load the focus, the neighbours and the rows' documents.
	ids := []string{docID}
	idSeen := map[string]bool{docID: true}
	push := func(id string) {
		if id != "" && !idSeen[id] && len(ids) < legalFocusMaxNeighbours {
			idSeen[id] = true
			ids = append(ids, id)
		}
	}
	// Strong relations first so "căn cứ" is what the load cap drops.
	sort.SliceStable(order, func(i, j int) bool {
		return legalStrongest(rels[order[i]]) < legalStrongest(rels[order[j]])
	})
	for _, k := range order {
		push(k.id)
	}
	for _, r := range append(append([]*types.LegalGraphProvisionRow{}, in...), outRows...) {
		push(r.DocID)
	}
	docs, err := s.repo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]*types.LegalGraphDoc{}
	for _, d := range docs {
		byID[d.ID] = s.graphDoc(d, sc)
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
			if len(ids) >= legalFocusMaxNeighbours {
				out.NeighboursTruncated = true
			}
			continue
		}
		list := rels[k]
		sort.SliceStable(list, func(i, j int) bool { return legalRelationRank[list[i]] < legalRelationRank[list[j]] })
		names := make([]string, len(list))
		for i, r := range list {
			names[i] = string(r)
		}
		n := &types.LegalGraphNeighbour{Doc: d, Direction: k.dir, Relations: names, FromProvisions: derived[k]}
		g := k.dir + ":" + names[0]
		if _, ok := groups[g]; !ok {
			groupOrder = append(groupOrder, g)
		}
		groups[g] = append(groups[g], n)
	}
	for _, g := range groupOrder {
		list := groups[g]
		out.NeighbourTotals[g] = len(list)
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
	for _, r := range append(append([]*types.LegalGraphProvisionRow{}, in...), outRows...) {
		if d := byID[r.DocID]; d != nil {
			out.Documents[r.DocID] = d
		}
	}
	return out, nil
}

// candidates fills the picker: held documents matching q, else the best
// connected.
func (s *legalGraphFocusService) candidates(
	ctx context.Context, sc *legalKBScope, ranked []string, q string, out *types.LegalGraphFocus,
) error {
	q = legalFoldText(q)
	if q == "" {
		ranked = ranked[:min(len(ranked), legalFocusCandidates)]
	}
	docs, err := s.repo.GetByIDs(ctx, ranked)
	if err != nil {
		return err
	}
	byID := map[string]*types.LegalDocument{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	words := strings.Fields(q)
	for _, id := range ranked {
		d := byID[id]
		if d == nil {
			continue
		}
		if len(words) > 0 {
			hay := legalFoldText(d.DocType + " " + d.DocumentNumber + " " + d.Title + " " + sc.held[id].title)
			ok := true
			for _, w := range words {
				if !strings.Contains(hay, w) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		out.Candidates = append(out.Candidates, s.graphDoc(d, sc))
		if len(out.Candidates) >= legalFocusCandidates {
			break
		}
	}
	return nil
}

func (s *legalGraphFocusService) graphDoc(d *types.LegalDocument, sc *legalKBScope) *types.LegalGraphDoc {
	g := &types.LegalGraphDoc{
		ID: d.ID, DocumentNumber: d.DocumentNumber, DocType: d.DocType, Title: d.Title, Status: d.Status,
		Issuer: d.Issuer, IssuedOn: isoDate(d.IssuedOn), EffectiveOn: isoDate(d.EffectiveOn),
		Focusable: sc.reach[d.ID], Degree: sc.degree[d.ID],
	}
	if h, ok := sc.held[d.ID]; ok {
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

func legalStrongest(list []types.LegalRelation) int {
	best := len(legalRelationRank)
	for _, r := range list {
		if v, ok := legalRelationRank[r]; ok && v < best {
			best = v
		}
	}
	return best
}

// legalFoldText lowercases and drops diacritics ("Nghị định" → "nghi dinh").
func legalFoldText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("đ", "d", "ð", "d").Replace(s)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	if folded, _, err := transform.String(t, s); err == nil {
		return folded
	}
	return s
}
