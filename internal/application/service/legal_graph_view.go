package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	legalGraphDefaultLimit = 150
	legalGraphMaxLimit     = 400
	// legalGraphMaxAnchors bounds the KB documents read per graph.
	legalGraphMaxAnchors = 500
)

// legalGraphRelations are the edges the focus, explore and related views
// walk (based_on included, references not). The KB graph reads its own
// lists (legalKBGraphWeak, legalKBGraphStrong).
var legalGraphRelations = []types.LegalRelation{
	types.LegalRelationReplaces, types.LegalRelationRepeals, types.LegalRelationExpires,
	types.LegalRelationPartiallyExpires, types.LegalRelationSuspends, types.LegalRelationAmends,
	types.LegalRelationGuides, types.LegalRelationInterprets, types.LegalRelationConsolidates,
	types.LegalRelationCorrects, types.LegalRelationBasedOn, types.LegalRelationSucceeds,
}

// The KB graph draws a KB document's căn cứ and dẫn chiếu (the weak
// relations) also when the other end is outside the KB: most of a Party
// document's edges point to documents the KB does not hold. Only the ones
// a KB document states are read (outgoing): the reverse, every document
// citing a held Luật Tổ chức Chính phủ as its basis, runs into thousands
// and would swamp the picture; an edge between two KB documents comes in
// through the citing one. Strong relations are read both ways.
var (
	legalKBGraphWeak   = []types.LegalRelation{types.LegalRelationBasedOn, types.LegalRelationReferences}
	legalKBGraphStrong = legalGraphStrongRelations()
)

// legalKBGraphStrongWeight: in ranking which documents get the node budget,
// a strong edge (replaces, amends, guides …) counts this many weak ones, so
// a KB document is not ranked high for citing many generic căn cứ.
const legalKBGraphStrongWeight = 4

func legalGraphStrongRelations() []types.LegalRelation {
	out := make([]types.LegalRelation, 0, len(legalGraphRelations))
	for _, r := range legalGraphRelations {
		if r != types.LegalRelationBasedOn && r != types.LegalRelationReferences {
			out = append(out, r)
		}
	}
	return out
}

func legalKBGraphWeakRelation(r types.LegalRelation) bool {
	return r == types.LegalRelationBasedOn || r == types.LegalRelationReferences
}

func (s *legalCatalogService) KBGraph(ctx context.Context, kbID string, limit int) (*types.GraphView, error) {
	view := &types.GraphView{Nodes: []*types.GraphViewNode{}, Edges: []*types.GraphViewEdge{}}
	if s.reviews == nil {
		return view, nil
	}
	if limit <= 0 {
		limit = legalGraphDefaultLimit
	}
	limit = min(limit, legalGraphMaxLimit)
	rows, _, err := s.reviews.ListLinks(ctx, types.LegalReviewApproved, kbID, 0, legalGraphMaxAnchors)
	if err != nil {
		return nil, err
	}
	held := map[string]struct{ knowledgeID, title string }{}
	var anchors []string
	for _, row := range rows {
		id := row.Link.LegalDocumentID
		if id == "" {
			continue
		}
		if _, ok := held[id]; !ok {
			anchors = append(anchors, id)
		}
		held[id] = struct{ knowledgeID, title string }{row.Link.KnowledgeID, row.KnowledgeTitle}
	}
	if len(anchors) == 0 {
		return view, nil
	}

	// outgoing: strong and weak (bounded by what ≤ legalGraphMaxAnchors
	// documents cite); incoming: strong only (see legalKBGraphWeak)
	var edges []types.LegalDocumentRelation
	for _, incoming := range []bool{false, true} {
		rels := legalKBGraphStrong
		if !incoming {
			rels = append(append([]types.LegalRelation{}, legalKBGraphStrong...), legalKBGraphWeak...)
		}
		e, err := s.repo.ListEdges(ctx, anchors, rels, incoming)
		if err != nil {
			return nil, err
		}
		edges = append(edges, e...)
	}
	isHeld := func(id string) bool { _, ok := held[id]; return ok }
	// Documents outside the KB reached by a drawn edge.
	touches := map[string]int{}
	kept := edges[:0]
	seenEdge := map[types.LegalDocumentRelation]bool{}
	for _, e := range edges {
		key := types.LegalDocumentRelation{FromID: e.FromID, ToID: e.ToID, Relation: e.Relation}
		if seenEdge[key] || e.FromID == e.ToID {
			continue
		}
		seenEdge[key] = true
		kept = append(kept, e)
		for _, id := range []string{e.FromID, e.ToID} {
			if !isHeld(id) {
				touches[id]++
			}
		}
	}
	// degree is the nodes' edge count; rank orders the budget, a strong
	// edge counting legalKBGraphStrongWeight weak ones
	degree, rank := map[string]int{}, map[string]int{}
	for _, e := range kept {
		w := legalKBGraphStrongWeight
		if legalKBGraphWeakRelation(e.Relation) {
			w = 1
		}
		for _, id := range []string{e.FromID, e.ToID} {
			degree[id]++
			rank[id] += w
		}
	}
	byDegree := func(list []string) {
		sort.SliceStable(list, func(i, j int) bool {
			if rank[list[i]] != rank[list[j]] {
				return rank[list[i]] > rank[list[j]]
			}
			return list[i] < list[j]
		})
	}
	// KB documents first, best-connected first (isolated ones last); a
	// third of the budget stays for neighbours when there are some.
	byDegree(anchors)
	neighbours := make([]string, 0, len(touches))
	for id := range touches {
		neighbours = append(neighbours, id)
	}
	byDegree(neighbours)
	anchorCap := limit
	if len(neighbours) > 0 {
		anchorCap = max(limit-len(neighbours), limit*2/3)
	}
	ids := anchors
	if len(ids) > anchorCap {
		ids, view.Truncated = ids[:anchorCap], true
	}
	ids = append([]string{}, ids...)
	if room := limit - len(ids); len(neighbours) > room {
		neighbours, view.Truncated = neighbours[:max(room, 0)], true
	}
	ids = append(ids, neighbours...)

	docs, err := s.repo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, d := range docs {
		present[d.ID] = true
		meta := map[string]string{
			"document_number": d.DocumentNumber, "title": d.Title, "status": d.Status,
			"issuer": d.Issuer, "issued_on": isoDate(d.IssuedOn),
		}
		if h, ok := held[d.ID]; ok {
			meta["in_kb"] = "true"
			meta["knowledge_id"] = h.knowledgeID
			meta["knowledge_title"] = h.title
		}
		view.Nodes = append(view.Nodes, &types.GraphViewNode{
			ID: d.ID, Name: legalGraphNodeName(d), Type: d.DocType, Degree: degree[d.ID], Meta: meta,
		})
	}
	for _, e := range kept {
		if present[e.FromID] && present[e.ToID] {
			view.Edges = append(view.Edges, &types.GraphViewEdge{Source: e.FromID, Target: e.ToID, Type: string(e.Relation)})
		}
	}
	return view, nil
}

func isoDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// legalGraphNodeName is the short node label: "Luật 109/2025/QH15".
func legalGraphNodeName(d *types.LegalDocument) string {
	if num := strings.TrimSpace(d.DocumentNumber); num != "" {
		return strings.TrimSpace(d.DocType + " " + num)
	}
	return d.Title
}
