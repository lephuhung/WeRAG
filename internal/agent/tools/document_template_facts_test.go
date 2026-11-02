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
	signer := signerScope(user, "Nguyễn Văn An")
	cases := []struct {
		v        string
		scope    []string
		want     string
		wantPart bool
	}{
		// the model lengthened the title with words of the conversation
		{"Phó Giám đốc Công an tỉnh", signer, "Phó Giám đốc", false},
		{"Giám đốc Công an tỉnh", signer, "Giám đốc", false},
		// the rest is the model's own: nothing is kept
		{"Phó Giám đốc Sở Tài chính", signer, "", false},
		// placeholders are no part of the value
		{"CÔNG AN TỈNH ……………", nil, "CÔNG AN TỈNH", true},
		{"Phòng Tham mưu [...]", nil, "Phòng Tham mưu", true},
		{"Phòng Tham mưu ...", nil, "Phòng Tham mưu", true},
		{"[tên cơ quan]", nil, "", true},
		// a name is never cut ("Nguyễn Văn" of "Nguyễn Văn Anh")
		{"Nguyễn Văn Anh", nil, "", false},
		{"Đại tá Nguyễn Văn An", nil, "Đại tá Nguyễn Văn An", false},
		// a one-word head is no title
		{"Phó Chủ tịch", signer, "", false},
		// a complete form ending in a structural word is a head
		{"Công an tỉnh Thừa Thiên Huế", []string{"Cơ quan ban hành: Công an tỉnh, Phòng Tham mưu."}, "Công an tỉnh", false},
		// a head ending in a bare structural word is no name, nor a part
		// of a longer title ("Phó Giám đốc Sở" goes on)
		{"Phó Giám đốc Sở Tài chính", []string{"Người ký: Nguyễn Văn An, Phó Giám đốc Sở, ký trực tiếp."}, "", false},
	}
	for _, c := range cases {
		got, part := spokenValue(user, user+" Thừa Thiên Huế", c.v, c.scope)
		if got != c.want || part != c.wantPart {
			t.Errorf("spokenValue(%q) = %q, %v; want %q, %v", c.v, got, part, c.want, c.wantPart)
		}
	}
	// the rest must be whole words of the conversation: "an" is not in "thanh"
	if got, _ := spokenValue("Phó Giám đốc, thanh tra", "thanh tra", "Phó Giám đốc An", []string{"Phó Giám đốc, thanh tra"}); got != "" {
		t.Errorf("rest found inside a word: %q", got)
	}
	// the head must end a clause where the user wrote it: "Phó Giám đốc
	// phụ trách đào tạo" is another title
	u := "Người ký: Trần Bình, Phó Giám đốc phụ trách đào tạo, ký trực tiếp."
	if got, _ := spokenValue(u, u+" Công an tỉnh", "Phó Giám đốc Công an tỉnh", signerScope(u, "Trần Bình")); got != "" {
		t.Errorf("head inside a longer title taken: %q", got)
	}
	// the head must be written about the signer
	u = "Phó Giám đốc, anh Bình, phát biểu khai mạc tại Công an tỉnh. Người ký: Nguyễn Văn An, ký trực tiếp."
	if got, _ := spokenValue(u, u, "Phó Giám đốc Công an tỉnh", signerScope(u, "Nguyễn Văn An")); got != "" {
		t.Errorf("another person's title taken for the signer: %q", got)
	}
}

// The clause ends a signer's title may have, the "Chức vụ:" line, and the
// complete forms ending in a structural word.
func TestSpokenValueSignerForms(t *testing.T) {
	cases := []struct{ text, v, want string }{
		{"Người ký: Nguyễn Văn An - Phó Giám đốc - ký thay Giám đốc", "Phó Giám đốc Công an tỉnh", "Phó Giám đốc"},
		{"Người ký Nguyễn Văn An, Phó Giám đốc ký thay Giám đốc", "Phó Giám đốc Công an tỉnh", "Phó Giám đốc"},
		{"Người ký: Nguyễn Văn An\nChức vụ: Phó Giám đốc\nHình thức: ký thay Giám đốc", "Phó Giám đốc Công an tỉnh", "Phó Giám đốc"},
		{"Người ký: Lê Văn Cường, Trưởng phòng, ký trực tiếp.", "Trưởng phòng Tham mưu", "Trưởng phòng"},
		{"Người ký: Lê Văn Cường (Chánh Văn phòng) ký trực tiếp.", "Chánh Văn phòng Công an tỉnh", "Chánh Văn phòng"},
	}
	for _, c := range cases {
		wider := c.text + "\nPhòng Tham mưu chủ trì tại hội trường Công an tỉnh."
		name := "Nguyễn Văn An"
		if !strings.Contains(c.text, name) {
			name = "Lê Văn Cường"
		}
		if got, _ := spokenValue(c.text, wider, c.v, signerScope(c.text, name)); got != c.want {
			t.Errorf("%q: %q → %q, want %q", c.text, c.v, got, c.want)
		}
	}
}

