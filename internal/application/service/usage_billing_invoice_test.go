package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

var testInvoice = types.UsageInvoiceDetails{
	CompanyName: "  Công ty TNHH   Hưng Phát ",
	TaxCode:     "0101 234 567",
	Address:     "12 Lê Lợi,\nHuế",
	Email:       "ketoan@hungphat.vn",
}

func TestVNTaxCodeShape(t *testing.T) {
	for _, ok := range []string{"0101234567", "0101234567-001"} {
		require.True(t, types.ValidVNTaxCode(ok), ok)
	}
	for _, bad := range []string{"", "010123456", "01012345678", "0101234567-01", "0101234567-0011",
		"0101234567001", "010123456A", "0101234567 -001", "-0101234567", "0101234567-"} {
		require.False(t, types.ValidVNTaxCode(bad), bad)
	}
	require.Equal(t, "0101234567-001", types.NormalizeVNTaxCode(" 0101 234 567 - 001 "))
}

func TestInvoiceDetailsNormalize(t *testing.T) {
	d, reason := testInvoice.Normalize()
	require.Empty(t, reason)
	require.Equal(t, "Công ty TNHH Hưng Phát", d.CompanyName)
	require.Equal(t, "0101234567", d.TaxCode)
	require.Equal(t, "12 Lê Lợi, Huế", d.Address)

	cases := map[string]func(*types.UsageInvoiceDetails){
		"invoice_company_required": func(d *types.UsageInvoiceDetails) { d.CompanyName = "  " },
		"invoice_tax_code_invalid": func(d *types.UsageInvoiceDetails) { d.TaxCode = "12345" },
		"invoice_address_required": func(d *types.UsageInvoiceDetails) { d.Address = "" },
		"invoice_email_invalid":    func(d *types.UsageInvoiceDetails) { d.Email = "Kế toán <kt@x.vn>" },
	}
	for want, mutate := range cases {
		d := testInvoice
		mutate(&d)
		_, reason := d.Normalize()
		require.Equal(t, want, reason)
	}
	for _, bad := range []string{"kt", "kt@", "kt@localhost", "a@b.vn,c@d.vn", "a b@c.vn"} {
		d := testInvoice
		d.Email = bad
		_, reason := d.Normalize()
		require.Equal(t, "invoice_email_invalid", reason, bad)
	}
}

// The browser checks emails the same way (frontend-next
// lib/billing-invoice.test.ts carries this table too): keep them in step.
func TestInvoiceEmailSameAsBrowser(t *testing.T) {
	cases := map[string]bool{
		"kt@a.vn": true, "a+b@x.vn": true, "Nguyễn@x.vn": true, "a@[1.2.3.4]": true, "a@x_y.vn": true,
		"a!#$%&'*/=?^_`{|}~-@x.vn": true, "a@1.2": true,
		".a@x.vn": false, "a..b@x.vn": false, `"a"@x.vn`: false, "a@x..vn": false, "a@x.vn.": false,
		"kt@localhost": false, "a@b@c.vn": false, "a(c)@x.vn": false, `a\b@x.vn`: false,
	}
	for email, want := range cases {
		d := testInvoice
		d.Email = email
		_, reason := d.Normalize()
		require.Equal(t, want, reason == "", email)
	}
}

func TestOrderWithInvoiceAndPrefill(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()

	none, err := f.svc.LastInvoiceDetails(ctx, "tenant", "10003")
	require.NoError(t, err)
	require.Nil(t, none)

	// bad details refuse the order before anything is written
	bad := testInvoice
	bad.TaxCode = "0101234567-1"
	_, err = f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, Invoice: &bad}, "10003", "u1")
	require.Equal(t, "invoice_tax_code_invalid", badRequestReason(t, err))
	require.Empty(t, f.repo.orders)

	inv := testInvoice
	o, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, Invoice: &inv}, "10003", "u1")
	require.NoError(t, err)
	require.True(t, o.InvoiceRequested)
	require.Equal(t, "0101234567", o.InvoiceTaxCode)
	require.Equal(t, "Công ty TNHH Hưng Phát", f.repo.orders[o.ID].InvoiceCompanyName)

	// a later order without an invoice does not hide the last details
	f.svc.now = func() time.Time { return billingNow.Add(time.Hour) }
	_, err = f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, ConfirmDuplicate: true}, "10003", "u1")
	require.NoError(t, err)
	got, err := f.svc.LastInvoiceDetails(ctx, "tenant", "10003")
	require.NoError(t, err)
	require.Equal(t, "0101234567", got.TaxCode)
	require.Equal(t, "12 Lê Lợi, Huế", got.Address)

	// another scope has its own
	other, err := f.svc.LastInvoiceDetails(ctx, "user", "u1")
	require.NoError(t, err)
	require.Nil(t, other)
}

