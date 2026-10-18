package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// Incoming payments: see types.UsagePaymentWebhookRequest.

const (
	usagePaymentMaxSourceLen     = 32
	usagePaymentMaxExternalIDLen = 128
	// usagePaymentMaxCodes bounds the lookups for one transfer content.
	usagePaymentMaxCodes = 5
)

func (s *usageBillingService) RecordPayment(
	ctx context.Context, req types.UsagePaymentWebhookRequest, raw []byte,
) (*types.UsagePaymentResult, error) {
	t, direction, err := s.newPaymentTransaction(req, raw)
	if err != nil {
		return nil, err
	}
	s.screenPayment(ctx, t, direction)
	result := &types.UsagePaymentResult{Transaction: t}
	// One transaction: a concurrent resend waits on the unique key and then
	// finds the row; a failure before commit stores nothing, so the
	// checker's retry is processed afresh.
	err = s.repo.InTransaction(ctx, func(ctx context.Context) error {
		inserted, err := s.repo.CreatePaymentTransaction(ctx, t)
		if err != nil {
			return err
		}
		if !inserted {
			prev, err := s.repo.GetPaymentTransactionByExternalID(ctx, t.Source, t.ExternalID)
			if err != nil {
				return err
			}
			result.Transaction = prev
			result.Duplicate = true
			return nil
		}
		if t.Status == types.UsagePaymentIgnored {
			return nil
		}
		return s.settlePayment(ctx, t, result)
	})
	if err != nil {
		return nil, err
	}
	if result.Duplicate && result.Transaction.OrderID != nil {
		if o, err := s.repo.GetOrder(ctx, *result.Transaction.OrderID); err == nil {
			result.Order = s.view(ctx, o)
		}
	}
	return result, nil
}

func (s *usageBillingService) newPaymentTransaction(
	req types.UsagePaymentWebhookRequest, raw []byte,
) (*types.UsagePaymentTransaction, string, error) {
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" || len(source) > usagePaymentMaxSourceLen {
		return nil, "", apperrors.NewBadRequestError(fmt.Sprintf("source is required (at most %d characters)", usagePaymentMaxSourceLen))
	}
	for _, r := range source {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return nil, "", apperrors.NewBadRequestError("source may only contain letters, digits, '-', '_' and '.'")
		}
	}
	externalID := strings.TrimSpace(req.TransactionID)
	if externalID == "" || len(externalID) > usagePaymentMaxExternalIDLen {
		return nil, "", apperrors.NewBadRequestError(fmt.Sprintf("transaction_id is required (at most %d characters)", usagePaymentMaxExternalIDLen))
	}
	direction := strings.ToLower(strings.TrimSpace(req.Direction))
	if direction == "" {
		direction = types.UsagePaymentDirectionIn
	}
	if direction != types.UsagePaymentDirectionIn && direction != types.UsagePaymentDirectionOut {
		return nil, "", apperrors.NewBadRequestError("direction must be in or out")
	}
	now := s.now()
	at := now
	if req.TransactedAt != nil && !req.TransactedAt.IsZero() {
		at = *req.TransactedAt
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	return &types.UsagePaymentTransaction{
		ID:           uuid.New().String(),
		Source:       source,
		ExternalID:   externalID,
		AmountVND:    req.AmountVND,
		Content:      strings.TrimSpace(req.Content),
		AccountNo:    strings.TrimSpace(req.AccountNo),
		TransactedAt: at,
		Status:       types.UsagePaymentUnmatched,
		Raw:          types.JSON(raw),
		CreatedAt:    now,
		UpdatedAt:    now,
	}, direction, nil
}

// screenPayment marks transactions that are not money in to the
// receiving account as ignored; they are still stored for the record.
func (s *usageBillingService) screenPayment(ctx context.Context, t *types.UsagePaymentTransaction, direction string) {
	switch {
	case direction == types.UsagePaymentDirectionOut:
		t.Status, t.Reason = types.UsagePaymentIgnored, "outgoing transfer"
	case t.AmountVND <= 0:
		t.Status, t.Reason = types.UsagePaymentIgnored, "amount is not positive"
	case t.AccountNo != "":
		if want := s.BillingInfo(ctx).BankAccountNo; want != "" && accountDigits(want) != accountDigits(t.AccountNo) {
			t.Status, t.Reason = types.UsagePaymentIgnored, "not the receiving account"
		}
	}
}

func accountDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '.' {
			return -1
		}
		return r
	}, strings.ToUpper(s))
}

