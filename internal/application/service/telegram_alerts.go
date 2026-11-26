package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/alerting/telegram"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// TelegramAlerts is the system alert bot (types.TelegramAlertChat): it
// queues alerts for the linked chats (an outbox in Postgres), sends them in
// the background with retries, and long-polls the bot for the commands
// that link and unlink chats (/start <code>, /stop).
//
// Only SuperAdmins configure it (system settings telegram_alerts.*) and
// only chats linked by an active SuperAdmin receive alerts: a group linked
// that way is how several people handle the alerts together. Alerts carry
// a summary and a link to the page that handles them, never secrets.
//
// Sending never blocks or fails the caller: Send queues in the background
// and logs failures; Telegram being down only delays the messages (a
// message still unsent after a day is given up).
type TelegramAlerts struct {
	db       *gorm.DB
	settings interfaces.SystemSettingService
	users    interfaces.UserRepository
	rdb      *redis.Client
	baseURL  func() string
	// apiBase and httpClient: the Bot API endpoint (tests point it at a
	// fake server).
	apiBase    string
	httpClient *http.Client
	now        func() time.Time
	// digestQuiet: the digest writes of one event (one per SuperAdmin) are
	// gathered this long and relayed as one message.
	digestQuiet time.Duration

	mu sync.Mutex
	// codes: the link codes when Redis is not available (hash → code)
	codes map[string]telegramLinkEntry
	// offsets: the update offsets when Redis is not available
	offsets map[string]int64
	// bot: the getMe result of the current token
	bot telegramBotInfo
	// pollErr: why the last poll failed ("" = fine)
	pollErr string
	// digests: the digest writes being gathered, per kind
	digests map[string]*telegramDigestBatch

	wake    chan struct{}
	stopped bool
	stop    chan struct{}
	wg      sync.WaitGroup
}

type telegramLinkEntry struct {
	userID  string
	expires time.Time
}

type telegramBotInfo struct {
	token   string
	user    *telegram.User
	err     error
	checked time.Time
}

type telegramDigestBatch struct {
	ctx   context.Context
	best  types.UserNotification
	count int
	timer *time.Timer
}

// TelegramAlert is one alert to queue.
type TelegramAlert struct {
	Kind string
	// Key names the alert: queued once per chat (a unique index), so a
	// key that may come back names its episode (a deadline, a day, …).
	Key string
	// Title and Body make the text; Link is an app path ("/platform/…")
	// made absolute with the frontend base URL.
	Title, Body, Link string
	Silent            bool
	// ChatIDs restricts the alert to these Telegram chats (test messages);
	// nil = every active chat.
	ChatIDs []int64
}

const (
	telegramLinkCodeTTL   = 10 * time.Minute
	telegramLinkCodeRunes = 10
	// telegramLinkAlphabet leaves out look-alikes (0/O, 1/I/L).
	telegramLinkAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	telegramLinkRedisKey = "telegram_alerts:link:"

	telegramSendInterval = 2 * time.Second
	telegramSendBatch    = 20
	// spacing between two messages to one chat: Telegram allows about one
	// a second to a person and twenty a minute to a group
	telegramChatSpacing  = 1100 * time.Millisecond
	telegramGroupSpacing = 3100 * time.Millisecond
	// telegramClaimLease: a message being sent is not picked by another
	// instance for this long (a crash mid-send retries it after it)
	telegramClaimLease = 5 * time.Minute
	// telegramBatchBudget bounds one batch, well inside the claim lease
	telegramBatchBudget   = 90 * time.Second
	telegramMaxAttempts   = 8
	telegramMessageMaxAge = 24 * time.Hour
	telegramPurgeAfter    = 30 * 24 * time.Hour
	// telegramResolveWithin: an alert older than this gets no "✅" (it is
	// stale; the chat has long moved on)
	telegramResolveWithin = 48 * time.Hour
	telegramPollTimeout   = 25 // seconds
	telegramBotCheckTTL   = time.Minute
	telegramSenderLock    = "telegram_alerts:sender"
	telegramPollerLock    = "telegram_alerts:poller"
	// telegramPollerLease covers a long poll (25 s, HTTP timeout 45 s) and
	// the replies to its updates
	telegramPollerLease = 2 * time.Minute
	telegramDigestQuiet = 3 * time.Second
	telegramSendTimeout = 30 * time.Second
)

// NewTelegramAlerts creates the bot service; Start runs its loops.
func NewTelegramAlerts(db *gorm.DB, settings interfaces.SystemSettingService, users interfaces.UserRepository,
	rdb *redis.Client, cfg *config.Config,
) *TelegramAlerts {
	return &TelegramAlerts{
		db: db, settings: settings, users: users, rdb: rdb,
		baseURL: func() string {
			base := ""
			if cfg != nil {
				base = strings.TrimSpace(cfg.FrontendBaseURL)
			}
			if base == "" {
				base = strings.TrimSpace(os.Getenv("FRONTEND_BASE_URL"))
			}
			return strings.TrimRight(base, "/")
		},
		httpClient:  &http.Client{Timeout: time.Duration(telegramPollTimeout+20) * time.Second},
		now:         time.Now,
		digestQuiet: telegramDigestQuiet,
		codes:       map[string]telegramLinkEntry{},
		offsets:     map[string]int64{},
		digests:     map[string]*telegramDigestBatch{},
		wake:        make(chan struct{}, 1),
		stop:        make(chan struct{}),
	}
}

func (a *TelegramAlerts) token(ctx context.Context) string {
	return strings.TrimSpace(a.settings.GetString(ctx, types.TelegramAlertsBotTokenSettingKey, "", ""))
}

// active reports whether alerts go out: the switch on and a token saved.
func (a *TelegramAlerts) active(ctx context.Context) bool {
	if a == nil || a.settings == nil {
		return false
	}
	return a.settings.GetBool(ctx, types.TelegramAlertsEnabledSettingKey, "", false) && a.token(ctx) != ""
}

