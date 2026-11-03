package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/mailer"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ---- fakes -------------------------------------------------------------------

type mailSettings struct {
	interfaces.SystemSettingService
	values map[string]any
}

func (s *mailSettings) GetBool(_ context.Context, key, _ string, def bool) bool {
	if v, ok := s.values[key].(bool); ok {
		return v
	}
	return def
}
func (s *mailSettings) GetString(_ context.Context, key, _ string, def string) string {
	if v, ok := s.values[key].(string); ok {
		return v
	}
	return def
}
func (s *mailSettings) GetInt(_ context.Context, key, _ string, def int64) int64 {
	if v, ok := s.values[key].(int64); ok {
		return v
	}
	return def
}

func smtpOn() *mailSettings {
	return &mailSettings{values: map[string]any{
		types.SMTPEnabledSettingKey:     true,
		types.SMTPHostSettingKey:        "smtp.example.com",
		types.SMTPFromAddressSettingKey: "no-reply@example.com",
		types.SMTPPasswordSettingKey:    "hunter2",
	}}
}

type fakeMailSender struct {
	mu   sync.Mutex
	sent []mailer.Message
	fail error
}

func (f *fakeMailSender) Send(_ context.Context, cfg mailer.Config, msg mailer.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !cfg.Ready() {
		return errors.New("not ready")
	}
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeMailSender) recipients() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.sent {
		out = append(out, m.To...)
	}
	return out
}

type mailUserRepo struct {
	interfaces.UserRepository
	users map[string]*types.User
}

func (r *mailUserRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}
func (r *mailUserRepo) GetUsersByIDs(_ context.Context, ids []string) (map[string]*types.User, error) {
	out := map[string]*types.User{}
	for _, id := range ids {
		if u, ok := r.users[id]; ok {
			out[id] = u
		}
	}
	return out, nil
}
func (r *mailUserRepo) ListSystemAdmins(context.Context, int, int) ([]*types.User, int64, error) {
	var out []*types.User
	for _, u := range r.users {
		if u.IsSystemAdmin {
			out = append(out, u)
		}
	}
	return out, int64(len(out)), nil
}

type mailMemberRepo struct {
	interfaces.TenantMemberRepository
	byTenant map[uint64][]*types.TenantMember
}

func (r *mailMemberRepo) ListByTenant(_ context.Context, id uint64) ([]*types.TenantMember, error) {
	return r.byTenant[id], nil
}

type mailTenantRepo struct {
	interfaces.TenantRepository
}

func (mailTenantRepo) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: id, Name: "Sở Tư pháp"}, nil
}

type mailPlanRepo struct {
	interfaces.UsagePlanRepository
	subs map[string]*types.UsageSubscription
}

func (r *mailPlanRepo) GetSubscription(_ context.Context, id string) (*types.UsageSubscription, error) {
	if s, ok := r.subs[id]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

// memEmailStore is the in-memory email_notifications table.
type memEmailStore struct {
	mu     sync.Mutex
	claims map[string]*types.EmailNotification
	subs   []*types.UsageSubscription
}

func (m *memEmailStore) Claim(_ context.Context, n *types.EmailNotification) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.claims[n.DedupeKey]; ok {
		return false, nil
	}
	cp := *n
	m.claims[n.DedupeKey] = &cp
	return true, nil
}
func (m *memEmailStore) Release(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.claims, key)
	return nil
}
func (m *memEmailStore) SetRecipients(_ context.Context, key string, n int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.claims[key]; ok {
		c.Recipients = n
	}
	return nil
}
func (m *memEmailStore) ListSubscriptionsEndingBetween(_ context.Context, from, to time.Time) ([]*types.UsageSubscription, error) {
	var out []*types.UsageSubscription
	for _, s := range m.subs {
		if s.Status == types.UsageSubscriptionActive && s.EndsAt != nil && s.EndsAt.After(from) && !s.EndsAt.After(to) {
			out = append(out, s)
		}
	}
	return out, nil
}

func vnTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, types.UsageLocation())
	require.NoError(t, err)
	return v
}

type mailFixture struct {
	n        *EmailNotifier
	sender   *fakeMailSender
	store    *memEmailStore
	settings *mailSettings
	now      time.Time
}

