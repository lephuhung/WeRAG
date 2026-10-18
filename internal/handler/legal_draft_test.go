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

type fakeLegalDraftService struct {
	fetched, opened, approvedKB, discarded, status string
	offset, limit                                  int
	viewer                                         types.LegalDraftViewer
	err                                            error
}

func (f *fakeLegalDraftService) FetchForRequest(_ context.Context, id string) (*types.LegalDocumentDraft, error) {
	f.fetched = id
	if f.err != nil {
		return nil, f.err
	}
	return &types.LegalDocumentDraft{ID: "d1", RequestID: id, LegalDocumentID: "3003", Status: types.LegalDraftOCR}, nil
}

func (f *fakeLegalDraftService) FetchDocument(context.Context, string, string) (*types.LegalDocumentDraft, error) {
	return nil, nil
}

func (f *fakeLegalDraftService) Get(_ context.Context, id string) (*types.LegalDraftItem, error) {
	if id != "d1" {
		return nil, apperrors.NewNotFoundError("legal draft not found")
	}
	return &types.LegalDraftItem{LegalDocumentDraft: &types.LegalDocumentDraft{ID: "d1", OCRMarkdown: "Điều 1."},
		Request: &types.LegalDocumentRequest{ID: "r1"}}, nil
}

func (f *fakeLegalDraftService) List(_ context.Context, status string, offset, limit int) ([]*types.LegalDraftItem, int64, error) {
	f.status, f.offset, f.limit = status, offset, limit
	return nil, 0, nil
}

func (f *fakeLegalDraftService) Open(_ context.Context, id string, viewer types.LegalDraftViewer) (*types.LegalDraftOpenResult, error) {
	f.opened, f.viewer = id, viewer
	return &types.LegalDraftOpenResult{Draft: &types.LegalDocumentDraft{ID: id}, SessionID: "s1", DocumentID: "ws1",
		TenantID: types.PlatformTenantID, PDFURL: "/r/tok",
		Editor: &types.DocumentEditorConfig{DocumentServerURL: "http://ds", Config: map[string]interface{}{"documentType": "word"}}}, nil
}

func (f *fakeLegalDraftService) Structure(_ context.Context, id string) (*types.LegalDraftStructure, error) {
	return &types.LegalDraftStructure{Ready: true, Articles: []types.LegalDraftArticle{{Number: "1"}}}, nil
}

func (f *fakeLegalDraftService) Approve(_ context.Context, id, kbID string) (*types.LegalDraftApproveResult, error) {
	f.approvedKB = kbID
	if f.err != nil {
		return nil, f.err
	}
	return &types.LegalDraftApproveResult{Draft: &types.LegalDocumentDraft{ID: id, Status: types.LegalDraftApproved},
		Result: "created", KnowledgeID: "k1", Articles: 3}, nil
}

func (f *fakeLegalDraftService) Discard(_ context.Context, id string) error {
	f.discarded = id
	return f.err
}

func (f *fakeLegalDraftService) Summaries(context.Context, []string) (map[string]*types.LegalDraftSummary, error) {
	return nil, nil
}

func newLegalDraftRouter(svc *fakeLegalDraftService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, "u-super")
		ctx = context.WithValue(ctx, types.UserContextKey, &types.User{ID: "u-super", Username: "super"})
		c.Request = c.Request.WithContext(ctx)
	})
	h := NewLegalDraftHandler(svc, nil)
	r.POST("/system/legal-requests/:id/fetch", h.FetchLegalDraft)
	r.GET("/system/legal-drafts", h.ListLegalDrafts)
	r.GET("/system/legal-drafts/:id", h.GetLegalDraft)
	r.POST("/system/legal-drafts/:id/open", h.OpenLegalDraft)
	r.GET("/system/legal-drafts/:id/structure", h.LegalDraftStructure)
	r.POST("/system/legal-drafts/:id/approve", h.ApproveLegalDraft)
	r.DELETE("/system/legal-drafts/:id", h.DiscardLegalDraft)
	return r
}