func (a *TelegramAlerts) client(token string) *telegram.Client {
	return telegram.New(token, a.apiBase, a.httpClient)
}

func (a *TelegramAlerts) muted(ctx context.Context, kind string) bool {
	if kind == types.TelegramAlertTest || kind == types.TelegramAlertLinked {
		return false
	}
	return types.TelegramMutedKinds(a.settings.GetString(ctx, types.TelegramAlertsMutedKindsSettingKey, "", ""))[kind]
}

// ── queueing ──

// Send queues alert in the background (the caller never waits on it).
func (a *TelegramAlerts) Send(ctx context.Context, alert TelegramAlert) {
	a.Run(ctx, "queueing "+alert.Kind, func(ctx context.Context) error {
		_, err := a.Enqueue(ctx, alert)
		return err
	})
}

// Run runs fn in the background with the caller's values but not its
// cancellation, under telegramSendTimeout; failures are only logged.
func (a *TelegramAlerts) Run(ctx context.Context, what string, fn func(context.Context) error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.wg.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.wg.Done()
		run, cancel := context.WithTimeout(context.WithoutCancel(ctx), telegramSendTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf(run, "[TelegramAlerts] %s panicked: %v", what, r)
			}
		}()
		if err := fn(run); err != nil {
			logger.Warnf(run, "[TelegramAlerts] %s: %v", what, err)
		}
	}()
}

// Enqueue queues alert for every active chat (or alert.ChatIDs) that does
// not have it yet; returns how many messages were queued. Nothing is
// queued while the bot is off or the kind muted.
func (a *TelegramAlerts) Enqueue(ctx context.Context, alert TelegramAlert) (int, error) {
	if !a.active(ctx) || a.muted(ctx, alert.Kind) {
		return 0, nil
	}
	chats, err := a.activeChats(ctx)
	if err != nil {
		return 0, err
	}
	if alert.ChatIDs != nil {
		want := map[int64]bool{}
		for _, id := range alert.ChatIDs {
			want[id] = true
		}
		kept := chats[:0]
		for _, c := range chats {
			if want[c.ChatID] {
				kept = append(kept, c)
			}
		}
		chats = kept
	}
	if len(chats) == 0 {
		return 0, nil
	}
	text := a.render(alert)
	now := a.now()
	queued := 0
	for _, c := range chats {
		msg := &types.TelegramAlertMessage{
			ID: uuid.NewString(), ChatID: c.ChatID, Kind: alert.Kind, EventKey: alert.Key, Text: text,
			Silent: alert.Silent, Status: types.TelegramMessagePending, NextAttemptAt: now, CreatedAt: now,
		}
		// the same alert from two callers or instances: one row per chat
		res := a.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "chat_id"}, {Name: "event_key"}}, DoNothing: true,
		}).Create(msg)
		if res.Error != nil {
			return queued, res.Error
		}
		queued += int(res.RowsAffected)
	}
	if queued > 0 {
		a.kick()
	}
	return queued, nil
}

// render makes the plain text of alert.
func (a *TelegramAlerts) render(alert TelegramAlert) string {
	var b strings.Builder
	b.WriteString(telegramAlertIcon(alert))
	b.WriteString(" ")
	b.WriteString(strings.TrimSpace(alert.Title))
	if body := strings.TrimSpace(alert.Body); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
	}
	if alert.Link != "" {
		if base := a.baseURL(); base != "" {
			b.WriteString("\n\n")
			b.WriteString(base + alert.Link)
		}
	}
	return telegram.CutText(b.String())
}

func telegramAlertIcon(alert TelegramAlert) string {
	if strings.HasPrefix(alert.Key, "resolved:") {
		return "✅"
	}
	switch alert.Kind {
	case types.TelegramAlertTest, types.TelegramAlertLinked:
		return "🔔"
	case types.TelegramAlertOrderPaid:
		return "💰"
	case types.TelegramAlertOrderPaymentHeld:
		return "⚠️"
	case types.TelegramAlertOrderExpiring, types.TelegramAlertOrderExpired, types.TelegramAlertOrderUnderpaid:
		return "⏰"
	}
	if k, ok := types.TelegramAlertKindOf(alert.Kind); ok {
		switch k.Group {
		case "incident":
			return "⚠️"
		case "billing":
			return "💳"
		}
	}
	return "📝"
}

// activeChats are the chats that get alerts: not disabled, linked by an
// account that is still an active human SuperAdmin.
func (a *TelegramAlerts) activeChats(ctx context.Context) ([]types.TelegramAlertChat, error) {
	var chats []types.TelegramAlertChat
	if err := a.db.WithContext(ctx).Where("disabled_at IS NULL").Order("linked_at").Find(&chats).Error; err != nil {
		return nil, err
	}
	if len(chats) == 0 {
		return nil, nil
	}
	admins, err := a.superAdmins(ctx)
	if err != nil {
		return nil, err
	}
	out := chats[:0]
	for _, c := range chats {
		if admins[c.LinkedBy] {
			out = append(out, c)
		}
	}
	return out, nil
}

// superAdmins are the ids of the active human SuperAdmins.
func (a *TelegramAlerts) superAdmins(ctx context.Context) (map[string]bool, error) {
	list, _, err := a.users.ListSystemAdmins(ctx, 0, reviewNoticeMaxRecipients)
	if err != nil {
		return nil, fmt.Errorf("listing SuperAdmins: %w", err)
	}
	out := map[string]bool{}
	for _, u := range list {
		if u != nil && u.IsActive && u.IsSystemAdmin && u.ID != "" && !types.IsSyntheticUserID(u.ID) {
			out[u.ID] = true
		}
	}
	return out, nil
}

// ── digests relayed from the in-app notifications ──

