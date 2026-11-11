package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// imScopeChannelSession mirrors the im_channel_sessions columns the session
// queries read (kept local: no import of internal/im).
type imScopeChannelSession struct {
	ID          string `gorm:"primaryKey"`
	SessionID   string
	Platform    string
	ChatID      string
	ThreadID    string
	UserID      string
	AgentID     string
	IMChannelID string `gorm:"column:im_channel_id"`
	TenantID    uint64
	DeletedAt   gorm.DeletedAt
}

func (imScopeChannelSession) TableName() string { return "im_channel_sessions" }

type imScopeMessageRepo struct {
	interfaces.MessageRepository
	cleared []string
}

func (r *imScopeMessageRepo) GetKnowledgeIDsBySessionID(context.Context, string) ([]string, error) {
	return nil, nil
}

func (r *imScopeMessageRepo) GetLatestContextCheckpoint(context.Context, string) (*types.Message, error) {
	return nil, nil
}

func (r *imScopeMessageRepo) ListMessagesBySessionBeforeCursor(
	context.Context, string, time.Time, string, int,
) ([]*types.Message, error) {
	return nil, nil
}

func (r *imScopeMessageRepo) DeleteMessagesBySessionID(_ context.Context, sessionID string) error {
	r.cleared = append(r.cleared, sessionID)
	return nil
}

type imScopeFixture struct {
	db       *gorm.DB
	sessions *sessionService
	messages *messageService
	msgRepo  *imScopeMessageRepo
	own      string // alice's web session
	legacy   string // ownerless web row (legacy design: still everyone's)
	telegram string // ownerless IM session
}

func newIMScopeFixture(t *testing.T) *imScopeFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Session{}, &types.ForkSnapshotLease{}, &types.DocumentWorkspace{}, &imScopeChannelSession{}))
	repo := repository.NewSessionRepository(db)
	msgRepo := &imScopeMessageRepo{}
	f := &imScopeFixture{
		db:       db,
		msgRepo:  msgRepo,
		sessions: &sessionService{sessionRepo: repo, messageRepo: msgRepo, webSearchStateRepo: deleteForkWebSearchState{}},
		messages: &messageService{sessionRepo: repo, messageRepo: msgRepo},
	}
	create := func(userID, title string) string {
		s := &types.Session{TenantID: 1, UserID: userID, Title: title}
		require.NoError(t, db.Create(s).Error)
		return s.ID
	}
	f.own = create("alice", "của Alice")
	f.legacy = create("", "phiên cũ")
	f.telegram = create("", "Hỏi về thuế (Telegram)")
	require.NoError(t, db.Create(&imScopeChannelSession{ID: "m1", SessionID: f.telegram, Platform: "telegram", TenantID: 1}).Error)
	return f
}

func imScopeCtx(role types.TenantRole) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "alice")
	ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "alice", Role: role})
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
}

func (f *imScopeFixture) exists(t *testing.T, id string) bool {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Model(&types.Session{}).Where("id = ?", id).Count(&n).Error)
	return n == 1
}

func listIDs(t *testing.T, svc *sessionService, ctx context.Context, keyword string) []string {
	t.Helper()
	res, err := svc.ListSessions(ctx, &types.SessionListQuery{Keyword: keyword, Page: 1, PageSize: 50})
	require.NoError(t, err)
	var ids []string
	for _, it := range res.Data.([]*types.SessionListItem) {
		ids = append(ids, it.ID)
	}
	return ids
}

