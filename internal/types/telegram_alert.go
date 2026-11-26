package types

import (
	"strings"
	"time"
)

// The system alert bot on Telegram (migration 000203): one bot for the
// whole platform, configured and used by SuperAdmins only. SuperAdmins link
// their private chat, or a group of the people who handle the alerts, with
// a one-time code; the bot then writes the SuperAdmin alerts there (the
// review-queue digests, the incidents, the payment-order events).

// Settings of the alert bot (system settings, category "telegram").
const (
	TelegramAlertsEnabledSettingKey    = "telegram_alerts.enabled"
	TelegramAlertsBotTokenSettingKey   = "telegram_alerts.bot_token"
	TelegramAlertsMutedKindsSettingKey = "telegram_alerts.muted_kinds"
)

// Alert kinds sent only to Telegram (the others are the SuperAdmin
// UserNotification kinds, relayed from the in-app digests).
const (
	// TelegramAlertOrderPaid: a payment order was paid (transfer matched or
	// confirmed by a SuperAdmin).
	TelegramAlertOrderPaid = "order_paid"
	// TelegramAlertOrderUnderpaid: a transfer brought less than the order
	// asks.
	TelegramAlertOrderUnderpaid = "order_underpaid"
	// TelegramAlertOrderExpiring: a pending order is in the last part of
	// its payment window (TelegramOrderExpiringWithin).
	TelegramAlertOrderExpiring = "order_expiring"
	// TelegramAlertOrderExpired: a pending order passed its payment deadline
	// and was cancelled.
	TelegramAlertOrderExpired = "order_expired"
	// TelegramAlertOrderPaymentHeld: a transfer came for an order that is
	// closed (expired past the grace, cancelled, paid): a SuperAdmin decides.
	TelegramAlertOrderPaymentHeld = "order_payment_held"
	// TelegramAlertTest: "Gửi tin thử" (never muted).
	TelegramAlertTest = "test"
	// TelegramAlertLinked: the bot greets a chat just linked (never muted).
	TelegramAlertLinked = "linked"
)

// TelegramOrderExpiringWithin is how long before an order's payment
// deadline the "sắp hết hạn" alert goes out: the last third of the payment
// window (10 minutes of 30), at most 12 hours. Only an order at least that
// old is told, so it never comes with the order's creation.
func TelegramOrderExpiringWithin(window time.Duration) time.Duration {
	if window <= 0 {
		return 12 * time.Hour
	}
	within := window / 3
	if within > 12*time.Hour {
		within = 12 * time.Hour
	}
	return within
}

// TelegramAlertKind is one kind a SuperAdmin can mute, with its group in
// the settings page.
type TelegramAlertKind struct {
	Kind  string `json:"kind"`
	Group string `json:"group"` // billing, review, incident
	// Label names the kind in Vietnamese (the resolution messages); the
	// pages use their own dictionary.
	Label string `json:"label"`
	// Resolvable kinds are digests: the bot says (silently) when their queue
	// is empty or the incident is over.
	Resolvable bool `json:"resolvable"`
}

// TelegramAlertKinds lists the kinds the bot sends, in page order.
func TelegramAlertKinds() []TelegramAlertKind {
	return []TelegramAlertKind{
		{Kind: UserNotificationPaymentOrderPending, Group: "billing", Label: "đơn thanh toán chờ xác nhận", Resolvable: true},
		{Kind: TelegramAlertOrderPaid, Group: "billing", Label: "đơn đã thanh toán"},
		{Kind: TelegramAlertOrderUnderpaid, Group: "billing", Label: "đơn chuyển thiếu tiền"},
		{Kind: TelegramAlertOrderExpiring, Group: "billing", Label: "đơn sắp hết hạn thanh toán"},
		{Kind: TelegramAlertOrderExpired, Group: "billing", Label: "đơn quá hạn đã tự hủy"},
		{Kind: TelegramAlertOrderPaymentHeld, Group: "billing", Label: "tiền chuyển vào đơn đã đóng"},
		{Kind: UserNotificationAbbreviationPending, Group: "review", Label: "từ viết tắt chờ duyệt", Resolvable: true},
		{Kind: UserNotificationLegalRequestPending, Group: "review", Label: "văn bản pháp luật người dùng yêu cầu", Resolvable: true},
		{Kind: UserNotificationLegalDraftReady, Group: "review", Label: "bản OCR chờ duyệt", Resolvable: true},
		{Kind: UserNotificationLegalLinkPending, Group: "review", Label: "liên kết sửa đổi chờ duyệt", Resolvable: true},
		{Kind: UserNotificationLegalIdentityPending, Group: "review", Label: "định danh văn bản chờ duyệt", Resolvable: true},
		{Kind: UserNotificationBackupAlert, Group: "incident", Label: "sự cố sao lưu", Resolvable: true},
		{Kind: UserNotificationLegalSyncAlert, Group: "incident", Label: "sự cố đồng bộ dữ liệu pháp luật", Resolvable: true},
		{Kind: UserNotificationSearchIndexAlert, Group: "incident", Label: "sự cố chỉ mục tìm kiếm", Resolvable: true},
	}
}

