package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/alerting/telegram"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const testBotToken = "123456789:AAH-test_token_abcdefghijklmnopqrstu"

// fakeBot is a Bot API server: it records the calls and answers sendMessage
// with refuse(chatID) when set, getUpdates with the queued updates.
type fakeBot struct {
	mu      sync.Mutex
	calls   []fakeBotCall
	updates []telegram.Update
	refuse  func(chatID int64) (int, string, map[string]any)
	nextID  int64
	srv     *httptest.Server
}

type fakeBotCall struct {
	Method string
	Body   map[string]any
}

func newFakeBot(t *testing.T) *fakeBot {
	b := &fakeBot{}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/bot" + testBotToken + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, prefix)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.mu.Lock()
		b.calls = append(b.calls, fakeBotCall{Method: method, Body: body})
		write := func(v any) {
			raw, _ := json.Marshal(map[string]any{"ok": true, "result": v})
			_, _ = w.Write(raw)
		}
		switch method {
		case "getMe":
			write(map[string]any{"id": 123456789, "is_bot": true, "first_name": "WeRAG cảnh báo", "username": "werag_alert_bot"})
		case "getChatMember":
			status := "member"
			if int64(body["user_id"].(float64)) == 1 {
				status = "administrator"
			}
			write(map[string]any{"status": status})
		case "getUpdates":
			ups := b.updates
			b.updates = nil
			write(ups)
		case "sendMessage":
			chat := int64(body["chat_id"].(float64))
			if b.refuse != nil {
				if code, desc, params := b.refuse(chat); code != 0 {
					raw, _ := json.Marshal(map[string]any{"ok": false, "error_code": code, "description": desc, "parameters": params})
					_, _ = w.Write(raw)
					b.mu.Unlock()
					return
				}
			}
			b.nextID++
			write(map[string]any{"message_id": b.nextID, "chat": map[string]any{"id": chat}})
		default:
			write(true)
		}
		b.mu.Unlock()
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *fakeBot) sent() []fakeBotCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []fakeBotCall
	for _, c := range b.calls {
		if c.Method == "sendMessage" {
			out = append(out, c)
		}
	}
	return out
}

func (b *fakeBot) push(u telegram.Update) {
	b.mu.Lock()
	b.updates = append(b.updates, u)
	b.mu.Unlock()
}

type telegramEnv struct {
	alerts   *TelegramAlerts
	db       *gorm.DB
	bot      *fakeBot
	settings *mailSettings
	users    *mailUserRepo
	client   *telegram.Client
}

func newTelegramEnv(t *testing.T) *telegramEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.TelegramAlertChat{}, &types.TelegramAlertMessage{}))
	settings := &mailSettings{values: map[string]any{
		types.TelegramAlertsEnabledSettingKey:  true,
		types.TelegramAlertsBotTokenSettingKey: testBotToken,
	}}
	users := &mailUserRepo{users: map[string]*types.User{
		"admin1": {ID: "admin1", Username: "an", IsActive: true, IsSystemAdmin: true},
		"admin2": {ID: "admin2", Username: "binh", IsActive: true, IsSystemAdmin: true},
		"member": {ID: "member", Username: "chi", IsActive: true},
	}}
	bot := newFakeBot(t)
	a := NewTelegramAlerts(db, settings, users, nil, nil)
	a.apiBase = bot.srv.URL
	a.baseURL = func() string { return "https://ai.example.vn" }
	a.digestQuiet = 20 * time.Millisecond
	return &telegramEnv{alerts: a, db: db, bot: bot, settings: settings, users: users,
		client: telegram.New(testBotToken, bot.srv.URL, nil)}
}

func (e *telegramEnv) link(t *testing.T, chatID int64, by string) {
	t.Helper()
	require.NoError(t, e.db.Create(&types.TelegramAlertChat{
		ID: uuid.NewString(), ChatID: chatID, ChatType: "private", LinkedBy: by, LinkedAt: time.Now(),
	}).Error)
}

func (e *telegramEnv) messages(t *testing.T) []types.TelegramAlertMessage {
	t.Helper()
	var out []types.TelegramAlertMessage
	require.NoError(t, e.db.Order("created_at, chat_id").Find(&out).Error)
	return out
}

