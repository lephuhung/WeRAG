package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

func setupEmailNotificationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE email_notifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind VARCHAR(32) NOT NULL,
		dedupe_key VARCHAR(200) NOT NULL UNIQUE,
		ref_id VARCHAR(64) NOT NULL,
		recipients INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE usage_plans (
		id VARCHAR(36) PRIMARY KEY, code VARCHAR(64), name VARCHAR(128), description TEXT,
		scope_kind VARCHAR(16), mode VARCHAR(16), limits TEXT, features TEXT, price_label VARCHAR(128),
		price_vnd INTEGER, price_quarter_vnd INTEGER, price_year_vnd INTEGER, sort_order INTEGER,
		is_active BOOLEAN, created_at DATETIME, updated_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE usage_subscriptions (
		id VARCHAR(36) PRIMARY KEY, scope_type VARCHAR(16), scope_id VARCHAR(64), plan_id VARCHAR(36),
		mode VARCHAR(16), seats INTEGER, limits_override TEXT, starts_at DATETIME, ends_at DATETIME,
		status VARCHAR(16), note TEXT, created_at DATETIME, updated_at DATETIME)`).Error)
	return db
}

func TestEmailNotificationClaimIsOnceOnly(t *testing.T) {
	db := setupEmailNotificationDB(t)
	repo := NewEmailNotificationRepository(db)
	ctx := context.Background()
	n := func() *types.EmailNotification {
		return &types.EmailNotification{Kind: types.EmailNotificationSubscriptionExpiry, DedupeKey: "k1", RefID: "sub"}
	}
	ok, err := repo.Claim(ctx, n())
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.Claim(ctx, n())
	require.NoError(t, err)
	require.False(t, ok, "a second claim of the same key loses")

	require.NoError(t, repo.SetRecipients(ctx, "k1", 3))
	var row types.EmailNotification
	require.NoError(t, db.Where("dedupe_key = ?", "k1").First(&row).Error)
	require.Equal(t, 3, row.Recipients)

	require.NoError(t, repo.Release(ctx, "k1"))
	ok, err = repo.Claim(ctx, n())
	require.NoError(t, err)
	require.True(t, ok, "a released claim can be taken again")
}

func TestListSubscriptionsEndingBetween(t *testing.T) {
	db := setupEmailNotificationDB(t)
	repo := NewEmailNotificationRepository(db)
	ctx := context.Background()
	now := time.Date(2027, 1, 1, 2, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&types.UsagePlan{ID: "p", Code: "go", Name: "Go", IsActive: true}).Error)
	at := func(d int) *time.Time { v := now.AddDate(0, 0, d); return &v }
	for _, s := range []*types.UsageSubscription{
		{ID: "in", PlanID: "p", Status: types.UsageSubscriptionActive, EndsAt: at(5)},
		{ID: "past", PlanID: "p", Status: types.UsageSubscriptionActive, EndsAt: at(-1)},
		{ID: "far", PlanID: "p", Status: types.UsageSubscriptionActive, EndsAt: at(40)},
		{ID: "cancelled", PlanID: "p", Status: types.UsageSubscriptionCancelled, EndsAt: at(5)},
		{ID: "open", PlanID: "p", Status: types.UsageSubscriptionActive},
	} {
		require.NoError(t, db.Omit("Plan").Create(s).Error)
	}
	subs, err := repo.ListSubscriptionsEndingBetween(ctx, now, now.AddDate(0, 0, 16))
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Equal(t, "in", subs[0].ID)
	require.NotNil(t, subs[0].Plan)
	require.Equal(t, "Go", subs[0].Plan.Name)
}

func TestEmailNotificationReleaseStale(t *testing.T) {
	db := setupEmailNotificationDB(t)
	repo := NewEmailNotificationRepository(db)
	ctx := context.Background()
	now := time.Date(2027, 1, 1, 2, 0, 0, 0, time.UTC)
	kind := types.EmailNotificationSubscriptionExpiry
	for _, n := range []*types.EmailNotification{
		{Kind: kind, DedupeKey: "old-pending", RefID: "a", CreatedAt: now.Add(-time.Hour)},
		{Kind: kind, DedupeKey: "new-pending", RefID: "b", CreatedAt: now.Add(-time.Minute)},
		{Kind: kind, DedupeKey: "old-sent", RefID: "c", CreatedAt: now.Add(-time.Hour)},
		{Kind: kind, DedupeKey: "old-nobody", RefID: "d", CreatedAt: now.Add(-time.Hour)},
		{Kind: "other", DedupeKey: "other-pending", RefID: "e", CreatedAt: now.Add(-time.Hour)},
	} {
		ok, err := repo.Claim(ctx, n)
		require.NoError(t, err)
		require.True(t, ok)
	}
	require.NoError(t, repo.SetRecipients(ctx, "old-sent", 2))
	require.NoError(t, repo.SetRecipients(ctx, "old-nobody", types.EmailRecipientsNone))

	released, err := repo.ReleaseStale(ctx, kind, now.Add(-types.EmailClaimStaleAfter))
	require.NoError(t, err)
	require.Equal(t, int64(1), released)
	var keys []string
	require.NoError(t, db.Model(&types.EmailNotification{}).Order("dedupe_key").Pluck("dedupe_key", &keys).Error)
	require.Equal(t, []string{"new-pending", "old-nobody", "old-sent", "other-pending"}, keys)
}