// digest relays a SuperAdmin digest written in-app (n is the fresh notice
// of one recipient, before merging into an unread one). One event writes
// one digest per SuperAdmin, which may differ (the actor's leaves out what
// they did): the writes of a kind are gathered digestQuiet and the one
// naming the most items is sent.
func (a *TelegramAlerts) digest(ctx context.Context, n types.UserNotification) {
	if a == nil {
		return
	}
	if k, ok := types.TelegramAlertKindOf(n.Kind); !ok || k.Group == "" {
		return
	}
	count := telegramPayloadCount(n.Payload)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	if b, ok := a.digests[n.Kind]; ok {
		if count >= b.count {
			b.best, b.count = n, count
		}
		return
	}
	b := &telegramDigestBatch{ctx: context.WithoutCancel(ctx), best: n, count: count}
	a.digests[n.Kind] = b
	a.wg.Add(1)
	kind := n.Kind
	b.timer = time.AfterFunc(a.digestQuiet, func() {
		defer a.wg.Done()
		a.mu.Lock()
		cur := a.digests[kind]
		delete(a.digests, kind)
		a.mu.Unlock()
		if cur == nil {
			return
		}
		a.sendDigest(cur.ctx, cur.best)
	})
}

func (a *TelegramAlerts) sendDigest(ctx context.Context, n types.UserNotification) {
	run, cancel := context.WithTimeout(ctx, telegramSendTimeout)
	defer cancel()
	episode, err := a.lastResolution(run, n.Kind)
	if err != nil {
		logger.Warnf(run, "[TelegramAlerts] relaying the %s digest: %v", n.Kind, err)
		return
	}
	// the same text is told once per day and episode (an incident still
	// going on is reminded the next day, a new one after a resolution at once)
	sum := sha256.Sum256([]byte(n.Title + "\n" + n.Body + "\n" + episode))
	day := a.now().In(types.UsageLocation()).Format("20060102")
	if _, err := a.Enqueue(run, TelegramAlert{
		Kind: n.Kind, Key: "digest:" + n.Kind + ":" + day + ":" + hex.EncodeToString(sum[:8]),
		Title: n.Title, Body: n.Body, Link: n.Link,
	}); err != nil {
		logger.Warnf(run, "[TelegramAlerts] relaying the %s digest: %v", n.Kind, err)
	}
}

// latestTelegramMessage is the newest message of q, nil when none (Find,
// not Take: an empty result is normal here and must not log as an error).
func latestTelegramMessage(q *gorm.DB) (*types.TelegramAlertMessage, error) {
	var rows []types.TelegramAlertMessage
	if err := q.Order("created_at DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// markResolved ends the episode of kind without telling anyone (the bot is
// off or the kind muted): a chat-less marker row (chat 0, never sent).
func (a *TelegramAlerts) markResolved(ctx context.Context, kind string) error {
	last, err := latestTelegramMessage(a.db.WithContext(ctx).Where("kind = ?", kind))
	if err != nil || last == nil || strings.HasPrefix(last.EventKey, "resolved:") {
		return err
	}
	now := a.now()
	return a.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "chat_id"}, {Name: "event_key"}}, DoNothing: true,
	}).Create(&types.TelegramAlertMessage{
		ID: uuid.NewString(), ChatID: 0, Kind: kind, EventKey: "resolved:" + kind + ":" + last.ID,
		Status: types.TelegramMessageMarker, NextAttemptAt: now, CreatedAt: now,
	}).Error
}

// lastResolution names the episode of kind: the key of its latest
// resolution ("" before the first).
func (a *TelegramAlerts) lastResolution(ctx context.Context, kind string) (string, error) {
	var keys []string
	err := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).
		Where("kind = ? AND event_key LIKE ?", kind, "resolved:%").
		Order("created_at DESC").Limit(1).Pluck("event_key", &keys).Error
	if err != nil || len(keys) == 0 {
		return "", err
	}
	return keys[0], nil
}

func telegramPayloadCount(p types.JSON) int {
	var v struct {
		Count int `json:"count"`
	}
	if len(p) > 0 {
		_ = json.Unmarshal(p, &v)
	}
	return v.Count
}

// resolved tells (silently) the chats that were told about kind that its
// queue or incident is over, once per alert: a chat whose latest alert of
// the kind (queued or sent) is not a resolution already. A digest still
// being gathered is dropped: what it reports is gone. Runs in the
// background (it is called from the close of the in-app digests).
func (a *TelegramAlerts) resolved(ctx context.Context, kind string) {
	if a == nil {
		return
	}
	k, ok := types.TelegramAlertKindOf(kind)
	if !ok || !k.Resolvable {
		return
	}
	a.mu.Lock()
	if b, ok := a.digests[kind]; ok && b.timer.Stop() {
		delete(a.digests, kind)
		a.wg.Done()
	}
	a.mu.Unlock()
	a.Run(ctx, "resolving "+kind, func(ctx context.Context) error { return a.resolve(ctx, k) })
}

func (a *TelegramAlerts) resolve(ctx context.Context, k types.TelegramAlertKind) error {
	if !a.active(ctx) || a.muted(ctx, k.Kind) {
		// nobody is told, but the episode ends: an identical alert later
		// is a new one, and unmuting does not resolve this one late
		return a.markResolved(ctx, k.Kind)
	}
	chats, err := a.activeChats(ctx)
	if err != nil {
		return err
	}
	title := "Không còn " + k.Label + "."
	if k.Group == "incident" {
		title = "Đã hết " + k.Label + "."
	}
	// an alert older than the latest resolution (told, or a marker written
	// while muted) is over already; read once, before this run adds any
	var ended time.Time
	prev, err := latestTelegramMessage(a.db.WithContext(ctx).Where("kind = ? AND event_key LIKE ?", k.Kind, "resolved:%"))
	if err != nil {
		return err
	}
	if prev != nil {
		ended = prev.CreatedAt
	}
	for _, c := range chats {
		last, err := latestTelegramMessage(a.db.WithContext(ctx).
			Where("kind = ? AND chat_id = ? AND status IN ? AND created_at > ?", k.Kind, c.ChatID,
				[]string{types.TelegramMessagePending, types.TelegramMessageSent},
				a.now().Add(-telegramResolveWithin)))
		if err != nil {
			return err
		}
		if last == nil {
			continue
		}
		if strings.HasPrefix(last.EventKey, "resolved:") || !last.CreatedAt.After(ended) {
			continue
		}
		if _, err := a.Enqueue(ctx, TelegramAlert{
			Kind: k.Kind, Key: "resolved:" + k.Kind + ":" + last.ID, Title: title, Silent: true,
			ChatIDs: []int64{c.ChatID},
		}); err != nil {
			return err
		}
	}
	return nil
}

