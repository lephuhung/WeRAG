package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// The SuperAdmin digest of a fragmented BM25 index. SearchIndexHealth
// already reports on the runtime page and warns in the log every hour, but
// both of those wait for somebody to look: the last time this index
// fragmented it reached 187 segments unnoticed, and keyword search had been
// paying a fixed ~550 ms per query for who knows how long. So the check also
// raises an in-app notice, the way the backup and legal-sync monitors do.
//
// One open digest per SuperAdmin, rewritten only when the signature changes
// or a day after it was read, and stamped resolved once the index is back
// under its threshold (see incidentDigest).
const (
	// SearchIndexAlertLink opens the runtime page holding the index card.
	SearchIndexAlertLink = "/platform/admin?section=queues"

	searchIndexAlertLockKey   = "werag:lock:search-index-alert"
	searchIndexAlertLockLease = 2 * time.Minute
)

// WithAlerts turns on the SuperAdmin digest. Without it — or without a user
// and notification repository — the check still reports and logs, which is
// what the unit tests and any partially wired container get.
func (h *SearchIndexHealth) WithAlerts(users interfaces.UserRepository,
	notifications interfaces.UserNotificationRepository, rdb *redis.Client,
) *SearchIndexHealth {
	if h == nil || users == nil || notifications == nil {
		return h
	}
	h.digests = newReviewDigests("SearchIndexHealth", users, notifications)
	h.redis = rdb
	return h
}

// alerting reports whether the digest is wired.
func (h *SearchIndexHealth) alerting() bool {
	return h != nil && h.digests != nil
}

// incident is the search_index_alert digest machinery.
func (h *SearchIndexHealth) incident() incidentDigest {
	return incidentDigest{
		digests: h.digests,
		kind:    types.UserNotificationSearchIndexAlert,
		what:    "search index alert",
	}
}

// alert raises or resolves the digest for rep. Only one replica does this
// per interval — the digest is shared state, unlike the log line — and a
// report that could not be read at all decides nothing: it is neither an
// incident nor evidence that one ended.
func (h *SearchIndexHealth) alert(ctx context.Context, rep SearchIndexHealthReport) {
	if !h.alerting() || !rep.Available {
		return
	}
	release, ok := monitorLock(ctx, h.redis, searchIndexAlertLockKey,
		searchIndexAlertLockLease, "SearchIndexHealth")
	if !ok {
		return // another replica is handling this pass
	}
	defer release()

	var err error
	if rep.NeedsRebuild {
		payload := searchIndexAlertPayload(rep)
		err = h.incident().raise(ctx, payload.Signature,
			func(userID string, at time.Time) *types.UserNotification {
				return searchIndexAlertNotification(userID, payload, at)
			})
	} else {
		err = h.incident().resolve(ctx)
	}
	if err != nil {
		logger.Warnf(ctx, "[SearchIndexHealth] writing the SuperAdmin digest failed: %v", err)
	}
}

// searchIndexAlertPayload describes the incident. The signature is the index
// name alone: segments keep creeping up while an incident lasts, and keying
// on the count would rewrite the digest on every pass.
func searchIndexAlertPayload(rep SearchIndexHealthReport) types.UserNotificationSearchIndexAlertPayload {
	return types.UserNotificationSearchIndexAlertPayload{
		Index:       rep.Index,
		Segments:    rep.Segments,
		Threshold:   rep.Threshold,
		DeletedDocs: rep.DeletedDocs,
		Signature:   rep.Index + ":fragmented",
	}
}

// searchIndexAlertNotification builds one SuperAdmin's digest. Title and
// body carry the stored Vietnamese text; the clients re-word it from the
// payload so a locale change does not need a rewrite.
func searchIndexAlertNotification(userID string,
	p types.UserNotificationSearchIndexAlertPayload, at time.Time,
) *types.UserNotification {
	payload, _ := json.Marshal(p)
	body := fmt.Sprintf(
		"Chỉ mục %s có %d segment (ngưỡng %d), còn %d bản ghi đã xoá. "+
			"Mỗi truy vấn từ khoá đang tốn thêm một khoản cố định cho tới khi dựng lại chỉ mục.",
		p.Index, p.Segments, p.Threshold, p.DeletedDocs)
	return &types.UserNotification{
		TenantID:  types.PlatformTenantID,
		UserID:    userID,
		Kind:      types.UserNotificationSearchIndexAlert,
		Title:     "Chỉ mục tìm kiếm từ khoá cần dựng lại",
		Body:      queueEllipsis(body, searchIndexAlertBodyRunes),
		Link:      SearchIndexAlertLink,
		Payload:   types.JSON(payload),
		DedupeKey: types.UserNotificationOpenKey(types.UserNotificationSearchIndexAlert, userID),
		CreatedAt: at,
	}
}

// searchIndexAlertBodyRunes caps the stored body like the other digests.
const searchIndexAlertBodyRunes = 300
