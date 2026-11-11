package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type recentMessagesRepo struct {
	interfaces.MessageRepository
	messages []*types.Message
}

func (r *recentMessagesRepo) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	return r.messages, nil
}

func decreeCard(t *testing.T, query string) *tools.LegalCandidatesCard {
	t.Helper()
	card := tools.LegalCandidatesFromDocs(query, "", []*types.LegalDocument{
		{DocumentNumber: "53/2022/NĐ-CP", DocType: "Nghị định", Title: "Quy định chi tiết Luật An ninh mạng"},
		{DocumentNumber: "53/2024/NĐ-CP", DocType: "Nghị định", Title: "Quy định về tài nguyên nước"},
	})
	require.NotNil(t, card)
	require.Equal(t, "Nghị định", card.Reference.DocType)
	require.Equal(t, "53", card.Reference.Number)
	return card
}

// cardMessage is an assistant message carrying the card as stored: the
// payload goes through JSON like agent_steps in the database.
func cardMessage(t *testing.T, id string, card *tools.LegalCandidatesCard) *types.Message {
	t.Helper()
	raw, err := json.Marshal(card.Data())
	require.NoError(t, err)
	var data map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &data))
	return &types.Message{ID: id, Role: "assistant", AgentSteps: types.AgentSteps{{ToolCalls: []types.ToolCall{{
		Name: tools.LegalCandidatesType, Result: &types.ToolResult{Success: true, Data: data},
	}}}}}
}

func pickRequest(query string) *types.QARequest {
	return &types.QARequest{Session: &types.Session{ID: "s"}, Query: query, UserMessageID: "u-now"}
}

func TestRememberedLegalPick(t *testing.T) {
	first := decreeCard(t, "Nghị định 53 quy định gì về lưu trữ dữ liệu?")
	resend := first.Resend(first.Candidates[0])
	now := decreeCard(t, "Nghị định 53 còn hiệu lực không?")

	repo := &recentMessagesRepo{messages: []*types.Message{
		{ID: "u1", Role: "user", Content: "Nghị định 53 quy định gì về lưu trữ dữ liệu?"},
		cardMessage(t, "a1", first),
		{ID: "u2", Role: "user", Content: resend},
		{ID: "a2", Role: "assistant", Content: "…"},
		{ID: "u-now", Role: "user", Content: "Nghị định 53 còn hiệu lực không?"},
	}}
	s := &sessionService{messageRepo: repo}
	pin := s.rememberedLegalPick(context.Background(), pickRequest("Nghị định 53 còn hiệu lực không?"), now)
	require.NotNil(t, pin)
	require.Equal(t, types.LegalReferencePin{Reference: "Nghị định 53", DocumentNumber: "53/2022/NĐ-CP",
		DocType: "Nghị định", Title: "Quy định chi tiết Luật An ninh mạng"}, *pin)

	// a typed answer naming one số hiệu is a pick too; the newest pick wins
	repo.messages = append(repo.messages[:4:4],
		cardMessage(t, "a3", first),
		&types.Message{ID: "u3", Role: "user", Content: "ý tôi là 53/2024/nđ-cp"},
		&types.Message{ID: "u-now", Role: "user", Content: "Nghị định 53 còn hiệu lực không?"})
	pin = s.rememberedLegalPick(context.Background(), pickRequest("Nghị định 53 còn hiệu lực không?"), now)
	require.NotNil(t, pin)
	require.Equal(t, "53/2024/NĐ-CP", pin.DocumentNumber)
}

func TestRememberedLegalPickNeedsAPick(t *testing.T) {
	first := decreeCard(t, "Nghị định 53 quy định gì?")
	now := decreeCard(t, "Nghị định 53 còn hiệu lực không?")
	s := &sessionService{}
	for name, msgs := range map[string][]*types.Message{
		"card ignored":  {cardMessage(t, "a1", first), {ID: "u2", Role: "user", Content: "thôi, hỏi việc khác"}},
		"both named":    {cardMessage(t, "a1", first), {ID: "u2", Role: "user", Content: "53/2022/NĐ-CP hay 53/2024/NĐ-CP?"}},
		"card is last":  {cardMessage(t, "a1", first), {ID: "u-now", Role: "user", Content: "Nghị định 53 còn hiệu lực không?"}},
		"other number":  {cardMessage(t, "a1", decreeCardFor(t, "Thông tư 01 là gì?")), {ID: "u2", Role: "user", Content: "01/2011/TT-BNV"}},
		"longer number": {cardMessage(t, "a1", first), {ID: "u2", Role: "user", Content: "Nghị định 153/2022/NĐ-CP"}},
	} {
		s.messageRepo = &recentMessagesRepo{messages: msgs}
		require.Nil(t, s.rememberedLegalPick(context.Background(), pickRequest("Nghị định 53 còn hiệu lực không?"), now), name)
	}
}

