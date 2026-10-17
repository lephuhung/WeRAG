package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// scriptedChat answers the template draft calls in turn.
type scriptedChat struct {
	chatBase
	replies []string
	calls   [][]chat.Message
}

func (s *scriptedChat) Chat(_ context.Context, msgs []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	s.calls = append(s.calls, msgs)
	r := s.replies[min(len(s.calls), len(s.replies))-1]
	return &types.ChatResponse{Content: r}, nil
}

func (s *scriptedChat) GetModelName() string { return "test" }

func draftJSON(t *testing.T, d map[string]any) string {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return "```json\n" + string(b) + "\n```"
}

var templateToday = time.Date(2026, 2, 5, 9, 0, 0, 0, time.UTC)

func huếProfile() *types.TenantAgencyProfile {
	return &types.TenantAgencyProfile{
		ParentAgency: "UBND tỉnh Thừa Thiên Huế", AgencyName: "Sở Nội vụ", AgencyAbbr: "SNV", Locality: "Huế",
		HeadTitle: "Giám đốc", DefaultRecipients: types.StringArray{"- UBND tỉnh (để b/c);"},
	}
}

func planDraft() map[string]any {
	return map[string]any{
		"subject": "Đào tạo kỹ năng ứng dụng AI cho cán bộ",
		"paragraphs": []map[string]string{
			{"kind": "heading", "text": "I. MỤC ĐÍCH, YÊU CẦU"},
			{"kind": "body", "text": "Nâng cao kỹ năng ứng dụng AI cho cán bộ trong quý I."},
			{"kind": "heading", "text": "II. NỘI DUNG"},
			{"kind": "body", "text": "Tổ chức 3 lớp tập huấn."},
		},
		"recipients": []string{"Các phòng thuộc Sở", "- Lưu: VT"},
		"signer":     map[string]string{"name": "Nguyễn Văn A", "position": "Giám đốc", "authority": "truc_tiep"},
	}
}

func templateInput(model chat.Chat, id string, inputs ...string) DocumentTemplateInput {
	return DocumentTemplateInput{
		Request: &types.DocumentTemplateRequest{SchemaVersion: 1, RequestID: "req-1", TemplateID: id, DocumentID: "doc-1"},
		Profile: huếProfile(), User: TemplateUserContext{Unit: "Văn phòng", UnitAbbr: "VP"},
		Inputs: inputs, Today: templateToday, Model: model,
	}
}

func TestDocumentTemplateReady(t *testing.T) {
	model := &scriptedChat{replies: []string{draftJSON(t, planDraft())}}
	out, err := PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
		"Tạo cho tôi văn bản kế hoạch theo NĐ 30, thông tin tôi cần là: đào tạo AI cho cán bộ trong quý I, 3 lớp tập huấn",
		"Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp"))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Ready || out.Data["status"] != "ready" || len(model.calls) != 1 {
		t.Fatalf("not ready: %+v (calls %d)", out.Data, len(model.calls))
	}
	f := out.Data["fields"].(map[string]interface{})
	want := map[string]interface{}{
		"agency": "SỞ NỘI VỤ", "parent_agency": "UBND TỈNH THỪA THIÊN HUẾ", "locality": "Huế",
		"date": "ngày 05 tháng 02 năm 2026", "number_symbol": "      /KH-SNV", "signer_name": "Nguyễn Văn A",
		"subject": "Đào tạo kỹ năng ứng dụng AI cho cán bộ",
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("%s = %q, want %q", k, f[k], v)
		}
	}
	rec := f["recipients"].([]string)
	if strings.Join(rec, "|") != "- Các phòng thuộc Sở;|- Lưu: VT, VP." {
		t.Errorf("recipients: %q", rec)
	}
	if sig := f["signature_lines"].([]string); len(sig) != 1 || sig[0] != "GIÁM ĐỐC" {
		t.Errorf("signature: %q", sig)
	}
	for _, k := range []string{"schema_version", "request_id", "document_id", "template_id"} {
		if out.Data[k] == nil {
			t.Errorf("payload lacks %s", k)
		}
	}
	// the server's values reach the model; the date and the agency are not
	// left to it
	prompt := model.calls[0][1].Content
	if !strings.Contains(prompt, "Sở Nội vụ") || !strings.Contains(prompt, "Văn phòng (VP)") {
		t.Errorf("prompt lacks the profile or the drafting unit:\n%s", prompt)
	}
}

