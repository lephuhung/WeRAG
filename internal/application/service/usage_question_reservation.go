package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Questions are counted when the turn's user message is saved, after the
// usage check has let the turn through. Two turns of one scope checked
// together both read the same count and both passed, so a burst overshot
// a questions cap (a Guest's 10 a day). The check now reserves the
// question it lets through: the counters are read and the reservation
// taken under one lock per counted scope, and the reservation is released
// once the question is in the counters (UsageTurnGrant.ReleaseReservation,
// called by the message meter) or after questionReservationTTL for a turn
// that never saved its question. A refused or failed turn is not counted,
// as before: there is nothing to refund.
//
// The lock and the reservations are held in this process: with several
// backend replicas a burst split across them can still overshoot by the
// number of replicas. One backend serves this deployment.

// questionReservationTTL bounds how long a reservation of a turn that never
// saved its question holds a place under the cap.
const questionReservationTTL = 2 * time.Minute

const questionReservationStripes = 64

// questionReservations are the questions let through and not yet counted,
// per counted scope ("scopeType/scopeID").
type questionReservations struct {
	ttl     time.Duration
	seq     atomic.Uint64
	stripes [questionReservationStripes]reservationStripe
}

type reservationStripe struct {
	mu   sync.Mutex
	held map[string]map[uint64]time.Time // scope → reservation → expiry
}

func newQuestionReservations(ttl time.Duration) *questionReservations {
	return &questionReservations{ttl: ttl}
}

// sharedQuestionReservations is the registry every usage guard of the
// process shares (NewUsageGuard).
var sharedQuestionReservations = newQuestionReservations(questionReservationTTL)

func (r *questionReservations) stripe(key string) *reservationStripe {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &r.stripes[h.Sum32()%questionReservationStripes]
}

// pending counts key's live reservations, dropping expired ones. The
// stripe's lock must be held.
func (s *reservationStripe) pending(key string, now time.Time) int64 {
	var n int64
	for id, exp := range s.held[key] {
		if !now.Before(exp) {
			delete(s.held[key], id)
			continue
		}
		n++
	}
	if len(s.held[key]) == 0 {
		delete(s.held, key)
	}
	return n
}

// add records a reservation of key and returns its release. The stripe's
// lock must be held; the release takes it itself and runs once.
func (r *questionReservations) add(s *reservationStripe, key string, now time.Time) func() {
	id := r.seq.Add(1)
	if s.held == nil {
		s.held = map[string]map[uint64]time.Time{}
	}
	if s.held[key] == nil {
		s.held[key] = map[uint64]time.Time{}
	}
	s.held[key][id] = now.Add(r.ttl)
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.held[key], id)
			if len(s.held[key]) == 0 {
				delete(s.held, key)
			}
		})
	}
}

// reserveQuestion holds a place for the coming question under ent's
// questions caps, or refuses the turn when the counted questions plus the
// reservations of turns not yet counted reach a cap. Nil release when ent
// has no questions cap. A counters read that fails lets the turn through,
// as the rest of the check does.
func (g *usageGuard) reserveQuestion(ctx context.Context, ent *types.UsageEntitlement) (func(), error) {
	if g.reservations == nil || ent == nil || ent.CounterScopeID == "" {
		return nil, nil
	}
	var limits types.UsageLimits
	for _, st := range ent.Limits {
		if st.Metric == types.UsageMetricQuestions && st.Limit > 0 {
			limits = append(limits, types.UsageLimit{Metric: st.Metric, Period: st.Period, Limit: st.Limit})
		}
	}
	if len(limits) == 0 {
		return nil, nil
	}
	key := ent.CounterScopeType + "/" + ent.CounterScopeID
	s := g.reservations.stripe(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := g.now()
	statuses, err := g.limitStatus(ctx, limits, ent.CounterScopeType, ent.CounterScopeID, now)
	if err != nil {
		logger.Warnf(ctx, "usage guard: questions of %s unavailable, letting the turn through: %v", key, err)
		return nil, nil
	}
	pending := s.pending(key, now)
	for _, st := range statuses {
		if used := st.Used + pending; used >= st.Limit {
			return nil, apperrors.NewTooManyRequestsError(
				fmt.Sprintf("usage limit reached: %d %s per %s", st.Limit, st.Metric, st.Period)).
				WithDetails(types.UsageDenial{
					Reason: types.UsageDenialLimit, Metric: st.Metric, Period: st.Period,
					Limit: st.Limit, Used: used,
				})
		}
	}
	return g.reservations.add(s, key, now), nil
}