// The acting head's form keeps the head's title (as before the deputy
// check: "Q. GIÁM ĐỐC").
func TestDocumentTemplateActingHeadSigns(t *testing.T) {
	for _, written := range []string{"Người ký: Nguyễn Văn An, Q. Giám đốc.", "Người ký: Nguyễn Văn An, Quyền Giám đốc, ký Q."} {
		d := planDraft()
		d["signer"] = map[string]string{"name": "Nguyễn Văn An", "position": "Giám đốc", "authority": "Q", "on_behalf_of": "Giám đốc"}
		model := &scriptedChat{replies: []string{draftJSON(t, d)}}
		out, _ := PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
			"Tạo kế hoạch đào tạo AI cho cán bộ, 3 lớp tập huấn. "+written))
		if !out.Ready {
			t.Errorf("%q: not ready: %s", written, out.Answer)
			continue
		}
		if sig := strings.Join(out.Data["fields"].(map[string]interface{})["signature_lines"].([]string), "|"); sig != "Q. GIÁM ĐỐC" {
			t.Errorf("%q: signature = %q", written, sig)
		}
	}
}

// A title is not said by a deputy's: "Giám đốc" is not in "Phó Giám đốc".
func TestSaidNotByADeputy(t *testing.T) {
	cases := []struct {
		text, s string
		want    bool
	}{
		{"Người ký: Phó Giám đốc", "Giám đốc", false},
		{"Phó Trưởng phòng ký", "Trưởng phòng", false},
		{"Quyền Giám đốc ký", "Giám đốc", false},
		{"Phó Giám đốc, ký thay Giám đốc", "Giám đốc", true},
		{"Người ký: Phó Giám đốc", "Phó Giám đốc", true},
		{"Giám đốc ký trực tiếp", "Giám đốc", true},
	}
	for _, c := range cases {
		if got := said(c.text, c.s); got != c.want {
			t.Errorf("said(%q, %q) = %v", c.text, c.s, got)
		}
	}
}

// A shorter agency only from what the user wrote of the issuing agency:
// a place named for the event is not the agency.
func TestDocumentTemplateAgencyNotFromAVenue(t *testing.T) {
	d := planDraft()
	d["agency_name"] = "Phòng Tham mưu Công an tỉnh"
	for _, c := range []struct {
		said string
		want bool
	}{
		{"Tập huấn 3 lớp tại hội trường Phòng Tham mưu, Công an tỉnh. Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp", false},
		{"Cơ quan ban hành: Phòng Tham mưu. Tập huấn 3 lớp tại Công an tỉnh. Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp", true},
	} {
		model := &scriptedChat{replies: []string{draftJSON(t, d)}}
		in := templateInput(model, "ke-hoach", c.said)
		in.Profile = nil
		out, _ := PrepareDocumentTemplate(context.Background(), in)
		missing, _ := out.Data["missing_fields"].([]string)
		asked := strings.Contains(strings.Join(missing, "\n"), "Tên cơ quan ban hành")
		if asked == c.want {
			t.Errorf("%q: agency asked = %v", c.said, asked)
		}
	}
}

// A legal ground with a placeholder is no ground the user wrote.
func TestGroundWithPlaceholderNotSaid(t *testing.T) {
	user := "Căn cứ Luật Tổ chức chính quyền địa phương"
	if groundSaid(user, "Căn cứ Luật Tổ chức chính quyền địa phương ……;") {
		t.Errorf("a ground with a placeholder taken")
	}
	if !groundSaid(user, "Căn cứ Luật Tổ chức chính quyền địa phương;") {
		t.Errorf("the user's ground refused")
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
	// the user names Công an tỉnh only as the venue ("tại hội trường Công
	// an tỉnh"): no issuing agency, its header is left blank; said as the
	// issuing agency, what was written stays and the rest is blank
	for _, c := range []struct{ extra, agency string }{
		{"", templateBlank},
		{" Cơ quan ban hành: Công an tỉnh.", "CÔNG AN TỈNH " + templateBlank},
	} {
		model := &scriptedChat{replies: []string{draftJSON(t, d)}}
		in := templateInput(model, "ke-hoach", "Soạn kế hoạch từ nội dung đã trao đổi")
		in.Profile = nil
		in.Conversation = []TemplateTurn{{Role: "user", Text: settledPlanMessage + c.extra}}
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
		if f["agency"] != c.agency {
			t.Errorf("%q: agency = %q, want %q", c.extra, f["agency"], c.agency)
		}
		if sig := strings.Join(f["signature_lines"].([]string), "|"); sig != "KT. GIÁM ĐỐC|PHÓ GIÁM ĐỐC" {
			t.Errorf("signature = %q", sig)
		}
		if f["signer_name"] != "Nguyễn Văn An" {
			t.Errorf("signer = %q", f["signer_name"])
		}
	}
}

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
	// times, phone numbers, fractions are no counts or dates
	if fs := settledFacts(nil, []string{"Họp lúc 8 giờ 30, liên hệ 0912 345 678; 3/4 số cán bộ dự, 14h30 kết thúc."}); len(fs) != 0 {
		t.Errorf("times, phones or fractions taken as facts: %q", factTexts(fs))
	}
}

