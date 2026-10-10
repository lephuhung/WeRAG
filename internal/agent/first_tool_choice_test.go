package agent

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestFirstRoundToolChoice(t *testing.T) {
	offered := []chat.Tool{
		{Type: "function", Function: chat.FunctionDef{Name: "rewrite_paragraphs"}},
		{Type: "function", Function: chat.FunctionDef{Name: "mark_passages"}},
	}
	ctx := types.WithFirstToolChoice(context.Background(), "rewrite_paragraphs")
	if got := firstRoundToolChoice(ctx, 0, offered); got != "rewrite_paragraphs" {
		t.Fatalf("first round: %q", got)
	}
	if got := firstRoundToolChoice(ctx, 1, offered); got != "" {
		t.Fatalf("later rounds choose freely: %q", got)
	}
	if got := firstRoundToolChoice(types.WithFirstToolChoice(context.Background(), "check_spelling"), 0, offered); got != "" {
		t.Fatalf("a tool not offered is not forced: %q", got)
	}
	if got := firstRoundToolChoice(context.Background(), 0, offered); got != "" {
		t.Fatalf("nothing asked: %q", got)
	}
}