// orderCodesIn returns the order codes that may be in a transfer content.
// Banks upper-case the text and often drop spaces and punctuation, so the
// code can run into the next word; every "WR" followed by 8 characters of
// the code alphabet is a candidate, overlapping ones included.
func orderCodesIn(content string) []string {
	up := strings.ToUpper(content)
	const n = 10 // "WR" + 8
	var out []string
	seen := map[string]bool{}
	for i := 0; i+n <= len(up) && len(out) < usagePaymentMaxCodes; i++ {
		if up[i] != 'W' || up[i+1] != 'R' {
			continue
		}
		code := up[i : i+n]
		if strings.Trim(code[2:], usageOrderCodeAlphabet) != "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	return out
}

// settlePayment matches t to an order in the transaction of ctx and
// confirms the order when it may. Refusals are written on t; only storage
// errors are returned.
func (s *usageBillingService) settlePayment(
	ctx context.Context, t *types.UsagePaymentTransaction, result *types.UsagePaymentResult,
) error {
	// One transfer pays one order: the first open one named in the
	// content, else the first known one (to report why it was refused).
	var found *types.UsageOrder
	for _, code := range orderCodesIn(t.Content) {
		o, err := s.repo.GetOrderByCode(ctx, code)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if found == nil {
			found = o
		}
		if o.Status == types.UsageOrderPending || o.Status == types.UsageOrderExpired {
			found = o
			break
		}
	}
	if found == nil {
		t.Status, t.Reason = types.UsagePaymentUnmatched, "no order code in the transfer content"
		return s.repo.UpdatePaymentTransaction(ctx, t)
	}
	o, err := s.repo.GetOrderForUpdate(ctx, found.ID)
	if err != nil {
		return err
	}
	t.OrderID, t.OrderCode = &o.ID, o.OrderCode

	revive := false
	switch {
	case o.Status == types.UsageOrderPending:
		// still open, even if past expires_at: nothing has been released
		// and the page still showed the QR code
	case o.Status == types.UsageOrderExpired && (o.ExpiresAt == nil || !t.TransactedAt.After(*o.ExpiresAt)):
		// paid in time, but the checker reported it after the order was
		// expired: reopen it
		revive = true
	case o.Status == types.UsageOrderExpired:
		t.Status, t.Reason = types.UsagePaymentOrderClosed, "transferred after the order expired"
	default:
		t.Status, t.Reason = types.UsagePaymentOrderClosed, "the order is already "+o.Status
	}
	if t.Status == types.UsagePaymentUnmatched && t.AmountVND < o.TotalVND {
		t.Status, t.Reason = types.UsagePaymentUnderpaid,
			fmt.Sprintf("transferred %d of %d VND", t.AmountVND, o.TotalVND)
	}
	if t.Status != types.UsagePaymentUnmatched {
		result.Order = s.view(ctx, o)
		return s.repo.UpdatePaymentTransaction(ctx, t)
	}

	var view *types.UsageOrderView
	// a savepoint: a refused confirmation leaves the order as it was and
	// the transaction is still stored with the reason
	err = s.repo.InTransaction(ctx, func(ctx context.Context) error {
		if revive {
			if err := s.reopenExpiredOrder(ctx, o); err != nil {
				return err
			}
		}
		v, err := s.ConfirmOrder(ctx, o.ID, paymentConfirmedBy(t.Source))
		view = v
		return err
	})
	var appErr *apperrors.AppError
	switch {
	case err == nil:
		t.Status = types.UsagePaymentMatched
		if extra := t.AmountVND - o.TotalVND; extra > 0 {
			t.Reason = fmt.Sprintf("overpaid by %d VND", extra)
		}
		result.Order = view
	case errors.As(err, &appErr):
		t.Status, t.Reason = types.UsagePaymentConfirmFailed, appErr.Message
		if cur, gerr := s.repo.GetOrder(ctx, o.ID); gerr == nil {
			result.Order = s.view(ctx, cur)
		}
	default:
		return err
	}
	return s.repo.UpdatePaymentTransaction(ctx, t)
}

// reopenExpiredOrder puts an expired order back to pending, taking back
// the promo use that expiry released. The promo is not re-checked for
// activity or dates: the order was valid when the buyer committed to it.
func (s *usageBillingService) reopenExpiredOrder(ctx context.Context, o *types.UsageOrder) error {
	if o.PromoCodeID != nil {
		ok, err := s.repo.ReservePromoUse(ctx, *o.PromoCodeID)
		if err != nil {
			return err
		}
		if !ok {
			return apperrors.NewConflictError("the order expired and its promo code has no use left")
		}
	}
	o.Status = types.UsageOrderPending
	return s.repo.UpdateOrder(ctx, o)
}

// paymentConfirmedBy is the confirmed_by of an order the checker paid
// (the column holds 36 characters).
func paymentConfirmedBy(source string) string {
	by := "payment:" + source
	if len(by) > 36 {
		by = by[:36]
	}
	return by
}

func (s *usageBillingService) PaymentWebhookSecret(ctx context.Context) string {
	return strings.TrimSpace(s.settings.GetString(ctx,
		types.BillingWebhookSecretSettingKey, types.BillingWebhookSecretEnv, ""))
}

func (s *usageBillingService) ListPayments(
	ctx context.Context, filter types.UsagePaymentFilter,
) ([]*types.UsagePaymentTransaction, int64, error) {
	return s.repo.ListPaymentTransactions(ctx, filter)
}
