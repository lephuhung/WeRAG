package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
	"github.com/stretchr/testify/require"
)

type fixedAbbreviationPreparer struct {
	result types.AbbreviationResolution
	calls  int
}

func (p *fixedAbbreviationPreparer) Prepare(context.Context, AbbreviationPrepareInput) (types.AbbreviationResolution, error) {
	p.calls++
	return p.result, nil
}

type abbreviationSessionRepo struct {
	interfaces.SessionRepository
	userID string
}

func (r *abbreviationSessionRepo) Get(_ context.Context, tenantID uint64, userID, id string) (*types.Session, error) {
	want := r.userID
	if want == "" {
		want = "alice"
	}
	if tenantID != 7 || userID != want || id != "s" {
		return nil, types.ErrAbbreviationNotFound
	}
	return &types.Session{ID: "s", TenantID: 7, UserID: want}, nil
}

type abbreviationMessageRepo struct {
	interfaces.MessageRepository
	messages map[string]*types.Message
}

func newAbbreviationMessageRepo() *abbreviationMessageRepo {
	return &abbreviationMessageRepo{messages: map[string]*types.Message{
		"u1": {ID: "u1", SessionID: "s", Role: "user", Content: "ATTT có yêu cầu gì"},
		"a1": {ID: "a1", SessionID: "s", Role: "assistant"},
	}}
}

func (r *abbreviationMessageRepo) GetMessage(_ context.Context, sessionID, id string) (*types.Message, error) {
	m := r.messages[id]
	if m == nil || m.SessionID != sessionID {
		return nil, types.ErrAbbreviationNotFound
	}
	return m, nil
}

func abbreviationTestContext() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "alice")
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
}

func abbreviationTestRequest(query string) *types.QARequest {
	return &types.QARequest{
		Session: &types.Session{ID: "s", TenantID: 7, UserID: "alice"},
		Query:   query, UserMessageID: "u1", AssistantMessageID: "a1",
	}
}

func TestSessionAbbreviationStopsBothQAEntries(t *testing.T) {
	for _, mode := range []string{"agent", "knowledge"} {
		t.Run(mode, func(t *testing.T) {
			preparer := &fixedAbbreviationPreparer{result: types.AbbreviationResolution{
				RequestID: "r1", RootUserMessageID: "u1", OriginalQuery: "ATTT có yêu cầu gì",
				Status: types.AbbreviationStatusNeedsDefinition, UnknownTerms: []string{"ATTT"}, Version: 2,
			}}
			svc := &sessionService{sessionRepo: &abbreviationSessionRepo{}, messageRepo: newAbbreviationMessageRepo(), abbreviationPreparer: preparer}
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
			ctx = context.WithValue(ctx, types.UserIDContextKey, "alice")
			ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
			req := &types.QARequest{Session: &types.Session{ID: "s", TenantID: 7, UserID: "alice"}, Query: "ATTT có yêu cầu gì", UserMessageID: "u1", AssistantMessageID: "a1"}
			bus := event.NewEventBus()
			finalDone, complete := 0, 0
			var answer string
			bus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
				data, ok := evt.Data.(event.AgentFinalAnswerData)
				if ok && data.Done {
					finalDone++
					answer = data.Content
				}
				return nil
			})
			bus.On(event.EventAgentComplete, func(context.Context, event.Event) error { complete++; return nil })
			var err error
			if mode == "agent" {
				err = svc.AgentQA(ctx, req, bus)
			} else {
				err = svc.KnowledgeQA(ctx, req, bus)
			}
			require.NoError(t, err)
			require.Equal(t, 1, finalDone)
			require.Equal(t, 1, complete)
			require.Contains(t, answer, "ATTT")
			require.NotContains(t, answer, "An toàn thông tin")
			require.Equal(t, 1, preparer.calls)
		})
	}
}

func TestSessionAbbreviationMissingPreparerFailsClosed(t *testing.T) {
	svc := &sessionService{sessionRepo: &abbreviationSessionRepo{}, messageRepo: newAbbreviationMessageRepo()}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "alice")
	req := &types.QARequest{Session: &types.Session{ID: "s", TenantID: 7, UserID: "alice"}, Query: "ATTT có yêu cầu gì", UserMessageID: "u1", AssistantMessageID: "a1"}
	for _, qa := range []func(context.Context, *types.QARequest, *event.EventBus) error{svc.AgentQA, svc.KnowledgeQA} {
		require.Error(t, qa(ctx, req, event.NewEventBus()))
	}
}

func TestSessionAbbreviationReadyIdempotentNoDuplicateEvents(t *testing.T) {
	preparer := &fixedAbbreviationPreparer{result: types.AbbreviationResolution{
		RequestID: "r1", RootUserMessageID: "u1", OriginalQuery: "xin chào",
		Status: types.AbbreviationStatusReady, EffectiveQuery: "xin chào", Version: 2,
	}}
	svc := &sessionService{sessionRepo: &abbreviationSessionRepo{}, messageRepo: newAbbreviationMessageRepo(), abbreviationPreparer: preparer}
	ctx := abbreviationTestContext()
	req := abbreviationTestRequest("xin chào")
	req.UserMessageID, req.AssistantMessageID = "u1", "a1"
	svc.messageRepo.(*abbreviationMessageRepo).messages["u1"].Content = "xin chào"
	bus := event.NewEventBus()
	states := 0
	bus.On(event.EventAbbreviationResolution, func(context.Context, event.Event) error { states++; return nil })

	bound, handled, err := svc.PrepareAbbreviationTurn(ctx, req, bus)
	require.NoError(t, err)
	require.False(t, handled)
	require.Equal(t, "xin chào", abbreviationEffectiveQuery(bound, req.Query))
	require.Equal(t, 1, states)

	// A second gate pass over the same bound context must be a no-op: no
	// duplicate turn lookup and no duplicate resolution event.
	again, handled, err := svc.PrepareAbbreviationTurn(bound, req, bus)
	require.NoError(t, err)
	require.False(t, handled)
	require.Same(t, bound, again)
	require.Equal(t, 1, preparer.calls)
	require.Equal(t, 1, states)
}