func decreeCardFor(t *testing.T, query string) *tools.LegalCandidatesCard {
	t.Helper()
	card := tools.LegalCandidatesFromDocs(query, "", []*types.LegalDocument{
		{DocumentNumber: "01/2011/TT-BNV", DocType: "Thông tư"},
		{DocumentNumber: "01/2023/TT-BCA", DocType: "Thông tư"},
	})
	require.NotNil(t, card)
	return card
}

func TestContainsNumber(t *testing.T) {
	require.True(t, containsNumber("THEO 53/2022/NĐ-CP, ĐIỀU 5", "53/2022/NĐ-CP"))
	require.True(t, containsNumber("53/2022/NĐ-CP", "53/2022/NĐ-CP"))
	require.False(t, containsNumber("153/2022/NĐ-CP", "53/2022/NĐ-CP"))
	require.False(t, containsNumber("53/2022/NĐ-CPA", "53/2022/NĐ-CP"))
	require.True(t, containsNumber("153/2022/NĐ-CP VÀ 53/2022/NĐ-CP", "53/2022/NĐ-CP"))
}

// The pick is used only while the current card still offers it; a later
// message naming another candidate replaces it; an ordinal answer right
// after the card picks that choice; the look-alike Ð matches Đ.
func TestRememberedLegalPickFollowsTheConversation(t *testing.T) {
	first := decreeCard(t, "Nghị định 53 quy định gì?")
	s := &sessionService{}
	ask := pickRequest("Nghị định 53 còn hiệu lực không?")
	base := func(reply string) []*types.Message {
		return []*types.Message{cardMessage(t, "a1", first), {ID: "u2", Role: "user", Content: reply}}
	}

	// narrowed by topic: the earlier pick is not among the choices
	taxOnly := tools.LegalCandidatesFromDocs("Nghị định 53 về tài nguyên nước", "", []*types.LegalDocument{
		{DocumentNumber: "53/2024/NĐ-CP", DocType: "Nghị định"}, {DocumentNumber: "53/2019/NĐ-CP", DocType: "Nghị định"},
	})
	s.messageRepo = &recentMessagesRepo{messages: base(first.Resend(first.Candidates[0]))}
	require.Nil(t, s.rememberedLegalPick(context.Background(), ask, taxOnly))

	now := decreeCard(t, "Nghị định 53 còn hiệu lực không?")
	// a later message naming the other one, not right after the card
	s.messageRepo = &recentMessagesRepo{messages: append(base(first.Resend(first.Candidates[0])),
		&types.Message{ID: "a2", Role: "assistant", Content: "…"},
		&types.Message{ID: "u3", Role: "user", Content: "không, tôi hỏi 53/2024/NĐ-CP"})}
	pin := s.rememberedLegalPick(context.Background(), ask, now)
	require.NotNil(t, pin)
	require.Equal(t, "53/2024/NĐ-CP", pin.DocumentNumber)
	require.Equal(t, "Quy định về tài nguyên nước", pin.Title)

	// "2" typed under the card (Word add-in)
	s.messageRepo = &recentMessagesRepo{messages: base("2")}
	pin = s.rememberedLegalPick(context.Background(), ask, now)
	require.NotNil(t, pin)
	require.Equal(t, "53/2024/NĐ-CP", pin.DocumentNumber)
	// …but not out of the blue later on
	s.messageRepo = &recentMessagesRepo{messages: append(base("thôi"),
		&types.Message{ID: "a2", Role: "assistant"}, &types.Message{ID: "u3", Role: "user", Content: "2"})}
	require.Nil(t, s.rememberedLegalPick(context.Background(), ask, now))

	// Ð (U+00D0) typed for Đ
	s.messageRepo = &recentMessagesRepo{messages: base("53/2022/NÐ-CP")}
	pin = s.rememberedLegalPick(context.Background(), ask, now)
	require.NotNil(t, pin)
	require.Equal(t, "53/2022/NĐ-CP", pin.DocumentNumber)
}

