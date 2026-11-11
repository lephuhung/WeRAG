package service

import (
	"context"
	"encoding/json"
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

type abbrNoticeEnv struct {
	db       *gorm.DB
	svc      interfaces.AbbreviationService
	notifier *AbbreviationReviewNotifier
	notices  interfaces.UserNotificationRepository
}

func newAbbrNoticeEnv(t *testing.T) *abbrNoticeEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.UserNotification{}, &types.Abbreviation{}))
	require.NoError(t, db.Exec(`CREATE TABLE abbreviation_suggestion_locks ("key" VARCHAR(64) PRIMARY KEY)`).Error)
	for _, u := range []types.User{
		{ID: "sa1", Username: "sa1", Email: "sa1@x", IsActive: true, IsSystemAdmin: true, CanAccessAllTenants: true},
		{ID: "sa2", Username: "sa2", Email: "sa2@x", IsActive: true, IsSystemAdmin: true},
		{ID: "sa-off", Username: "sa-off", Email: "off@x", IsActive: true, IsSystemAdmin: true},
		{ID: "system-7", Username: "synthetic", Email: "syn@x", IsActive: true, IsSystemAdmin: true},
		{ID: "member", Username: "member", Email: "m@x", IsActive: true},
	} {
		u := u
		require.NoError(t, db.Create(&u).Error)
	}
	// gorm's default:true would turn a false IsActive back on at insert
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "sa-off").Update("is_active", false).Error)
	abbrRepo := repository.NewAbbreviationRepository(db)
	notices := repository.NewUserNotificationRepository(db)
	notifier := NewAbbreviationReviewNotifier(repository.NewUserRepository(db), notices, abbrRepo)
	return &abbrNoticeEnv{db: db, svc: NewAbbreviationService(abbrRepo, nil, notifier), notifier: notifier, notices: notices}
}

func asUser(id string) context.Context {
	return context.WithValue(context.Background(), types.UserIDContextKey, id)
}

func (e *abbrNoticeEnv) suggest(t *testing.T, by, short, full string) *types.Abbreviation {
	t.Helper()
	row, err := e.svc.Suggest(asUser(by), &types.AbbreviationCreateRequest{ShortForm: short, FullForm: full})
	require.NoError(t, err)
	e.notifier.Wait()
	return row
}

func (e *abbrNoticeEnv) digests(t *testing.T, user string, unreadOnly bool) []*types.UserNotification {
	t.Helper()
	rows, err := e.notices.List(context.Background(), user, unreadOnly, nil, 100)
	require.NoError(t, err)
	return rows
}

func digestPayload(t *testing.T, n *types.UserNotification) types.UserNotificationAbbreviationPendingPayload {
	t.Helper()
	var p types.UserNotificationAbbreviationPendingPayload
	require.NoError(t, json.Unmarshal(n.Payload, &p))
	return p
}