// A retracted or possibly revised figure is never asked for again.
func TestSettledFactsRevisions(t *testing.T) {
	cases := []struct {
		msgs []string
		want string
	}{
		// "không phải X mà là Y": X is dropped, Y kept
		{[]string{"Kinh phí không phải 50 triệu đồng mà là 60 triệu đồng."}, "60 triệu đồng"},
		{[]string{"Kinh phí 60 triệu đồng chứ không phải 50 triệu đồng."}, "60 triệu đồng"},
		// a later amount may revise the earlier one
		{[]string{"Kinh phí dự kiến 50 triệu.", "Chốt kinh phí 60 triệu."}, "60 triệu"},
		// a later date may revise the earlier ones
		{[]string{"Tổ chức từ 25/10/2026 đến 27/10/2026.", "Bắt đầu 26/10/2026 nhé."}, "26/10/2026"},
		// a count of another noun does not revise
		{[]string{"3 lớp.", "Mỗi lớp 40 học viên."}, "3 lớp|40 học viên"},
		// the retracted figure revises the earlier one, the words that
		// retract it make no count ("35 thay")
		{[]string{"Mỗi lớp 40 học viên.", "Mỗi lớp 35 thay vì 40 học viên."}, ""},
		{[]string{"Mỗi lớp 40 học viên.", "Mỗi lớp 35 học viên thay vì 40."}, "35 học viên"},
		{[]string{"Mỗi lớp 40 học viên.", "Thay vì 40 học viên thì 35."}, ""},
		// two amounts of one sentence, a choice: ambiguous
		{[]string{"Kinh phí 50 triệu đồng, trong đó 30 triệu từ ngân sách."}, ""},
		{[]string{"Mời 40 hoặc 50 học viên."}, ""},
	}
	for _, c := range cases {
		if got := strings.Join(factTexts(settledFacts(nil, c.msgs)), "|"); got != c.want {
			t.Errorf("%q: facts = %q, want %q", c.msgs, got, c.want)
		}
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

// A signer signing KT./TL. for their own title is asked again.
func TestDocumentTemplateSignerNotForTheirOwnTitle(t *testing.T) {
	for _, c := range []struct{ written, authority string }{
		{"Người ký: Nguyễn Văn An, Phó Giám đốc, ký thay Giám đốc.", "KT"},
		{"Người ký: Nguyễn Văn An, Chánh Văn phòng, thừa lệnh Giám đốc.", "TL"},
	} {
		d := planDraft()
		d["signer"] = map[string]string{"name": "Nguyễn Văn An", "position": "Giám đốc", "authority": c.authority, "on_behalf_of": "Giám đốc"}
		model := &scriptedChat{replies: []string{draftJSON(t, d)}}
		out, _ := PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
			"Tạo kế hoạch đào tạo AI cho cán bộ, 3 lớp tập huấn. "+c.written))
		missing, _ := out.Data["missing_fields"].([]string)
		if out.Ready || !strings.Contains(strings.Join(missing, "|"), "Người ký") {
			t.Errorf("%q: %s. GIÁM ĐỐC | GIÁM ĐỐC not asked: %s", c.written, c.authority, out.Answer)
		}
	}
	// the signer's own title stays
	d := planDraft()
	d["signer"] = map[string]string{"name": "Nguyễn Văn An", "position": "Phó Giám đốc", "authority": "KT", "on_behalf_of": "Giám đốc"}
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	out, _ := PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
		"Tạo kế hoạch đào tạo AI cho cán bộ, 3 lớp tập huấn. Người ký: Nguyễn Văn An, Phó Giám đốc, ký thay Giám đốc."))
	if !out.Ready || strings.Join(out.Data["fields"].(map[string]interface{})["signature_lines"].([]string), "|") != "KT. GIÁM ĐỐC|PHÓ GIÁM ĐỐC" {
		t.Errorf("deputy signer refused: %s", out.Answer)
	}
}
