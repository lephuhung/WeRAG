package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type fakeGraphRepo struct {
	interfaces.RetrieveGraphRepository
	graph    *types.GraphData
	gotTerms []string
}

func (f *fakeGraphRepo) SearchNode(_ context.Context, _ types.NameSpace, nodes []string) (*types.GraphData, error) {
	f.gotTerms = nodes
	return f.graph, nil
}

type fakeGraphChunkRepo struct {
	interfaces.ChunkRepository
	gotTenant uint64
}

func (f *fakeGraphChunkRepo) ListChunksByID(_ context.Context, tenantID uint64, ids []string) ([]*types.Chunk, error) {
	f.gotTenant = tenantID
	out := make([]*types.Chunk, 0, len(ids))
	for _, id := range ids {
		out = append(out, &types.Chunk{ID: id, KnowledgeID: "k-" + id, Content: "nội dung " + id})
	}
	return out, nil
}

type fakeGraphChunkService struct {
	interfaces.ChunkService
	repo *fakeGraphChunkRepo
}

func (f *fakeGraphChunkService) GetRepository() interfaces.ChunkRepository { return f.repo }

func TestGraphQueryTerms(t *testing.T) {
	require.Equal(t, []string{"Nghị định 53/2022/nđ-cp", "53/2022/NĐ-CP", "Bộ Công an"},
		graphQueryTerms("Nghị định 53/2022/nđ-cp; Bộ Công an"))
}

func TestQueryKnowledgeGraphReadsTheGraphStore(t *testing.T) {
	kb := &stubKnowledgeBaseService{
		kb:      &types.KnowledgeBase{ID: "kb-1", TenantID: 7, ExtractConfig: &types.ExtractConfig{Enabled: true}},
		results: []*types.SearchResult{{ID: "text-hit", Content: "text search result"}},
	}
	graph := &fakeGraphRepo{graph: &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "13/2023/NĐ-CP", Chunks: []string{"c1"}},
			{Name: "53/2022/NĐ-CP", Chunks: []string{"c2", "c1"}},
		},
		Relation: []*types.GraphRelation{{Node1: "13/2023/NĐ-CP", Type: "BAI_BO", Node2: "53/2022/NĐ-CP"}},
	}}
	chunks := &fakeGraphChunkService{repo: &fakeGraphChunkRepo{}}
	whole := types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 10000}}
	tool := NewQueryKnowledgeGraphTool(kb, whole).WithGraph(graph, chunks, nil)

	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"knowledge_base_ids":["kb-1"],"query":"Nghị định 53/2022/NĐ-CP"}`))
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Contains(t, graph.gotTerms, "53/2022/NĐ-CP")
	require.EqualValues(t, 10000, chunks.repo.gotTenant, "chunks are read under the KB owner's tenant")
	rows := res.Data["results"].([]map[string]interface{})
	require.Len(t, rows, 2, "graph chunks replace text search")
	require.Equal(t, "c1", rows[0]["chunk_id"])
	require.Equal(t, "- 13/2023/NĐ-CP —BAI_BO→ 53/2022/NĐ-CP\n", res.Data["graph_relations_text"])
	require.Empty(t, res.Data["text_fallback_kbs"])
	edges := res.Data["graph_data"].(map[string]interface{})["edges"].([]map[string]interface{})
	require.Len(t, edges, 1)

	// A scope narrowed to one document keeps its chunks, never relations.
	narrowed := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-1",
		TenantID: 10000, KnowledgeIDs: []string{"k-c1"}}}
	tool = NewQueryKnowledgeGraphTool(kb, narrowed).WithGraph(graph, chunks, nil)
	require.False(t, tool.wholeKBInScope("kb-1"))

	// Nothing in the graph: text search, reported as such.
	empty := &fakeGraphRepo{graph: &types.GraphData{}}
	tool = NewQueryKnowledgeGraphTool(kb, whole).WithGraph(empty, chunks, nil)
	res, err = tool.Execute(context.Background(), json.RawMessage(`{"knowledge_base_ids":["kb-1"],"query":"không có"}`))
	require.NoError(t, err)
	require.Equal(t, []string{"kb-1"}, res.Data["text_fallback_kbs"])
	require.Equal(t, "text-hit", res.Data["results"].([]map[string]interface{})[0]["chunk_id"])
}

func TestQueryKnowledgeGraphRefusesKBWithoutExtraction(t *testing.T) {
	kb := &stubKnowledgeBaseService{kb: &types.KnowledgeBase{ID: "kb-1", ExtractConfig: &types.ExtractConfig{}}}
	res, err := NewQueryKnowledgeGraphTool(kb).Execute(context.Background(),
		json.RawMessage(`{"knowledge_base_ids":["kb-1"],"query":"x"}`))
	require.NoError(t, err)
	require.Contains(t, res.Data["errors"], "KB kb-1: graph extraction not enabled")
}