// P1: the default list (sidebar, ⌘K title lookup) shows a member no IM
// session; an admin's list is unchanged; legacy ownerless web rows stay.
func TestListSessionsHidesOwnerlessIMFromMembers(t *testing.T) {
	f := newIMScopeFixture(t)
	member, admin := imScopeCtx(types.TenantRoleMember), imScopeCtx(types.TenantRoleAdmin)

	require.ElementsMatch(t, []string{f.own, f.legacy}, listIDs(t, f.sessions, member, ""))
	require.Empty(t, listIDs(t, f.sessions, member, "Telegram"), "title lookup does not reveal it either")
	require.ElementsMatch(t, []string{f.own, f.legacy, f.telegram}, listIDs(t, f.sessions, admin, ""))

	// a soft-deleted (cleared) mapping still marks the session as IM
	require.NoError(t, f.db.Delete(&imScopeChannelSession{ID: "m1"}).Error)
	require.ElementsMatch(t, []string{f.own, f.legacy}, listIDs(t, f.sessions, member, ""))
}

// P2: every owner-scoped session path refuses a member the IM session, as
// not found, and keeps working for an admin and for legacy ownerless rows.
func TestSessionWritePathsRefuseIMSessionToMembers(t *testing.T) {
	f := newIMScopeFixture(t)
	member, admin := imScopeCtx(types.TenantRoleMember), imScopeCtx(types.TenantRoleAdmin)
	notFound := func(err error) { t.Helper(); require.ErrorIs(t, err, apperrors.ErrSessionNotFound) }

	_, err := f.sessions.GetOwnedSession(member, f.telegram)
	notFound(err)
	notFound(f.sessions.UpdateSession(member, &types.Session{ID: f.telegram, TenantID: 1, Title: "đổi tên"}))
	n, err := f.sessions.SetSessionPinned(member, f.telegram, true)
	require.NoError(t, err)
	require.Zero(t, n, "pin answers 404")
	require.NoError(t, f.sessions.UpdateSessionLastRequestState(member, f.telegram, &types.SessionLastRequestState{AgentID: "x"}))

	_, err = f.messages.CreateMessage(member, &types.Message{SessionID: f.telegram, Role: "user", Content: "x"})
	notFound(err)
	notFound(f.messages.UpdateMessage(member, &types.Message{ID: "m", SessionID: f.telegram}))
	_, err = f.messages.SetMessageFeedback(member, f.telegram, "m", 1, "")
	notFound(err)
	notFound(f.messages.DeleteMessage(member, f.telegram, "m"))
	notFound(f.messages.ClearSessionMessages(member, f.telegram))
	require.Empty(t, f.msgRepo.cleared)

	notFound(f.sessions.DeleteSession(member, f.telegram))
	notFound(f.sessions.BatchDeleteSessions(member, []string{f.telegram}))
	require.True(t, f.exists(t, f.telegram))

	var stored types.Session
	require.NoError(t, f.db.First(&stored, "id = ?", f.telegram).Error)
	require.Equal(t, "Hỏi về thuế (Telegram)", stored.Title)
	require.False(t, stored.IsPinned)
	require.Nil(t, stored.LastRequestState)

	// legacy ownerless web rows keep the legacy behaviour
	_, err = f.sessions.GetOwnedSession(member, f.legacy)
	require.NoError(t, err)
	require.NoError(t, f.messages.ClearSessionMessages(member, f.legacy))

	// an admin keeps today's behaviour on the IM session
	_, err = f.sessions.GetOwnedSession(admin, f.telegram)
	require.NoError(t, err)
	n, err = f.sessions.SetSessionPinned(admin, f.telegram, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.NoError(t, f.messages.ClearSessionMessages(admin, f.telegram))
	require.Contains(t, f.msgRepo.cleared, f.telegram)
	require.NoError(t, f.sessions.DeleteSession(admin, f.telegram))
	require.False(t, f.exists(t, f.telegram))
}

// The IM runtime itself (IM principal) and internal work with no principal
// keep reaching IM sessions.
func TestIMSessionStillReachableByIMRuntime(t *testing.T) {
	f := newIMScopeFixture(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "system-1")
	ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleMember)
	im := types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalIMUser, ID: "1:ch:telegram:42"})
	_, err := f.sessions.GetOwnedSession(im, f.telegram)
	require.NoError(t, err)
	require.NoError(t, f.messages.ClearSessionMessages(im, f.telegram))
	_, err = f.sessions.GetOwnedSession(ctx, f.telegram)
	require.NoError(t, err, "no principal, synthetic user: internal work is not gated")
	// an explicit web principal with the same synthetic id is a request: gated
	web := types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "7"})
	_, err = f.sessions.GetOwnedSession(web, f.telegram)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound, "an API key does not reach another channel's IM chat")
}

