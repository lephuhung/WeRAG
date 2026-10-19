package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type publicBillingStub struct {
	interfaces.UsageBillingService
	scope string
	err   error
	plans []*types.UsagePlan
}

func (s *publicBillingStub) PurchasablePlans(_ context.Context, scope string) ([]*types.UsagePlanOffer, error) {
	s.scope = scope
	offers := make([]*types.UsagePlanOffer, 0, len(s.plans))
	for _, p := range s.plans {
		if p == nil {
			offers = append(offers, nil)
			continue
		}
		offers = append(offers, &types.UsagePlanOffer{UsagePlan: p})
	}
	return offers, s.err
}

func publicBillingTestRouter(s *publicBillingStub) *gin.Engine {
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	RegisterPublicBillingRoutes(r, handler.NewUsageBillingHandler(s))
	// As in NewRouter, public routes are registered before account auth.
	r.Use(func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	r.GET("/private", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestPublicBillingPlansAnonymousCatalogue(t *testing.T) {
	s := &publicBillingStub{plans: []*types.UsagePlan{
		{ID: "live", Name: "Live plan", IsActive: true, PriceVND: 100000, Features: types.JSON(`{"internal":"hidden"}`)},
		{ID: "inactive", IsActive: false, PriceVND: 100000},
		{ID: "not-for-sale", IsActive: true, PriceVND: 0},
		nil,
	}}
	r := publicBillingTestRouter(s)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/billing/plans?scope=user", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("anonymous status = %d: %s", w.Code, w.Body.String())
	}
	if s.scope != "user" {
		t.Fatalf("scope = %q", s.scope)
	}
	var body struct {
		Success bool             `json:"success"`
		Data    []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || len(body.Data) != 1 || body.Data[0]["id"] != "live" {
		t.Fatalf("catalogue = %s", w.Body.String())
	}
	for _, key := range []string{"features", "created_at", "updated_at"} {
		if _, exists := body.Data[0][key]; exists {
			t.Fatalf("unexpected field %q", key)
		}
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/private", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("private route status = %d", w.Code)
	}
}

func TestPublicBillingPlansScopeAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		err         error
		status      int
	}{
		{"all", "", nil, http.StatusOK},
		{"tenant", "?scope=tenant", nil, http.StatusOK},
		{"invalid", "?scope=admin", nil, http.StatusBadRequest},
		{"failure", "?scope=user", errors.New("database unavailable"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &publicBillingStub{err: tc.err}
			w := httptest.NewRecorder()
			publicBillingTestRouter(s).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/billing/plans"+tc.query, nil))
			if w.Code != tc.status {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			if tc.status == http.StatusOK && w.Body.String() != "{\"data\":[],\"success\":true}" {
				t.Fatalf("empty catalogue = %s", w.Body.String())
			}
		})
	}
}
