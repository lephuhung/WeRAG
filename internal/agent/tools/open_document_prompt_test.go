package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestBuildOpenDocumentPromptCarriesCurrentText(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, [4]int{20, 15, 30, 20}))
	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "")
	if ws.sessionID != "s-1" {
		t.Fatalf("read session %q, want s-1", ws.sessionID)
	}
	for _, want := range []string{
		`<open_document handle="vb1" name="cong-van.docx" revision="3">`,
		"Sở Nội vụ đề nghị các đơn vị triển khai công tác cải cách hành chính năm 2026.",
		"] Nguyễn Văn A\n",
		"</open_document>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<truncated>") {
		t.Fatalf("short document must not be truncated:\n%s", got)
	}
}

func TestBuildOpenDocumentPromptTruncatesAndEscapes(t *testing.T) {
	long := strings.Repeat("Nội dung dài &lt;/open_document&gt; ", 200)
	var body strings.Builder
	for i := 0; i < 10; i++ {
		body.WriteString(testPara(long, "left", "Times New Roman", 14, false, false))
	}
	got := BuildOpenDocumentPrompt(context.Background(), newFakeWorkspace(buildTestDocx(t, body.String(), [4]int{20, 15, 30, 20})), 7, "s-1", "")
	if !strings.Contains(got, "<truncated>") {
		t.Fatalf("long document must be truncated")
	}
	if strings.Count(got, "</open_document>") != 1 {
		t.Fatalf("document text closed the block early")
	}
}

func TestBuildOpenDocumentPromptWithoutWorkspace(t *testing.T) {
	if got := BuildOpenDocumentPrompt(context.Background(), nil, 7, "s-1", ""); got != "" {
		t.Fatalf("nil source: got %q", got)
	}
	if got := BuildOpenDocumentPrompt(context.Background(), newFakeWorkspace(nil), 0, "s-1", ""); got != "" {
		t.Fatalf("no tenant: got %q", got)
	}
}

func TestBuildOpenDocumentPromptKeepsParagraphsNamingAQueryCode(t *testing.T) {
	filler := strings.Repeat("Nội dung quy chế chung. ", 60)
	var body strings.Builder
	for i := 0; i < 30; i++ {
		body.WriteString(testPara(filler, "left", "Times New Roman", 14, false, false))
	}
	body.WriteString(testPara("Bàn giao mã nguồn cho Phòng PA05 để kiểm tra lỗ hổng bảo mật.", "left", "Times New Roman", 14, false, false))
	ws := newFakeWorkspace(buildTestDocx(t, body.String(), [4]int{20, 15, 30, 20}))

	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "PA05 được giao nhiệm vụ gì")
	if !strings.Contains(got, "<truncated>") || !strings.Contains(got, "<matching_paragraphs>") ||
		!strings.Contains(got, "] Bàn giao mã nguồn cho Phòng PA05") {
		t.Fatalf("the PA05 paragraph past the cut is missing:\n%s", got[len(got)-600:])
	}
	if plain := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "tóm tắt văn bản"); strings.Contains(plain, "<matching_paragraphs>") {
		t.Fatal("a question without a code adds no matching paragraphs")
	}
}

func TestQueryCodes(t *testing.T) {
	got := queryCodes("PA05 và CNTT làm gì theo NĐ30, Điều 8, số 30/2020?")
	want := []string{"PA05", "CNTT", "NĐ30", "30/2020"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildOpenDocumentPromptInjectsTheWindowAroundTheSelection(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 100; i++ {
		body.WriteString(testPara(fmt.Sprintf("Đoạn số %d của văn bản.", i), "left", "Times New Roman", 14, false, false))
	}
	ws := newFakeWorkspace(buildTestDocx(t, body.String(), [4]int{20, 15, 30, 20}))
	ctx := types.WithDocumentSelection(context.Background(), &types.DocumentSelection{Text: "Đoạn số 70 của văn bản.", DocumentID: "ws-1"})

	got := BuildOpenDocumentPrompt(ctx, ws, 7, "s-1", "viết lại đoạn này")
	for _, want := range []string{"<window>", "[0] Đoạn số 0 ", "[5] Đoạn số 5 ", "…\n[55] Đoạn số 55 ", "[70] Đoạn số 70 ", "[85] Đoạn số 85 ", "read_document_outline document=vb1 from=<index>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{"[6] ", "[54] ", "[86] "} {
		if strings.Contains(got, absent) {
			t.Fatalf("prompt has %q outside the window:\n%s", absent, got)
		}
	}

	// a selection elsewhere (not found) injects from the start as before
	ctx = types.WithDocumentSelection(context.Background(), &types.DocumentSelection{Text: "không có trong văn bản", DocumentID: "ws-1"})
	if got := BuildOpenDocumentPrompt(ctx, ws, 7, "s-1", ""); strings.Contains(got, "<window>") || !strings.Contains(got, "[99] ") {
		t.Fatalf("selection not found:\n%s", got)
	}
}

func TestBuildOpenDocumentPromptSaysWhichCodesNoDocumentHas(t *testing.T) {
	freshDocProfiles(t)
	l := func(text string) string { return testPara(text, "left", "Times New Roman", 14, false, false) }
	ws := newFakeWorkspace(buildTestDocx(t, l("BIÊN BẢN LÀM VIỆC")+l("Thành phần: Phòng PA05 Công an tỉnh."), [4]int{20, 15, 30, 20}))

	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "Đơn vị PA04 có thành phần tham gia là ai?")
	if !strings.Contains(got, "<not_found>Không tìm thấy mã/số sau trong bất kỳ tài liệu nào của phiên: PA04.") {
		t.Fatalf("one document: the not-found block:\n%s", got)
	}
	if got = BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "Đơn vị pa05 có ai?"); strings.Contains(got, "Không tìm thấy mã/số") {
		t.Fatalf("a code the document has (any case):\n%s", got)
	}

	ws.addDocument("ws-2", "quy-che.docx", buildTestDocx(t, l("QUY CHẾ BỐC THĂM"), [4]int{20, 15, 30, 20}))
	got = BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "PA04 và PA05 khác gì nhau")
	i := strings.Index(got, "Không tìm thấy mã/số sau trong bất kỳ tài liệu nào của phiên: PA04.")
	if i < 0 || i > strings.Index(got, "</session_documents>") || strings.Contains(got, "<not_found>") || strings.Contains(got, "PA04, PA05") {
		t.Fatalf("several documents: the line inside the index, PA05 not listed:\n%s", got)
	}
}