func TestDocumentTemplateAsksForTheSigner(t *testing.T) {
	d := planDraft()
	// the model chose a signer and a form of signing nobody named
	d["signer"] = map[string]string{"name": "Trần Văn B", "position": "Phó Giám đốc", "authority": "KT", "on_behalf_of": "Giám đốc"}
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	out, err := PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
		"Tạo kế hoạch đào tạo AI cho cán bộ trong quý I, 3 lớp tập huấn"))
	if err != nil {
		t.Fatal(err)
	}
	missing, _ := out.Data["missing_fields"].([]string)
	if out.Ready || out.Data["status"] != "needs_input" || out.Data["fields"] != nil {
		t.Fatalf("ready without a stated signer: %+v", out.Data)
	}
	all := strings.Join(missing, "\n")
	if !strings.Contains(all, "Người ký") || !strings.Contains(all, "Hình thức ký") {
		t.Fatalf("missing: %q", missing)
	}
	if !strings.Contains(out.Answer, "Người ký") {
		t.Fatalf("answer: %s", out.Answer)
	}
}

func TestDocumentTemplateSigningForms(t *testing.T) {
	cases := []struct {
		said, authority, behalf string
		want                    []string
	}{
		{"Phó Giám đốc Trần Văn B ký thay Giám đốc", "KT", "", []string{"KT. GIÁM ĐỐC", "PHÓ GIÁM ĐỐC"}},
		{"Chánh Văn phòng Trần Văn B ký, TL. Giám đốc", "TL", "Giám đốc", []string{"TL. GIÁM ĐỐC", "CHÁNH VĂN PHÒNG"}},
		{"Trần Văn B, Phó Giám đốc phụ trách, ký Q. Giám đốc ", "Q", "", []string{"Q. GIÁM ĐỐC"}},
	}
	for _, c := range cases {
		d := planDraft()
		pos := map[string]string{"KT": "Phó Giám đốc", "TL": "Chánh Văn phòng", "Q": "Phó Giám đốc"}[c.authority]
		d["signer"] = map[string]string{"name": "Trần Văn B", "position": pos, "authority": c.authority, "on_behalf_of": c.behalf}
		model := &scriptedChat{replies: []string{draftJSON(t, d)}}
		out, err := PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
			"Kế hoạch đào tạo AI cho cán bộ trong quý I, 3 lớp tập huấn", c.said))
		if err != nil || !out.Ready {
			t.Fatalf("%s: not ready: %+v %v", c.said, out.Data, err)
		}
		got := out.Data["fields"].(map[string]interface{})["signature_lines"].([]string)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: signature %q, want %q", c.said, got, c.want)
		}
	}
}

func TestDocumentTemplateRefusesInventedNumbers(t *testing.T) {
	d := planDraft()
	d["paragraphs"] = []map[string]string{{"kind": "body", "text": "Hoàn thành trước ngày 15 tháng 3, kinh phí 200 triệu đồng."}}
	model := &scriptedChat{replies: []string{draftJSON(t, d)}} // the retry repeats it
	out, err := PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
		"Kế hoạch đào tạo AI cho cán bộ", "Nguyễn Văn A, Giám đốc, ký trực tiếp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(model.calls) != 2 || !strings.Contains(model.calls[1][2].Content, "the number 15") {
		t.Fatalf("the model is asked again once, told what it invented: %d calls", len(model.calls))
	}
	missing := strings.Join(out.Data["missing_fields"].([]string), "\n")
	if out.Ready || !strings.Contains(missing, "15") || !strings.Contains(missing, "200") {
		t.Fatalf("invented facts let through: %+v", out.Data)
	}

	// heading and article numbers and the current year are not facts
	d["paragraphs"] = []map[string]string{{"kind": "heading", "text": "1. Mục tiêu năm 2026"}, {"kind": "body", "text": "Đào tạo cán bộ."}}
	model = &scriptedChat{replies: []string{draftJSON(t, d)}}
	out, _ = PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
		"Kế hoạch đào tạo AI cho cán bộ", "Nguyễn Văn A, Giám đốc, ký trực tiếp"))
	if !out.Ready || len(model.calls) != 1 {
		t.Fatalf("numbering refused: %+v", out.Data)
	}
}

