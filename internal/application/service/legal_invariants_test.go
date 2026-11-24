package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// invariantSettings answers the worker's settings from a map.
type invariantSettings map[string]string

func (s invariantSettings) GetInt(_ context.Context, key, _ string, def int64) int64 {
	if v, ok := s[key]; ok {
		var n int64
		if err := json.Unmarshal([]byte(v), &n); err == nil {
			return n
		}
	}
	return def
}

func (s invariantSettings) GetBool(_ context.Context, _, _ string, def bool) bool { return def }

func (s invariantSettings) GetString(_ context.Context, key, _ string, def string) string {
	if v, ok := s[key]; ok {
		return v
	}
	return def
}

type legalInvariantsEnv struct {
	t  *testing.T
	db *gorm.DB
	w  *LegalInvariants
}

// newLegalInvariantsEnv: the legal tables, the public KB "kb-tt" (and a
// tenant KB the check ignores), no runs.
func newLegalInvariantsEnv(t *testing.T, settings invariantSettings) *legalInvariantsEnv {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalSyncRun{}, &types.LegalDocument{}, &types.LegalDocumentRelation{},
		&types.LegalDocumentLink{}, &types.LegalProvisionLink{}, &types.LegalProvisionLinkCandidate{},
		&types.LegalDocumentRequest{}))
	for _, stmt := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, name TEXT, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, title TEXT, parse_status TEXT,
			updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY, knowledge_id TEXT, deleted_at DATETIME)`,
		`INSERT INTO knowledge_bases VALUES ('kb-tt', 'Kho VBPL – Thông tư', 0, NULL), ('kb-own', 'Kho riêng', 7, NULL)`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	var s legalSyncSettings
	if settings != nil {
		s = settings
	}
	return &legalInvariantsEnv{t: t, db: db, w: NewLegalInvariants(db, repository.NewLegalSyncRunRepository(db), s)}
}

func (e *legalInvariantsEnv) exec(sql string, args ...any) {
	e.t.Helper()
	require.NoError(e.t, e.db.Exec(sql, args...).Error)
}

func (e *legalInvariantsEnv) doc(id, source string) {
	e.t.Helper()
	require.NoError(e.t, e.db.Create(&types.LegalDocument{ID: id, Source: source, DocumentNumber: id, NumberKey: id}).Error)
}

func (e *legalInvariantsEnv) rel(from, to, label string) {
	e.t.Helper()
	require.NoError(e.t, e.db.Create(&types.LegalDocumentRelation{FromID: from, ToID: to,
		Relation: types.LegalRelationReferences, SourceLabel: label}).Error)
}

// copy adds a public-KB knowledge in status, changed ago, with an approved
// link to doc ("" none) and one chunk when chunked.
func (e *legalInvariantsEnv) copy(id, kb, status string, ago time.Duration, doc string, chunked bool) {
	e.t.Helper()
	e.exec(`INSERT INTO knowledges (id, knowledge_base_id, title, parse_status, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, kb, "Văn bản "+id, status, time.Now().Add(-ago))
	if doc != "" {
		require.NoError(e.t, e.db.Create(&types.LegalDocumentLink{KnowledgeID: id, KnowledgeBaseID: kb,
			LegalDocumentID: doc, ReviewStatus: types.LegalReviewApproved}).Error)
	}
	if chunked {
		e.exec(`INSERT INTO chunks (id, knowledge_id) VALUES (?, ?)`, uuid.NewString(), id)
	}
}

func (e *legalInvariantsEnv) run(trigger string) *types.LegalSyncRun {
	e.t.Helper()
	r, err := e.w.Run(context.Background(), trigger)
	require.NoError(e.t, err)
	return r
}

