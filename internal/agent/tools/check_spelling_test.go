package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// spellChat answers each call with the next reply.
type spellChat struct {
	chatBase
	replies []string
	err     error
	calls   int
	opts    []*chat.ChatOptions
	users   []string
}

func (f *spellChat) Chat(_ context.Context, msgs []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	f.opts = append(f.opts, opts)
	f.users = append(f.users, msgs[len(msgs)-1].Content)
	if f.err != nil {
		return nil, f.err
	}
	r := f.replies[min(f.calls, len(f.replies)-1)]
	f.calls++
	return &types.ChatResponse{Content: r}, nil
}

func (f *spellChat) GetModelName() string { return "spell-model" }
func (f *spellChat) GetModelID() string   { return "spell-model" }

func spellDoc(t *testing.T) []byte {
	body := strings.Join([]string{
		testPara("Sở Nội vụ đề nghi các đơn vị triễn khai kế hoạch.", "both", "Times New Roman", 14, false, false),
		testPara("Chính p`hủ ban hành kế hoạch cải cách.", "both", "Times New Roman", 14, false, false),
		testPara("Báo cáo gửi về trước ngày 30 tháng 11.", "both", "Times New Roman", 14, false, false),
		testPara("", "", "", 0, false, false),
		testPara("Sở Nội vụ đề nghi các đơn vị triễn khai kế hoạch.", "both", "Times New Roman", 14, false, false),
	}, "")
	return buildTestDocx(t, body, nd30Margins)
}

const spellReply = "```json\n" + `{"findings":[
	{"paragraph":0,"wrong":"đề nghi","correct":"đề nghị","reason":"thiếu dấu nặng"},
	{"paragraph":0,"wrong":"triễn khai","correct":"triển khai","reason":"sai dấu"},
	{"paragraph":0,"wrong":"triễn khai","correct":"triển khai","reason":"lặp"},
	{"paragraph":1,"wrong":"p` + "`" + `hủ","correct":"phủ","reason":"ký tự thừa"},
	{"paragraph":2,"wrong":"không có","correct":"x","reason":"bịa"},
	{"paragraph":2,"wrong":"Báo cáo","correct":"Báo cáo","reason":"không đổi"},
	{"paragraph":9,"wrong":"x","correct":"y","reason":"ngoài lô"},
	{"paragraph":"4","wrong":"triễn khai","correct":"triển khai","reason":"sai dấu"}
]}` + "\n```"

func spellFindings(t *testing.T, res *types.ToolResult) []SpellingFinding {
	t.Helper()
	f, ok := res.Data["findings"].([]SpellingFinding)
	if !ok {
		t.Fatalf("findings = %T", res.Data["findings"])
	}
	return f
}

func TestCheckSpellingValidatesDedupesAndMarks(t *testing.T) {
	ws := newFakeWorkspace(spellDoc(t))
	model := &spellChat{replies: []string{spellReply}}
	res := runTool(t, NewCheckSpellingTool(ws, model, "s"), `{}`)
	if !res.Success || res.Data["scope"] != "document" {
		t.Fatalf("result: %+v", res)
	}
	got := spellFindings(t, res)
	want := [][4]string{
		{"0", "đề nghi", "đề nghị", "thiếu dấu nặng"},
		{"0", "triễn khai", "triển khai", "sai dấu"},
		{"1", "p`hủ", "phủ", strayCharReason}, // found in code, the model's copy deduped
		{"4", "triễn khai", "triển khai", "sai dấu"},
	}
	if len(got) != len(want) {
		t.Fatalf("findings: %+v", got)
	}
	for i, w := range want {
		f := got[i]
		if [4]string{strconv.Itoa(f.Paragraph), f.Wrong, f.Correct, f.Reason} != w || f.Unmarked {
			t.Errorf("finding %d = %+v, want %v", i, f, w)
		}
	}
	if model.calls != 1 || len(model.opts) != 1 || model.opts[0].Thinking == nil || *model.opts[0].Thinking ||
		model.opts[0].Temperature > 0.2 {
		t.Fatalf("model call: calls=%d opts=%+v", model.calls, model.opts)
	}
	if strings.Contains(model.users[0], "[3]") {
		t.Fatal("empty paragraphs must not be sent")
	}
	ops := resultOps(t, res)
	if len(ops) != 4 || ops[0].Op != OpMark || ops[0].Style != "underline" || ops[0].Text != "đề nghi" ||
		ops[3].Anchor.Occurrence != 2 || ops[2].Text != "p`hủ" {
		t.Fatalf("ops: %+v", ops)
	}
	if len(ws.snapshots) != 1 || ws.snapshots[0] != "ai: kiểm tra chính tả" || res.Data["snapshot_seq"] != 11 {
		t.Fatalf("snapshot: %v", ws.snapshots)
	}
	for _, want := range []string{"Phát hiện 4 lỗi", "1. Đoạn [0]: “đề nghi” → “đề nghị” (thiếu dấu nặng)",
		"3. Đoạn [1]: “p`hủ” → “phủ” (ký tự lạ trong từ)", "gạch chân đỏ", "không bị sửa"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q:\n%s", want, res.Output)
		}
	}
	// the plan underlines without changing the text
	before := paraTexts(t, ws.content)
	applyPlan(t, ws, res)
	if strings.Join(paraTexts(t, ws.content), "|") != strings.Join(before, "|") {
		t.Fatal("marking changed the text")
	}
}