// NewTelegramDigestRelay decorates the in-app notification store: every
// SuperAdmin digest it writes (review queues, incidents) is relayed to the
// Telegram bot, and a queue or incident closed is told there. The store
// works as before when the bot is off.
func NewTelegramDigestRelay(inner interfaces.UserNotificationRepository, alerts *TelegramAlerts) interfaces.UserNotificationRepository {
	if alerts == nil {
		return inner
	}
	return &telegramDigestRelay{UserNotificationRepository: inner, alerts: alerts}
}

type telegramDigestRelay struct {
	interfaces.UserNotificationRepository
	alerts *TelegramAlerts
}

func (r *telegramDigestRelay) UpsertOpenDigest(ctx context.Context, n *types.UserNotification,
	merge func(prev, next *types.UserNotification),
) (bool, error) {
	var fresh types.UserNotification
	if n != nil {
		fresh = *n
	}
	inserted, err := r.UserNotificationRepository.UpsertOpenDigest(ctx, n, merge)
	if err == nil && n != nil {
		r.alerts.digest(ctx, fresh)
	}
	return inserted, err
}

func (r *telegramDigestRelay) CloseOpenDigests(ctx context.Context, kind string, before, at time.Time) (int64, error) {
	n, err := r.UserNotificationRepository.CloseOpenDigests(ctx, kind, before, at)
	if err == nil {
		r.alerts.resolved(ctx, kind)
	}
	return n, err
}

// ── sending ──

func (a *TelegramAlerts) kick() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Start runs the sender, the command poller and the purge; the returned
// func stops them.
func (a *TelegramAlerts) Start() func() {
	ctx, cancel := context.WithCancel(context.Background())
	var loops sync.WaitGroup
	loops.Add(2)
	go func() { defer loops.Done(); a.senderLoop(ctx) }()
	go func() { defer loops.Done(); a.pollerLoop(ctx) }()
	return func() {
		a.mu.Lock()
		a.stopped = true
		for kind, b := range a.digests { // send what is being gathered now
			if b.timer.Stop() {
				delete(a.digests, kind)
				go func(b *telegramDigestBatch) { defer a.wg.Done(); a.sendDigest(b.ctx, b.best) }(b)
			}
		}
		a.mu.Unlock()
		done := make(chan struct{})
		go func() { a.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		cancel()
		loops.Wait()
	}
}

func (a *TelegramAlerts) senderLoop(ctx context.Context) {
	tick := time.NewTicker(telegramSendInterval)
	defer tick.Stop()
	purge := time.NewTicker(time.Hour)
	defer purge.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-a.wake:
		case <-purge.C:
			a.purge(ctx)
			continue
		}
		a.sendDue(ctx)
	}
}

// sendDue sends the messages that are due (one instance at a time).
func (a *TelegramAlerts) sendDue(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[TelegramAlerts] sender panicked: %v", r)
		}
	}()
	var due int64
	if err := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).
		Where("status = ? AND next_attempt_at <= ?", types.TelegramMessagePending, a.now()).
		Count(&due).Error; err != nil || due == 0 {
		return
	}
	release, ok := monitorLock(ctx, a.rdb, telegramSenderLock, time.Minute, "TelegramAlerts")
	if !ok {
		return
	}
	defer release()
	token := a.token(ctx)
	if token == "" || !a.settings.GetBool(ctx, types.TelegramAlertsEnabledSettingKey, "", false) {
		return // queued messages wait for the bot to be on again (or go stale)
	}
	if _, err := a.SendBatch(ctx, a.client(token)); err != nil && ctx.Err() == nil {
		logger.Warnf(ctx, "[TelegramAlerts] sending: %v", err)
	}
}

// SendBatch sends up to telegramSendBatch due messages with c; returns how
// many were sent.
func (a *TelegramAlerts) SendBatch(ctx context.Context, c *telegram.Client) (int, error) {
	now := a.now()
	// alerts that waited a day are no longer worth a ping
	if err := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).
		Where("status = ? AND created_at < ?", types.TelegramMessagePending, now.Add(-telegramMessageMaxAge)).
		Updates(map[string]any{"status": types.TelegramMessageFailed, "last_error": "quá một ngày chưa gửi được"}).Error; err != nil {
		return 0, err
	}
	var msgs []types.TelegramAlertMessage
	if err := a.db.WithContext(ctx).
		Where("status = ? AND next_attempt_at <= ?", types.TelegramMessagePending, now).
		Order("created_at").Limit(telegramSendBatch).Find(&msgs).Error; err != nil {
		return 0, err
	}
	if len(msgs) == 0 {
		return 0, nil
	}
	chats, err := a.activeChats(ctx)
	if err != nil {
		return 0, err
	}
	live := map[int64]bool{}
	for _, ch := range chats {
		live[ch.ChatID] = true
	}
	deadline := time.Now().Add(telegramBatchBudget)
	lastTo := map[int64]time.Time{}
	waiting := map[int64]bool{} // rate limited: the chat's other messages wait too
	sent := 0
	for i := range msgs {
		m := &msgs[i]
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		if time.Now().After(deadline) {
			break // the rest goes in the next batch
		}
		if waiting[m.ChatID] {
			continue
		}
		if !live[m.ChatID] {
			a.finish(ctx, m, types.TelegramMessageFailed, "chat không còn nhận cảnh báo", nil)
			continue
		}
		spacing := telegramChatSpacing
		if m.ChatID < 0 { // groups and supergroups have negative ids
			spacing = telegramGroupSpacing
		}
		if t, ok := lastTo[m.ChatID]; ok {
			if wait := spacing - time.Since(t); wait > 0 {
				select {
				case <-ctx.Done():
					return sent, ctx.Err()
				case <-time.After(wait):
				}
			}
		}
		// claim it: another instance (the sender lock lapsed) leaves it alone
		claim := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).
			Where("id = ? AND status = ? AND next_attempt_at <= ?", m.ID, types.TelegramMessagePending, a.now()).
			Update("next_attempt_at", a.now().Add(telegramClaimLease))
		if claim.Error != nil {
			return sent, claim.Error
		}
		if claim.RowsAffected == 0 {
			continue
		}
		id, err := c.SendMessage(ctx, m.ChatID, m.Text, telegram.SendOptions{Silent: m.Silent})
		lastTo[m.ChatID] = time.Now()
		if err == nil {
			a.finish(ctx, m, types.TelegramMessageSent, "", &id)
			sent++
			continue
		}
		if api := telegram.AsAPIError(err); api != nil && (api.RetryAfter > 0 || api.MigrateToChatID != 0 || api.ChatGone()) {
			// flood wait, moved or gone: the chat's other messages of this
			// batch are not tried (the next batch reads them again)
			waiting[m.ChatID] = true
		}
		a.failed(ctx, m, err)
	}
	return sent, nil
}

