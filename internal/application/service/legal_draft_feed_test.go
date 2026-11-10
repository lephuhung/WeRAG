package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const kl114OCR = `<!-- page 1 -->

BAN CHẤP HÀNH TRUNG ƯƠNG

ĐẢNG CỘNG SẢN VIỆT NAM

Số 114-KL/TW

# KẾT LUẬN

của Bộ Chính trị về tiếp tục hoàn thiện bộ máy quản lý nhà nước về an toàn thực phẩm

1. Thống nhất một đầu mối quản lý nhà nước về an toàn thực phẩm từ Trung ương đến địa phương.

<!-- page 2 -->

2. Giao Đảng uỷ Chính phủ chỉ đạo rà soát, sửa đổi các quy định pháp luật có liên quan.

T/M BỘ CHÍNH TRỊ
`

// draftNamedKBs names the public KBs the approve dialog offers.
type draftNamedKBs struct {
	interfaces.KnowledgeBaseService
}

func (draftNamedKBs) GetKnowledgeBaseByIDOnly(_ context.Context, id string) (*types.KnowledgeBase, error) {
	name := LegalKBNamePrefix + "Quyết định, Chỉ thị"
	if id == "kb-dang" {
		name = LegalPartyKBName
	}
	return &types.KnowledgeBase{ID: id, Name: name, TenantID: types.PlatformTenantID, OwnerTenantID: 0}, nil
}

// A scan a feed pushes: a feed row and a draft; a Kết luận without Điều is
// approved as plain text into the Party KB with the feed's address.
func TestLegalFeedDraftFlow(t *testing.T) {
	svc, db, know, ws, _, _, _ := newDraftTestService(t)
	ctx := superAdminCtx()
	svc.kbs = draftNamedKBs{}
	// heldDoc reads the knowledges' age
	if err := db.Exec(`ALTER TABLE knowledges ADD COLUMN created_at DATETIME`).Error; err != nil {
		t.Fatal(err)
	}
	svc.Push = NewLegalPushService(db, svc.feed, svc.kbs, repository.NewLegalRequestRepository(db), nil)
	svc.OCR = func(_ context.Context, pdf []byte, name string) (string, int, string, error) {
		if !strings.HasPrefix(string(pdf), "%PDF") || name != "kltw114_signed.pdf" {
			t.Errorf("OCR got %q %q", pdf, name)
		}
		return kl114OCR, 2, "vietnamese_legal", nil
	}
	in := LegalFeedDraftInput{PDF: []byte("%PDF-1.4\nscan"), FileName: "kltw114_signed.pdf", Metadata: legalPartyMeta()}

	if _, err := svc.PushFeedDraft(ctx, LegalFeedDraftInput{PDF: []byte("<html>"), Metadata: legalPartyMeta()}, LegalFeedActor{}); httpStatus(err) != http.StatusBadRequest {
		t.Fatalf("not a PDF: %v", err)
	}
	res, err := svc.PushFeedDraft(ctx, in, LegalFeedActor{EndpointID: "apikey:9"})
	if err != nil {
		t.Fatal(err)
	}
	if item, err := svc.Get(ctx, res.Draft.ID); err != nil || item.Status != types.LegalDraftReady || item.OCRPages != 2 {
		t.Fatalf("after OCR: %+v %v", item, err)
	}
	if !res.CatalogueCreated || res.LegalDocumentID != "dangcongsan-tulieu-9001" || res.Existing ||
		res.Draft.DocumentNumber != "114-KL/TW" || res.Draft.DocType != "Kết luận" ||
		!strings.Contains(string(res.Draft.SourceMeta), "tulieuvankien.dangcongsan.vn/x-9001") || res.Draft.CreatedBy != "apikey:9" {
		t.Fatalf("push = %+v draft %+v", res, res.Draft)
	}
	again, err := svc.PushFeedDraft(ctx, in, LegalFeedActor{})
	if err != nil || !again.Existing || again.Draft.ID != res.Draft.ID || again.CatalogueCreated {
		t.Fatalf("again = %+v %v", again, err)
	}

	if _, err := svc.Open(ctx, res.Draft.ID, types.LegalDraftViewer{UserID: "u-super"}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.Structure(ctx, res.Draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || report.Blocking != 0 || len(report.Articles) != 0 {
		t.Fatalf("a Kết luận without Điều is ready: %+v", report)
	}
	found := false
	for _, w := range report.Warnings {
		if w.Code == "too_few_articles" {
			found = !w.Blocking && strings.Contains(w.Message, "không chia Điều")
		}
	}
	if !found {
		t.Fatalf("warnings = %+v", report.Warnings)
	}

	// a Party document goes to the Party KB only
	if _, err := svc.Approve(ctx, res.Draft.ID, "kb-qd"); httpStatus(err) != http.StatusBadRequest ||
		!strings.Contains(err.Error(), LegalPartyKBName) {
		t.Fatalf("approve into a type KB: %v", err)
	}
	out, err := svc.Approve(ctx, res.Draft.ID, "kb-dang")
	if err != nil {
		t.Fatal(err)
	}
	if out.Result != "created" || out.Articles != 0 || out.Draft.Status != types.LegalDraftApproved {
		t.Fatalf("approve = %+v", out)
	}
	m := know.lastMeta
	if len(know.created) != 1 || !strings.HasPrefix(know.created[0], "kb-dang|114-KL-TW - Kết luận của Bộ Chính trị") ||
		m["external_id"] != "dangcongsan-tulieu-9001" || m["source"] != "dangcongsan" || m["source_doc_id"] != "tulieu-9001" ||
		m["source_url"] != "https://tulieuvankien.dangcongsan.vn/x-9001" || m["source_format"] != LegalSourcePDFReviewed ||
		m["doc_number"] != "114-KL/TW" || m["issue_date"] != "2026-11-03" || m["reviewed_by"] != "u-super" {
		t.Fatalf("ingest = %v meta %v", know.created, m)
	}
	_ = ws
}

func TestAllowNoArticles(t *testing.T) {
	blocked := func() *types.LegalDraftStructure {
		return &types.LegalDraftStructure{Blocking: 1, Warnings: []types.LegalDraftWarning{
			{Code: "too_few_articles", Severity: legalDraftSeverityError, Blocking: true}}}
	}
	r := blocked()
	allowNoArticles(r, "Kết luận")
	if !r.Ready || r.Blocking != 0 {
		t.Fatalf("Kết luận: %+v", r)
	}
	r = blocked()
	allowNoArticles(r, "Quy định")
	if r.Ready || r.Blocking != 1 {
		t.Fatalf("a Quy định without Điều stays blocked: %+v", r)
	}
	r = blocked()
	r.Articles = []types.LegalDraftArticle{{Number: "1"}}
	allowNoArticles(r, "Kết luận")
	if r.Ready {
		t.Fatalf("one Điều found: the check stays: %+v", r)
	}
	r = blocked()
	r.Warnings = append(r.Warnings, types.LegalDraftWarning{Code: "ocr_degenerate", Blocking: true})
	r.Blocking = 2
	allowNoArticles(r, "Thông báo")
	if r.Ready || r.Blocking != 1 {
		t.Fatalf("other errors still block: %+v", r)
	}
}
