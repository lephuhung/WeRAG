package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/docformat/docxwrite"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vbpl"
)

// draftOCRText is a short Thông tư as the OCR reads it: Điều 3 is lost, the
// Đ of a số hiệu is lost, "Chương II" has no title, a khoản is repeated.
const draftOCRText = `<!-- page 1 -->

BỘ TÀI CHÍNH

CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM

Số: 3/2021/TT-BTC

# THÔNG TƯ

Quy định về quản lý phí

Căn cứ Nghị định số 14/2023/ND-CP ngày 20 tháng 4 năm 2023;

Chương I

QUY ĐỊNH CHUNG

**Điều 1. Phạm vi điều chỉnh**

1\. Thông tư này quy định về quản lý phí.

2\. Khoản hai của Điều 1.

a) điểm a;

b) điểm b.

Điều 2. Đối tượng áp dụng

Thông tư này áp dụng đối với cơ quan thu phí.

| Mức phí | Đơn vị |
|---|---|
| 10.000 | đồng |

<!-- page 2 -->

![doc_page_2.jpg](images/doc_page_2.jpg)

Chương II

Điều 4. Hiệu lực thi hành

1\. Thông tư này có hiệu lực từ ngày 01 tháng 01 năm 2022.

1\. Khoản lặp lại do OCR.

Nơi nhận:

- Như trên;

KT. BỘ TRƯỞNG
`

type draftFiles struct {
	interfaces.FileService
	saved map[string][]byte
}

func (f *draftFiles) SaveBytes(_ context.Context, data []byte, tenantID uint64, name string, _ bool) (string, error) {
	ref := "resource://" + name
	f.saved[ref] = data
	return ref, nil
}

type draftCatalog struct {
	interfaces.ResourceCatalog
	bound []string
}

func (c *draftCatalog) Bind(_ context.Context, ref, ownerType, ownerID, _ string) error {
	c.bound = append(c.bound, ref+"|"+ownerType+"|"+ownerID)
	return nil
}

func (c *draftCatalog) CreateAccessGrant(context.Context, string, time.Duration) (string, error) {
	return "tok", nil
}

type draftSessions struct {
	interfaces.SessionService
	created []*types.Session
}

func (s *draftSessions) CreateSession(_ context.Context, sess *types.Session) (*types.Session, error) {
	sess.ID = "s-" + uuid.NewString()[:8]
	s.created = append(s.created, sess)
	return sess, nil
}

func (s *draftSessions) GetSessionByID(_ context.Context, tenantID uint64, id string) (*types.Session, error) {
	for _, sess := range s.created {
		if sess.ID == id && sess.TenantID == tenantID {
			return sess, nil
		}
	}
	return nil, apperrors.NewNotFoundError("session not found")
}

type draftAttachments struct {
	interfaces.TemporaryDocumentService
	docs map[string]*types.TemporaryDocument
	data map[string][]byte
	opts types.TemporaryDocumentCreateOptions
}

func (a *draftAttachments) Create(_ context.Context, tenantID uint64, sessionID, name, mime string, size int64, r io.Reader,
	opts types.TemporaryDocumentCreateOptions,
) (*types.TemporaryDocument, error) {
	data, _ := io.ReadAll(r)
	doc := &types.TemporaryDocument{ID: "att-" + uuid.NewString()[:8], TenantID: tenantID, SessionID: sessionID,
		FileName: name, MimeType: mime, FileSize: size}
	a.docs[doc.ID], a.data[doc.ID], a.opts = doc, data, opts
	return doc, nil
}

func (a *draftAttachments) Get(_ context.Context, tenantID uint64, sessionID, id string) (*types.TemporaryDocument, error) {
	if d := a.docs[id]; d != nil && d.TenantID == tenantID && d.SessionID == sessionID {
		return d, nil
	}
	return nil, nil
}

type draftWorkspaces struct {
	interfaces.DocumentWorkspaceService
	att     *draftAttachments
	ws      *types.DocumentWorkspace
	data    []byte
	created int
	origin  string
}

