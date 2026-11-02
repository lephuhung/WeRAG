package service

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type feedKnowledge struct {
	interfaces.KnowledgeService
	created  []string
	lastMeta map[string]string
	lastType string
}

func (f *feedKnowledge) CreateKnowledgeFromFile(_ context.Context, kbID string, fh *multipart.FileHeader,
	meta map[string]string, _ *bool, name string, _ []string, channel string, _ *types.KnowledgeProcessOverrides,
) (*types.Knowledge, error) {
	f.created = append(f.created, kbID+"|"+name+"|"+meta["external_id"]+"|"+channel)
	f.lastMeta, f.lastType = meta, fh.Header.Get("Content-Type")
	return &types.Knowledge{ID: "k-new", KnowledgeBaseID: kbID, Title: name}, nil
}

type feedRequests struct {
	interfaces.LegalRequestRepository
	marked []string
}

func (f *feedRequests) MarkAddedByDocument(_ context.Context, docID, kid, kbID, actor string) (int64, error) {
	f.marked = append(f.marked, docID+"|"+kid+"|"+kbID+"|"+actor)
	return 1, nil
}

// feedGateway serves vbpl.vn gateway answers keyed by the document id.
type feedGateway struct{ base string }

func (g feedGateway) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme, clone.URL.Host = "http", strings.TrimPrefix(g.base, "http://")
	return http.DefaultTransport.RoundTrip(clone)
}