func TestInvoiceEditRules(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)
	upd := types.UsageInvoiceUpdate{Requested: true, UsageInvoiceDetails: testInvoice}

	// pending: may be asked for, changed and withdrawn
	v, err := f.svc.UpdateOrderInvoice(ctx, o.ID, upd)
	require.NoError(t, err)
	require.True(t, v.InvoiceRequested)
	require.Equal(t, "0101234567", v.InvoiceTaxCode)
	v, err = f.svc.UpdateOrderInvoice(ctx, o.ID, types.UsageInvoiceUpdate{Requested: false})
	require.NoError(t, err)
	require.False(t, v.InvoiceRequested)
	require.Empty(t, v.InvoiceCompanyName)

	bad := upd
	bad.TaxCode = "abc"
	_, err = f.svc.UpdateOrderInvoice(ctx, o.ID, bad)
	require.Equal(t, "invoice_tax_code_invalid", badRequestReason(t, err))

	// paid: still editable until the invoice is issued
	_, err = f.svc.ConfirmOrder(ctx, o.ID, "admin")
	require.NoError(t, err)
	upd.TaxCode = "0101234567-002"
	v, err = f.svc.UpdateOrderInvoice(ctx, o.ID, upd)
	require.NoError(t, err)
	require.Equal(t, "0101234567-002", v.InvoiceTaxCode)
	require.Equal(t, types.UsageOrderPaid, v.Status)

	// issued: frozen
	_, err = f.svc.MarkInvoiceIssued(ctx, o.ID, "admin", types.UsageInvoiceIssue{InvoiceNo: "123"})
	require.NoError(t, err)
	_, err = f.svc.UpdateOrderInvoice(ctx, o.ID, upd)
	require.Equal(t, 409, appErrHTTP(t, err))
	require.Equal(t, "invoice_issued", appErrReason(t, err)["reason"])
	_, err = f.svc.UpdateOrderInvoice(ctx, o.ID, types.UsageInvoiceUpdate{Requested: false})
	require.Equal(t, "invoice_issued", appErrReason(t, err)["reason"])
	require.True(t, f.repo.orders[o.ID].InvoiceRequested)

	// undone by a SuperAdmin: editable again
	_, err = f.svc.UnmarkInvoiceIssued(ctx, o.ID)
	require.NoError(t, err)
	_, err = f.svc.UpdateOrderInvoice(ctx, o.ID, upd)
	require.NoError(t, err)

	// cancelled and expired orders are closed
	c := f.poolOrder(t)
	_, err = f.svc.CancelOrder(ctx, c.ID, false)
	require.NoError(t, err)
	_, err = f.svc.UpdateOrderInvoice(ctx, c.ID, upd)
	require.Equal(t, "invoice_order_closed", appErrReason(t, err)["reason"])

	e, err := f.svc.CreateOrder(ctx, types.UsageQuoteRequest{ScopeType: "user", PlanID: "me", Months: 1}, "u1", "u1")
	require.NoError(t, err)
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	_, err = f.svc.UpdateOrderInvoice(ctx, e.ID, upd)
	require.Equal(t, "invoice_order_closed", appErrReason(t, err)["reason"])
	require.Equal(t, types.UsageOrderExpired, f.repo.orders[e.ID].Status)

	_, err = f.svc.UpdateOrderInvoice(ctx, "nope", upd)
	require.Equal(t, 404, appErrHTTP(t, err))
}

