package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/people"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePeopleLookupCall struct {
	method string
	query  string
	limit  int64
}

type fakeQuickPeopleLookupService struct {
	enabled bool
	result  *people.SearchResult
	calls   []fakePeopleLookupCall
}

func (f *fakeQuickPeopleLookupService) Enabled() bool { return f.enabled }

func (f *fakeQuickPeopleLookupService) SearchByCCCD(_ context.Context, q string) *people.SearchResult {
	f.calls = append(f.calls, fakePeopleLookupCall{method: "cccd", query: q})
	return f.result
}

func (f *fakeQuickPeopleLookupService) SearchByBHXH(_ context.Context, q string) *people.SearchResult {
	f.calls = append(f.calls, fakePeopleLookupCall{method: "bhxh", query: q})
	return f.result
}

func (f *fakeQuickPeopleLookupService) SearchByPhone(_ context.Context, q string) *people.SearchResult {
	f.calls = append(f.calls, fakePeopleLookupCall{method: "phone", query: q})
	return f.result
}

func (f *fakeQuickPeopleLookupService) SearchByName(_ context.Context, q string, limit int64) *people.SearchResult {
	f.calls = append(f.calls, fakePeopleLookupCall{method: "name", query: q, limit: limit})
	return f.result
}

type capturedEvent struct {
	typ  event.EventType
	data interface{}
}

func captureEvents(bus *event.EventBus) *[]capturedEvent {
	events := &[]capturedEvent{}
	record := func(typ event.EventType) event.EventHandler {
		return func(_ context.Context, evt event.Event) error {
			*events = append(*events, capturedEvent{typ: typ, data: evt.Data})
			return nil
		}
	}
	bus.On(event.EventAgentToolCall, record(event.EventAgentToolCall))
	bus.On(event.EventAgentToolResult, record(event.EventAgentToolResult))
	bus.On(event.EventAgentFinalAnswer, record(event.EventAgentFinalAnswer))
	return events
}

func systemAdminCtx() context.Context {
	return context.WithValue(context.Background(), types.SystemAdminContextKey, true)
}

func TestKnowledgeQAPeopleLookupSystemAdminShortCircuits(t *testing.T) {
	fake := &fakeQuickPeopleLookupService{
		enabled: true,
		result: &people.SearchResult{
			Found:      true,
			Display:    "PERSON DISPLAY",
			LookupType: "phone",
		},
	}
	svc := &sessionService{peopleService: fake}
	bus := event.NewEventBus()
	events := captureEvents(bus)

	req := &types.QARequest{
		Session: &types.Session{ID: "sess-1"},
		Query:   "Tra cứu số điện thoại 0989755968",
	}
	err := svc.KnowledgeQA(systemAdminCtx(), req, bus)
	require.NoError(t, err)

	require.Len(t, fake.calls, 1)
	assert.Equal(t, "phone", fake.calls[0].method)
	assert.Equal(t, "0989755968", fake.calls[0].query)

	// Found results still emit a text answer — but only the headline line;
	// the profile dump is rendered as person cards from the tool result data.
	require.Len(t, *events, 4)
	assert.Equal(t, event.EventAgentToolCall, (*events)[0].typ)
	assert.Equal(t, event.EventAgentToolResult, (*events)[1].typ)
	assert.Equal(t, event.EventAgentFinalAnswer, (*events)[2].typ)
	assert.Equal(t, event.EventAgentFinalAnswer, (*events)[3].typ)

	callData, ok := (*events)[0].data.(event.AgentToolCallData)
	require.True(t, ok)
	assert.Equal(t, tools.ToolPeopleLookup, callData.ToolName)
	assert.Equal(t, "phone", callData.Arguments["lookup_type"])
	assert.Equal(t, "0989755968", callData.Arguments["query"])

	resultData, ok := (*events)[1].data.(event.AgentToolResultData)
	require.True(t, ok)
	assert.True(t, resultData.Success)
	assert.Equal(t, "PERSON DISPLAY", resultData.Output)
	assert.Equal(t, true, resultData.Data["found"])
	assert.Equal(t, "phone", resultData.Data["lookup_type"])

	answer, ok := (*events)[2].data.(event.AgentFinalAnswerData)
	require.True(t, ok)
	assert.Equal(t, "PERSON DISPLAY", answer.Content)
	assert.False(t, answer.Done)

	done, ok := (*events)[3].data.(event.AgentFinalAnswerData)
	require.True(t, ok)
	assert.True(t, done.Done)
}

