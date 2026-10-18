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
	created []string
}

func (f *feedKnowledge) CreateKnowledgeFromFile(_ context.Context, kbID string, fh *multipart.FileHeader,
	meta map[string]string, _ *bool, name string, _ []string, channel string, _ *types.KnowledgeProcessOverrides,
) (*types.Knowledge, error) {
	f.created = append(f.created, kbID+"|"+name+"|"+meta["external_id"]+"|"+channel)
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
	kb := &types.KnowledgeBase{ID: "kb-nd", OwnerTenantID: 0}
	actor := LegalFeedActor{TenantID: 10000, UserID: "u-super", Channel: types.ChannelWeb}
	ctx := context.Background()

	for id, want := range map[string]string{"1001": "fully expired", "2002": "no published full text", "404": "vbpl.vn không có văn bản với id 404", "stale": "vbpl.vn không có văn bản với id stale (danh mục có thể đã cũ)", "../x": "not a vbpl.vn document id"} {
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
	if err != nil || res.Result != "created" || res.KnowledgeID != "k-new" || res.DocumentNumber != "53/2022/NĐ-CP" {
		t.Fatalf("ingest: %+v %v", res, err)
	}
	if len(know.created) != 1 || !strings.HasPrefix(know.created[0], "kb-nd|Nghị định 53-2022-NĐ-CP quy định chi tiết.html|155366|web") {
		t.Fatalf("created = %v", know.created)
	}
	if len(reqs.marked) != 1 || reqs.marked[0] != "155366|k-new|kb-nd|u-super" {
		t.Fatalf("marked = %v", reqs.marked)
	}
	var logged int64
	db.Raw(`SELECT COUNT(*) FROM legal_feed_log WHERE action = 'ingest_created' AND vbpl_id = '155366' AND actor_tenant_id = 10000`).Scan(&logged)
	if logged != 1 {
		t.Fatalf("feed log rows = %d", logged)
	}
}