func TestTelegramEnqueueOnlyActiveSuperAdminChats(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, 1001, "admin1")
	e.link(t, 1002, "member") // no longer a SuperAdmin
	e.link(t, 1003, "admin2")
	now := time.Now()
	require.NoError(t, e.db.Model(&types.TelegramAlertChat{}).Where("chat_id = ?", 1003).Update("disabled_at", &now).Error)

	n, err := e.alerts.Enqueue(ctx, TelegramAlert{Kind: types.TelegramAlertOrderPaid, Key: "order_paid:o1",
		Title: "Đơn WR1 đã thanh toán", Body: "Tổ chức A", Link: "/platform/admin?section=billing&status=paid"})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	msgs := e.messages(t)
	require.Len(t, msgs, 1)
	require.Equal(t, int64(1001), msgs[0].ChatID)
	require.Equal(t, "💰 Đơn WR1 đã thanh toán\nTổ chức A\n\nhttps://ai.example.vn/platform/admin?section=billing&status=paid", msgs[0].Text)

	// the same key is not queued twice
	n, err = e.alerts.Enqueue(ctx, TelegramAlert{Kind: types.TelegramAlertOrderPaid, Key: "order_paid:o1", Title: "x"})
	require.NoError(t, err)
	require.Zero(t, n)

	// muted kind, bot off: nothing
	e.settings.values[types.TelegramAlertsMutedKindsSettingKey] = "order_expired, order_paid"
	n, _ = e.alerts.Enqueue(ctx, TelegramAlert{Kind: types.TelegramAlertOrderPaid, Key: "order_paid:o2", Title: "x"})
	require.Zero(t, n)
	delete(e.settings.values, types.TelegramAlertsMutedKindsSettingKey)
	e.settings.values[types.TelegramAlertsEnabledSettingKey] = false
	n, _ = e.alerts.Enqueue(ctx, TelegramAlert{Kind: types.TelegramAlertOrderPaid, Key: "order_paid:o3", Title: "x"})
	require.Zero(t, n)
}

func TestTelegramDigestOncePerDayAndEpisode(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, 1001, "admin1")
	clock := time.Date(2026, 11, 26, 9, 0, 0, 0, types.UsageLocation())
	e.alerts.now = func() time.Time { return clock }
	n := types.UserNotification{Kind: types.UserNotificationBackupAlert, Title: "Sao lưu gặp sự cố: 1 việc", Body: "pg_dump quá hạn"}
	e.alerts.sendDigest(ctx, n)
	clock = clock.Add(2 * time.Hour)
	e.alerts.sendDigest(ctx, n) // the same incident, same day: once
	require.Len(t, e.messages(t), 1)
	// resolved, then the same text again within the hour: a new episode
	require.NoError(t, e.alerts.resolve(ctx, mustKind(t, types.UserNotificationBackupAlert)))
	clock = clock.Add(10 * time.Minute)
	e.alerts.sendDigest(ctx, n)
	msgs := e.messages(t)
	require.Len(t, msgs, 3)
	require.True(t, strings.HasPrefix(msgs[1].EventKey, "resolved:"))
	require.Equal(t, "⚠️ Sao lưu gặp sự cố: 1 việc\npg_dump quá hạn", msgs[2].Text)
	// still going on the next day: reminded once
	clock = clock.Add(24 * time.Hour)
	e.alerts.sendDigest(ctx, n)
	e.alerts.sendDigest(ctx, n)
	require.Len(t, e.messages(t), 4)
}

func TestTelegramResolutionWhileMuted(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, 1001, "admin1")
	clock := time.Date(2026, 11, 26, 9, 0, 0, 0, types.UsageLocation())
	e.alerts.now = func() time.Time { return clock }
	k := mustKind(t, types.UserNotificationBackupAlert)
	n := types.UserNotification{Kind: k.Kind, Title: "Sao lưu gặp sự cố: 1 việc"}
	e.alerts.sendDigest(ctx, n)
	// muted while the incident ends: nobody told, the episode ends
	clock = clock.Add(time.Minute)
	e.settings.values[types.TelegramAlertsMutedKindsSettingKey] = k.Kind
	require.NoError(t, e.alerts.resolve(ctx, k))
	// unmuted: no late "✅" for the old alert
	clock = clock.Add(time.Minute)
	delete(e.settings.values, types.TelegramAlertsMutedKindsSettingKey)
	require.NoError(t, e.alerts.resolve(ctx, k))
	msgs := e.messages(t)
	require.Len(t, msgs, 2)
	require.Equal(t, types.TelegramMessageMarker, msgs[1].Status)
	require.Equal(t, int64(0), msgs[1].ChatID)
	// the same incident again the same day is a new one
	clock = clock.Add(time.Minute)
	e.alerts.sendDigest(ctx, n)
	require.Len(t, e.messages(t), 3)
	// an alert older than 48 h gets no "✅"
	clock = clock.Add(49 * time.Hour)
	require.NoError(t, e.alerts.resolve(ctx, k))
	require.Len(t, e.messages(t), 3)
}

