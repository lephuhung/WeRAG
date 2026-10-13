package agent

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestUsedWikiPages(t *testing.T) {
	ok := &types.ToolResult{Success: true}
	failed := &types.ToolResult{Success: false}
	cases := []struct {
		name  string
		steps []types.AgentStep
		want  bool
	}{
		{"no tools", nil, false},
		{"search_knowledge only", []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: "search_knowledge", Result: ok}}}}, false},
		{"wiki_search", []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: "wiki_search", Result: ok}}}}, true},
		{"wiki_read_page in later step", []types.AgentStep{{}, {ToolCalls: []types.ToolCall{{Name: "wiki_read_page", Result: ok}}}}, true},
		{"failed wiki call", []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: "wiki_search", Result: failed}}}}, false},
		{"no result", []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: "wiki_search"}}}}, false},
		{"wiki write tool", []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: "wiki_write_page", Result: ok}}}}, false},
	}
	for _, c := range cases {
		if got := usedWikiPages(c.steps); got != c.want {
			t.Errorf("%s: usedWikiPages = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWikiCaveatBlock(t *testing.T) {
	if b := wikiCaveatBlock("vi"); !strings.Contains(b, "đối chiếu với văn bản gốc") {
		t.Errorf("vi block = %q", b)
	}
	if b := wikiCaveatBlock("vi-VN"); !strings.Contains(b, "Lưu ý") {
		t.Errorf("vi-VN block = %q", b)
	}
	if b := wikiCaveatBlock("en-US"); !strings.Contains(b, "verify against the source document") {
		t.Errorf("en block = %q", b)
	}
	if b := wikiCaveatBlock(""); !strings.Contains(b, "Note:") {
		t.Errorf("default block = %q", b)
	}
}
