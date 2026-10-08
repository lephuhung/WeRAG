package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// twoDocWorkspace is a session with two open documents: vb1 (ws-1, active)
// and vb2 (ws-2, "to-trinh.docx").
func twoDocWorkspace(t *testing.T) *fakeWorkspace {
	t.Helper()
	ws := newFakeWorkspace(testCongVan(t, [4]int{20, 15, 30, 20}))
	ws.addDocument("ws-2", "to-trinh.docx", testCongVan(t, [4]int{20, 15, 30, 20}))
	return ws
}

func TestResolveDocumentSingleDocumentNeedsNoMention(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, [4]int{20, 15, 30, 20}))
	got, err := resolveDocument(toolCtx(), ws, "s-1", "", true)
	if err != nil || got.ID != "ws-1" {
		t.Fatalf("one document is the target without @: %v, %v", got, err)
	}
}

func TestResolveDocumentEditNeedsTheUserToNameTheDocument(t *testing.T) {
	ws := twoDocWorkspace(t)

	// no @ at all: an edit is refused, a read falls back to the active tab
	if _, err := resolveDocument(toolCtx(), ws, "s-1", "", true); err == nil || !strings.Contains(err.Error(), "@") {
		t.Fatalf("edit without a named document must be refused with a hint about @, got %v", err)
	}
	if got, err := resolveDocument(toolCtx(), ws, "s-1", "", false); err != nil || got.ID != "ws-1" {
		t.Fatalf("read without a target uses the active document: %v, %v", got, err)
	}

	// the model names vb2 but the user only mentioned vb1
	ctx := types.WithMentionedDocuments(toolCtx(), []string{"ws-1"})
	if _, err := resolveDocument(ctx, ws, "s-1", "vb2", true); err == nil || !strings.Contains(err.Error(), "chưa gọi đích danh") {
		t.Fatalf("edit of a document the user did not name must be refused, got %v", err)
	}
	if got, err := resolveDocument(ctx, ws, "s-1", "vb2", false); err != nil || got.ID != "ws-2" {
		t.Fatalf("reading another document needs no @: %v, %v", got, err)
	}

	// one @-mention is the default target
	ctx = types.WithMentionedDocuments(toolCtx(), []string{"ws-2"})
	if got, err := resolveDocument(ctx, ws, "s-1", "", true); err != nil || got.ID != "ws-2" {
		t.Fatalf("the single mentioned document is the edit target: %v, %v", got, err)
	}
	if got, err := resolveDocument(ctx, ws, "s-1", "to-trinh", true); err != nil || got.ID != "ws-2" {
		t.Fatalf("a partial file name names the document: %v, %v", got, err)
	}

	// a selection in vb2 designates it too
	ctx = types.WithDocumentSelection(toolCtx(), &types.DocumentSelection{Text: "đoạn", DocumentID: "ws-2"})
	if got, err := resolveDocument(ctx, ws, "s-1", "", true); err != nil || got.ID != "ws-2" {
		t.Fatalf("the selection's document is the edit target: %v, %v", got, err)
	}

	if _, err := resolveDocument(toolCtx(), ws, "s-1", "vb9", false); err == nil || !strings.Contains(err.Error(), "vb1") {
		t.Fatalf("an unknown document lists the open ones, got %v", err)
	}
}

func TestEditToolRoutesOpsToTheNamedDocument(t *testing.T) {
	ws := twoDocWorkspace(t)
	tool := NewMarkPassagesTool(ws, "s-1")
	args := json.RawMessage(`{"document":"vb2","marks":[{"paragraph":0,"reason":"kiểm tra"}]}`)

	res, err := tool.Execute(toolCtx(), args)
	if err != nil || res.Success {
		t.Fatalf("an edit without @ must fail when two documents are open: %+v %v", res, err)
	}
	if len(ws.snapshots) != 0 {
		t.Fatal("a refused edit must not snapshot")
	}

	ctx := types.WithMentionedDocuments(toolCtx(), []string{"ws-2"})
	res, err = tool.Execute(ctx, args)
	if err != nil || !res.Success {
		t.Fatalf("mark on the mentioned document: %+v %v", res, err)
	}
	if ws.documentID != "ws-2" || res.Data["document_id"] != "ws-2" {
		t.Fatalf("ops must target ws-2: snapshot=%q data=%v", ws.documentID, res.Data["document_id"])
	}
}

func TestBuildOpenDocumentPromptListsDocumentsAndInjectsTheNamedOnes(t *testing.T) {
	freshDocProfiles(t)
	ws := twoDocWorkspace(t)

	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "")
	for _, want := range []string{"<session_documents>", "vb1 · cong-van.docx (văn bản làm việc, tab đang xem)", "vb2 · to-trinh.docx"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	// Without @ no document is injected whole: each one without a card
	// yet shows its opening lines instead.
	if strings.Contains(got, "<open_document handle") {
		t.Fatalf("without @ no text is injected in full:\n%s", got)
	}
	for _, want := range []string{`<relevant_passages handle="vb1"`, `<relevant_passages handle="vb2"`, "<head>Đầu văn bản"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}

	ctx := types.WithMentionedDocuments(context.Background(), []string{"ws-1", "ws-2"})
	got = BuildOpenDocumentPrompt(ctx, ws, 7, "s-1", "")
	if !strings.Contains(got, `<open_document handle="vb1"`) || !strings.Contains(got, `<open_document handle="vb2"`) {
		t.Fatalf("both named documents are injected:\n%s", got)
	}
	if !strings.Contains(got, "vb2 · to-trinh.docx (văn bản làm việc) (người dùng gọi đích danh trong yêu cầu này)") {
		t.Fatalf("the index marks the named documents:\n%s", got)
	}
	if strings.Contains(got, "<relevant_passages handle") {
		t.Fatalf("a named document turn carries no retrieved passages:\n%s", got)
	}
}