func (a *TelegramAlerts) finish(ctx context.Context, m *types.TelegramAlertMessage, status, reason string, msgID *int64) {
	up := map[string]any{"status": status, "last_error": reason, "attempts": m.Attempts + 1}
	if status == types.TelegramMessageSent {
		up["sent_at"] = a.now()
		up["message_id"] = *msgID
		up["last_error"] = ""
	}
	// a lost write of a sent message would send it again after the claim:
	// try a few times
	var err error
	for i := 0; i < 3; i++ {
		if err = a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).Where("id = ?", m.ID).Updates(up).Error; err == nil {
			return
		}
		sleepCtx(ctx, time.Duration(i+1)*200*time.Millisecond)
	}
	logger.Warnf(ctx, "[TelegramAlerts] saving message %s: %v", m.ID, err)
}

// failed handles a refused send: a chat that is gone is disabled (its
// messages dropped), a group that became a supergroup moves, a rate limit
// or other error retries later with backoff.
func (a *TelegramAlerts) failed(ctx context.Context, m *types.TelegramAlertMessage, err error) {
	api := telegram.AsAPIError(err)
	reason := err.Error()
	switch {
	case api != nil && api.MigrateToChatID != 0:
		a.moveChat(ctx, m.ChatID, api.MigrateToChatID, m.ID)
		return
	case api != nil && api.ChatGone():
		a.disableChat(ctx, m.ChatID, "Telegram từ chối chat: "+api.Description)
		return
	case api != nil && api.RetryAfter > 0:
		// flood wait: every message of the chat waits it out, no attempt spent
		if err := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).
			Where("chat_id = ? AND status = ?", m.ChatID, types.TelegramMessagePending).
			Updates(map[string]any{"last_error": reason, "next_attempt_at": a.now().Add(api.RetryAfter + time.Second)}).Error; err != nil {
			logger.Warnf(ctx, "[TelegramAlerts] delaying chat messages: %v", err)
		}
		return
	}
	attempts := m.Attempts + 1
	if attempts >= telegramMaxAttempts {
		a.finish(ctx, m, types.TelegramMessageFailed, reason, nil)
		return
	}
	delay := time.Duration(1<<attempts) * 5 * time.Second
	if delay > 30*time.Minute {
		delay = 30 * time.Minute
	}
	if err := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).Where("id = ?", m.ID).Updates(map[string]any{
		"attempts": attempts, "last_error": reason, "next_attempt_at": a.now().Add(delay),
	}).Error; err != nil {
		logger.Warnf(ctx, "[TelegramAlerts] saving message %s: %v", m.ID, err)
	}
}

// moveChat moves a group that became a supergroup (and its messages);
// msgID, the message that learned it, goes out at once.
func (a *TelegramAlerts) moveChat(ctx context.Context, from, to int64, msgID string) {
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// the supergroup may be linked already (linked again after the move)
		var n int64
		if err := tx.Model(&types.TelegramAlertChat{}).Where("chat_id = ?", to).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			if err := tx.Where("chat_id = ?", from).Delete(&types.TelegramAlertChat{}).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&types.TelegramAlertChat{}).Where("chat_id = ?", from).
			Updates(map[string]any{"chat_id": to, "chat_type": "supergroup"}).Error; err != nil {
			return err
		}
		// the supergroup already holds some alerts: drop those copies first
		if err := tx.Exec(`DELETE FROM telegram_alert_messages WHERE chat_id = ? AND EXISTS (
			SELECT 1 FROM telegram_alert_messages x WHERE x.chat_id = ? AND x.event_key = telegram_alert_messages.event_key)`,
			from, to).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.TelegramAlertMessage{}).Where("chat_id = ?", from).Update("chat_id", to).Error; err != nil {
			return err
		}
		// the claimed message goes out to the supergroup at once
		return tx.Model(&types.TelegramAlertMessage{}).Where("id = ? AND status = ?", msgID, types.TelegramMessagePending).
			Update("next_attempt_at", a.now()).Error
	})
	if err != nil {
		logger.Warnf(ctx, "[TelegramAlerts] moving chat to its supergroup: %v", err)
	}
}

func (a *TelegramAlerts) disableChat(ctx context.Context, chatID int64, reason string) {
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&types.TelegramAlertChat{}).Where("chat_id = ? AND disabled_at IS NULL", chatID).
			Updates(map[string]any{"disabled_at": a.now(), "last_error": reason}).Error; err != nil {
			return err
		}
		return tx.Model(&types.TelegramAlertMessage{}).
			Where("chat_id = ? AND status = ?", chatID, types.TelegramMessagePending).
			Updates(map[string]any{"status": types.TelegramMessageFailed, "last_error": reason}).Error
	})
	if err != nil {
		logger.Warnf(ctx, "[TelegramAlerts] disabling a chat: %v", err)
	}
}

