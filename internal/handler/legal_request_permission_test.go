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

// decideRouterWithCaller wires DecideLegalRequest behind a middleware that
// seeds the caller the same way applyAuthSession does.
func decideRouterWithCaller(svc *fakeLegalRequestService, caller types.Caller, systemAdmin, legalUploader bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CallerContextKey, caller.Normalize())
		ctx = context.WithValue(ctx, types.UserIDContextKey, caller.UserID)
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, systemAdmin)
		ctx = context.WithValue(ctx, types.LegalUploaderContextKey, legalUploader)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	h := NewLegalRequestHandler(svc)
	r.PUT("/system/legal-requests/:id", h.DecideLegalRequest)
	return r
}

func decide(t *testing.T, engine *gin.Engine, action string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(types.LegalRequestDecision{Action: action, KnowledgeID: "k1", KnowledgeBaseID: "kb1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/system/legal-requests/r1", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	return w
}

func TestDecideLegalRequest_NarrowUploaderMayOnlyCloseAsAdded(t *testing.T) {
	operator := types.Caller{TenantID: types.PlatformTenantID, UserID: "u-operator"}

	svc := &fakeLegalRequestService{}
	w := decide(t, decideRouterWithCaller(svc, operator, false, true), types.LegalRequestActionAdded)
	if w.Code != http.StatusOK {
		t.Fatalf("added should pass for the uploader, got %d: %s", w.Code, w.Body.String())
	}
	if svc.decision == nil || svc.decision.Action != types.LegalRequestActionAdded {
		t.Fatalf("service did not receive the decision: %+v", svc.decision)
	}

	for _, action := range []string{types.LegalRequestActionIngest, types.LegalRequestActionReject} {
		svc := &fakeLegalRequestService{}
		w := decide(t, decideRouterWithCaller(svc, operator, false, true), action)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s must be SuperAdmin-only, got %d: %s", action, w.Code, w.Body.String())
		}
		if svc.decision != nil {
			t.Fatalf("%s reached the service", action)
		}
	}
}

func TestDecideLegalRequest_SuperAdminKeepsEveryAction(t *testing.T) {
	admin := types.Caller{TenantID: types.PlatformTenantID, UserID: "u-admin"}
	for _, action := range []string{types.LegalRequestActionAdded, types.LegalRequestActionReject} {
		svc := &fakeLegalRequestService{}
		w := decide(t, decideRouterWithCaller(svc, admin, true, false), action)
		if w.Code != http.StatusOK {
			t.Fatalf("%s should pass for a SuperAdmin, got %d: %s", action, w.Code, w.Body.String())
		}
	}
}

func TestDecideLegalRequest_NarrowUploaderMayClaimAndRelease(t *testing.T) {
	operator := types.Caller{TenantID: types.PlatformTenantID, UserID: "u-operator"}

	for _, action := range []string{types.LegalRequestActionClaim, types.LegalRequestActionRelease} {
		svc := &fakeLegalRequestService{}
		w := decide(t, decideRouterWithCaller(svc, operator, false, true), action)
		if w.Code != http.StatusOK {
			t.Fatalf("%s should pass for the uploader, got %d: %s", action, w.Code, w.Body.String())
		}
		if svc.claimedID != "r1" || svc.claimAction != action {
			t.Fatalf("%s did not reach Claim: id=%q action=%q", action, svc.claimedID, svc.claimAction)
		}
		if svc.decision != nil {
			t.Fatalf("%s must not go through Decide", action)
		}
	}
}

func TestDecideLegalRequest_ClaimConflictIsConflict(t *testing.T) {
	operator := types.Caller{TenantID: types.PlatformTenantID, UserID: "u-operator"}
	svc := &fakeLegalRequestService{claimErr: apperrors.NewConflictError("another operator is fetching this document")}
	w := decide(t, decideRouterWithCaller(svc, operator, false, true), types.LegalRequestActionClaim)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409 for a foreign claim, got %d: %s", w.Code, w.Body.String())
	}
}