func TestTryQuickAnswerPeopleLookupSystemAdminOnly(t *testing.T) {
	fake := &fakeQuickPeopleLookupService{enabled: true, result: &people.SearchResult{Display: "x"}}
	svc := &sessionService{peopleService: fake}
	bus := event.NewEventBus()

	ownerCtx := context.WithValue(context.Background(), types.TenantRoleContextKey, types.TenantRoleOwner)
	ownerReq := &types.QARequest{Session: &types.Session{ID: "sess-1"}, Query: "Tra cứu số điện thoại 0989755968"}
	handled, err := svc.tryQuickAnswerPeopleLookup(ownerCtx, ownerReq, bus)
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Empty(t, fake.calls)

	adminReq := &types.QARequest{Session: &types.Session{ID: "sess-1"}, Query: "Tra cứu số điện thoại 0989755968"}
	handled, err = svc.tryQuickAnswerPeopleLookup(systemAdminCtx(), adminReq, bus)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Len(t, fake.calls, 1)
}

func TestTryQuickAnswerPeopleLookupDisabledOrUnmatched(t *testing.T) {
	disabled := &fakeQuickPeopleLookupService{enabled: false}
	svc := &sessionService{peopleService: disabled}
	bus := event.NewEventBus()

	disabledReq := &types.QARequest{Session: &types.Session{ID: "sess-1"}, Query: "Tra cứu số điện thoại 0989755968"}
	handled, err := svc.tryQuickAnswerPeopleLookup(systemAdminCtx(), disabledReq, bus)
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Empty(t, disabled.calls)

	enabled := &fakeQuickPeopleLookupService{enabled: true, result: &people.SearchResult{Display: "x"}}
	svc = &sessionService{peopleService: enabled}
	unmatchedReq := &types.QARequest{Session: &types.Session{ID: "sess-1"}, Query: "Quy định về thời hiệu khởi kiện"}
	handled, err = svc.tryQuickAnswerPeopleLookup(systemAdminCtx(), unmatchedReq, bus)
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Empty(t, enabled.calls)
}

func TestTryQuickAnswerPeopleLookupPropagatesEventError(t *testing.T) {
	fake := &fakeQuickPeopleLookupService{enabled: true, result: &people.SearchResult{Display: "x"}}
	svc := &sessionService{peopleService: fake}
	bus := event.NewEventBus()
	bus.On(event.EventAgentToolCall, func(_ context.Context, _ event.Event) error {
		return errors.New("event failed")
	})

	req := &types.QARequest{Session: &types.Session{ID: "sess-1"}, Query: "Tra cứu số điện thoại 0989755968"}
	handled, err := svc.tryQuickAnswerPeopleLookup(systemAdminCtx(), req, bus)
	assert.True(t, handled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "emit people_lookup tool call")
	assert.Contains(t, err.Error(), "event failed")
	assert.Empty(t, fake.calls)
}

func TestTryQuickAnswerPeopleLookupMultiIntentContinuesPipeline(t *testing.T) {
	fake := &fakeQuickPeopleLookupService{
		enabled: true,
		result: &people.SearchResult{
			Found:      true,
			Display:    "PERSON DISPLAY",
			LookupType: "phone",
		},
	}
	svc := &sessionService{peopleService: fake}
	bus := event.NewEventBus()
	events := captureEvents(bus)

	req := &types.QARequest{
		Session:       &types.Session{ID: "sess-1"},
		Query:         "Tìm thông tin số điện thoại 0989755968 và đối chiếu quy định về hồ sơ cấp độ xem vi phạm gì không",
		QuotedContext: "EXISTING CONTEXT",
	}
	handled, err := svc.tryQuickAnswerPeopleLookup(systemAdminCtx(), req, bus)
	require.NoError(t, err)
	assert.False(t, handled)

	require.Len(t, fake.calls, 1)
	assert.Equal(t, "phone", fake.calls[0].method)
	assert.Equal(t, "0989755968", fake.calls[0].query)

	assert.Contains(t, req.QuotedContext, "EXISTING CONTEXT")
	assert.Contains(t, req.QuotedContext, "<people_lookup>")
	assert.Contains(t, req.QuotedContext, "PERSON DISPLAY")
	assert.Contains(t, req.QuotedContext, "</people_lookup>")

	require.Len(t, *events, 2)
	assert.Equal(t, event.EventAgentToolCall, (*events)[0].typ)
	assert.Equal(t, event.EventAgentToolResult, (*events)[1].typ)
	for _, evt := range *events {
		assert.NotEqual(t, event.EventAgentFinalAnswer, evt.typ)
	}
}

func TestCanUsePeopleLookupSystemAdminOnly(t *testing.T) {
	assert.False(t, canUsePeopleLookup(context.Background()))

	adminCtx := context.WithValue(context.Background(), types.TenantRoleContextKey, types.TenantRoleAdmin)
	assert.False(t, canUsePeopleLookup(adminCtx))

	ownerCtx := context.WithValue(context.Background(), types.TenantRoleContextKey, types.TenantRoleOwner)
	assert.False(t, canUsePeopleLookup(ownerCtx))

	assert.True(t, canUsePeopleLookup(systemAdminCtx()))
}
