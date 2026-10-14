package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestClassifyKnowledgeLookup(t *testing.T) {
	ctx := context.Background()
	for reply, want := range map[string]bool{
		`{"lookup":true}`:                  true,
		"```json\n{\"lookup\": true}\n```": true,
		`{"lookup":false}`:                 false,
		`{"lookup":"yes"}`:                 false,
		`không phải JSON`:                  false,
	} {
		if got := ClassifyKnowledgeLookup(ctx, &fakeCompleter{reply: reply}, "xử lý như thế nào theo quy định"); got != want {
			t.Errorf("%q: got %v, want %v", reply, got, want)
		}
	}
	f := &fakeCompleter{reply: `{"lookup":true}`}
	ClassifyKnowledgeLookup(ctx, f, "Đơn vị bị phát hiện tài liệu BMNN nên xử lý thế nào")
	if len(f.got) != 2 || !strings.Contains(f.got[1].Content, "tài liệu BMNN") {
		t.Fatalf("messages: %+v", f.got)
	}
	if ClassifyKnowledgeLookup(ctx, &fakeCompleter{err: errors.New("down")}, "theo quy định") {
		t.Error("error must not force a lookup")
	}
	if ClassifyKnowledgeLookup(ctx, nil, "theo quy định") || ClassifyKnowledgeLookup(ctx, &fakeCompleter{reply: `{"lookup":true}`}, "  ") {
		t.Error("no model or empty query must not force a lookup")
	}
}

func TestClassifyChatKnowledgeLookup(t *testing.T) {
	ctx := context.Background()
	f := &fakeCompleter{reply: `{"lookup":true}`}
	if !ClassifyChatKnowledgeLookup(ctx, f, "Cơ quan nào có thẩm quyền đình chỉ chương trình viện trợ phi dự án?") {
		t.Fatal("want lookup")
	}
	if len(f.got) != 2 || strings.Contains(f.got[0].Content, "open Word document") {
		t.Fatalf("chat turns use the no-document prompt: %+v", f.got[0].Content)
	}
}