func TestMarkInvoiceIssued(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	o := f.poolOrder(t)

	// pending: nothing to issue yet
	_, err := f.svc.UpdateOrderInvoice(ctx, o.ID, types.UsageInvoiceUpdate{Requested: true, UsageInvoiceDetails: testInvoice})
	require.NoError(t, err)
	_, err = f.svc.MarkInvoiceIssued(ctx, o.ID, "admin", types.UsageInvoiceIssue{})
	require.Equal(t, "order_not_paid", appErrReason(t, err)["reason"])

	_, err = f.svc.ConfirmOrder(ctx, o.ID, "admin")
	require.NoError(t, err)
	counts, err := f.svc.AdminCounts(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, counts.InvoicesToIssue)

	_, err = f.svc.MarkInvoiceIssued(ctx, o.ID, "admin", types.UsageInvoiceIssue{Date: "03/11/2026"})
	require.Equal(t, "invoice_date_invalid", badRequestReason(t, err))
	_, err = f.svc.MarkInvoiceIssued(ctx, o.ID, "admin", types.UsageInvoiceIssue{InvoiceSeries: "1C26TAAAAAAAAAAAAAA"})
	require.Equal(t, "invoice_too_long", badRequestReason(t, err))

	// no date = today in Vietnam (billingNow is 13/10 17:00 VN)
	v, err := f.svc.MarkInvoiceIssued(ctx, o.ID, "admin", types.UsageInvoiceIssue{InvoiceNo: " 0000123 ", InvoiceSeries: "1C26TAA"})
	require.NoError(t, err)
	require.True(t, v.InvoiceIssued())
	require.Equal(t, "0000123", v.InvoiceNo)
	require.Equal(t, "1C26TAA", v.InvoiceSeries)
	require.Equal(t, "admin", v.InvoiceIssuedBy)
	require.Equal(t, "2026-10-13", v.InvoiceDate.In(types.UsageLocation()).Format("2006-01-02"))
	firstAt := *v.InvoiceIssuedAt

	counts, err = f.svc.AdminCounts(ctx)
	require.NoError(t, err)
	require.Zero(t, counts.InvoicesToIssue)

	// marking again corrects the number and date, keeps who/when
	f.svc.now = func() time.Time { return billingNow.Add(24 * time.Hour) }
	v, err = f.svc.MarkInvoiceIssued(ctx, o.ID, "admin2", types.UsageInvoiceIssue{InvoiceNo: "0000124", Date: "2026-10-12"})
	require.NoError(t, err)
	require.Equal(t, "0000124", v.InvoiceNo)
	require.Empty(t, v.InvoiceSeries)
	require.Equal(t, "admin", v.InvoiceIssuedBy)
	require.True(t, firstAt.Equal(*v.InvoiceIssuedAt))
	require.Equal(t, "2026-10-12", v.InvoiceDate.In(types.UsageLocation()).Format("2006-01-02"))

	v, err = f.svc.UnmarkInvoiceIssued(ctx, o.ID)
	require.NoError(t, err)
	require.False(t, v.InvoiceIssued())
	require.Empty(t, v.InvoiceNo)
	require.Nil(t, v.InvoiceDate)

	// a paid order that did not ask for one
	p := f.poolOrder(t)
	_, err = f.svc.ConfirmOrder(ctx, p.ID, "admin")
	require.NoError(t, err)
	_, err = f.svc.MarkInvoiceIssued(ctx, p.ID, "admin", types.UsageInvoiceIssue{})
	require.Equal(t, "invoice_not_requested", appErrReason(t, err)["reason"])
}

func TestOrderReceipt(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	st := f.svc.settings.(billingSettings)
	st.strings[types.BillingSellerNameSettingKey] = " Công ty CP weRAG "
	st.strings[types.BillingSellerTaxCodeSettingKey] = "3300123456"
	st.strings[types.BillingContactSettingKey] = "0234 123 456"

	o := f.poolOrder(t)
	_, err := f.svc.OrderReceipt(ctx, o)
	require.Equal(t, "order_not_paid", appErrReason(t, err)["reason"])

	_, err = f.svc.ConfirmOrder(ctx, o.ID, "admin")
	require.NoError(t, err)
	paid, err := f.svc.GetOrderByCode(ctx, o.OrderCode)
	require.NoError(t, err)
	r, err := f.svc.OrderReceipt(ctx, paid)
	require.NoError(t, err)
	require.Equal(t, "Công ty CP weRAG", r.SellerName)
	require.Equal(t, "3300123456", r.SellerTaxCode)
	require.Empty(t, r.SellerAddress)
	require.Equal(t, "0234 123 456", r.SellerContact)
	require.Equal(t, "0011002233", r.BankAccountNo)
	require.Equal(t, "10003", r.BuyerName, "no tenant repo in the fixture: the scope id")
	require.Equal(t, o.OrderCode, r.Order.OrderCode)
	require.NotNil(t, r.Order.SubscriptionEndsAt)
}
