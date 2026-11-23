package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// The catalogue map (types.LegalGraphExplore): a document and the
// documents around it to a depth, walked over the whole legal catalogue.

const (
	// legalExploreMaxDepth bounds the steps walked on each side.
	legalExploreMaxDepth = 3
	// legalExploreDefaultDepth is the depth without a request.
	legalExploreDefaultDepth = 2
	// legalExploreFanCap bounds, per node and relation, the documents of
	// the next step (a law is "căn cứ" of hundreds: the rest is counted).
	legalExploreFanCap = 30
	// legalExploreBasisCap is the cap for "căn cứ", the widest fan.
	legalExploreBasisCap = 12
	// legalExploreMaxNodes bounds the whole map.
	legalExploreMaxNodes = 400
)

type legalGraphExploreService struct {
	repo    interfaces.LegalCatalogRepository
	reviews interfaces.LegalReviewRepository
	kbs     interfaces.KnowledgeBaseRepository
}

// NewLegalGraphExploreService creates the catalogue map service.
func NewLegalGraphExploreService(
	repo interfaces.LegalCatalogRepository, reviews interfaces.LegalReviewRepository, kbs interfaces.KnowledgeBaseRepository,
) interfaces.LegalGraphExploreService {
	return &legalGraphExploreService{repo: repo, reviews: reviews, kbs: kbs}
}

// exploreStep is one node reached by the walk.
type exploreStep struct {
	id    string
	depth int
}

// Explore walks depth steps out of docID on both sides and returns the
// induced subgraph (every drawn relation between two reached documents).
func (s *legalGraphExploreService) Explore(ctx context.Context, docID string, depth int) (*types.LegalGraphExplore, error) {
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return nil, types.ErrLegalGraphFocusNotFound
	}
	if depth <= 0 {
		depth = legalExploreDefaultDepth
	}
	if depth > legalExploreMaxDepth {
		depth = legalExploreMaxDepth
	}
	out := &types.LegalGraphExplore{Nodes: []*types.LegalGraphExploreNode{}, Edges: []*types.LegalGraphExploreEdge{}, Depth: depth}

	reached := map[string]*exploreStep{docID: {id: docID, depth: 0}}
	order := []string{docID}
	dropped := map[string]map[string]int{}
	add := func(id string, d int) bool {
		if _, ok := reached[id]; ok {
			return true
		}
		if len(reached) >= legalExploreMaxNodes {
			out.NodeCapHit = true
			return false
		}
		reached[id] = &exploreStep{id: id, depth: d}
		order = append(order, id)
		return true
	}

	// Each side walks on its own, so a document on the "in" side is never
	// expanded towards the "out" side (a law's decrees, then the decrees'
	// circulars; not the decrees' own căn cứ back to other laws).
	for _, incoming := range []bool{true, false} {
		frontier := []string{docID}
		for step := 1; step <= depth && len(frontier) > 0; step++ {
			edges, err := s.repo.ListEdges(ctx, frontier, legalGraphRelations, incoming)
			if err != nil {
				return nil, err
			}
			// group by the frontier node, then by relation; the strongest
			// relations first, so the cap drops "căn cứ" before "thay thế"
			byNode := map[string][]types.LegalDocumentRelation{}
			for _, e := range edges {
				if e.FromID == e.ToID {
					continue
				}
				near, far := e.FromID, e.ToID
				if incoming {
					near, far = e.ToID, e.FromID
				}
				if far == docID {
					continue
				}
				byNode[near] = append(byNode[near], e)
			}
			var next []string
			signed := step
			if !incoming {
				signed = -step
			}
			for _, near := range frontier {
				list := byNode[near]
				sort.SliceStable(list, func(i, j int) bool {
					ri, rj := legalRelationRank[list[i].Relation], legalRelationRank[list[j].Relation]
					if ri != rj {
						return ri < rj
					}
					return exploreFar(list[i], incoming) < exploreFar(list[j], incoming)
				})
				perRel := map[types.LegalRelation]int{}
				seenFar := map[string]bool{}
				for _, e := range list {
					far := exploreFar(e, incoming)
					if seenFar[far] {
						continue
					}
					seenFar[far] = true
					cap := legalExploreFanCap
					if e.Relation == types.LegalRelationBasedOn {
						cap = legalExploreBasisCap
					}
					if _, known := reached[far]; !known && perRel[e.Relation] >= cap {
						if dropped[near] == nil {
							dropped[near] = map[string]int{}
						}
						dropped[near][string(e.Relation)]++
						continue
					}
					perRel[e.Relation]++
					if add(far, signed) && reached[far].depth == signed {
						next = append(next, far)
					}
				}
			}
			frontier = next
		}
	}

	// The induced subgraph: every drawn relation between two reached nodes.
	ids := order
	edges, err := s.repo.ListEdges(ctx, ids, legalGraphRelations, false)
	if err != nil {
		return nil, err
	}
	seenEdge := map[string]bool{}
	for _, e := range edges {
		if _, ok := reached[e.ToID]; !ok || e.FromID == e.ToID {
			continue
		}
		k := e.FromID + "\x00" + e.ToID + "\x00" + string(e.Relation)
		if seenEdge[k] {
			continue
		}
		seenEdge[k] = true
		out.Edges = append(out.Edges, &types.LegalGraphExploreEdge{From: e.FromID, To: e.ToID, Relation: string(e.Relation)})
	}
	sort.SliceStable(out.Edges, func(i, j int) bool {
		if out.Edges[i].From != out.Edges[j].From {
			return out.Edges[i].From < out.Edges[j].From
		}
		if out.Edges[i].To != out.Edges[j].To {
			return out.Edges[i].To < out.Edges[j].To
		}
		return legalRelationRank[types.LegalRelation(out.Edges[i].Relation)] < legalRelationRank[types.LegalRelation(out.Edges[j].Relation)]
	})

	docs, err := s.repo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]*types.LegalDocument{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	if byID[docID] == nil {
		return nil, types.ErrLegalGraphFocusNotFound
	}
	copies := legalReadableCopies(ctx, s.repo, s.reviews, s.kbs, ids)
	for _, id := range order {
		d := byID[id]
		if d == nil {
			continue
		}
		g := legalGraphDocOf(d, nil, true, 0)
		if c := copies[id]; len(c) > 0 {
			g.InKB, g.KnowledgeID, g.KnowledgeTitle = true, c[0].KnowledgeID, c[0].Title
		}
		node := &types.LegalGraphExploreNode{Doc: g, Depth: reached[id].depth, Copies: copies[id], Dropped: dropped[id]}
		if id == docID {
			out.Focus = g
		}
		out.Nodes = append(out.Nodes, node)
	}
	return out, nil
}

// exploreFar is the end of e away from the frontier.
func exploreFar(e types.LegalDocumentRelation, incoming bool) string {
	if incoming {
		return e.FromID
	}
	return e.ToID
}