func (w *draftWorkspaces) CreateFromAttachmentFor(_ context.Context, tenantID uint64, sessionID, userID, attID, kind string,
) (*types.DocumentWorkspace, error) {
	w.created++
	w.data = w.att.data[attID]
	w.ws = &types.DocumentWorkspace{ID: "ws-" + uuid.NewString()[:8], TenantID: tenantID, SessionID: sessionID,
		AttachmentID: attID, UserID: userID, EditorKind: kind, Revision: 3}
	return w.ws, nil
}

func (w *draftWorkspaces) Get(_ context.Context, tenantID uint64, sessionID, id string) (*types.DocumentWorkspace, error) {
	if w.ws != nil && w.ws.ID == id && w.ws.SessionID == sessionID && w.ws.TenantID == tenantID {
		return w.ws, nil
	}
	return nil, apperrors.NewNotFoundError("Document workspace not found")
}

func (w *draftWorkspaces) View(ctx context.Context, ws *types.DocumentWorkspace, userID, userName, lang string,
) (*types.DocumentWorkspaceView, error) {
	w.origin = DocumentEditorOrigin(ctx)
	return &types.DocumentWorkspaceView{DocumentWorkspace: ws, EditorKey: ws.ID + "-3",
		Editor: &types.DocumentEditorConfig{DocumentServerURL: "http://ds", Config: map[string]interface{}{"lang": lang}}}, nil
}

func (w *draftWorkspaces) PrepareExternalWrite(_ context.Context, tenantID uint64, sessionID, id string, _ time.Duration,
) (*types.DocumentWorkspace, []byte, error) {
	return w.ws, w.data, nil
}

type draftKBs struct {
	interfaces.KnowledgeBaseService
}

func (draftKBs) GetKnowledgeBaseByIDOnly(_ context.Context, id string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: id, TenantID: types.PlatformTenantID, OwnerTenantID: 0}, nil
}