func TestAbbreviationReviewNoticeDigest(t *testing.T) {
	e := newAbbrNoticeEnv(t)
	ctx := context.Background()

	vp := e.suggest(t, "member", "VP", "Văn phòng")
	for _, sa := range []string{"sa1", "sa2"} {
		rows := e.digests(t, sa, true)
		require.Len(t, rows, 1, sa)
		n := rows[0]
		require.Equal(t, types.UserNotificationAbbreviationPending, n.Kind)
		require.Equal(t, types.PlatformTenantID, n.TenantID)
		require.Equal(t, "Có 1 từ viết tắt mới chờ duyệt", n.Title)
		require.Equal(t, "VP → Văn phòng", n.Body)
		require.Equal(t, AbbreviationReviewLink, n.Link)
		require.Equal(t, 1, digestPayload(t, n).Count)
	}
	for _, nobody := range []string{"member", "sa-off", "system-7"} {
		require.Empty(t, e.digests(t, nobody, false), nobody)
	}

	first := e.digests(t, "sa2", true)[0]
	_, err := e.notices.MarkSeen(ctx, "sa2", []string{first.ID}, time.Now())
	require.NoError(t, err)

	// a second suggestion updates the same digest and toasts it again
	e.suggest(t, "member", "CA", "Công an")
	rows := e.digests(t, "sa2", false)
	require.Len(t, rows, 1)
	require.Equal(t, first.ID, rows[0].ID)
	require.Nil(t, rows[0].SeenAt)
	p := digestPayload(t, rows[0])
	require.Equal(t, 2, p.Count)
	require.Equal(t, "CA", p.Items[0].ShortForm)
	require.Equal(t, "Có 2 từ viết tắt mới chờ duyệt", rows[0].Title)
	require.Equal(t, "CA → Công an, VP → Văn phòng", rows[0].Body)

	// the same pair again (an existing row) changes nothing
	again := e.suggest(t, "member", " vp ", "VĂN PHÒNG")
	require.Equal(t, vp.ID, again.ID)
	require.Equal(t, 2, digestPayload(t, e.digests(t, "sa2", true)[0]).Count)

	// a SuperAdmin's own suggestion does not notify them
	e.suggest(t, "sa1", "TW", "Trung ương")
	require.Equal(t, 2, digestPayload(t, e.digests(t, "sa1", true)[0]).Count)
	require.Equal(t, 3, digestPayload(t, e.digests(t, "sa2", true)[0]).Count)

	// after reading, the next suggestion opens a new digest
	ok, err := e.notices.MarkRead(ctx, "sa2", first.ID, time.Now())
	require.NoError(t, err)
	require.True(t, ok)
	e.suggest(t, "member", "BCA", "Bộ Công an")
	require.Len(t, e.digests(t, "sa2", false), 2)
	unread := e.digests(t, "sa2", true)
	require.Len(t, unread, 1)
	require.NotEqual(t, first.ID, unread[0].ID)
	require.Equal(t, 1, digestPayload(t, unread[0]).Count)
}

func TestAbbreviationReviewNoticeClosedWhenQueueEmpty(t *testing.T) {
	e := newAbbrNoticeEnv(t)
	ctx := context.Background()
	vp := e.suggest(t, "member", "VP", "Văn phòng")
	ca := e.suggest(t, "member", "CA", "Công an")
	require.Len(t, e.digests(t, "sa1", true), 1)

	// approving one of two leaves the digest open
	active := true
	_, err := e.svc.Update(asUser("sa1"), vp.ID, &types.AbbreviationUpdateRequest{IsActive: &active})
	require.NoError(t, err)
	e.notifier.Wait()
	require.Len(t, e.digests(t, "sa1", true), 1)

	// rejecting the last one empties the queue: every digest is read
	require.NoError(t, e.svc.Delete(asUser("sa1"), ca.ID))
	e.notifier.Wait()
	for _, sa := range []string{"sa1", "sa2"} {
		require.Empty(t, e.digests(t, sa, true), sa)
		require.Len(t, e.digests(t, sa, false), 1, sa)
	}

	// the next suggestion opens a fresh digest
	e.suggest(t, "member", "TW", "Trung ương")
	unread := e.digests(t, "sa1", true)
	require.Len(t, unread, 1)
	require.Equal(t, 1, digestPayload(t, unread[0]).Count)
	_ = ctx
}

func TestAbbreviationPendingBody(t *testing.T) {
	long := "Một tên đầy đủ rất dài vượt quá bốn mươi ký tự của thông báo"
	body := abbreviationPendingBody(types.UserNotificationAbbreviationPendingPayload{Count: 7, Items: []types.UserNotificationAbbreviationItem{
		{ShortForm: "PV01", FullForm: "PV01", ProperName: true},
		{ShortForm: "X", FullForm: long},
	}})
	require.Equal(t, "PV01 (tên riêng), X → Một tên đầy đủ rất dài vượt quá bốn mươ…, và 5 từ khác", body)
}