func newMailFixture(t *testing.T, settings *mailSettings) *mailFixture {
	t.Helper()
	f := &mailFixture{sender: &fakeMailSender{}, store: &memEmailStore{claims: map[string]*types.EmailNotification{}},
		settings: settings, now: vnTime(t, "2027-01-01 09:00")}
	users := &mailUserRepo{users: map[string]*types.User{
		"sa":       {ID: "sa", Username: "super", Email: "super@example.com", IsActive: true, IsSystemAdmin: true},
		"sa-off":   {ID: "sa-off", Email: "gone@example.com", IsActive: false, IsSystemAdmin: true},
		"admin":    {ID: "admin", Username: "admin", Email: "admin@example.com", IsActive: true},
		"member":   {ID: "member", Email: "member@example.com", IsActive: true},
		"personal": {ID: "personal", Username: "an", Email: "an@example.com", IsActive: true},
	}}
	members := &mailMemberRepo{byTenant: map[uint64][]*types.TenantMember{
		7: {
			{UserID: "admin", TenantID: 7, Role: types.TenantRoleAdmin, Status: types.TenantMemberStatusActive},
			{UserID: "member", TenantID: 7, Role: types.TenantRoleMember, Status: types.TenantMemberStatusActive},
		},
	}}
	f.n = NewEmailNotifier(nil, settings, users, members, mailTenantRepo{}, &mailPlanRepo{subs: map[string]*types.UsageSubscription{}}, f.store)
	f.n.sender = f.sender
	f.n.baseURL = func() string { return "https://ai.example.vn" }
	f.n.now = func() time.Time { return f.now }
	f.n.async = func(fn func()) { fn() }
	return f
}

func endsVN(t *testing.T, date string) *time.Time {
	// a package end is stored as the Vietnam midnight after its last day
	e := vnTime(t, date+" 00:00")
	return &e
}

// ---- reminders ---------------------------------------------------------------

func TestDueExpiryReminderThresholds(t *testing.T) {
	cases := map[int]int{20: 0, 15: 0, 14: 14, 10: 14, 8: 14, 7: 7, 5: 7, 2: 7, 1: 1, 0: 1, -1: 0}
	for days, want := range cases {
		require.Equal(t, want, types.DueExpiryReminder(days), "days left %d", days)
	}
	// end 15/01 00:00 VN = last usable day 14/01; on 07/01 that is 7 days ahead
	now := vnTime(t, "2027-01-07 23:30")
	require.Equal(t, 7, types.DaysUntilLastUsableDayVN(now, *endsVN(t, "2027-01-15")))
	require.Equal(t, "14/01/2027", formatDateVN(types.LastUsableDayVN(*endsVN(t, "2027-01-15"))))
}

func TestExpiryRemindersOncePerThreshold(t *testing.T) {
	f := newMailFixture(t, smtpOn())
	ctx := context.Background()
	f.store.subs = []*types.UsageSubscription{
		{ID: "sub-t", ScopeType: types.UsageScopeTenant, ScopeID: "7", Status: types.UsageSubscriptionActive,
			EndsAt: endsVN(t, "2027-01-11"), Plan: &types.UsagePlan{Name: "Cơ bản"}},
		{ID: "sub-u", ScopeType: types.UsageScopeUser, ScopeID: "personal", Status: types.UsageSubscriptionActive,
			EndsAt: endsVN(t, "2027-01-30"), Plan: &types.UsagePlan{Name: "Go"}},
		// not reminded: the SuperAdmin package
		{ID: "sub-sa", ScopeType: types.UsageScopeUser, ScopeID: "sa", Status: types.UsageSubscriptionActive,
			EndsAt: endsVN(t, "2027-01-03"), Plan: &types.UsagePlan{Name: "SA", Code: types.UsageSystemAdminPlanCode}},
	}

	// 01/01: sub-t's last day is 10/01 (9 days) → 14-day reminder, to the
	// Tenant Admin only; sub-u (29 days) is not due
	n, err := f.n.RunExpiryReminders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []string{"admin@example.com"}, f.sender.recipients())
	msg := f.sender.sent[0]
	require.Contains(t, msg.Subject, "Cơ bản")
	require.Contains(t, msg.Text, "dùng được đến hết ngày 10/01/2027")
	require.Contains(t, msg.Text, "Tổ chức Sở Tư pháp")
	require.Contains(t, msg.Text, "https://ai.example.vn/platform/system/workspace/billing")

	// the same day again, and a restart (same store): nothing more
	n, err = f.n.RunExpiryReminders(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Len(t, f.sender.sent, 1)

	// 03/01: 7 days left → the 7-day reminder, once
	f.now = vnTime(t, "2027-01-03 10:00")
	_, err = f.n.RunExpiryReminders(ctx)
	require.NoError(t, err)
	_, err = f.n.RunExpiryReminders(ctx)
	require.NoError(t, err)
	require.Len(t, f.sender.sent, 2)
	require.Contains(t, f.sender.sent[1].Text, "còn 7 ngày")

	// 09/01: 1 day left
	f.now = vnTime(t, "2027-01-09 10:00")
	_, err = f.n.RunExpiryReminders(ctx)
	require.NoError(t, err)
	require.Len(t, f.sender.sent, 3)
	require.Contains(t, f.sender.sent[2].Text, "còn 1 ngày")

	// a renewal moves the end: the new period is reminded afresh
	f.store.subs[0].EndsAt = endsVN(t, "2027-01-20")
	_, err = f.n.RunExpiryReminders(ctx)
	require.NoError(t, err)
	require.Len(t, f.sender.sent, 4)
	require.Len(t, f.store.claims, 4)
	for _, c := range f.store.claims {
		require.Equal(t, 1, c.Recipients)
	}
}