func TestDocumentTemplateDecisionNeedsStatedGrounds(t *testing.T) {
	d := planDraft()
	d["paragraphs"] = []map[string]string{{"kind": "article", "text": "Điều 1. Cử cán bộ đi học"}, {"kind": "body", "text": "Cử ông Lê Văn C đi học."}}
	d["grounds"] = []string{"Căn cứ Nghị định số 30/2020/NĐ-CP;", "Căn cứ Quyết định số 99/QĐ-UBND"}
	d["request_basis"] = "Theo đề nghị của Chánh Văn phòng."
	said := "Quyết định cử ông Lê Văn C đi học. Căn cứ Nghị định số 30/2020/NĐ-CP. Theo đề nghị của Chánh Văn phòng. Nguyễn Văn A, Giám đốc, ký trực tiếp"
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	out, _ := PrepareDocumentTemplate(context.Background(), templateInput(model, "quyet-dinh", said))
	missing := strings.Join(out.Data["missing_fields"].([]string), "\n")
	// Quyết định 99 was never named (and 99 is an invented number)
	if out.Ready || !strings.Contains(missing, "Căn cứ ban hành") {
		t.Fatalf("an unstated ground let through: %+v", out.Data)
	}

	d["grounds"] = []string{"Căn cứ Nghị định số 30/2020/NĐ-CP"}
	model = &scriptedChat{replies: []string{draftJSON(t, d)}}
	out, _ = PrepareDocumentTemplate(context.Background(), templateInput(model, "quyet-dinh", said))
	if !out.Ready {
		t.Fatalf("not ready: %+v", out.Data)
	}
	f := out.Data["fields"].(map[string]interface{})
	if g := f["grounds"].([]string); len(g) != 1 || g[0] != "Căn cứ Nghị định số 30/2020/NĐ-CP;" {
		t.Errorf("grounds: %q", g)
	}
	if f["request_basis"] != "Theo đề nghị của Chánh Văn phòng." || f["decision_authority"] != "GIÁM ĐỐC SỞ NỘI VỤ" ||
		f["number_symbol"] != "      /QĐ-SNV" {
		t.Errorf("decision fields: %+v", f)
	}
}

func TestDocumentTemplateLetterAndProfileConflict(t *testing.T) {
	d := planDraft()
	d["paragraphs"] = []map[string]string{{"kind": "body", "text": "Đề nghị các đơn vị cử cán bộ dự tập huấn./."}}
	d["addressee"] = "Kính gửi: Các phòng thuộc Sở"
	d["locality"] = "Thừa Thiên Huế"
	said := []string{"Công văn gửi các phòng thuộc Sở đề nghị cử cán bộ dự tập huấn, địa danh ghi Thừa Thiên Huế",
		"Nguyễn Văn A, Giám đốc, ký trực tiếp"}
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	in := templateInput(model, "cong-van", said...)
	out, _ := PrepareDocumentTemplate(context.Background(), in)
	missing := strings.Join(out.Data["missing_fields"].([]string), "\n")
	if out.Ready || !strings.Contains(missing, "hồ sơ đơn vị ghi «Huế», bạn ghi «Thừa Thiên Huế»") {
		t.Fatalf("a conflict with the profile is asked: %+v", out.Data)
	}
	// asked once: the user's value stands the next time
	in.Asked = out.Asked
	in.Inputs = append(in.Inputs, "Thừa Thiên Huế")
	model.calls = nil
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if !out.Ready {
		t.Fatalf("not ready after the answer: %+v", out.Data)
	}
	f := out.Data["fields"].(map[string]interface{})
	if f["locality"] != "Thừa Thiên Huế" || f["addressee"] != "Các phòng thuộc Sở" || f["number_symbol"] != "      /SNV-VP" {
		t.Errorf("letter fields: %+v", f)
	}
}

