package tools

import (
	"context"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// docProfileRefreshDelay is the fixed refresh schedule of a target's
// profile: it is looked at again this long after the first save it does
// not cover. Later saves inside the window do not move the mark, so a user
// who keeps typing still gets a fresh profile every few minutes (unlike the
// format re-check, which waits for editing to settle).
const docProfileRefreshDelay = 5 * time.Minute

// profileScheduler holds one pending refresh per document. now and after
// are the clock and the timer, replaced in tests.
type profileScheduler struct {
	mu      sync.Mutex
	pending map[string]*profileTimer
	delay   time.Duration
	now     func() time.Time
	after   func(d time.Duration, f func()) (stop func() bool)
}

type profileTimer struct {
	due  time.Time
	stop func() bool
	run  func()
}

func newProfileScheduler() *profileScheduler {
	return &profileScheduler{
		pending: map[string]*profileTimer{},
		delay:   docProfileRefreshDelay,
		now:     time.Now,
		after: func(d time.Duration, f func()) func() bool {
			return time.AfterFunc(d, f).Stop
		},
	}
}

var docProfileSchedule = newProfileScheduler()

// schedule runs run at mark + delay (at once when that is past), unless a
// refresh of the document is already pending: then the mark is kept and
// it returns false.
func (s *profileScheduler) schedule(documentID string, mark time.Time, run func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pending[documentID]; ok {
		return false
	}
	due := mark.Add(s.delay)
	wait := max(due.Sub(s.now()), 0)
	e := &profileTimer{due: due, run: run}
	s.pending[documentID] = e
	e.stop = s.after(wait, func() {
		if s.take(documentID, e) {
			run()
		}
	})
	return true
}

// take removes e if it is still the document's pending refresh.
func (s *profileScheduler) take(documentID string, e *profileTimer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending[documentID] != e {
		return false
	}
	delete(s.pending, documentID)
	return true
}

func (s *profileScheduler) cancel(documentID string) {
	s.mu.Lock()
	e := s.pending[documentID]
	delete(s.pending, documentID)
	s.mu.Unlock()
	if e != nil {
		e.stop()
	}
}

// fireNow runs the pending refresh of the document now instead of at its
// mark; false when none is pending.
func (s *profileScheduler) fireNow(documentID string) bool {
	s.mu.Lock()
	e := s.pending[documentID]
	delete(s.pending, documentID)
	s.mu.Unlock()
	if e == nil {
		return false
	}
	e.stop()
	go e.run()
	return true
}

func (s *profileScheduler) due(documentID string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.pending[documentID]
	if !ok {
		return time.Time{}, false
	}
	return e.due, true
}

// ScheduleDocumentProfileRefresh plans the refresh of a target edited
// since its profile: run is called docProfileRefreshDelay after the first
// save the profile does not cover — the save time seen on the row when it
// is newer than the profile, else now (an AI edit records no save time).
// A refresh already pending keeps its mark; false then.
func ScheduleDocumentProfileRefresh(ctx context.Context, ws *types.DocumentWorkspace, run func()) bool {
	if ws == nil || run == nil {
		return false
	}
	mark := docProfileSchedule.now()
	if st := SessionDocumentProfile(ctx, ws.ID); st != nil && ws.LastSavedAt != nil && ws.LastSavedAt.Before(mark) {
		covered := st.StartedAt
		if st.GeneratedAt != nil && st.GeneratedAt.After(covered) {
			covered = *st.GeneratedAt
		}
		if ws.LastSavedAt.After(covered) {
			mark = *ws.LastSavedAt
		}
	}
	return docProfileSchedule.schedule(ws.ID, mark, run)
}

// CancelDocumentProfileRefresh drops a pending refresh (the tab was closed
// or turned into a source); the last profile is kept.
func CancelDocumentProfileRefresh(documentID string) {
	docProfileSchedule.cancel(documentID)
}

// PromoteDocumentProfileRefresh runs a pending refresh now: a chat turn
// found the profile older than the document's last save. false when no
// refresh is pending (one is already running, or none was planned).
func PromoteDocumentProfileRefresh(documentID string) bool {
	return docProfileSchedule.fireNow(documentID)
}