func mustKind(t *testing.T, kind string) types.TelegramAlertKind {
	t.Helper()
	k, ok := types.TelegramAlertKindOf(kind)
	require.True(t, ok)
	return k
}

func TestTelegramEnqueueConcurrentSameKey(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, 1001, "admin1")
	e.link(t, 1002, "admin2")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.alerts.Enqueue(ctx, TelegramAlert{Kind: types.TelegramAlertOrderPaid, Key: "order_paid:o1:1", Title: "x"})
		}()
	}
	wg.Wait()
	require.Len(t, e.messages(t), 2, "one per chat, whatever the races")
}

func TestTelegramSendBatch(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, 1001, "admin1")
	e.link(t, 1002, "admin2")
	e.link(t, 1003, "admin1")
	e.bot.refuse = func(chat int64) (int, string, map[string]any) {
		switch chat {
		case 1002:
			return 403, "Forbidden: bot was blocked by the user", nil
		case 1003:
			return 429, "Too Many Requests: retry after 7", map[string]any{"retry_after": 7}
		}
		return 0, "", nil
	}
	_, err := e.alerts.Enqueue(ctx, TelegramAlert{Kind: types.TelegramAlertTest, Key: "test:1", Title: "Tin thử", Silent: true})
	require.NoError(t, err)
	_, err = e.alerts.Enqueue(ctx, TelegramAlert{Kind: types.TelegramAlertTest, Key: "test:2", Title: "Tin thử 2", ChatIDs: []int64{1003}})
	require.NoError(t, err)
	sent, err := e.alerts.SendBatch(ctx, e.client)
	require.NoError(t, err)
	require.Equal(t, 1, sent)

	byChat := map[int64]types.TelegramAlertMessage{}
	var second types.TelegramAlertMessage
	for _, m := range e.messages(t) {
		if m.EventKey == "test:2" {
			second = m
			continue
		}
		byChat[m.ChatID] = m
	}
	calls1003 := 0
	for _, c := range e.bot.sent() {
		if int64(c.Body["chat_id"].(float64)) == 1003 {
			calls1003++
		}
	}
	require.Equal(t, 1, calls1003, "after a flood wait the chat's next message is not tried in the batch")
	require.Equal(t, types.TelegramMessagePending, second.Status)
	require.True(t, second.NextAttemptAt.After(time.Now().Add(5*time.Second)), "the chat's other messages wait too")
	require.Equal(t, types.TelegramMessageSent, byChat[1001].Status)
	require.NotNil(t, byChat[1001].MessageID)
	require.Equal(t, types.TelegramMessageFailed, byChat[1002].Status, "a blocked chat drops its messages")
	require.Equal(t, types.TelegramMessagePending, byChat[1003].Status, "rate limited: retried later")
	require.Zero(t, byChat[1003].Attempts, "a flood wait spends no attempt")
	require.True(t, byChat[1003].NextAttemptAt.After(time.Now().Add(5*time.Second)))

	var blocked types.TelegramAlertChat
	require.NoError(t, e.db.Where("chat_id = ?", 1002).Take(&blocked).Error)
	require.NotNil(t, blocked.DisabledAt)

	calls := e.bot.sent()
	require.Equal(t, true, calls[0].Body["disable_notification"], "silent alert")
}

func TestTelegramSendMovesMigratedGroup(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, -100, "admin1")
	e.bot.refuse = func(chat int64) (int, string, map[string]any) {
		if chat == -100 {
			return 400, "Bad Request: group chat was upgraded to a supergroup chat", map[string]any{"migrate_to_chat_id": -1001234}
		}
		return 0, "", nil
	}
	_, _ = e.alerts.Enqueue(ctx, TelegramAlert{Kind: types.TelegramAlertTest, Key: "test:m", Title: "x"})
	_, err := e.alerts.SendBatch(ctx, e.client)
	require.NoError(t, err)
	sent, err := e.alerts.SendBatch(ctx, e.client)
	require.NoError(t, err)
	require.Equal(t, 1, sent, "sent to the supergroup")
	var c types.TelegramAlertChat
	require.NoError(t, e.db.Take(&c).Error)
	require.Equal(t, int64(-1001234), c.ChatID)
	require.Nil(t, c.DisabledAt)
}