// seed: 3 vbpl docs, 1 Party doc, 1 tvpl row; 30 vbpl list edges, 2 text
// edges from vbpl, 1 from the Party doc; 3 readable approved copies, one in
// progress, one stuck, one failed; provision links and candidates; open
// requests.
func (e *legalInvariantsEnv) seed() {
	e.t.Helper()
	for _, id := range []string{"v1", "v2", "v3"} {
		e.doc(id, "vbpl")
	}
	e.doc("d1", "dangcongsan")
	e.doc("t1", "tvpl")
	for i := 0; i < 30; i++ {
		e.rel("v1", "x"+uuid.NewString()[:8], "Văn bản được hướng dẫn")
	}
	e.rel("v1", "v2", types.LegalRelationSourceText)
	e.rel("v2", "v3", types.LegalRelationSourceText)
	e.rel("d1", "v1", types.LegalRelationSourceText)
	e.copy("k1", "kb-tt", types.ParseStatusCompleted, time.Hour, "v1", true)
	e.copy("k2", "kb-tt", types.ParseStatusCompleted, time.Hour, "v2", true)
	e.copy("k3", "kb-tt", types.ParseStatusFinalizing, time.Hour, "v3", true)
	e.copy("k4", "kb-tt", types.ParseStatusProcessing, time.Hour, "", false)
	e.copy("k5", "kb-tt", types.ParseStatusPending, 7*time.Hour, "", false)
	e.copy("k6", "kb-tt", types.ParseStatusFailed, time.Hour, "", false)
	// a tenant KB's copies are not counted
	e.copy("own", "kb-own", types.ParseStatusFailed, time.Hour, "", false)
	for i, src := range []string{"vbpl_editor", "vbpl_editor", "tvpl"} {
		require.NoError(e.t, e.db.Create(&types.LegalProvisionLink{TargetDocID: "v1", TargetProvisionID: "p" + string(rune('a'+i)),
			RelationCode: 1, Op: "amend", SourceProvisionID: "s", Source: src}).Error)
	}
	require.NoError(e.t, e.db.Create(&types.LegalProvisionLinkCandidate{Feed: "tvpl", FeedDocID: "f", TargetDocID: "v1",
		RelationCode: 1, Op: "amend", MatchStatus: "matched", ReviewStatus: "pending"}).Error)
	now := time.Now()
	for i, r := range []struct{ status, source string }{
		{types.LegalRequestRequested, "vbpl_no_text"}, {types.LegalRequestRequested, "vbpl_no_text"},
		{types.LegalRequestAutoIngest, ""}, {types.LegalRequestAdded, "catalogue"},
	} {
		require.NoError(e.t, e.db.Create(&types.LegalDocumentRequest{ID: uuid.NewString(), NumberKey: "n" + string(rune('a'+i)),
			Status: r.status, Source: r.source, FirstRequestedAt: now, LastRequestedAt: now}).Error)
	}
}

func TestLegalInvariantsSnapshot(t *testing.T) {
	e := newLegalInvariantsEnv(t, nil)
	e.seed()
	// a copy that kept its link after the delete, a link to no catalogue
	// row, an approved completed copy without chunks
	e.copy("gone", "kb-tt", types.ParseStatusCompleted, time.Hour, "v1", true)
	e.exec(`UPDATE knowledges SET deleted_at = ? WHERE id = 'gone'`, time.Now())
	e.copy("k7", "kb-tt", types.ParseStatusCompleted, time.Hour, "nowhere", false)

	s, err := LegalInvariantsSnapshot(context.Background(), e.db, time.Now())
	require.NoError(t, err)
	c := s.Counts
	require.EqualValues(t, 30, c["relations.list"])
	require.EqualValues(t, 2, c["relations.text.vbpl"])
	require.EqualValues(t, 1, c["relations.text.dangcongsan"])
	require.EqualValues(t, 0, c["relations.text.tvpl"], "known sources are always there")
	require.EqualValues(t, 30, c["relations.orphan"], "list edges to documents not in the catalogue")
	require.EqualValues(t, 3, c["catalogue.vbpl"])
	require.EqualValues(t, 1, c["catalogue.dangcongsan"])
	require.EqualValues(t, 1, c["catalogue.tvpl"])
	require.EqualValues(t, 0, c["catalogue.manual"])
	require.Equal(t, map[string]string{"kb-tt": "Kho VBPL – Thông tư"}, s.KBs)
	require.EqualValues(t, 4, c["kb.kb-tt.readable"])
	require.EqualValues(t, 2, c["kb.kb-tt.in_progress"])
	require.EqualValues(t, 1, c["kb.kb-tt.failed"])
	require.EqualValues(t, 4, c["kb.kb-tt.approved_links"], "the deleted copy's link is not live")
	require.EqualValues(t, 1, c["copies.failed"])
	require.Equal(t, []LegalInvariantItem{{ID: "k6", Title: "Văn bản k6"}}, s.FailedCopies)
	require.EqualValues(t, 1, c["copies.stuck"])
	require.Equal(t, "k5", s.StuckCopies[0].ID)
	require.EqualValues(t, 1, c["copies.no_chunks"])
	require.EqualValues(t, 1, c["links.dangling_knowledge"])
	require.Equal(t, []string{"gone"}, s.DanglingKnowledge)
	require.EqualValues(t, 1, c["links.dangling_catalogue"])
	require.Equal(t, []string{"k7"}, s.DanglingCatalogue)
	require.EqualValues(t, 0, c["links.kb_mismatch"])
	require.EqualValues(t, 3, c["provisions.total"])
	require.EqualValues(t, 2, c["provisions.vbpl_editor"])
	require.EqualValues(t, 1, c["provisions.tvpl"])
	require.EqualValues(t, 0, c["provisions.orphan"])
	require.EqualValues(t, 1, c["candidates.pending"])
	require.EqualValues(t, 2, c["requests.requested.vbpl_no_text"])
	require.EqualValues(t, 1, c["requests.auto_ingest.none"])
	_, closed := c["requests.added.catalogue"]
	require.False(t, closed, "closed requests are not counted")

	// the summary stays small
	raw, err := json.Marshal(s)
	require.NoError(t, err)
	require.Less(t, len(raw), 4096)
}

