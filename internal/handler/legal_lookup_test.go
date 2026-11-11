package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

type fakeLegalLookup struct {
	gotQuery string
	calls    int
	err      error
}

func (f *fakeLegalLookup) LookupValidity(_ context.Context, q string, _ int) (*types.LegalValidityLookup, error) {
	f.calls++
	f.gotQuery = q
	if f.err != nil {
		return nil, f.err
	}
	return &types.LegalValidityLookup{Query: q, By: "number", CatalogSnapshotDate: "2026-11-10",
		Matches: []types.LegalValidityMatch{{
			LegalValidityRef: types.LegalValidityRef{ID: "d1", DocumentNumber: "30/2020/NĐ-CP",
				Status: types.LegalStatusKindInForce},
			StatusText: "Còn hiệu lực",
		}}}, nil
}

func serveLegalLookup(f *fakeLegalLookup, rawQuery string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			if e, ok := c.Errors.Last().Err.(*apperrors.AppError); ok {
				c.AbortWithStatusJSON(e.HTTPCode, gin.H{"error": e.Message})
			}
		}
	})
	r.GET("/legal-lookup", NewLegalLookupHandler(f, nil).LookupLegalValidity)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/legal-lookup?"+rawQuery, nil))
	return w
}

func TestLookupLegalValidity(t *testing.T) {
	f := &fakeLegalLookup{}
	w := serveLegalLookup(f, "q="+url.QueryEscape("  30/2020/NĐ-CP "))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", w.Code, w.Body.String())
	}
	if f.gotQuery != "30/2020/NĐ-CP" {
		t.Fatalf("query = %q, want it trimmed", f.gotQuery)
	}
	var body struct {
		Success bool                      `json:"success"`
		Data    types.LegalValidityLookup `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.CatalogSnapshotDate != "2026-11-10" || len(body.Data.Matches) != 1 ||
		body.Data.Matches[0].Status != types.LegalStatusKindInForce {
		t.Fatalf("body = %+v", body)
	}
	// the JSON field names the frontend reads
	for _, field := range []string{`"catalog_snapshot_date"`, `"status_text"`, `"replaced_by"`, `"amended_by"`,
		`"ended_by"`, `"document_number"`, `"knowledge"`} {
		if !strings.Contains(w.Body.String(), field) {
			t.Errorf("response lacks %s", field)
		}
	}
}

func TestLookupLegalValidityRejectsBadQueries(t *testing.T) {
	f := &fakeLegalLookup{}
	if w := serveLegalLookup(f, "q=%20%20"); w.Code != http.StatusBadRequest {
		t.Fatalf("blank q = %d, want 400", w.Code)
	}
	if w := serveLegalLookup(f, "q="+strings.Repeat("ạ", 201)); w.Code != http.StatusBadRequest {
		t.Fatalf("long q = %d, want 400", w.Code)
	}
	if f.calls != 0 {
		t.Fatal("bad queries must not reach the catalogue")
	}
	f.err = errors.New("db down")
	if w := serveLegalLookup(f, "q=53"); w.Code != http.StatusInternalServerError {
		t.Fatalf("service error = %d, want 500", w.Code)
	}
}

type fakeLookupMisses struct {
	byCount       bool
	offset, limit int
	deleted       uint64
}

func (f *fakeLookupMisses) ListMisses(_ context.Context, byCount bool, offset, limit int) ([]*types.LegalLookupMiss, int64, error) {
	f.byCount, f.offset, f.limit = byCount, offset, limit
	return []*types.LegalLookupMiss{{ID: 3, Query: "777/2099/nđ-cp", Count: 2, Reason: types.LegalLookupMissNumber}}, 1, nil
}

func (f *fakeLookupMisses) DeleteMiss(_ context.Context, id uint64) error {
	f.deleted = id
	return nil
}

func TestLegalLookupMissEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := &fakeLookupMisses{}
	h := NewLegalLookupHandler(&fakeLegalLookup{}, m)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			if e, ok := c.Errors.Last().Err.(*apperrors.AppError); ok {
				c.AbortWithStatus(e.HTTPCode)
			}
		}
	})
	r.GET("/misses", h.ListLegalLookupMisses)
	r.DELETE("/misses/:id", h.DeleteLegalLookupMiss)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/misses?page=2&page_size=5&sort=recent", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) ||
		!strings.Contains(w.Body.String(), `"reason":"number_missing"`) {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}
	if m.byCount || m.offset != 5 || m.limit != 5 {
		t.Fatalf("list args = %+v", m)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/misses?page_size=1000", nil))
	if !m.byCount || m.limit != 20 {
		t.Fatalf("default sort/size = %+v", m)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/misses/3", nil))
	if w.Code != http.StatusOK || m.deleted != 3 {
		t.Fatalf("delete = %d, deleted %d", w.Code, m.deleted)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/misses/x", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", w.Code)
	}
}
