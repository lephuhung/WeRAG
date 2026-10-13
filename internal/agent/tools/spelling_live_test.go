package tools

import (
	"context"
	"errors"
	"strconv"
	"strings"
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
	ops, found, missed := tool.LiveMarks(context.Background(), paras)
	if len(missed) != 0 || found != 3 {
		t.Fatalf("found %d missed %v: %+v", found, missed, ops)
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
	ops, found, missed = tool.LiveMarks(context.Background(), paras[1:2])
	if model.calls != calls || len(missed) != 0 || found != 1 || len(ops) != 1 || ops[0].Anchor.Occurrence != 2 {
		t.Fatalf("cached review: calls %d→%d, %+v", calls, model.calls, ops)
	}
}

func TestLiveMarksLeavesUnansweredParagraphsUnreviewed(t *testing.T) {
	spellPasses.reset()
	model := &spellChat{err: errors.New("model down")}
	tool := NewCheckSpellingTool(nil, model, "sess-1")
	paras := []LiveParagraph{
		{Text: "Sở Nội vụ đề nghi các đơn vị triễn khai kế hoạch.", Edited: true},
		{Text: "Chính p`hủ ban hành kế hoạch cải cách."},
	}
	ops, found, missed := tool.LiveMarks(context.Background(), paras)
	// the stray character alone must not pass the paragraphs off as reviewed,
	// nor may the edited one lose its old underlines
	if len(ops) != 0 || found != 0 || len(missed) != 2 {
		t.Fatalf("ops %+v found %d missed %v", ops, found, missed)
	}
	// nothing cached: a later answer is asked for
	model.err, model.replies = nil, []string{spellReply}
	if _, _, missed = tool.LiveMarks(context.Background(), paras); len(missed) != 0 || model.calls == 0 {
		t.Fatalf("second try: missed %v calls %d", missed, model.calls)
	}
}

func TestLiveMarksReviewsALongParagraphInParts(t *testing.T) {
	spellPasses.reset()
	sentence := "Các cơ quan chủ động triển khai nhiệm vụ được giao. "
	long := strings.Repeat(sentence, 2*livePartRunes/len([]rune(sentence))) + "Cuối cùng đề nghi báo cáo."
	parts := liveParts(long)
	if len(parts) < 2 {
		t.Fatalf("parts: %d", len(parts))
	}
	for _, p := range parts {
		if n := len([]rune(p)); n > livePartRunes || !strings.Contains(long, p) {
			t.Fatalf("part of %d runes", n)
		}
	}
	last := len(parts) - 1
	reply := `{"findings":[{"paragraph":` + strconv.Itoa(last) + `,"wrong":"đề nghi","correct":"đề nghị","reason":"thiếu dấu"}]}`
	model := &spellChat{replies: []string{reply}}
	tool := NewCheckSpellingTool(nil, model, "sess-1")
	ops, found, missed := tool.LiveMarks(context.Background(), []LiveParagraph{{Text: long}})
	if len(missed) != 0 || found != 1 || len(ops) != 1 || ops[0].Text != "đề nghi" || ops[0].Anchor.Text != long {
		t.Fatalf("ops %+v found %d missed %v", ops, found, missed)
	}
	if !strings.Contains(model.users[0], parts[last]) {
		t.Fatalf("the last part was not sent")
	}
}
