package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

type replaceFixture struct {
	db   *gorm.DB
	know *feedKnowledgeDeleting
	feed *LegalFeedService
	now  time.Time
}

func newReplaceFixture(t *testing.T) *replaceFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeBase{}, &types.LegalDocument{},
		&types.LegalDocumentRequest{}, &types.LegalRequestRequester{}, &types.LegalDocumentLink{}))
	require.NoError(t, db.Exec(`CREATE TABLE legal_feed_log (id INTEGER PRIMARY KEY, action TEXT, vbpl_id TEXT, knowledge_id TEXT,
		knowledge_base_id TEXT, document_number TEXT, reason TEXT, actor_endpoint_id TEXT, actor_tenant_id INTEGER, detail TEXT,
		created_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE chunk_revisions (knowledge_id TEXT, chunk_id TEXT)`).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", Name: "VBPL"}).Error)
	require.NoError(t, db.Create(&[]types.LegalDocument{{ID: "9", Source: "vbpl", DocumentNumber: "9/2026/NĐ-CP"},
		{ID: "tvpl-5", Source: "tvpl", DocumentNumber: "5/2026/NĐ-CP"}}).Error)
	know := &feedKnowledgeDeleting{db: db}
	feed := NewLegalFeedService(db, know, repository.NewLegalRequestRepository(db))
	return &replaceFixture{db: db, know: know, feed: feed, now: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)}
}

// copy stores a public copy of doc; replaces "" for none.
func (f *replaceFixture) copy(t *testing.T, id, doc, parse, enable, replaces, errMsg string, age time.Duration) {
	t.Helper()
	meta := map[string]string{"external_id": doc}
	if replaces != "" {
		meta[types.LegalReplacesKnowledgeKey] = replaces
	}
	m, _ := json.Marshal(meta)
	at := f.now.Add(-age)
	require.NoError(t, f.db.Create(&types.Knowledge{ID: id, KnowledgeBaseID: "kb", Type: "file", Title: id, FileName: id + ".html",
		FileHash: "h-" + id, ParseStatus: parse, EnableStatus: enable, ErrorMessage: errMsg, Metadata: m}).Error)
	require.NoError(t, f.db.Exec(`UPDATE knowledges SET created_at = ?, updated_at = ? WHERE id = ?`, at, at, id).Error)
}

func (f *replaceFixture) request(t *testing.T, id, doc, knowledgeID, detail string) {
	t.Helper()
	r := types.LegalDocumentRequest{ID: id, NumberKey: id, LegalDocumentID: doc, Status: types.LegalRequestAdded, KnowledgeID: knowledgeID,
		DecidedBy: legalAutoIngestActor, FirstRequestedAt: f.now, LastRequestedAt: f.now}
	if detail != "" {
		r.Detail = types.JSON(detail)
	}
	require.NoError(t, f.db.Create(&r).Error)
}

func (f *replaceFixture) req(t *testing.T, id string) (types.LegalDocumentRequest, map[string]any) {
	t.Helper()
	var r types.LegalDocumentRequest
	require.NoError(t, f.db.Take(&r, "id = ?", id).Error)
	var d map[string]any
	_ = json.Unmarshal(r.Detail, &d)
	return r, d
}

func (f *replaceFixture) logged(t *testing.T, action string) []string {
	t.Helper()
	var rows []string
	require.NoError(t, f.db.Raw(`SELECT detail FROM legal_feed_log WHERE action = ? ORDER BY id`, action).Scan(&rows).Error)
	return rows
}

