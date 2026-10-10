package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
)

type fakeCompleter struct {
	reply string
	err   error
	got   []docformat.Message
}

func (f *fakeCompleter) Complete(_ context.Context, m []docformat.Message) (string, error) {
	f.got = m
	return f.reply, f.err
}

func TestClassifySelectionIntent(t *testing.T) {
	ctx := context.Background()
	for reply, want := range map[string]string{
		`{"intent":"rewrite"}`:                     SelectionIntentRewrite,
		"```json\n{\"intent\": \"Spelling\"}\n```": SelectionIntentSpelling,
		`{"intent":"other"}`:                       SelectionIntentOther,
		`{"intent":"translate"}`:                   SelectionIntentOther,
		`không phải JSON`:                          SelectionIntentOther,
	} {
		if got := ClassifySelectionIntent(ctx, &fakeCompleter{reply: reply}, "Viết lại cho tôi nhé", "đoạn"); got != want {
			t.Errorf("%q: got %q, want %q", reply, got, want)
		}
	}
	f := &fakeCompleter{reply: `{"intent":"rewrite"}`}
	ClassifySelectionIntent(ctx, f, "gợi ý viết lại đoạn này", "Đoàn kiểm tra đã thực hiện")
	if len(f.got) != 2 || !strings.Contains(f.got[1].Content, "gợi ý viết lại") || !strings.Contains(f.got[1].Content, "Đoàn kiểm tra") {
		t.Fatalf("messages: %+v", f.got)
	}
	if ClassifySelectionIntent(ctx, &fakeCompleter{err: errors.New("down")}, "viết lại", "x") != SelectionIntentOther ||
		ClassifySelectionIntent(ctx, nil, "viết lại", "x") != SelectionIntentOther {
		t.Fatal("no answer: the turn runs as before")
	}
	if FirstToolForSelection(SelectionIntentRewrite) != ToolRewriteParagraphs ||
		FirstToolForSelection(SelectionIntentSpelling) != ToolCheckSpelling || FirstToolForSelection(SelectionIntentOther) != "" {
		t.Fatal("tool mapping")
	}
}