// TelegramAlertKindOf returns the kind's entry; false for a kind the bot
// does not send.
func TelegramAlertKindOf(kind string) (TelegramAlertKind, bool) {
	for _, k := range TelegramAlertKinds() {
		if k.Kind == kind {
			return k, true
		}
	}
	return TelegramAlertKind{}, false
}

// TelegramAlertChat is a chat the bot writes alerts to.
type TelegramAlertChat struct {
	ID       string `json:"id"         gorm:"type:varchar(36);primaryKey"`
	ChatID   int64  `json:"-"          gorm:"not null;uniqueIndex"`
	ChatType string `json:"chat_type"  gorm:"type:varchar(16);not null;default:''"`
	Title    string `json:"title"      gorm:"type:text;not null;default:''"`
	// LinkedBy is the SuperAdmin whose code linked the chat; the chat gets
	// alerts while that account is an active SuperAdmin.
	LinkedBy   string     `json:"linked_by"   gorm:"type:varchar(36);not null"`
	LinkedAt   time.Time  `json:"linked_at"`
	DisabledAt *time.Time `json:"disabled_at"`
	LastError  string     `json:"last_error"  gorm:"type:text;not null;default:''"`
}

// TableName returns the table name.
func (TelegramAlertChat) TableName() string { return "telegram_alert_chats" }

// Statuses of TelegramAlertMessage.
const (
	TelegramMessagePending = "pending"
	TelegramMessageSent    = "sent"
	TelegramMessageFailed  = "failed"
	// TelegramMessageMarker: a chat-less row (chat 0, never sent) that ends
	// the episode of a kind while nobody is told (bot off, kind muted).
	TelegramMessageMarker = "marker"
)

// TelegramAlertMessage is one alert to one chat (the outbox).
type TelegramAlertMessage struct {
	ID            string     `gorm:"type:varchar(36);primaryKey"`
	ChatID        int64      `gorm:"not null;uniqueIndex:idx_telegram_alert_messages_chat_event,priority:1"`
	Kind          string     `gorm:"type:varchar(64);not null"`
	EventKey      string     `gorm:"type:varchar(200);not null;index;uniqueIndex:idx_telegram_alert_messages_chat_event,priority:2"`
	Text          string     `gorm:"type:text;not null"`
	Silent        bool       `gorm:"not null;default:false"`
	Status        string     `gorm:"type:varchar(16);not null;default:'pending'"`
	Attempts      int        `gorm:"not null;default:0"`
	NextAttemptAt time.Time  `gorm:"not null"`
	MessageID     *int64     `gorm:""`
	LastError     string     `gorm:"type:text;not null;default:''"`
	CreatedAt     time.Time  `gorm:"not null"`
	SentAt        *time.Time `gorm:""`
}

// TableName returns the table name.
func (TelegramAlertMessage) TableName() string { return "telegram_alert_messages" }

// TelegramAlertChatView is a linked chat as the settings page shows it.
type TelegramAlertChatView struct {
	TelegramAlertChat
	LinkedByName string `json:"linked_by_name"`
	// Active: the chat gets alerts (not disabled, its SuperAdmin still one).
	Active bool `json:"active"`
}

// TelegramAlertStatus is the state of the bot for the settings page.
type TelegramAlertStatus struct {
	Enabled  bool `json:"enabled"`
	HasToken bool `json:"has_token"`
	// BotUsername is the bot's @name (getMe), "" when the token is not
	// checked or refused.
	BotUsername string `json:"bot_username"`
	BotName     string `json:"bot_name"`
	// BotError: why the bot cannot work (token refused, network, another
	// program reading the same bot's updates).
	BotError string `json:"bot_error,omitempty"`
	// Pending: messages waiting to be sent; Failed24h: given up on in the
	// last day.
	Pending   int64               `json:"pending"`
	Failed24h int64               `json:"failed_24h"`
	LastSent  *time.Time          `json:"last_sent_at"`
	Kinds     []TelegramAlertKind `json:"kinds"`
	Muted     []string            `json:"muted_kinds"`
}

// TelegramLinkCode is a one-time code that links a chat to the bot.
type TelegramLinkCode struct {
	Code string `json:"code"`
	// PrivateLink opens a private chat with the bot; GroupLink adds the bot
	// to a group. Both carry the code.
	PrivateLink string    `json:"private_link"`
	GroupLink   string    `json:"group_link"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ValidTelegramBotToken reports whether s looks like a Bot API token
// ("123456789:AAH…": the bot id, a colon, 30+ letters, digits, '-' or '_').
func ValidTelegramBotToken(s string) bool {
	id, secret, ok := strings.Cut(s, ":")
	if !ok || len(id) < 5 || len(id) > 20 || len(secret) < 30 || len(secret) > 100 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	for _, r := range secret {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// TelegramMutedKinds parses the muted_kinds setting (comma separated).
func TelegramMutedKinds(raw string) map[string]bool {
	out := map[string]bool{}
	for _, k := range strings.Split(raw, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out[k] = true
		}
	}
	return out
}
