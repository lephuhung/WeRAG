package tools

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

const profileReplyJSON = `{"gist":"Quyết định ban hành kế hoạch","key_points":["Ban hành kế hoạch","Hiệu lực từ ngày ký"],
"sections":[{"n":1,"summary":"Các căn cứ"}],"topics":["y tế"],"typical_questions":["Kế hoạch có hiệu lực khi nào?","Ai thi hành?"]}`

// profileChat answers every thinking-off call with reply and counts them.
type profileChat struct {
	chatBase
	mu    sync.Mutex
	calls int
	reply string
}

func (f *profileChat) Chat(_ context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return &types.ChatResponse{Content: f.reply}, nil
}

func (f *profileChat) GetModelName() string { return "qwen-test" }

func (f *profileChat) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// freshDocProfiles gives the test empty profile states, results and
// schedule, restored afterwards.
func freshDocProfiles(t *testing.T) {
	t.Helper()
	prevStore, prevSchedule := docProfiles, docProfileSchedule
	docProfiles = &docProfileStore{results: map[string]*types.DocumentProfile{}}
	docProfileSchedule = newProfileScheduler()
	t.Cleanup(func() { docProfiles, docProfileSchedule = prevStore, prevSchedule })
}

// waitProfile waits for the document's background profile job to end.
func waitProfile(t *testing.T, documentID string) *types.DocumentProfile {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, queued := docProfiles.queued.Load(documentID)
		if st := SessionDocumentProfile(toolCtx(), documentID); st != nil && !st.InProgress() && !queued {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("profile of %s did not finish", documentID)
	return nil
}

func TestDocumentProfileStateRoundTripWithoutRedis(t *testing.T) {
	freshDocProfiles(t)
	ws := &types.DocumentWorkspace{ID: "ws-p", Revision: 1, SaveCount: 2}
	if start, refresh := DocumentProfileNeed(toolCtx(), ws); !start || refresh {
		t.Fatal("a document without a profile needs one")
	}
	st := &types.DocumentProfile{Status: types.DocumentProfileReady, Gist: "g", TextHash: "h", Role: types.DocumentWorkspaceRoleTarget, Revision: 1, SaveCount: 2}
	docProfiles.storeState(toolCtx(), ws.ID, st)
	st.Gist = "changed after store"
	got := SessionDocumentProfile(toolCtx(), ws.ID)
	if got == nil || got.Gist != "g" || got.TextHash != "h" {
		t.Fatalf("state round trip: %+v", got)
	}
	if start, refresh := DocumentProfileNeed(toolCtx(), ws); start || refresh {
		t.Fatal("a current profile needs nothing")
	}
	ws.SaveCount = 3
	if start, refresh := DocumentProfileNeed(toolCtx(), ws); start || !refresh {
		t.Fatal("an edited target plans a refresh")
	}
	src := *ws
	src.Role, src.TextStatus = types.DocumentWorkspaceRoleSource, types.DocumentSourceTextReady
	if start, _ := DocumentProfileNeed(toolCtx(), &src); !start {
		t.Fatal("a document that changed role is profiled again")
	}
	src.TextStatus = types.DocumentSourceTextProcessing
	if start, refresh := DocumentProfileNeed(toolCtx(), &src); start || refresh {
		t.Fatal("a source still being read waits")
	}
	// a run that died long ago is not shown as running
	docProfiles.storeState(toolCtx(), "ws-dead", &types.DocumentProfile{Status: types.DocumentProfileRunning, StartedAt: time.Now().Add(-time.Hour)})
	if SessionDocumentProfile(toolCtx(), "ws-dead") != nil {
		t.Fatal("a dead run without a profile counts as none")
	}
}

func TestDocumentProfilerMakesAndKeepsTheProfile(t *testing.T) {
	freshDocProfiles(t)
	f := fakeWorkspaceWithID(quyetDinhFixture(t), "ws-prof")
	model := &profileChat{reply: profileReplyJSON}
	p := NewDocumentProfiler(f, model, "s-1")
	ws, _ := f.Get(toolCtx(), 7, "s-1", "ws-prof")
	p.Start(toolCtx(), ws)
	st := waitProfile(t, "ws-prof")
	if st.Status != types.DocumentProfileReady || st.DocumentNumber != "1234/QĐ-SYT" || st.Gist == "" || st.TextHash == "" ||
		st.Model != "qwen-test" || st.Revision != 3 || model.count() != 1 {
		t.Fatalf("profile: %+v (calls %d)", st, model.count())
	}

	// a formatting-only save: same text, kept without a model call
	f.ws.SaveCount++
	p.Refresh(toolCtx(), "ws-prof")
	if st := SessionDocumentProfile(toolCtx(), "ws-prof"); st.SaveCount != f.ws.SaveCount || st.Status != types.DocumentProfileReady || model.count() != 1 {
		t.Fatalf("unchanged text must keep the profile: %+v (calls %d)", st, model.count())
	}
	// the same text in another document reuses the cached result
	g := fakeWorkspaceWithID(quyetDinhFixture(t), "ws-copy")
	gws, _ := g.Get(toolCtx(), 7, "s-1", "ws-copy")
	NewDocumentProfiler(g, model, "s-1").Start(toolCtx(), gws)
	if st := waitProfile(t, "ws-copy"); st.Status != types.DocumentProfileReady || model.count() != 1 {
		t.Fatalf("cached by text hash: %+v (calls %d)", st, model.count())
	}

	// a text change makes it again; the old profile stays shown meanwhile
	f.content = testCongVan(t, nd30Margins)
	f.ws.Revision++
	p.Refresh(toolCtx(), "ws-prof")
	st = waitProfile(t, "ws-prof")
	if st.Revision != f.ws.Revision || st.DocumentNumber != "12/SNV-VP" || model.count() != 2 {
		t.Fatalf("changed text must be profiled again: %+v (calls %d)", st, model.count())
	}
}

func TestDocumentProfilerWithoutModelFails(t *testing.T) {
	freshDocProfiles(t)
	f := fakeWorkspaceWithID(quyetDinhFixture(t), "ws-nomodel")
	ws, _ := f.Get(toolCtx(), 7, "s-1", "ws-nomodel")
	NewDocumentProfiler(f, nil, "s-1").Start(toolCtx(), ws)
	if st := waitProfile(t, "ws-nomodel"); st.Status != types.DocumentProfileFailed || st.Error != "no model" {
		t.Fatalf("no model: %+v", st)
	}
	if start, _ := DocumentProfileNeed(toolCtx(), ws); start {
		t.Fatal("a failure is not retried at once")
	}
}

func TestDocumentProfilerProfilesASource(t *testing.T) {
	freshDocProfiles(t)
	f := newSourceWorkspace(t)
	model := &profileChat{reply: `{"gist":"Báo cáo ngân sách","entities":{"figures":["1.250 tỷ đồng thu ngân sách (Chương I)"]}}`}
	ws, _ := f.Get(toolCtx(), 7, "s-1", "ws-src")
	NewDocumentProfiler(f, model, "s-1").Start(toolCtx(), ws)
	st := waitProfile(t, "ws-src")
	if st.Status != types.DocumentProfileReady || st.Unit != types.DocumentProfileUnitChunk || len(st.Sections) != 2 ||
		st.Sections[1].Title != "Chương II" || st.Sections[1].From != 1 || st.Entities == nil || st.Role != types.DocumentWorkspaceRoleSource {
		t.Fatalf("source profile: %+v", st)
	}
}

func TestFormatCheckRunMakesTheProfileFirst(t *testing.T) {
	freshDocProfiles(t)
	f := fakeWorkspaceWithID(quyetDinhFixture(t), "ws-fc-prof")
	model := &profileChat{reply: profileReplyJSON}
	tool := NewCheckDocumentFormatToolForWorkspace(f, &fakeChat{reply: `{"labels":{}}`}, "s-1").ForDocument("ws-fc-prof").
		WithProfiler(NewDocumentProfiler(f, model, "s-1"))
	content, name, rev, err := tool.source(toolCtx(), 7, "ws-fc-prof")
	if err != nil {
		t.Fatal(err)
	}
	tool.runBackground(toolCtx(), content, name, rev)
	if st := SessionDocumentProfile(toolCtx(), "ws-fc-prof"); st == nil || st.Status != types.DocumentProfileReady || model.count() != 1 {
		t.Fatalf("the background check makes the profile before its evaluation: %+v", st)
	}
}

// manualTimers replaces the scheduler's clock and timers.
type manualTimers struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
	fires []func()
}

