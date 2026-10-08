package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/types"
)

func plainLines(texts ...string) []profileLine {
	out := make([]profileLine, len(texts))
	for i, t := range texts {
		out[i] = profileLine{Index: i, Text: t}
	}
	return out
}

func sectionTitles(secs []types.DocumentProfileSection) []string {
	out := make([]string, len(secs))
	for i, s := range secs {
		out[i] = fmt.Sprintf("%s [%d-%d]", s.Title, s.From, s.To)
	}
	return out
}

func TestDetectProfileSectionsByArticle(t *testing.T) {
	lines := plainLines(
		"Căn cứ Luật Tổ chức chính quyền địa phương;",
		"Căn cứ Nghị định 30/2020/NĐ-CP;",
		"QUYẾT ĐỊNH:",
		"Điều 1. Ban hành kèm theo Quyết định này Quy chế làm việc.",
		"Điều 2. Quyết định có hiệu lực từ ngày ký.",
		"Điều 3. Chánh Văn phòng và các đơn vị chịu trách nhiệm thi hành.",
		"1. Văn phòng theo dõi việc thực hiện.",
	)
	lines[0].Label, lines[1].Label = "can_cu", "can_cu"
	got := sectionTitles(detectProfileSections(lines))
	want := []string{"Căn cứ [0-1]", "Mở đầu [2-2]", "Điều 1. Ban hành kèm theo Quyết định này Quy chế làm việc. [3-3]",
		"Điều 2. Quyết định có hiệu lực từ ngày ký. [4-4]", "Điều 3. Chánh Văn phòng và các đơn vị chịu trách nhiệm thi hành. [5-6]"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sections:\n got %q\nwant %q", got, want)
	}
}

func TestDetectProfileSectionsPrefersChaptersAndKeepsAppendix(t *testing.T) {
	got := sectionTitles(detectProfileSections(plainLines(
		"Chương I",
		"QUY ĐỊNH CHUNG",
		"Điều 1. Phạm vi điều chỉnh",
		"Điều 2. Đối tượng áp dụng",
		"Chương II",
		"QUY ĐỊNH CỤ THỂ",
		"Mục 1",
		"Điều 3. Nhiệm vụ",
		"PHỤ LỤC",
		"Biểu mẫu báo cáo",
	)))
	want := []string{"Chương I. QUY ĐỊNH CHUNG [0-3]", "Chương II. QUY ĐỊNH CỤ THỂ [4-7]", "PHỤ LỤC. Biểu mẫu báo cáo [8-9]"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sections:\n got %q\nwant %q", got, want)
	}
}

func TestDetectProfileSectionsRomanAndNumbered(t *testing.T) {
	got := sectionTitles(detectProfileSections(plainLines(
		"Thực hiện chỉ đạo của UBND tỉnh, Sở báo cáo như sau:",
		"I. KẾT QUẢ THỰC HIỆN",
		"1. Công tác chỉ đạo",
		"Đã ban hành 12 văn bản.",
		"2. Kết quả cụ thể",
		"II. TỒN TẠI, HẠN CHẾ",
		"III. NHIỆM VỤ THỜI GIAN TỚI",
	)))
	want := []string{"Mở đầu [0-0]", "I. KẾT QUẢ THỰC HIỆN [1-4]", "II. TỒN TẠI, HẠN CHẾ [5-5]", "III. NHIỆM VỤ THỜI GIAN TỚI [6-6]"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sections:\n got %q\nwant %q", got, want)
	}
	// only arabic headings: split by them; a long numbered line is body
	got = sectionTitles(detectProfileSections(plainLines(
		"1. Mục tiêu",
		"Nâng cao chất lượng.",
		"2. Nhiệm vụ "+strings.Repeat("rất dài ", 30),
		"3) Kinh phí",
	)))
	want = []string{"1. Mục tiêu [0-2]", "3) Kinh phí [3-3]"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("numbered sections:\n got %q\nwant %q", got, want)
	}
}

func TestDetectProfileSectionsWithoutHeadings(t *testing.T) {
	var texts []string
	for i := 0; i < 90; i++ {
		texts = append(texts, fmt.Sprintf("Đoạn văn số %d nói về nội dung.", i))
	}
	got := detectProfileSections(plainLines(texts...))
	if len(got) != 3 || got[0].From != 0 || got[0].To != 39 || got[2].From != 80 || got[2].To != 89 ||
		!strings.HasPrefix(got[1].Title, "Đoạn văn số 40") {
		t.Fatalf("blocks of 40 lines expected, got %q", sectionTitles(got))
	}
	short := detectProfileSections(plainLines("Một.", "Hai."))
	if len(short) != 1 || short[0].Title != "Nội dung" || short[0].To != 1 {
		t.Fatalf("a short text is one section, got %q", sectionTitles(short))
	}
	// header and signature lines belong to no section
	lines := plainLines("SỞ NỘI VỤ", "Nội dung chính.", "GIÁM ĐỐC")
	lines[0].Label, lines[2].Label = "co_quan_ban_hanh", "chuc_danh"
	if got := sectionTitles(detectProfileSections(lines)); len(got) != 1 || got[0] != "Nội dung [1-1]" {
		t.Fatalf("labels: %q", got)
	}
}

