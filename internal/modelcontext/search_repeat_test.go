package modelcontext

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// A search_knowledge call answered by the repeat guard has no rows and no
// display_type: the model gets its Output as-is, never an empty <retrieval>.
func TestModelOutputRendersSearchRepeatAsText(t *testing.T) {
	registry := NewRegistry(true)
	text := "This query repeats the search_knowledge call of round 7 (mode=hybrid, 15 result(s)); " +
		"running it again would return the same chunks, so it was not run."
	out := registry.ModelToolResultForTool("search_knowledge", &types.ToolResult{
		Success: true,
		Output:  text,
		Data:    map[string]any{"repeat_of_round": 7, "query": "Nghị định 30/2020", "mode": "hybrid"},
	})
	if !strings.Contains(out, text) {
		t.Fatalf("repeat output lost: %q", out)
	}
	if strings.Contains(out, "<retrieval") {
		t.Fatalf("repeat rendered as retrieval: %q", out)
	}
}