// Deleting "all" as a member deletes their own and legacy rows, never the
// IM session; as an admin it stays everything in scope.
func TestDeleteAllSessionsSparesIMSessionsForMembers(t *testing.T) {
	f := newIMScopeFixture(t)
	require.NoError(t, f.sessions.DeleteAllSessions(imScopeCtx(types.TenantRoleMember)))
	require.False(t, f.exists(t, f.own))
	require.False(t, f.exists(t, f.legacy))
	require.True(t, f.exists(t, f.telegram))

	g := newIMScopeFixture(t)
	require.NoError(t, g.sessions.DeleteAllSessions(imScopeCtx(types.TenantRoleAdmin)))
	require.False(t, g.exists(t, g.telegram))
}

// failingScopeRepo makes the IM lookup or the session listing fail.
type failingScopeRepo struct {
	interfaces.SessionRepository
	platformErr, listErr error
}

func (r failingScopeRepo) GetIMPlatform(ctx context.Context, tenantID uint64, id string) (string, error) {
	if r.platformErr != nil {
		return "", r.platformErr
	}
	return r.SessionRepository.GetIMPlatform(ctx, tenantID, id)
}

func (r failingScopeRepo) GetByTenantID(ctx context.Context, tenantID uint64, userID string) ([]*types.Session, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.SessionRepository.GetByTenantID(ctx, tenantID, userID)
}

func TestChannelGateCrossTenantAdmin(t *testing.T) {
	f := newIMScopeFixture(t)
	other := context.WithValue(imScopeCtx(types.TenantRoleAdmin), types.TenantIDContextKey, uint64(2))
	_, err := f.sessions.GetOwnedSession(other, f.telegram)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound, "an admin of another organization never reaches it")
	require.Empty(t, listIDs(t, f.sessions, other, ""))
}

func TestChannelGateFailsClosedOnLookupError(t *testing.T) {
	f := newIMScopeFixture(t)
	broken := failingScopeRepo{SessionRepository: f.sessions.sessionRepo, platformErr: errors.New("db down")}
	f.sessions.sessionRepo, f.messages.sessionRepo = broken, broken
	member, admin := imScopeCtx(types.TenantRoleMember), imScopeCtx(types.TenantRoleAdmin)

	_, err := f.sessions.GetOwnedSession(member, f.legacy)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound, "write path: cannot tell, refuse")
	_, err = f.sessions.GetSession(member, f.legacy)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound, "read path: cannot tell, refuse")
	_, err = f.sessions.GetOwnedSession(member, f.own)
	require.NoError(t, err, "the caller's own session needs no lookup")
	_, err = f.sessions.GetSession(admin, f.legacy)
	require.NoError(t, err)
}

func TestChannelGateSoftDeletedMappingOnWritePath(t *testing.T) {
	f := newIMScopeFixture(t)
	require.NoError(t, f.db.Delete(&imScopeChannelSession{ID: "m1"}).Error) // /clear retires the mapping
	member := imScopeCtx(types.TenantRoleMember)
	_, err := f.sessions.GetOwnedSession(member, f.telegram)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)
	require.ErrorIs(t, f.messages.ClearSessionMessages(member, f.telegram), apperrors.ErrSessionNotFound)
}

func TestChannelGateWorkerContextWithRealUser(t *testing.T) {
	f := newIMScopeFixture(t)
	// a user id but no principal (a worker rebuilding a request context)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "alice")
	_, err := f.sessions.GetOwnedSession(ctx, f.telegram)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)
}