func TestLegalFeedIngestFromPortal(t *testing.T) {
	body := `<p class="prov-article">Điều 1. Phạm vi điều chỉnh</p>` + strings.Repeat("<p>nội dung</p>", 30)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.EscapedPath() == "/api/qtdc/public/doc/minio/buckets/vbpl/3003/VanBanGoc_3%20TT.pdf/download":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "%PDF-1.4\nscan")
		case strings.HasSuffix(r.URL.Path, "/3003"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"3003","docNum":"3/2021/TT-BTC","title":"Thông tư 3/2021/TT-BTC",
				"docType":{"name":"Thông tư"},"effStatus":{"name":"Còn hiệu lực"},"documentContentFileName":"VanBanGoc_3 TT.pdf","documentContent":{"content":""}}}`)
		case strings.HasSuffix(r.URL.Path, "/155366"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"155366","docNum":"53/2022/NĐ-CP","title":"Nghị định 53/2022/NĐ-CP quy định chi tiết",
				"docType":{"name":"Nghị định"},"effStatus":{"name":"Còn hiệu lực"},"documentContent":{"content":"`+strings.ReplaceAll(body, `"`, `\"`)+`"}}}`)
		case strings.HasSuffix(r.URL.Path, "/1001"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"1001","docNum":"27/2015/NĐ-CP","effStatus":{"name":"Hết hiệu lực toàn bộ"},
				"documentContent":{"content":"`+strings.ReplaceAll(body, `"`, `\"`)+`"}}}`)
		case strings.HasSuffix(r.URL.Path, "/2002"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"2002","docNum":"1/2020/TT-X","effStatus":{"name":"Còn hiệu lực"},"documentContent":{"content":"<p></p>"}}}`)
		case strings.HasSuffix(r.URL.Path, "/stale"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"success":"false","httpStatus":"400","messageCode":"invalid.document.entity.not.found","message":"Văn bản pháp luật bạn chọn không tồn tại."}`)
		case strings.HasSuffix(r.URL.Path, "/broken"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `<html>bad request</html>`)
		default:
			_, _ = io.WriteString(w, `{"success":true,"data":null}`)
		}
	}))
	defer srv.Close()

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE knowledges (id TEXT, knowledge_base_id TEXT, file_hash TEXT, metadata TEXT, deleted_at DATETIME)`,
		`CREATE TABLE knowledge_bases (id TEXT, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE legal_feed_log (id INTEGER PRIMARY KEY, action TEXT, vbpl_id TEXT, knowledge_id TEXT, knowledge_base_id TEXT,
			document_number TEXT, reason TEXT, actor_endpoint_id TEXT, actor_tenant_id INTEGER, detail TEXT, created_at DATETIME)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	know, reqs := &feedKnowledge{}, &feedRequests{}
	feed := NewLegalFeedService(db, know, reqs)
	feed.HTTP = &http.Client{Transport: feedGateway{base: srv.URL}}
	reviews := &fakeReviewLinks{links: map[string]*types.LegalDocumentLink{}}
	feed.Reviews = reviews
	kb := &types.KnowledgeBase{ID: "kb-nd", OwnerTenantID: 0}
	actor := LegalFeedActor{TenantID: 10000, UserID: "u-super", Channel: types.ChannelWeb}
	ctx := context.Background()

	for id, want := range map[string]string{"1001": "fully expired", "2002": "vbpl.vn không có toàn văn (HTML hay PDF) cho văn bản 2002", "404": "vbpl.vn không có văn bản với id 404", "stale": "vbpl.vn không có văn bản với id stale (danh mục có thể đã cũ)", "../x": "not a vbpl.vn document id"} {
		_, err := feed.IngestFromPortal(ctx, id, kb, actor, "")
		if _, ok := err.(*LegalFeedRefusal); !ok || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want refusal %q", id, err, want)
		}
	}
	// a 400 without the not-found body stays a gateway failure (API 503)
	if _, err := feed.IngestFromPortal(ctx, "broken", kb, actor, ""); err == nil || strings.Contains(err.Error(), "không có văn bản") {
		t.Fatalf("broken: err = %v", err)
	} else if _, ok := err.(*LegalFeedRefusal); ok {
		t.Fatalf("broken must not be a refusal: %v", err)
	}
	if _, err := feed.IngestFromPortal(ctx, "155366", &types.KnowledgeBase{ID: "own", OwnerTenantID: 7}, actor, ""); err == nil {
		t.Fatal("a tenant KB must be refused")
	}

	res, err := feed.IngestFromPortal(ctx, "155366", kb, actor, "test")
	if err != nil || res.Result != "created" || res.KnowledgeID != "k-new" || res.DocumentNumber != "53/2022/NĐ-CP" ||
		res.SourceFormat != LegalSourceHTML {
		t.Fatalf("ingest: %+v %v", res, err)
	}
	if len(know.created) != 1 || !strings.HasPrefix(know.created[0], "kb-nd|Nghị định 53-2022-NĐ-CP quy định chi tiết.html|155366|web") {
		t.Fatalf("created = %v", know.created)
	}
	if len(reqs.marked) != 1 || reqs.marked[0] != "155366|k-new|kb-nd|u-super" {
		t.Fatalf("marked = %v", reqs.marked)
	}
	// the ingest links the document to its catalogue row at once, as the scan would
	if l := reviews.links["k-new"]; l == nil || l.ReviewStatus != types.LegalReviewApproved || l.LegalDocumentID != "155366" ||
		l.ReviewedBy != types.LegalReviewerVBPL || l.KnowledgeBaseID != "kb-nd" || l.ReviewedAt == nil ||
		!strings.Contains(string(l.Suggestion), `"document_number":"53/2022/NĐ-CP"`) {
		t.Fatalf("review link = %+v", l)
	}
	if know.lastType != "text/html" || know.lastMeta["source_format"] != "" {
		t.Fatalf("html upload: type %q meta %v", know.lastType, know.lastMeta)
	}
	var logged int64
	db.Raw(`SELECT COUNT(*) FROM legal_feed_log WHERE action = 'ingest_created' AND vbpl_id = '155366' AND actor_tenant_id = 10000`).Scan(&logged)
	if logged != 1 {
		t.Fatalf("feed log rows = %d", logged)
	}

	// only a scanned PDF: always refused (a scan goes through a reviewed
	// draft), the refusal says what exists
	_, err = feed.IngestFromPortal(ctx, "3003", kb, actor, "")
	refusal, ok := err.(*LegalFeedRefusal)
	if !ok || !refusal.PDFOnly || refusal.Portal == nil || refusal.Portal.PDFFile != "VanBanGoc_3 TT.pdf" || refusal.Portal.HTML ||
		!strings.Contains(err.Error(), "Kéo về bản nháp") {
		t.Fatalf("pdf only: %v %+v", err, refusal)
	}
	if len(know.created) != 1 {
		t.Fatalf("a scan was ingested: %v", know.created)
	}

	// IngestHTML: a reviewed draft's HTML with its own metadata
	draftHTML := []byte(`<p class="prov-article">Điều 1. Phạm vi</p>`)
	res, err = feed.IngestHTML(ctx, LegalIngestDocument{VBPLID: "3003", DocumentNumber: "3/2021/TT-BTC",
		FileName: "Thông tư 3-2021-TT-BTC.html", SourceFormat: LegalSourcePDFReviewed,
		Metadata:  map[string]string{"external_id": "3003", "source_format": LegalSourcePDFReviewed},
		LogDetail: map[string]any{"draft_id": "d1"}}, draftHTML, kb, actor, "draft")
	if err != nil || res.Result != "created" || res.SourceFormat != LegalSourcePDFReviewed || !res.VBPLMarkup {
		t.Fatalf("ingest html: %+v %v", res, err)
	}
	if know.lastType != "text/html" || know.lastMeta["source_format"] != "pdf_reviewed" {
		t.Fatalf("reviewed upload: type %q meta %v", know.lastType, know.lastMeta)
	}
	var detail string
	db.Raw(`SELECT detail FROM legal_feed_log WHERE vbpl_id = '3003'`).Scan(&detail)
	if !strings.Contains(detail, `"source_format":"pdf_reviewed"`) || !strings.Contains(detail, `"draft_id":"d1"`) {
		t.Fatalf("draft log detail = %s", detail)
	}

	portal, err := feed.CheckPortal(ctx, "3003")
	if err != nil || !portal.Exists || portal.HTML || portal.PDFFile != "VanBanGoc_3 TT.pdf" || portal.Status != "Còn hiệu lực" {
		t.Fatalf("check 3003: %+v %v", portal, err)
	}
	if portal, err = feed.CheckPortal(ctx, "stale"); err != nil || portal.Exists {
		t.Fatalf("check stale: %+v %v", portal, err)
	}
	if portal, err = feed.CheckPortal(ctx, "155366"); err != nil || !portal.HTML || portal.PDFFile != "" {
		t.Fatalf("check 155366: %+v %v", portal, err)
	}
}

func TestLegalFeedReviewLinkKeepsReviewerDecision(t *testing.T) {
	reviews := &fakeReviewLinks{links: map[string]*types.LegalDocumentLink{
		"k1": {KnowledgeID: "k1", LegalDocumentID: "other", ReviewStatus: types.LegalReviewRejected, ReviewedBy: "u-rev"},
	}}
	feed := &LegalFeedService{Reviews: reviews}
	doc := LegalIngestDocument{VBPLID: "155366", DocumentNumber: "53/2022/NĐ-CP"}
	feed.saveReviewLink(context.Background(), doc, "k1", "kb", false)
	if l := reviews.links["k1"]; l.ReviewStatus != types.LegalReviewRejected || l.LegalDocumentID != "other" {
		t.Fatalf("an unchanged copy overwrote the reviewer's link: %+v", l)
	}
	feed.saveReviewLink(context.Background(), doc, "k2", "kb", false)
	if l := reviews.links["k2"]; l == nil || l.ReviewStatus != types.LegalReviewApproved || l.ReviewedBy != types.LegalReviewerVBPL {
		t.Fatalf("an unchanged copy without a link got none: %+v", l)
	}
}
