package agent

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestIsFormatCheckRequest(t *testing.T) {
	yes := []string{
		"Kiểm tra thể thức văn bản này theo Nghị định 30/2020/NĐ-CP",
		"kiểm tra thể thức văn bản này",
		"Văn bản này đúng thể thức chưa?",
		"Soát lỗi căn lề, cỡ chữ của văn bản",
		"Rà soát trình bày theo NĐ 30 giúp tôi",
		"Văn bản có đúng Nghị định 30 không",
	}
	no := []string{
		"Nghị định 30/2020/NĐ-CP quy định gì về thể thức?",
		"Thể thức văn bản theo Nghị định 30 gồm những thành phần nào",
		"Tóm tắt văn bản này",
		"Kiểm tra chính tả văn bản này",
		"Điều 8 Nghị định 30 quy định thế nào",
		"",
	}
	for _, q := range yes {
		if !isFormatCheckRequest(q) {
			t.Errorf("should be a format check: %q", q)
		}
	}
	for _, q := range no {
		if isFormatCheckRequest(q) {
			t.Errorf("should not be a format check: %q", q)
		}
	}
}

func TestFirstRoundToolChoicePrefersFormatCheck(t *testing.T) {
	tools := []chat.Tool{
		{Function: chat.FunctionDef{Name: "search_knowledge"}},
		{Function: chat.FunctionDef{Name: toolCheckDocumentFormat}},
	}
	ctx := types.WithUserQuery(context.Background(), "Kiểm tra thể thức văn bản này theo Nghị định 30/2020/NĐ-CP")
	ctx = types.WithFirstToolChoice(ctx, "search_knowledge")
	if got := firstRoundToolChoice(ctx, 0, tools); got != toolCheckDocumentFormat {
		t.Fatalf("iteration 0: got %q", got)
	}
	if got := firstRoundToolChoice(ctx, 1, tools); got != "" {
		t.Fatalf("iteration 1: got %q", got)
	}
	// the tool is not offered: the earlier choice stands
	if got := firstRoundToolChoice(ctx, 0, tools[:1]); got != "search_knowledge" {
		t.Fatalf("without the tool: got %q", got)
	}
	// a question about the decree keeps the knowledge lookup
	ctx2 := types.WithFirstToolChoice(types.WithUserQuery(context.Background(), "Nghị định 30 quy định gì về thể thức?"), "search_knowledge")
	if got := firstRoundToolChoice(ctx2, 0, tools); got != "search_knowledge" {
		t.Fatalf("decree question: got %q", got)
	}
	// the grounding gate does not hold a format check
	gctx := types.WithGroundingRequired(ctx)
	if isUngrounded(markUngrounded(gctx, nil)) {
		t.Fatal("a format check must not be held to the grounding gate")
	}
	if !isUngrounded(markUngrounded(types.WithGroundingRequired(ctx2), nil)) {
		t.Fatal("a decree question stays under the grounding gate")
	}
}
