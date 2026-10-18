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
