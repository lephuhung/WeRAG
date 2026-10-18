package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// memBillingRepo keeps promos and orders in memory. InTransaction restores
// them, and the plan repo's subscriptions, when fn fails.
type memBillingRepo struct {
	interfaces.UsageBillingRepository
	promos map[string]*types.UsagePromoCode
	orders map[string]*types.UsageOrder
	// payments are keyed by source + "/" + external_id
	payments map[string]*types.UsagePaymentTransaction
	plans    *memPlanRepo
	// failUpdateOrder makes the next UpdateOrder fail (a lost connection
	// after the subscription was written).
	failUpdateOrder bool
	// stalePromoReads makes GetPromoByCode report no uses taken, as a
	// request that read the code before a concurrent order took its use.
	stalePromoReads bool
}

func (m *memBillingRepo) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	promos := map[string]types.UsagePromoCode{}
	for k, v := range m.promos {
		promos[k] = *v
	}
	orders := map[string]types.UsageOrder{}
	for k, v := range m.orders {
		orders[k] = *v
	}
	payments := map[string]types.UsagePaymentTransaction{}
	for k, v := range m.payments {
		payments[k] = *v
	}
	subs := map[string]types.UsageSubscription{}
	if m.plans != nil {
		for k, v := range m.plans.active {
			subs[k] = *v
		}
	}
	err := fn(ctx)
	if err == nil {
		return nil
	}
	m.promos, m.orders = map[string]*types.UsagePromoCode{}, map[string]*types.UsageOrder{}
	for k, v := range promos {
		v := v
		m.promos[k] = &v
	}
	for k, v := range orders {
		v := v
		m.orders[k] = &v
	}
	m.payments = map[string]*types.UsagePaymentTransaction{}
	for k, v := range payments {
		v := v
		m.payments[k] = &v
	}
	if m.plans != nil {
		m.plans.active = map[string]*types.UsageSubscription{}
		for k, v := range subs {
			v := v
			m.plans.active[k] = &v
		}
	}
	return err
}
func (m *memBillingRepo) LockScope(context.Context, string, string) error { return nil }
func (m *memBillingRepo) ReservePromoUse(_ context.Context, id string) (bool, error) {
	p, ok := m.promos[id]
	if !ok || (p.MaxUses > 0 && p.UsedCount >= p.MaxUses) {
		return false, nil
	}
	p.UsedCount++
	return true, nil
}
func (m *memBillingRepo) GetOrderForUpdate(ctx context.Context, id string) (*types.UsageOrder, error) {
	return m.GetOrder(ctx, id)
}

func newMemBillingRepo() *memBillingRepo {
	return &memBillingRepo{
		promos:   map[string]*types.UsagePromoCode{},
		orders:   map[string]*types.UsageOrder{},
		payments: map[string]*types.UsagePaymentTransaction{},
	}
}