func TestLegalInvariantRules(t *testing.T) {
	limits := legalInvariantLimits{dropPercent: 5, dropMinRows: 20}
	snap := func(counts map[string]int64) *LegalInvariantSnapshot {
		return &LegalInvariantSnapshot{Counts: counts, KBs: map[string]string{"kb-tt": "Kho VBPL – Thông tư"}}
	}
	// no previous snapshot: only the absolutes
	require.Empty(t, legalInvariantRules(snap(map[string]int64{"relations.list": 10}), nil, limits))

	prev := snap(map[string]int64{"relations.list": 132000, "relations.text.dangcongsan": 644,
		"kb.kb-tt.readable": 6845, "catalogue.vbpl": 400, "provisions.total": 1000, "kb.kb-tt.in_progress": 500,
		"requests.requested.vbpl_no_text": 900})
	cases := []struct {
		name string
		cur  map[string]int64
		want []string
	}{
		{"steady", map[string]int64{"relations.list": 132000, "relations.text.dangcongsan": 644,
			"kb.kb-tt.readable": 6845, "catalogue.vbpl": 400, "provisions.total": 1000}, nil},
		{"class to zero, over and under the thresholds; queues never", map[string]int64{
			"relations.list":             131990, // -10: under both
			"relations.text.dangcongsan": 0,      // to zero
			"kb.kb-tt.readable":          6100,   // -745, 10.9 %
			"catalogue.vbpl":             370,    // -30 rows but 7.5 %: over both
			"provisions.total":           960,    // -40 rows, 4 %: under the percentage
		}, []string{
			"Quan hệ đọc từ toàn văn (văn bản Đảng): 644 → 0 (mất 644)",
			"Kho VBPL – Thông tư: văn bản đọc được 6.845 → 6.100 (giảm 745, 10,9 %)",
			"Danh mục văn bản (vbpl.vn): 400 → 370 (giảm 30, 7,5 %)",
		}},
		{"a vanished class counts as 0", map[string]int64{"relations.text.dangcongsan": 644,
			"kb.kb-tt.readable": 6845, "catalogue.vbpl": 400, "provisions.total": 1000},
			[]string{"Quan hệ theo danh sách vbpl.vn: 132.000 → 0 (mất 132.000)"}},
	}
	for _, c := range cases {
		require.Equal(t, c.want, legalInvariantRules(snap(c.cur), prev, limits), c.name)
	}

	// absolutes, most severe first; links to deleted copies are leftovers
	// of normal work: their count is no rule
	cur := snap(map[string]int64{"copies.failed": 3, "copies.stuck": 1, "links.dangling_knowledge": 242,
		"links.dangling_catalogue": 2})
	cur.FailedCopies = []LegalInvariantItem{{ID: "a", Title: "Thông tư 1"}, {ID: "b"}, {ID: "c", Title: "Nghị định 2"}}
	cur.StuckCopies = []LegalInvariantItem{{ID: "s", Title: "Luật 3"}}
	cur.DanglingKnowledge = []string{"k1", "k2"}
	cur.DanglingCatalogue = []string{"c1", "c2"}
	lines := legalInvariantRules(cur, nil, limits)
	require.Equal(t, []string{
		"2 liên kết đã duyệt trỏ tới văn bản không có trong danh mục (mã bản sao): c1, c2",
		"3 văn bản tách đoạn lỗi: «Thông tư 1», «b», «Nghị định 2»",
		"1 văn bản đang xử lý quá 6 giờ: «Luật 3»",
	}, lines)
	text := legalInvariantErrorText(lines)
	require.Equal(t, "3 vi phạm, nặng nhất: 2 liên kết đã duyệt trỏ tới văn bản không có trong danh mục (mã bản sao): c1, c2",
		legalSyncErrorGist(text), "the alert shows the gist")

	// their growth is: over 50 new ones since the previous check
	dPrev := snap(map[string]int64{"links.dangling_knowledge": 242})
	require.Empty(t, legalInvariantRules(snap(map[string]int64{"links.dangling_knowledge": 292}), dPrev, limits), "+50: tens a night")
	require.Empty(t, legalInvariantRules(snap(map[string]int64{"links.dangling_knowledge": 10}), dPrev, limits), "fewer is fine")
	require.Equal(t, []string{"Liên kết đã duyệt trỏ tới toàn văn đã xóa: 242 → 900 (thêm 658)"},
		legalInvariantRules(snap(map[string]int64{"links.dangling_knowledge": 900}), dPrev, limits))
	require.Equal(t, "x", legalInvariantErrorText([]string{"x"}))
	// a long list is cut in the message
	require.Equal(t, "a, b, c, d, e, …", legalInvariantList([]string{"a", "b", "c", "d", "e", "f"}, 40))
}

