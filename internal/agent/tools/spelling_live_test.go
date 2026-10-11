package tools

import (
	"context"
	"testing"
)

func TestLiveMarksUnmarksEditedParagraphsAndCaches(t *testing.T) {
	spellPasses.reset()
	model := &spellChat{replies: []string{spellReply}}
	tool := NewCheckSpellingTool(nil, model, "sess-1")
	paras := []LiveParagraph{
		{Text: "Sở Nội vụ đề nghi các đơn vị triễn khai kế hoạch.", Edited: true},
		{Text: "Chính p`hủ ban hành kế hoạch cải cách.", Occurrence: 2},
		{Text: "Báo cáo gửi về trước ngày 30 tháng 11."},
		{Text: "   "},
	}
	ops, found, ok := tool.LiveMarks(context.Background(), paras)
	if !ok || found != 3 {
		t.Fatalf("found %d ok %v: %+v", found, ok, ops)
	}
	if len(ops) != 4 || ops[0].Op != OpUnmark || ops[0].Anchor.Text != paras[0].Text {
		t.Fatalf("the edited paragraph is unmarked first: %+v", ops)
	}
	if ops[1].Op != OpMark || ops[1].Text != "đề nghi" || ops[2].Text != "triễn khai" {
		t.Fatalf("marks of paragraph 0: %+v", ops[1:3])
	}
	if ops[3].Text != "p`hủ" || ops[3].Anchor.Occurrence != 2 || ops[3].Style != "underline" {
		t.Fatalf("mark of paragraph 1: %+v", ops[3])
	}
	// the same text again: from the cache, no model call
	calls := model.calls
	ops, found, ok = tool.LiveMarks(context.Background(), paras[1:2])
	if model.calls != calls || !ok || found != 1 || len(ops) != 1 || ops[0].Anchor.Occurrence != 2 {
		t.Fatalf("cached review: calls %d→%d, %+v", calls, model.calls, ops)
	}
}
