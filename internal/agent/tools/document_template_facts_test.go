package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

const settledPlanMessage = "Chốt như sau: 3 lớp, mỗi lớp 40 học viên, từ ngày 25/10/2026 đến 27/10/2026 tại hội trường Công an tỉnh; " +
	"giảng viên mời từ Sở Khoa học và Công nghệ; kinh phí 50 triệu đồng từ ngân sách đơn vị; Phòng Tham mưu chủ trì. " +
	"Người ký: Đại tá Nguyễn Văn An, Phó Giám đốc, ký thay Giám đốc."

func TestSpokenValue(t *testing.T) {
	user := settledPlanMessage
	cases := []struct {
		v        string
		head     bool
		want     string
		wantPart bool
	}{
		// the model lengthened the title with words of the conversation
		{"Phó Giám đốc Công an tỉnh", true, "Phó Giám đốc", false},
		{"Giám đốc Công an tỉnh", true, "Giám đốc", false},
		// the rest is the model's own: nothing is kept
		{"Phó Giám đốc Sở Tài chính", true, "", false},
		// placeholders are no part of the value
		{"CÔNG AN TỈNH ……………", true, "CÔNG AN TỈNH", true},
		{"Phòng Tham mưu [...]", true, "Phòng Tham mưu", true},
		{"Phòng Tham mưu ...", true, "Phòng Tham mưu", true},
		{"[tên cơ quan]", true, "", true},
		// a name is never cut ("Nguyễn Văn" of "Nguyễn Văn Anh")
		{"Nguyễn Văn Anh", false, "", false},
		{"Đại tá Nguyễn Văn An", false, "Đại tá Nguyễn Văn An", false},
		// a one-word head is no title
		{"Phó Chủ tịch", true, "", false},
	}
	for _, c := range cases {
		got, part := spokenValue(user, user, c.v, c.head)
		if got != c.want || part != c.wantPart {
			t.Errorf("spokenValue(%q) = %q, %v; want %q, %v", c.v, got, part, c.want, c.wantPart)
		}
	}
	// the rest must be whole words of the conversation: "an" is not in "thanh"
	if got, _ := spokenValue("Phó Giám đốc, thanh tra", "thanh tra", "Phó Giám đốc An", true); got != "" {
		t.Errorf("rest found inside a word: %q", got)
	}
}

// The signer and the agency the model decorated are the user's: the
// signer is not asked again and a placeholder agency keeps what the user
// wrote, with a blank for the rest.
func TestDocumentTemplateSignerTitleLengthenedByTheModel(t *testing.T) {
	d := planDraft()
	d["agency_name"] = "CÔNG AN TỈNH ……………"
	d["paragraphs"] = []map[string]string{{"kind": "body", "text": "Tổ chức 3 lớp, mỗi lớp 40 học viên từ 25/10/2026 đến 27/10/2026, kinh phí 50 triệu đồng."}}
	d["signer"] = map[string]string{"name": "Nguyễn Văn An", "position": "Phó Giám đốc Công an tỉnh", "authority": "KT", "on_behalf_of": "Giám đốc Công an tỉnh"}
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	in := templateInput(model, "ke-hoach", "Soạn kế hoạch từ nội dung đã trao đổi")
	in.Profile = nil
	in.Conversation = []TemplateTurn{{Role: "user", Text: settledPlanMessage}}
	out, err := PrepareDocumentTemplate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	missing := strings.Join(out.Data["missing_fields"].([]string), "\n")
	if strings.Contains(missing, "Người ký") || strings.Contains(missing, "Hình thức ký") {
		t.Fatalf("signer asked again: %s", missing)
	}
	if !strings.Contains(missing, "Tên cơ quan ban hành") {
		t.Fatalf("a placeholder agency taken as known: %s", missing)
	}

	in.Inputs = append(in.Inputs, "bỏ qua")
	in.LastDraft = out.Draft
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if !out.Ready {
		t.Fatalf("skip not ready: %s", out.Answer)
	}
	f := out.Data["fields"].(map[string]interface{})
	if f["agency"] != "CÔNG AN TỈNH "+templateBlank {
		t.Errorf("agency = %q", f["agency"])
	}
	if sig := strings.Join(f["signature_lines"].([]string), "|"); sig != "KT. GIÁM ĐỐC|PHÓ GIÁM ĐỐC" {
		t.Errorf("signature = %q", sig)
	}
	if f["signer_name"] != "Nguyễn Văn An" {
		t.Errorf("signer = %q", f["signer_name"])
	}
}