func TestCheckSpellingSelectionScope(t *testing.T) {
	ws := newFakeWorkspace(spellDoc(t))
	model := &spellChat{replies: []string{spellReply}}
	ctx := types.WithDocumentSelection(toolCtx(), &types.DocumentSelection{Text: "Chính p`hủ ban hành kế hoạch cải cách."})
	res, err := NewCheckSpellingTool(ws, model, "s").Execute(ctx, json.RawMessage(`{"mark":false}`))
	if err != nil || !res.Success || res.Data["scope"] != "selection" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	got := spellFindings(t, res)
	if len(got) != 1 || got[0].Paragraph != 1 || got[0].Reason != strayCharReason {
		t.Fatalf("only the selected paragraph is reviewed: %+v", got)
	}
	if !strings.Contains(model.users[0], "[1] Chính") || strings.Contains(model.users[0], "[0]") {
		t.Fatalf("model saw: %s", model.users[0])
	}
	if _, ok := res.Data["document_ops"]; ok || len(ws.snapshots) != 0 {
		t.Fatal("mark=false: no ops and no snapshot")
	}
	// scope=selection without a selection is refused
	res = runTool(t, NewCheckSpellingTool(ws, model, "s"), `{"scope":"selection"}`)
	if res.Success || !strings.Contains(res.Error, "bôi đen") {
		t.Fatalf("result: %+v", res)
	}
}

func TestCheckSpellingHandlesBadReplies(t *testing.T) {
	// not JSON twice: the batch fails, the code-found typo is still reported
	ws := newFakeWorkspace(spellDoc(t))
	model := &spellChat{replies: []string{"Tôi thấy vài lỗi.", "vẫn không phải JSON"}}
	res := runTool(t, NewCheckSpellingTool(ws, model, "s"), `{}`)
	if !res.Success || model.calls != 2 || res.Data["failed_batches"] != 1 {
		t.Fatalf("result: %+v calls=%d", res, model.calls)
	}
	if got := spellFindings(t, res); len(got) != 1 || got[0].Reason != strayCharReason {
		t.Fatalf("findings: %+v", got)
	}
	if !strings.Contains(res.Output, "có thể còn sót lỗi") {
		t.Fatalf("output: %s", res.Output)
	}
	// the retry fixes it
	model = &spellChat{replies: []string{"xin lỗi", `[{"paragraph":2,"wrong":"30 tháng 11","correct":"30/11","reason":"x"}]`}}
	res = runTool(t, NewCheckSpellingTool(newFakeWorkspace(spellDoc(t)), model, "s"), `{"mark":false}`)
	if got := spellFindings(t, res); !res.Success || len(got) != 2 {
		t.Fatalf("findings after retry: %+v", got)
	}
	// a clean document with a model that is down
	clean := buildTestDocx(t, testPara("Báo cáo gửi về trước ngày 30.", "both", "Times New Roman", 14, false, false), nd30Margins)
	res = runTool(t, NewCheckSpellingTool(newFakeWorkspace(clean), &spellChat{err: errors.New("down")}, "s"), `{}`)
	if res.Success || !strings.Contains(res.Error, "không trả lời") {
		t.Fatalf("result: %+v", res)
	}
	// no findings
	res = runTool(t, NewCheckSpellingTool(newFakeWorkspace(clean), &spellChat{replies: []string{`{"findings":[]}`}}, "s"), `{}`)
	if !res.Success || len(spellFindings(t, res)) != 0 || !strings.Contains(res.Output, "Không phát hiện") {
		t.Fatalf("result: %+v", res)
	}
}

func TestCheckSpellingWindowAndBatches(t *testing.T) {
	var paras []string
	for i := 0; i < 90; i++ {
		paras = append(paras, testPara("Đoạn số "+itoaTest(i)+" không có lỗi.", "both", "Times New Roman", 14, false, false))
	}
	ws := newFakeWorkspace(buildTestDocx(t, strings.Join(paras, ""), nd30Margins))
	model := &spellChat{replies: []string{`{"findings":[]}`}}
	res := runTool(t, NewCheckSpellingTool(ws, model, "s"), `{"from":5,"limit":70}`)
	if !res.Success || model.calls != 2 || res.Data["next_from"] != 75 || !strings.Contains(res.Output, "from=75") {
		t.Fatalf("result: %+v calls=%d", res.Data, model.calls)
	}
	if !strings.Contains(model.users[0], "[5] ") || !strings.Contains(model.users[1], "[74] ") || strings.Contains(model.users[1], "[75] ") {
		t.Fatal("window not respected")
	}
	if res := runTool(t, NewCheckSpellingTool(ws, nil, "s"), `{}`); res.Success {
		t.Fatal("no model must refuse")
	}
}

