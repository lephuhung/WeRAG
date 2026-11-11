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
	// an @-mentioned document counts as "văn bản này"
	ctx = types.WithMentionedDocuments(ask("@Tờ trình.docx còn hiệu lực không"), []string{"ws-2"})
	if got := firstRoundToolChoice(ctx, 0, offered); got != agenttools.ToolCheckDocumentLegalBasis {
		t.Fatalf("mentioned document: %q", got)
	}
	// not offered (no open document): nothing forced
	if got := firstRoundToolChoice(ask("Rà soát căn cứ pháp lý"), 0, offered[:2]); got != "" {
		t.Fatalf("not offered: %q", got)
	}
}

func legalBasisStep(success bool, errText string) types.AgentStep {
	r := &types.ToolResult{Success: success, Error: errText}
	if success {
		r.Output = "Đã rà soát căn cứ pháp lý của vb1: 3 văn bản được viện dẫn"
	}
	return types.AgentStep{ToolCalls: []types.ToolCall{{Name: agenttools.ToolCheckDocumentLegalBasis, Result: r}}}
}

// A pure check is answered by its report, like a format check; a request
// asking more keeps the grounding gate until a search returns evidence.
func TestGroundingGateLegalBasis(t *testing.T) {
	gate := func(q string) context.Context {
		return types.WithGroundingRequired(types.WithUserQuery(context.Background(), q))
	}
	ok := []types.AgentStep{legalBasisStep(true, "")}
	if isUngrounded(markUngrounded(gate("Các văn bản viện dẫn còn hiệu lực không?"), ok)) {
		t.Fatal("pure check with its report: still gated")
	}
	if !isUngrounded(markUngrounded(gate("Các văn bản viện dẫn còn hiệu lực không?"), nil)) {
		t.Fatal("before the check runs the gate holds")
	}
	mixed := gate("rà soát căn cứ pháp lý của văn bản này và cho biết mức phạt nồng độ cồn")
	if !isUngrounded(markUngrounded(mixed, ok)) {
		t.Fatal("mixed request: the report alone must not open the gate")
	}
	if !isUngrounded(markUngrounded(gate("mức phạt nồng độ cồn là bao nhiêu"), ok)) {
		t.Fatal("the report is no evidence for another question")
	}
	if hasEvidence(ok) {
		t.Fatal("the report is not generic evidence")
	}
}

// A failed pure check ends the turn with why, written in code.
func TestLegalBasisFailureMessage(t *testing.T) {
	ask := func(q string) context.Context { return types.WithUserQuery(context.Background(), q) }
	pure := ask("Rà soát căn cứ pháp lý")
	failed := legalBasisStep(false, "Không rà soát được căn cứ pháp lý của vb1: danh mục văn bản pháp luật tạm thời không khả dụng.")
	if got := legalBasisFailureMessage(pure, failed); got != "Không rà soát được căn cứ pháp lý của vb1: danh mục văn bản pháp luật tạm thời không khả dụng." {
		t.Fatalf("service failure: %q", got)
	}
	noDoc := legalBasisStep(false, "Cuộc hội thoại này chưa mở tài liệu nào trong trình soạn thảo.\n\n[Công cụ … ĐỪNG gọi lại]")
	if got := legalBasisFailureMessage(pure, noDoc); got != "Không rà soát được căn cứ pháp lý: Cuộc hội thoại này chưa mở tài liệu nào trong trình soạn thảo." {
		t.Fatalf("no document: %q", got)
	}
	if got := legalBasisFailureMessage(pure, legalBasisStep(true, "")); got != "" {
		t.Fatalf("success: %q", got)
	}
	if got := legalBasisFailureMessage(ask("rà soát căn cứ pháp lý của văn bản này và cho biết mức phạt nồng độ cồn"), failed); got != "" {
		t.Fatalf("mixed request keeps going (the search part): %q", got)
	}
	both := types.AgentStep{ToolCalls: append(legalBasisStep(true, "").ToolCalls, failed.ToolCalls...)}
	if got := legalBasisFailureMessage(pure, both); got != "" {
		t.Fatalf("one check succeeded: %q", got)
	}
}

func TestAnswerGuardDropsUnderlineClaimAfterLegalBasis(t *testing.T) {
	ctx := context.Background()
	answer := "Có 2 văn bản hết hiệu lực. Các viện dẫn này đã được gạch chân trong văn bản. Bạn có thể bấm Gạch chân ở thẻ kết quả.\n"
	legal := types.ToolCall{Name: agenttools.ToolCheckDocumentLegalBasis, Result: &types.ToolResult{Success: true,
		Data: map[string]interface{}{agenttools.LegalBasisReportKey: &agenttools.LegalBasisReport{}}}}
	b := collectTurnButtons([]types.AgentStep{{ToolCalls: []types.ToolCall{legal}}})
	out, dropped := cleanAnswer(ctx, answer, b)
	if dropped != 1 || out != "Có 2 văn bản hết hiệu lực. Bạn có thể bấm Gạch chân ở thẻ kết quả.\n" {
		t.Fatalf("dropped=%d out=%q", dropped, out)
	}
	// only the clause goes; other wordings too
	for in, want := range map[string]string{
		"Luật 29/2018/QH14 đã hết hiệu lực, tôi đã đánh dấu chỗ viện dẫn. ": "Luật 29/2018/QH14 đã hết hiệu lực. ",
		"Các chỗ cần xem đã tô vàng; hãy kiểm tra Điều 5.\n":                "hãy kiểm tra Điều 5.\n",
		"Tôi đã highlight 3 chỗ, còn 1 viện dẫn chưa rõ hiệu lực. ":         "còn 1 viện dẫn chưa rõ hiệu lực. ",
		"Tôi đã tổng hợp kết quả, gồm 5 văn bản. ":                          "Tôi đã tổng hợp kết quả, gồm 5 văn bản. ",
	} {
		if got, _ := cleanAnswer(ctx, in, b); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	// streamed the same way
	sctx := withTurnButtons(types.WithAnswerButtonGuard(ctx), []types.AgentStep{{ToolCalls: []types.ToolCall{legal}}})
	var got strings.Builder
	g := newAnswerGuard(sctx, func(s string) { got.WriteString(s) })
	for _, chunk := range []string{"Có 2 văn bản hết hiệu lực. Các viện dẫn này đã được ", "gạch chân trong văn bản. Bạn có thể bấm Gạch chân ở thẻ kết quả.\n"} {
		g.Write(chunk)
	}
	g.Flush()
	if got.String() != out {
		t.Fatalf("stream %q, whole %q", got.String(), out)
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
