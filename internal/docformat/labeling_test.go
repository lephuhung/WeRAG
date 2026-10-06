package docformat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) ([]byte, parityFixture) {
	t.Helper()
	content, err := os.ReadFile("testdata/parity/" + name + ".docx")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/parity/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var fx parityFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	return content, fx
}

// fakeLLM answers with a canned reply and records what it was asked.
type fakeLLM struct {
	replies []string
	err     error
	calls   [][]Message
}

func (f *fakeLLM) Complete(_ context.Context, msgs []Message) (string, error) {
	f.calls = append(f.calls, msgs)
	if f.err != nil {
		return "", f.err
	}
	r := f.replies[0]
	if len(f.replies) > 1 {
		f.replies = f.replies[1:]
	}
	return r, nil
}

// goldReply labels the thông báo fixture whose centred, cue-less signature
// block the positional heuristic cannot see.
func goldReply(t *testing.T, content []byte) string {
	t.Helper()
	l := InspectDocx(content)
	gold := map[string]string{
		"SỞ Y TẾ": "co_quan_ban_hanh", "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM": "quoc_hieu",
		"Độc lập - Tự do - Hạnh phúc": "tieu_ngu", "Số: 12/TB-SYT": "so_ky_hieu",
		"Huế, ngày 05 tháng 10 năm 2026": "dia_danh_ngay_thang", "THÔNG BÁO": "trich_yeu",
		"Lịch tiếp công dân tháng 10/2026": "trich_yeu", "GIÁM ĐỐC": "chuc_danh",
		"Phạm Văn Em": "nguoi_ky", "Nơi nhận:": "noi_nhan", "- Lưu: VT.": "noi_nhan",
	}
	labels := map[string]string{}
	for _, u := range BuildUnits(l, nil) {
		if g, ok := gold[u.Text]; ok {
			labels[u.ID] = g
		} else {
			labels[u.ID] = "noi_dung"
		}
	}
	b, _ := json.Marshal(map[string]any{"document_type": "thong_bao", "labels": labels})
	return string(b)
}

func TestLLMLabelsFixWhatTheHeuristicMisses(t *testing.T) {
	content, _ := fixture(t, "fx_flat_centered")
	heur := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic})
	if strings.Contains(heur.Components["nguoi_ky"].textOrEmpty(), "Phạm Văn Em") {
		t.Fatal("fixture no longer exercises the heuristic gap")
	}
	llm := &fakeLLM{replies: []string{"Kết quả:\n```json\n" + goldReply(t, content) + "\n```"}}
	r := Check(context.Background(), content, Options{LLM: llm, ModelName: "qwen"})
	if !r.OK || r.Segmentation.Method != SegmenterLLM || r.Segmentation.Model != "qwen" {
		t.Fatalf("unexpected segmentation: %+v", r.Segmentation)
	}
	if got := r.Components["nguoi_ky"].Text; got != "Phạm Văn Em" {
		t.Fatalf("nguoi_ky = %q", got)
	}
	// the signer is set at 11pt: only caught once labelled nguoi_ky
	var size *CheckResult
	for i := range r.Checks {
		if r.Checks[i].ID == "nguoi_ky.size" {
			size = &r.Checks[i]
		}
	}
	if size == nil || size.Status == StatusPass || size.Evidence[0]["text"] != "Phạm Văn Em" {
		t.Fatalf("nguoi_ky.size = %+v", size)
	}
	if len(llm.calls) != 1 || llm.calls[0][0].Role != "system" ||
		!strings.Contains(llm.calls[0][1].Content, "Phạm Văn Em") {
		t.Fatalf("prompt not sent as expected: %+v", llm.calls)
	}
}

func (c *Component) textOrEmpty() string {
	if c == nil {
		return ""
	}
	return c.Text
}

func TestLLMRetriesOnceOnNonJSON(t *testing.T) {
	content, _ := fixture(t, "fx_flat_centered")
	llm := &fakeLLM{replies: []string{"xin lỗi, tôi không chắc", goldReply(t, content)}}
	r := Check(context.Background(), content, Options{LLM: llm})
	if r.Segmentation.Method != SegmenterLLM || len(llm.calls) != 2 {
		t.Fatalf("method=%s calls=%d", r.Segmentation.Method, len(llm.calls))
	}
	if last := llm.calls[1]; last[len(last)-1].Role != "user" || last[len(last)-2].Role != "assistant" {
		t.Fatal("corrective turn missing")
	}
}

