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
	ctx = agenttools.WithExplicitMentions(ask("@Tờ trình.docx còn hiệu lực không"), 1)
	if got := firstRoundToolChoice(ctx, 0, offered); got != agenttools.ToolCheckDocumentLegalBasis {
		t.Fatalf("mentioned document: %q", got)
	}
	// an attachment naming a tab is no @: it does not point at the document
	ctx = types.WithMentionedDocuments(ask("Tờ trình còn hiệu lực không"), []string{"ws-2"})
	if got := firstRoundToolChoice(ctx, 0, offered); got != "" {
		t.Fatalf("attachment counted as a mention: %q", got)
	}
	// not offered (no open document): nothing forced
	if got := firstRoundToolChoice(ask("Rà soát căn cứ pháp lý"), 0, offered[:2]); got != "" {
		t.Fatalf("not offered: %q", got)
	}
}

func legalBasisStep(success bool, errText string, runnerFailed bool) types.AgentStep {
	r := &types.ToolResult{Success: success, Error: errText}
	if success {
		r.Output = "Đã rà soát căn cứ pháp lý của vb1: 3 văn bản được viện dẫn"
		r.Data = map[string]interface{}{"summary_line": "Đã rà soát căn cứ pháp lý của vb1: 3 văn bản được viện dẫn — hết hiệu lực 1; còn hiệu lực 2."}
	}
	if runnerFailed {
		r.Data = map[string]interface{}{agenttools.LegalBasisRunnerFailedKey: true}
	}
	return types.AgentStep{ToolCalls: []types.ToolCall{{Name: agenttools.ToolCheckDocumentLegalBasis, Result: r}}}
}

// A pure check is answered by its report, like a format check; a request
// asking more keeps the grounding gate until a search returns evidence.
func TestGroundingGateLegalBasis(t *testing.T) {
	gate := func(q string) context.Context {
		return types.WithGroundingRequired(types.WithUserQuery(context.Background(), q))
	}
	ok := []types.AgentStep{legalBasisStep(true, "", false)}
	if isUngrounded(markUngrounded(gate("Các văn bản viện dẫn còn hiệu lực không?"), ok)) {
		t.Fatal("pure check with its report: still gated")
	}
	if !isUngrounded(markUngrounded(gate("Các văn bản viện dẫn còn hiệu lực không?"), nil)) {
		t.Fatal("before the check runs the gate holds")
	}
	mixed := markUngrounded(gate("rà soát căn cứ pháp lý của văn bản này và cho biết mức phạt nồng độ cồn"), ok)
	if !isUngrounded(mixed) {
		t.Fatal("mixed request: the report alone must not open the gate")
	}
	// the safety net: the turn ends with the check's summary, not "no evidence"
	if got := ungroundedAnswerFor(mixed); got != "Đã rà soát căn cứ pháp lý của vb1: 3 văn bản được viện dẫn — hết hiệu lực 1; còn hiệu lực 2. "+legalBasisRestNote {
		t.Fatalf("safety net: %q", got)
	}
	// the check failed and the search found nothing: both are said
	failedMixed := markUngrounded(gate("rà soát căn cứ pháp lý của văn bản này và cho biết mức phạt nồng độ cồn"),
		[]types.AgentStep{legalBasisStep(false, "Không rà soát được căn cứ pháp lý của vb1: danh mục văn bản pháp luật tạm thời không khả dụng", true)})
	if got := ungroundedAnswerFor(failedMixed); got != "Không rà soát được căn cứ pháp lý của vb1: danh mục văn bản pháp luật tạm thời không khả dụng. "+ungroundedAnswer {
		t.Fatalf("failed check, no evidence: %q", got)
	}
	plain := markUngrounded(gate("mức phạt nồng độ cồn là bao nhiêu"), nil)
	if !isUngrounded(plain) || ungroundedAnswerFor(plain) != ungroundedAnswer {
		t.Fatal("without a check the usual answer")
	}
	if hasEvidence(ok) {
		t.Fatal("the report is not generic evidence")
	}
}

