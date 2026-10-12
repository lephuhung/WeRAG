package agent

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// An agent turn's knowledge search must reach the client as references (the
// citation badges); before, only the quick-answer pipeline sent them.
func TestEmitToolOutcomeStreamsSearchKnowledgeReferences(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	var got []*types.SearchResult
	engine.eventBus.On(event.EventAgentReferences, func(_ context.Context, evt event.Event) error {
		got = evt.Data.(event.AgentReferencesData).References.([]*types.SearchResult)
		return nil
	})
	rows := []map[string]interface{}{
		{"chunk_id": "c1", "chunk_index": 3, "content": "Mạng LAN xử lý BMNN không kết nối Internet", "knowledge_id": "k1",
			"knowledge_base_id": "kb1", "knowledge_title": "Quy chế BMNN", "score": 0.81},
		{"faq_id": "f1", "content": "Hỏi đáp", "knowledge_title": "FAQ"},
		{"content": "no id, skipped"},
	}
	engine.emitToolOutcome(context.Background(), types.ToolCall{
		ID: "call-1", Name: tools.ToolSearchKnowledge,
		Result: &types.ToolResult{Success: true, Data: map[string]interface{}{"results": rows}},
	}, 0, "sess")

	require.Len(t, got, 2)
	require.Equal(t, &types.SearchResult{
		ID: "c1", ChunkIndex: 3, Content: "Mạng LAN xử lý BMNN không kết nối Internet", KnowledgeID: "k1",
		KnowledgeBaseID: "kb1", KnowledgeTitle: "Quy chế BMNN", Score: 0.81,
	}, got[0])
	require.Equal(t, "f1", got[1].ID)
}

func TestEmitToolOutcomeNoReferencesForOtherToolsOrFailures(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	fired := 0
	engine.eventBus.On(event.EventAgentReferences, func(context.Context, event.Event) error { fired++; return nil })
	data := map[string]interface{}{"results": []map[string]interface{}{{"chunk_id": "c1"}}}
	engine.emitToolOutcome(context.Background(), types.ToolCall{
		ID: "a", Name: tools.ToolReadDocument, Result: &types.ToolResult{Success: true, Data: data},
	}, 0, "sess")
	engine.emitToolOutcome(context.Background(), types.ToolCall{
		ID: "b", Name: tools.ToolSearchKnowledge, Result: &types.ToolResult{Success: false, Data: data},
	}, 0, "sess")
	engine.emitToolOutcome(context.Background(), types.ToolCall{
		ID: "c", Name: tools.ToolSearchKnowledge, Result: &types.ToolResult{Success: true, Data: map[string]interface{}{"results": []map[string]interface{}{}}},
	}, 0, "sess")
	require.Zero(t, fired)
}

func TestToolResultReferencesGraphAndWeb(t *testing.T) {
	graph := toolResultReferences(tools.ToolQueryKnowledgeGraph, map[string]interface{}{
		"results": []map[string]interface{}{{"chunk_id": "g1", "chunk_type": "text", "knowledge_title": "Luật BVBMNN"}},
	})
	require.Len(t, graph, 1)
	require.Equal(t, "g1", graph[0].ID)
	require.Equal(t, "Luật BVBMNN", graph[0].KnowledgeTitle)

	web := toolResultReferences(tools.ToolWebSearch, map[string]interface{}{
		"results": []map[string]interface{}{
			{"title": "Cổng TTĐT Chính phủ", "url": "https://chinhphu.vn/a", "snippet": "Tóm tắt", "source": "searxng"},
			{"title": "no url, skipped"},
		},
	})
	require.Len(t, web, 1)
	require.Equal(t, "https://chinhphu.vn/a", web[0].ID)
	require.Equal(t, string(types.ChunkTypeWebSearch), web[0].ChunkType)
	require.Equal(t, "https://chinhphu.vn/a", web[0].Metadata["url"])
	require.Equal(t, "Cổng TTĐT Chính phủ\n\nTóm tắt", web[0].Content)

	require.Nil(t, toolResultReferences(tools.ToolWikiSearch, map[string]interface{}{"results": []map[string]interface{}{{"chunk_id": "x"}}}))
}
