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

// RecordPayment assumes ONE source (checker) per receiving bank account:
// short transfers are only added up within the source that reported them,
// and (source, transaction_id) is the only idempotency key, so a second
// checker on the same account would report each transfer again under its
// own name. A transfer with the same amount and booking time as one
// already held for the order is counted once.
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
	// Short transfers add up: the buyer is asked to send the rest with the
	// same content, and the one that completes the total confirms. Only the
	// same source's transfers are added (one checker per bank account, see
	// RecordPayment): two checkers reporting the same money never add up,
	// while two real transfers of the same amount from one source both count
	// (a re-push of the same transfer keeps its external id and is a
	// Duplicate).
	var earlier []*types.UsagePaymentTransaction
	if t.Status == types.UsagePaymentUnmatched {
		pays, err := s.repo.ListOrderPayments(ctx, o.ID)
		if err != nil {
			return err
		}
		var before int64
		earlier, before = heldPayments(pays, t.Source, types.UsagePaymentUnderpaid)
		// oldest first, the completing transfer last
		for i, j := 0, len(earlier)-1; i < j; i, j = i+1, j-1 {
			earlier[i], earlier[j] = earlier[j], earlier[i]
		}
		if received := before + t.AmountVND; received < o.TotalVND {
			t.Status, t.Reason = types.UsagePaymentUnderpaid,
				fmt.Sprintf("transferred %d of %d VND", received, o.TotalVND)
		}
	}
	// a short transfer made in time keeps an expired order open, as a
	// complete one does
	if revive && t.Status == types.UsagePaymentUnderpaid {
		err := s.repo.InTransaction(ctx, func(ctx context.Context) error {
			o.ExpiresAt = s.paymentDeadline(ctx, o.TotalVND, s.now())
			return s.reopenExpiredOrder(ctx, o)
		})
		var appErr *apperrors.AppError
		switch {
		case errors.As(err, &appErr):
			t.Status, t.Reason = types.UsagePaymentOrderClosed,
				"transferred in time but the order cannot be reopened: "+appErr.Message
			if cur, gerr := s.repo.GetOrder(ctx, o.ID); gerr == nil {
				o = cur
			}
		case err != nil:
			return err
		}
	}
	if t.Status != types.UsagePaymentUnmatched {
		result.Order = s.view(ctx, o)
		return s.repo.UpdatePaymentTransaction(ctx, t)
	}
	view, err := s.confirmPaid(ctx, o, append(earlier, t), paymentConfirmedBy(t.Source), revive)
	if err != nil {
		return err
	}
	result.Order = view
	return nil
}

// confirmPaid confirms o with the transfers that pay it, the completing
// one last (a savepoint: a refusal leaves the order as it was; revive
// reopens the expired order first). On success they are all marked
// matched; on a refusal the last is marked confirm_failed with the reason,
// so the money shows as received and waits for a SuperAdmin. Every
// transfer is written; only storage errors are returned.
func (s *usageBillingService) confirmPaid(
	ctx context.Context, o *types.UsageOrder, pays []*types.UsagePaymentTransaction, by string, revive bool,
) (*types.UsageOrderView, error) {
	last := pays[len(pays)-1]
	var view *types.UsageOrderView
	err := s.repo.InTransaction(ctx, func(ctx context.Context) error {
		if revive {
			if err := s.reopenExpiredOrder(ctx, o); err != nil {
				return err
			}
		}
		v, err := s.ConfirmOrder(ctx, o.ID, by)
		view = v
		return err
	})
	var appErr *apperrors.AppError
	switch {
	case err == nil:
		var paid int64
		for _, p := range pays {
			paid += p.AmountVND
			p.Status, p.Reason = types.UsagePaymentMatched, ""
			if p != last {
				p.Reason = fmt.Sprintf("completed by transfer %s", last.ExternalID)
			}
		}
		if len(pays) > 1 {
			last.Reason = fmt.Sprintf("completes %d VND in %d transfers", paid, len(pays))
		}
		if extra := paid - o.TotalVND; extra > 0 {
			last.Reason = strings.TrimPrefix(last.Reason+"; ", "; ") + fmt.Sprintf("overpaid by %d VND", extra)
		}
	case errors.As(err, &appErr):
		last.Status, last.Reason = types.UsagePaymentConfirmFailed, appErr.Message
		view = nil
		if cur, gerr := s.repo.GetOrder(ctx, o.ID); gerr == nil {
			view = s.view(ctx, cur)
		}
	default:
		return nil, err
	}
	for _, p := range pays {
		if err := s.repo.UpdatePaymentTransaction(ctx, p); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// heldPayments returns the transfers of pays in statuses and the money
// they brought. With a source, the sum of that source's transfers. With
// source "", the largest per-source sum: two checkers report the same
// money, so their reports never add up (see receivedAcrossSources).
func heldPayments(
	pays []*types.UsagePaymentTransaction, source string, statuses ...string,
) ([]*types.UsagePaymentTransaction, int64) {
	var out []*types.UsagePaymentTransaction
	bySource := map[string]int64{}
	for _, p := range pays {
		if source != "" && p.Source != source {
			continue
		}
		held := false
		for _, st := range statuses {
			if p.Status == st {
				held = true
				break
			}
		}
		if !held {
			continue
		}
		bySource[p.Source] += p.AmountVND
		out = append(out, p)
	}
	return out, receivedAcrossSources(bySource)
}

// receivedAcrossSources is the money an order received given each
// source's sum: the largest one.
func receivedAcrossSources(bySource map[string]int64) int64 {
	var best int64
	for _, v := range bySource {
		if v > best {
			best = v
		}
	}
	return best
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
			return apperrors.NewConflictError("the order expired and its promo code has no use left").
				WithDetails(map[string]string{"reason": "promo_exhausted"})
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