// A runner failure of a pure check ends the turn with why, written in code.
func TestLegalBasisFailureMessage(t *testing.T) {
	ask := func(q string) context.Context { return types.WithUserQuery(context.Background(), q) }
	pure := ask("Rà soát căn cứ pháp lý")
	const why = "Không rà soát được căn cứ pháp lý của vb1: danh mục văn bản pháp luật tạm thời không khả dụng."
	failed := legalBasisStep(false, why, true)
	if got := legalBasisFailureMessage(pure, []types.AgentStep{failed}); got != why {
		t.Fatalf("runner failure: %q", got)
	}
	// a resolve or argument error is the model's to retry
	if got := legalBasisFailureMessage(pure, []types.AgentStep{legalBasisStep(false, "không có văn bản vb3; hãy truyền tham số document", false)}); got != "" {
		t.Fatalf("resolve error: %q", got)
	}
	if got := legalBasisFailureMessage(pure, []types.AgentStep{legalBasisStep(false, "invalid arguments: unexpected end of JSON input", false)}); got != "" {
		t.Fatalf("argument error: %q", got)
	}
	// a call the 2-call cap refused
	refused := legalBasisStep(false, why, true)
	refused.ToolCalls[0].Result.Refused = true
	if got := legalBasisFailureMessage(pure, []types.AgentStep{refused}); got != "" {
		t.Fatalf("refused call: %q", got)
	}
	// vb1 checked in an earlier round, vb2 failed now: the model answers
	if got := legalBasisFailureMessage(pure, []types.AgentStep{legalBasisStep(true, "", false), failed}); got != "" {
		t.Fatalf("earlier success: %q", got)
	}
	both := types.AgentStep{ToolCalls: append(legalBasisStep(true, "", false).ToolCalls, failed.ToolCalls...)}
	if got := legalBasisFailureMessage(pure, []types.AgentStep{both}); got != "" {
		t.Fatalf("same-round success: %q", got)
	}
	// an impure request carries on (the search part)
	if got := legalBasisFailureMessage(ask("rà soát căn cứ pháp lý của văn bản này và cho biết mức phạt nồng độ cồn"), []types.AgentStep{failed}); got != "" {
		t.Fatalf("impure request: %q", got)
	}
	// a stop note for the model is not shown
	noted := legalBasisStep(false, "Không rà soát được căn cứ pháp lý của vb1: lỗi hệ thống.\n\n[Công cụ … ĐỪNG gọi lại]", true)
	if got := legalBasisFailureMessage(pure, []types.AgentStep{noted}); got != "Không rà soát được căn cứ pháp lý của vb1: lỗi hệ thống." {
		t.Fatalf("stop note: %q", got)
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
	for in, want := range map[string]string{
		// a subordinate clause goes, the sentence stays
		"Luật 29/2018/QH14 đã hết hiệu lực, tôi đã đánh dấu chỗ viện dẫn. ": "Luật 29/2018/QH14 đã hết hiệu lực. ",
		"Có 2 văn bản hết hiệu lực, đã được gạch chân trong văn bản. ":      "Có 2 văn bản hết hiệu lực. ",
		// the claim is the main clause: the sentence goes
		"Các chỗ cần xem đã tô vàng; hãy kiểm tra Điều 5.\n":        "",
		"Tôi đã highlight 3 chỗ, còn 1 viện dẫn chưa rõ hiệu lực. ": "",
		"Luật A, Luật B, đã được gạch chân. ":                       "",
		// not claims
		"Tôi đã tổng hợp kết quả, gồm 5 văn bản. ":                                    "Tôi đã tổng hợp kết quả, gồm 5 văn bản. ",
		"Tôi đã tô màu bảng theo yêu cầu. ":                                           "Tôi đã tô màu bảng theo yêu cầu. ",
		"NĐ 100 đã được đánh dấu hết hiệu lực trong danh mục, nên thay bằng NĐ 168. ": "NĐ 100 đã được đánh dấu hết hiệu lực trong danh mục, nên thay bằng NĐ 168. ",
		// a list item keeps its text
		"- Luật Đất đai 2013: hết hiệu lực (đã gạch chân)\n":     "- Luật Đất đai 2013: hết hiệu lực\n",
		"- Luật Đất đai 2013: hết hiệu lực, đã được gạch chân\n": "- Luật Đất đai 2013: hết hiệu lực\n",
		// review round 3: statuses with emphasis or a place, negated or
		// hypothetical claims are no claims
		"NĐ 100 đã được đánh dấu **hết hiệu lực** trên vbpl.vn. ":  "NĐ 100 đã được đánh dấu **hết hiệu lực** trên vbpl.vn. ",
		"NĐ 100 đã được đánh dấu trong danh mục là hết hiệu lực. ": "NĐ 100 đã được đánh dấu trong danh mục là hết hiệu lực. ",
		"Tôi chưa gạch chân chỗ nào, bạn bấm nút trên thẻ nhé. ":   "Tôi chưa gạch chân chỗ nào, bạn bấm nút trên thẻ nhé. ",
		"Sau khi bạn đã gạch chân, hãy lưu văn bản. ":              "Sau khi bạn đã gạch chân, hãy lưu văn bản. ",
		"Nếu đã tô vàng các chỗ viện dẫn, hãy xem lại. ":           "Nếu đã tô vàng các chỗ viện dẫn, hãy xem lại. ",
		// the claim ends a sentence of short list items: it goes whole
		"Luật 29/2018/QH14, Nghị định 100 — tôi đã đánh dấu. ": "",
		// a separator inside brackets does not cut the clause
		"Có 2 văn bản hết hiệu lực (Luật 29, Nghị định 100), đã được gạch chân trong văn bản. ": "Có 2 văn bản hết hiệu lực (Luật 29, Nghị định 100). ",
		// a numbered item whose text is the claim leaves no bare marker
		"Kết quả:\n1. Đã gạch chân các chỗ viện dẫn hết hiệu lực.\n2. Còn 1 văn bản chưa rõ.\n": "Kết quả:\n2. Còn 1 văn bản chưa rõ.\n",
		// a table row keeps its cells
		"| 29/2018/QH14 | Hết hiệu lực (đã gạch chân) | Luật 117/2025 |\n": "| 29/2018/QH14 | Hết hiệu lực | Luật 117/2025 |\n",
	} {
		if got, _ := cleanAnswer(ctx, in, b); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	// streamed the same way
	sctx := withTurnButtons(types.WithAnswerButtonGuard(ctx), []types.AgentStep{{ToolCalls: []types.ToolCall{legal}}})
	stream := func(chunks ...string) string {
		var got strings.Builder
		g := newAnswerGuard(sctx, func(s string) { got.WriteString(s) })
		for _, c := range chunks {
			g.Write(c)
		}
		g.Flush()
		return got.String()
	}
	if got := stream("Có 2 văn bản hết hiệu lực. Các viện dẫn này đã được ", "gạch chân trong văn bản. Bạn có thể bấm Gạch chân ở thẻ kết quả.\n"); got != out {
		t.Fatalf("stream %q, whole %q", got, out)
	}
	// a dropped line keeps its break exactly as cleanAnswer does
	for _, text := range []string{
		"Kết quả:\n1. Đã gạch chân các chỗ viện dẫn hết hiệu lực.\n2. Còn 1 văn bản chưa rõ.\n",
		"Tôi đã highlight 3 chỗ viện dẫn.\nCòn 1 viện dẫn chưa rõ.\n",
		"Kết quả:\nTôi đã highlight 3 chỗ viện dẫn.\nCòn 1 viện dẫn chưa rõ.\n",
		"Kết quả: 2 văn bản. Tôi đã highlight 3 chỗ viện dẫn.\nCòn 1.\n",
	} {
		whole, _ := cleanAnswer(ctx, text, b)
		if got := stream(text); got != whole {
			t.Errorf("stream %q, whole %q", got, whole)
		}
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
