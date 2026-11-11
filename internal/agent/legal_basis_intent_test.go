package agent

import (
	"context"
	"strings"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestFirstRoundToolChoiceLegalBasis(t *testing.T) {
	offered := []chat.Tool{
		{Type: "function", Function: chat.FunctionDef{Name: agenttools.ToolSearchKnowledge}},
		{Type: "function", Function: chat.FunctionDef{Name: agenttools.ToolCheckSpelling}},
		{Type: "function", Function: chat.FunctionDef{Name: agenttools.ToolCheckDocumentLegalBasis}},
	}
	ask := func(q string) context.Context { return types.WithUserQuery(context.Background(), q) }

	// the knowledge-lookup classifier asked for a search: the check wins
	ctx := types.WithFirstToolChoice(ask("Các văn bản viện dẫn còn hiệu lực không?"), agenttools.ToolSearchKnowledge)
	if got := firstRoundToolChoice(ctx, 0, offered); got != agenttools.ToolCheckDocumentLegalBasis {
		t.Fatalf("over the search: %q", got)
	}
	if got := firstRoundToolChoice(ask("Rà soát căn cứ pháp lý"), 0, offered); got != agenttools.ToolCheckDocumentLegalBasis {
		t.Fatalf("nothing asked: %q", got)
	}
	if got := firstRoundToolChoice(ctx, 1, offered); got != "" {
		t.Fatalf("later rounds choose freely: %q", got)
	}
	// a highlighted passage's spelling request keeps its tool
	ctx = types.WithFirstToolChoice(ask("kiểm tra chính tả và các văn bản viện dẫn còn hiệu lực không"), agenttools.ToolCheckSpelling)
	if got := firstRoundToolChoice(ctx, 0, offered); got != agenttools.ToolCheckSpelling {
		t.Fatalf("selection intent: %q", got)
	}
	// a question about one law stays a search
	ctx = types.WithFirstToolChoice(ask("Nghị định 30/2020/NĐ-CP còn hiệu lực không?"), agenttools.ToolSearchKnowledge)
	if got := firstRoundToolChoice(ctx, 0, offered); got != agenttools.ToolSearchKnowledge {
		t.Fatalf("one law: %q", got)
	}
	// not offered (no open document): nothing forced
	if got := firstRoundToolChoice(ask("Rà soát căn cứ pháp lý"), 0, offered[:2]); got != "" {
		t.Fatalf("not offered: %q", got)
	}
}

func TestLegalBasisReportIsEvidence(t *testing.T) {
	steps := []types.AgentStep{{ToolCalls: []types.ToolCall{{Name: agenttools.ToolCheckDocumentLegalBasis,
		Result: &types.ToolResult{Success: true, Output: "Đã rà soát căn cứ pháp lý của vb1: 3 văn bản được viện dẫn"}}}}}
	if !hasEvidence(steps) {
		t.Fatal("the catalogue's report must count as evidence")
	}
}

func TestAnswerGuardDropsUnderlineClaimAfterLegalBasis(t *testing.T) {
	ctx := context.Background()
	answer := "Có 2 văn bản hết hiệu lực. Các viện dẫn này đã được gạch chân trong văn bản. Bạn có thể bấm Gạch chân ở thẻ kết quả.\n"
	legal := types.ToolCall{Name: agenttools.ToolCheckDocumentLegalBasis, Result: &types.ToolResult{Success: true,
		Data: map[string]interface{}{agenttools.LegalBasisReportKey: &agenttools.LegalBasisReport{}}}}
	b := collectTurnButtons([]types.AgentStep{{ToolCalls: []types.ToolCall{legal}}})
	out, dropped := cleanAnswer(ctx, answer, b)
	if dropped != 1 || strings.Contains(out, "đã được gạch chân") || !strings.Contains(out, "bấm Gạch chân ở thẻ") {
		t.Fatalf("dropped=%d out=%q", dropped, out)
	}
	// a format check in the same turn did underline: the sentence stands
	marks := types.ToolCall{Name: agenttools.ToolCheckDocumentFormat, Result: &types.ToolResult{Success: true,
		Data: map[string]interface{}{"document_ops": []agenttools.DocumentOp{{Op: agenttools.OpMark}}}}}
	b = collectTurnButtons([]types.AgentStep{{ToolCalls: []types.ToolCall{legal, marks}}})
	if _, dropped := cleanAnswer(ctx, answer, b); dropped != 0 {
		t.Fatalf("with underlines sent, dropped %d", dropped)
	}
	// without the legal basis check the guard leaves such sentences alone
	if _, dropped := cleanAnswer(ctx, answer, turnButtons{}); dropped != 0 {
		t.Fatalf("other turns: dropped %d", dropped)
	}
}
