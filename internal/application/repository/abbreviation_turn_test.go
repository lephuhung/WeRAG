package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// abbreviationTurnRepoRoot locates the repository root from this package
// directory so tests execute the real migration files.
func abbreviationTurnRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	up := filepath.Join(root, "migrations", "sqlite", "000031_abbreviation_turn_states.up.sql")
	require.FileExists(t, up, "real sqlite migration must exist")
	return root
}

// splitMigrationStatements splits a migration file into executable
// statements. The abbreviation migration files contain no triggers or
// embedded SQL semicolons, so filtering comments before splitting on
// semicolons is exact.
func splitMigrationStatements(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	var out []string
	for _, part := range strings.Split(strings.Join(lines, "\n"), ";") {
		if stmt := strings.TrimSpace(part); stmt != "" {
			out = append(out, stmt)
		}
	}
	require.NotEmpty(t, out, "migration must contain statements: %s", path)
	return out
}

// newAbbreviationTurnRepo opens an isolated in-memory SQLite database, runs
// the REAL sqlite migration from the repo root (never bare AutoMigrate for
// the new tables), and seeds the minimal session/message rows foreign keys
// and liveness checks rely on.
func newAbbreviationTurnRepo(t *testing.T) (*gorm.DB, interfaces.AbbreviationTurnRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(
		"file:"+uuid.NewString()+"?mode=memory&cache=shared&_busy_timeout=5000&_fk=1"), &gorm.Config{})
	require.NoError(t, err)
	setupAbbreviationTurnSchema(t, db)
	return db, NewAbbreviationTurnRepository(db)
}

// newAbbreviationTurnRepoWAL opens a file-backed database with the
// deployed SQLite configuration (WAL, busy timeout, enforced FKs) for the
// concurrent CAS test. The repository's reservation-first write protocol
// serializes overlapping writers, so the loser observes the bumped version
// and reports a clean (false, nil) race loss with no test-only lock mode.
func newAbbreviationTurnRepoWAL(t *testing.T) (*gorm.DB, interfaces.AbbreviationTurnRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(
		filepath.Join(t.TempDir(), "turn.db")+"?_journal_mode=WAL&_busy_timeout=10000&_fk=1"), &gorm.Config{})
	require.NoError(t, err)
	setupAbbreviationTurnSchema(t, db)
	return db, NewAbbreviationTurnRepository(db)
}

func setupAbbreviationTurnSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	root := abbreviationTurnRepoRoot(t)
	// Minimal but truthful baseline: real column names/types for the
	// ownership (sessions.user_id VARCHAR(512)) and side (messages.role)
	// checks; no invented identity columns on messages.
	require.NoError(t, db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id VARCHAR(36) PRIMARY KEY, tenant_id INTEGER NOT NULL,
		user_id VARCHAR(512), deleted_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE IF NOT EXISTS messages (
		id VARCHAR(36) PRIMARY KEY, session_id VARCHAR(36) NOT NULL,
		role VARCHAR(50) NOT NULL, deleted_at DATETIME
	)`).Error)
	for _, stmt := range splitMigrationStatements(t, filepath.Join(
		root, "migrations", "sqlite", "000031_abbreviation_turn_states.up.sql")) {
		require.NoError(t, db.Exec(stmt).Error, "migration statement: %s", stmt)
	}
	require.NoError(t, db.Exec(
		`INSERT INTO sessions (id, tenant_id, user_id) VALUES
		('s', 7, 'alice'), ('s-other', 7, 'bob')`).Error)
	require.NoError(t, db.Exec(
		`INSERT INTO messages (id, session_id, role) VALUES
		('u1', 's', 'user'), ('u2', 's', 'user'), ('def-1', 's', 'user'),
		('a1', 's', 'assistant'), ('u-other', 's-other', 'user')`).Error)
}

func turnTestOwner() types.AbbreviationOwner {
	return types.AbbreviationOwner{TenantID: 7, SessionID: "s", OwnerID: "alice", PrincipalID: "web_user:alice"}
}

func turnTestRow(expires time.Time) *types.AbbreviationTurnState {
	return &types.AbbreviationTurnState{
		ID: "r", TenantID: 7, SessionID: "s",
		OwnerID: "alice", PrincipalID: "web_user:alice", RootUserMessageID: "u1",
		State: "inspecting", Version: 1, ExpiresAt: expires,
		Payload: types.AbbreviationTurnPayload{
			Resolution: types.AbbreviationResolution{
				OriginalQuery: "ATTT là gì", Status: "needs_definition",
				UnknownTerms: []string{"ATTT"},
			},
		},
	}
}

func TestAbbreviationTurnCAS(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()

	row := turnTestRow(time.Now().Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, row))
	row.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, owner, 1, row)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.CompareAndSwap(ctx, owner, 1, row)
	require.NoError(t, err)
	require.False(t, ok)

	other := owner
	other.OwnerID = "mallory"
	_, err = repo.Get(ctx, other, "r")
	require.ErrorIs(t, err, types.ErrAbbreviationNotFound)

	got, err := repo.Get(ctx, owner, "r")
	require.NoError(t, err)
	require.Equal(t, "awaiting_definition", got.State)
	require.Equal(t, uint64(2), got.Version)
	require.Equal(t, []string{"ATTT"}, got.Payload.Resolution.UnknownTerms)
}

func TestAbbreviationTurnBeginCancelsPreviousAwaiting(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()

	first := turnTestRow(time.Now().Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, first))
	first.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, owner, 1, first)
	require.NoError(t, err)
	require.True(t, ok)

	second := turnTestRow(time.Now().Add(24 * time.Hour))
	second.ID = "r2"
	second.RootUserMessageID = "u2"
	require.NoError(t, repo.Begin(ctx, owner, second))

	prev, err := repo.Get(ctx, owner, "r")
	require.NoError(t, err)
	require.Equal(t, "cancelled", prev.State)
	require.Equal(t, uint64(3), prev.Version)

	// The same root question cannot begin twice.
	dupe := turnTestRow(time.Now().Add(24 * time.Hour))
	dupe.ID = "r3"
	require.Error(t, repo.Begin(ctx, owner, dupe))
}

func TestAbbreviationTurnTransitions(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()
	fixed := time.Date(2026, 9, 26, 2, 0, 0, 0, time.UTC)
	previous := abbreviationTurnNow
	abbreviationTurnNow = func() time.Time { return fixed }
	t.Cleanup(func() { abbreviationTurnNow = previous })

	row := turnTestRow(fixed.Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, row))

	// Illegal jump: inspecting cannot complete directly.
	bad := turnTestRow(fixed.Add(24 * time.Hour))
	bad.State = "completed"
	_, err := repo.CompareAndSwap(ctx, owner, 1, bad)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection)

	// Expiry cannot be claimed before the deadline.
	row.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, owner, 1, row)
	require.NoError(t, err)
	require.True(t, ok)
	row.State = "expired"
	_, err = repo.CompareAndSwap(ctx, owner, 2, row)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection)

	// Past the deadline the expiry transition lands.
	abbreviationTurnNow = func() time.Time { return fixed.Add(25 * time.Hour) }
	ok, err = repo.CompareAndSwap(ctx, owner, 2, row)
	require.NoError(t, err)
	require.True(t, ok)

	// Terminal states freeze.
	row.State = "cancelled"
	_, err = repo.CompareAndSwap(ctx, owner, 3, row)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection)
}

func TestAbbreviationTurnExpiryPreservedAndAwaiting(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()
	fixed := time.Date(2026, 9, 26, 2, 0, 0, 0, time.UTC)
	previous := abbreviationTurnNow
	abbreviationTurnNow = func() time.Time { return fixed }
	t.Cleanup(func() { abbreviationTurnNow = previous })

	deadline := fixed.Add(24 * time.Hour)
	row := turnTestRow(deadline)
	require.NoError(t, repo.Begin(ctx, owner, row))
	row.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, owner, 1, row)
	require.NoError(t, err)
	require.True(t, ok)

	// A partial reply persists on awaiting_definition without moving the
	// deadline or touching the frozen snapshot and question.
	row.ExpiresAt = fixed.Add(48 * time.Hour)
	row.State = "awaiting_definition"
	ok, err = repo.CompareAndSwap(ctx, owner, 2, row)
	require.NoError(t, err)
	require.True(t, ok)
	got, err := repo.Get(ctx, owner, "r")
	require.NoError(t, err)
	require.True(t, got.ExpiresAt.Equal(deadline), "expiry overwritten: %v", got.ExpiresAt)
	require.Equal(t, "awaiting_definition", got.State)

	// Scope-widening and question-replacing writes conflict.
	widened := turnTestRow(deadline)
	widened.Payload.Snapshot.KnowledgeBaseIDs = []string{"kb-evil"}
	widened.State = "ready"
	_, err = repo.CompareAndSwap(ctx, owner, 3, widened)
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
	renamed := turnTestRow(deadline)
	renamed.Payload.Resolution.OriginalQuery = "câu hỏi khác"
	renamed.State = "ready"
	_, err = repo.CompareAndSwap(ctx, owner, 3, renamed)
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)

	// Partial replies keep the turn resumable via Awaiting.
	live, err := repo.Awaiting(ctx, owner, fixed)
	require.NoError(t, err)
	require.NotNil(t, live, "partially replied turn must still await")
	require.Equal(t, "r", live.ID)

	abbreviationTurnNow = func() time.Time { return fixed }
	row2 := turnTestRow(deadline)
	row2.ID = "r-wait"
	row2.RootUserMessageID = "u2"
	require.NoError(t, repo.Begin(ctx, owner, row2))
	// Inspecting turns never await.
	waiting, err := repo.Awaiting(ctx, owner, fixed)
	require.NoError(t, err)
	require.Nil(t, waiting, "inspecting turn is not awaiting")
	row2.State = "awaiting_definition"
	ok, err = repo.CompareAndSwap(ctx, owner, 1, row2)
	require.NoError(t, err)
	require.True(t, ok)
	waiting, err = repo.Awaiting(ctx, owner, fixed)
	require.NoError(t, err)
	require.NotNil(t, waiting)
	require.Equal(t, "r-wait", waiting.ID)
	expired, err := repo.Awaiting(ctx, owner, deadline.Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, expired, "expired turn must not await")
}

func TestAbbreviationTurnScopedReads(t *testing.T) {
	db, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()

	row := turnTestRow(time.Now().Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, row))

	_, err := repo.Get(ctx, owner, "missing")
	require.ErrorIs(t, err, types.ErrAbbreviationNotFound)

	foreign := owner
	foreign.SessionID = "s-other"
	foreign.OwnerID = "bob"
	foreign.PrincipalID = "web_user:bob"
	_, err = repo.Get(ctx, foreign, "r")
	require.ErrorIs(t, err, types.ErrAbbreviationNotFound)
	live, err := repo.Awaiting(ctx, foreign, time.Now())
	require.NoError(t, err)
	require.Nil(t, live)

	// Soft-deleted root message hides the turn.
	require.NoError(t, db.Exec("UPDATE messages SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'u1'").Error)
	_, err = repo.Get(ctx, owner, "r")
	require.ErrorIs(t, err, types.ErrAbbreviationNotFound)

	// Soft-deleted session hides awaiting turns.
	require.NoError(t, db.Exec("UPDATE messages SET deleted_at = NULL WHERE id = 'u1'").Error)
	require.NoError(t, db.Exec("UPDATE sessions SET deleted_at = CURRENT_TIMESTAMP WHERE id = 's'").Error)
	_, err = repo.Get(ctx, owner, "r")
	require.ErrorIs(t, err, types.ErrAbbreviationNotFound)
	live, err = repo.Awaiting(ctx, owner, time.Now())
	require.NoError(t, err)
	require.Nil(t, live)
}

func TestAbbreviationTurnLinkMessages(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()

	row := turnTestRow(time.Now().Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, row))

	links := []types.AbbreviationMessageLink{
		{MessageID: "u1", RequestID: "r", Role: "root"},
		{MessageID: "def-1", RequestID: "r", Role: "definition"},
	}
	require.NoError(t, repo.LinkMessages(ctx, owner, "r", links))

	resolved, err := repo.ByMessages(ctx, owner, []string{"u1", "def-1", "u2"})
	require.NoError(t, err)
	require.Len(t, resolved, 2)
	require.Equal(t, "r", resolved["u1"].ID)
	require.Equal(t, "r", resolved["def-1"].ID)
	// Linked copies are independent.
	resolved["u1"].State = "MUTATED"
	fresh, err := repo.Get(ctx, owner, "r")
	require.NoError(t, err)
	require.Equal(t, "inspecting", fresh.State)

	// Cross-session messages conflict; missing ones are not found.
	require.ErrorIs(t, repo.LinkMessages(ctx, owner, "r",
		[]types.AbbreviationMessageLink{{MessageID: "u-other", RequestID: "r", Role: "definition"}}),
		types.ErrAbbreviationConflict)
	require.ErrorIs(t, repo.LinkMessages(ctx, owner, "r",
		[]types.AbbreviationMessageLink{{MessageID: "ghost", RequestID: "r", Role: "definition"}}),
		types.ErrAbbreviationNotFound)
	require.ErrorIs(t, repo.LinkMessages(ctx, owner, "r",
		[]types.AbbreviationMessageLink{{MessageID: "u2", RequestID: "other", Role: "definition"}}),
		types.ErrAbbreviationBadSelection)
	require.ErrorIs(t, repo.LinkMessages(ctx, owner, "r",
		[]types.AbbreviationMessageLink{{MessageID: "u2", RequestID: "r", Role: "bogus"}}),
		types.ErrAbbreviationBadSelection)
	// Exact retries are idempotent no-ops after the same checks.
	require.NoError(t, repo.LinkMessages(ctx, owner, "r",
		[]types.AbbreviationMessageLink{{MessageID: "u1", RequestID: "r", Role: "root"}}))
}

func TestAbbreviationTurnCancel(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()

	first := turnTestRow(time.Now().Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, first))
	first.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, owner, 1, first)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.LinkMessages(ctx, owner, "r",
		[]types.AbbreviationMessageLink{{MessageID: "u1", RequestID: "r", Role: "root"}}))

	second := turnTestRow(time.Now().Add(24 * time.Hour))
	second.ID = "r2"
	second.RootUserMessageID = "u2"
	require.NoError(t, repo.Begin(ctx, owner, second))

	// Cancelling by message only touches linked non-terminal turns.
	require.NoError(t, repo.CancelByMessages(ctx, owner, []string{"u1"}))
	got, err := repo.Get(ctx, owner, "r2")
	require.NoError(t, err)
	require.Equal(t, "inspecting", got.State)

	// Completed turns survive cancellation.
	second.State = "ready"
	ok, err = repo.CompareAndSwap(ctx, owner, 1, second)
	require.NoError(t, err)
	require.True(t, ok)
	second.State = "running"
	ok, err = repo.CompareAndSwap(ctx, owner, 2, second)
	require.NoError(t, err)
	require.True(t, ok)
	second.State = "completed"
	ok, err = repo.CompareAndSwap(ctx, owner, 3, second)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, repo.CancelSession(ctx, owner))
	done, err := repo.Get(ctx, owner, "r2")
	require.NoError(t, err)
	require.Equal(t, "completed", done.State)
	require.Equal(t, uint64(4), done.Version)
}

func TestAbbreviationTurnConcurrentCAS(t *testing.T) {
	_, repo := newAbbreviationTurnRepoWAL(t)
	ctx := context.Background()
	owner := turnTestOwner()

	row := turnTestRow(time.Now().Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, row))

	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			attempt := turnTestRow(time.Now().Add(24 * time.Hour))
			attempt.State = "awaiting_definition"
			ok, err := repo.CompareAndSwap(ctx, owner, 1, attempt)
			results[n] = ok
			errs[n] = err
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.True(t, results[0] != results[1], "exactly one CAS must win, got %v", results)

	// Two awaiting turns in one session are blocked by the partial unique.
	var count int64
	require.NoError(t, repo.(*abbreviationTurnRepository).db.
		Table("abbreviation_turn_states").
		Where("tenant_id = 7 AND session_id = 's' AND state = 'awaiting_definition'").
		Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestAbbreviationTurnSecondAwaitingBlockedByUnique(t *testing.T) {
	db, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()

	first := turnTestRow(time.Now().Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, first))
	first.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, owner, 1, first)
	require.NoError(t, err)
	require.True(t, ok)

	// Bypassing Begin's cancellation, a second awaiting row violates the
	// partial unique index at the database level.
	direct := turnTestRow(time.Now().Add(24 * time.Hour))
	direct.ID = "r-sneak"
	direct.RootUserMessageID = "u2"
	direct.State = "awaiting_definition"
	direct.Version = 1
	now := time.Now()
	direct.CreatedAt, direct.UpdatedAt = now, now
	err = db.Create(direct).Error
	require.Error(t, err, "partial unique must block a second awaiting turn")
}

// Begin validates the actual live session, its owner binding, and a live
// user root message before touching any existing awaiting turn.
func TestReviewTurnBeginValidatesActualOwnership(t *testing.T) {
	for _, which := range []string{"wrong-owner", "missing-root", "foreign-root", "assistant-root"} {
		t.Run(which, func(t *testing.T) {
			db, repo := newAbbreviationTurnRepo(t)
			o := turnTestOwner()
			r := turnTestRow(time.Now().Add(time.Hour))
			switch which {
			case "wrong-owner":
				o.OwnerID = "mallory"
				o.PrincipalID = "web_user:mallory"
				r.OwnerID = o.OwnerID
				r.PrincipalID = o.PrincipalID
			case "missing-root":
				r.RootUserMessageID = "absent"
			case "foreign-root":
				r.RootUserMessageID = "u-other"
			case "assistant-root":
				r.RootUserMessageID = "a1"
				_ = db
			}
			if err := repo.Begin(context.Background(), o, r); err == nil {
				t.Fatalf("Begin accepted %s", which)
			}
		})
	}
}

// A failed Begin must not cancel an existing valid awaiting turn.
func TestReviewTurnFailedBeginPreservesAwaiting(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	owner := turnTestOwner()
	first := turnTestRow(time.Now().Add(24 * time.Hour))
	require.NoError(t, repo.Begin(ctx, owner, first))
	first.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, owner, 1, first)
	require.NoError(t, err)
	require.True(t, ok)

	bad := turnTestRow(time.Now().Add(24 * time.Hour))
	bad.ID = "r-bad"
	bad.RootUserMessageID = "absent"
	require.Error(t, repo.Begin(ctx, owner, bad))

	waiting, err := repo.Awaiting(ctx, owner, time.Now())
	require.NoError(t, err)
	require.NotNil(t, waiting, "failed Begin cancelled the valid awaiting turn")
	require.Equal(t, "r", waiting.ID)
	require.Equal(t, uint64(2), waiting.Version)
}

// Partial replies persist on awaiting_definition with fixed expiry;
// blocked turns retry explicitly through inspecting; ready is the only
// claim to running; past-expiry turns only expire.
func TestReviewTurnLifecyclePartialAndBlockedRetry(t *testing.T) {
	for _, which := range []string{"partial", "blocked-retry", "skip-ready", "expired-ready"} {
		t.Run(which, func(t *testing.T) {
			_, repo := newAbbreviationTurnRepo(t)
			ctx := context.Background()
			o := turnTestOwner()
			r := turnTestRow(time.Now().Add(time.Hour))
			require.NoError(t, repo.Begin(ctx, o, r))
			if which == "blocked-retry" {
				r.State = "blocked_error"
			} else {
				r.State = "awaiting_definition"
			}
			ok, err := repo.CompareAndSwap(ctx, o, 1, r)
			require.NoError(t, err)
			require.True(t, ok)
			switch which {
			case "partial":
				r.State = "awaiting_definition"
			case "blocked-retry":
				r.State = "inspecting"
			case "skip-ready":
				r.State = "running"
			case "expired-ready":
				r.State = "ready"
				previous := abbreviationTurnNow
				abbreviationTurnNow = func() time.Time { return r.ExpiresAt.Add(time.Second) }
				defer func() { abbreviationTurnNow = previous }()
			}
			ok, err = repo.CompareAndSwap(ctx, o, 2, r)
			if which == "partial" || which == "blocked-retry" {
				if err != nil || !ok {
					t.Fatalf("required %s failed: %v %v", which, ok, err)
				}
			} else if err == nil && ok {
				t.Fatalf("forbidden %s succeeded", which)
			}
		})
	}
}

// Deleted roots and definitions hide turns from Get/Awaiting/ByMessages and
// block advancement; live links never expose a root-deleted task.
func TestReviewTurnDeletedSourcesHidden(t *testing.T) {
	for _, which := range []string{"root-history", "definition-get", "definition-await", "deleted-cas"} {
		t.Run(which, func(t *testing.T) {
			db, repo := newAbbreviationTurnRepo(t)
			ctx := context.Background()
			o := turnTestOwner()
			r := turnTestRow(time.Now().Add(time.Hour))
			require.NoError(t, repo.Begin(ctx, o, r))
			r.State = "awaiting_definition"
			ok, err := repo.CompareAndSwap(ctx, o, 1, r)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, repo.LinkMessages(ctx, o, "r", []types.AbbreviationMessageLink{
				{MessageID: "u1", RequestID: "r", Role: "root"},
				{MessageID: "def-1", RequestID: "r", Role: "definition"},
			}))
			deleted := "def-1"
			if which == "root-history" || which == "deleted-cas" {
				deleted = "u1"
			}
			require.NoError(t, db.Exec("UPDATE messages SET deleted_at=CURRENT_TIMESTAMP WHERE id=?", deleted).Error)
			switch which {
			case "root-history":
				out, err := repo.ByMessages(ctx, o, []string{"def-1"})
				require.NoError(t, err)
				if len(out) != 0 {
					t.Fatal("ByMessages exposes task with deleted root")
				}
			case "definition-get":
				if out, err := repo.Get(ctx, o, "r"); err == nil && out != nil {
					t.Fatal("Get returns task with deleted definition")
				}
			case "definition-await":
				out, err := repo.Awaiting(ctx, o, time.Now())
				require.NoError(t, err)
				if out != nil {
					t.Fatal("Awaiting returns task with deleted definition")
				}
			case "deleted-cas":
				r.State = "ready"
				if ok, err := repo.CompareAndSwap(ctx, o, 2, r); err == nil && ok {
					t.Fatal("CAS advances task with deleted root")
				}
			}
		})
	}
}

// Sibling ByMessages results never alias nested payload state.
func TestReviewTurnNestedCopyIsolation(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	o := turnTestOwner()
	r := turnTestRow(time.Now().Add(time.Hour))
	require.NoError(t, repo.Begin(ctx, o, r))
	require.NoError(t, repo.LinkMessages(ctx, o, "r", []types.AbbreviationMessageLink{
		{MessageID: "u1", RequestID: "r", Role: "root"},
		{MessageID: "def-1", RequestID: "r", Role: "definition"},
	}))
	out, err := repo.ByMessages(ctx, o, []string{"u1", "def-1"})
	require.NoError(t, err)
	out["u1"].Payload.Resolution.UnknownTerms[0] = "MUTATED"
	if out["def-1"].Payload.Resolution.UnknownTerms[0] == "MUTATED" {
		t.Fatal("ByMessages returned shallow aliased payloads")
	}
}

// The harness enforces foreign keys on every pooled connection.
func TestReviewTurnHarnessEnforcesForeignKeys(t *testing.T) {
	db, _ := newAbbreviationTurnRepo(t)
	var enabled int
	require.NoError(t, db.Raw("PRAGMA foreign_keys").Scan(&enabled).Error)
	if enabled != 1 {
		t.Fatal("repository test harness runs with foreign keys OFF")
	}
}

// Exact link retries are idempotent; conflicting request/role pairs and
// same-batch conflicts fail; the batch rolls back atomically.
func TestReviewTurnLinkIdempotencyAndRollback(t *testing.T) {
	db, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	o := turnTestOwner()
	r := turnTestRow(time.Now().Add(time.Hour))
	require.NoError(t, repo.Begin(ctx, o, r))
	base := []types.AbbreviationMessageLink{
		{MessageID: "u1", RequestID: "r", Role: "root"},
		{MessageID: "def-1", RequestID: "r", Role: "definition"},
	}
	require.NoError(t, repo.LinkMessages(ctx, o, "r", base))
	// Exact retry is a no-op.
	require.NoError(t, repo.LinkMessages(ctx, o, "r", base))
	// Same-batch exact duplicates are fine.
	require.NoError(t, repo.LinkMessages(ctx, o, "r", append(base, base...)))
	// A different request for an existing message conflicts.
	require.ErrorIs(t, repo.LinkMessages(ctx, o, "r",
		[]types.AbbreviationMessageLink{{MessageID: "u1", RequestID: "other", Role: "root"}}),
		types.ErrAbbreviationBadSelection)
	// An incompatible role for an already-linked message conflicts instead
	// of being silently ignored (same user side, different recorded role).
	require.ErrorIs(t, repo.LinkMessages(ctx, o, "r",
		[]types.AbbreviationMessageLink{{MessageID: "u1", RequestID: "r", Role: "definition"}}),
		types.ErrAbbreviationConflict)
	// Mixed-validity batches roll back: nothing from the batch persists.
	mixed := []types.AbbreviationMessageLink{
		{MessageID: "a1", RequestID: "r", Role: "clarification"},
		{MessageID: "ghost", RequestID: "r", Role: "definition"},
	}
	require.Error(t, repo.LinkMessages(ctx, o, "r", mixed))
	var count int64
	require.NoError(t, db.Table("abbreviation_turn_messages").
		Where("message_id = 'a1'").Count(&count).Error)
	require.Zero(t, count, "rolled-back batch leaked a link row")
}

func foreignEvidenceRow(expires time.Time) *types.AbbreviationTurnState {
	r := turnTestRow(expires)
	r.Payload.Resolution.Terms = []types.AbbreviationTerm{{
		ShortForm: "ATTT", Key: "attt", FullForm: "F",
		Source: types.AbbreviationSourceUserCurrent,
		Definition: &types.AbbreviationDefinition{
			ShortForm: "ATTT", FullForm: "F", SourceMessageID: "u-other", Start: 0, End: 1,
		},
	}}
	return r
}

func TestReReviewTurnProposedEvidence(t *testing.T) {
	for _, phase := range []string{"begin", "cas"} {
		t.Run(phase, func(t *testing.T) {
			_, repo := newAbbreviationTurnRepo(t)
			ctx := context.Background()
			o := turnTestOwner()
			r := foreignEvidenceRow(time.Now().Add(time.Hour))
			if phase == "begin" {
				if err := repo.Begin(ctx, o, r); err == nil {
					t.Fatal("Begin stored foreign proposed evidence")
				}
				return
			}
			clean := turnTestRow(time.Now().Add(time.Hour))
			require.NoError(t, repo.Begin(ctx, o, clean))
			r.State = "awaiting_definition"
			if ok, err := repo.CompareAndSwap(ctx, o, 1, r); err == nil && ok {
				t.Fatal("CAS validated only old payload and stored foreign proposed evidence")
			}
		})
	}
}

// A write invalidated between its authorization point and its update rolls
// back: the fault is injected by a GORM callback firing on the write
// statement itself, inside the same transaction.
func TestReReviewTurnWriteAtomicityFaultInjection(t *testing.T) {
	for _, phase := range []string{"begin", "cas"} {
		t.Run(phase, func(t *testing.T) {
			db, repo := newAbbreviationTurnRepo(t)
			ctx := context.Background()
			o := turnTestOwner()
			if phase == "cas" {
				r := turnTestRow(time.Now().Add(24 * time.Hour))
				require.NoError(t, repo.Begin(ctx, o, r))
				r.State = "awaiting_definition"
				ok, err := repo.CompareAndSwap(ctx, o, 1, r)
				require.NoError(t, err)
				require.True(t, ok)
			}
			hook := "turn:re-review-sabotage-" + phase
			sabotage := func(tx *gorm.DB) {
				tx.Exec("UPDATE messages SET deleted_at = CURRENT_TIMESTAMP WHERE id IN ('u1', 'u2')")
			}
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register(hook, sabotage))
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register(hook, sabotage))
			t.Cleanup(func() {
				db.Callback().Create().Remove(hook)
				db.Callback().Update().Remove(hook)
			})
			if phase == "begin" {
				r := turnTestRow(time.Now().Add(24 * time.Hour))
				r.ID = "r-sabotaged"
				r.RootUserMessageID = "u2"
				require.Error(t, repo.Begin(ctx, o, r), "sabotaged Begin must fail")
				_, err := repo.Get(ctx, o, "r-sabotaged")
				require.ErrorIs(t, err, types.ErrAbbreviationNotFound, "sabotaged Begin must roll back")
				return
			}
			r := turnTestRow(time.Now().Add(24 * time.Hour))
			r.State = "ready"
			_, err := repo.CompareAndSwap(ctx, o, 2, r)
			require.Error(t, err, "sabotaged CAS must fail")
			got, err := repo.Get(ctx, o, "r")
			require.NoError(t, db.Exec("UPDATE messages SET deleted_at = NULL WHERE id = 'u1'").Error)
			require.NoError(t, err)
			require.Equal(t, uint64(2), got.Version, "sabotaged CAS must roll back")
			require.Equal(t, "awaiting_definition", got.State)
		})
	}
}

func TestReReviewTurnSourceReadErrorsPropagate(t *testing.T) {
	for _, method := range []string{"get", "awaiting", "by-messages"} {
		t.Run(method, func(t *testing.T) {
			db, repo := newAbbreviationTurnRepo(t)
			ctx := context.Background()
			o := turnTestOwner()
			r := turnTestRow(time.Now().Add(time.Hour))
			require.NoError(t, repo.Begin(ctx, o, r))
			r.State = "awaiting_definition"
			ok, err := repo.CompareAndSwap(ctx, o, 1, r)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, repo.LinkMessages(ctx, o, "r", []types.AbbreviationMessageLink{
				{MessageID: "u1", RequestID: "r", Role: "root"},
			}))
			failure := errors.New("injected source lookup database outage")
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("review:source-failure",
				func(tx *gorm.DB) {
					if tx.Statement.Table == "messages" {
						tx.AddError(failure)
					}
				}))
			t.Cleanup(func() { db.Callback().Query().Remove("review:source-failure") })
			var callErr error
			switch method {
			case "get":
				_, callErr = repo.Get(ctx, o, "r")
			case "awaiting":
				_, callErr = repo.Awaiting(ctx, o, time.Now())
			case "by-messages":
				_, callErr = repo.ByMessages(ctx, o, []string{"u1"})
			}
			if !errors.Is(callErr, failure) {
				t.Fatalf("%s hid database outage as %v", method, callErr)
			}
		})
	}
}

func TestReReviewTurnWaitingTTLNotExecutionTTL(t *testing.T) {
	for _, phase := range []string{"finish-running", "completed-metadata"} {
		t.Run(phase, func(t *testing.T) {
			_, repo := newAbbreviationTurnRepo(t)
			ctx := context.Background()
			o := turnTestOwner()
			r := turnTestRow(time.Now().Add(time.Hour))
			require.NoError(t, repo.Begin(ctx, o, r))
			r.State = "ready"
			ok, err := repo.CompareAndSwap(ctx, o, 1, r)
			require.NoError(t, err)
			require.True(t, ok)
			r.State = "running"
			ok, err = repo.CompareAndSwap(ctx, o, 2, r)
			require.NoError(t, err)
			require.True(t, ok)
			version := uint64(3)
			if phase == "completed-metadata" {
				r.State = "completed"
				ok, err = repo.CompareAndSwap(ctx, o, version, r)
				require.NoError(t, err)
				require.True(t, ok)
				version++
			}
			oldClock := abbreviationTurnNow
			abbreviationTurnNow = func() time.Time { return r.ExpiresAt.Add(time.Second) }
			defer func() { abbreviationTurnNow = oldClock }()
			r.State = "completed"
			ok, err = repo.CompareAndSwap(ctx, o, version, r)
			if err != nil || !ok {
				t.Fatalf("waiting TTL blocked %s: ok=%v err=%v", phase, ok, err)
			}
		})
	}
}

// Expiry edges exist where the lifecycle needs them and nowhere else:
// crashed inspecting/running turns and completed records can expire, while
// cancelled/expired turns never reopen and live continuation still needs a
// live deadline.
func TestReReviewTurnExpiryEdges(t *testing.T) {
	_, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	o := turnTestOwner()
	fixed := time.Date(2026, 9, 26, 2, 0, 0, 0, time.UTC)
	oldClock := abbreviationTurnNow
	abbreviationTurnNow = func() time.Time { return fixed }
	defer func() { abbreviationTurnNow = oldClock }()

	newTurn := func(id, root string) *types.AbbreviationTurnState {
		r := turnTestRow(fixed.Add(time.Hour))
		r.ID = id
		r.RootUserMessageID = root
		return r
	}
	inspecting := newTurn("r-inspect", "u1")
	require.NoError(t, repo.Begin(ctx, o, inspecting))
	abbreviationTurnNow = func() time.Time { return fixed.Add(2 * time.Hour) }
	inspecting.State = "expired"
	ok, err := repo.CompareAndSwap(ctx, o, 1, inspecting)
	require.NoError(t, err)
	require.True(t, ok, "crashed inspecting turn must expire past its deadline")
	// Reopening proposes live lifecycle states, never a repeated expired:
	// both are rejected, past the deadline and back inside it.
	inspecting.State = "ready"
	_, err = repo.CompareAndSwap(ctx, o, 2, inspecting)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection, "expired turn must not resume ready")
	abbreviationTurnNow = func() time.Time { return fixed }
	inspecting.State = "inspecting"
	_, err = repo.CompareAndSwap(ctx, o, 2, inspecting)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection, "expired turn must not reopen")

	running := newTurn("r-run", "u2")
	require.NoError(t, repo.Begin(ctx, o, running))
	running.State = "ready"
	ok, err = repo.CompareAndSwap(ctx, o, 1, running)
	require.NoError(t, err)
	require.True(t, ok)
	running.State = "running"
	ok, err = repo.CompareAndSwap(ctx, o, 2, running)
	require.NoError(t, err)
	require.True(t, ok)
	abbreviationTurnNow = func() time.Time { return fixed.Add(2 * time.Hour) }
	// Execution records never expire: running finalizes, completed keeps
	// metadata recovery, and neither lifecycle is overwritten by expiry.
	running.State = "expired"
	_, err = repo.CompareAndSwap(ctx, o, 3, running)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection, "running must finalize, not expire")
	running.State = "completed"
	ok, err = repo.CompareAndSwap(ctx, o, 3, running)
	require.NoError(t, err)
	require.True(t, ok, "running finalization survives the deadline")
	running.State = "expired"
	_, err = repo.CompareAndSwap(ctx, o, 4, running)
	require.ErrorIs(t, err, types.ErrAbbreviationBadSelection, "completed must not expire")
}

func TestFix2ReviewBeginOwnerInvalidation(t *testing.T) {
	db, repo := newAbbreviationTurnRepo(t)
	ctx := context.Background()
	o := turnTestOwner()
	first := turnTestRow(time.Now().Add(time.Hour))
	require.NoError(t, repo.Begin(ctx, o, first))
	first.State = "awaiting_definition"
	ok, err := repo.CompareAndSwap(ctx, o, 1, first)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("review:owner-change",
		func(tx *gorm.DB) {
			if tx.Statement.Table == "abbreviation_turn_states" {
				tx.AddError(tx.Exec("UPDATE sessions SET user_id='mallory' WHERE id='s'").Error)
			}
		}))
	t.Cleanup(func() { db.Callback().Create().Remove("review:owner-change") })
	second := turnTestRow(time.Now().Add(time.Hour))
	second.ID = "r2"
	second.RootUserMessageID = "u2"
	require.Error(t, repo.Begin(ctx, o, second), "Begin must roll back if session ownership changed at its write boundary")
}

func TestFix2ReviewLinkInvalidation(t *testing.T) {
	for _, target := range []string{"root", "definition"} {
		t.Run(target, func(t *testing.T) {
			db, repo := newAbbreviationTurnRepo(t)
			ctx := context.Background()
			o := turnTestOwner()
			r := turnTestRow(time.Now().Add(time.Hour))
			require.NoError(t, repo.Begin(ctx, o, r))
			id := "u1"
			if target == "definition" {
				id = "def-1"
			}
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("review:source-change",
				func(tx *gorm.DB) {
					if tx.Statement.Table == "abbreviation_turn_messages" {
						tx.AddError(tx.Exec("UPDATE messages SET deleted_at=CURRENT_TIMESTAMP WHERE id=?", id).Error)
					}
				}))
			t.Cleanup(func() { db.Callback().Create().Remove("review:source-change") })
			require.Error(t, repo.LinkMessages(ctx, o, "r", []types.AbbreviationMessageLink{
				{MessageID: "def-1", RequestID: "r", Role: "definition"},
			}), "link source/root invalidation must roll back")
		})
	}
}

func TestFix2ReviewProductionSQLiteCAS(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "turn.db")+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(2)
	t.Cleanup(func() { raw.Close() })
	setupAbbreviationTurnSchema(t, db)
	repo := NewAbbreviationTurnRepository(db)
	ctx := context.Background()
	o := turnTestOwner()
	r := turnTestRow(time.Now().Add(time.Hour))
	require.NoError(t, repo.Begin(ctx, o, r))
	var reads int32
	barrier := make(chan struct{})
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("review:overlap",
		func(tx *gorm.DB) {
			if tx.Statement.Table == "abbreviation_turn_states" {
				n := atomic.AddInt32(&reads, 1)
				if n == 2 {
					close(barrier)
				}
				if n <= 2 {
					select {
					case <-barrier:
					case <-time.After(200 * time.Millisecond):
					}
				}
			}
		}))
	t.Cleanup(func() { db.Callback().Query().Remove("review:overlap") })
	var wg sync.WaitGroup
	wins := make([]bool, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			next := *r
			next.State = "awaiting_definition"
			wins[n], errs[n] = repo.CompareAndSwap(ctx, o, 1, &next)
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0], "CAS must support deployed SQLite configuration")
	require.NoError(t, errs[1], "stale loser must not be a lock-upgrade error")
	require.NotEqual(t, wins[0], wins[1])
}

func TestAbbreviationTurnDependencyMessageIDs(t *testing.T) {
	stored := turnTestRow(time.Now().Add(time.Hour))
	stored.Payload.Resolution.Terms = []types.AbbreviationTerm{{Source: types.AbbreviationSourceUserCurrent, Definition: &types.AbbreviationDefinition{SourceMessageID: "stored-only"}}}
	proposed := turnTestRow(stored.ExpiresAt)
	proposed.Payload.Resolution.Terms = []types.AbbreviationTerm{{Source: types.AbbreviationSourceUserCurrent, Definition: &types.AbbreviationDefinition{SourceMessageID: "proposed-only"}}}
	proposed.ClarificationMessageID = "a1"
	links := []types.AbbreviationMessageLink{{MessageID: "def-1", Role: types.AbbreviationMessageRoleDefinition}}
	batch := []types.AbbreviationMessageLink{
		{MessageID: "u2", Role: types.AbbreviationMessageRoleDefinition},
		{MessageID: "new-answer", Role: types.AbbreviationMessageRoleAnswer},
		{MessageID: "new-clarification", Role: types.AbbreviationMessageRoleClarification},
	}
	require.Equal(t, []string{"a1", "def-1", "new-answer", "new-clarification", "proposed-only", "stored-only", "u1", "u2"},
		dependencyMessageIDs(stored, links, proposed, batch))
	require.Equal(t, []string{"u1"}, dependencyMessageIDs(nil, nil, turnTestRow(stored.ExpiresAt), nil))
}
