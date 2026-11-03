package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// invoiceBillingStub serves two paid orders: WRTEN of Tổ chức 10003
// (bought by u-buyer) and WRME of person u-me; it records the calls that
// got past the access checks.
type invoiceBillingStub struct {
	interfaces.UsageBillingService
	calls []string
}

func (s *invoiceBillingStub) GetOrderByCode(_ context.Context, code string) (*types.UsageOrderView, error) {
	switch code {
	case "WRTEN":
		return &types.UsageOrderView{UsageOrder: &types.UsageOrder{ID: "o-ten", OrderCode: code,
			ScopeType: types.UsageScopeTenant, ScopeID: "10003", CreatedBy: "u-buyer", Status: types.UsageOrderPaid}}, nil
	case "WRME":
		return &types.UsageOrderView{UsageOrder: &types.UsageOrder{ID: "o-me", OrderCode: code,
			ScopeType: types.UsageScopeUser, ScopeID: "u-me", CreatedBy: "u-me", Status: types.UsageOrderPaid}}, nil
	}
	return nil, apperrors.NewNotFoundError("order not found")
}

func (s *invoiceBillingStub) UpdateOrderInvoice(_ context.Context, id string, req types.UsageInvoiceUpdate) (*types.UsageOrderView, error) {
	s.calls = append(s.calls, "invoice "+id+" "+req.TaxCode)
	return &types.UsageOrderView{UsageOrder: &types.UsageOrder{ID: id}}, nil
}

func (s *invoiceBillingStub) OrderReceipt(_ context.Context, o *types.UsageOrderView) (*types.UsageOrderReceipt, error) {
	s.calls = append(s.calls, "receipt "+o.ID)
	return &types.UsageOrderReceipt{Order: o, SellerName: "weRAG"}, nil
}

func (s *invoiceBillingStub) MarkInvoiceIssued(_ context.Context, id, by string, req types.UsageInvoiceIssue) (*types.UsageOrderView, error) {
	s.calls = append(s.calls, "issued "+id+" "+req.InvoiceNo+" by "+by)
	return &types.UsageOrderView{UsageOrder: &types.UsageOrder{ID: id}}, nil
}

func (s *invoiceBillingStub) UnmarkInvoiceIssued(_ context.Context, id string) (*types.UsageOrderView, error) {
	s.calls = append(s.calls, "unissued "+id)
	return &types.UsageOrderView{UsageOrder: &types.UsageOrder{ID: id}}, nil
}

func (s *invoiceBillingStub) LastInvoiceDetails(_ context.Context, scopeType, scopeID string) (*types.UsageInvoiceDetails, error) {
	s.calls = append(s.calls, "last "+scopeType+" "+scopeID)
	return &types.UsageInvoiceDetails{TaxCode: "0101234567"}, nil
}

type billingCaller struct {
	user  string
	role  types.TenantRole
	super bool
}

