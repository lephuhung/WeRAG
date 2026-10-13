package agent

import (
	"reflect"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestToolResultChunkIDs(t *testing.T) {
	steps := []types.AgentStep{{ToolCalls: []types.ToolCall{
		{Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
			"results": []interface{}{map[string]interface{}{"chunk_id": "a"}, map[string]interface{}{"chunk_id": "b"}},
		}}},
		{Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
			"chunks": []map[string]interface{}{{"chunk_id": "b"}, {"chunk_id": "c"}},
		}}},
		{Result: &types.ToolResult{Success: false, Data: map[string]interface{}{
			"chunks": []map[string]interface{}{{"chunk_id": "x"}},
		}}},
		{Result: nil},
	}}}
	if got := toolResultChunkIDs(steps); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("ids = %v", got)
	}
}