func serveDraft(r *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestLegalDraftHandler(t *testing.T) {
	svc := &fakeLegalDraftService{}
	r := newLegalDraftRouter(svc)

	w := serveDraft(r, http.MethodPost, "/system/legal-requests/r1/fetch", "", nil)
	if w.Code != http.StatusOK || svc.fetched != "r1" || !strings.Contains(w.Body.String(), `"status":"ocr"`) {
		t.Fatalf("fetch: %d %s", w.Code, w.Body.String())
	}
	svc.err = apperrors.NewBadRequestError("vbpl.vn có toàn văn HTML")
	if w = serveDraft(r, http.MethodPost, "/system/legal-requests/r1/fetch", "", nil); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "toàn văn HTML") {
		t.Fatalf("refused fetch: %d %s", w.Code, w.Body.String())
	}
	svc.err = nil

	w = serveDraft(r, http.MethodGet, "/system/legal-drafts?page=3&page_size=20", "", nil)
	if w.Code != http.StatusOK || svc.status != "all" || svc.offset != 40 || svc.limit != 20 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("list: %d %q %d %d %s", w.Code, svc.status, svc.offset, svc.limit, w.Body.String())
	}
	if w = serveDraft(r, http.MethodGet, "/system/legal-drafts/d1", "", nil); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `"ocr_markdown":"Điều 1."`) || !strings.Contains(w.Body.String(), `"request":{"id":"r1"`) {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	if w = serveDraft(r, http.MethodGet, "/system/legal-drafts/x", "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d", w.Code)
	}

	w = serveDraft(r, http.MethodPost, "/system/legal-drafts/d1/open", "", map[string]string{
		"Referer": "https://werag.example/settings/legal-drafts/d1", "Accept-Language": "en-US,en;q=0.9"})
	var open struct {
		Data struct {
			SessionID  string                 `json:"session_id"`
			DocumentID string                 `json:"document_id"`
			TenantID   uint64                 `json:"tenant_id"`
			PDFURL     string                 `json:"pdf_url"`
			Editor     map[string]interface{} `json:"editor"`
		} `json:"data"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &open) != nil || open.Data.SessionID != "s1" ||
		open.Data.TenantID != types.PlatformTenantID || open.Data.PDFURL != "/r/tok" || open.Data.Editor["document_server_url"] != "http://ds" {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	if svc.viewer.UserID != "u-super" || svc.viewer.UserName != "super" || svc.viewer.Origin != "https://werag.example" || svc.viewer.Lang != "en" {
		t.Fatalf("viewer = %+v", svc.viewer)
	}

	if w = serveDraft(r, http.MethodGet, "/system/legal-drafts/d1/structure", "", nil); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `"ready_to_approve":true`) {
		t.Fatalf("structure: %d %s", w.Code, w.Body.String())
	}

	if w = serveDraft(r, http.MethodPost, "/system/legal-drafts/d1/approve", `{"knowledge_base_id":"kb-tt"}`, nil); w.Code != http.StatusOK ||
		svc.approvedKB != "kb-tt" || !strings.Contains(w.Body.String(), `"knowledge_id":"k1"`) {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	if w = serveDraft(r, http.MethodPost, "/system/legal-drafts/d1/approve", `{`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("approve bad json: %d", w.Code)
	}
	svc.err = apperrors.NewBadRequestError("the draft is not ready to approve")
	if w = serveDraft(r, http.MethodPost, "/system/legal-drafts/d1/approve", `{"knowledge_base_id":"kb-tt"}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("approve refused: %d", w.Code)
	}
	svc.err = nil

	if w = serveDraft(r, http.MethodDelete, "/system/legal-drafts/d1", "", nil); w.Code != http.StatusOK || svc.discarded != "d1" {
		t.Fatalf("discard: %d %s", w.Code, w.Body.String())
	}
	svc.err = apperrors.NewConflictError("approved")
	if w = serveDraft(r, http.MethodDelete, "/system/legal-drafts/d1", "", nil); w.Code != http.StatusConflict {
		t.Fatalf("discard approved: %d", w.Code)
	}
}