func TestExpiryReminderFailedSendIsRetried(t *testing.T) {
	f := newMailFixture(t, smtpOn())
	ctx := context.Background()
	f.store.subs = []*types.UsageSubscription{{ID: "sub-u", ScopeType: types.UsageScopeUser, ScopeID: "personal",
		Status: types.UsageSubscriptionActive, EndsAt: endsVN(t, "2027-01-05"), Plan: &types.UsagePlan{Name: "Go"}}}
	f.sender.fail = errors.New("535 auth failed")
	n, err := f.n.RunExpiryReminders(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Empty(t, f.store.claims, "a failed send releases its claim")

	f.sender.fail = nil
	n, err = f.n.RunExpiryReminders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []string{"an@example.com"}, f.sender.recipients())
}

func TestEmailSkippedWhenSMTPOff(t *testing.T) {
	for name, settings := range map[string]*mailSettings{
		"disabled": {values: map[string]any{types.SMTPEnabledSettingKey: false,
			types.SMTPHostSettingKey: "smtp.example.com", types.SMTPFromAddressSettingKey: "no-reply@example.com"}},
		"no host": {values: map[string]any{types.SMTPEnabledSettingKey: true,
			types.SMTPFromAddressSettingKey: "no-reply@example.com"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMailFixture(t, settings)
			ctx := context.Background()
			f.store.subs = []*types.UsageSubscription{{ID: "s", ScopeType: types.UsageScopeUser, ScopeID: "personal",
				Status: types.UsageSubscriptionActive, EndsAt: endsVN(t, "2027-01-05")}}
			n, err := f.n.RunExpiryReminders(ctx)
			require.NoError(t, err)
			require.Equal(t, 0, n)
			require.Empty(t, f.store.claims, "nothing is claimed while SMTP is off")

			o := testOrderView()
			f.n.OrderCreated(ctx, o)
			o.Status = types.UsageOrderPaid
			f.n.OrderPaid(ctx, o)
			require.Empty(t, f.sender.sent)
			require.Error(t, f.n.SendTest(ctx, "super@example.com"))
		})
	}
}

// ---- orders ------------------------------------------------------------------

func testOrderView() *types.UsageOrderView {
	created := time.Date(2027, 1, 1, 7, 5, 0, 0, time.UTC) // 14h05 VN
	return &types.UsageOrderView{
		UsageOrder: &types.UsageOrder{
			ID: "o1", OrderCode: "WR7K2M", ScopeType: types.UsageScopeTenant, ScopeID: "7",
			PlanName: "Cơ bản", Mode: types.UsageModeSeat, Seats: 5, Months: 3, TotalVND: 1250000,
			Status: types.UsageOrderPending, CreatedBy: "admin", CreatedAt: created, Kind: types.UsageOrderKindPlan,
			Addons: types.UsageOrderAddonLines{{AgentID: "a1", Name: "Thuế HKD"}},
		},
		ScopeName: "Sở <Tư> pháp",
	}
}

func TestOrderCreatedMailsActiveSuperAdmins(t *testing.T) {
	f := newMailFixture(t, smtpOn())
	f.n.OrderCreated(context.Background(), testOrderView())
	require.Equal(t, []string{"super@example.com"}, f.sender.recipients())
	m := f.sender.sent[0]
	require.Contains(t, m.Subject, "WR7K2M")
	require.Contains(t, m.Text, "Tổ chức Sở <Tư> pháp")
	require.Contains(t, m.Text, "Cơ bản · 3 tháng · 5 chỗ · Trợ lý chuyên ngành: Thuế HKD")
	require.Contains(t, m.Text, "1.250.000 đ")
	require.Contains(t, m.Text, "14h05 - 01/01/2027")
	require.Contains(t, m.Text, "https://ai.example.vn/platform/admin?section=billing")
	require.Contains(t, m.HTML, "Sở &lt;Tư&gt; pháp", "HTML is escaped")
	require.NotContains(t, m.HTML, "Sở <Tư>")

	// a 0-đồng order is mailed too
	f.sender.sent = nil
	o := testOrderView()
	o.TotalVND = 0
	f.n.OrderCreated(context.Background(), o)
	require.Len(t, f.sender.sent, 1)
	require.Contains(t, f.sender.sent[0].Text, "0 đ")
}