func TestChannelGateAPIKeys(t *testing.T) {
	f := newIMScopeFixture(t)
	key := func(full bool) context.Context {
		role := types.TenantRoleMember
		if full {
			role = types.TenantRoleAdmin // what auth gives a full-access tenant key
		}
		ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.UserIDContextKey, "system-1")
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 9, FullAccess: full})
		return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "9"})
	}
	_, err := f.sessions.GetOwnedSession(key(true), f.telegram)
	require.NoError(t, err, "a full-access key counts as admin")
	_, err = f.sessions.GetOwnedSession(key(false), f.telegram)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound, "a scoped key does not")
}

func TestDeleteAllSessionsListingErrorDeletesNothingForMembers(t *testing.T) {
	f := newIMScopeFixture(t)
	f.sessions.sessionRepo = failingScopeRepo{SessionRepository: f.sessions.sessionRepo, listErr: errors.New("db down")}
	require.Error(t, f.sessions.DeleteAllSessions(imScopeCtx(types.TenantRoleMember)))
	for _, id := range []string{f.own, f.legacy, f.telegram} {
		require.True(t, f.exists(t, id))
	}
}

// Legacy embed rows have no owner either: members neither list nor change
// them; the embed runtime keeps them.
func TestChannelGateLegacyEmbedRows(t *testing.T) {
	f := newIMScopeFixture(t)
	embed := &types.Session{TenantID: 1, Title: "widget", Description: types.EmbedSessionMarkerPrefix + "ch-1"}
	require.NoError(t, f.db.Create(embed).Error)
	member := imScopeCtx(types.TenantRoleMember)
	require.NotContains(t, listIDs(t, f.sessions, member, ""), embed.ID)
	_, err := f.sessions.GetOwnedSession(member, embed.ID)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)

	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = types.WithPrincipal(ctx, types.EmbedSessionPrincipal(1, "ch-1", embed.ID))
	_, err = f.sessions.GetOwnedSession(ctx, embed.ID)
	require.NoError(t, err, "the embed runtime keeps its rows")
	_, err = f.sessions.GetOwnedSession(ctx, f.telegram)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound, "but never an IM chat")
}

// A capped IM chat is rotated with ContinueSession before its new mapping
// row is written: the continuation carries the IM description, so members
// cannot list or touch it in that window, and the IM runtime can.
func TestIMRotationContinuationIsLockedBeforeItsMapping(t *testing.T) {
	f := newIMScopeFixture(t)
	require.NoError(t, f.db.Model(&types.Session{}).Where("id = ?", f.telegram).
		Update("description", types.IMSessionDescription("telegram", false)).Error)
	var old types.Session
	require.NoError(t, f.db.First(&old, "id = ?", f.telegram).Error)

	imCtx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	imCtx = context.WithValue(imCtx, types.UserIDContextKey, "system-1")
	imCtx = types.WithPrincipal(imCtx, types.Principal{Type: types.PrincipalIMUser, ID: "1:ch:telegram:42"})
	next, err := f.sessions.ContinueSession(imCtx, &old)
	require.NoError(t, err)
	require.Empty(t, next.UserID)
	require.True(t, types.IsIMSessionDescription(next.Description))

	member := imScopeCtx(types.TenantRoleMember)
	require.NotContains(t, listIDs(t, f.sessions, member, ""), next.ID)
	_, err = f.sessions.GetOwnedSession(member, next.ID)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)
	_, err = f.sessions.GetSession(member, next.ID)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound)

	_, err = f.sessions.GetSession(imCtx, next.ID)
	require.NoError(t, err, "the IM runtime reads it before the mapping exists")
	require.NoError(t, f.messages.ClearSessionMessages(imCtx, next.ID))
	require.Contains(t, listIDs(t, f.sessions, imScopeCtx(types.TenantRoleAdmin), ""), next.ID)
}