func (m *memBillingRepo) GetPromoByCode(_ context.Context, code string) (*types.UsagePromoCode, error) {
	for _, p := range m.promos {
		if p.Code == code {
			cp := *p
			if m.stalePromoReads {
				cp.UsedCount = 0
			}
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memBillingRepo) GetPromo(_ context.Context, id string) (*types.UsagePromoCode, error) {
	if p, ok := m.promos[id]; ok {
		return p, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (m *memBillingRepo) CreatePromo(_ context.Context, p *types.UsagePromoCode) error {
	m.promos[p.ID] = p
	return nil
}
func (m *memBillingRepo) AddPromoUse(_ context.Context, id string, delta int) error {
	if p, ok := m.promos[id]; ok {
		p.UsedCount += delta
	}
	return nil
}
func (m *memBillingRepo) CreateOrder(_ context.Context, o *types.UsageOrder) error {
	cp := *o
	m.orders[o.ID] = &cp
	return nil
}
func (m *memBillingRepo) GetOrder(_ context.Context, id string) (*types.UsageOrder, error) {
	if o, ok := m.orders[id]; ok {
		cp := *o
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (m *memBillingRepo) UpdateOrder(_ context.Context, o *types.UsageOrder) error {
	if m.failUpdateOrder {
		m.failUpdateOrder = false
		return errors.New("connection reset")
	}
	cp := *o
	m.orders[o.ID] = &cp
	return nil
}
func (m *memBillingRepo) ListOrders(_ context.Context, f types.UsageOrderFilter) ([]*types.UsageOrder, error) {
	var out []*types.UsageOrder
	for _, o := range m.orders {
		if f.Status != "" && o.Status != f.Status {
			continue
		}
		cp := *o
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OrderCode < out[j].OrderCode })
	return out, nil
}
func (m *memBillingRepo) ExpirePendingOrders(_ context.Context, now time.Time) ([]*types.UsageOrder, error) {
	var out []*types.UsageOrder
	for _, o := range m.orders {
		if o.Status == types.UsageOrderPending && o.ExpiresAt != nil && o.ExpiresAt.Before(now) {
			o.Status = types.UsageOrderExpired
			cp := *o
			out = append(out, &cp)
		}
	}
	return out, nil
}

// memPlanRepo is the subscription store the billing service extends into.
type memPlanRepo struct {
	interfaces.UsagePlanRepository
	active map[string]*types.UsageSubscription
}

func (m *memPlanRepo) GetActiveSubscription(_ context.Context, scopeType, scopeID string) (*types.UsageSubscription, error) {
	return m.active[scopeType+"/"+scopeID], nil
}
func (m *memPlanRepo) UpdateSubscription(_ context.Context, sub *types.UsageSubscription) error {
	if sub.Status == types.UsageSubscriptionActive {
		m.active[sub.ScopeType+"/"+sub.ScopeID] = sub
	} else {
		delete(m.active, sub.ScopeType+"/"+sub.ScopeID)
	}
	return nil
}
func (m *memPlanRepo) CreateSubscription(_ context.Context, sub *types.UsageSubscription) error {
	m.active[sub.ScopeType+"/"+sub.ScopeID] = sub
	return nil
}

// planCatalog is the plan service: a fixed catalogue plus CreateSubscription.
type planCatalog struct {
	interfaces.UsagePlanService
	plans map[string]*types.UsagePlan
	repo  *memPlanRepo
	now   time.Time
}

func (p *planCatalog) GetPlan(_ context.Context, id string) (*types.UsagePlan, error) {
	if pl, ok := p.plans[id]; ok {
		return pl, nil
	}
	return nil, apperrors.NewNotFoundError("plan not found")
}
func (p *planCatalog) ListPlans(_ context.Context, includeInactive bool) ([]*types.UsagePlan, error) {
	var out []*types.UsagePlan
	for _, pl := range p.plans {
		if includeInactive || pl.IsActive {
			out = append(out, pl)
		}
	}
	return out, nil
}
func (p *planCatalog) CreateSubscription(ctx context.Context, sub *types.UsageSubscription) (*types.UsageSubscriptionView, error) {
	sub.ID = "sub-" + sub.PlanID
	sub.Plan = p.plans[sub.PlanID]
	if prev := p.repo.active[sub.ScopeType+"/"+sub.ScopeID]; prev != nil {
		prev.Status = types.UsageSubscriptionCancelled
	}
	_ = p.repo.CreateSubscription(ctx, sub)
	return &types.UsageSubscriptionView{UsageSubscription: sub}, nil
}

type billingSettings struct {
	interfaces.SystemSettingService
	strings map[string]string
	ints    map[string]int64
}

func (s billingSettings) GetString(_ context.Context, key, _ string, def string) string {
	if v, ok := s.strings[key]; ok {
		return v
	}
	return def
}
func (s billingSettings) GetInt(_ context.Context, key, _ string, def int64) int64 {
	if v, ok := s.ints[key]; ok {
		return v
	}
	return def
}

var billingNow = time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

type billingFixture struct {
	svc   *usageBillingService
	repo  *memBillingRepo
	plans *memPlanRepo
}

func newBillingFixture(t *testing.T) *billingFixture {
	t.Helper()
	planRepo := &memPlanRepo{active: map[string]*types.UsageSubscription{}}
	catalog := &planCatalog{plans: map[string]*types.UsagePlan{
		"pool": {ID: "pool", Code: "pool", Name: "Pool", ScopeKind: "tenant", Mode: "pool", PriceVND: 100_000, IsActive: true},
		"seat": {ID: "seat", Code: "seat", Name: "Seat", ScopeKind: "tenant", Mode: "seat", PriceVND: 49_000, IsActive: true},
		"me":   {ID: "me", Code: "me", Name: "Personal", ScopeKind: "user", Mode: "pool", PriceVND: 20_000, IsActive: true},
		"free": {ID: "free", Code: "free", Name: "Free", ScopeKind: "tenant", Mode: "pool", PriceVND: 0, IsActive: true},
	}, repo: planRepo, now: billingNow}
	repo := newMemBillingRepo()
	repo.plans = planRepo
	settings := billingSettings{
		strings: map[string]string{
			types.BillingBankBINSettingKey:         "970436",
			types.BillingBankAccountNoSettingKey:   "0011002233",
			types.BillingBankAccountNameSettingKey: "CONG AN HUE",
		},
		ints: map[string]int64{types.BillingOrderTTLHoursSettingKey: 48},
	}
	svc := NewUsageBillingService(repo, catalog, planRepo, nil, nil, settings).(*usageBillingService)
	svc.now = func() time.Time { return billingNow }
	return &billingFixture{svc: svc, repo: repo, plans: planRepo}
}

func (f *billingFixture) promo(t *testing.T, p types.UsagePromoCode) *types.UsagePromoCode {
	t.Helper()
	created, err := f.svc.CreatePromo(context.Background(), &p)
	require.NoError(t, err)
	return created
}

func TestBillingQuoteSeatPlanMultipliesSeatsAndMonths(t *testing.T) {
	f := newBillingFixture(t)
	q, err := f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 3, Seats: 5}, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(49_000*3*5), q.SubtotalVND)
	require.Equal(t, q.SubtotalVND, q.TotalVND)
	require.Equal(t, "seat", q.Mode)
}

func TestBillingQuoteRejectsFreeAndWrongScopePlans(t *testing.T) {
	f := newBillingFixture(t)
	_, err := f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "free", Months: 1}, "10003")
	require.Error(t, err)
	_, err = f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "user", PlanID: "pool", Months: 1}, "10003")
	require.Error(t, err)
	_, err = f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1}, "10003")
	require.Error(t, err, "seat plan without seats")
}

