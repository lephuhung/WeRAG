package types

import "time"

// Outgoing email (user decision 03/11/2026): SMTP is configured at runtime
// by a SuperAdmin through these system settings (category "email"); nothing
// is mailed while email.smtp_enabled is off or the host / sender address is
// empty.
const (
	SMTPEnabledSettingKey     = "email.smtp_enabled"
	SMTPHostSettingKey        = "email.smtp_host"
	SMTPPortSettingKey        = "email.smtp_port"
	SMTPUsernameSettingKey    = "email.smtp_username"
	SMTPPasswordSettingKey    = "email.smtp_password"
	SMTPFromAddressSettingKey = "email.smtp_from_address"
	SMTPFromNameSettingKey    = "email.smtp_from_name"
	SMTPSecuritySettingKey    = "email.smtp_security"

	// SMTP security modes: plain (none), upgrade with STARTTLS, or TLS from
	// the first byte (implicit TLS, usually port 465).
	SMTPSecurityNone     = "none"
	SMTPSecuritySTARTTLS = "starttls"
	SMTPSecurityTLS      = "tls"

	// DefaultSMTPPort is the submission port used with STARTTLS.
	DefaultSMTPPort = 587
)

// SubscriptionExpiryReminderDays are the reminders sent before a package's
// last usable day: 14, 7 and 1 day(s) ahead, each at most once per period.
var SubscriptionExpiryReminderDays = []int{14, 7, 1}

// Kinds of EmailNotification rows.
const (
	EmailNotificationSubscriptionExpiry = "subscription_expiry"
)

// EmailNotification.Recipients values besides a count: a claim still being
// sent (or left by a backend that died mid-send, released after
// EmailClaimStaleAfter) and one that had nobody to mail (kept, not retried).
const (
	EmailRecipientsPending = 0
	EmailRecipientsNone    = -1
)

// EmailClaimStaleAfter is how long a claim may stay pending before a run
// takes it for a crashed send and releases it for a retry. Longer than one
// reminder run may take.
const EmailClaimStaleAfter = 15 * time.Minute

// EmailNotification records a notification that must be mailed at most
// once (migration 000182): the row is claimed before sending (unique
// DedupeKey, so two backends cannot both send it) and removed again when
// the send fails, so the next run retries it.
type EmailNotification struct {
	ID         uint64    `json:"id"          gorm:"primaryKey;autoIncrement"`
	Kind       string    `json:"kind"        gorm:"type:varchar(32);not null"`
	DedupeKey  string    `json:"dedupe_key"  gorm:"type:varchar(200);not null;uniqueIndex"`
	RefID      string    `json:"ref_id"      gorm:"type:varchar(64);not null"`
	Recipients int       `json:"recipients"`
	CreatedAt  time.Time `json:"created_at"`
}

// TableName returns the table name.
func (EmailNotification) TableName() string { return "email_notifications" }

// LastUsableDayVN is the Vietnamese date a package end stored as the next
// Vietnam midnight still covers (end − 1 day), at 00:00 Vietnam time.
func LastUsableDayVN(endsAt time.Time) time.Time {
	local := endsAt.Add(-time.Nanosecond).In(usageLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, usageLocation)
}

// DaysUntilLastUsableDayVN counts whole Vietnamese days from now's date to
// the last usable day of endsAt (0 = today is the last day, negative =
// already over).
func DaysUntilLastUsableDayVN(now, endsAt time.Time) int {
	last := LastUsableDayVN(endsAt)
	n := now.In(usageLocation)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, usageLocation)
	a := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// DueExpiryReminder returns the reminder threshold due for daysLeft (the
// smallest of SubscriptionExpiryReminderDays not below it), or 0 when none
// is due. A package bought with 5 days left gets the 7-day reminder, never
// the 14-day one afterwards.
func DueExpiryReminder(daysLeft int) int {
	if daysLeft < 0 {
		return 0
	}
	due := 0
	for _, d := range SubscriptionExpiryReminderDays {
		if daysLeft <= d && (due == 0 || d < due) {
			due = d
		}
	}
	return due
}