func groupChat(t *testing.T, db *gorm.DB) types.TelegramAlertChat {
	t.Helper()
	var c types.TelegramAlertChat
	require.NoError(t, db.Where("chat_id = ?", -500).Take(&c).Error)
	return c
}

func TestTelegramLinkCodeFlow(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	code, err := e.alerts.CreateLinkCode(ctx, "admin1")
	require.NoError(t, err)
	require.Len(t, code.Code, telegramLinkCodeRunes)
	require.Equal(t, "https://t.me/werag_alert_bot?start="+code.Code, code.PrivateLink)
	require.Equal(t, "https://t.me/werag_alert_bot?startgroup="+code.Code, code.GroupLink)

	msg := func(chat int64, typ, text string) telegram.Update {
		return telegram.Update{UpdateID: time.Now().UnixNano(), Message: &telegram.Message{
			Chat: telegram.Chat{ID: chat, Type: typ, Title: "Nhóm vận hành"}, From: &telegram.User{ID: 1}, Text: text}}
	}
	// addressed to another bot: ignored
	e.bot.push(msg(-500, "group", "/start@other_bot "+code.Code))
	// the group link sends /start@bot <code>
	e.bot.push(msg(-500, "group", "/start@werag_alert_bot "+strings.ToLower(code.Code)))
	// the code is single use
	e.bot.push(msg(77, "private", "/start\n"+code.Code))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)

	var chats []types.TelegramAlertChat
	require.NoError(t, e.db.Find(&chats).Error)
	require.Len(t, chats, 1)
	require.Equal(t, int64(-500), chats[0].ChatID)
	require.Equal(t, "admin1", chats[0].LinkedBy)
	require.Equal(t, "Nhóm vận hành", chats[0].Title)
	sent := e.bot.sent()
	require.Len(t, sent, 2)
	require.Contains(t, sent[0].Body["text"], "Đã liên kết nhóm này")
	require.Contains(t, sent[1].Body["text"], "không đúng hoặc đã hết hạn")

	// a code of an account that lost SuperAdmin links nothing
	code2, err := e.alerts.CreateLinkCode(ctx, "member")
	require.NoError(t, err)
	e.bot.push(msg(88, "private", "/start "+code2.Code))
	// /stop in the group: a plain member is refused, a group admin is not
	plain := msg(-500, "group", "/stop@werag_alert_bot")
	plain.Message.From = &telegram.User{ID: 2}
	e.bot.push(plain)
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Nil(t, groupChat(t, e.db).DisabledAt)
	e.bot.push(msg(-500, "group", "/stop@werag_alert_bot"))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	var n int64
	require.NoError(t, e.db.Model(&types.TelegramAlertChat{}).Where("chat_id = ?", 88).Count(&n).Error)
	require.Zero(t, n)
	chats[0] = groupChat(t, e.db)
	require.NotNil(t, chats[0].DisabledAt)

	// the bot removed from a chat disables it too; linking again revives it
	code3, _ := e.alerts.CreateLinkCode(ctx, "admin2")
	e.bot.push(msg(-500, "group", "/link@werag_alert_bot "+code3.Code))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	chats[0] = groupChat(t, e.db)
	require.Nil(t, chats[0].DisabledAt)
	require.Equal(t, "admin2", chats[0].LinkedBy)
	left := telegram.Update{UpdateID: 1, MyChatMember: &telegram.ChatMemberUpdated{Chat: telegram.Chat{ID: -500}}}
	left.MyChatMember.NewChatMember.Status = "kicked"
	e.bot.push(left)
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	chats[0] = groupChat(t, e.db)
	require.NotNil(t, chats[0].DisabledAt)
}

// fakeDigestStore is the inner notification store of the relay.
type fakeDigestStore struct {
	interfaces.UserNotificationRepository
	upserts, closes int
}

func (s *fakeDigestStore) UpsertOpenDigest(_ context.Context, n *types.UserNotification,
	merge func(prev, next *types.UserNotification),
) (bool, error) {
	s.upserts++
	// the store merges into an unread digest: the relay must not see that
	prev := &types.UserNotification{Title: "Có 9 đơn", Body: "cũ"}
	merge(prev, n)
	return true, nil
}

func (s *fakeDigestStore) CloseOpenDigests(context.Context, string, time.Time, time.Time) (int64, error) {
	s.closes++
	return 0, nil
}