// The first check records a baseline; a lost class fails the next one; a
// change that is kept alerts once and is the baseline after.
func TestLegalInvariantsRunBaselineThenAlertOnce(t *testing.T) {
	e := newLegalInvariantsEnv(t, nil)
	e.seed()
	// absolutes off for this test: no failed or stuck copies
	e.exec(`DELETE FROM knowledges WHERE id IN ('k5', 'k6')`)

	first := e.run(legalInvariantsTriggerManual)
	require.Equal(t, types.LegalSyncOK, first.Status, first.Error)
	var sum map[string]any
	require.NoError(t, json.Unmarshal(first.Summary, &sum))
	require.Equal(t, true, sum["baseline"])
	require.Equal(t, "manual", sum["trigger"])
	require.Equal(t, "30 4 * * *", sum["schedule"])
	require.Nil(t, sum["previous"])

	// an old importer wipes the text relations
	e.exec(`DELETE FROM legal_document_relations WHERE source_label = ?`, types.LegalRelationSourceText)
	second := e.run(legalInvariantsTriggerManual)
	require.Equal(t, types.LegalSyncFailed, second.Status)
	require.Equal(t, legalInvariantsStep, second.Step)
	require.Equal(t, "Quan hệ đọc từ toàn văn (vbpl.vn): 2 → 0 (mất 2)\n"+
		"Quan hệ đọc từ toàn văn (văn bản Đảng): 1 → 0 (mất 1)\n"+
		"2 vi phạm, nặng nhất: Quan hệ đọc từ toàn văn (vbpl.vn): 2 → 0 (mất 2)", second.Error)
	sum = nil
	require.NoError(t, json.Unmarshal(second.Summary, &sum))
	prev := sum["previous"].(map[string]any)
	require.EqualValues(t, first.ID, prev["run_id"])
	require.EqualValues(t, 2, prev["counts"].(map[string]any)["relations.text.vbpl"], "the page shows the delta")
	require.EqualValues(t, 2, sum["violations"])
	raw, _ := json.Marshal(sum)
	require.Less(t, len(raw), 8192)

	// nothing came back, but the failed run is the baseline now
	third := e.run(legalInvariantsTriggerManual)
	require.Equal(t, types.LegalSyncOK, third.Status, third.Error)

	// a snapshot that could not be taken fails the run without one; the
	// next run compares with the last snapshot
	e.exec(`ALTER TABLE legal_provision_link_candidates RENAME TO c_off`)
	broken := e.run(legalInvariantsTriggerManual)
	require.Equal(t, types.LegalSyncFailed, broken.Status)
	require.True(t, strings.HasPrefix(broken.Error, "Không đọc được số liệu: candidates"), broken.Error)
	require.False(t, legalInvariantsHasSnapshot(broken))
	e.exec(`ALTER TABLE c_off RENAME TO legal_provision_link_candidates`)
	e.exec(`DELETE FROM legal_document_relations`)
	fourth := e.run(legalInvariantsTriggerManual)
	require.Equal(t, types.LegalSyncFailed, fourth.Status)
	require.Contains(t, fourth.Error, "Quan hệ theo danh sách vbpl.vn: 30 → 0 (mất 30)")
	sum = nil
	require.NoError(t, json.Unmarshal(fourth.Summary, &sum))
	require.EqualValues(t, third.ID, sum["previous"].(map[string]any)["run_id"])
}