func (m *manualTimers) install(s *profileScheduler) {
	s.now = func() time.Time { m.mu.Lock(); defer m.mu.Unlock(); return m.now }
	s.after = func(d time.Duration, f func()) func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.waits = append(m.waits, d)
		m.fires = append(m.fires, f)
		return func() bool { return true }
	}
}

func TestDocumentProfileRefreshScheduleKeepsItsMark(t *testing.T) {
	freshDocProfiles(t)
	clock := &manualTimers{now: time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)}
	clock.install(docProfileSchedule)
	generated := clock.now.Add(-time.Hour)
	docProfiles.storeState(toolCtx(), "ws-s", &types.DocumentProfile{Status: types.DocumentProfileReady, TextHash: "h", GeneratedAt: &generated, StartedAt: generated})

	saved := clock.now.Add(-time.Minute) // first save after the profile, seen a minute later
	ws := &types.DocumentWorkspace{ID: "ws-s", LastSavedAt: &saved}
	runs := 0
	if !ScheduleDocumentProfileRefresh(toolCtx(), ws, func() { runs++ }) {
		t.Fatal("the first unprocessed save plans a refresh")
	}
	if due, _ := docProfileSchedule.due("ws-s"); !due.Equal(saved.Add(docProfileRefreshDelay)) || clock.waits[0] != 4*time.Minute {
		t.Fatalf("due %v after a wait of %v; want 5 minutes after the save", due, clock.waits[0])
	}
	// more saves inside the window do not move the mark
	later := clock.now.Add(2 * time.Minute)
	ws.LastSavedAt = &later
	clock.now = clock.now.Add(3 * time.Minute)
	if ScheduleDocumentProfileRefresh(toolCtx(), ws, func() { runs += 10 }) {
		t.Fatal("a pending refresh keeps its mark")
	}
	if due, _ := docProfileSchedule.due("ws-s"); !due.Equal(saved.Add(docProfileRefreshDelay)) {
		t.Fatalf("mark moved to %v", due)
	}
	clock.fires[0]()
	if runs != 1 {
		t.Fatalf("the refresh runs once at its mark, ran %d", runs)
	}
	if _, pending := docProfileSchedule.due("ws-s"); pending {
		t.Fatal("a fired refresh is no longer pending")
	}

	// a chat turn runs a pending refresh now; closing the tab cancels it
	done := make(chan struct{})
	ScheduleDocumentProfileRefresh(toolCtx(), ws, func() { close(done) })
	if !PromoteDocumentProfileRefresh("ws-s") {
		t.Fatal("a pending refresh can be run now")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("promoted refresh did not run")
	}
	ScheduleDocumentProfileRefresh(toolCtx(), ws, func() { runs += 100 })
	CancelDocumentProfileRefresh("ws-s")
	clock.fires[len(clock.fires)-1]()
	if runs != 1 || PromoteDocumentProfileRefresh("ws-s") {
		t.Fatalf("a cancelled refresh must not run (runs %d)", runs)
	}
}