func TestDocumentTemplateWithoutProfileOrMemory(t *testing.T) {
	d := planDraft()
	d["drafting_unit"], d["drafting_unit_abbr"], d["user_position"] = "Phòng Tổ chức", "TC", "Chuyên viên"
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	in := templateInput(model, "ke-hoach", "Kế hoạch đào tạo AI cho cán bộ trong quý I, 3 lớp tập huấn. Tôi là Chuyên viên Phòng Tổ chức (TC).",
		"Nguyễn Văn A, Giám đốc, ký trực tiếp")
	in.Profile, in.User = nil, TemplateUserContext{}
	out, _ := PrepareDocumentTemplate(context.Background(), in)
	missing := strings.Join(out.Data["missing_fields"].([]string), "\n")
	for _, m := range []string{"Tên cơ quan ban hành", "viết tắt của cơ quan", "Địa danh"} {
		if !strings.Contains(missing, m) {
			t.Errorf("missing lacks %q: %s", m, missing)
		}
	}
	if strings.Contains(missing, "Phòng/bộ phận") {
		t.Errorf("the unit the user stated is asked again: %s", missing)
	}
	if out.Learned == nil || *out.Learned != (TemplateUserContext{Unit: "Phòng Tổ chức", UnitAbbr: "TC", Position: "Chuyên viên"}) {
		t.Errorf("learned: %+v", out.Learned)
	}

	// a unit the user never wrote is not learned
	d["drafting_unit"], d["drafting_unit_abbr"], d["user_position"] = "Phòng Kế hoạch", "KH", ""
	model = &scriptedChat{replies: []string{draftJSON(t, d)}}
	in.Model, in.Inputs = model, []string{"Kế hoạch đào tạo AI cho cán bộ"}
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if out.Learned != nil || !strings.Contains(strings.Join(out.Data["missing_fields"].([]string), "\n"), "Phòng/bộ phận soạn thảo") {
		t.Errorf("invented unit: learned %+v, data %+v", out.Learned, out.Data)
	}
}