func TestLegalInvariantsDue(t *testing.T) {
	vn := legalInvariantsZone()
	day := func(h, m int) time.Time { return time.Date(2026, 11, 24, h, m, 0, 0, vn) }
	sched := func(at time.Time, status string, snapshot bool) *types.LegalSyncRun {
		sum := `{"trigger":"schedule"}`
		if snapshot {
			sum = `{"trigger":"schedule","snapshot":{"counts":{}}}`
		}
		return &types.LegalSyncRun{Status: status, StartedAt: at, HeartbeatAt: &at, Summary: types.JSON(sum)}
	}
	require.False(t, legalInvariantsDue(day(4, 29), 4, 30, nil), "not before the time")
	require.True(t, legalInvariantsDue(day(4, 30), 4, 30, nil))
	require.True(t, legalInvariantsDue(day(11, 0), 4, 30, nil), "a backend down at 04:30 runs when it is back")
	require.True(t, legalInvariantsDue(day(4, 30).UTC(), 4, 30, nil), "the Vietnam day, whatever the zone")
	require.False(t, legalInvariantsDue(day(9, 0), 4, 30, []*types.LegalSyncRun{sched(day(4, 31), types.LegalSyncOK, true)}))
	require.False(t, legalInvariantsDue(day(9, 0), 4, 30, []*types.LegalSyncRun{sched(day(4, 31), types.LegalSyncFailed, true)}),
		"a failed check with a snapshot is the day's check")
	manual := &types.LegalSyncRun{Status: types.LegalSyncOK, StartedAt: day(3, 0), Summary: types.JSON(`{"trigger":"manual","snapshot":{"counts":{}}}`)}
	require.True(t, legalInvariantsDue(day(4, 30), 4, 30, []*types.LegalSyncRun{manual}), "a manual run does not count")
	require.False(t, legalInvariantsDue(day(4, 40), 4, 30, []*types.LegalSyncRun{sched(day(4, 35), types.LegalSyncRunning, false)}),
		"in progress elsewhere")
	require.True(t, legalInvariantsDue(day(5, 30), 4, 30, []*types.LegalSyncRun{sched(day(4, 35), types.LegalSyncRunning, false)}),
		"left running by a restart: retried")
	noSnap := []*types.LegalSyncRun{sched(day(4, 31), types.LegalSyncFailed, false), sched(day(4, 41), types.LegalSyncFailed, false)}
	require.True(t, legalInvariantsDue(day(5, 0), 4, 30, noSnap))
	noSnap = append(noSnap, sched(day(4, 51), types.LegalSyncFailed, false))
	require.False(t, legalInvariantsDue(day(5, 0), 4, 30, noSnap), "three tries a day")
}