func TestBillingPromoPercentAndAmount(t *testing.T) {
	f := newBillingFixture(t)
	f.promo(t, types.UsagePromoCode{Code: "tet20", DiscountType: "percent", DiscountValue: 20, IsActive: true})
	f.promo(t, types.UsagePromoCode{Code: "MINUS", DiscountType: "amount", DiscountValue: 999_999, IsActive: true})

	q, err := f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 2, PromoCode: " tet20 "}, "10003")
	require.NoError(t, err)
	require.Equal(t, "TET20", q.PromoCode)
	require.Equal(t, int64(40_000), q.DiscountVND)
	require.Equal(t, int64(160_000), q.TotalVND)

	q, err = f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "minus"}, "10003")
	require.NoError(t, err)
	require.Equal(t, int64(100_000), q.DiscountVND, "amount capped at subtotal")
	require.Equal(t, int64(0), q.TotalVND)
}

func TestBillingPromoRulesSurfaceAsQuoteNoteButBlockOrders(t *testing.T) {
	f := newBillingFixture(t)
	ends := billingNow.Add(-time.Hour)
	f.promo(t, types.UsagePromoCode{Code: "OLD", DiscountType: "percent", DiscountValue: 10, IsActive: true,
		StartsAt: billingNow.Add(-48 * time.Hour), EndsAt: &ends})
	f.promo(t, types.UsagePromoCode{Code: "SEATONLY", DiscountType: "percent", DiscountValue: 10, IsActive: true, PlanIDs: types.StringArray{"seat"}})
	f.promo(t, types.UsagePromoCode{Code: "ONCE", DiscountType: "percent", DiscountValue: 10, IsActive: true, MaxUses: 1})

	cases := map[string]string{"OLD": "promo_expired", "SEATONLY": "promo_wrong_plan", "NOPE": "promo_not_found"}
	for code, reason := range cases {
		q, err := f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: code}, "10003")
		require.NoError(t, err, code)
		require.Equal(t, reason, q.PromoError, code)
		require.Equal(t, int64(0), q.DiscountVND, code)
		_, err = f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: code}, "10003", "u1")
		require.Error(t, err, code)
	}

	// A single-use code is consumed by the first order and refused after.
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "ONCE"}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, int64(90_000), o.TotalVND)
	q, err := f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "ONCE"}, "10003")
	require.NoError(t, err)
	require.Equal(t, "promo_exhausted", q.PromoError)
	// Cancelling gives the use back.
	_, err = f.svc.CancelOrder(context.Background(), o.ID)
	require.NoError(t, err)
	q, err = f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "ONCE"}, "10003")
	require.NoError(t, err)
	require.Equal(t, "", q.PromoError)
}

