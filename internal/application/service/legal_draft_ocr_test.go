package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
	"github.com/Tencent/WeKnora/internal/types"
)

var loopPage = "40. Hồ sơ trình phê duyệt | Thời hạn\nTHỐ SƠ TRÀ THẢO " + strings.Repeat("TẾ ", 300)

const tablePage = `| STT | Tên nhóm hồ sơ, tài liệu | Thời hạn lưu trữ |
|---|---|---|
| 38 | Hồ sơ tổ chức hội nghị tổng kết | Vĩnh viễn |
| 39 | Hồ sơ hội nghị sơ kết | 10 năm |
| 40 | Hồ sơ trình phê duyệt nhiệm vụ lập quy hoạch | Vĩnh viễn |
| 41 | Hồ sơ thẩm định quy hoạch | Vĩnh viễn |
| 42 | Hồ sơ công bố quy hoạch | Vĩnh viễn |
| 43 | Báo cáo thống kê định kỳ | 0 | 0 | 0 | 0 | 0 | 0 | 0 |`

func TestIsDegenerateOCRPage(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		want bool
	}{
		"one token looping":          {loopPage, true},
		"table page":                 {tablePage, false},
		"short legit repetition":     {"Tính Tính toán chi phí theo quy định tại Điều 5.", false},
		"five in a row is not a run": {"a " + strings.Repeat("TẾ ", 5) + "b", false},
		"case and punctuation":       {"tế, Tế. TẾ; tế tế TẾ!", true},
		"low distinct ratio": {strings.Repeat("hồ sơ lưu trữ hồ sơ lưu trữ ", 12) +
			"hồ sơ lưu trữ", true},
		"short low ratio page": {"hồ sơ hồ sơ hồ sơ", false},
		"empty":                {"", false},
	} {
		if got := IsDegenerateOCRPage(tc.text); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
	md := "<!-- page 1 -->\n\nĐiều 1. Phạm vi\n\n<!-- page 6 -->\n\n" + loopPage + "\n\n<!-- page 7 -->\n\n" + tablePage
	if got := DegenerateOCRPages(md); !reflect.DeepEqual(got, []int{6}) {
		t.Fatalf("pages = %v", got)
	}
	if got := CollapseOCRRepetition("Bảng: THẢO TẾ TẾ TẾ TẾ TẾ TẾ TẾ hết. Tính Tính"); got != "Bảng: THẢO TẾ … hết. Tính Tính" {
		t.Fatalf("collapse = %q", got)
	}
}

func TestRepairDegeneratePages(t *testing.T) {
	md := "<!-- page 1 -->\n\nĐiều 1. Phạm vi\n\n<!-- page 2 -->\n\n" + loopPage + "\n\n<!-- page 3 -->\n\nĐầu bảng " +
		strings.Repeat("TẾ ", 50) + "\n\n<!-- page 4 -->\n\nĐiều 4. Hiệu lực"
	var rendered []int
	svc := &LegalDraftService{
		PageImages: func(_ context.Context, pdf []byte, _ string, pages []int) (map[int][]byte, error) {
			rendered = pages
			return map[int][]byte{2: []byte("img2"), 3: []byte("img3")}, nil
		},
		ImageOCR: func(_ context.Context, img []byte, page int) (string, error) {
			if page == 2 {
				return "40. Hồ sơ trình phê duyệt nhiệm vụ lập quy hoạch | Vĩnh viễn", nil
			}
			return strings.Repeat("TẾ ", 40), nil // still looping
		},
	}
	out, report := svc.repairDegeneratePages(context.Background(), md, []byte("%PDF"), "x.pdf")
	if !reflect.DeepEqual(rendered, []int{2, 3}) || !reflect.DeepEqual(report.PagesReOCR, []int{2, 3}) ||
		!reflect.DeepEqual(report.DegeneratePages, []int{3}) {
		t.Fatalf("rendered %v report %+v", rendered, report)
	}
	if !strings.Contains(out, "<!-- page 2 -->\n\n40. Hồ sơ trình phê duyệt nhiệm vụ lập quy hoạch | Vĩnh viễn\n\n<!-- page 3 -->") ||
		!strings.Contains(out, "Đầu bảng TẾ …") || strings.Contains(out, "TẾ TẾ") ||
		!strings.Contains(out, "<!-- page 1 -->\n\nĐiều 1. Phạm vi") || !strings.HasSuffix(out, "Điều 4. Hiệu lực") {
		t.Fatalf("repaired markdown:\n%s", out)
	}

	// rendering fails: every bad page keeps its text, loops cut
	svc.PageImages = func(context.Context, []byte, string, []int) (map[int][]byte, error) {
		return nil, errors.New("docreader down")
	}
	out, report = svc.repairDegeneratePages(context.Background(), md, nil, "x.pdf")
	if !reflect.DeepEqual(report.DegeneratePages, []int{2, 3}) || strings.Contains(out, "TẾ TẾ") {
		t.Fatalf("render failure: %+v\n%s", report, out)
	}
	// a clean first pass is left as it is
	clean := "<!-- page 1 -->\n\nĐiều 1. Phạm vi"
	if out, report = svc.repairDegeneratePages(context.Background(), clean, nil, "x.pdf"); out != clean ||
		len(report.PagesReOCR) != 0 || report.DegeneratePages == nil {
		t.Fatalf("clean: %+v %q", report, out)
	}
}

