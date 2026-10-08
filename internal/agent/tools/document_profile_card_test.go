package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

func readyProfile(ws *types.DocumentWorkspace, sections int) *types.DocumentProfile {
	p := &types.DocumentProfile{
		Status: types.DocumentProfileReady, TextHash: "h", Role: types.DocumentRoleOf(ws), Revision: ws.Revision, SaveCount: ws.SaveCount,
		DocType: "Quyết định", DocumentNumber: "1234/QĐ-SYT", Issuer: "SỞ Y TẾ", Date: "05/10/2026",
		Gist: "Ban hành kế hoạch triển khai Thông tư 01/2025/TT-BYT", Unit: types.DocumentProfileUnitParagraph,
		KeyPoints: []string{"Ban hành kế hoạch", "Hiệu lực từ ngày ký", "Giao Văn phòng theo dõi", "Ý thứ tư"},
	}
	for i := 0; i < sections; i++ {
		p.Sections = append(p.Sections, types.DocumentProfileSection{Title: fmt.Sprintf("Điều %d. Nội dung %d", i+1, i+1), From: 10 + 2*i, To: 11 + 2*i})
	}
	return p
}

func TestDocumentCardRendersTheProfile(t *testing.T) {
	freshDocProfiles(t)
	ws := twoDocWorkspace(t)
	docs, _ := ws.List(context.Background(), 7, "s-1")
	docProfiles.storeState(toolCtx(), "ws-1", readyProfile(docs[0], 4))

	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "")
	for _, want := range []string{
		"- vb1 · cong-van.docx (văn bản làm việc, tab đang xem)\n  Quyết định · số 1234/QĐ-SYT · SỞ Y TẾ · ngày 05/10/2026\n",
		"  Nội dung: Ban hành kế hoạch triển khai Thông tư 01/2025/TT-BYT\n",
		"  Ý chính: Ban hành kế hoạch; Hiệu lực từ ngày ký; Giao Văn phòng theo dõi\n",
		"  Mục (đoạn): Điều 1. Nội dung 1 [10–11]; Điều 2. Nội dung 2 [12–13]; Điều 3. Nội dung 3 [14–15]; Điều 4. Nội dung 4 [16–17]\n",
		"- vb2 · to-trinh.docx (văn bản làm việc) (hồ sơ đang được lập)\n",
		"read_document_outline document=vbN from=<from>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Ý thứ tư") {
		t.Fatal("a card carries three key points")
	}
	// the full text of the targets is still injected
	if !strings.Contains(got, `handle="vb1"`) || !strings.Contains(got, `handle="vb2"`) {
		t.Fatalf("cards do not change which texts are injected:\n%s", got)
	}

	// a turn that finds the card older than the document flags it and runs
	// the planned refresh now
	clock := &manualTimers{now: time.Now()}
	clock.install(docProfileSchedule)
	old := readyProfile(docs[0], 1)
	old.Revision--
	docProfiles.storeState(toolCtx(), "ws-1", old)
	ran := make(chan struct{})
	ScheduleDocumentProfileRefresh(toolCtx(), docs[0], func() { close(ran) })
	got = BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "")
	if !strings.Contains(got, "- vb1 · cong-van.docx (văn bản làm việc, tab đang xem) (đã sửa sau lần đọc)\n") {
		t.Fatalf("stale card:\n%s", got)
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the stale card's refresh was not run now")
	}
}

func TestDocumentCardCapsSectionsAndLength(t *testing.T) {
	ws := &types.DocumentWorkspace{ID: "ws-x", FileName: "quy-che.docx", Position: 1, Revision: 2}
	p := readyProfile(ws, 30)
	card := documentCard("- "+ws.Label()+" (văn bản làm việc)", ws, p)
	if n := utf8.RuneCountInString(card); n > documentCardRunes {
		t.Fatalf("card is %d runes:\n%s", n, card)
	}
	if !strings.Contains(card, "… (30 mục)") || strings.Contains(card, "Điều 11.") {
		t.Fatalf("a long section list is cut:\n%s", card)
	}
	few := documentCard("- x", ws, readyProfile(ws, 12))
	if strings.Contains(few, "…") || !strings.Contains(few, "Điều 12.") {
		t.Fatalf("twelve sections are listed in full:\n%s", few)
	}

	// edited since the card: flagged, key points dropped while it refreshes
	ws.SaveCount++
	stale := documentCard("- x", ws, readyProfile(&types.DocumentWorkspace{Revision: 2}, 2))
	if !strings.Contains(stale, "- x (đã sửa sau lần đọc)\n") {
		t.Fatalf("stale card:\n%s", stale)
	}
	// a failed profile without content adds nothing; a failed source text neither
	if got := documentCard("- x", ws, &types.DocumentProfile{Status: types.DocumentProfileFailed}); got != "- x\n" {
		t.Fatalf("failed profile: %q", got)
	}
	src := &types.DocumentWorkspace{Role: types.DocumentWorkspaceRoleSource, TextStatus: types.DocumentSourceTextFailed}
	if got := documentCard("- y", src, nil); got != "- y\n" {
		t.Fatalf("unreadable source: %q", got)
	}
}

func TestDocumentCardOfASourceUsesItsChunks(t *testing.T) {
	freshDocProfiles(t)
	ws := newSourceWorkspace(t)
	src, _ := ws.Get(context.Background(), 7, "s-1", "ws-src")
	p := readyProfile(src, 2)
	p.Unit, p.DocType, p.DocumentNumber = types.DocumentProfileUnitChunk, "Báo cáo", "15/BC-STC"
	docProfiles.storeState(toolCtx(), "ws-src", p)
	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "")
	if !strings.Contains(got, "- vb2 · bao-cao.pdf (tài liệu nguồn, chỉ tra cứu, pdf, 2.0 MB; tra cứu bằng read_document_outline document=vb2)\n  Báo cáo · số 15/BC-STC") ||
		!strings.Contains(got, "  Mục (chunk): ") {
		t.Fatalf("source card:\n%s", got)
	}
	if strings.Contains(got, `handle="vb2"`) {
		t.Fatal("a source is still never injected")
	}
}