func newDraftTestService(t *testing.T) (*LegalDraftService, *gorm.DB, *feedKnowledge, *draftWorkspaces, *draftSessions, *draftAttachments, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := strings.Repeat("<p>nội dung</p>", 30)
		switch {
		case strings.Contains(r.URL.Path, "/minio/buckets/vbpl/3003/"):
			_, _ = io.WriteString(w, "%PDF-1.4\nscan")
		case strings.HasSuffix(r.URL.Path, "/3003"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"3003","docNum":"3/2021/TT-BTC","title":"Thông tư 3/2021/TT-BTC quy định về quản lý phí",
				"docType":{"name":"Thông tư"},"effStatus":{"name":"Còn hiệu lực"},"documentContentFileName":"VanBanGoc_3 TT.pdf","documentContent":{"content":""}}}`)
		case strings.HasSuffix(r.URL.Path, "/155366"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"155366","docNum":"53/2022/NĐ-CP","effStatus":{"name":"Còn hiệu lực"},
				"documentContent":{"content":"`+html+`"}}}`)
		case strings.HasSuffix(r.URL.Path, "/1001"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"1001","docNum":"27/2015/NĐ-CP","effStatus":{"name":"Hết hiệu lực toàn bộ"},
				"documentContentFileName":"VanBanGoc_27.pdf","documentContent":{"content":""}}}`)
		default:
			_, _ = io.WriteString(w, `{"success":true,"data":null}`)
		}
	}))
	t.Cleanup(srv.Close)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.LegalDocumentRequest{}, &types.LegalDocumentDraft{}); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE knowledges (id TEXT, knowledge_base_id TEXT, file_hash TEXT, metadata TEXT, deleted_at DATETIME, parse_status TEXT, title TEXT, file_name TEXT)`,
		`CREATE TABLE knowledge_bases (id TEXT, name TEXT, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE legal_feed_log (id INTEGER PRIMARY KEY, action TEXT, vbpl_id TEXT, knowledge_id TEXT, knowledge_base_id TEXT,
			document_number TEXT, reason TEXT, actor_endpoint_id TEXT, actor_tenant_id INTEGER, detail TEXT, created_at DATETIME)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	requests := repository.NewLegalRequestRepository(db)
	drafts := repository.NewLegalDraftRepository(db)
	know := &feedKnowledge{}
	feed := NewLegalFeedService(db, know, requests)
	feed.HTTP = &http.Client{Transport: feedGateway{base: srv.URL}}
	att := &draftAttachments{docs: map[string]*types.TemporaryDocument{}, data: map[string][]byte{}}
	ws := &draftWorkspaces{att: att}
	sessions := &draftSessions{}
	reqSvc := NewLegalRequestService(requests, nil, draftKBs{}, feed, drafts)
	svc := NewLegalDraftService(drafts, requests, reqSvc, feed, &draftFiles{saved: map[string][]byte{}}, &draftCatalog{},
		nil, nil, sessions, att, ws, draftKBs{})
	svc.OCR = func(_ context.Context, pdf []byte, name string) (string, int, string, error) {
		if !strings.HasPrefix(string(pdf), "%PDF") || name != "VanBanGoc_3 TT.pdf" {
			t.Errorf("OCR got %q %q", pdf, name)
		}
		return draftOCRText, 2, "vietnamese_legal", nil
	}
	svc.Go = func(fn func()) { fn() }
	return svc, db, know, ws, sessions, att, srv
}

func superAdminCtx() context.Context {
	ctx := context.WithValue(context.Background(), types.SystemAdminContextKey, true)
	ctx = context.WithValue(ctx, types.UserIDContextKey, "u-super")
	ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(7))
	return context.WithValue(ctx, types.CallerContextKey, types.Caller{TenantID: 7, UserID: "u-super"})
}

func TestLegalDraftFetchRefusals(t *testing.T) {
	svc, db, _, _, _, _, _ := newDraftTestService(t)
	ctx := superAdminCtx()
	requests := repository.NewLegalRequestRepository(db)
	for id, want := range map[string]string{
		"155366": "có toàn văn HTML", "1001": "fully expired", "404": "không có văn bản", "../x": "not a vbpl.vn document id",
	} {
		_, err := svc.FetchDocument(ctx, id, "mcp:ep")
		if httpStatus(err) != http.StatusBadRequest || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v, want 400 %q", id, err, want)
		}
	}
	unknown, _ := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "12/2024/QD-UBND", LastSessionID: "s"})
	if _, err := svc.FetchForRequest(ctx, unknown.ID); httpStatus(err) != http.StatusBadRequest {
		t.Fatalf("no vbpl id: %v", err)
	}
	if _, err := svc.FetchForRequest(ctx, "nope"); httpStatus(err) != http.StatusNotFound {
		t.Fatalf("missing: %v", err)
	}
	var n int64
	db.Model(&types.LegalDocumentDraft{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d drafts after refusals", n)
	}
}

func TestLegalDraftFlow(t *testing.T) {
	svc, db, know, ws, sessions, att, _ := newDraftTestService(t)
	ctx := superAdminCtx()
	requests := repository.NewLegalRequestRepository(db)
	req, err := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "3/2021/TT-BTC", DocumentNumber: "3/2021/TT-BTC",
		LegalDocumentID: "3003", LastSessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}

	// fetch: PDF stored, OCRed, request fetched; again → same draft
	draft, err := svc.FetchForRequest(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, err := svc.Get(ctx, draft.ID)
	if err != nil || item.Status != types.LegalDraftReady || item.OCRPages != 2 || item.OCREngine != "vietnamese_legal" ||
		item.RequestID != req.ID || item.PDFFileName != "VanBanGoc_3 TT.pdf" || item.DocumentNumber != "3/2021/TT-BTC" ||
		!strings.Contains(item.OCRMarkdown, "Điều 4.") || item.Request == nil || item.PDFRef == "" {
		t.Fatalf("draft = %+v %v", item, err)
	}
	if row, _ := requests.Get(ctx, req.ID); row.Status != types.LegalRequestFetched {
		t.Fatalf("request status %q", row.Status)
	}
	again, err := svc.FetchDocument(ctx, "3003", "mcp:ep")
	if err != nil || again.ID != draft.ID || again.OCRMarkdown != "" {
		t.Fatalf("refetch: %+v %v", again, err)
	}
	// asking again keeps the request in its draft state
	if row, _ := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "3/2021/TT-BTC", LegalDocumentID: "3003",
		LastSessionID: "s2"}); row.Status != types.LegalRequestFetched || row.RequestCount != 2 {
		t.Fatalf("asked again: %+v", row)
	}
	sums, _ := svc.Summaries(ctx, []string{"3003", "x"})
	if s := sums["3003"]; s == nil || s.Status != types.LegalDraftReady || s.OCRPages != 2 || len(sums) != 1 {
		t.Fatalf("summaries = %v", sums)
	}
	items, total, err := svc.List(ctx, "all", 0, 10)
	if err != nil || total != 1 || items[0].OCRMarkdown != "" || items[0].Request == nil {
		t.Fatalf("list: %v %d %v", items, total, err)
	}
	if _, _, err := svc.List(ctx, "bogus", 0, 10); httpStatus(err) != http.StatusBadRequest {
		t.Fatalf("bad status: %v", err)
	}

	// structure before open: no editor document yet
	if _, err := svc.Structure(ctx, draft.ID); httpStatus(err) != http.StatusConflict {
		t.Fatalf("structure before open: %v", err)
	}

	// open: docx + hidden session + workspace, once
	opened, err := svc.Open(ctx, draft.ID, types.LegalDraftViewer{UserID: "u-super", UserName: "super", Lang: "vi",
		Origin: "https://werag.example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions.created) != 1 {
		t.Fatalf("sessions = %d", len(sessions.created))
	}
	sess := sessions.created[0]
	if sess.TenantID != types.PlatformTenantID || sess.UserID != "u-super" || sess.Description != types.LegalDraftSessionMarker+draft.ID ||
		sess.Title != "Biên tập OCR 3/2021/TT-BTC" || sess.LastRequestState == nil ||
		sess.LastRequestState.AgentID != types.BuiltinDocumentAssistantID {
		t.Fatalf("session = %+v", sess)
	}
	if opened.SessionID != sess.ID || opened.DocumentID != ws.ws.ID || opened.TenantID != types.PlatformTenantID ||
		opened.PDFURL != "/r/tok" || opened.Editor == nil || opened.Revision != 3 || opened.Draft.Status != types.LegalDraftReviewing ||
		ws.origin != "https://werag.example" {
		t.Fatalf("open = %+v (origin %q)", opened, ws.origin)
	}
	if att.opts.InputGate != types.InputGateDocument || att.opts.ResourceTenantID != types.PlatformTenantID {
		t.Fatalf("attachment options = %+v", att.opts)
	}
	for _, d := range att.docs {
		if d.FileName != "Bản nháp OCR 3-2021-TT-BTC.docx" {
			t.Fatalf("attachment name %q", d.FileName)
		}
	}
	doc, err := docxedit.Open(ws.data)
	if err != nil {
		t.Fatalf("draft docx: %v", err)
	}
	texts := map[string]string{}
	layout := docformat.InspectDocx(ws.data)
	for _, p := range doc.Paragraphs() {
		style := "Normal"
		if sn := layout.Paragraphs[p.Index].StyleName; sn != nil {
			style = *sn
		}
		texts[p.Text] = style
	}
	for text, style := range map[string]string{
		"[Trang 1]": "PageMarker", "[Trang 2]": "PageMarker", "Chương I": "heading 2", "QUY ĐỊNH CHUNG": "heading 2",
		"Điều 1. Phạm vi điều chỉnh": "heading 4", "1. Thông tư này quy định về quản lý phí.": "Normal",
		"Mức phí | Đơn vị": "Normal", "10.000 | đồng": "Normal", "THÔNG TƯ": "Normal",
	} {
		got, ok := texts[text]
		if !ok || got != style {
			t.Errorf("paragraph %q: style %q (present %v)", text, got, ok)
		}
	}
	for text := range texts {
		if strings.Contains(text, "doc_page") || strings.Contains(text, "**") || strings.Contains(text, "---") {
			t.Errorf("markdown left in %q", text)
		}
	}
	if row, _ := requests.Get(ctx, req.ID); row.Status != types.LegalRequestReviewing {
		t.Fatalf("request after open: %q", row.Status)
	}
	reopened, err := svc.Open(ctx, draft.ID, types.LegalDraftViewer{UserID: "u-super"})
	if err != nil || reopened.SessionID != opened.SessionID || reopened.DocumentID != opened.DocumentID ||
		len(sessions.created) != 1 || ws.created != 1 {
		t.Fatalf("reopen: %+v %v (sessions %d, workspaces %d)", reopened, err, len(sessions.created), ws.created)
	}

	// structure of the OCR text as opened
	report, err := svc.Structure(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for _, w := range report.Warnings {
		codes[w.Code] += w.Message + "; "
	}
	if len(report.Articles) != 3 || !report.Ready || report.Blocking != 0 || report.PageMarkers != 2 || report.Revision != 3 {
		t.Fatalf("report = %+v", report)
	}
	for code, want := range map[string]string{
		"article_gap": "Thiếu Điều 3 giữa Điều 2 và Điều 4", "ocr_suspect": "14/2023/ND-CP", "heading_without_title": "Chương II",
		"subdivision_rejected": "1. (Điều 4)", "page_marker": "Còn 2 dấu",
	} {
		if !strings.Contains(codes[code], want) {
			t.Errorf("warning %s = %q, want %q", code, codes[code], want)
		}
	}
	if len(report.Headings) != 5 || report.Headings[1].Kind != "dieu" || report.Headings[1].Parent != 0 ||
		report.Headings[1].Number != "1" || report.Headings[3].Kind != "chuong" || report.Headings[3].Number != "II" ||
		report.Headings[4].Parent != 3 || report.Articles[2].Number != "4" {
		t.Fatalf("headings = %+v", report.Headings)
	}
	stored, _ := svc.Get(ctx, draft.ID)
	if !strings.Contains(string(stored.Structure), `"ready_to_approve":true`) {
		t.Fatalf("structure not cached: %s", stored.Structure)
	}

	// the reviewer deleted two Điều: approval is blocked
	good := ws.data
	short, _ := docxwrite.Build(docxwrite.Document{Paragraphs: []docxwrite.Paragraph{
		{Text: "Điều 1. Phạm vi", Style: docxwrite.StyleHeading4}, {Text: "1. Nội dung."},
	}})
	ws.data = short
	if _, err := svc.Approve(ctx, draft.ID, "kb-tt"); httpStatus(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "Mới nhận ra 1 Điều") {
		t.Fatalf("blocked approve: %v", err)
	}
	if len(know.created) != 0 {
		t.Fatalf("ingested while blocked: %v", know.created)
	}
	ws.data = good
	if _, err := svc.Approve(ctx, draft.ID, ""); httpStatus(err) != http.StatusBadRequest {
		t.Fatalf("approve without KB: %v", err)
	}

	// approve: HTML ingested like a portal page, request added
	res, err := svc.Approve(ctx, draft.ID, "kb-tt")
	if err != nil {
		t.Fatal(err)
	}
	if res.Result != "created" || res.KnowledgeID != "k-new" || res.Articles != 3 || res.Draft.Status != types.LegalDraftApproved ||
		res.Draft.ApprovedKBID != "kb-tt" || res.Request == nil || res.Request.Status != types.LegalRequestAdded ||
		res.Request.DecisionNote != LegalDraftApprovedNote || res.Request.KnowledgeID != "k-new" {
		t.Fatalf("approve = %+v request %+v", res, res.Request)
	}
	if len(know.created) != 1 || know.created[0] != "kb-tt|Thông tư 3-2021-TT-BTC quy định về quản lý phí.html|3003|web" ||
		know.lastType != "text/html" || know.lastMeta["source_format"] != LegalSourcePDFReviewed ||
		know.lastMeta["source_file"] != "VanBanGoc_3 TT.pdf" || know.lastMeta["reviewed_by"] != "u-super" ||
		know.lastMeta["reviewed_at"] == "" || know.lastMeta["doc_number"] != "3/2021/TT-BTC" {
		t.Fatalf("ingest = %v meta %v type %q", know.created, know.lastMeta, know.lastType)
	}
	if _, err := svc.Approve(ctx, draft.ID, "kb-tt"); httpStatus(err) != http.StatusConflict {
		t.Fatalf("approve twice: %v", err)
	}
	if err := svc.Discard(ctx, draft.ID); httpStatus(err) != http.StatusConflict {
		t.Fatalf("discard approved: %v", err)
	}
}

func TestLegalDraftDiscardAndFailure(t *testing.T) {
	svc, db, _, _, _, _, _ := newDraftTestService(t)
	ctx := superAdminCtx()
	requests := repository.NewLegalRequestRepository(db)
	req, _ := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "3/2021/TT-BTC", LegalDocumentID: "3003", LastSessionID: "s1"})

	calls := 0
	svc.OCR = func(context.Context, []byte, string) (string, int, string, error) {
		calls++
		if calls == 1 {
			return "", 0, "vietnamese_legal", io.ErrUnexpectedEOF
		}
		return draftOCRText, 2, "vietnamese_legal", nil
	}
	first, err := svc.FetchForRequest(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	failed, _ := svc.Get(ctx, first.ID)
	if failed.Status != types.LegalDraftFailed || !strings.Contains(failed.Error, "OCR không thành công") {
		t.Fatalf("failed draft = %+v", failed)
	}
	if row, _ := requests.Get(ctx, req.ID); row.Status != types.LegalRequestRequested {
		t.Fatalf("request after failure: %q", row.Status)
	}
	// a failed draft is fetched again from scratch
	second, err := svc.FetchForRequest(ctx, req.ID)
	if err != nil || second.ID == first.ID {
		t.Fatalf("refetch failed draft: %+v %v", second, err)
	}
	if got, _ := svc.Get(ctx, second.ID); got.Status != types.LegalDraftReady {
		t.Fatalf("second draft = %+v", got)
	}
	if _, err := svc.Approve(ctx, second.ID, "kb-tt"); httpStatus(err) != http.StatusConflict {
		t.Fatalf("approve before open: %v", err)
	}
	if err := svc.Discard(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, second.ID); httpStatus(err) != http.StatusNotFound {
		t.Fatalf("discarded draft: %v", err)
	}
	if row, _ := requests.Get(ctx, req.ID); row.Status != types.LegalRequestRequested {
		t.Fatalf("request after discard: %q", row.Status)
	}
}

func TestLegalDraftHTMLConverts(t *testing.T) {
	var paras []LegalDraftParagraph
	for i, p := range LegalDraftParagraphsFromOCR(draftOCRText) {
		paras = append(paras, LegalDraftParagraph{Index: i, Text: p.Text, Marker: p.Style == docxwrite.StylePageMarker})
	}
	html := BuildLegalDraftHTML(paras)
	for _, want := range []string{
		`<p>BỘ TÀI CHÍNH</p>`, `<p class="prov-chapter">Chương I</p>`, `<p class="prov-chapter">QUY ĐỊNH CHUNG</p>`,
		`<p class="prov-article">Điều 1. Phạm vi điều chỉnh</p>`, `<p class="prov-clause">2. Khoản hai của Điều 1.</p>`,
		`<p class="prov-item">a) điểm a;</p>`, `<p class="prov-content">Mức phí | Đơn vị</p>`, `<p>Nơi nhận:</p>`, `<p>KT. BỘ TRƯỞNG</p>`,
	} {
		if !strings.Contains(string(html), want) {
			t.Errorf("html lacks %s", want)
		}
	}
	if strings.Contains(string(html), "Trang 1") {
		t.Error("page marker in html")
	}
	doc, err := vbpl.ConvertBytes(html)
	if err != nil || !doc.Structured || doc.Stats.Articles != 3 || doc.Stats.Clauses < 3 || doc.Stats.Items != 2 {
		t.Fatalf("converted: %+v %v", doc, err)
	}
	if !vbpl.LooksLikeVBPL(html) {
		t.Fatal("not vbpl markup")
	}
}

func TestAnalyzeLegalDraftBlocksAndSuspects(t *testing.T) {
	paras := []LegalDraftParagraph{
		{Index: 0, Text: "QUYẾT ĐỊNH"},
		{Index: 1, Text: "Điều 1.  Ban hành kèm theo Quyết định này"},
		{Index: 2, Text: "Ðiều 2. Quyết định số 5/QD-UBND hết hiệu lực."},
		{Index: 3, Text: "[Trang 2]", Marker: true},
	}
	r := AnalyzeLegalDraft(paras)
	if r.Ready || r.Blocking != 1 || len(r.Articles) != 1 {
		t.Fatalf("report = %+v", r)
	}
	var msgs []string
	for _, w := range r.Warnings {
		msgs = append(msgs, w.Code+": "+w.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"too_few_articles: Mới nhận ra 1 Điều", "khoảng trắng kép", "\"5/QD-UBND\": có thể OCR đã mất chữ Đ",
		"\"Ðiều\": chữ Ð (Latin Eth)", "page_marker: Còn 1 dấu"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings lack %q:\n%s", want, joined)
		}
	}
}
