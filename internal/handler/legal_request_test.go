package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
)

type fakeLegalRequestService struct {
	status        string
	offset, limit int
	decidedID     string
	decision      *types.LegalRequestDecision
	decideErr     error
}

func (f *fakeLegalRequestService) List(_ context.Context, status string, offset, limit int) ([]*types.LegalRequestItem, int64, map[string]int64, error) {
	f.status, f.offset, f.limit = status, offset, limit
	row := &types.LegalDocumentRequest{ID: "r1", NumberKey: "53/2022/ND-CP", DocumentNumber: "53/2022/NĐ-CP",
		LegalDocumentID: "155366", Status: types.LegalRequestRequested, RequestCount: 3}
	return []*types.LegalRequestItem{{LegalDocumentRequest: row, SuggestedKBID: "kb-nd",
			Document: &types.LegalRequestDocument{ID: "155366", Title: "Quy định chi tiết", URL: "https://vbpl.vn/van-ban/chi-tiet/155366"}}},
		1, map[string]int64{"requested": 1, "added": 0, "rejected": 2}, nil
}

func (f *fakeLegalRequestService) Decide(_ context.Context, id string, d *types.LegalRequestDecision) (*types.LegalRequestItem, error) {
	f.decidedID, f.decision = id, d
	if f.decideErr != nil {
		return nil, f.decideErr
	}
	return &types.LegalRequestItem{LegalDocumentRequest: &types.LegalDocumentRequest{ID: id, Status: types.LegalRequestRejected,
		DecisionNote: d.Note}}, nil
}

func (f *fakeLegalRequestService) PublicKnowledgeBases(context.Context) ([]types.LegalPublicKB, error) {
	return []types.LegalPublicKB{{ID: "kb-nd", Name: "VBPL – Nghị định"}}, nil
}

func newLegalRequestRouter(svc *fakeLegalRequestService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := NewLegalRequestHandler(svc)
	r.GET("/system/legal-requests", h.ListLegalRequests)
	r.GET("/system/legal-requests/public-knowledge-bases", h.ListLegalRequestKnowledgeBases)
	r.PUT("/system/legal-requests/:id", h.DecideLegalRequest)
	return r
}

func TestLegalRequestHandlerList(t *testing.T) {
	svc := &fakeLegalRequestService{}
	w := httptest.NewRecorder()
	newLegalRequestRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/system/legal-requests?page=2&page_size=10", nil))
	if w.Code != http.StatusOK || svc.status != "requested" || svc.offset != 10 || svc.limit != 10 {
		t.Fatalf("code %d status %q offset %d limit %d: %s", w.Code, svc.status, svc.offset, svc.limit, w.Body.String())
	}
	var body struct {
		Data struct {
			Items  []map[string]any `json:"items"`
			Total  int64            `json:"total"`
			Counts map[string]int64 `json:"counts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	item := body.Data.Items[0]
	// the request's own fields are flat on the item, beside document and suggested_kb_id
	if body.Data.Total != 1 || body.Data.Counts["rejected"] != 2 || item["id"] != "r1" || item["request_count"] != float64(3) ||
		item["suggested_kb_id"] != "kb-nd" || item["document"].(map[string]any)["url"] != "https://vbpl.vn/van-ban/chi-tiet/155366" {
		t.Fatalf("body = %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	newLegalRequestRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/system/legal-requests?status=all", nil))
	if w.Code != http.StatusOK || svc.status != "all" {
		t.Fatalf("status all: %d %q", w.Code, svc.status)
	}
	w = httptest.NewRecorder()
	newLegalRequestRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/system/legal-requests/public-knowledge-bases", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "VBPL – Nghị định") {
		t.Fatalf("public KBs: %d %s", w.Code, w.Body.String())
	}
}

func TestLegalRequestHandlerDecide(t *testing.T) {
	svc := &fakeLegalRequestService{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/system/legal-requests/r1",
		strings.NewReader(`{"action":"reject","note":"Ngoài phạm vi"}`))
	req.Header.Set("Content-Type", "application/json")
	newLegalRequestRouter(svc).ServeHTTP(w, req)
	if w.Code != http.StatusOK || svc.decidedID != "r1" || svc.decision.Action != "reject" || !strings.Contains(w.Body.String(), "Ngoài phạm vi") {
		t.Fatalf("decide: %d %s", w.Code, w.Body.String())
	}

	// a refused ingest (fully expired, no full text) comes back as 4xx
	svc.decideErr = apperrors.NewBadRequestError(`document 1 (27/2015/NĐ-CP) is "Hết hiệu lực toàn bộ"`)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/system/legal-requests/r1",
		strings.NewReader(`{"action":"ingest","knowledge_base_id":"kb-nd"}`))
	req.Header.Set("Content-Type", "application/json")
	newLegalRequestRouter(svc).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || svc.decision.KnowledgeBaseID != "kb-nd" || !strings.Contains(w.Body.String(), "Hết hiệu lực") {
		t.Fatalf("refused ingest: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/system/legal-requests/r1", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	newLegalRequestRouter(svc).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", w.Code)
	}
}