func TestTelegramDigestRelay(t *testing.T) {
	e := newTelegramEnv(t)
	ctx := context.Background()
	e.link(t, 1001, "admin1")
	inner := &fakeDigestStore{}
	store := NewTelegramDigestRelay(inner, e.alerts)
	payload := func(n int) types.JSON {
		raw, _ := json.Marshal(types.UserNotificationQueuePayload{Count: n})
		return types.JSON(raw)
	}
	merge := func(prev, next *types.UserNotification) { next.Title = "merged" }
	// one event: two SuperAdmins, the actor's digest names less
	for i, c := range []int{1, 2} {
		_, err := store.UpsertOpenDigest(ctx, &types.UserNotification{
			UserID: fmt.Sprintf("admin%d", i+1), Kind: types.UserNotificationPaymentOrderPending,
			Title: fmt.Sprintf("Có %d đơn thanh toán mới chờ xác nhận", c), Body: "WR1, WR2",
			Link: PaymentOrderReviewLink, Payload: payload(c),
		}, merge)
		require.NoError(t, err)
	}
	// a kind the bot does not relay (a member's notice)
	_, _ = store.UpsertOpenDigest(ctx, &types.UserNotification{Kind: types.UserNotificationLegalRequestAdded, Title: "x"}, merge)
	require.Eventually(t, func() bool { return len(e.messages(t)) == 1 }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	msgs := e.messages(t)
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0].Text, "Có 2 đơn thanh toán mới chờ xác nhận\nWR1, WR2")
	require.Equal(t, 3, inner.upserts)

	// the queue emptied: told once, silently
	_, err := store.CloseOpenDigests(ctx, types.UserNotificationPaymentOrderPending, time.Now(), time.Now())
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(e.messages(t)) == 2 }, 2*time.Second, 10*time.Millisecond)
	_, _ = store.CloseOpenDigests(ctx, types.UserNotificationPaymentOrderPending, time.Now(), time.Now())
	time.Sleep(100 * time.Millisecond)
	msgs = e.messages(t)
	require.Len(t, msgs, 2)
	require.True(t, msgs[1].Silent)
	require.Equal(t, "✅ Không còn đơn thanh toán chờ xác nhận.", msgs[1].Text)
	// a kind never alerted: no resolution
	_, _ = store.CloseOpenDigests(ctx, types.UserNotificationLegalDraftReady, time.Now(), time.Now())
	time.Sleep(100 * time.Millisecond)
	require.Len(t, e.messages(t), 2)

	// a digest still being gathered when the queue empties is dropped
	e.alerts.digestQuiet = 200 * time.Millisecond
	_, _ = store.UpsertOpenDigest(ctx, &types.UserNotification{Kind: types.UserNotificationLegalDraftReady,
		Title: "Có 1 bản nháp OCR mới chờ duyệt", Payload: payload(1)}, merge)
	_, _ = store.CloseOpenDigests(ctx, types.UserNotificationLegalDraftReady, time.Now(), time.Now())
	time.Sleep(400 * time.Millisecond)
	require.Len(t, e.messages(t), 2)
}

func TestTelegramClientRedactsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // connection refused: Go quotes the URL in the error
	_, err := telegram.New(testBotToken, url, nil).SendMessage(context.Background(), 1, "x", telegram.SendOptions{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), testBotToken)
	require.NotContains(t, err.Error(), "AAH-test")
	require.Contains(t, err.Error(), "<token>")
}

func TestValidTelegramBotToken(t *testing.T) {
	require.True(t, types.ValidTelegramBotToken(testBotToken))
	for _, bad := range []string{"", "abc", "123:short", "12345:" + strings.Repeat("•", 35), "12a45:" + strings.Repeat("a", 35)} {
		require.False(t, types.ValidTelegramBotToken(bad), bad)
	}
	require.NoError(t, validateRegistryEntry(types.TelegramAlertsMutedKindsSettingKey, "order_paid, backup_alert"))
	require.Error(t, validateRegistryEntry(types.TelegramAlertsMutedKindsSettingKey, "order_paid,nope"))
	require.Error(t, validateRegistryEntry(types.TelegramAlertsBotTokenSettingKey, "••••••"))
}

func TestBillingOrderTTLMinutesValidation(t *testing.T) {
	for _, ok := range []any{float64(0), float64(5), float64(30), int64(10080), "30"} {
		require.NoError(t, validateRegistryEntry(types.BillingOrderTTLMinutesSettingKey, ok), ok)
	}
	for _, bad := range []any{float64(1), float64(4), float64(10081), int64(-5), "1", "999999", int32(2)} {
		require.Error(t, validateRegistryEntry(types.BillingOrderTTLMinutesSettingKey, bad), bad)
	}
}
