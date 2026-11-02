package vbpl

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type toServer struct{ base string }

func (s toServer) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme, clone.URL.Host = "http", strings.TrimPrefix(s.base, "http://")
	return http.DefaultTransport.RoundTrip(clone)
}

func TestFetchDetailStaleIDIsNoDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/187374"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"success":"false","httpStatus":"400","messageCode":"invalid.document.entity.not.found","message":"Văn bản pháp luật bạn chọn không tồn tại."}`)
		case strings.HasSuffix(r.URL.Path, "/gone"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"success":false}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `oops`)
		}
	}))
	defer srv.Close()
	client := &http.Client{Transport: toServer{base: srv.URL}}
	for _, id := range []string{"187374", "gone"} {
		_, err := FetchDetail(context.Background(), client, id)
		var missing *NoDocumentError
		if !errors.As(err, &missing) || missing.ID != id || !strings.Contains(err.Error(), "danh mục có thể đã cũ") {
			t.Fatalf("%s: err = %v", id, err)
		}
	}
	_, err := FetchDetail(context.Background(), client, "other")
	var missing *NoDocumentError
	if err == nil || errors.As(err, &missing) || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("plain 400: err = %v", err)
	}
}

func TestFetchAttachment(t *testing.T) {
	var gotPath, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotUA = r.URL.EscapedPath(), r.Header.Get("User-Agent")
		switch {
		case strings.Contains(r.URL.Path, "/179132/"):
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "%PDF-1.4\nscan")
		case strings.Contains(r.URL.Path, "/big/"):
			_, _ = io.WriteString(w, "%PDF-1.4\n")
			_, _ = w.Write(make([]byte, MaxAttachmentBytes))
		case strings.Contains(r.URL.Path, "/html/"):
			_, _ = io.WriteString(w, "<html>Không có nội dung văn bản</html>")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	client := &http.Client{Transport: toServer{base: srv.URL}}
	ctx := context.Background()

	body, err := FetchAttachment(ctx, client, "179132", "VanBanGoc_TT __TT_BTC.pdf")
	if err != nil || string(body) != "%PDF-1.4\nscan" {
		t.Fatalf("fetch: %q %v", body, err)
	}
	if gotPath != "/api/qtdc/public/doc/minio/buckets/vbpl/179132/VanBanGoc_TT%20__TT_BTC.pdf/download" {
		t.Fatalf("path = %s", gotPath)
	}
	if !strings.HasPrefix(gotUA, "Mozilla/5.0") {
		t.Fatalf("user agent = %q", gotUA)
	}
	if _, err := FetchAttachment(ctx, client, "big", "a.pdf"); err == nil || !strings.Contains(err.Error(), "50 MB") {
		t.Fatalf("big: err = %v", err)
	}
	if _, err := FetchAttachment(ctx, client, "html", "a.pdf"); err == nil || !strings.Contains(err.Error(), "not a PDF") {
		t.Fatalf("html: err = %v", err)
	}
	var missing *NoDocumentError
	if _, err := FetchAttachment(ctx, client, "gone", "a.pdf"); !errors.As(err, &missing) || missing.FileName != "a.pdf" {
		t.Fatalf("gone: err = %v", err)
	}
	if _, err := FetchAttachment(ctx, client, "179132", "../x.pdf"); err == nil {
		t.Fatal("a file name with a slash must be refused")
	}
}

func TestDetailFileNamesAndPDF(t *testing.T) {
	d := &Detail{ID: "1", DocNum: "74/2022/TT-BTC", Title: "Thông tư quy định", ContentFileName: "VanBanGoc_74.TT.PDF"}
	if got := PDFFileName(d); got != "74-2022-TT-BTC - Thông tư quy định.pdf" {
		t.Fatalf("PDFFileName = %q", got)
	}
	if !d.HasPDF() || d.HasHTML() {
		t.Fatalf("HasPDF/HasHTML wrong for %+v", d)
	}
}