// factTexts are the facts' texts, sorted.
func factTexts(fs []settledFact) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Text)
	}
	sort.Strings(out)
	return out
}

func TestSettledFacts(t *testing.T) {
	got := strings.Join(factTexts(settledFacts([]TemplateTurn{
		{Role: "user", Text: "Tôi cần tổ chức lớp tập huấn theo Nghị định 30/2020/NĐ-CP, kế hoạch nên có những gì? Có nên mời 60 học viên?"},
		{Role: "assistant", Text: "Gợi ý: kinh phí 80 triệu đồng, 2 lớp."},
		{Role: "user", Text: settledPlanMessage},
	}, []string{"Tạo cho tôi văn bản kế hoạch theo NĐ30, thông tin tôi cần là: như trên"})), "|")
	want := "25/10/2026|27/10/2026|3 lớp|40 học viên|50 triệu đồng"
	if got != want {
		t.Errorf("facts = %q, want %q", got, want)
	}

	// a later statement revises: the count of the same noun, the dates of
	// a statement that moves them, the figure after "thay vì"
	got = strings.Join(factTexts(settledFacts([]TemplateTurn{
		{Role: "user", Text: settledPlanMessage},
		{Role: "user", Text: "Sửa lại: mỗi lớp 35 học viên thay vì 40 học viên. Dời sang 26/10/2026."},
	}, nil)), "|")
	want = "26/10/2026|3 lớp|35 học viên|50 triệu đồng"
	if got != want {
		t.Errorf("revised facts = %q, want %q", got, want)
	}
	// codes and references are no counts
	if fs := settledFacts(nil, []string{"Đơn vị PV01 soạn theo Điều 5 Thông tư 01, 1 văn bản, giao 2 văn bản."}); len(fs) != 0 {
		t.Errorf("references taken as facts: %q", factTexts(fs))
	}
}

func TestDraftOmissions(t *testing.T) {
	facts := settledFacts([]TemplateTurn{{Role: "user", Text: settledPlanMessage}}, nil)
	d := &templateDraft{Paragraphs: []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}{{Kind: "body", Text: "Tổ chức ba lớp, mỗi lớp 40 học viên, từ ngày 25 tháng 10 năm 2026 đến ngày 27/10/2026; kinh phí 50.000.000 đồng."}}}
	if left := draftOmissions(d, facts); len(left) != 0 {
		t.Errorf("present facts reported left out: %q", left)
	}
	d.Paragraphs[0].Text = "Tổ chức 3 lớp, mỗi lớp 40 học viên, từ 25/10/2026 đến 27/10/2026."
	if left := draftOmissions(d, facts); strings.Join(left, "|") != "50 triệu đồng" {
		t.Errorf("left out = %q", left)
	}
}

func settledPlanDraft(body string) map[string]any {
	d := planDraft()
	d["paragraphs"] = []map[string]string{{"kind": "body", "text": body}}
	d["signer"] = map[string]string{"name": "Nguyễn Văn An", "position": "Phó Giám đốc", "authority": "KT", "on_behalf_of": "Giám đốc"}
	return d
}

const (
	fullPlanBody  = "Tổ chức 3 lớp, mỗi lớp 40 học viên, từ 25/10/2026 đến 27/10/2026; kinh phí 50 triệu đồng."
	shortPlanBody = "Tổ chức 3 lớp, mỗi lớp 40 học viên, từ 25/10/2026 đến 27/10/2026."
)

