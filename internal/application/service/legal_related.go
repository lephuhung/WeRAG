package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// legalRelatedMaxRoots bounds how many matches of an ambiguous
	// reference are walked.
	legalRelatedMaxRoots = 3
	// legalRelatedPerLevel / legalRelatedMax bound a walk: a Luật can be
	// guided by hundreds of documents, more than an answer can use.
	legalRelatedPerLevel = 30
	legalRelatedMax      = 60
	// legalExpiredStatus is the portal status of a document no longer in
	// force at all.
	legalExpiredStatus = "Hết hiệu lực toàn bộ"
)

// legalWalkStep is one kind of hop: the relations followed and whether the
// walk goes against the edge (to documents acting on the current one).
type legalWalkStep struct {
	rels     []types.LegalRelation
	incoming bool
}

// legalRelatedWalks defines each mode and its depth ceiling.
var legalRelatedWalks = map[types.LegalRelatedMode]struct {
	steps    []legalWalkStep
	maxDepth int
}{
	// Luật ← Nghị định ← Thông tư: who details or guides it, transitively.
	types.LegalRelatedGuidance: {[]legalWalkStep{
		{[]types.LegalRelation{types.LegalRelationGuides, types.LegalRelationInterprets}, true},
	}, 3},
	// The amending documents, and what amended those.
	types.LegalRelatedAmendments: {[]legalWalkStep{
		{[]types.LegalRelation{types.LegalRelationAmends, types.LegalRelationCorrects, types.LegalRelationConsolidates}, true},
	}, 2},
	// Earlier and later versions along replacement and repeal.
	types.LegalRelatedHistory: {[]legalWalkStep{
		{legalSupersessionRelations, true},
		{legalSupersessionRelations, false},
	}, 3},
	// Up to the law it implements.
	types.LegalRelatedBasis: {[]legalWalkStep{
		{[]types.LegalRelation{types.LegalRelationBasedOn, types.LegalRelationGuides}, false},
	}, 2},
}

var legalSupersessionRelations = []types.LegalRelation{
	types.LegalRelationReplaces, types.LegalRelationRepeals,
	types.LegalRelationExpires, types.LegalRelationPartiallyExpires,
}

func (s *legalCatalogService) Related(
	ctx context.Context, documentNumber, title string, mode types.LegalRelatedMode,
	depth int, includeExpired bool,
) ([]*types.LegalRelatedReport, error) {
	walk, ok := legalRelatedWalks[mode]
	if !ok {
		return nil, fmt.Errorf("unknown relation mode %q", mode)
	}
	if depth <= 0 || depth > walk.maxDepth {
		depth = walk.maxDepth
	}
	// Old versions are the point of a history walk.
	keepExpired := includeExpired || mode == types.LegalRelatedHistory
	roots, err := s.find(ctx, documentNumber, title, legalRelatedMaxRoots)
	if err != nil {
		return nil, err
	}
	reports := make([]*types.LegalRelatedReport, 0, len(roots))
	for _, root := range roots {
		rep, err := s.walk(ctx, root, mode, walk.steps, depth, keepExpired)
		if err != nil {
			return nil, err
		}
		reports = append(reports, rep)
	}
	return reports, nil
}

// walk runs a breadth-first walk from root. Each level keeps at most
// legalRelatedPerLevel documents, in force first and newest first.
func (s *legalCatalogService) walk(
	ctx context.Context, root *types.LegalDocument, mode types.LegalRelatedMode,
	steps []legalWalkStep, depth int, keepExpired bool,
) (*types.LegalRelatedReport, error) {
	rep := &types.LegalRelatedReport{Document: root, Mode: mode}
	seen := map[string]bool{root.ID: true}
	frontier := []string{root.ID}
	for level := 1; level <= depth && len(frontier) > 0 && len(rep.Related) < legalRelatedMax; level++ {
		found := map[string]*types.LegalRelatedDocument{}
		for _, step := range steps {
			edges, err := s.repo.ListEdges(ctx, frontier, step.rels, step.incoming)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				parent, other := e.FromID, e.ToID
				if step.incoming {
					parent, other = e.ToID, e.FromID
				}
				if seen[other] || found[other] != nil {
					continue
				}
				found[other] = &types.LegalRelatedDocument{
					Depth: level, ParentID: parent, Relation: e.Relation, Incoming: step.incoming,
				}
			}
		}
		if len(found) == 0 {
			break
		}
		ids := make([]string, 0, len(found))
		for id := range found {
			ids = append(ids, id)
		}
		docs, err := s.repo.GetByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		kept := make([]*types.LegalRelatedDocument, 0, len(docs))
		for _, d := range docs {
			seen[d.ID] = true
			if !keepExpired && d.Status == legalExpiredStatus {
				continue
			}
			r := found[d.ID]
			r.Document = d
			kept = append(kept, r)
		}
		sort.SliceStable(kept, func(i, j int) bool {
			ei, ej := kept[i].Document.Status == legalExpiredStatus, kept[j].Document.Status == legalExpiredStatus
			if ei != ej {
				return !ei
			}
			return legalIssuedAfter(kept[i].Document, kept[j].Document)
		})
		room := min(legalRelatedPerLevel, legalRelatedMax-len(rep.Related))
		if len(kept) > room {
			kept, rep.Truncated = kept[:room], true
		}
		frontier = frontier[:0]
		for _, r := range kept {
			rep.Related = append(rep.Related, *r)
			frontier = append(frontier, r.Document.ID)
		}
	}
	if err := s.markKnowledge(ctx, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

// markKnowledge attaches the public-KB copies of every related document.
func (s *legalCatalogService) markKnowledge(ctx context.Context, rep *types.LegalRelatedReport) error {
	if s.reviews == nil || len(rep.Related) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rep.Related))
	for _, r := range rep.Related {
		ids = append(ids, r.Document.ID)
	}
	refs, err := s.reviews.KnowledgeForDocuments(ctx, ids)
	if err != nil {
		return err
	}
	for i := range rep.Related {
		rep.Related[i].Knowledge = refs[rep.Related[i].Document.ID]
	}
	return nil
}

