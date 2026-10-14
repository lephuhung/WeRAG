package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
)

// The format-check tests observe the evaluation through the model call
// (fakeChat.evaluation, countingChat.evals): they run the full model
// evaluation. TestCheckDocumentFormatJudgesByRules covers production.
func init() { formatEvalOptions.ForceLLM = true }

// In production the judgment rules decide the Bảng kiểm of a well-formed
// công văn: no model call, and the result counts as evaluated.
func TestCheckDocumentFormatJudgesByRules(t *testing.T) {
	saved := formatEvalOptions
	formatEvalOptions = docformat.EvalOptions{}
	t.Cleanup(func() { formatEvalOptions = saved })
	checkInline(t)
	resetFormatChecks(t)
	model := &countingChat{fakeChat: fakeChat{reply: "{}"}} // an evaluation call would fail
	tool := NewCheckDocumentFormatToolForWorkspace(newFakeWorkspace(docxFixture(t)), model, "sess-rules")
	res, err := tool.Execute(toolCtx(), json.RawMessage(`{}`))
	if err != nil || !res.Success || res.Data["evaluated"] != true {
		t.Fatalf("result: %+v %v", res, err)
	}
	if n := model.evalCount(); n != 0 {
		t.Fatalf("model evaluations = %d, want 0", n)
	}
	if !strings.Contains(res.Output, "## Bảng kiểm") || strings.Contains(res.Output, "<skill") {
		t.Fatalf("output:\n%s", res.Output)
	}
}
