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
)

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
