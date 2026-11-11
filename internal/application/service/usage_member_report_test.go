package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// reportTenants serves two organizations and the public portal.
type reportTenants struct{ interfaces.TenantRepository }

func (reportTenants) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	switch id {
	case 10001, 10002:
		return &types.Tenant{ID: id, Name: "Sở " + types.UsageTenantScopeID(id)}, nil
	case 10050:
		return &types.Tenant{ID: id, Kind: types.TenantKindPublicPortal}, nil
	}
	return nil, gorm.ErrRecordNotFound
}

type memberReportFixture struct {
	db      *gorm.DB
	meter   *usageMeter
	service *usagePlanService
	members interfaces.TenantMemberRepository
}

func newMemberReportFixture(t *testing.T) *memberReportFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.UsageCounter{}, &types.UsagePlan{}, &types.UsageSubscription{},
		&types.UsageSeat{}, &types.TenantMember{}, &types.User{}, &types.Session{}))
	require.NoError(t, db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT, role TEXT,
		usage TEXT, created_at DATETIME, usage_metered_at DATETIME NULL)`).Error)
	counters := repository.NewUsageCounterRepository(db)
	members := repository.NewTenantMemberRepository(db)
	svc := NewUsagePlanService(repository.NewUsagePlanRepository(db), counters, reportTenants{},
		repository.NewUserRepository(db), members, nil, nil).(*usagePlanService)
	svc.now = func() time.Time { return time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC) }
	svc.guard.now = svc.now
	return &memberReportFixture{
		db: db, service: svc, members: members,
		meter: &usageMeter{repo: counters, now: time.Now},
	}
}

func (f *memberReportFixture) user(t *testing.T, id, name string) {
	t.Helper()
	require.NoError(t, f.db.Create(&types.User{ID: id, Username: name, Email: name + "@so.gov.vn", PasswordHash: "x"}).Error)
}

func (f *memberReportFixture) member(t *testing.T, tenantID uint64, userID string, role types.TenantRole) {
	t.Helper()
	require.NoError(t, f.members.Create(context.Background(), &types.TenantMember{
		UserID: userID, TenantID: tenantID, Role: role, Status: types.TenantMemberStatusActive, JoinedAt: time.Now(),
	}))
}

// turn records one question and one answer of userID in tenantID at `at`.
func (f *memberReportFixture) turn(t *testing.T, tenantID uint64, userID string, at time.Time, tokens int, costMicro int64) {
	t.Helper()
	f.meter.now = func() time.Time { return at }
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	f.meter.RecordQuestion(ctx, tenantID, userID, "")
	// the meter claims the saved message row before counting its tokens
	msgID := uuid.NewString()
	require.NoError(t, f.db.Exec(`INSERT INTO messages (id, role) VALUES (?, 'assistant')`, msgID).Error)
	f.meter.RecordMessageUsage(ctx, &types.Message{ID: msgID, Role: "assistant",
		Usage: &types.TokenUsage{PromptTokens: tokens - 10, CompletionTokens: 10, TotalTokens: tokens, CostMicroVND: costMicro}})
}

func reportRow(t *testing.T, r *types.UsageMemberReport, userID string) *types.UsageMemberUsage {
	t.Helper()
	for _, m := range r.Members {
		if m.UserID == userID {
			return m
		}
	}
	t.Fatalf("no line for %s in %+v", userID, r.Members)
	return nil
}

// The report names each member of the organization with what they used in
// it alone: a member of two organizations shows only this one's turns, the
// IM/API principal has no line (its turns stay unattributed), a member who
// asked nothing is listed at zero, and someone removed keeps their line.
// The month follows the Vietnam calendar.
func TestTenantMemberUsageCountsEachMemberInThisOrganization(t *testing.T) {
	f := newMemberReportFixture(t)
	ctx := context.Background()
	for id, name := range map[string]string{"u-alice": "alice", "u-bob": "bob", "u-carol": "carol", "u-dave": "dave", "u-eve": "eve"} {
		f.user(t, id, name)
	}
	f.member(t, 10001, "u-alice", types.TenantRoleAdmin)
	f.member(t, 10001, "u-bob", types.TenantRoleMember)
	f.member(t, 10001, "u-carol", types.TenantRoleMember)
	f.member(t, 10001, "u-eve", types.TenantRoleMember)
	f.member(t, 10002, "u-alice", types.TenantRoleMember)
	f.member(t, 10002, "u-dave", types.TenantRoleMember)

	// alice holds a personal plan, bob none
	plan := &types.UsagePlan{ID: "p-own", Code: "ca-nhan", Name: "Cá nhân", ScopeKind: "user", Mode: "pool", IsActive: true}
	require.NoError(t, f.db.Create(plan).Error)
	require.NoError(t, f.db.Create(&types.UsageSubscription{ID: "s-alice", ScopeType: "user", ScopeID: "u-alice",
		PlanID: plan.ID, Mode: "pool", Status: types.UsageSubscriptionActive, StartsAt: time.Now().Add(-time.Hour)}).Error)

	nov := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)
	f.turn(t, 10001, "u-alice", nov, 1000, 5_000_000)
	f.turn(t, 10001, "u-alice", nov, 500, 2_000_000)
	f.turn(t, 10001, "u-bob", nov, 3000, 9_000_000)
	f.turn(t, 10002, "u-alice", nov, 7000, 1_000_000) // the other organization
	f.turn(t, 10001, "system-10001", nov, 400, 1_000_000)
	f.turn(t, 10001, "u-eve", nov, 200, 0)
	require.NoError(t, f.members.SoftDelete(ctx, "u-eve", 10001))
	// 2026-10-31 17:30 UTC is already 1 November in Vietnam ...
	f.turn(t, 10001, "u-bob", time.Date(2026, 10, 31, 17, 30, 0, 0, time.UTC), 100, 0)
	// ... and 16:59 UTC still 31 October
	f.turn(t, 10001, "u-bob", time.Date(2026, 10, 31, 16, 59, 0, 0, time.UTC), 50, 0)

	r, err := f.service.TenantMemberUsage(ctx, 10001, "", false)
	require.NoError(t, err)
	require.Equal(t, "2026-11", r.Month)
	require.Equal(t, types.UsageMemberSourceCounters, r.Source)
	require.False(t, r.CostVisible)

	ids := make([]string, 0, len(r.Members))
	for _, m := range r.Members {
		ids = append(ids, m.UserID)
	}
	require.Equal(t, []string{"u-bob", "u-alice", "u-eve", "u-carol"}, ids, "tokens desc; no synthetic user, no other org's member")

	alice := reportRow(t, r, "u-alice")
	require.Equal(t, int64(2), alice.Usage.Questions)
	require.Equal(t, int64(1500), alice.Usage.Tokens, "the other organization's 7000 tokens stay out")
	require.Equal(t, int64(1480), alice.Usage.PromptTokens)
	require.Equal(t, int64(20), alice.Usage.CompletionTokens)
	require.Zero(t, alice.Usage.CostMicroVND, "cost is the operator's: hidden from a Tenant Admin")
	require.Equal(t, "Cá nhân", alice.PersonalPlan)
	require.Equal(t, "alice", alice.Username)
	require.Equal(t, "alice@so.gov.vn", alice.Email)
	require.True(t, alice.Member)
	require.Nil(t, alice.HasSeat, "no seat column without a seat package")

	bob := reportRow(t, r, "u-bob")
	require.Equal(t, int64(3100), bob.Usage.Tokens, "the turn at 00:30 on 1 November (Vietnam) counts in November")
	require.Equal(t, int64(2), bob.Usage.Questions)
	require.Empty(t, bob.PersonalPlan)

	carol := reportRow(t, r, "u-carol")
	require.Zero(t, carol.Usage.Tokens)
	require.True(t, carol.Member)

	eve := reportRow(t, r, "u-eve")
	require.False(t, eve.Member, "removed this month, still accounted for")
	require.Empty(t, eve.Role)

	require.Equal(t, int64(1500+3100+200+400), r.Total.Tokens)
	require.Equal(t, int64(1500+3100+200), r.MembersTotal.Tokens)
	require.Equal(t, int64(400), r.Unattributed.Tokens, "the IM/API principal")
	require.Equal(t, int64(1), r.Unattributed.Questions)
	require.Zero(t, r.Total.CostMicroVND)

	oct, err := f.service.TenantMemberUsage(ctx, 10001, "2026-10", false)
	require.NoError(t, err)
	require.Equal(t, int64(50), reportRow(t, oct, "u-bob").Usage.Tokens)

	// a SuperAdmin sees the cost
	r, err = f.service.TenantMemberUsage(ctx, 10001, "2026-11", true)
	require.NoError(t, err)
	require.True(t, r.CostVisible)
	require.Equal(t, int64(7_000_000), reportRow(t, r, "u-alice").Usage.CostMicroVND)
	require.Equal(t, int64(17_000_000), r.Total.CostMicroVND)

	// the other organization sees alice's 7000 tokens there and never bob
	other, err := f.service.TenantMemberUsage(ctx, 10002, "2026-11", false)
	require.NoError(t, err)
	require.Len(t, other.Members, 2)
	require.Equal(t, int64(7000), reportRow(t, other, "u-alice").Usage.Tokens)
	for _, m := range other.Members {
		require.NotEqual(t, "u-bob", m.UserID)
	}
}

// In seat mode each line says whether the member holds a seat, and the
// package comes with the report for the quota.
func TestTenantMemberUsageSeatMode(t *testing.T) {
	f := newMemberReportFixture(t)
	f.user(t, "u-dave", "dave")
	f.user(t, "u-fay", "fay")
	f.member(t, 10002, "u-dave", types.TenantRoleMember)
	f.member(t, 10002, "u-fay", types.TenantRoleMember)
	plan := &types.UsagePlan{ID: "p-seat", Code: "seat", Name: "Theo chỗ", ScopeKind: "tenant", Mode: "seat", IsActive: true,
		Limits: types.UsageLimits{{Metric: types.UsageMetricTokens, Period: types.UsagePeriodMonth, Limit: 1_000_000}}}
	require.NoError(t, f.db.Create(plan).Error)
	require.NoError(t, f.db.Create(&types.UsageSubscription{ID: "s-org", ScopeType: "tenant", ScopeID: "10002",
		PlanID: plan.ID, Mode: "seat", Seats: 5, Status: types.UsageSubscriptionActive, StartsAt: time.Now().Add(-time.Hour)}).Error)
	require.NoError(t, f.db.Create(&types.UsageSeat{SubscriptionID: "s-org", UserID: "u-dave", AssignedAt: time.Now()}).Error)

	r, err := f.service.TenantMemberUsage(context.Background(), 10002, "2026-11", false)
	require.NoError(t, err)
	require.NotNil(t, r.Subscription)
	require.Equal(t, "Theo chỗ", r.Subscription.Plan.Name)
	require.NotNil(t, reportRow(t, r, "u-dave").HasSeat)
	require.True(t, *reportRow(t, r, "u-dave").HasSeat)
	require.False(t, *reportRow(t, r, "u-fay").HasSeat)
}

// A month before member counting is summed from the chat history of the
// organization's sessions (questions and tokens; no OCR, no cost), with the
// IM/API sessions left out.
func TestTenantMemberUsageFallsBackToChatHistory(t *testing.T) {
	f := newMemberReportFixture(t)
	f.user(t, "u-alice", "alice")
	f.member(t, 10001, "u-alice", types.TenantRoleMember)
	mine := &types.Session{TenantID: 10001, UserID: "u-alice", Title: "a"}
	bot := &types.Session{TenantID: 10001, UserID: "system-10001", Title: "im"}
	elsewhere := &types.Session{TenantID: 10002, UserID: "u-alice", Title: "b"}
	require.NoError(t, f.db.Create([]*types.Session{mine, bot, elsewhere}).Error)
	// a deleted session's usage was spent all the same
	require.NoError(t, f.db.Delete(mine).Error)
	msg := func(id, session, role, usage string, at time.Time) {
		require.NoError(t, f.db.Exec(`INSERT INTO messages (id, session_id, role, usage, created_at) VALUES (?, ?, ?, ?, ?)`,
			id, session, role, usage, at.UTC()).Error)
	}
	sep := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	msg("m1", mine.ID, "user", "", sep)
	msg("m2", mine.ID, "assistant", `{"prompt_tokens":90,"completion_tokens":10,"total_tokens":100}`, sep)
	msg("m3", mine.ID, "user", "", sep)
	msg("m4", mine.ID, "assistant", `{"prompt_tokens":40,"completion_tokens":10}`, sep) // no total: prompt + completion
	msg("m5", bot.ID, "user", "", sep)
	msg("m6", bot.ID, "assistant", `{"total_tokens":999}`, sep)
	msg("m7", elsewhere.ID, "user", "", sep)
	// 30 September 17:30 UTC is 1 October in Vietnam: not September
	msg("m8", mine.ID, "user", "", time.Date(2026, 9, 30, 17, 30, 0, 0, time.UTC))
	// 31 August 17:30 UTC is already 1 September
	msg("m9", mine.ID, "user", "", time.Date(2026, 8, 31, 17, 30, 0, 0, time.UTC))

	r, err := f.service.TenantMemberUsage(context.Background(), 10001, "2026-09", true)
	require.NoError(t, err)
	require.Equal(t, types.UsageMemberSourceMessages, r.Source)
	require.Len(t, r.Members, 1)
	alice := r.Members[0]
	require.Equal(t, int64(3), alice.Usage.Questions)
	require.Equal(t, int64(150), alice.Usage.Tokens)
	require.Equal(t, int64(130), alice.Usage.PromptTokens)
}

func TestTenantMemberUsageRejectsBadMonthAndHiddenTenants(t *testing.T) {
	f := newMemberReportFixture(t)
	ctx := context.Background()
	for _, month := range []string{"2026-13", "11-2026", "2026-1", "2026-12"} {
		_, err := f.service.TenantMemberUsage(ctx, 10001, month, false)
		require.Error(t, err, month)
	}
	_, err := f.service.TenantMemberUsage(ctx, 10050, "", true)
	require.Error(t, err, "the public portal has no members")
	_, err = f.service.TenantMemberUsage(ctx, 99999, "", true)
	require.Error(t, err)
}

// The member counters change no quota: a seat cap still reads the person's
// "user" counters, which keep adding every organization.
func TestMemberCountersLeaveUserCountersAlone(t *testing.T) {
	f := newMemberReportFixture(t)
	at := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)
	f.turn(t, 10001, "u-alice", at, 100, 0)
	f.turn(t, 10002, "u-alice", at, 200, 0)
	rows, err := f.service.counters.Get(context.Background(), types.UsageScopeUser, "u-alice", []string{"2026-11"})
	require.NoError(t, err)
	got := types.UsagePeriodTotals{}
	for _, r := range rows {
		got.AddMetric(r.Metric, r.Value)
	}
	require.Equal(t, int64(300), got.Tokens)
	require.Equal(t, int64(2), got.Questions)
}