func (f *replaceFixture) alive(t *testing.T, id string) bool {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Raw(`SELECT COUNT(*) FROM knowledges WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n).Error)
	return n == 1
}

// After a restart between the parse and the swap, the sweep finishes it;
// a copy parsed a moment ago is left to its own hook.
func TestLegalSweepFinishesInterruptedSwaps(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "new", "9", types.ParseStatusProcessing, "enabled", "old", "", time.Hour)
	f.copy(t, "old2", "tvpl-5", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "fresh", "tvpl-5", types.ParseStatusCompleted, "enabled", "old2", "", time.Minute)

	finished, cleaned, lastErr := f.feed.SweepIngests(ctx, f.now, 5)
	require.Empty(t, lastErr)
	require.Equal(t, 1, finished)
	require.Zero(t, cleaned)
	require.False(t, f.alive(t, "old"))
	require.True(t, f.alive(t, "old2"), "parsed a minute ago: its own hook swaps it")
	require.Len(t, f.logged(t, legalReplacedAction), 1)

	// the hook of a parse does it at once
	f.feed.LegalReplaceAfterParse(ctx, "fresh")
	require.False(t, f.alive(t, "old2"))
}

// A replacement whose parse fails: the old copy stays readable, the failed
// one is deleted and logged, the request comes back to the worker; after
// three failures the worker gives up on it, visibly.
func TestLegalSweepCleansAFailedReplacement(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "new", "9", types.ParseStatusFailed, "disabled", "old", "docreader: timeout", time.Hour)
	f.request(t, "r9", "9", "new", "")

	_, cleaned, lastErr := f.feed.SweepIngests(ctx, f.now, 5)
	require.Empty(t, lastErr)
	require.Equal(t, 1, cleaned)
	require.True(t, f.alive(t, "old"), "the old copy stays readable")
	require.False(t, f.alive(t, "new"))
	logs := f.logged(t, legalReplaceFailedAction)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0], "docreader: timeout")
	r, d := f.req(t, "r9")
	require.Equal(t, types.LegalRequestAutoIngest, r.Status, "back in the worker's queue")
	require.Empty(t, r.KnowledgeID)
	require.EqualValues(t, 1, d[types.LegalRequestDetailParseFailures])
	require.Nil(t, d[types.LegalRequestDetailAutoIngestSkip])
	require.False(t, legalAutoIngestGivenUp(&r))

	// the third failure: given up, shown in the queue with why
	f.copy(t, "new3", "9", types.ParseStatusFailed, "disabled", "old", "", time.Hour)
	require.NoError(t, f.db.Exec(`UPDATE legal_document_requests SET status = 'added', knowledge_id = 'new3',
		detail = '{"parse_failures":2}' WHERE id = 'r9'`).Error)
	_, cleaned, _ = f.feed.SweepIngests(ctx, f.now, 5)
	require.Equal(t, 1, cleaned)
	r, d = f.req(t, "r9")
	require.EqualValues(t, 3, d[types.LegalRequestDetailParseFailures])
	require.Contains(t, d[types.LegalRequestDetailAutoIngestSkip], "thất bại 3 lần")
	require.True(t, legalAutoIngestGivenUp(&r), "the worker does not try it again")
	require.True(t, f.alive(t, "old"))
}

// A first ingest whose parse failed is cleaned and retried too; a feed's
// document goes back to the operators. A replacement stuck parsing is given
// up after six hours. A failed copy no request names is left to a person.
func TestLegalSweepCleansFailedFirstCopiesAndStuckOnes(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	f.copy(t, "first", "tvpl-5", types.ParseStatusFailed, "disabled", "", "OCR failed", time.Hour)
	f.request(t, "r5", "tvpl-5", "first", "")
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "stuck", "9", types.ParseStatusProcessing, "disabled", "old", "", 7*time.Hour)
	f.copy(t, "young", "9", types.ParseStatusProcessing, "disabled", "old", "", time.Hour)
	f.copy(t, "manual", "9", types.ParseStatusFailed, "disabled", "", "x", time.Hour)

	_, cleaned, lastErr := f.feed.SweepIngests(ctx, f.now, 5)
	require.Empty(t, lastErr)
	require.Equal(t, 2, cleaned)
	require.False(t, f.alive(t, "first"))
	require.Len(t, f.logged(t, legalIngestFailedAction), 1)
	r, _ := f.req(t, "r5")
	require.Equal(t, types.LegalRequestRequested, r.Status, "a feed's text: back to the operators")
	require.False(t, f.alive(t, "stuck"))
	require.Contains(t, f.logged(t, legalReplaceFailedAction)[0], "chưa phân tích xong")
	require.True(t, f.alive(t, "young"), "still within six hours")
	require.True(t, f.alive(t, "manual"), "no request names it: a person decides")
	require.True(t, f.alive(t, "old"))
}

// While a replacement parses, the old copy is the one read; a newer text
// meanwhile supersedes the in-flight copy and keeps the old one.
func TestLegalReplacementInFlight(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "k-new", "9", types.ParseStatusProcessing, "disabled", "old", "", time.Minute)

	push := NewLegalPushService(f.db, nil, nil, nil, nil)
	held, err := push.heldDoc(ctx, "9")
	require.NoError(t, err)
	require.Equal(t, "old", held.KnowledgeID, "the readable old copy is read until the swap")
	require.True(t, held.Readable())
	// both parsed (the instant before the swap): still the old one
	require.NoError(t, f.db.Exec(`UPDATE knowledges SET enable_status = 'enabled' WHERE id = 'k-new'`).Error)
	held, _ = push.heldDoc(ctx, "9")
	require.Equal(t, "old", held.KnowledgeID)
	ingested, err := repository.NewLegalRequestRepository(f.db).PublicKnowledgeByExternalIDs(ctx, []string{"9"})
	require.NoError(t, err)
	require.True(t, ingested["9"])

	// a newer text while k-new waits for its swap
	require.NoError(t, f.db.Exec(`UPDATE knowledges SET enable_status = 'disabled' WHERE id = 'k-new'`).Error)
	res, err := f.feed.IngestHTML(ctx, LegalIngestDocument{VBPLID: "9", DocumentNumber: "9/2026/NĐ-CP", FileName: "9.html",
		Metadata: map[string]string{"external_id": "9"}}, []byte("<p>bản mới hơn</p>"), &types.KnowledgeBase{ID: "kb"},
		LegalFeedActor{UserID: "u"}, "r")
	require.NoError(t, err)
	require.Equal(t, "replaced", res.Result)
	require.Equal(t, "old", res.ReplacedKnowledgeID)
	require.Equal(t, []string{"k-new"}, f.know.deleted, "the in-flight copy is superseded, the old one kept")
	require.Equal(t, "old", f.know.lastMeta[types.LegalReplacesKnowledgeKey])
	require.True(t, f.alive(t, "old"))
	require.Len(t, f.logged(t, legalReplaceStartedAction), 1)
}

// A reviewer's decision on the old copy is the new copy's after the swap,
// also when it was made while the new copy was parsing.
func TestLegalSwapCarriesTheReviewersDecision(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	reviewed := f.now
	reviews := &fakeReviewLinks{links: map[string]*types.LegalDocumentLink{
		"old": {KnowledgeID: "old", KnowledgeBaseID: "kb", LegalDocumentID: "9", ReviewStatus: types.LegalReviewRejected,
			ReviewedBy: "u-rev", ReviewedAt: &reviewed},
		"new": {KnowledgeID: "new", KnowledgeBaseID: "kb", LegalDocumentID: "9", ReviewStatus: types.LegalReviewApproved,
			ReviewedBy: types.LegalReviewerVBPL, ReviewedAt: &reviewed},
	}}
	f.feed.Reviews = reviews
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "new", "9", types.ParseStatusCompleted, "enabled", "old", "", time.Minute)
	done, err := f.feed.FinishReplacement(ctx, "new")
	require.NoError(t, err)
	require.True(t, done)
	l := reviews.links["new"]
	require.Equal(t, types.LegalReviewRejected, l.ReviewStatus)
	require.Equal(t, "u-rev", l.ReviewedBy)
	require.Equal(t, "new", l.KnowledgeID)
}

// A marker naming another document's copy, or a copy in another KB, deletes
// nothing: the refusal is logged and the marker cleared.
func TestLegalSwapNeverDeletesAnotherDocument(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	require.NoError(t, f.db.Create(&types.KnowledgeBase{ID: "kb2", Name: "other"}).Error)
	f.copy(t, "other-doc", "tvpl-5", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "same", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	require.NoError(t, f.db.Exec(`UPDATE knowledges SET knowledge_base_id = 'kb2' WHERE id = 'same'`).Error)
	f.copy(t, "new", "9", types.ParseStatusCompleted, "enabled", "other-doc,same", "", time.Minute)

	done, err := f.feed.FinishReplacement(ctx, "new")
	require.NoError(t, err)
	require.True(t, done, "the marker is cleared, so the sweep does not loop")
	require.Empty(t, f.know.deleted)
	require.True(t, f.alive(t, "other-doc"))
	require.True(t, f.alive(t, "same"))
	refused := f.logged(t, legalReplaceRefusedAction)
	require.Len(t, refused, 2)
	require.Contains(t, refused[0], `"old_external_id":"tvpl-5"`)
	require.Contains(t, refused[1], `"old_knowledge_base_id":"kb2"`)
	var meta string
	require.NoError(t, f.db.Raw(`SELECT metadata FROM knowledges WHERE id = 'new'`).Row().Scan(&meta))
	require.NotContains(t, meta, types.LegalReplacesKnowledgeKey)
}

// The worker's run counts the sweep's work, also on a pass that tried no
// document.
func TestLegalAutoIngestRecordsTheSweep(t *testing.T) {
	f := newReplaceFixture(t)
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "new", "9", types.ParseStatusCompleted, "enabled", "old", "", time.Hour)
	require.NoError(t, f.db.Exec(`UPDATE knowledges SET updated_at = ? WHERE id = 'new'`, time.Now().Add(-time.Hour)).Error)
	runs := &fakeRuns{}
	w := &LegalAutoIngest{requests: repository.NewLegalRequestRepository(f.db), feed: f.feed, runs: runs}
	require.Equal(t, 0, w.Tick(context.Background()))
	require.False(t, f.alive(t, "old"))
	require.Len(t, runs.runs, 1)
	require.Equal(t, 1, runs.runs[0].summary[legalAutoIngestSwapped])
	require.Equal(t, 0, runs.runs[0].summary["tried"])
	require.Equal(t, types.LegalSyncOK, runs.runs[0].status)
}

func (f *replaceFixture) link(t *testing.T, knowledgeID, doc string, status types.LegalReviewStatus, by string) {
	t.Helper()
	at := f.now
	require.NoError(t, f.db.Create(&types.LegalDocumentLink{KnowledgeID: knowledgeID, KnowledgeBaseID: "kb", LegalDocumentID: doc,
		ReviewStatus: status, ReviewedBy: by, ReviewedAt: &at}).Error)
}

func (f *replaceFixture) links(t *testing.T) map[string]types.LegalDocumentLink {
	t.Helper()
	var rows []types.LegalDocumentLink
	require.NoError(t, f.db.Find(&rows).Error)
	out := map[string]types.LegalDocumentLink{}
	for _, l := range rows {
		out[l.KnowledgeID] = l
	}
	return out
}

// The swap drops the link of the copy it deletes (no link to a deleted
// knowledge is left behind); the new copy keeps its own, carrying the
// reviewer's decision.
func TestLegalSwapDropsTheReplacedCopysLink(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	f.feed.Reviews = repository.NewLegalReviewRepository(f.db)
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "new", "9", types.ParseStatusCompleted, "enabled", "old", "", time.Minute)
	f.link(t, "old", "9", types.LegalReviewApproved, "u-rev")
	f.link(t, "new", "9", types.LegalReviewApproved, types.LegalReviewerVBPL)

	done, err := f.feed.FinishReplacement(ctx, "new")
	require.NoError(t, err)
	require.True(t, done)
	require.False(t, f.alive(t, "old"))
	links := f.links(t)
	require.Len(t, links, 1)
	require.Equal(t, "u-rev", links["new"].ReviewedBy, "the reviewer's decision carried over first")
	require.Equal(t, types.LegalReviewApproved, links["new"].ReviewStatus)
}

// A new copy without a link (its ingest could not write one) is given the
// old copy's before that one goes: the document stays linked. Without a way
// to give it one, the old link is kept rather than leaving none.
func TestLegalSwapNeverLeavesTheDocumentUnlinked(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	f.feed.Reviews = repository.NewLegalReviewRepository(f.db)
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "new", "9", types.ParseStatusCompleted, "enabled", "old", "", time.Minute)
	f.link(t, "old", "9", types.LegalReviewApproved, types.LegalReviewerVBPL)

	done, err := f.feed.FinishReplacement(ctx, "new")
	require.NoError(t, err)
	require.True(t, done)
	links := f.links(t)
	require.Len(t, links, 1)
	require.Equal(t, "9", links["new"].LegalDocumentID)
	require.Equal(t, types.LegalReviewApproved, links["new"].ReviewStatus)

	g := newReplaceFixture(t) // no review repository: nothing to give the new copy
	g.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	g.copy(t, "new", "9", types.ParseStatusCompleted, "enabled", "old", "", time.Minute)
	g.link(t, "old", "9", types.LegalReviewApproved, types.LegalReviewerVBPL)
	done, err = g.feed.FinishReplacement(ctx, "new")
	require.NoError(t, err)
	require.True(t, done)
	require.Contains(t, g.links(t), "old", "no link for the new copy: the old one is not dropped")
}

// A failed new copy goes with the link its ingest gave it; the old copy and
// its link stay.
func TestLegalSweepDropsTheFailedCopysLink(t *testing.T) {
	f := newReplaceFixture(t)
	ctx := context.Background()
	f.copy(t, "old", "9", types.ParseStatusCompleted, "enabled", "", "", 48*time.Hour)
	f.copy(t, "new", "9", types.ParseStatusFailed, "disabled", "old", "docreader: timeout", time.Hour)
	f.request(t, "r9", "9", "new", "")
	f.link(t, "old", "9", types.LegalReviewApproved, "u-rev")
	f.link(t, "new", "9", types.LegalReviewApproved, types.LegalReviewerVBPL)

	_, cleaned, lastErr := f.feed.SweepIngests(ctx, f.now, 5)
	require.Empty(t, lastErr)
	require.Equal(t, 1, cleaned)
	require.False(t, f.alive(t, "new"))
	links := f.links(t)
	require.Len(t, links, 1)
	require.Equal(t, "u-rev", links["old"].ReviewedBy)
}
