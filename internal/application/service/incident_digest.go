package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Tencent/WeKnora/internal/common/redislock"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// incidentDigest is the SuperAdmin alert of an ongoing incident (backup
// jobs failing, the legal data workers failing): one open digest per
// SuperAdmin and kind (reviewDigests), whose payload carries a signature
// of what is wrong and, once a check finds everything healthy again, a
// resolved_at stamp.
//
// A digest is written only when the signature changes, or a day after a
// SuperAdmin read it while the incident goes on, so a check every few
// minutes does not toast the same incident again. resolve stamps every
// SuperAdmin's latest alert resolved (in its payload, so a restart still
// tells a recurrence from the same incident) and closes the open ones.
type incidentDigest struct {
	digests *reviewDigests
	kind    string
	// what names the alert in errors ("backup alert").
	what string
}

// incidentRemindAfter: a read alert of an incident still going on is
// written again after this long.
const incidentRemindAfter = 24 * time.Hour

// incidentPayloadHead is the part of an incident payload the digest
// machinery reads; the rest belongs to the kind.
type incidentPayloadHead struct {
	Signature  string     `json:"signature"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// raise writes the digest built by build to every SuperAdmin whose latest
// alert of the kind is not this incident already (open with the same
// signature, or read less than a day ago, and not resolved since).
func (d incidentDigest) raise(ctx context.Context, signature string,
	build func(userID string, at time.Time) *types.UserNotification,
) error {
	recipients, err := d.digests.reviewers(ctx, "")
	if err != nil {
		return fmt.Errorf("listing SuperAdmins: %w", err)
	}
	now := d.digests.now()
	var failed []string
	for _, userID := range recipients {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		prev, err := d.digests.notifications.LatestOfKind(ctx, userID, d.kind)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
			continue
		}
		if incidentAlertCurrent(prev, signature, now) {
			continue
		}
		if _, err := d.digests.notifications.UpsertOpenDigest(ctx, build(userID, now), func(_, next *types.UserNotification) {
			*next = *build(next.UserID, next.CreatedAt)
		}); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%s of %d SuperAdmin(s) not written: %s", d.what, len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// resolve stamps every SuperAdmin's latest alert of the kind resolved
// (once) and closes the open ones.
func (d incidentDigest) resolve(ctx context.Context) error {
	recipients, err := d.digests.reviewers(ctx, "")
	if err != nil {
		return fmt.Errorf("listing SuperAdmins: %w", err)
	}
	now := d.digests.now()
	var failed []string
	for _, userID := range recipients {
		prev, err := d.digests.notifications.LatestOfKind(ctx, userID, d.kind)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
			continue
		}
		if prev == nil {
			continue
		}
		raw, ok := incidentResolvedPayload(prev.Payload, now)
		if !ok {
			continue
		}
		if err := d.digests.notifications.SetPayload(ctx, userID, prev.ID, raw); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", userID, err))
		}
	}
	if err := d.digests.closeIfEmpty(ctx, d.kind,
		func(context.Context) (int64, error) { return 0, nil }); err != nil {
		failed = append(failed, err.Error())
	}
	if len(failed) > 0 {
		return fmt.Errorf("resolving the %s: %s", d.what, strings.Join(failed, "; "))
	}
	return nil
}

// incidentResolvedPayload stamps resolved_at into payload, keeping the
// kind's own fields; false when it is resolved already or not an object
// (not ours to rewrite).
func incidentResolvedPayload(payload types.JSON, at time.Time) (types.JSON, bool) {
	fields := map[string]json.RawMessage{}
	if len(payload) > 0 && json.Unmarshal(payload, &fields) != nil {
		return nil, false
	}
	if r, ok := fields["resolved_at"]; ok && string(r) != "null" {
		return nil, false
	}
	stamp, _ := json.Marshal(at)
	fields["resolved_at"] = stamp
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, false
	}
	return types.JSON(raw), true
}

// incidentAlertCurrent reports whether prev (the person's latest alert of
// the kind) already tells the incident of signature: same signature, not
// resolved since, and unread or read less than a day ago.
func incidentAlertCurrent(prev *types.UserNotification, signature string, now time.Time) bool {
	if prev == nil {
		return false
	}
	var p incidentPayloadHead
	if len(prev.Payload) == 0 || json.Unmarshal(prev.Payload, &p) != nil || p.Signature != signature || p.ResolvedAt != nil {
		return false
	}
	if prev.ReadAt == nil {
		return true
	}
	return now.Sub(*prev.ReadAt) < incidentRemindAfter
}

// monitorLock takes a cross-instance lock of a monitor's check (Redis);
// without Redis, or when Redis fails, the check runs anyway (the checks
// are idempotent). ok false: another instance holds it.
func monitorLock(ctx context.Context, rdb *redis.Client, key string, lease time.Duration, tag string) (release func(), ok bool) {
	if rdb == nil {
		return func() {}, true
	}
	token, err := redislock.NewToken()
	if err != nil {
		return func() {}, true
	}
	got, err := redislock.TryAcquire(ctx, rdb, key, token, lease)
	if err != nil {
		logger.Warnf(ctx, "[%s] lock: %v (checking anyway)", tag, err)
		return func() {}, true
	}
	if !got {
		return nil, false
	}
	return func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = redislock.Release(rctx, rdb, key, token)
	}, true
}
