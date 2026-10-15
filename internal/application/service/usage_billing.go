package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// usageBillingService implements interfaces.UsageBillingService.
type usageBillingService struct {
	repo     interfaces.UsageBillingRepository
	plans    interfaces.UsagePlanService
	planRepo interfaces.UsagePlanRepository
	tenants  interfaces.TenantRepository
	users    interfaces.UserRepository
	settings interfaces.SystemSettingService
	now      func() time.Time
}

// NewUsageBillingService creates the service.
func NewUsageBillingService(
	repo interfaces.UsageBillingRepository,
	plans interfaces.UsagePlanService,
	planRepo interfaces.UsagePlanRepository,
	tenants interfaces.TenantRepository,
	users interfaces.UserRepository,
	settings interfaces.SystemSettingService,
) interfaces.UsageBillingService {
	return &usageBillingService{
		repo: repo, plans: plans, planRepo: planRepo, tenants: tenants, users: users,
		settings: settings, now: time.Now,
	}
}

const (
	usageOrderMaxMonths = 36
	usageOrderMaxSeats  = 10000
	// usageOrderCodeAlphabet avoids 0/O and 1/I so the code survives a
	// hand-typed transfer description.
	usageOrderCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

func (s *usageBillingService) BillingInfo(ctx context.Context) *types.UsageBillingInfo {
	info := &types.UsageBillingInfo{
		BankBIN:         strings.TrimSpace(s.settings.GetString(ctx, types.BillingBankBINSettingKey, "", "")),
		BankName:        strings.TrimSpace(s.settings.GetString(ctx, types.BillingBankNameSettingKey, "", "")),
		BankAccountNo:   strings.TrimSpace(s.settings.GetString(ctx, types.BillingBankAccountNoSettingKey, "", "")),
		BankAccountName: strings.TrimSpace(s.settings.GetString(ctx, types.BillingBankAccountNameSettingKey, "", "")),
		OrderTTLHours:   s.settings.GetInt(ctx, types.BillingOrderTTLHoursSettingKey, "", 72),
		Contact:         strings.TrimSpace(s.settings.GetString(ctx, types.BillingContactSettingKey, "", "")),
	}
	info.Enabled = info.BankAccountNo != ""
	return info
}

func (s *usageBillingService) PurchasablePlans(ctx context.Context, scopeKind string) ([]*types.UsagePlan, error) {
	all, err := s.plans.ListPlans(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]*types.UsagePlan, 0, len(all))
	for _, p := range all {
		if scopeKind != "" && p.ScopeKind != scopeKind {
			continue
		}
		if p.PriceVND <= 0 {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// quote prices req for scopeID; when strict, an unusable promo code is an
// error rather than a note on the quote.
func (s *usageBillingService) quote(
	ctx context.Context, req types.UsageQuoteRequest, scopeID string, strict bool,
) (*types.UsageQuote, *types.UsagePromoCode, error) {
	if req.ScopeType != types.UsageScopeTenant && req.ScopeType != types.UsageScopeUser {
		return nil, nil, apperrors.NewBadRequestError("scope_type must be tenant or user")
	}
	plan, err := s.plans.GetPlan(ctx, strings.TrimSpace(req.PlanID))
	if err != nil {
		return nil, nil, err
	}
	if !plan.IsActive || plan.PriceVND <= 0 {
		return nil, nil, apperrors.NewBadRequestError("this plan is not sold online")
	}
	if plan.ScopeKind != req.ScopeType {
		return nil, nil, apperrors.NewBadRequestError(fmt.Sprintf("plan %s is a %s plan", plan.Code, plan.ScopeKind))
	}
	if req.Months <= 0 {
		req.Months = 1
	}
	if req.Months > usageOrderMaxMonths {
		return nil, nil, apperrors.NewBadRequestError(fmt.Sprintf("months must be at most %d", usageOrderMaxMonths))
	}
	q := &types.UsageQuote{Plan: plan, Mode: plan.Mode, Months: req.Months, UnitPriceVND: plan.PriceVND}
	if plan.ScopeKind == types.UsageScopeTenant && plan.Mode == types.UsageModeSeat {
		if req.Seats <= 0 {
			return nil, nil, apperrors.NewBadRequestError("seat plans need seats > 0")
		}
		if req.Seats > usageOrderMaxSeats {
			return nil, nil, apperrors.NewBadRequestError(fmt.Sprintf("seats must be at most %d", usageOrderMaxSeats))
		}
		q.Seats = req.Seats
		q.SubtotalVND = plan.PriceVND * int64(req.Months) * int64(req.Seats)
		if err := s.priceSeatRenewal(ctx, q, scopeID); err != nil {
			return nil, nil, err
		}
	} else {
		q.Mode = types.UsageModePool
		q.SubtotalVND = plan.PriceVND * int64(req.Months)
	}
	q.TotalVND = q.SubtotalVND

	var promo *types.UsagePromoCode
	if code := types.NormalizeUsagePromoCode(req.PromoCode); code != "" {
		q.PromoCode = code
		p, err := s.repo.GetPromoByCode(ctx, code)
		if err != nil {
			return nil, nil, err
		}
		ok, reason := p.AppliesTo(plan.ID, s.now())
		if !ok {
			if strict {
				return nil, nil, apperrors.NewBadRequestError("promo code cannot be used").WithDetails(map[string]string{"reason": reason})
			}
			q.PromoError = reason
		} else {
			promo = p
			q.PromoName = p.Name
			q.DiscountVND = p.Discount(q.SubtotalVND)
			q.TotalVND = q.SubtotalVND - q.DiscountVND
		}
	}
	return q, promo, nil
}

// usageProrationDaysPerMonth turns a monthly seat price into a daily one
// for the time left on a period.
const usageProrationDaysPerMonth = 30

// priceSeatRenewal applies the seat rules of an order that will extend the
// scope's current subscription (see apply): fewer seats than it has are
// refused, since the extension would cover every seat; added seats also
// pay for the days left on the current period, since they start now.
func (s *usageBillingService) priceSeatRenewal(ctx context.Context, q *types.UsageQuote, scopeID string) error {
	if scopeID == "" {
		return nil
	}
	cur, err := s.planRepo.GetActiveSubscription(ctx, q.Plan.ScopeKind, scopeID)
	if err != nil {
		return err
	}
	now := s.now()
	if !renewsSubscription(cur, q.Plan.ID, types.UsageModeSeat, now) {
		return nil
	}
	q.CurrentSeats = cur.Seats
	if q.Seats < cur.Seats {
		return apperrors.NewBadRequestError(fmt.Sprintf(
			"the current subscription has %d seats; a renewal must cover at least that many", cur.Seats,
		)).WithDetails(map[string]interface{}{"reason": "seats_below_current", "current_seats": cur.Seats})
	}
	if q.Seats == cur.Seats || cur.EndsAt == nil || !cur.EndsAt.After(now) {
		return nil
	}
	days := int(math.Ceil(cur.EndsAt.Sub(now).Hours() / 24))
	added := int64(q.Seats - cur.Seats)
	q.ProratedDays = days
	// rounded up: a partial đồng is not given away
	q.ProrationVND = (q.UnitPriceVND*added*int64(days) + usageProrationDaysPerMonth - 1) / usageProrationDaysPerMonth
	q.SubtotalVND += q.ProrationVND
	return nil
}

// renewsSubscription reports whether an order of planID/mode extends cur
// rather than starting a new subscription.
func renewsSubscription(cur *types.UsageSubscription, planID, mode string, now time.Time) bool {
	return cur != nil && cur.PlanID == planID && cur.Mode == mode && cur.IsCurrentAt(now)
}

func (s *usageBillingService) Quote(ctx context.Context, req types.UsageQuoteRequest, scopeID string) (*types.UsageQuote, error) {
	q, _, err := s.quote(ctx, req, strings.TrimSpace(scopeID), false)
	return q, err
}

func newUsageOrderCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("WR")
	for _, x := range b {
		sb.WriteByte(usageOrderCodeAlphabet[int(x)%len(usageOrderCodeAlphabet)])
	}
	return sb.String(), nil
}

func (s *usageBillingService) CreateOrder(
	ctx context.Context, req types.UsageQuoteRequest, scopeID, createdBy string,
) (*types.UsageOrderView, error) {
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return nil, apperrors.NewBadRequestError("scope_id is required")
	}
	q, promo, err := s.quote(ctx, req, scopeID, true)
	if err != nil {
		return nil, err
	}
	now := s.now()
	order := &types.UsageOrder{
		ID:            uuid.New().String(),
		ScopeType:     req.ScopeType,
		ScopeID:       scopeID,
		PlanID:        q.Plan.ID,
		PlanName:      q.Plan.Name,
		Mode:          q.Mode,
		Seats:         q.Seats,
		Months:        q.Months,
		UnitPriceVND:  q.UnitPriceVND,
		SubtotalVND:   q.SubtotalVND,
		PromoCode:     q.PromoCode,
		DiscountVND:   q.DiscountVND,
		TotalVND:      q.TotalVND,
		Status:        types.UsageOrderPending,
		PaymentMethod: types.UsagePaymentBankTransfer,
		CreatedBy:     createdBy,
		Note:          strings.TrimSpace(req.Note),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if promo != nil {
		id := promo.ID
		order.PromoCodeID = &id
	}
	if ttl := s.settings.GetInt(ctx, types.BillingOrderTTLHoursSettingKey, "", 72); ttl > 0 {
		exp := now.Add(time.Duration(ttl) * time.Hour)
		order.ExpiresAt = &exp
	}
	// The promo use is reserved in the order's transaction: the code's last
	// use goes to one order only, and a failed insert gives it back.
	err = s.repo.InTransaction(ctx, func(ctx context.Context) error {
		if promo != nil {
			ok, err := s.repo.ReservePromoUse(ctx, promo.ID)
			if err != nil {
				return err
			}
			if !ok {
				return apperrors.NewBadRequestError("promo code cannot be used").
					WithDetails(map[string]string{"reason": "promo_exhausted"})
			}
		}
		// A code collision is astronomically unlikely but cheap to retry;
		// each attempt is a savepoint so a failed insert leaves the
		// transaction usable.
		for attempt := 0; attempt < 5; attempt++ {
			code, err := newUsageOrderCode()
			if err != nil {
				return err
			}
			order.OrderCode = code
			err = s.repo.InTransaction(ctx, func(ctx context.Context) error {
				return s.repo.CreateOrder(ctx, order)
			})
			if err == nil {
				return nil
			}
			if !isUniqueViolation(err) || attempt == 4 {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if order.TotalVND <= 0 {
		// Nothing to pay: a free trial or a 100% code.
		return s.ConfirmOrder(ctx, order.ID, createdBy)
	}
	return s.view(ctx, order), nil
}

func (s *usageBillingService) expireDue(ctx context.Context) error {
	return s.repo.InTransaction(ctx, func(ctx context.Context) error {
		expired, err := s.repo.ExpirePendingOrders(ctx, s.now())
		if err != nil {
			return err
		}
		for _, o := range expired {
			if o.PromoCodeID != nil {
				if err := s.repo.AddPromoUse(ctx, *o.PromoCodeID, -1); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *usageBillingService) GetOrder(ctx context.Context, id string) (*types.UsageOrderView, error) {
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	o, err := s.repo.GetOrder(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("order not found")
	}
	if err != nil {
		return nil, err
	}
	return s.view(ctx, o), nil
}

func (s *usageBillingService) GetOrderByCode(ctx context.Context, code string) (*types.UsageOrderView, error) {
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	o, err := s.repo.GetOrderByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("order not found")
	}
	if err != nil {
		return nil, err
	}
	return s.view(ctx, o), nil
}

func (s *usageBillingService) ListOrders(ctx context.Context, filter types.UsageOrderFilter) ([]*types.UsageOrderView, error) {
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	orders, err := s.repo.ListOrders(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]*types.UsageOrderView, 0, len(orders))
	for _, o := range orders {
		out = append(out, s.view(ctx, o))
	}
	return out, nil
}

// getPendingOrderForUpdate locks order id in the transaction of ctx and
// checks it is still pending; verb names the refused action.
func (s *usageBillingService) getPendingOrderForUpdate(ctx context.Context, id, verb string) (*types.UsageOrder, error) {
	o, err := s.repo.GetOrderForUpdate(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("order not found")
	}
	if err != nil {
		return nil, err
	}
	if o.Status != types.UsageOrderPending {
		return nil, apperrors.NewConflictError("only pending orders can be " + verb)
	}
	return o, nil
}

func (s *usageBillingService) CancelOrder(ctx context.Context, id string) (*types.UsageOrderView, error) {
	var o *types.UsageOrder
	err := s.repo.InTransaction(ctx, func(ctx context.Context) error {
		var err error
		if o, err = s.getPendingOrderForUpdate(ctx, id, "cancelled"); err != nil {
			return err
		}
		o.Status = types.UsageOrderCancelled
		if err := s.repo.UpdateOrder(ctx, o); err != nil {
			return err
		}
		if o.PromoCodeID != nil {
			return s.repo.AddPromoUse(ctx, *o.PromoCodeID, -1)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, o), nil
}

// ConfirmOrder settles an order in one transaction: the order row is
// locked and must still be pending, the subscription change and the paid
// status commit together. A failure rolls everything back, so the next
// confirmation applies the order once, and a second confirmation finds it
// paid.
func (s *usageBillingService) ConfirmOrder(ctx context.Context, id, confirmedBy string) (*types.UsageOrderView, error) {
	var o *types.UsageOrder
	err := s.repo.InTransaction(ctx, func(ctx context.Context) error {
		var err error
		if o, err = s.getPendingOrderForUpdate(ctx, id, "confirmed"); err != nil {
			return err
		}
		if err := s.repo.LockScope(ctx, o.ScopeType, o.ScopeID); err != nil {
			return err
		}
		now := s.now()
		subID, err := s.apply(ctx, o, now)
		if err != nil {
			return err
		}
		o.Status = types.UsageOrderPaid
		o.PaidAt = &now
		o.ConfirmedBy = confirmedBy
		o.SubscriptionID = &subID
		return s.repo.UpdateOrder(ctx, o)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, o), nil
}

// apply turns a paid order into subscription time: the scope's current
// subscription of the same plan is extended, otherwise a new one starts
// now and replaces whatever the scope had.
func (s *usageBillingService) apply(ctx context.Context, o *types.UsageOrder, now time.Time) (string, error) {
	cur, err := s.planRepo.GetActiveSubscription(ctx, o.ScopeType, o.ScopeID)
	if err != nil {
		return "", err
	}
	if renewsSubscription(cur, o.PlanID, o.Mode, now) {
		// the order paid for o.Seats only (its quote refused fewer seats
		// than the subscription had); seats added since then would be
		// extended for free
		if o.Mode == types.UsageModeSeat && o.Seats < cur.Seats {
			return "", apperrors.NewConflictError(fmt.Sprintf(
				"the subscription now has %d seats but order %s pays for %d; cancel it and order again",
				cur.Seats, o.OrderCode, o.Seats))
		}
		from := now
		if cur.EndsAt != nil && cur.EndsAt.After(now) {
			from = *cur.EndsAt
		}
		ends := from.AddDate(0, o.Months, 0)
		cur.EndsAt = &ends
		if o.Mode == types.UsageModeSeat && o.Seats > cur.Seats {
			cur.Seats = o.Seats
		}
		cur.Note = appendOrderNote(cur.Note, o.OrderCode)
		if err := s.planRepo.UpdateSubscription(ctx, cur); err != nil {
			return "", err
		}
		return cur.ID, nil
	}
	ends := now.AddDate(0, o.Months, 0)
	sub := &types.UsageSubscription{
		ScopeType: o.ScopeType,
		ScopeID:   o.ScopeID,
		PlanID:    o.PlanID,
		Mode:      o.Mode,
		Seats:     o.Seats,
		StartsAt:  now,
		EndsAt:    &ends,
		Status:    types.UsageSubscriptionActive,
		Note:      appendOrderNote("", o.OrderCode),
	}
	v, err := s.plans.CreateSubscription(ctx, sub)
	if err != nil {
		return "", err
	}
	return v.ID, nil
}

func appendOrderNote(note, code string) string {
	tag := "order " + code
	if strings.TrimSpace(note) == "" {
		return tag
	}
	return note + "; " + tag
}

func (s *usageBillingService) view(ctx context.Context, o *types.UsageOrder) *types.UsageOrderView {
	v := &types.UsageOrderView{UsageOrder: o, TransferContent: o.OrderCode}
	v.ScopeName = s.scopeName(ctx, o.ScopeType, o.ScopeID)
	if o.Status == types.UsageOrderPending && o.TotalVND > 0 {
		v.QRURL = vietQRURL(s.BillingInfo(ctx), o.TotalVND, o.OrderCode)
	}
	return v
}

// vietQRURL builds the img.vietqr.io image for a transfer, "" when the
// receiving account is not configured.
func vietQRURL(info *types.UsageBillingInfo, amount int64, content string) string {
	if info == nil || !info.Enabled || info.BankBIN == "" {
		return ""
	}
	q := url.Values{}
	q.Set("amount", fmt.Sprintf("%d", amount))
	q.Set("addInfo", content)
	if info.BankAccountName != "" {
		q.Set("accountName", info.BankAccountName)
	}
	return fmt.Sprintf("https://img.vietqr.io/image/%s-%s-compact2.png?%s",
		url.PathEscape(info.BankBIN), url.PathEscape(info.BankAccountNo), q.Encode())
}

func (s *usageBillingService) scopeName(ctx context.Context, scopeType, scopeID string) string {
	switch scopeType {
	case types.UsageScopeTenant:
		var id uint64
		if _, err := fmt.Sscanf(scopeID, "%d", &id); err == nil && s.tenants != nil {
			if t, err := s.tenants.GetTenantByID(ctx, id); err == nil && t != nil {
				return t.Name
			}
		}
	case types.UsageScopeUser:
		if s.users != nil {
			if u, err := s.users.GetUserByID(ctx, scopeID); err == nil && u != nil {
				if u.Username != "" {
					return u.Username
				}
				return u.Email
			}
		}
	}
	return scopeID
}

// ---- promo codes (SuperAdmin) ------------------------------------------------

func normalizePromo(p *types.UsagePromoCode, now time.Time) error {
	p.Code = types.NormalizeUsagePromoCode(p.Code)
	if p.Code == "" {
		return apperrors.NewBadRequestError("code is required")
	}
	for _, r := range p.Code {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return apperrors.NewBadRequestError("code may only contain letters, digits, - and _")
		}
	}
	p.Name = strings.TrimSpace(p.Name)
	switch p.DiscountType {
	case types.UsagePromoPercent:
		if p.DiscountValue <= 0 || p.DiscountValue > 100 {
			return apperrors.NewBadRequestError("percent discount must be between 1 and 100")
		}
	case types.UsagePromoAmount:
		if p.DiscountValue <= 0 {
			return apperrors.NewBadRequestError("amount discount must be > 0")
		}
	default:
		return apperrors.NewBadRequestError("discount_type must be percent or amount")
	}
	if p.MaxUses < 0 {
		return apperrors.NewBadRequestError("max_uses must be >= 0")
	}
	if p.PlanIDs == nil {
		p.PlanIDs = types.StringArray{}
	}
	if p.StartsAt.IsZero() {
		p.StartsAt = now
	}
	if p.EndsAt != nil && !p.EndsAt.After(p.StartsAt) {
		return apperrors.NewBadRequestError("ends_at must be after starts_at")
	}
	return nil
}

func (s *usageBillingService) ListPromos(ctx context.Context, includeInactive bool) ([]*types.UsagePromoCode, error) {
	return s.repo.ListPromos(ctx, includeInactive)
}

func (s *usageBillingService) CreatePromo(ctx context.Context, p *types.UsagePromoCode) (*types.UsagePromoCode, error) {
	if err := normalizePromo(p, s.now()); err != nil {
		return nil, err
	}
	p.ID = uuid.New().String()
	p.UsedCount = 0
	p.CreatedAt = s.now()
	p.UpdatedAt = p.CreatedAt
	if err := s.repo.CreatePromo(ctx, p); err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.NewConflictError("a promo code with this code already exists")
		}
		return nil, err
	}
	return p, nil
}

func (s *usageBillingService) UpdatePromo(ctx context.Context, p *types.UsagePromoCode) (*types.UsagePromoCode, error) {
	existing, err := s.repo.GetPromo(ctx, p.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewNotFoundError("promo code not found")
	}
	if err != nil {
		return nil, err
	}
	if err := normalizePromo(p, s.now()); err != nil {
		return nil, err
	}
	p.CreatedAt = existing.CreatedAt
	p.UsedCount = existing.UsedCount
	if err := s.repo.UpdatePromo(ctx, p); err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.NewConflictError("a promo code with this code already exists")
		}
		return nil, err
	}
	return p, nil
}

func (s *usageBillingService) DeletePromo(ctx context.Context, id string) error {
	if _, err := s.repo.GetPromo(ctx, id); errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.NewNotFoundError("promo code not found")
	} else if err != nil {
		return err
	}
	// Orders keep the code as text, so deleting is safe for history.
	return s.repo.DeletePromo(ctx, id)
}