func TestSectionsFromHeaders(t *testing.T) {
	got := sectionTitles(sectionsFromHeaders([]string{"", "Chương I > Điều 1", "Chương I > Điều 2", "Chương II > Điều 5", "Chương II"}))
	want := []string{"Mở đầu [0-0]", "Chương I [1-2]", "Chương II [3-4]"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
	// markdown breadcrumbs: the document title is shared, the next depth splits
	got = sectionTitles(sectionsFromHeaders([]string{"# Báo cáo\n## A", "# Báo cáo\n## A", "# Báo cáo\n## B"}))
	if strings.Join(got, "|") != "A [0-1]|B [2-2]" {
		t.Fatalf("markdown headers: %q", got)
	}
	if sectionsFromHeaders([]string{"X", "X"}) != nil || sectionsFromHeaders([]string{"", ""}) != nil {
		t.Fatal("headers that do not vary give no sections")
	}
}

func quyetDinhFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../docformat/testdata/parity/fx_qd_so_y_te.docx")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTargetProfileInputReadsTheHeader(t *testing.T) {
	ws := &types.DocumentWorkspace{ID: "ws-1", FileName: "quyet-dinh.docx"}
	in := targetProfileInput(ws, docformat.InspectDocx(quyetDinhFixture(t)))
	if in.ident.Issuer != "SỞ Y TẾ" || in.ident.Date != "05/10/2026" ||
		in.ident.Subject != "Về việc ban hành Kế hoạch triển khai Thông tư số 01/2025/TT-BYT" {
		t.Fatalf("header fields: issuer %q date %q subject %q", in.ident.Issuer, in.ident.Date, in.ident.Subject)
	}
	got := sectionTitles(in.sections)
	want := []string{"Căn cứ [12-13]", "Mở đầu [14-14]", "Điều 1. Ban hành kèm theo Quyết định này Kế hoạch ... [15-15]",
		"Điều 2. Quyết định này có hiệu lực kể từ ngày ký. [16-16]"}
	if strings.Join(got, "|") != strings.Join(want, "|") || in.unit != types.DocumentProfileUnitParagraph {
		t.Fatalf("sections:\n got %q\nwant %q", got, want)
	}
	// the hash is of the text: a formatting-only change keeps it
	same := &docformat.Layout{Paragraphs: docformat.InspectDocx(quyetDinhFixture(t)).Paragraphs}
	for _, p := range same.Paragraphs {
		p.Alignment = "left"
	}
	if targetProfileInput(ws, same).hash() != in.hash() {
		t.Fatal("a formatting-only change must keep the text hash")
	}
	same.Paragraphs[15].Text += " sửa"
	if targetProfileInput(ws, same).hash() == in.hash() {
		t.Fatal("a text change must change the hash")
	}
}

// scriptedCompleter answers each call with the next reply.
type scriptedCompleter struct {
	replies []string
	calls   []string
}

func (s *scriptedCompleter) Complete(_ context.Context, msgs []docformat.Message) (string, error) {
	s.calls = append(s.calls, msgs[len(msgs)-1].Content)
	if len(s.replies) == 0 {
		return "", errors.New("no reply")
	}
	r := s.replies[0]
	s.replies = s.replies[1:]
	return r, nil
}

func TestParseProfileReplyIsLenient(t *testing.T) {
	r, err := parseProfileReply("Đây là hồ sơ:\n```json\n{\"gist\":\"Kế hoạch\",\"key_points\":\"một ý\",\"sections\":{\"2\":\"mục hai\"},\"entities\":{\"figures\":[\"1.250 tỷ\", 12]}}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if r.Gist != "Kế hoạch" || len(r.KeyPoints) != 1 || r.Summaries[2] != "mục hai" || len(r.Entities.Figures) != 2 {
		t.Fatalf("parsed: %+v", r)
	}
	r, err = parseProfileReply(`{"sections":[{"id":"3","summary":"ba"},{"summary":"không số"}]}`)
	if err != nil || r.Summaries[3] != "ba" || r.Summaries[2] != "không số" {
		t.Fatalf("list sections: %+v %v", r, err)
	}
	if _, err := parseProfileReply("không có JSON"); err == nil {
		t.Fatal("prose without JSON must fail")
	}
}