// issuerDocx is a short công văn of issuer with one body paragraph.
func issuerDocx(t *testing.T, issuer, number, body string) []byte {
	return buildTestDocx(t, strings.Join([]string{
		testPara("UBND TỈNH THỪA THIÊN HUẾ", "center", "Times New Roman", 13, false, false),
		testPara(issuer, "center", "Times New Roman", 13, true, false),
		testPara("CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "center", "Times New Roman", 13, true, false),
		testPara("Số: "+number, "center", "Times New Roman", 13, false, false),
		testPara(body, "left", "Times New Roman", 14, false, false),
	}, ""), [4]int{20, 15, 30, 20})
}

// "hai văn bản này do đơn vị nào ban hành" with two tabs and no @ is
// answered from the cards, or from the opening lines while a card is
// being made — never from the full texts.
func TestTwoDocumentsIssuerQuestionUsesCardsOrHeads(t *testing.T) {
	freshDocProfiles(t)
	filler := strings.Repeat("Nội dung chi tiết của kế hoạch triển khai. ", 40)
	ws := newFakeWorkspace(issuerDocx(t, "SỞ NỘI VỤ", "12/SNV-VP", filler))
	ws.addDocument("ws-2", "to-trinh.docx", issuerDocx(t, "SỞ TÀI CHÍNH", "45/TTr-STC", filler))
	const question = "hai văn bản này do đơn vị nào ban hành"

	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", question)
	if strings.Contains(got, "<open_document handle") || strings.Contains(got, filler[:120]+filler[:40]) {
		t.Fatalf("no full text without @:\n%s", got)
	}
	for _, want := range []string{"] SỞ NỘI VỤ\n", "] SỞ TÀI CHÍNH\n", "] Số: 45/TTr-STC\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the opening lines lack %q:\n%s", want, got)
		}
	}

	// once the cards are made they carry the issuers; no opening lines
	docs, _ := ws.List(context.Background(), 7, "s-1")
	for i, issuer := range []string{"SỞ NỘI VỤ", "SỞ TÀI CHÍNH"} {
		p := readyProfile(docs[i], 2)
		p.Issuer = issuer
		docProfiles.storeState(toolCtx(), docs[i].ID, p)
	}
	got = BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", question)
	if strings.Contains(got, "<head>") || strings.Contains(got, "<open_document handle") {
		t.Fatalf("with cards neither heads nor full texts:\n%s", got)
	}
	if !strings.Contains(got, "· SỞ NỘI VỤ ·") || !strings.Contains(got, "· SỞ TÀI CHÍNH ·") {
		t.Fatalf("the cards name the issuers:\n%s", got)
	}
}

// A question naming a code finds its paragraph deep in either document;
// the passages stay under their budget and carry their neighbours.
func TestRelevantPassagesFindTheQuestionInEveryDocument(t *testing.T) {
	freshDocProfiles(t)
	var a, b strings.Builder
	for i := 0; i < 60; i++ {
		a.WriteString(testPara(fmt.Sprintf("Đoạn %d nói về công tác chung của văn phòng.", i), "left", "Times New Roman", 14, false, false))
		b.WriteString(testPara(fmt.Sprintf("Mục %d bàn về việc phối hợp chung.", i), "left", "Times New Roman", 14, false, false))
		if i == 40 {
			a.WriteString(testPara("Giao Phòng PA05 rà soát lỗ hổng bảo mật trước ngày 30/11.", "left", "Times New Roman", 14, false, false))
			b.WriteString(testPara("Phòng PA05 báo cáo kết quả rà soát cho Giám đốc.", "left", "Times New Roman", 14, false, false))
		}
	}
	ws := newFakeWorkspace(buildTestDocx(t, a.String(), [4]int{20, 15, 30, 20}))
	ws.addDocument("ws-2", "bao-cao.docx", buildTestDocx(t, b.String(), [4]int{20, 15, 30, 20}))
	docs, _ := ws.List(context.Background(), 7, "s-1")
	for _, d := range docs {
		docProfiles.storeState(toolCtx(), d.ID, readyProfile(d, 1))
	}

	got := BuildOpenDocumentPrompt(context.Background(), ws, 7, "s-1", "PA05 rà soát gì")
	for _, want := range []string{
		`<relevant_passages handle="vb1" name="cong-van.docx" role="văn bản làm việc" unit="đoạn">`,
		"[41] Giao Phòng PA05 rà soát lỗ hổng bảo mật",
		"[40] Đoạn 40 nói về", "[42] Đoạn 41 nói về",
		`<relevant_passages handle="vb2"`, "] Phòng PA05 báo cáo kết quả rà soát",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Đoạn 5 nói về") || strings.Contains(got, "<open_document handle") {
		t.Fatalf("only the matching passages are injected:\n%s", got)
	}
	if strings.Count(got, "<instruction>"+relevantPassagesInstruction) != 1 {
		t.Fatal("the passages instruction is given once")
	}
}

func TestBuildOpenDocumentPromptWithoutDocumentsIsEmpty(t *testing.T) {
	if got := BuildOpenDocumentPrompt(context.Background(), &emptyWorkspace{}, 7, "s-1", "PA05"); got != "" {
		t.Fatalf("a session without documents adds nothing to the prompt, got %q", got)
	}
}

// emptyWorkspace is a session that holds no document.
type emptyWorkspace struct{ fakeWorkspace }

func (*emptyWorkspace) List(context.Context, uint64, string) ([]*types.DocumentWorkspace, error) {
	return nil, nil
}
