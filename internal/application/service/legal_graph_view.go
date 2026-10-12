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

// legalGraphRelations are the edges drawn. based_on is drawn only between
// two documents of the KB: a Luật Tổ chức Chính phủ is the basis of almost
// everything and would swamp the picture.
var legalGraphRelations = []types.LegalRelation{
	types.LegalRelationReplaces, types.LegalRelationRepeals, types.LegalRelationExpires,
	types.LegalRelationPartiallyExpires, types.LegalRelationSuspends, types.LegalRelationAmends,
	types.LegalRelationGuides, types.LegalRelationInterprets, types.LegalRelationConsolidates,
	types.LegalRelationCorrects, types.LegalRelationBasedOn,
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

	var edges []types.LegalDocumentRelation
	for _, incoming := range []bool{false, true} {
		e, err := s.repo.ListEdges(ctx, anchors, legalGraphRelations, incoming)
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
		if e.Relation == types.LegalRelationBasedOn && !(isHeld(e.FromID) && isHeld(e.ToID)) {
			continue
		}
		kept = append(kept, e)
		for _, id := range []string{e.FromID, e.ToID} {
			if !isHeld(id) {
				touches[id]++
			}
		}
	}
	degree := map[string]int{}
	for _, e := range kept {
		degree[e.FromID]++
		degree[e.ToID]++
	}
	byDegree := func(list []string) {
		sort.SliceStable(list, func(i, j int) bool {
			if degree[list[i]] != degree[list[j]] {
				return degree[list[i]] > degree[list[j]]
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
