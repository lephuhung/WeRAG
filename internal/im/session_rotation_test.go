package im

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// rotationSessions continues sessions with fresh IDs and serves them back.
type rotationSessions struct {
	interfaces.SessionService
	continued int
}

func (r *rotationSessions) ContinueSession(_ context.Context, old *types.Session) (*types.Session, error) {
	r.continued++
	return &types.Session{ID: fmt.Sprintf("%s-next-%d", old.ID, r.continued), TenantID: old.TenantID}, nil
}

func (r *rotationSessions) GetSession(_ context.Context, id string) (*types.Session, error) {
	return &types.Session{ID: id, TenantID: 1}, nil
}

func newRotationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:im-rotation-%d?mode=memory&cache=shared", time.Now().UnixNano())),
		&gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// minimal schemas: the production ones use PostgreSQL defaults
	for _, stmt := range []string{
		`CREATE TABLE im_channel_sessions (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			platform TEXT NOT NULL, user_id TEXT NOT NULL, chat_id TEXT NOT NULL DEFAULT '',
			thread_id TEXT NOT NULL DEFAULT '', session_id TEXT NOT NULL, tenant_id INTEGER NOT NULL,
			agent_id TEXT DEFAULT '', im_channel_id TEXT DEFAULT '', status TEXT NOT NULL DEFAULT 'active',
			metadata TEXT DEFAULT '{}', created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, deleted_at DATETIME)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return db
}

func reachedPreflight(sessionID string) imUsagePreflight {
	features := types.UsagePlanFeatures{SessionMaxTurns: 15}
	return imUsagePreflight{grant: &types.UsageTurnGrant{
		Features: features,
		Session:  types.NewUsageSessionStatus(features, sessionID, 15, 0),
	}}
}

func liveMappings(t *testing.T, db *gorm.DB) []ChannelSession {
	t.Helper()
	var rows []ChannelSession
	if err := db.Where("deleted_at IS NULL").Find(&rows).Error; err != nil {
		t.Fatalf("list mappings: %v", err)
	}
	return rows
}

// Two messages of one chat that both saw the cap: the first continues the
// session, the second finds the mapping already retired, drops its own
// continuation and runs in the first one's. One live mapping remains.
func TestRotateCappedSessionConcurrentMessagesMakeOneContinuation(t *testing.T) {
	db := newRotationTestDB(t)
	sessions := &rotationSessions{}
	svc := &Service{db: db, sessionService: sessions}
	if err := db.Exec(`INSERT INTO sessions (id) VALUES ('s1'), ('s1-next-1'), ('s1-next-2')`).Error; err != nil {
		t.Fatal(err)
	}
	stale := &ChannelSession{ID: "m1", Platform: "test", UserID: "u", ChatID: "c", SessionID: "s1", TenantID: 1, AgentID: "a"}
	if err := db.Create(stale).Error; err != nil {
		t.Fatal(err)
	}
	old := &types.Session{ID: "s1", TenantID: 1}
	ctx := context.Background()

	// both messages resolved the same mapping before either rotated
	firstMapping, first, firstPre := svc.rotateCappedSession(ctx, stale, old, reachedPreflight("s1"))
	secondMapping, second, secondPre := svc.rotateCappedSession(ctx, stale, old, reachedPreflight("s1"))

	if first.ID != "s1-next-1" {
		t.Fatalf("first message should continue the session, got %s", first.ID)
	}
	if second.ID != first.ID || secondMapping.ID != firstMapping.ID {
		t.Fatalf("second message must run in the first continuation, got session %s mapping %s (want %s %s)",
			second.ID, secondMapping.ID, first.ID, firstMapping.ID)
	}
	if live := liveMappings(t, db); len(live) != 1 || live[0].SessionID != "s1-next-1" {
		t.Fatalf("want one live mapping to s1-next-1, got %+v", live)
	}
	var losing types.Session
	if err := db.Unscoped().Where("id = ?", "s1-next-2").First(&losing).Error; err != nil {
		t.Fatal(err)
	}
	if !losing.DeletedAt.Valid {
		t.Fatal("the losing continuation must be deleted")
	}
	for _, pre := range []imUsagePreflight{firstPre, secondPre} {
		if pre.grant.Session.Reached || pre.grant.Session.Turns != 0 {
			t.Fatalf("the continuation starts unused: %+v", pre.grant.Session)
		}
	}
}

type countingGuard struct{ calls int }

func (g *countingGuard) CheckTurn(context.Context, uint64, string, string, types.SessionCapMode) (*types.UsageTurnGrant, error) {
	g.calls++
	return &types.UsageTurnGrant{}, nil
}
func (g *countingGuard) Entitlement(context.Context, uint64, string) (*types.UsageEntitlement, error) {
	return nil, nil
}
func (g *countingGuard) Enforced(context.Context) bool { return true }
func (g *countingGuard) PreviewSession(
	context.Context, uint64, string, string, types.SessionCapMode,
) (*types.UsageSessionPreview, error) {
	return &types.UsageSessionPreview{}, nil
}

// The QA worker reuses the check HandleMessage made: one guard call per
// message, not two.
func TestCheckUsageReusesThePreflight(t *testing.T) {
	guard := &countingGuard{}
	svc := &Service{usageGuard: guard}
	ctx := context.Background()

	pre := svc.preflightUsage(ctx, "s1")
	got, err := svc.checkUsage(withUsagePreflight(ctx, pre), "s1")
	if err != nil || guard.calls != 1 || types.UsageTurnGrantFromContext(got) == nil {
		t.Fatalf("want one guard call and a grant on ctx, got calls=%d err=%v", guard.calls, err)
	}
	// a path that skipped HandleMessage still checks
	if _, err := svc.checkUsage(ctx, "s1"); err != nil || guard.calls != 2 {
		t.Fatalf("want a fresh check without a preflight, got calls=%d err=%v", guard.calls, err)
	}
}

// A session under its cap is left alone.
func TestRotateCappedSessionLeavesUncappedSession(t *testing.T) {
	svc := &Service{sessionService: &rotationSessions{}}
	features := types.UsagePlanFeatures{SessionMaxTurns: 15}
	pre := imUsagePreflight{grant: &types.UsageTurnGrant{
		Features: features, Session: types.NewUsageSessionStatus(features, "s1", 3, 0),
	}}
	cs := &ChannelSession{ID: "m1"}
	gotCS, got, _ := svc.rotateCappedSession(context.Background(), cs, &types.Session{ID: "s1"}, pre)
	if gotCS != cs || got.ID != "s1" {
		t.Fatalf("uncapped session must not rotate: %+v %+v", gotCS, got)
	}
}