func TestBillingOrderIsPendingWithQRAndExpiry(t *testing.T) {
	f := newBillingFixture(t)
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPending, o.Status)
	require.Len(t, o.OrderCode, 10)
	require.Equal(t, o.OrderCode, o.TransferContent)
	require.Contains(t, o.QRURL, "970436-0011002233")
	require.Contains(t, o.QRURL, "amount=100000")
	require.Contains(t, o.QRURL, "addInfo="+o.OrderCode)
	require.NotNil(t, o.ExpiresAt)
	require.Equal(t, billingNow.Add(48*time.Hour), *o.ExpiresAt)
	require.Nil(t, f.plans.active["tenant/10003"], "nothing granted before payment")

	// Past its expiry the order flips when listed.
	f.svc.now = func() time.Time { return billingNow.Add(49 * time.Hour) }
	list, err := f.svc.ListOrders(context.Background(), types.UsageOrderFilter{})
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderExpired, list[0].Status)
	_, err = f.svc.ConfirmOrder(context.Background(), o.ID, "sa")
	require.Error(t, err, "expired orders cannot be confirmed")
}

func TestBillingConfirmCreatesThenExtendsSubscription(t *testing.T) {
	f := newBillingFixture(t)
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 2, Seats: 3}, "10003", "u1")
	require.NoError(t, err)
	paid, err := f.svc.ConfirmOrder(context.Background(), o.ID, "sa")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPaid, paid.Status)
	require.Equal(t, "sa", paid.ConfirmedBy)
	sub := f.plans.active["tenant/10003"]
	require.NotNil(t, sub)
	require.Equal(t, "seat", sub.PlanID)
	require.Equal(t, 3, sub.Seats)
	require.Equal(t, billingNow.AddDate(0, 2, 0), *sub.EndsAt)
	require.Equal(t, sub.ID, *paid.SubscriptionID)

	// Same plan again: extended from the current end.
	o2, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 3}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, int64(49_000*3), o2.TotalVND, "same seats: no proration")
	_, err = f.svc.ConfirmOrder(context.Background(), o2.ID, "sa")
	require.NoError(t, err)
	sub = f.plans.active["tenant/10003"]
	require.Equal(t, billingNow.AddDate(0, 3, 0), *sub.EndsAt)
	require.Equal(t, 3, sub.Seats)

	// A different plan replaces it, starting now.
	o3, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(context.Background(), o3.ID, "sa")
	require.NoError(t, err)
	sub = f.plans.active["tenant/10003"]
	require.Equal(t, "pool", sub.PlanID)
	require.Equal(t, billingNow, sub.StartsAt)
	require.Equal(t, billingNow.AddDate(0, 1, 0), *sub.EndsAt)
	// Paid orders cannot be confirmed twice.
	_, err = f.svc.ConfirmOrder(context.Background(), o3.ID, "sa")
	require.Error(t, err)
}

func TestBillingFreeOrderConfirmsItself(t *testing.T) {
	f := newBillingFixture(t)
	f.promo(t, types.UsagePromoCode{Code: "FULL", DiscountType: "percent", DiscountValue: 100, IsActive: true})
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "user", PlanID: "me", Months: 1, PromoCode: "FULL"}, "user-7", "user-7")
	require.NoError(t, err)
	require.Equal(t, types.UsageOrderPaid, o.Status)
	require.Equal(t, int64(0), o.TotalVND)
	require.Equal(t, "", o.QRURL)
	require.NotNil(t, f.plans.active["user/user-7"])
}

func TestBillingPromoValidation(t *testing.T) {
	f := newBillingFixture(t)
	_, err := f.svc.CreatePromo(context.Background(), &types.UsagePromoCode{Code: "bad code", DiscountType: "percent", DiscountValue: 10})
	require.Error(t, err)
	_, err = f.svc.CreatePromo(context.Background(), &types.UsagePromoCode{Code: "P", DiscountType: "percent", DiscountValue: 150})
	require.Error(t, err)
	_, err = f.svc.CreatePromo(context.Background(), &types.UsagePromoCode{Code: "P", DiscountType: "amount", DiscountValue: 0})
	require.Error(t, err)
	p, err := f.svc.CreatePromo(context.Background(), &types.UsagePromoCode{Code: "ok-1", DiscountType: "amount", DiscountValue: 5000})
	require.NoError(t, err)
	require.Equal(t, "OK-1", p.Code)
	require.Equal(t, billingNow, p.StartsAt)
}

