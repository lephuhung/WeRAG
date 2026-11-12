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

	// SuperAdmin review-queue digests (payload:
	// UserNotificationQueuePayload), one open digest per SuperAdmin and
	// kind like abbreviation_pending, closed for everyone once the queue
	// is empty.

	// UserNotificationPaymentOrderPending: new payment orders wait for
	// their transfer / a SuperAdmin's confirmation (items: order code,
	// plan, amount, organization).
	UserNotificationPaymentOrderPending = "payment_order_pending"
	// UserNotificationLegalRequestPending: people asked in chat for legal
	// documents the system lacks (items: số hiệu, title, request count).
	UserNotificationLegalRequestPending = "legal_request_pending"
	// UserNotificationLegalDraftReady: OCR drafts of scanned legal
	// documents are ready for review (items: số hiệu, title).
	UserNotificationLegalDraftReady = "legal_draft_ready"
	// UserNotificationLegalLinkPending: amendment links a legal feed pushed
	// wait for review (items: the pushed document, its pending links).
	UserNotificationLegalLinkPending = "legal_link_pending"
	// UserNotificationLegalIdentityPending: documents of the public legal
	// KBs wait for their identity review (items: số hiệu or file title).
	UserNotificationLegalIdentityPending = "legal_identity_pending"

	// UserNotificationBackupAlert: backup jobs failed, are overdue or
	// left no readable status (payload: UserNotificationBackupAlertPayload).
	// One open digest per SuperAdmin like the queues above, closed for
	// everyone once every job is healthy again.
	UserNotificationBackupAlert = "backup_alert"
)

// UserNotificationBackupAlertPayload is the payload of a backup_alert
// digest: the jobs with a problem, in job order. Signature names the set
// of (job, problem) pairs, so an unchanged incident is not toasted again.
type UserNotificationBackupAlertPayload struct {
	Count     int                         `json:"count"`
	Jobs      []UserNotificationBackupJob `json:"jobs"`
	Signature string                      `json:"signature"`
}

// Problems of a UserNotificationBackupJob.
const (
	BackupProblemFailed     = "failed"     // the last run ended with ok=false
	BackupProblemStale      = "stale"      // the last run is older than the job's limit
	BackupProblemMissing    = "missing"    // no status file on the backup storage
	BackupProblemUnreadable = "unreadable" // the status file could not be read or parsed
)

// UserNotificationBackupJob is one job a backup_alert names. FinishedAt is
// its last run (nil: none known), LastOKAt its last successful run when
// known; Note the error or note of that run, or the read error.
type UserNotificationBackupJob struct {
	Job        string     `json:"job"`
	Problem    string     `json:"problem"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	LastOKAt   *time.Time `json:"last_ok_at,omitempty"`
	Note       string     `json:"note,omitempty"`
}

// UserNotificationQueueMaxItems caps the newest items a review-queue
// digest names; Count says how many arrived.
const UserNotificationQueueMaxItems = 5

// UserNotificationQueuePayload is the payload of a review-queue digest:
// how many items arrived since the person last read it, and the newest of
// them first.
type UserNotificationQueuePayload struct {
	Count int                         `json:"count"`
	Items []UserNotificationQueueItem `json:"items"`
}

// UserNotificationQueueItem is one item a review-queue digest names. Ref
// is the order code or the số hiệu (a file title when none is known); the
// other fields are set by the queues that have them.
type UserNotificationQueueItem struct {
	Ref   string `json:"ref"`
	Title string `json:"title,omitempty"`
	// Count: the requests of a legal document request, the pending links
	// of a pushed document.
	Count     int    `json:"count,omitempty"`
	Plan      string `json:"plan,omitempty"`
	AmountVND int64  `json:"amount_vnd,omitempty"`
	Org       string `json:"org,omitempty"`
	// Untitled: an item without số hiệu or title (Ref empty); clients name
	// it "untitled document" in the reader's language.
	Untitled bool `json:"untitled,omitempty"`
}

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
