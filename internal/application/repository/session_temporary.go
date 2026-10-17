package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// keepTemporarySession clears is_temporary on the session a message was just
// written to and on its document-assistant root: a sub-chat works on the
// root's documents, so chatting there keeps the root as well. The predicate
// makes it a no-op for every session that is already kept.
func keepTemporarySession(tx *gorm.DB, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return tx.Exec(`UPDATE sessions SET is_temporary = FALSE
		WHERE is_temporary AND (id = ? OR id = (
			SELECT NULLIF(chat_root_id, '') FROM sessions WHERE id = ?))`,
		sessionID, sessionID).Error
}

// PurgeExpiredTemporarySessions soft-deletes the temporary sessions idle
// past their TTL (plain chat / with an open document) through the SQL
// function of migration 000153, which an insert trigger on sessions also
// runs. It returns how many sessions were removed.
func PurgeExpiredTemporarySessions(ctx context.Context, db *gorm.DB, plainTTL, documentTTL time.Duration) (int, error) {
	var removed int
	err := db.WithContext(ctx).
		Raw("SELECT purge_expired_temporary_sessions(make_interval(secs => ?), make_interval(secs => ?))",
			plainTTL.Seconds(), documentTTL.Seconds()).
		Scan(&removed).Error
	return removed, err
}