// The scheduled tick runs once per Vietnam day, across a restart (a new
// worker over the same database), after a manual run too; the worker and
// its runs share one injected clock.
func TestLegalInvariantsTickOncePerDay(t *testing.T) {
	e := newLegalInvariantsEnv(t, invariantSettings{})
	e.seed()
	ctx := context.Background()
	vn := legalInvariantsZone()
	clock := time.Date(2026, 11, 24, 3, 0, 0, 0, vn)
	now := func() time.Time { return clock }
	worker := func() *LegalInvariants {
		w := NewLegalInvariants(e.db, repository.NewLegalSyncRunRepository(e.db).WithClock(now), e.w.settings)
		w.now = now
		return w
	}
	e.w = worker()
	count := func() int64 {
		var n int64
		require.NoError(t, e.db.Model(&types.LegalSyncRun{}).Where("worker = ?", types.LegalSyncWorkerInvariants).Count(&n).Error)
		return n
	}
	require.False(t, e.w.Tick(ctx), "not before 04:30")
	e.run(legalInvariantsTriggerManual) // 03:00, manual
	clock = time.Date(2026, 11, 24, 4, 30, 0, 0, vn)
	require.True(t, e.w.Tick(ctx), "a manual run does not stand for the scheduled one")
	clock = clock.Add(10 * time.Minute)
	require.False(t, e.w.Tick(ctx))
	clock = time.Date(2026, 11, 24, 23, 59, 0, 0, vn)
	require.False(t, worker().Tick(ctx), "a restart does not repeat the day")
	require.EqualValues(t, 2, count())
	// a manual run on a day that has one records another row
	e.run(legalInvariantsTriggerManual)
	require.EqualValues(t, 3, count())
	// the next Vietnam day (17:00 UTC is midnight in Vietnam): once again,
	// by a worker restarted after the time
	clock = time.Date(2026, 11, 24, 17, 0, 0, 0, time.UTC)
	require.False(t, worker().Tick(ctx), "00:00 of the 25th, before 04:30")
	clock = time.Date(2026, 11, 25, 9, 0, 0, 0, vn)
	require.True(t, worker().Tick(ctx), "a backend down at 04:30 runs when it is back")
	require.False(t, worker().Tick(ctx))
	require.EqualValues(t, 4, count())

	// a bad time of day falls back to 04:30; the thresholds read the settings
	e.w.settings = invariantSettings{types.LegalInvariantsRunAtSettingKey: "25:99",
		types.LegalInvariantsDropPercentSettingKey: "10", types.LegalInvariantsDropMinRowsSettingKey: "0"}
	h, m := e.w.runAt(ctx)
	require.Equal(t, []int{4, 30}, []int{h, m})
	require.Equal(t, legalInvariantLimits{dropPercent: 10, dropMinRows: 0}, e.w.limits(ctx))
}

// The monitor watches the check through its runs and the last_failed alert
// names it, its step and the gist.
func TestLegalInvariantsMonitorAndAlert(t *testing.T) {
	sc, ok := legalSyncScheduleOf(types.LegalSyncWorkerInvariants)
	require.True(t, ok)
	require.Equal(t, legalSyncSchedule{staleAfter: 26 * time.Hour, longRun: time.Hour}, sc)
	require.False(t, LegalSyncReportingWorkers[types.LegalSyncWorkerInvariants], "it writes its own runs")
	ws, unknown := types.ParseLegalSyncWorkerList("invariants")
	require.Empty(t, unknown)
	require.True(t, ws[types.LegalSyncWorkerInvariants], "its alert can be muted on its own")
	_, err := (&LegalSyncMonitor{}).StartReportedRun(context.Background(), types.LegalSyncWorkerInvariants, "audit", nil)
	require.ErrorIs(t, err, ErrLegalSyncReportWorker, "no API key can forge its runs")

	e := newLegalSyncAlertEnv(t)
	started := e.now.Add(-3 * time.Hour)
	end := started.Add(2 * time.Second)
	require.NoError(t, e.db.Create(&types.LegalSyncRun{Worker: types.LegalSyncWorkerInvariants, Status: types.LegalSyncFailed,
		Step: legalInvariantsStep, StartedAt: started, FinishedAt: &end, HeartbeatAt: &end,
		Summary: types.JSON(`{"trigger":"schedule","schedule":"30 4 * * *"}`),
		Error: "Quan hệ đọc từ toàn văn (văn bản Đảng): 644 → 0 (mất 644)\n3 văn bản tách đoạn lỗi: «a»\n" +
			"2 vi phạm, nặng nhất: Quan hệ đọc từ toàn văn (văn bản Đảng): 644 → 0 (mất 644)"}).Error)
	e.check(t)
	n, p := e.alert(t, "sa1")
	require.NotNil(t, n)
	require.Equal(t, "invariants:last_failed", p.Signature)
	require.Contains(t, n.Body, "Kiểm tra dữ liệu: lỗi ở bước đối chiếu số liệu với lần trước")
	require.Contains(t, n.Body, "— 2 vi phạm, nặng nhất: Quan hệ đọc từ toàn văn (văn bản Đảng): 644 → 0 (mất 644)")
}

// LEGAL_INVARIANTS_SQL=1 go test ./internal/application/service/ -run TestLegalInvariantsPrintSQL -v
// prints the snapshot's Postgres queries, to run read-only on the live DB.
func TestLegalInvariantsPrintSQL(t *testing.T) {
	qs := LegalInvariantsSQL(time.Now())
	require.NotEmpty(t, qs)
	require.Contains(t, strings.Join(qs, "\n"), "LEFT JOIN LATERAL")
	if os.Getenv("LEGAL_INVARIANTS_SQL") == "" {
		return
	}
	for _, q := range qs {
		t.Log("\n" + q)
	}
}