func TestBillingConfirmRetryAfterFailedSaveAppliesOnce(t *testing.T) {
	f := newBillingFixture(t)
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1}, "10003", "u1")
	require.NoError(t, err)
	f.repo.failUpdateOrder = true
	_, err = f.svc.ConfirmOrder(context.Background(), o.ID, "sa")
	require.Error(t, err)
	require.Nil(t, f.plans.active["tenant/10003"], "the failed confirmation granted nothing")
	require.Equal(t, types.UsageOrderPending, f.repo.orders[o.ID].Status)

	_, err = f.svc.ConfirmOrder(context.Background(), o.ID, "sa")
	require.NoError(t, err)
	require.Equal(t, billingNow.AddDate(0, 1, 0), *f.plans.active["tenant/10003"].EndsAt, "one month for a one-month order")
	_, err = f.svc.ConfirmOrder(context.Background(), o.ID, "sa")
	require.Error(t, err)
	require.Equal(t, billingNow.AddDate(0, 1, 0), *f.plans.active["tenant/10003"].EndsAt)
}

func TestBillingSeatRenewalMustCoverCurrentSeats(t *testing.T) {
	f := newBillingFixture(t)
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 100}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(context.Background(), o.ID, "sa")
	require.NoError(t, err)

	// paying one seat cannot extend a hundred
	_, err = f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 1}, "10003")
	require.Error(t, err)
	_, err = f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 1}, "10003", "u1")
	require.Error(t, err)
	require.Equal(t, billingNow.AddDate(0, 1, 0), *f.plans.active["tenant/10003"].EndsAt)
	// another workspace has no subscription to cover
	_, err = f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 1}, "10004")
	require.NoError(t, err)
}

func TestBillingSeatRenewalWithMoreSeatsPaysTheRestOfThePeriod(t *testing.T) {
	f := newBillingFixture(t)
	o, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 10}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(context.Background(), o.ID, "sa")
	require.NoError(t, err)

	// 31 days left (13 Oct → 13 Nov): 5 added seats pay 31/30 of a month
	q, err := f.svc.Quote(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 15}, "10003")
	require.NoError(t, err)
	require.Equal(t, 10, q.CurrentSeats)
	require.Equal(t, 31, q.ProratedDays)
	require.Equal(t, int64((49_000*5*31+29)/30), q.ProrationVND)
	require.Equal(t, int64(49_000*15)+q.ProrationVND, q.SubtotalVND)

	o2, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 15}, "10003", "u1")
	require.NoError(t, err)
	require.Equal(t, q.TotalVND, o2.TotalVND)
	_, err = f.svc.ConfirmOrder(context.Background(), o2.ID, "sa")
	require.NoError(t, err)
	sub := f.plans.active["tenant/10003"]
	require.Equal(t, 15, sub.Seats)
	require.Equal(t, billingNow.AddDate(0, 2, 0), *sub.EndsAt)
}

func TestBillingConfirmRefusesOrderOutgrownBySubscription(t *testing.T) {
	f := newBillingFixture(t)
	first, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(context.Background(), first.ID, "sa")
	require.NoError(t, err)
	// both renewals priced against 5 seats; the bigger one is paid first
	small, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 5}, "10003", "u1")
	require.NoError(t, err)
	big, err := f.svc.CreateOrder(context.Background(), types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "seat", Months: 1, Seats: 50}, "10003", "u1")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(context.Background(), big.ID, "sa")
	require.NoError(t, err)
	_, err = f.svc.ConfirmOrder(context.Background(), small.ID, "sa")
	require.Error(t, err, "5 paid seats cannot extend 50")
	require.Equal(t, billingNow.AddDate(0, 2, 0), *f.plans.active["tenant/10003"].EndsAt)
	require.Equal(t, types.UsageOrderPending, f.repo.orders[small.ID].Status)
}

func TestBillingSingleUsePromoCannotBeTakenTwice(t *testing.T) {
	f := newBillingFixture(t)
	f.promo(t, types.UsagePromoCode{Code: "ONCE", DiscountType: "percent", DiscountValue: 10, IsActive: true, MaxUses: 1})
	req := types.UsageQuoteRequest{ScopeType: "tenant", PlanID: "pool", Months: 1, PromoCode: "ONCE"}
	_, err := f.svc.CreateOrder(context.Background(), req, "10003", "u1")
	require.NoError(t, err)
	// a concurrent request that read the code while it still had its use
	f.repo.stalePromoReads = true
	_, err = f.svc.CreateOrder(context.Background(), req, "10004", "u2")
	require.Error(t, err)
	for _, p := range f.repo.promos {
		require.Equal(t, 1, p.UsedCount)
	}
	require.Len(t, f.repo.orders, 1, "the refused order was not created")
}