func (a *TelegramAlerts) purge(ctx context.Context) {
	if err := a.db.WithContext(ctx).
		Where("status <> ? AND created_at < ?", types.TelegramMessagePending, a.now().Add(-telegramPurgeAfter)).
		Delete(&types.TelegramAlertMessage{}).Error; err != nil {
		logger.Warnf(ctx, "[TelegramAlerts] purging old messages: %v", err)
	}
}

// ── commands: linking chats ──

func (a *TelegramAlerts) pollerLoop(ctx context.Context) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		token := a.token(ctx)
		if token == "" || !a.settings.GetBool(ctx, types.TelegramAlertsEnabledSettingKey, "", false) {
			a.setPollErr("")
			sleepCtx(ctx, 10*time.Second)
			continue
		}
		release, ok := monitorLock(ctx, a.rdb, telegramPollerLock, telegramPollerLease, "TelegramAlerts")
		if !ok { // another instance polls
			sleepCtx(ctx, 15*time.Second)
			continue
		}
		// the offset is shared (Redis): the instance that polls next does
		// not get the updates this one handled again
		offset := a.loadOffset(ctx, token)
		next, err := a.PollOnce(ctx, a.client(token), offset)
		if err == nil && next != offset {
			a.saveOffset(ctx, token, next)
		}
		release()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.setPollErr(telegramPollError(err))
			sleepCtx(ctx, backoff)
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = 5 * time.Second
		a.setPollErr("")
	}
}

func telegramOffsetKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "telegram_alerts:offset:" + hex.EncodeToString(sum[:8])
}

func (a *TelegramAlerts) loadOffset(ctx context.Context, token string) int64 {
	key := telegramOffsetKey(token)
	if a.rdb != nil {
		v, err := a.rdb.Get(ctx, key).Int64()
		if err == nil {
			return v
		}
		if !errors.Is(err, redis.Nil) {
			logger.Warnf(ctx, "[TelegramAlerts] reading the update offset: %v", err)
		}
		// expired or never saved: Telegram resends only what is unconfirmed
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.offsets[key]
}

func (a *TelegramAlerts) saveOffset(ctx context.Context, token string, offset int64) {
	key := telegramOffsetKey(token)
	if a.rdb == nil {
		a.mu.Lock()
		a.offsets[key] = offset
		a.mu.Unlock()
		return
	}
	{
		if err := a.rdb.Set(ctx, key, offset, 7*24*time.Hour).Err(); err != nil {
			logger.Warnf(ctx, "[TelegramAlerts] saving the update offset: %v", err)
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (a *TelegramAlerts) setPollErr(s string) {
	a.mu.Lock()
	a.pollErr = s
	a.mu.Unlock()
}

// telegramPollError says in Vietnamese why the bot cannot read its
// commands.
func telegramPollError(err error) string {
	api := telegram.AsAPIError(err)
	switch {
	case api == nil:
		return "Không kết nối được tới Telegram: " + err.Error()
	case api.Code == http.StatusUnauthorized || api.Code == http.StatusNotFound:
		return "Telegram từ chối token của bot (token sai hoặc đã bị thu hồi)."
	case api.Code == http.StatusConflict && strings.Contains(strings.ToLower(api.Description), "webhook"):
		return "Bot này đang gắn webhook (có thể đang được dùng ở nơi khác, ví dụ làm bot hỏi đáp). Hãy tạo một bot riêng cho cảnh báo."
	case api.Code == http.StatusConflict:
		return "Một chương trình khác đang đọc tin nhắn của bot này; hãy dùng một bot riêng cho cảnh báo."
	}
	return api.Error()
}

// PollOnce reads one batch of updates after offset and handles them;
// returns the next offset. A bot with a webhook (used by another program)
// is refused with a 409, reported on the page: the alert bot must be a bot
// of its own, so its webhook is never removed here.
func (a *TelegramAlerts) PollOnce(ctx context.Context, c *telegram.Client, offset int64) (int64, error) {
	ups, err := c.GetUpdates(ctx, offset, telegramPollTimeout)
	if err != nil {
		return offset, err
	}
	for _, u := range ups {
		if u.UpdateID >= offset {
			offset = u.UpdateID + 1
		}
		a.handleUpdate(ctx, c, u)
	}
	return offset, nil
}

func (a *TelegramAlerts) handleUpdate(ctx context.Context, c *telegram.Client, u telegram.Update) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf(ctx, "[TelegramAlerts] handling update %d panicked: %v", u.UpdateID, r)
		}
	}()
	if m := u.MyChatMember; m != nil {
		if s := m.NewChatMember.Status; s == "left" || s == "kicked" {
			a.disableChat(ctx, m.Chat.ID, "Bot đã bị chặn hoặc bị xóa khỏi chat")
		}
		return
	}
	m := u.Message
	if m == nil || m.Chat.Type == "channel" {
		return
	}
	cmd, arg, ok := a.parseCommand(ctx, c, m.Text)
	if !ok {
		return
	}
	reply := func(text string) {
		if _, err := c.SendMessage(ctx, m.Chat.ID, text, telegram.SendOptions{}); err != nil {
			logger.Warnf(ctx, "[TelegramAlerts] replying to /%s: %v", cmd, err)
		}
	}
	switch cmd {
	case "start", "link":
		if arg == "" {
			if a.chatLinked(ctx, m.Chat.ID) {
				reply("Chat này đang nhận cảnh báo của WeRAG. Gõ /stop để ngừng nhận.")
				return
			}
			reply("Bot này chỉ dành cho quản trị hệ thống WeRAG. Để nhận cảnh báo, SuperAdmin mở Quản trị hệ thống → Telegram, bấm \"Tạo mã liên kết\" rồi gửi /start <mã> tại đây.")
			return
		}
		userID, ok := a.consumeLinkCode(ctx, arg)
		if !ok {
			reply("Mã liên kết không đúng hoặc đã hết hạn (mã dùng một lần trong 10 phút). Hãy tạo mã mới trong trang Quản trị hệ thống → Telegram.")
			return
		}
		admins, err := a.superAdmins(ctx)
		if err != nil || !admins[userID] {
			reply("Tài khoản tạo mã không còn là SuperAdmin; chat này chưa được liên kết.")
			return
		}
		if err := a.linkChat(ctx, m.Chat, userID); err != nil {
			logger.Warnf(ctx, "[TelegramAlerts] linking a chat: %v", err)
			reply("Không liên kết được chat này, hãy thử lại sau.")
			return
		}
		where := "chat này"
		if m.Chat.Type == "group" || m.Chat.Type == "supergroup" {
			where = "nhóm này (mọi thành viên trong nhóm đều đọc được cảnh báo)"
		}
		reply("Đã liên kết " + where + " với WeRAG. Từ giờ bot gửi cảnh báo cho SuperAdmin tại đây: đơn thanh toán, hàng chờ duyệt, sự cố hệ thống. Gõ /stop để ngừng nhận.")
	case "stop":
		if !a.chatLinked(ctx, m.Chat.ID) {
			reply("Chat này không nhận cảnh báo của WeRAG.")
			return
		}
		if m.Chat.Type == "group" || m.Chat.Type == "supergroup" {
			if m.From == nil || !c.IsChatAdmin(ctx, m.Chat.ID, m.From.ID) {
				reply("Chỉ quản trị viên của nhóm được tắt cảnh báo. SuperAdmin cũng có thể gỡ nhóm trong trang Quản trị hệ thống → Telegram.")
				return
			}
		}
		a.disableChat(ctx, m.Chat.ID, "Đã tắt bằng lệnh /stop trên Telegram")
		reply("Đã ngừng gửi cảnh báo tới chat này. SuperAdmin có thể liên kết lại bằng mã mới.")
	case "status":
		if a.chatLinked(ctx, m.Chat.ID) {
			reply("Chat này đang nhận cảnh báo của WeRAG.")
		} else {
			reply("Chat này không nhận cảnh báo của WeRAG.")
		}
	}
}

// parseCommand reads "/cmd[@bot] [arg]"; a command addressed to another
// bot is not ours.
func (a *TelegramAlerts) parseCommand(ctx context.Context, c *telegram.Client, text string) (cmd, arg string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	fields := strings.Fields(text[1:])
	if len(fields) == 0 {
		return "", "", false
	}
	head, rest := fields[0], strings.Join(fields[1:], " ")
	cmd, at, addressed := strings.Cut(head, "@")
	if addressed {
		me := a.botUser(ctx, c)
		if me == nil || !strings.EqualFold(at, me.Username) {
			return "", "", false
		}
	}
	cmd = strings.ToLower(cmd)
	switch cmd {
	case "start", "link", "stop", "status":
	default:
		return "", "", false
	}
	return cmd, strings.TrimSpace(rest), true
}

func (a *TelegramAlerts) chatLinked(ctx context.Context, chatID int64) bool {
	var n int64
	_ = a.db.WithContext(ctx).Model(&types.TelegramAlertChat{}).
		Where("chat_id = ? AND disabled_at IS NULL", chatID).Count(&n).Error
	return n > 0
}

func (a *TelegramAlerts) linkChat(ctx context.Context, chat telegram.Chat, userID string) error {
	title := chat.Name()
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur types.TelegramAlertChat
		err := tx.Where("chat_id = ?", chat.ID).Take(&cur).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&types.TelegramAlertChat{
				ID: uuid.NewString(), ChatID: chat.ID, ChatType: chat.Type, Title: title,
				LinkedBy: userID, LinkedAt: a.now(),
			}).Error
		}
		if err != nil {
			return err
		}
		return tx.Model(&types.TelegramAlertChat{}).Where("id = ?", cur.ID).Updates(map[string]any{
			"chat_type": chat.Type, "title": title, "linked_by": userID, "linked_at": a.now(),
			"disabled_at": nil, "last_error": "",
		}).Error
	})
}