func TestIMSessionDescription(t *testing.T) {
	require.Equal(t, "Auto-created from telegram IM integration", types.IMSessionDescription("telegram", false))
	require.Equal(t, "Thread-based session from slack IM", types.IMSessionDescription("slack", true))
	require.True(t, types.IsIMSessionDescription("Thread-based session from slack IM"))
	require.False(t, types.IsIMSessionDescription("Auto-created from"))
	require.False(t, types.IsIMSessionDescription("ghi chú của tôi"))
}

// An update without a description (or with a planted marker) never removes
// or adds the IM / embed marker.
func TestUpdateSessionKeepsChannelMarkers(t *testing.T) {
	f := newIMScopeFixture(t)
	imDesc := types.IMSessionDescription("telegram", false)
	require.NoError(t, f.db.Model(&types.Session{}).Where("id = ?", f.telegram).Update("description", imDesc).Error)
	admin, member := imScopeCtx(types.TenantRoleAdmin), imScopeCtx(types.TenantRoleMember)

	require.NoError(t, f.sessions.UpdateSession(admin, &types.Session{ID: f.telegram, TenantID: 1, Title: "đổi tên"}))
	var stored types.Session
	require.NoError(t, f.db.First(&stored, "id = ?", f.telegram).Error)
	require.Equal(t, imDesc, stored.Description, "the IM marker survives an update without description")
	require.Equal(t, "đổi tên", stored.Title)

	require.NoError(t, f.sessions.UpdateSession(member, &types.Session{
		ID: f.own, TenantID: 1, Title: "của Alice", Description: types.EmbedSessionMarkerPrefix + "ch-x",
	}))
	var mine types.Session
	require.NoError(t, f.db.First(&mine, "id = ?", f.own).Error)
	require.Empty(t, mine.Description, "a client cannot plant a channel marker")
}

// The runtimes are exempt only on their own traffic: the IM runtime on IM
// chats, an embed visitor on its own row or its channel's legacy row.
func TestChannelGateRuntimeExemptionsAreNarrow(t *testing.T) {
	f := newIMScopeFixture(t)
	embedOther := &types.Session{TenantID: 1, Title: "kênh khác", Description: types.EmbedSessionMarkerPrefix + "ch-2"}
	embedOwn := &types.Session{TenantID: 1, Title: "kênh này", Description: types.EmbedSessionMarkerPrefix + "ch-1"}
	require.NoError(t, f.db.Create(embedOther).Error)
	require.NoError(t, f.db.Create(embedOwn).Error)
	base := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))

	im := types.WithPrincipal(context.WithValue(base, types.UserIDContextKey, "system-1"),
		types.Principal{Type: types.PrincipalIMUser, ID: "1:ch:telegram:42"})
	_, err := f.sessions.GetOwnedSession(im, f.telegram)
	require.NoError(t, err, "IM runtime on a mapped IM chat")
	_, err = f.sessions.GetOwnedSession(im, embedOwn.ID)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound, "IM runtime never on an embed row")
	_, err = f.sessions.GetOwnedSession(im, f.legacy)
	require.NoError(t, err, "a plain legacy web row is no channel session")

	visitor := types.EmbedSessionPrincipal(1, "ch-1", embedOwn.ID)
	embedCtx := types.WithPrincipal(base, visitor)
	_, err = f.sessions.GetOwnedSession(embedCtx, embedOwn.ID)
	require.NoError(t, err, "its channel's legacy row")
	_, err = f.sessions.GetOwnedSession(embedCtx, embedOther.ID)
	require.ErrorIs(t, err, apperrors.ErrSessionNotFound, "another channel's row")
	owned := &types.Session{TenantID: 1, UserID: visitor.StorageID(), Title: "của khách", Description: types.EmbedSessionMarkerPrefix + "ch-1"}
	require.NoError(t, f.db.Create(owned).Error)
	_, err = f.sessions.GetOwnedSession(embedCtx, owned.ID)
	require.NoError(t, err, "its own row by owner id")
}