func TestSpellcheckPromptMatchesTemplate(t *testing.T) {
	data, err := os.ReadFile("../../../config/prompt_templates/spellcheck_review.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Templates []struct {
			ID      string `yaml:"id"`
			Content string `yaml:"content"`
		} `yaml:"templates"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Templates) != 1 || file.Templates[0].ID != "spellcheck_review" {
		t.Fatalf("templates: %+v", file.Templates)
	}
	if strings.TrimSpace(file.Templates[0].Content) != strings.TrimSpace(DefaultSpellcheckPrompt) {
		t.Fatal("spellcheck_review.yaml and DefaultSpellcheckPrompt differ; keep them identical")
	}
	for _, rule := range []string{"hoà/hòa", "thuỷ/thủy", "lí/lý", "kĩ/kỹ", "all capitals", "proper nouns", "document numbers", "doubled words"} {
		if !strings.Contains(DefaultSpellcheckPrompt, rule) {
			t.Errorf("prompt lacks %q", rule)
		}
	}
}

func TestLocateInParagraphMapsBackToRawText(t *testing.T) {
	// NBSP, zero-width space and an NFD "ị" (i + U+0323) in the paragraph
	text := "Sở Nội vụ đề nghị​ các đơn vị; đề nghị lại"
	raw, occ, ok := locateInParagraph(text, "Sở Nội vụ", "")
	if !ok || raw != "Sở Nội vụ" || occ != 1 {
		t.Fatalf("raw=%q occ=%d ok=%v", raw, occ, ok)
	}
	raw, _, ok = locateInParagraph(text, "đề nghị", "")
	if !ok || raw != "đề nghị" {
		t.Fatalf("raw=%q ok=%v", raw, ok)
	}
	// inside a highlighted part: the second instance
	raw, occ, ok = locateInParagraph("đề nghi các đơn vị; xin đề nghi lại", "đề nghi", "xin đề nghi lại")
	if !ok || raw != "đề nghi" || occ != 2 {
		t.Fatalf("raw=%q occ=%d ok=%v", raw, occ, ok)
	}
	if _, _, ok := locateInParagraph(text, "không có", ""); ok {
		t.Fatal("absent text found")
	}
	// part of a composed letter cannot be underlined on its own
	if raw, _, ok := locateInParagraph("nghị", "i", ""); ok && raw != "" {
		t.Fatalf("raw=%q", raw)
	}
}

func TestCheckSpellingMarksRawTextAndOccurrence(t *testing.T) {
	body := testPara("Đề nghi các đơn vị.", "both", "Times New Roman", 14, false, false) +
		testPara("Sở Nội vụ đề nghi; đề nghi lại.", "both", "Times New Roman", 14, false, false)
	ws := newFakeWorkspace(buildTestDocx(t, body, nd30Margins))
	model := &spellChat{replies: []string{`{"findings":[
		{"paragraph":1,"wrong":"Sở Nội vụ đề nghi","correct":"Sở Nội vụ đề nghị","reason":"thiếu dấu"},
		{"paragraph":1,"wrong":"đề nghi lại","correct":"đề nghị lại","reason":"thiếu dấu"}]}`}}
	ctx := types.WithDocumentSelection(toolCtx(), &types.DocumentSelection{Text: "đề nghi lại."})
	res, err := NewCheckSpellingTool(ws, model, "s").Execute(ctx, json.RawMessage(`{}`))
	if err != nil || !res.Success {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// only the finding inside the highlighted part survives
	got := spellFindings(t, res)
	if len(got) != 1 || got[0].Wrong != "đề nghi lại" {
		t.Fatalf("findings: %+v", got)
	}
	ops := resultOps(t, res)
	if len(ops) != 1 || ops[0].Text != "đề nghi lại" || ops[0].TextOccurrence != 0 {
		t.Fatalf("ops: %+v", ops)
	}

	// document scope: the NBSP finding is underlined with the paragraph's raw text
	model = &spellChat{replies: []string{`{"findings":[{"paragraph":1,"wrong":"Sở Nội vụ đề nghi","correct":"Sở Nội vụ đề nghị","reason":"thiếu dấu"}]}`}}
	res = runTool(t, NewCheckSpellingTool(newFakeWorkspace(buildTestDocx(t, body, nd30Margins)), model, "s"), `{}`)
	ops = resultOps(t, res)
	if len(ops) != 1 || ops[0].Text != "Sở Nội vụ đề nghi" {
		t.Fatalf("ops: %+v", ops)
	}
}
