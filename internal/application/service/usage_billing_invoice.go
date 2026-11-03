package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// VAT invoice requests and receipts of usage orders (see
// types/usage_billing_invoice.go): the buyer's invoice details, the
// SuperAdmin marking the invoice issued, and the receipt of a paid order.

// invoiceDetailsError is the 400 for details Normalize refused.
func invoiceDetailsError(reason string) error {
	return apperrors.NewBadRequestError("invalid VAT invoice details: " + reason).
		WithDetails(map[string]string{"reason": reason})
}

// normalizeInvoiceRequest checks the invoice details of a new order (nil =
// no invoice).
func normalizeInvoiceRequest(d *types.UsageInvoiceDetails) (*types.UsageInvoiceDetails, error) {
	if d == nil {
		return nil, nil
	}
	out, reason := d.Normalize()
	if reason != "" {
		return nil, invoiceDetailsError(reason)
	}
	return &out, nil
}

func (s *usageBillingService) LastInvoiceDetails(
	ctx context.Context, scopeType, scopeID string,
) (*types.UsageInvoiceDetails, error) {
	list, err := s.repo.ListOrders(ctx, types.UsageOrderFilter{
		ScopeType: scopeType, ScopeID: scopeID, InvoiceRequested: true, Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	d := list[0].InvoiceDetails()
	return &d, nil
}

// getOrderForUpdate locks order id in the transaction of ctx.
func (s *usageBillingService) getOrderForUpdate(ctx context.Context, id string) (*types.UsageOrder, error) {
	o, err := s.repo.GetOrderForUpdate(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("order not found")
	}
	return o, err
}

func (s *usageBillingService) UpdateOrderInvoice(
	ctx context.Context, id string, req types.UsageInvoiceUpdate,
) (*types.UsageOrderView, error) {
	var details types.UsageInvoiceDetails
	if req.Requested {
		var reason string
		if details, reason = req.UsageInvoiceDetails.Normalize(); reason != "" {
			return nil, invoiceDetailsError(reason)
		}
	}
	// an order past its deadline is closed, not editable
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	err := s.repo.InTransaction(ctx, func(ctx context.Context) error {
		o, err := s.getOrderForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if o.Status != types.UsageOrderPending && o.Status != types.UsageOrderPaid {
			return apperrors.NewConflictError("the invoice of a cancelled or expired order cannot be changed").
				WithDetails(map[string]string{"reason": "invoice_order_closed"})
		}
		if o.InvoiceIssued() {
			return apperrors.NewConflictError("the VAT invoice of this order has been issued; contact the system administrator to change it").
				WithDetails(map[string]string{"reason": "invoice_issued"})
		}
		if req.Requested {
			o.SetInvoiceDetails(details)
		} else {
			o.ClearInvoiceRequest()
		}
		return s.repo.UpdateOrder(ctx, o)
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrder(ctx, id)
}

// parseInvoiceDate reads YYYY-MM-DD as that day in Vietnam ("" = today).
func parseInvoiceDate(v string, now time.Time) (time.Time, error) {
	loc := types.UsageLocation()
	v = strings.TrimSpace(v)
	if v == "" {
		n := now.In(loc)
		return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc), nil
	}
	return time.ParseInLocation("2006-01-02", v, loc)
}

func (s *usageBillingService) MarkInvoiceIssued(
	ctx context.Context, id, by string, req types.UsageInvoiceIssue,
) (*types.UsageOrderView, error) {
	no := strings.TrimSpace(req.InvoiceNo)
	series := strings.TrimSpace(req.InvoiceSeries)
	if utf8.RuneCountInString(no) > types.UsageInvoiceNoMax || utf8.RuneCountInString(series) > types.UsageInvoiceSeriesMax {
		return nil, apperrors.NewBadRequestError("invoice number or series too long").
			WithDetails(map[string]string{"reason": "invoice_too_long"})
	}
	now := s.now()
	date, err := parseInvoiceDate(req.Date, now)
	if err != nil {
		return nil, apperrors.NewBadRequestError("date must be YYYY-MM-DD").
			WithDetails(map[string]string{"reason": "invoice_date_invalid"})
	}
	// a typo in the year (2062, 2016) is refused: at most a year either way
	today, _ := parseInvoiceDate("", now)
	if date.Before(today.AddDate(-1, 0, 0)) || date.After(today.AddDate(1, 0, 0)) {
		return nil, apperrors.NewBadRequestError("the invoice date must be within a year of today").
			WithDetails(map[string]string{"reason": "invoice_date_invalid"})
	}
	err = s.repo.InTransaction(ctx, func(ctx context.Context) error {
		o, err := s.getOrderForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if o.Status != types.UsageOrderPaid {
			return apperrors.NewConflictError("only a paid order has an invoice to issue").
				WithDetails(map[string]string{"reason": "order_not_paid"})
		}
		if !o.InvoiceRequested {
			return apperrors.NewConflictError("this order did not ask for a VAT invoice").
				WithDetails(map[string]string{"reason": "invoice_not_requested"})
		}
		// marking again corrects the number/series/date; the first marking
		// time and who did it stay
		if o.InvoiceIssuedAt == nil {
			t := now
			o.InvoiceIssuedAt = &t
			o.InvoiceIssuedBy = by
		}
		o.InvoiceNo, o.InvoiceSeries = no, series
		o.InvoiceDate = &date
		return s.repo.UpdateOrder(ctx, o)
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrder(ctx, id)
}

func (s *usageBillingService) UnmarkInvoiceIssued(ctx context.Context, id string) (*types.UsageOrderView, error) {
	err := s.repo.InTransaction(ctx, func(ctx context.Context) error {
		o, err := s.getOrderForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if !o.InvoiceIssued() {
			return nil
		}
		o.InvoiceIssuedAt, o.InvoiceIssuedBy = nil, ""
		o.InvoiceNo, o.InvoiceSeries, o.InvoiceDate = "", "", nil
		return s.repo.UpdateOrder(ctx, o)
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrder(ctx, id)
}

func (s *usageBillingService) AdminCounts(ctx context.Context) (*types.UsageBillingAdminCounts, error) {
	awaiting, err := s.CountOrdersAwaitingAdmin(ctx)
	if err != nil {
		return nil, err
	}
	invoices, err := s.repo.CountInvoicesToIssue(ctx)
	if err != nil {
		return nil, err
	}
	return &types.UsageBillingAdminCounts{AwaitingAdmin: awaiting, InvoicesToIssue: invoices}, nil
}

func (s *usageBillingService) OrderReceipt(
	ctx context.Context, order *types.UsageOrderView,
) (*types.UsageOrderReceipt, error) {
	if order == nil || order.UsageOrder == nil {
		return nil, apperrors.NewNotFoundError("order not found")
	}
	if order.Status != types.UsageOrderPaid {
		return nil, apperrors.NewConflictError("only a paid order has a receipt").
			WithDetails(map[string]string{"reason": "order_not_paid"})
	}
	info := s.BillingInfo(ctx)
	r := &types.UsageOrderReceipt{
		Order:         order,
		SellerName:    info.SellerName,
		SellerTaxCode: info.SellerTaxCode,
		SellerAddress: info.SellerAddress,
		SellerContact: info.Contact,
		BankName:      info.BankName,
		BankAccountNo: info.BankAccountNo,
		BuyerName:     order.ScopeName,
	}
	if order.ScopeType == types.UsageScopeUser && s.users != nil {
		if u, err := s.users.GetUserByID(ctx, order.ScopeID); err == nil && u != nil && u.Email != r.BuyerName {
			r.BuyerEmail = u.Email
		}
	}
	return r, nil
}