func TestSessionAbbreviationContinuationRequiresVersion(t *testing.T) {
	preparer := &fixedAbbreviationPreparer{}
	svc := &sessionService{sessionRepo: &abbreviationSessionRepo{}, messageRepo: newAbbreviationMessageRepo(), abbreviationPreparer: preparer}
	req := abbreviationTestRequest("An toàn thông tin")
	req.ClarificationRequestID = "r1"
	_, _, err := svc.PrepareAbbreviationTurn(abbreviationTestContext(), req, event.NewEventBus())
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)
	require.Zero(t, preparer.calls)
}

func TestSessionAbbreviationResumeScopeWideningRejected(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	svc := &sessionService{
		sessionRepo:             &abbreviationSessionRepo{},
		messageRepo:             newAbbreviationMessageRepo(),
		abbreviationPreparer:    NewAbbreviationTurnCoordinator(store, dict, nil),
		abbreviationStore:       store,
		abbreviationCoordinator: NewAbbreviationTurnCoordinator(store, dict, nil),
	}
	ctx := abbreviationTestContext()

	req1 := abbreviationTestRequest("ATTT có yêu cầu gì")
	req1.KnowledgeBaseIDs = []string{"kb-1"}
	_, handled, err := svc.PrepareAbbreviationTurn(ctx, req1, event.NewEventBus())
	require.NoError(t, err)
	require.True(t, handled)

	row, err := store.Awaiting(ctx, owner, time.Now())
	require.NoError(t, err)
	require.NotNil(t, row)

	svc.messageRepo.(*abbreviationMessageRepo).messages["u2"] = &types.Message{ID: "u2", SessionID: "s", Role: "user", Content: "An toàn thông tin"}
	svc.messageRepo.(*abbreviationMessageRepo).messages["a2"] = &types.Message{ID: "a2", SessionID: "s", Role: "assistant"}

	resume := abbreviationTestRequest("An toàn thông tin")
	resume.UserMessageID, resume.AssistantMessageID = "u2", "a2"
	resume.KnowledgeBaseIDs = []string{"kb-1", "kb-2"}
	resume.ClarificationRequestID = row.ID
	resume.ClarificationVersion = &row.Version
	_, _, err = svc.PrepareAbbreviationTurn(ctx, resume, event.NewEventBus())
	require.ErrorIs(t, err, types.ErrAbbreviationConflict)

	// The same scope as the frozen snapshot resumes and binds the root query.
	resume.KnowledgeBaseIDs = []string{"kb-1"}
	bound, handled, err := svc.PrepareAbbreviationTurn(ctx, resume, event.NewEventBus())
	require.NoError(t, err)
	require.False(t, handled)
	require.Equal(t, "An toàn thông tin (ATTT) có yêu cầu gì", abbreviationEffectiveQuery(bound, resume.Query))
	// The raw request query still carries the user's definition text.
	require.Equal(t, "An toàn thông tin", resume.Query)
}

func TestSessionAbbreviationClaimOnceThenComplete(t *testing.T) {
	_, store, dict, owner := newTurnCoordinatorFixture(t)
	svc := &sessionService{
		sessionRepo:          &abbreviationSessionRepo{},
		messageRepo:          newAbbreviationMessageRepo(),
		abbreviationPreparer: NewAbbreviationTurnCoordinator(store, dict, nil),
		abbreviationStore:    store,
	}
	svc.messageRepo.(*abbreviationMessageRepo).messages["u1"].Content = "xin chào"
	ctx := abbreviationTestContext()
	req := abbreviationTestRequest("xin chào")
	bus := event.NewEventBus()

	bound, handled, err := svc.PrepareAbbreviationTurn(ctx, req, bus)
	require.NoError(t, err)
	require.False(t, handled)
	require.NoError(t, svc.claimAbbreviationExecution(bound, req))
	// A second claim of the already-running turn must conflict, not rerun.
	require.ErrorIs(t, svc.claimAbbreviationExecution(bound, req), types.ErrAbbreviationConflict)

	row, err := store.Get(ctx, owner, mustResolutionRequestID(t, bound))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationTurnRunning, row.State)

	svc.watchAbbreviationCompletion(bound, req, bus)
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: "s",
		Data: event.AgentFinalAnswerData{Content: "done", Done: true}}))
	row, err = store.Get(ctx, owner, row.ID)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationTurnCompleted, row.State)
}

func mustResolutionRequestID(t *testing.T, ctx context.Context) string {
	t.Helper()
	r, ok := abbreviation.ResolutionFromContext(ctx)
	require.True(t, ok)
	require.NotEmpty(t, r.RequestID)
	return r.RequestID
}