func legalIssuedAfter(a, b *types.LegalDocument) bool {
	if a.IssuedOn == nil {
		return false
	}
	return b.IssuedOn == nil || a.IssuedOn.After(*b.IssuedOn)
}

const (
	// legalExpandMaxAnchors bounds how many search hits are expanded.
	legalExpandMaxAnchors = 3
	// legalExpandGuidanceDepth: Luật ← Nghị định ← Thông tư.
	legalExpandGuidanceDepth = 2
	// legalExpandSuccessorDepth: the replacing document, and its own.
	legalExpandSuccessorDepth = 2
)

// legalSuccessorSteps walks to the documents that replace or end a hit —
// the newer law a reader of the old one needs. Repeals are left out: a
// repealing document does not carry the replacement rules.
var legalSuccessorSteps = []legalWalkStep{{[]types.LegalRelation{
	types.LegalRelationReplaces, types.LegalRelationExpires, types.LegalRelationPartiallyExpires,
}, true}}

func (s *legalCatalogService) ExpandRelated(
	ctx context.Context, knowledgeIDs []string,
) ([]types.LegalExpansion, error) {
	if s.reviews == nil || len(knowledgeIDs) == 0 {
		return nil, nil
	}
	links, err := s.reviews.ApprovedLinksForKnowledge(ctx, knowledgeIDs)
	if err != nil {
		return nil, err
	}
	// Anchors in result order, one per catalogue document.
	var anchorIDs []string
	anchorKnowledge := map[string]string{}
	for _, kid := range knowledgeIDs {
		l := links[kid]
		if l == nil || l.LegalDocumentID == "" || anchorKnowledge[l.LegalDocumentID] != "" {
			continue
		}
		anchorKnowledge[l.LegalDocumentID] = kid
		anchorIDs = append(anchorIDs, l.LegalDocumentID)
		if len(anchorIDs) == legalExpandMaxAnchors {
			break
		}
	}
	if len(anchorIDs) == 0 {
		return nil, nil
	}
	docs, err := s.repo.GetByIDs(ctx, anchorIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*types.LegalDocument, len(docs))
	for _, d := range docs {
		byID[d.ID] = d
	}
	var out []types.LegalExpansion
	seen := map[string]bool{}
	for _, id := range anchorIDs {
		seen[id] = true
	}
	for _, id := range anchorIDs {
		anchor := byID[id]
		if anchor == nil {
			continue
		}
		// Successors first: when the hit is an old law, its replacement
		// matters more than the old law's guidance.
		for _, w := range []struct {
			mode  types.LegalRelatedMode
			steps []legalWalkStep
			depth int
		}{
			{types.LegalRelatedHistory, legalSuccessorSteps, legalExpandSuccessorDepth},
			{types.LegalRelatedGuidance, legalRelatedWalks[types.LegalRelatedGuidance].steps, legalExpandGuidanceDepth},
			{types.LegalRelatedAmendments, legalRelatedWalks[types.LegalRelatedAmendments].steps, 1},
		} {
			rep, err := s.walk(ctx, anchor, w.mode, w.steps, w.depth, true)
			if err != nil {
				return nil, err
			}
			for _, r := range rep.Related {
				if seen[r.Document.ID] {
					continue
				}
				seen[r.Document.ID] = true
				out = append(out, types.LegalExpansion{Anchor: anchor, AnchorKnowledgeID: anchorKnowledge[id], Related: r})
			}
		}
	}
	return out, nil
}