// A reused pick is noted on the running turn's state (history) and sent to
// the chat as an abbreviation_resolution event (live); asking again is
// recognised whatever the spacing or case.
func TestNoteRememberedChoice(t *testing.T) {
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
	bound, _, err := svc.PrepareAbbreviationTurn(ctx, req, bus)
	require.NoError(t, err)
	require.NoError(t, svc.claimAbbreviationExecution(bound, req))

	var seen []types.AbbreviationTypo
	bus.On(event.EventAbbreviationResolution, func(_ context.Context, evt event.Event) error {
		seen = evt.Data.(event.AbbreviationResolutionData).RememberedSelections
		return nil
	})
	choice := types.AbbreviationTypo{Wrong: "Nghị định 53", Correct: "Nghị định 53/2022/NĐ-CP"}
	svc.noteRememberedChoice(bound, req, bus, choice)
	svc.noteRememberedChoice(bound, req, bus, choice) // once only

	row, err := store.Get(ctx, owner, mustResolutionRequestID(t, bound))
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationTurnRunning, row.State)
	require.Equal(t, []types.AbbreviationTypo{choice}, row.Payload.Resolution.RememberedSelections)
	require.Equal(t, []types.AbbreviationTypo{choice}, seen)

	// the completion watcher still finishes the turn after the extra write
	svc.watchAbbreviationCompletion(bound, req, bus)
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, SessionID: "s",
		Data: event.AgentFinalAnswerData{Content: "done", Done: true}}))
	row, err = store.Get(ctx, owner, row.ID)
	require.NoError(t, err)
	require.Equal(t, types.AbbreviationTurnCompleted, row.State)
	require.Equal(t, []types.AbbreviationTypo{choice}, row.Payload.Resolution.RememberedSelections)

	// without a gate turn in the context nothing happens
	svc.noteRememberedChoice(ctx, req, bus, types.AbbreviationTypo{Wrong: "x", Correct: "y"})

	require.True(t, askedAgain(&types.QARequest{AbbreviationAskWords: []string{" nghị  định 53 "}}, "Nghị định 53"))
	require.False(t, askedAgain(&types.QARequest{AbbreviationAskWords: []string{"ATTT"}}, "Nghị định 53"))
}

// A card shown again after the pick ("Không phải?") and left unanswered
// drops the pick: the next bare reference shows the card again.
func TestRememberedLegalPickDroppedWhenCardShownAgain(t *testing.T) {
	first := decreeCard(t, "Nghị định 53 quy định gì?")
	now := decreeCard(t, "Nghị định 53 còn hiệu lực không?")
	s := &sessionService{messageRepo: &recentMessagesRepo{messages: []*types.Message{
		cardMessage(t, "a1", first),
		{ID: "u2", Role: "user", Content: first.Resend(first.Candidates[0])},
		{ID: "a2", Role: "assistant", Content: "…"},
		{ID: "u3", Role: "user", Content: "Nghị định 53 quy định gì?"},
		cardMessage(t, "a3", first),
		{ID: "u4", Role: "user", Content: "thôi, hỏi việc khác"},
	}}}
	require.Nil(t, s.rememberedLegalPick(context.Background(), pickRequest("Nghị định 53 còn hiệu lực không?"), now))
}

type candidatesCatalog struct {
	interfaces.LegalCatalogService
	docs []*types.LegalDocument
}

func (c *candidatesCatalog) Candidates(context.Context, string, string, int) ([]*types.LegalDocument, int, error) {
	return c.docs, len(c.docs), nil
}

// Through the gate: a remembered pick lets the turn run with the pin; after
// "Không phải?" (the reference in recheck_words) the card is shown again.
func TestLegalCandidatesGateReusesPickUnlessAskedAgain(t *testing.T) {
	first := decreeCard(t, "Nghị định 53 quy định gì?")
	s := &sessionService{
		legalCatalog: &candidatesCatalog{docs: first.Candidates},
		messageRepo: &recentMessagesRepo{messages: []*types.Message{
			cardMessage(t, "a1", first),
			{ID: "u2", Role: "user", Content: first.Resend(first.Candidates[0])},
		}},
	}
	req := pickRequest("Nghị định 53 còn hiệu lực không?")
	req.CustomAgent = &types.CustomAgent{ID: types.BuiltinDocumentAssistantID}
	req.AssistantMessageID = "a-now"

	bus := event.NewEventBus()
	cards := 0
	bus.On(event.EventAgentToolResult, func(context.Context, event.Event) error { cards++; return nil })
	ctx, asked, err := s.legalCandidatesClarification(context.Background(), req, bus)
	require.NoError(t, err)
	require.False(t, asked)
	require.Zero(t, cards)
	pins := types.LegalReferencePinsFromContext(ctx)
	require.Len(t, pins, 1)
	require.Equal(t, "53/2022/NĐ-CP", pins[0].DocumentNumber)

	req.AbbreviationAskWords = []string{"nghị định 53"}
	ctx, asked, err = s.legalCandidatesClarification(context.Background(), req, bus)
	require.NoError(t, err)
	require.True(t, asked)
	require.Equal(t, 1, cards)
	require.Empty(t, types.LegalReferencePinsFromContext(ctx))
}