func TestGenerateDocumentProfileFromCongVan(t *testing.T) {
	ws := &types.DocumentWorkspace{ID: "ws-1", FileName: "quyet-dinh.docx"}
	in := targetProfileInput(ws, docformat.InspectDocx(quyetDinhFixture(t)))
	llm := &scriptedCompleter{replies: []string{`{"document_number":"99/XX-YY","issuer":"model","doc_type":"Công văn",
		"gist":"Đề nghị triển khai cải cách hành chính","key_points":["Triển khai CCHC năm 2026","Báo cáo trước 30/11/2026"],
		"sections":[{"n":1,"summary":"Đề nghị và hạn báo cáo"}],"topics":["cải cách hành chính"],
		"typical_questions":["Hạn báo cáo là khi nào?","Ai phải báo cáo?"]}`}}
	p, err := generateDocumentProfile(context.Background(), llm, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.calls) != 1 || !strings.Contains(llm.calls[0], "Số: 1234/QĐ-SYT") || !strings.Contains(llm.calls[0], "[3] Điều 1.") {
		t.Fatalf("one call with the head of the document expected: %q", llm.calls)
	}
	// header parse wins over the model; the invented number is dropped
	if p.DocumentNumber != "1234/QĐ-SYT" || p.Issuer != "SỞ Y TẾ" || p.Date != "05/10/2026" {
		t.Fatalf("identity: %+v", p)
	}
	// the tên loại line wins over the model's type
	if p.DocType != "Quyết định" || p.DocTypeCode != "quyet_dinh" || p.Gist == "" || len(p.TypicalQuestions) != 2 {
		t.Fatalf("model fields: %+v", p)
	}
	if p.Sections[0].Summary != "Đề nghị và hạn báo cáo" || p.Unit != types.DocumentProfileUnitParagraph {
		t.Fatalf("sections: %+v", p.Sections)
	}
}

func TestGenerateDocumentProfileSplitsALongDocumentAndMerges(t *testing.T) {
	var texts []string
	for a := 1; a <= 60; a++ {
		texts = append(texts, fmt.Sprintf("Điều %d. Quy định số %d", a, a))
		for k := 0; k < 3; k++ {
			texts = append(texts, strings.Repeat(fmt.Sprintf("Nội dung chi tiết của điều %d. ", a), 20))
		}
	}
	in := &profileInput{fileName: "quy-che.docx", role: types.DocumentWorkspaceRoleTarget, unit: types.DocumentProfileUnitParagraph,
		lines: plainLines(texts...), text: strings.Join(texts, "\n")}
	in.sections = detectProfileSections(in.lines)
	if len(in.sections) != 60 {
		t.Fatalf("60 articles expected, got %d", len(in.sections))
	}
	llm := &scriptedCompleter{replies: []string{
		`{"gist":"Quy chế","key_points":["a1","a2"],"sections":[{"n":1,"summary":"s1"}],"topics":["t1"],"typical_questions":["q1"]}`,
		`{"gist":"khác","key_points":["b1"],"sections":[{"n":30,"summary":"s30"}],"topics":["t2"],"typical_questions":["q2"]}`,
	}}
	p, err := generateDocumentProfile(context.Background(), llm, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(llm.calls); n != 2 {
		t.Fatalf("this long document takes 2 calls, got %d", n)
	}
	for _, c := range llm.calls {
		if r := len([]rune(c)); r > profileCallRunes {
			t.Fatalf("a call carries %d runes, over the cap", r)
		}
	}
	if p.Gist != "Quy chế" || strings.Join(p.KeyPoints, ",") != "a1,b1,a2" || strings.Join(p.TypicalQuestions, ",") != "q1,q2" ||
		strings.Join(p.Topics, ",") != "t1,t2" {
		t.Fatalf("merge: gist %q points %q questions %q", p.Gist, p.KeyPoints, p.TypicalQuestions)
	}
	if len(p.Sections) != types.DocumentProfileMaxSections || p.Sections[0].Summary != "s1" || p.Sections[29].Summary != "s30" {
		t.Fatal("section summaries are merged by number")
	}
}

func TestGenerateDocumentProfileFailsWithoutAnswer(t *testing.T) {
	in := &profileInput{lines: plainLines("Một."), text: "Một."}
	in.sections = detectProfileSections(in.lines)
	if _, err := generateDocumentProfile(context.Background(), &scriptedCompleter{}, in, nil); err == nil {
		t.Fatal("a model error must fail the profile")
	}
	if _, err := generateDocumentProfile(context.Background(), &scriptedCompleter{}, &profileInput{}, nil); err == nil {
		t.Fatal("an empty document has no profile")
	}
}

func TestProfileDate(t *testing.T) {
	for in, want := range map[string]string{
		"Hà Nội, ngày 5 tháng 3 năm 2024":          "05/03/2024",
		"Thành phố Huế, ngày   tháng 06 năm 2026":  "tháng 06 năm 2026",
		"Thành phố Huế, ngày ... tháng 6 năm 2026": "tháng 06 năm 2026",
		"Phú Hội, ngày      tháng      năm 2026":   "",
		"Thành phố Huế":                            "",
		"ngày 30 tháng 11 năm 2026":                "30/11/2026",
	} {
		if got := profileDate(in); got != want {
			t.Errorf("profileDate(%q) = %q, want %q", in, got, want)
		}
	}
}
