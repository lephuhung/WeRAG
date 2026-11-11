package types

import (
	"strings"
	"time"
)

// UserNotification is an in-app notice for one person (migration 000196),
// shown by the web app and the Word add-in while the person is online:
// a toast once (SeenAt), then in the bell list until read (ReadAt).
//
// Notifications belong to the account, not to an organization: the person
// sees all of theirs whichever organization (or the public portal) they
// are in; TenantID only records where the event happened. Producers insert
// with a DedupeKey so a repeated run never notifies twice.
type UserNotification struct {
	ID       string `json:"id"        gorm:"type:varchar(36);primaryKey"`
	TenantID uint64 `json:"tenant_id" gorm:"not null;default:0"`
	UserID   string `json:"-"         gorm:"type:varchar(36);not null;index:idx_user_notifications_user_read,priority:1"`
	Kind     string `json:"kind"      gorm:"type:varchar(64);not null"`
	Title    string `json:"title"     gorm:"type:text;not null;default:''"`
	Body     string `json:"body"      gorm:"type:text;not null;default:''"`
	// Link is an app-relative path ("/platform/chat/<id>"), "" for none.
	Link      string     `json:"link"       gorm:"type:varchar(1024);not null;default:''"`
	Payload   JSON       `json:"payload"    gorm:"type:jsonb"`
	DedupeKey string     `json:"-"          gorm:"type:varchar(200);not null;uniqueIndex"`
	CreatedAt time.Time  `json:"created_at"`
	SeenAt    *time.Time `json:"seen_at"`
	ReadAt    *time.Time `json:"read_at"    gorm:"index:idx_user_notifications_user_read,priority:2"`
}

// TableName returns the table name.
func (UserNotification) TableName() string { return "user_notifications" }

// Kinds of UserNotification.
const (
	// UserNotificationLegalRequestAdded: a legal document the person asked
	// for was added (payload: UserNotificationLegalRequestPayload).
	UserNotificationLegalRequestAdded = "legal_request_added"
	// UserNotificationAbbreviationPending: new abbreviation suggestions wait
	// for a SuperAdmin's review (payload:
	// UserNotificationAbbreviationPendingPayload). A digest: each SuperAdmin
	// has at most one unread notice of this kind, updated by every new
	// suggestion (UserNotificationOpenKey).
	UserNotificationAbbreviationPending = "abbreviation_pending"
)

// UserNotificationAbbreviationPendingMaxItems caps the newest suggestions an
// abbreviation_pending digest names; Count says how many there are.
const UserNotificationAbbreviationPendingMaxItems = 5

// UserNotificationAbbreviationPendingPayload is the payload of a
// UserNotificationAbbreviationPending digest: how many suggestions arrived
// since the person last read it, and the newest of them first.
type UserNotificationAbbreviationPendingPayload struct {
	Count int                                `json:"count"`
	Items []UserNotificationAbbreviationItem `json:"items"`
}

// UserNotificationAbbreviationItem is one suggestion named by the digest.
type UserNotificationAbbreviationItem struct {
	ShortForm  string `json:"short_form"`
	FullForm   string `json:"full_form"`
	ProperName bool   `json:"proper_name,omitempty"`
}

// userNotificationOpenSuffix ends the dedupe key of an open digest: the one
// unread notice of a kind a producer keeps updating for a person. Reading
// it (or the producer closing it) rotates the key to
// "<key>:read:<id>", freeing it for the next digest; the unique index on
// dedupe_key thus allows one open digest per key without a partial index.
const userNotificationOpenSuffix = ":open"

// UserNotificationOpenKey is the dedupe key of the open digest of kind for
// userID.
func UserNotificationOpenKey(kind, userID string) string {
	return kind + ":" + userID + userNotificationOpenSuffix
}

// IsUserNotificationOpenKey reports whether key is an open digest key.
func IsUserNotificationOpenKey(key string) bool {
	return strings.HasSuffix(key, userNotificationOpenSuffix)
}

// UserNotificationRotateOpenKeySQL is the SQL value of dedupe_key once a
// row is read: an open digest key becomes "<key>:read:<id>" (unique, as the
// id is), any other key is kept. Portable to Postgres and SQLite.
const UserNotificationRotateOpenKeySQL = "CASE WHEN dedupe_key LIKE '%" + userNotificationOpenSuffix +
	"' THEN dedupe_key || ':read:' || id ELSE dedupe_key END"

// UserNotificationLegalRequestPayload is the payload of a
// UserNotificationLegalRequestAdded notice. WordFileID is the Word file the
// conversation is bound to (the add-in opens the conversation only from
// that file), "" for a web conversation.
type UserNotificationLegalRequestPayload struct {
	RequestID      string `json:"request_id"`
	DocumentNumber string `json:"document_number"`
	Title          string `json:"title"`
	SessionID      string `json:"session_id"`
	Channel        string `json:"channel"`
	WordFileID     string `json:"word_file_id,omitempty"`
}

// UserNotificationListMax caps one page of the notification list.
const UserNotificationListMax = 100

// UserNotificationLinkPrefix starts every notification link: a page of the
// app (frontend-next /platform/…), never another site.
const UserNotificationLinkPrefix = "/platform/"

// UserNotificationRetention: read notifications older than this are purged.
const UserNotificationRetention = 90 * 24 * time.Hour

// ValidUserNotificationLink reports whether link is "" or an app-relative
// path under UserNotificationLinkPrefix with no control characters, no
// "//" and no backslash (which browsers may read as another host).
func ValidUserNotificationLink(link string) bool {
	if link == "" {
		return true
	}
	if !strings.HasPrefix(link, UserNotificationLinkPrefix) || strings.Contains(link, "//") || strings.Contains(link, "\\") {
		return false
	}
	for _, r := range link {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
