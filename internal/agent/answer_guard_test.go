package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

const sampleAnswer = "Văn bản có 5 lỗi thể thức theo Nghị định 30/2020/NĐ-CP. Bạn có thể sửa thủ công.\n" +
	"- Thiếu Nơi nhận: bổ sung khối Nơi nhận.\n" +
	"Bạn vui lòng chọn phương án rồi bấm nút **\"Thay vào văn bản\"** để áp dụng. Xin cảm ơn.\n"

func TestCleanAnswerDropsPhantomButton(t *testing.T) {
	ctx := context.Background()
	out, dropped := cleanAnswer(ctx, sampleAnswer, turnButtons{})
	if dropped != 1 || strings.Contains(out, "Thay vào văn bản") {
		t.Fatalf("dropped=%d out=%q", dropped, out)
	}
	for _, keep := range []string{"5 lỗi thể thức theo Nghị định 30/2020/NĐ-CP.", "- Thiếu Nơi nhận: bổ sung khối Nơi nhận.\n", "Xin cảm ơn."} {
		if !strings.Contains(out, keep) {
			t.Fatalf("lost %q in %q", keep, out)
		}
	}
	out, dropped = cleanAnswer(ctx, sampleAnswer, turnButtons{proposal: true})
	if dropped != 0 || out != sampleAnswer {
		t.Fatalf("with a proposal nothing is dropped: dropped=%d", dropped)
	}
}

func TestAnswerGuardStreamsLikeCleanAnswer(t *testing.T) {
	ctx := types.WithAnswerButtonGuard(context.Background())
	ctx = withTurnButtons(ctx, nil)
	var got strings.Builder
	g := newAnswerGuard(ctx, func(s string) { got.WriteString(s) })
	if g == nil {
		t.Fatal("guard must be on")
	}
	// feed in awkward chunks, including one that splits the button phrase
	for _, chunk := range []string{"Văn bản có 5 lỗi thể thức theo Nghị định 30/2020/NĐ-CP. Bạn có thể sửa thủ công.\n- Thiếu Nơi nhận: ",
		"bổ sung khối Nơi nhận.\nBạn vui lòng chọn phương án rồi bấm nút **\"Thay vào", " văn bản\"** để áp dụng. Xin ", "cảm ơn.\n"} {
		g.Write(chunk)
	}
	g.Flush()
	want, _ := cleanAnswer(ctx, sampleAnswer, turnButtons{})
	if got.String() != want {
		t.Fatalf("stream:\n%q\nwhole:\n%q", got.String(), want)
	}
	if g.dropped != 1 {
		t.Fatalf("dropped %d", g.dropped)
	}
}

func TestAnswerGuardOffWithoutFlag(t *testing.T) {
	if newAnswerGuard(context.Background(), func(string) {}) != nil {
		t.Fatal("guard must be off without the flag")
	}
	if got := cleanAnswerInContext(context.Background(), sampleAnswer); got != sampleAnswer {
		t.Fatal("no filtering without the flag")
	}
}

func TestCollectTurnButtons(t *testing.T) {
	steps := []types.AgentStep{{ToolCalls: []types.ToolCall{
		{Name: "rewrite_paragraphs", Result: &types.ToolResult{Success: true, Data: map[string]interface{}{"proposal": true}}},
		{Name: "apply_format_fixes", Result: &types.ToolResult{Success: true, Data: map[string]interface{}{"review": true, "applied": []map[string]interface{}{{"check_id": "x"}}}}},
		{Name: "draw_diagram", Result: &types.ToolResult{Success: false, Data: map[string]interface{}{"illustration": map[string]interface{}{}}}},
	}}}
	b := collectTurnButtons(steps)
	if !b.proposal || !b.fixPlan || b.illustration {
		t.Fatalf("buttons %+v", b)
	}
}