func TestLLMFailureFallsBackToHeuristic(t *testing.T) {
	content, _ := fixture(t, "fx_flat_centered")
	r := Check(context.Background(), content, Options{LLM: &fakeLLM{err: errors.New("upstream down")}})
	if !r.OK || r.Segmentation.Method != SegmenterHeuristic || !strings.Contains(r.Segmentation.Error, "upstream down") {
		t.Fatalf("segmentation = %+v", r.Segmentation)
	}
}

func TestApplyLabelsDiagnosticsAndStructuralType(t *testing.T) {
	content, _ := fixture(t, "fx_flat_centered")
	reply, err := ParseReply(goldReply(t, content))
	if err != nil {
		t.Fatal(err)
	}
	l := InspectDocx(content)
	task := PrepareTask(l, Segment(l))
	first, second := task.All[0].ID, task.All[1].ID
	delete(reply.Labels, first)
	reply.Labels[second] = "khong_ton_tai"
	reply.Labels["999"] = "noi_dung"
	reply.DocumentType = "bao_cao" // structural heading THÔNG BÁO must win
	seg, diag := ApplyLabels(l, reply, task)
	if len(diag.Missing) != 1 || diag.Missing[0] != first {
		t.Fatalf("missing = %v", diag.Missing)
	}
	if len(diag.Invalid) != 1 || diag.Invalid[0]["id"] != second {
		t.Fatalf("invalid = %v", diag.Invalid)
	}
	if len(diag.UnknownIDs) != 1 || diag.UnknownIDs[0] != "999" {
		t.Fatalf("unknown = %v", diag.UnknownIDs)
	}
	if seg.DetectedType != "thong_bao" || diag.DocumentType["llm"] != "bao_cao" {
		t.Fatalf("type = %s diag=%v", seg.DetectedType, diag.DocumentType)
	}
}

func TestLongBodyIsElided(t *testing.T) {
	content, _ := fixture(t, "fx_flat_centered")
	l := InspectDocx(content)
	// grow the body to 400 paragraphs by cloning a body paragraph
	body := l.Paragraphs[7]
	var paras []*Para
	paras = append(paras, l.Paragraphs[:7]...)
	for i := 0; i < 400; i++ {
		c := *body
		c.Index = len(paras)
		paras = append(paras, &c)
	}
	for _, p := range l.Paragraphs[8:] {
		c := *p
		c.Index = len(paras)
		paras = append(paras, &c)
	}
	l.Paragraphs = paras
	task := PrepareTask(l, Segment(l))
	if len(task.Units) >= 120 || task.ElidedCount < 250 {
		t.Fatalf("sent %d units, elided %d", len(task.Units), task.ElidedCount)
	}
	seg, diag := ApplyLabels(l, &Reply{Labels: map[string]string{}}, task)
	if len(diag.Missing) >= 120 || !seg.Comp("noi_dung").Found {
		t.Fatalf("missing=%d", len(diag.Missing))
	}
}

func TestRejectsNonDocx(t *testing.T) {
	r := Check(context.Background(), []byte("not a docx"), Options{})
	if r.OK || r.Error == "" {
		t.Fatalf("report = %+v", r)
	}
	if !strings.Contains(RenderText(r), "Không kiểm tra được") {
		t.Fatal("render of a failed report")
	}
}

func TestRenderTextListsFindings(t *testing.T) {
	content, _ := fixture(t, "fx_good_cong_van_arial")
	r := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic, SourceName: "cv.docx"})
	txt := RenderText(r)
	for _, want := range []string{"cv.docx", "## Lỗi", "noi_dung.font", "Arial", "## Thành phần bóc tách được", "Quốc hiệu"} {
		if !strings.Contains(txt, want) {
			t.Errorf("render lacks %q:\n%s", want, txt)
		}
	}
}

func TestRuleSetOverrideByID(t *testing.T) {
	rs, err := LoadRuleSet("bien_ban")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range rs.Checks {
		if c.ID == "signature.zone" {
			n++
			if _, ok := c.Value.([]any); !ok {
				t.Fatalf("bien_ban signature.zone not overridden: %v", c.Value)
			}
		}
	}
	if n != 1 {
		t.Fatalf("signature.zone appears %d times", n)
	}
	if len(AvailableTypes()) < 11 {
		t.Fatalf("types = %v", AvailableTypes())
	}
}
