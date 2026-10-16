package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// limitSettings answers the limits.* keys from a map and leaves the rest
// at their defaults.
type limitSettings struct {
	interfaces.SystemSettingService
	values map[string]int64
}

func (s limitSettings) GetInt(_ context.Context, key, _ string, def int64) int64 {
	if v, ok := s.values[key]; ok {
		return v
	}
	return def
}

func testPDF(n int, resources string) []byte {
	var b bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	b.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i))
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	for i := 0; i < n; i++ {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << %s >> >>", resources))
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

const (
	textResources = "/Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >>"
	scanResources = "/XObject << /Im0 << /Type /XObject /Subtype /Image /Width 1 /Height 1 >> >>"
)

func testDOCX(t *testing.T, pages int) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.Create("docProps/app.xml")
	fmt.Fprintf(w, "<Properties><Pages>%d</Pages></Properties>", pages)
	w, _ = zw.Create("word/document.xml")
	_, _ = w.Write([]byte("<w:document/>"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func gateDetails(t *testing.T, err error) types.InputGateDetails {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("want an AppError, got %v", err)
	}
	d, ok := appErr.Details.(types.InputGateDetails)
	if !ok {
		t.Fatalf("want gate details, got %#v", appErr.Details)
	}
	return d
}

// Chat attachments are capped tightly (20 text / 6 scan / 15 docx pages by
// default); the same files pass the wider knowledge-base caps.
func TestInputGateChatCapsAreTighterThanKnowledgeBase(t *testing.T) {
	g := newInputGate(nil)
	ctx := context.Background()
	cases := []struct {
		name string
		ext  string
		data []byte
		kind string
		n    int
	}{
		{"text pdf", ".pdf", testPDF(21, textResources), "pdf_text", 21},
		{"scan pdf", ".pdf", testPDF(7, scanResources), "pdf_scan", 7},
		{"docx", ".docx", testDOCX(t, 16), "docx", 16},
	}
	for _, c := range cases {
		err := g.CheckPages(ctx, types.InputGateChat, "a"+c.ext, c.ext, c.data)
		if err == nil {
			t.Fatalf("%s: chat upload of %d pages passed", c.name, c.n)
		}
		d := gateDetails(t, err)
		if d.Reason != types.InputGateReasonTooManyPages || d.Kind != c.kind || d.Pages != c.n {
			t.Fatalf("%s: details %+v", c.name, d)
		}
		if !strings.Contains(err.Error(), "vượt giới hạn") {
			t.Fatalf("%s: message %q", c.name, err.Error())
		}
		if err := g.CheckPages(ctx, types.InputGateKnowledgeBase, "a"+c.ext, c.ext, c.data); err != nil {
			t.Fatalf("%s: knowledge-base upload refused: %v", c.name, err)
		}
	}
}

func TestInputGateAtTheCapPasses(t *testing.T) {
	g := newInputGate(nil)
	if err := g.CheckPages(context.Background(), types.InputGateChat, "a.pdf", ".pdf", testPDF(20, textResources)); err != nil {
		t.Fatal(err)
	}
	if err := g.CheckPages(context.Background(), types.InputGateChat, "a.pdf", ".pdf", testPDF(6, scanResources)); err != nil {
		t.Fatal(err)
	}
}

// A setting changes the cap at once; 0 disables it.
func TestInputGateReadsTheSettings(t *testing.T) {
	ctx := context.Background()
	g := newInputGate(limitSettings{values: map[string]int64{
		types.SettingLimitsChatPDFTextPages: 2,
		types.SettingLimitsKBPDFTextPages:   0,
	}})
	err := g.CheckPages(ctx, types.InputGateChat, "a.pdf", ".pdf", testPDF(3, textResources))
	if d := gateDetails(t, err); d.Limit != 2 {
		t.Fatalf("details %+v", d)
	}
	if err := g.CheckPages(ctx, types.InputGateKnowledgeBase, "a.pdf", ".pdf", testPDF(900, textResources)); err != nil {
		t.Fatalf("cap 0 must disable the gate: %v", err)
	}
}

// Formats without pages and files that cannot be measured are left to the parser.
func TestInputGateSkipsWhatItCannotMeasure(t *testing.T) {
	g := newInputGate(nil)
	ctx := context.Background()
	for _, c := range []struct {
		ext  string
		data []byte
	}{
		{".png", bytes.Repeat([]byte{1}, 100)},
		{".pdf", []byte("not a pdf at all")},
		{".docx", []byte("not a zip")},
	} {
		if err := g.CheckPages(ctx, types.InputGateChat, "a"+c.ext, c.ext, c.data); err != nil {
			t.Fatalf("%s: %v", c.ext, err)
		}
	}
}

func TestParsedTextTooThin(t *testing.T) {
	if !ParsedTextTooThin("") || !ParsedTextTooThin("![scan](img://1)\n\n![scan](img://2)") || !ParsedTextTooThin("Trang 1") {
		t.Fatal("empty, image-only and a few runes must count as thin")
	}
	if ParsedTextTooThin(strings.Repeat("Điều 1. Phạm vi điều chỉnh. ", 5)) {
		t.Fatal("real text is not thin")
	}
}

// failingVLM errors on every call, as an unreachable OCR service does.
type failingVLM struct{ fakeVLM }

func (f *failingVLM) Predict(context.Context, [][]byte, string) (string, error) {
	return "", errors.New("dial tcp: lookup hrag-vllm-ocr: server misbehaving")
}

// When every OCR call fails, the upload fails as a service problem, not as
// an unreadable scan; when OCR works, nothing is reported.
func TestImageUnderstandingReportsAnOCROutage(t *testing.T) {
	svc := &temporaryDocumentService{modelService: &fakeVLMModelService{model: &failingVLM{}}}
	options := types.TemporaryDocumentCreateOptions{VLMModelID: "fake", ImageUnderstanding: true}
	pages := [][]byte{[]byte("page-1"), []byte("page-2")}
	text, failed := svc.imageUnderstanding(context.Background(), "pdf", options, nil, pages, "![p](x)")
	if text != "" || !failed {
		t.Fatalf("text=%q failed=%v, want an OCR outage", text, failed)
	}
	if msg := noTextMessage("pdf", "ocr_error"); !strings.Contains(msg, "OCR") {
		t.Fatalf("outage message %q", msg)
	}

	svc = &temporaryDocumentService{modelService: &fakeVLMModelService{model: &fakeVLM{}}}
	text, failed = svc.imageUnderstanding(context.Background(), "pdf", options, nil, pages, "![p](x)")
	if text == "" || failed {
		t.Fatalf("text=%q failed=%v, want OCR text and no outage", text, failed)
	}
}
