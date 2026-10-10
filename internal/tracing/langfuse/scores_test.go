package langfuse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newScoreTestManager(t *testing.T, host string) *Manager {
	t.Helper()
	m, err := Init(Config{
		Enabled:        true,
		Host:           host,
		PublicKey:      "pk",
		SecretKey:      "sk",
		Environment:    "dev",
		RequestTimeout: 2 * time.Second,
		SampleRate:     1.0,
		testExporter:   tracetest.NewInMemoryExporter(),
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
	return m
}

func TestUpsertScorePostsNumericScore(t *testing.T) {
	var got map[string]interface{}
	var user, pass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/public/scores" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		user, pass, _ = r.BasicAuth()
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := newScoreTestManager(t, srv.URL)
	err := m.UpsertScore(context.Background(), Score{
		ID: "user-feedback-m1", TraceID: "t1", Name: "user-feedback", Value: -1, Comment: "sai luật",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user != "pk" || pass != "sk" {
		t.Fatalf("auth = %q:%q", user, pass)
	}
	want := map[string]interface{}{
		"id": "user-feedback-m1", "traceId": "t1", "name": "user-feedback", "value": float64(-1),
		"dataType": "NUMERIC", "comment": "sai luật", "environment": "dev",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %#v, want %#v (body %v)", k, got[k], v, got)
		}
	}

	if err := m.UpsertScore(context.Background(), Score{ID: "user-feedback-m1", TraceID: "t1", Name: "user-feedback", Value: 1}); err != nil {
		t.Fatal(err)
	}
	if c, ok := got["comment"]; !ok || c != "" {
		t.Fatalf("a vote without a note must send an empty comment to replace the old one, got %#v", got)
	}
}

func TestUpsertScoreReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad trace", http.StatusBadRequest)
	}))
	defer srv.Close()

	m := newScoreTestManager(t, srv.URL)
	if err := m.UpsertScore(context.Background(), Score{ID: "a", TraceID: "t", Name: "n", Value: 1}); err == nil {
		t.Fatal("a 400 must surface as an error")
	}
}

func TestDeleteScoreIgnoresMissingScore(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	m := newScoreTestManager(t, srv.URL)
	if err := m.DeleteScore(context.Background(), "user-feedback-m1"); err != nil {
		t.Fatal(err)
	}
	if path != "DELETE /api/public/scores/user-feedback-m1" {
		t.Fatalf("request = %q", path)
	}
}

func TestScoresAreNoOpsWhenDisabled(t *testing.T) {
	var m *Manager
	if err := m.UpsertScore(context.Background(), Score{}); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteScore(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
}

func TestTraceIDFromTraceparent(t *testing.T) {
	cases := map[string]string{
		"00-37aadf8efce25560115039888de47f27-f9a7efb6c86129a2-01": "37aadf8efce25560115039888de47f27",
		"00-00000000000000000000000000000000-f9a7efb6c86129a2-01": "",
		"00-37AADF8EFCE25560115039888DE47F27-f9a7efb6c86129a2-01": "",
		"":         "",
		"garbage":  "",
		"00-ab-cd": "",
	}
	for in, want := range cases {
		if got := TraceIDFromTraceparent(in); got != want {
			t.Errorf("TraceIDFromTraceparent(%q) = %q, want %q", in, got, want)
		}
	}
}