func TestND30Date(t *testing.T) {
	for in, want := range map[time.Time]string{
		time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC):   "ngày 09 tháng 01 năm 2026",
		time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC):  "ngày 10 tháng 3 năm 2026",
		time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC): "ngày 31 tháng 12 năm 2026",
	} {
		if got := nd30Date(in); got != want {
			t.Errorf("nd30Date(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestDocumentTemplateRequestNormalized(t *testing.T) {
	ok := &types.DocumentTemplateRequest{SchemaVersion: 1, RequestID: " r1 ", TemplateID: "ke-hoach", DocumentID: "d1"}
	if n := ok.Normalized(); n == nil || n.RequestID != "r1" {
		t.Fatalf("normalized: %+v", n)
	}
	for _, bad := range []*types.DocumentTemplateRequest{
		nil,
		{SchemaVersion: 2, RequestID: "r", TemplateID: "ke-hoach", DocumentID: "d"},
		{SchemaVersion: 1, RequestID: "r", TemplateID: "nghi-quyet", DocumentID: "d"},
		{SchemaVersion: 1, RequestID: "", TemplateID: "ke-hoach", DocumentID: "d"},
		{SchemaVersion: 1, RequestID: "r", TemplateID: "ke-hoach", DocumentID: ""},
	} {
		if bad.Normalized() != nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestDocumentTemplateFromTheUsersWorkContext(t *testing.T) {
	d := planDraft()
	said := []string{"Kế hoạch đào tạo AI cho cán bộ trong quý I, 3 lớp tập huấn, viết tắt cơ quan CAT",
		"Nguyễn Văn A, Giám đốc, ký trực tiếp"}
	d["agency_abbr"] = "CAT"
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	in := templateInput(model, "ke-hoach", said...)
	in.Profile, in.WorkUnit, in.Province = nil, "Công an tỉnh", "Hà Tĩnh"
	out, _ := PrepareDocumentTemplate(context.Background(), in)
	missing := strings.Join(out.Data["missing_fields"].([]string), "\n")
	if out.Ready || !strings.Contains(missing, "cơ quan ban hành «CÔNG AN TỈNH HÀ TĨNH», địa danh «Hà Tĩnh»") ||
		strings.Contains(missing, "Tên cơ quan ban hành") || strings.Contains(missing, "Địa danh ghi") {
		t.Fatalf("the work context is put to the user once: %s", missing)
	}
	if !strings.Contains(model.calls[0][1].Content, `work unit "Công an tỉnh", province/city "Hà Tĩnh"`) {
		t.Errorf("the model is not told the work context")
	}
	// confirmed
	in.Asked, in.Inputs = out.Asked, append(in.Inputs, "đúng")
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if !out.Ready {
		t.Fatalf("not ready after confirming: %+v", out.Data)
	}
	f := out.Data["fields"].(map[string]interface{})
	if f["agency"] != "CÔNG AN TỈNH HÀ TĨNH" || f["locality"] != "Hà Tĩnh" || f["parent_agency"] != nil || f["number_symbol"] != "      /KH-CAT" {
		t.Errorf("fields: %+v", f)
	}

	// only a province: the agency is still asked for
	in = templateInput(&scriptedChat{replies: []string{draftJSON(t, planDraft())}}, "ke-hoach", said...)
	in.Profile, in.Province = nil, "Huế"
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	missing = strings.Join(out.Data["missing_fields"].([]string), "\n")
	if !strings.Contains(missing, "Tên cơ quan ban hành") || !strings.Contains(missing, "địa danh «Huế»") {
		t.Fatalf("missing: %s", missing)
	}
}

func TestWorkUnitAgency(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"Công an tỉnh", "Hà Tĩnh"}: "Công an tỉnh Hà Tĩnh",
		{"UBND thành phố", "Huế"}:   "UBND thành phố Huế",
		{"Sở Nội vụ", "Huế"}:        "Sở Nội vụ",
		{"  Phòng  Tư pháp ", ""}:   "Phòng Tư pháp",
		{"", "Huế"}:                 "",
	} {
		if got := workUnitAgency(in[0], in[1]); got != want {
			t.Errorf("workUnitAgency(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestDocumentTemplateLearnsOnlyWhatTheUserSaysOfThemself(t *testing.T) {
	d := planDraft()
	// the model takes the officer the decision is about for the user
	d["drafting_unit"], d["drafting_unit_abbr"], d["user_position"] = "Văn phòng", "VP", "chuyên viên"
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	in := templateInput(model, "ke-hoach",
		"Tạo cho tôi văn bản kế hoạch theo NĐ 30, thông tin tôi cần là: cử bà Lan, chuyên viên Văn phòng (VP), đi học 3 lớp",
		"Nguyễn Văn A, Giám đốc, ký trực tiếp")
	in.User = TemplateUserContext{Unit: "Phòng Tổ chức", UnitAbbr: "TC"}
	out, _ := PrepareDocumentTemplate(context.Background(), in)
	if out.Learned != nil {
		t.Fatalf("learned what the user did not say of themself: %+v", out.Learned)
	}
	if !out.Ready || out.Data["fields"].(map[string]interface{})["recipients"].([]string)[1] != "- Lưu: VT, TC." {
		t.Fatalf("the remembered unit is kept: %+v", out.Data)
	}

	d["drafting_unit"], d["drafting_unit_abbr"], d["user_position"] = "Phòng Tổ chức cán bộ", "TCCB", "Chuyên viên"
	model = &scriptedChat{replies: []string{draftJSON(t, d)}}
	in.Model = model
	in.Inputs = append(in.Inputs, "Tôi là chuyên viên Phòng Tổ chức cán bộ, viết tắt TCCB.")
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if out.Learned == nil || *out.Learned != (TemplateUserContext{Unit: "Phòng Tổ chức cán bộ", UnitAbbr: "TCCB", Position: "Chuyên viên"}) {
		t.Fatalf("learned: %+v", out.Learned)
	}
	// a unit without its abbreviation is not taken
	d["drafting_unit_abbr"] = ""
	model = &scriptedChat{replies: []string{draftJSON(t, d)}}
	in.Model = model
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if out.Learned == nil || out.Learned.Unit != "" || out.Learned.Position != "Chuyên viên" {
		t.Fatalf("learned without an abbreviation: %+v", out.Learned)
	}
}

func TestDocumentTemplateSubjectWithoutVv(t *testing.T) {
	for _, subject := range []string{"về việc báo cáo kết quả", "V/v báo cáo kết quả", "Về việc: báo cáo kết quả."} {
		d := planDraft()
		d["subject"] = subject
		model := &scriptedChat{replies: []string{draftJSON(t, d)}}
		out, _ := PrepareDocumentTemplate(context.Background(), templateInput(model, "ke-hoach",
			"Kế hoạch báo cáo kết quả, 3 lớp", "Nguyễn Văn A, Giám đốc, ký trực tiếp"))
		if got := out.Data["fields"].(map[string]interface{})["subject"]; got != "Báo cáo kết quả" {
			t.Errorf("subject %q → %q", subject, got)
		}
	}
}

func TestTemplateBodyDropsTheFrame(t *testing.T) {
	// a decision as Qwen3.6 wrote it on 2026-10-16: the header, the title,
	// the authority, the grounds and a repeated last paragraph in the body
	d := &templateDraft{Subject: "Về việc cử cán bộ đi học", RequestBasis: "Theo đề nghị của Chánh Văn phòng Sở.",
		Grounds: []string{"Căn cứ Quyết định số 12/2025/QĐ-UBND;"}}
	for _, p := range [][2]string{
		{"heading", "UBND TỈNH"}, {"heading", "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM"}, {"heading", "Độc lập - Tự do - Hạnh phúc"},
		{"heading", "---------------"}, {"heading", "QUYẾT ĐỊNH"}, {"heading", "Về việc cử cán bộ đi học"},
		{"body", "GIÁM ĐỐC SỞ NỘI VỤ"}, {"body", "Căn cứ Quyết định số 12/2025/QĐ-UBND;"}, {"body", "Theo đề nghị của Chánh Văn phòng Sở;"},
		{"body", "Quyết định:"}, {"article", "Điều 1. Cử bà Lan đi học."}, {"body", "Điều 2. Quyết định có hiệu lực kể từ ngày ký.\nChánh Văn phòng chịu trách nhiệm thi hành."},
		{"body", "Chánh Văn phòng chịu trách nhiệm thi hành."},
	} {
		d.Paragraphs = append(d.Paragraphs, struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}{p[0], p[1]})
	}
	got := templateBody(d, "quyet-dinh", "Sở Nội vụ", "UBND tỉnh")
	want := []templateParagraph{{"article", "Điều 1. Cử bà Lan đi học."}, {"article", "Điều 2. Quyết định có hiệu lực kể từ ngày ký."},
		{"body", "Chánh Văn phòng chịu trách nhiệm thi hành."}}
	if len(got) != len(want) {
		t.Fatalf("body: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paragraph %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	// a plan keeps its "Căn cứ" lines: they are content there
	d.Paragraphs = d.Paragraphs[:0]
	d.Paragraphs = append(d.Paragraphs, struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}{"body", "Căn cứ Kế hoạch năm của Sở, Văn phòng xây dựng kế hoạch như sau."})
	if got := templateBody(d, "ke-hoach", "Sở Nội vụ", ""); len(got) != 1 {
		t.Fatalf("a plan's ground removed: %+v", got)
	}
}

func TestTemplateRecipientsNhuTren(t *testing.T) {
	body := []templateParagraph{{"article", "Điều 1. Cử bà Lan."}, {"article", "Điều 3. Thi hành."}}
	defaults := []string{"- UBND tỉnh (để b/c);"}
	for _, c := range []struct {
		id   string
		want string
	}{
		{"cong-van", "- Như trên;|- Lưu: VT, VP."},
		{"quyet-dinh", "- Như Điều 3;|- Lưu: VT, VP."},
		{"ke-hoach", "- UBND tỉnh (để b/c);|- Lưu: VT, VP."},
	} {
		got := strings.Join(templateRecipients(c.id, []string{"- Như trên;"}, defaults, "VP", body), "|")
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.id, got, c.want)
		}
	}
}

func TestTemplateBodySubHeadings(t *testing.T) {
	d := &templateDraft{}
	for _, text := range []string{"1. Đối tượng tham gia", "Cán bộ, công chức của Sở.", "2. Thời gian: trong quý IV.", "a) Lớp thứ nhất"} {
		d.Paragraphs = append(d.Paragraphs, struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}{"body", text})
	}
	got := templateBody(d, "ke-hoach", "Sở Nội vụ", "")
	kinds := []string{got[0].Kind, got[1].Kind, got[2].Kind, got[3].Kind}
	if strings.Join(kinds, ",") != "heading,body,body,heading" {
		t.Fatalf("kinds: %v", kinds)
	}
}

func TestSubjectCase(t *testing.T) {
	for _, c := range [][3]string{
		{"cong-van", "Triển khai kế hoạch", "triển khai kế hoạch"},
		{"cong-van", "UBND tỉnh giao nhiệm vụ", "UBND tỉnh giao nhiệm vụ"},
		{"quyet-dinh", "cử cán bộ đi học", "Cử cán bộ đi học"},
	} {
		if got := subjectCase(c[0], c[1]); got != c[2] {
			t.Errorf("subjectCase(%s, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestDocumentTemplateSkipLeavesBlanks(t *testing.T) {
	d := planDraft()
	// nobody named a signer; the body holds a date the user never wrote
	d["signer"] = map[string]string{}
	d["paragraphs"] = []map[string]string{{"kind": "body", "text": "Hoàn thành trước ngày 15 tháng 3."}}
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	in := templateInput(model, "ke-hoach", "Tạo kế hoạch đào tạo AI cho cán bộ")
	in.Profile, in.User = nil, TemplateUserContext{}
	out, _ := PrepareDocumentTemplate(context.Background(), in)
	if out.Ready || !strings.Contains(out.Answer, "«bỏ qua»") {
		t.Fatalf("first turn: ready %v, answer %s", out.Ready, out.Answer)
	}

	in.Inputs = append(in.Inputs, "Bỏ qua, cứ tạo văn bản để tôi tự điền sau")
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if !out.Ready || out.Data["status"] != "ready" {
		t.Fatalf("skipped turn not ready: %+v", out.Data)
	}
	f := out.Data["fields"].(map[string]interface{})
	want := map[string]interface{}{
		"agency": templateBlank, "locality": templateBlank, "signer_name": templateBlank,
		"number_symbol": "      /KH-" + templateBlank,
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("%s = %q, want %q", k, f[k], v)
		}
	}
	if sig := f["signature_lines"].([]string); len(sig) != 1 || sig[0] != templateBlank {
		t.Errorf("signature: %q", sig)
	}
	body := f["paragraphs"].([]templateParagraph)
	if len(body) != 1 || strings.Contains(body[0].Text, "15") || !strings.Contains(body[0].Text, templateBlank) {
		t.Errorf("invented date kept: %+v", body)
	}
	for _, m := range []string{"Người ký", "Tên cơ quan ban hành", "Các số liệu"} {
		if !strings.Contains(out.Answer, m) {
			t.Errorf("answer does not list %q as blank: %s", m, out.Answer)
		}
	}
	if strings.Contains(out.Answer, "Xác nhận") {
		t.Errorf("answer still asks: %s", out.Answer)
	}
}

func TestDocumentTemplateSkipDecisionAndLetter(t *testing.T) {
	d := planDraft()
	d["signer"] = map[string]string{}
	d["paragraphs"] = []map[string]string{{"kind": "article", "text": "Điều 1. Cử cán bộ đi học."}}
	model := &scriptedChat{replies: []string{draftJSON(t, d)}}
	in := templateInput(model, "quyet-dinh", "Quyết định cử cán bộ đi học", "để trống phần còn thiếu")
	out, _ := PrepareDocumentTemplate(context.Background(), in)
	if !out.Ready {
		t.Fatalf("decision not ready: %s", out.Answer)
	}
	f := out.Data["fields"].(map[string]interface{})
	if g := f["grounds"].([]string); len(g) != 1 || g[0] != "Căn cứ "+templateBlank+";" {
		t.Errorf("grounds: %q", g)
	}
	if f["request_basis"] != "Theo đề nghị của "+templateBlank+"." || f["decision_authority"] == "" {
		t.Errorf("basis %q, authority %q", f["request_basis"], f["decision_authority"])
	}

	model = &scriptedChat{replies: []string{draftJSON(t, d)}}
	in = templateInput(model, "cong-van", "Công văn đề nghị cử cán bộ đi học", "bỏ qua")
	out, _ = PrepareDocumentTemplate(context.Background(), in)
	if !out.Ready || out.Data["fields"].(map[string]interface{})["addressee"] != templateBlank {
		t.Fatalf("letter: %+v", out.Data)
	}
}

func TestWantsBlanksOnlyFromTheCurrentMessage(t *testing.T) {
	if wantsBlanks([]string{"bỏ qua", "Người ký: Nguyễn Văn A"}) {
		t.Error("an earlier skip carried over")
	}
	for _, m := range []string{"Bỏ qua", "cứ tạo đi", "Tạo luôn, tôi điền sau", "không cần hỏi thêm",
		"Skip the missing details: create the document and leave blanks for me to fill in."} {
		if !wantsBlanks([]string{"x", m}) {
			t.Errorf("%q is not a skip", m)
		}
	}
	if wantsBlanks([]string{"Nội dung: đào tạo AI cho cán bộ"}) {
		t.Error("an ordinary answer is a skip")
	}
}