func serveInvoiceRoute(t *testing.T, stub *invoiceBillingStub, who billingCaller, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	RegisterBillingRoutes(engine.Group("/api/v1"), handler.NewUsageBillingHandler(stub), &rbacGuards{cfg: &config.Config{}})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), types.TenantIDContextKey, uint64(10003))
	ctx = context.WithValue(ctx, types.UserIDContextKey, who.user)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 10003, UserID: who.user, Role: who.role})
	if who.super {
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// The receipt and the invoice details of an order follow the order's
// access: its buyer, the Tổ chức admins for a Tổ chức order, a SuperAdmin;
// anyone else gets 404 and never reaches the service.
func TestBillingReceiptAndInvoiceAccess(t *testing.T) {
	buyer := billingCaller{user: "u-buyer", role: types.TenantRoleMember}
	admin := billingCaller{user: "u-admin", role: types.TenantRoleAdmin}
	member := billingCaller{user: "u-member", role: types.TenantRoleMember}
	me := billingCaller{user: "u-me", role: types.TenantRoleMember}
	super := billingCaller{user: "u-super", role: types.TenantRoleMember, super: true}
	body := `{"requested":true,"company_name":"A","tax_code":"0101234567","address":"Huế","email":"a@b.vn"}`

	cases := []struct {
		who  billingCaller
		code string
		ok   bool
	}{
		{buyer, "WRTEN", true}, {admin, "WRTEN", true}, {super, "WRTEN", true}, {member, "WRTEN", false},
		{me, "WRTEN", false}, {me, "WRME", true}, {admin, "WRME", false}, {super, "WRME", true}, {member, "WRME", false},
	}
	for _, c := range cases {
		stub := &invoiceBillingStub{}
		want := http.StatusNotFound
		if c.ok {
			want = http.StatusOK
		}
		rec := serveInvoiceRoute(t, stub, c.who, http.MethodGet, "/api/v1/billing/orders/"+c.code+"/receipt", "")
		if rec.Code != want {
			t.Fatalf("%s receipt %s = %d, want %d: %s", c.who.user, c.code, rec.Code, want, rec.Body.String())
		}
		rec = serveInvoiceRoute(t, stub, c.who, http.MethodPut, "/api/v1/billing/orders/"+c.code+"/invoice", body)
		if rec.Code != want {
			t.Fatalf("%s invoice %s = %d, want %d: %s", c.who.user, c.code, rec.Code, want, rec.Body.String())
		}
		if !c.ok && len(stub.calls) != 0 {
			t.Fatalf("refused caller reached the service: %v", stub.calls)
		}
		if c.ok && len(stub.calls) != 2 {
			t.Fatalf("calls = %v", stub.calls)
		}
	}

	stub := &invoiceBillingStub{}
	rec := serveInvoiceRoute(t, stub, buyer, http.MethodGet, "/api/v1/billing/orders/WRTEN/receipt", "")
	if !strings.Contains(rec.Body.String(), `"seller_name":"weRAG"`) || stub.calls[0] != "receipt o-ten" {
		t.Fatalf("receipt = %s %v", rec.Body.String(), stub.calls)
	}

	// prefill: the Tổ chức's needs a Tổ chức admin, the personal one is the caller's
	if rec := serveInvoiceRoute(t, &invoiceBillingStub{}, member, http.MethodGet, "/api/v1/billing/invoice-details?scope=tenant", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member, tenant prefill = %d", rec.Code)
	}
	stub = &invoiceBillingStub{}
	if rec := serveInvoiceRoute(t, stub, admin, http.MethodGet, "/api/v1/billing/invoice-details?scope=tenant", ""); rec.Code != http.StatusOK ||
		stub.calls[0] != "last tenant 10003" {
		t.Fatalf("admin, tenant prefill = %d %v", rec.Code, stub.calls)
	}
	stub = &invoiceBillingStub{}
	if rec := serveInvoiceRoute(t, stub, me, http.MethodGet, "/api/v1/billing/invoice-details?scope=user", ""); rec.Code != http.StatusOK ||
		stub.calls[0] != "last user u-me" {
		t.Fatalf("personal prefill = %d %v", rec.Code, stub.calls)
	}
}

// Marking an invoice issued is SuperAdmin-only.
func TestBillingInvoiceIssuedIsSuperAdminOnly(t *testing.T) {
	admin := billingCaller{user: "u-admin", role: types.TenantRoleOwner}
	super := billingCaller{user: "u-super", role: types.TenantRoleOwner, super: true}
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		stub := &invoiceBillingStub{}
		if rec := serveInvoiceRoute(t, stub, admin, method, "/api/v1/system/admin/usage/orders/o-ten/invoice-issued", `{"invoice_no":"12"}`); rec.Code != http.StatusForbidden {
			t.Fatalf("Tổ chức owner %s = %d, want 403", method, rec.Code)
		}
		if len(stub.calls) != 0 {
			t.Fatalf("refused caller reached the service: %v", stub.calls)
		}
	}
	stub := &invoiceBillingStub{}
	if rec := serveInvoiceRoute(t, stub, super, http.MethodPost, "/api/v1/system/admin/usage/orders/o-ten/invoice-issued", `{"invoice_no":"12"}`); rec.Code != http.StatusOK ||
		stub.calls[0] != "issued o-ten 12 by u-super" {
		t.Fatalf("SuperAdmin mark = %d %v", rec.Code, stub.calls)
	}
	// no body: everything optional
	stub = &invoiceBillingStub{}
	if rec := serveInvoiceRoute(t, stub, super, http.MethodPost, "/api/v1/system/admin/usage/orders/o-ten/invoice-issued", ""); rec.Code != http.StatusOK {
		t.Fatalf("SuperAdmin mark without body = %d %s", rec.Code, rec.Body.String())
	}
	stub = &invoiceBillingStub{}
	if rec := serveInvoiceRoute(t, stub, super, http.MethodDelete, "/api/v1/system/admin/usage/orders/o-ten/invoice-issued", ""); rec.Code != http.StatusOK ||
		stub.calls[0] != "unissued o-ten" {
		t.Fatalf("SuperAdmin unmark = %d %v", rec.Code, stub.calls)
	}
}