// A draft that drops a settled figure is asked once more, told what it
// left out; a retry that does not fix it is not taken.
func TestDocumentTemplateRetriesLeftOutFacts(t *testing.T) {
	model := &scriptedChat{replies: []string{draftJSON(t, settledPlanDraft(shortPlanBody)), draftJSON(t, settledPlanDraft(fullPlanBody))}}
	in := templateInput(model, "ke-hoach", "Soạn kế hoạch từ nội dung đã trao đổi")
	in.Conversation = []TemplateTurn{{Role: "user", Text: settledPlanMessage}}
	out, err := PrepareDocumentTemplate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(model.calls))
	}
	if c := model.calls[1][len(model.calls[1])-1].Content; !strings.HasPrefix(c, "Bạn đã bỏ sót: 50 triệu đồng") {
		t.Errorf("correction = %q", c)
	}
	if !strings.Contains(string(out.Draft), "50 triệu") {
		t.Errorf("retry not taken: %s", out.Draft)
	}

	// the retry invents a figure: the first draft stays
	model = &scriptedChat{replies: []string{draftJSON(t, settledPlanDraft(shortPlanBody)), draftJSON(t, settledPlanDraft(fullPlanBody+" Hạn báo cáo 15/11/2026."))}}
	in.Model = model
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if len(model.calls) != 2 || strings.Contains(string(out.Draft), "15/11") {
		t.Errorf("an inventing retry was taken (calls %d): %s", len(model.calls), out.Draft)
	}

	// nothing left out: one call
	model = &scriptedChat{replies: []string{draftJSON(t, settledPlanDraft(fullPlanBody))}}
	in.Model = model
	if _, err := PrepareDocumentTemplate(context.Background(), in); err != nil || len(model.calls) != 1 {
		t.Errorf("calls = %d, %v", len(model.calls), err)
	}
}

// "bỏ qua" builds from the previous turn's draft: no new draft that could
// drop what it held. A skip that adds a fact drafts again.
func TestDocumentTemplateSkipReusesTheLastDraft(t *testing.T) {
	model := &scriptedChat{replies: []string{draftJSON(t, settledPlanDraft(fullPlanBody))}}
	in := templateInput(model, "ke-hoach", "Soạn kế hoạch từ nội dung đã trao đổi")
	in.Profile = nil
	in.Conversation = []TemplateTurn{{Role: "user", Text: settledPlanMessage}}
	out, err := PrepareDocumentTemplate(context.Background(), in)
	if err != nil || out.Ready || len(out.Draft) == 0 {
		t.Fatalf("first turn: ready %v, draft %d bytes, %v", out.Ready, len(out.Draft), err)
	}

	// the model would now drop the budget
	model.replies = []string{draftJSON(t, settledPlanDraft(shortPlanBody))}
	model.calls = nil
	in.Inputs = append(in.Inputs, "Bỏ qua, cứ tạo văn bản để tôi tự điền sau")
	in.LastDraft = out.Draft
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if len(model.calls) != 0 {
		t.Fatalf("skip drafted again: %d calls", len(model.calls))
	}
	body, _ := json.Marshal(out.Data["fields"].(map[string]interface{})["paragraphs"])
	if !out.Ready || !strings.Contains(string(body), "50 triệu đồng") {
		t.Fatalf("skip lost content: ready %v %s", out.Ready, body)
	}

	in.Inputs[len(in.Inputs)-1] = "Bỏ qua, nhưng thêm hạn báo cáo 15/11/2026"
	model.calls = nil
	model.replies = []string{draftJSON(t, settledPlanDraft(fullPlanBody+" Hạn báo cáo 15/11/2026."))}
	PrepareDocumentTemplate(context.Background(), in)
	if len(model.calls) == 0 {
		t.Errorf("a skip with a new fact reused the old draft")
	}
}

// The state keeps the last draft between turns.
func TestDocumentTemplateStateKeepsTheLastDraft(t *testing.T) {
	ctx := context.Background()
	draft := json.RawMessage(`{"subject":"Tập huấn","paragraphs":[{"kind":"body","text":"kinh phí 50 triệu đồng"}]}`)
	SaveDocumentTemplateState(ctx, "sess-draft", "req-draft", &DocumentTemplateState{TemplateID: "ke-hoach", LastDraft: draft})
	st := LoadDocumentTemplateState(ctx, "sess-draft", "req-draft")
	if st == nil || string(st.LastDraft) != string(draft) {
		t.Fatalf("last draft not kept: %+v", st)
	}
	raw, _ := json.Marshal(st)
	var back DocumentTemplateState
	if json.Unmarshal(raw, &back) != nil || !strings.Contains(string(back.LastDraft), "50 triệu") {
		t.Fatalf("last draft lost in JSON (Redis): %s", raw)
	}
}