func TestOrderPaidMailsBuyerWithLastDay(t *testing.T) {
	f := newMailFixture(t, smtpOn())
	sub := &types.UsageSubscription{ID: "sub1", EndsAt: endsVN(t, "2027-04-02")}
	f.n.plans.(*mailPlanRepo).subs["sub1"] = sub
	o := testOrderView()
	o.Status = types.UsageOrderPaid
	o.SubscriptionID = &sub.ID
	paid := time.Date(2027, 1, 1, 8, 0, 0, 0, time.UTC)
	o.PaidAt = &paid
	f.n.OrderPaid(context.Background(), o)
	require.Equal(t, []string{"admin@example.com"}, f.sender.recipients())
	require.Contains(t, f.sender.sent[0].Text, "dùng được đến hết ngày 01/04/2027")

	// a pending order is never announced as paid
	f.sender.sent = nil
	f.n.OrderPaid(context.Background(), testOrderView())
	require.Empty(t, f.sender.sent)
}

func TestSendTestReturnsSMTPError(t *testing.T) {
	f := newMailFixture(t, smtpOn())
	require.NoError(t, f.n.SendTest(context.Background(), "super@example.com"))
	require.Equal(t, []string{"super@example.com"}, f.sender.recipients())
	f.sender.fail = errors.New("535 5.7.8 Username and Password not accepted")
	err := f.n.SendTest(context.Background(), "super@example.com")
	require.ErrorContains(t, err, "535")
	require.Error(t, f.n.SendTest(context.Background(), "not-an-email"))
}

// ---- billing decorator -------------------------------------------------------

type stubBilling struct {
	interfaces.UsageBillingService
	created *types.UsageOrderView
	paid    *types.UsageOrderView
	payment *types.UsagePaymentResult
}

func (s *stubBilling) CreateOrder(context.Context, types.UsageQuoteRequest, string, string) (*types.UsageOrderView, error) {
	return s.created, nil
}
func (s *stubBilling) ConfirmOrder(context.Context, string, string) (*types.UsageOrderView, error) {
	return s.paid, nil
}
func (s *stubBilling) RecordPayment(context.Context, types.UsagePaymentWebhookRequest, []byte) (*types.UsagePaymentResult, error) {
	return s.payment, nil
}

type recordingNotifier struct{ created, paid []string }

func (r *recordingNotifier) OrderCreated(_ context.Context, o *types.UsageOrderView) {
	r.created = append(r.created, o.OrderCode)
}
func (r *recordingNotifier) OrderPaid(_ context.Context, o *types.UsageOrderView) {
	r.paid = append(r.paid, o.OrderCode)
}

func TestBillingDecoratorNotifies(t *testing.T) {
	pending := testOrderView()
	paid := testOrderView()
	paid.Status = types.UsageOrderPaid
	inner := &stubBilling{created: pending, paid: paid}
	rec := &recordingNotifier{}
	b := &billingEmailNotify{UsageBillingService: inner, notifier: rec}
	ctx := context.Background()

	_, err := b.CreateOrder(ctx, types.UsageQuoteRequest{}, "7", "admin")
	require.NoError(t, err)
	_, err = b.ConfirmOrder(ctx, "o1", "sa")
	require.NoError(t, err)
	require.Equal(t, []string{"WR7K2M"}, rec.created)
	require.Equal(t, []string{"WR7K2M"}, rec.paid)

	// webhook: matched → mailed; a resend or an unmatched transfer → not
	for _, tc := range []struct {
		status string
		dup    bool
		want   int
	}{
		{types.UsagePaymentMatched, false, 2},
		{types.UsagePaymentMatched, true, 2},
		{types.UsagePaymentUnderpaid, false, 2},
	} {
		inner.payment = &types.UsagePaymentResult{
			Transaction: &types.UsagePaymentTransaction{Status: tc.status}, Order: paid, Duplicate: tc.dup,
		}
		_, err := b.RecordPayment(ctx, types.UsagePaymentWebhookRequest{}, nil)
		require.NoError(t, err)
		require.Len(t, rec.paid, tc.want, "%s dup=%v", tc.status, tc.dup)
	}
}

func TestFormatVND(t *testing.T) {
	require.Equal(t, "0 đ", formatVND(0))
	require.Equal(t, "999 đ", formatVND(999))
	require.Equal(t, "1.000 đ", formatVND(1000))
	require.Equal(t, "12.345.678 đ", formatVND(12345678))
	require.True(t, strings.HasPrefix(formatVND(-5000), "-5.000"))
}