// botUser is the bot of c (getMe, cached a minute per token).
func (a *TelegramAlerts) botUser(ctx context.Context, c *telegram.Client) *telegram.User {
	info := a.botInfo(ctx, a.token(ctx), c)
	return info.user
}

func (a *TelegramAlerts) botInfo(ctx context.Context, token string, c *telegram.Client) telegramBotInfo {
	a.mu.Lock()
	cached := a.bot
	a.mu.Unlock()
	if cached.token == token && a.now().Sub(cached.checked) < telegramBotCheckTTL {
		return cached
	}
	if c == nil {
		c = a.client(token)
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	u, err := c.GetMe(cctx)
	info := telegramBotInfo{token: token, user: u, err: err, checked: a.now()}
	a.mu.Lock()
	a.bot = info
	a.mu.Unlock()
	return info
}

// ── link codes ──

func telegramCodeHash(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

func newTelegramLinkCode() (string, error) {
	buf := make([]byte, telegramLinkCodeRunes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, telegramLinkCodeRunes)
	for i, b := range buf {
		out[i] = telegramLinkAlphabet[int(b)%len(telegramLinkAlphabet)]
	}
	return string(out), nil
}

// CreateLinkCode makes a one-time code (10 minutes) that links the chat it
// is sent from to the bot on behalf of userID (a SuperAdmin).
func (a *TelegramAlerts) CreateLinkCode(ctx context.Context, userID string) (*types.TelegramLinkCode, error) {
	token := a.token(ctx)
	if token == "" {
		return nil, apperrors.NewBadRequestError("Chưa lưu token của bot Telegram.")
	}
	if !a.settings.GetBool(ctx, types.TelegramAlertsEnabledSettingKey, "", false) {
		// the bot reads no command while off: the code would expire unused
		return nil, apperrors.NewBadRequestError("Hãy bật \"Gửi cảnh báo lên Telegram\" và lưu trước khi tạo mã liên kết.")
	}
	info := a.botInfo(ctx, token, nil)
	if info.err != nil || info.user == nil || info.user.Username == "" {
		return nil, apperrors.NewBadRequestError("Không kiểm tra được bot Telegram: " + telegramPollError(errOr(info.err)))
	}
	code, err := newTelegramLinkCode()
	if err != nil {
		return nil, err
	}
	expires := a.now().Add(telegramLinkCodeTTL)
	key := telegramCodeHash(code)
	if a.rdb != nil {
		if err := a.rdb.Set(ctx, telegramLinkRedisKey+key, userID, telegramLinkCodeTTL).Err(); err != nil {
			return nil, fmt.Errorf("saving the link code: %w", err)
		}
	} else {
		a.mu.Lock()
		for k, e := range a.codes {
			if a.now().After(e.expires) {
				delete(a.codes, k)
			}
		}
		a.codes[key] = telegramLinkEntry{userID: userID, expires: expires}
		a.mu.Unlock()
	}
	bot := info.user.Username
	return &types.TelegramLinkCode{
		Code:        code,
		PrivateLink: "https://t.me/" + bot + "?start=" + code,
		GroupLink:   "https://t.me/" + bot + "?startgroup=" + code,
		ExpiresAt:   expires,
	}, nil
}

func errOr(err error) error {
	if err == nil {
		return errors.New("bot không có tên người dùng")
	}
	return err
}

// consumeLinkCode returns the SuperAdmin of code and forgets it.
func (a *TelegramAlerts) consumeLinkCode(ctx context.Context, code string) (string, bool) {
	key := telegramCodeHash(code)
	if a.rdb != nil {
		userID, err := a.rdb.GetDel(ctx, telegramLinkRedisKey+key).Result()
		if err != nil || userID == "" {
			return "", false
		}
		return userID, true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.codes[key]
	delete(a.codes, key)
	if !ok || a.now().After(e.expires) {
		return "", false
	}
	return e.userID, true
}

// ── the settings page ──

// Status reports the bot for the settings page.
func (a *TelegramAlerts) Status(ctx context.Context) (*types.TelegramAlertStatus, error) {
	token := a.token(ctx)
	st := &types.TelegramAlertStatus{
		Enabled:  a.settings.GetBool(ctx, types.TelegramAlertsEnabledSettingKey, "", false),
		HasToken: token != "",
		Kinds:    types.TelegramAlertKinds(),
		Muted:    []string{},
	}
	for k := range types.TelegramMutedKinds(a.settings.GetString(ctx, types.TelegramAlertsMutedKindsSettingKey, "", "")) {
		st.Muted = append(st.Muted, k)
	}
	if token != "" {
		info := a.botInfo(ctx, token, nil)
		if info.err != nil {
			st.BotError = telegramPollError(info.err)
		} else if info.user != nil {
			st.BotUsername = info.user.Username
			st.BotName = strings.TrimSpace(info.user.FirstName + " " + info.user.LastName)
		}
		a.mu.Lock()
		if st.BotError == "" && st.Enabled {
			st.BotError = a.pollErr
		}
		a.mu.Unlock()
	}
	db := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{})
	if err := db.Where("status = ?", types.TelegramMessagePending).Count(&st.Pending).Error; err != nil {
		return nil, err
	}
	if err := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).
		Where("status = ? AND created_at > ?", types.TelegramMessageFailed, a.now().Add(-24*time.Hour)).
		Count(&st.Failed24h).Error; err != nil {
		return nil, err
	}
	var rows []types.TelegramAlertMessage
	if err := a.db.WithContext(ctx).Where("status = ?", types.TelegramMessageSent).Order("sent_at DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		st.LastSent = rows[0].SentAt
	}
	return st, nil
}

// ListChats lists the linked chats, newest first.
func (a *TelegramAlerts) ListChats(ctx context.Context) ([]types.TelegramAlertChatView, error) {
	var chats []types.TelegramAlertChat
	if err := a.db.WithContext(ctx).Order("linked_at DESC").Find(&chats).Error; err != nil {
		return nil, err
	}
	admins, err := a.superAdmins(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	out := make([]types.TelegramAlertChatView, 0, len(chats))
	for _, c := range chats {
		name, ok := names[c.LinkedBy]
		if !ok {
			if u, err := a.users.GetUserByID(ctx, c.LinkedBy); err == nil && u != nil {
				name = u.Username
				if name == "" {
					name = u.Email
				}
			}
			names[c.LinkedBy] = name
		}
		out = append(out, types.TelegramAlertChatView{
			TelegramAlertChat: c, LinkedByName: name,
			Active: c.DisabledAt == nil && admins[c.LinkedBy],
		})
	}
	return out, nil
}

// DeleteChat unlinks a chat (its unsent messages go with it).
func (a *TelegramAlerts) DeleteChat(ctx context.Context, id string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c types.TelegramAlertChat
		if err := tx.Where("id = ?", id).Take(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.NewNotFoundError("chat not found")
			}
			return err
		}
		if err := tx.Where("chat_id = ? AND status = ?", c.ChatID, types.TelegramMessagePending).
			Delete(&types.TelegramAlertMessage{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&types.TelegramAlertChat{}).Error
	})
}

// SendTest queues a test message to every active chat; returns how many.
func (a *TelegramAlerts) SendTest(ctx context.Context, user *types.User) (int, error) {
	if !a.active(ctx) {
		return 0, apperrors.NewBadRequestError("Bot Telegram đang tắt hoặc chưa có token.")
	}
	who := ""
	if user != nil {
		who = user.Username
	}
	n, err := a.Enqueue(ctx, TelegramAlert{
		Kind: types.TelegramAlertTest, Key: "test:" + uuid.NewString(),
		Title: "Tin thử từ WeRAG", Body: "SuperAdmin " + who + " vừa gửi tin thử lúc " + formatDateTimeVN(a.now()) + ". Bot cảnh báo đang hoạt động.",
	})
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, apperrors.NewBadRequestError("Chưa có chat nào đang nhận cảnh báo. Hãy tạo mã liên kết và gửi cho bot trước.")
	}
	return n, nil
}
