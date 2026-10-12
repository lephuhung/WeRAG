package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

const (
	// graphQueryMaxChunks / graphQueryMaxRelations bound what one KB
	// contributes: a node like a Luật touches hundreds of chunks.
	graphQueryMaxChunks    = 20
	graphQueryMaxRelations = 40
)

// WithGraph lets the tool read the graph store itself. Without it (Neo4j
// disabled) the tool falls back to text search and says so.
func (t *QueryKnowledgeGraphTool) WithGraph(
	repo interfaces.RetrieveGraphRepository, chunks interfaces.ChunkService, knowledge interfaces.KnowledgeService,
) *QueryKnowledgeGraphTool {
	t.graphRepo, t.chunkService, t.titleService = repo, chunks, knowledge
	return t
}

// graphQueryTerms splits the query into entity names ("A, B; C") and adds
// the canonical số hiệu of each one that cites a document.
func graphQueryTerms(query string) []string {
	return vietnamese_legal.GraphSearchTerms(strings.FieldsFunc(query, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	}))
}

// searchGraph returns the chunks behind the graph entities matching terms,
// and the relations among them, in one KB.
func (t *QueryKnowledgeGraphTool) searchGraph(
	ctx context.Context, kb *types.KnowledgeBase, terms []string,
) ([]*types.SearchResult, []*types.GraphRelation, error) {
	graph, err := t.graphRepo.SearchNode(ctx, types.NameSpace{KnowledgeBase: kb.ID}, terms)
	if err != nil || graph == nil {
		return nil, nil, err
	}
	var chunkIDs []string
	seen := map[string]bool{}
	for _, n := range graph.Node {
		for _, id := range n.Chunks {
			if !seen[id] && len(chunkIDs) < graphQueryMaxChunks {
				seen[id] = true
				chunkIDs = append(chunkIDs, id)
			}
		}
	}
	relations := graph.Relation
	if len(relations) > graphQueryMaxRelations {
		relations = relations[:graphQueryMaxRelations]
	}
	if len(chunkIDs) == 0 || t.chunkService == nil {
		return nil, relations, nil
	}
	// A shared or public KB stores its chunks under the owner's tenant.
	tenantID := t.searchTargets.GetTenantIDForKB(kb.ID)
	if tenantID == 0 {
		tenantID = kb.TenantID
	}
	chunks, err := t.chunkService.GetRepository().ListChunksByID(ctx, tenantID, chunkIDs)
	if err != nil {
		return nil, nil, err
	}
	titles := map[string]string{}
	if t.titleService != nil {
		var ids []string
		for _, c := range chunks {
			ids = append(ids, c.KnowledgeID)
		}
		if ks, err := t.titleService.GetKnowledgeBatch(ctx, tenantID, ids); err == nil {
			for _, k := range ks {
				titles[k.ID] = k.Title
			}
		}
	}
	results := make([]*types.SearchResult, 0, len(chunks))
	for _, c := range chunks {
		results = append(results, &types.SearchResult{
			ID: c.ID, Content: c.Content, KnowledgeID: c.KnowledgeID, KnowledgeBaseID: kb.ID,
			ChunkIndex: c.ChunkIndex, ChunkType: string(c.ChunkType), KnowledgeTitle: titles[c.KnowledgeID],
			Score: 1, MatchType: types.MatchTypeGraph,
		})
	}
	return results, relations, nil
}

// wholeKBInScope reports whether the agent may see the whole KB: graph
// relations carry no document ownership, so a scope narrowed to files or
// tags gets no relations, only its own chunks.
func (t *QueryKnowledgeGraphTool) wholeKBInScope(kbID string) bool {
	if !t.scopeEnforced {
		return true
	}
	for _, st := range t.searchTargets {
		if st != nil && st.KnowledgeBaseID == kbID && st.Type == types.SearchTargetTypeKnowledgeBase && len(st.TagIDs) == 0 {
			return true
		}
	}
	return false
}

// formatGraphRelations renders relations as "A —TYPE→ B" lines.
func formatGraphRelations(relations []*types.GraphRelation) string {
	var b strings.Builder
	for _, r := range relations {
		if r == nil || r.Node1 == "" || r.Node2 == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s —%s→ %s\n", r.Node1, r.Type, r.Node2)
	}
	return b.String()
}