func TestLegalDraftPageWarningDocxStructureHTML(t *testing.T) {
	md := "<!-- page 1 -->\n\nChương I\n\nQUY ĐỊNH CHUNG\n\nĐiều 1. Phạm vi\n\nNội dung.\n\nĐiều 2. Đối tượng\n\n" +
		"<!-- page 2 -->\n\nBảng THẢO TẾ …\n\nĐiều 3. Hiệu lực\n\n1. Có hiệu lực."
	paras := LegalDraftParagraphsFromOCR(md, []int{2})
	var styles []string
	for _, p := range paras {
		styles = append(styles, p.Style+"|"+p.Text)
	}
	joined := strings.Join(styles, "\n")
	if !strings.Contains(joined, "PageMarker|[Trang 2]\nPageWarning|[OCR hỏng ở trang 2 — nhập tay từ bản PDF]\n") ||
		strings.Contains(joined, "PageWarning|[OCR hỏng ở trang 1") {
		t.Fatalf("paragraphs:\n%s", joined)
	}
	data, err := docxwrite.Build(docxwrite.Document{Paragraphs: paras})
	if err != nil {
		t.Fatal(err)
	}
	read, err := ReadLegalDraftParagraphs(data)
	if err != nil {
		t.Fatal(err)
	}
	warned := 0
	for _, p := range read {
		if p.WarningPage == 2 {
			warned++
		}
	}
	if warned != 1 {
		t.Fatalf("warning lines read back: %d", warned)
	}
	report := AnalyzeLegalDraft(read)
	if report.Ready || !hasWarning(report, "ocr_degenerate", "trang 2", true) || len(report.Articles) != 3 {
		t.Fatalf("report = %+v", report.Warnings)
	}
	if html := string(BuildLegalDraftHTML(read)); strings.Contains(html, "OCR hỏng") || strings.Contains(html, "Trang 2") ||
		!strings.Contains(html, `<p class="prov-content">Bảng THẢO TẾ …</p>`) {
		t.Fatalf("html:\n%s", html)
	}

	// the reviewer deleted the red line but left the loop: still told, not blocked
	var kept []LegalDraftParagraph
	for _, p := range read {
		if p.WarningPage != 0 {
			continue
		}
		if p.Text == "Bảng THẢO TẾ …" {
			p.Text = "Bảng THẢO " + strings.Repeat("TẾ ", 8)
		}
		kept = append(kept, p)
	}
	report = AnalyzeLegalDraft(kept)
	if !report.Ready || hasWarning(report, "ocr_degenerate", "", true) || !hasWarning(report, "ocr_repetition", "lặp lại", false) {
		t.Fatalf("after deleting the warning: %+v", report.Warnings)
	}
}

func hasWarning(r *types.LegalDraftStructure, code, text string, blocking bool) bool {
	for _, w := range r.Warnings {
		if w.Code == code && strings.Contains(w.Message, text) && w.Blocking == blocking {
			return true
		}
	}
	return false
}

func TestLegalDraftFetchRepairsLoopingPage(t *testing.T) {
	svc, _, _, ws, _, _, _ := newDraftTestService(t)
	ctx := superAdminCtx()
	svc.OCR = func(context.Context, []byte, string) (string, int, string, error) {
		return strings.Replace(draftOCRText, "Chương II", "Chương II\n\nĐầu bảng "+strings.Repeat("TẾ ", 60), 1), 2, "vietnamese_legal", nil
	}
	svc.PageImages = func(context.Context, []byte, string, []int) (map[int][]byte, error) {
		return map[int][]byte{2: []byte("img")}, nil
	}
	svc.ImageOCR = func(context.Context, []byte, int) (string, error) { return strings.Repeat("TẾ ", 20), nil }
	draft, err := svc.FetchDocument(ctx, "3003", "mcp:ep")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(ctx, draft.ID)
	var report LegalOCRReport
	if err := json.Unmarshal(got.OCRReport, &report); err != nil || !reflect.DeepEqual(report.PagesReOCR, []int{2}) ||
		!reflect.DeepEqual(report.DegeneratePages, []int{2}) || strings.Contains(got.OCRMarkdown, "TẾ TẾ") {
		t.Fatalf("draft = %s %v\n%s", got.OCRReport, err, got.OCRMarkdown)
	}
	if !strings.Contains(string(got.OCRReport), `"ocr_pages_reocr":[2]`) {
		t.Fatalf("report json = %s", got.OCRReport)
	}
	if _, err := svc.Open(ctx, draft.ID, types.LegalDraftViewer{UserID: "u-super"}); err != nil {
		t.Fatal(err)
	}
	read, _ := ReadLegalDraftParagraphs(ws.data)
	report2 := AnalyzeLegalDraft(read)
	if !hasWarning(report2, "ocr_degenerate", "trang 2", true) || report2.Ready {
		t.Fatalf("structure = %+v", report2.Warnings)
	}
}
