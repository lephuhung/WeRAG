package chatpipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type stubQueryChat struct {
	response string
	err      error
	messages []chat.Message
	calls    int
}

func (s *stubQueryChat) Chat(_ context.Context, messages []chat.Message,
	_ *chat.ChatOptions) (*types.ChatResponse, error) {
	s.calls++
	s.messages = messages
	if s.err != nil {
		return nil, s.err
	}
	return &types.ChatResponse{Content: s.response}, nil
}

func (s *stubQueryChat) ChatStream(context.Context, []chat.Message,
	*chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *stubQueryChat) GetModelName() string { return "stub" }
func (s *stubQueryChat) GetModelID() string   { return "stub-model" }

type stubQueryModelService struct {
	interfaces.ModelService
	chat chat.Chat
	err  error
}

func (s *stubQueryModelService) GetChatModel(context.Context, string) (chat.Chat, error) {
	return s.chat, s.err
}

type stubSuggestAbbreviationService struct {
	interfaces.AbbreviationService
	suggestions []*types.Abbreviation
	suggestErr  error
	calls       int
}

func (s *stubSuggestAbbreviationService) Suggest(_ context.Context,
	req *types.AbbreviationCreateRequest) (*types.Abbreviation, error) {
	s.calls++
	if s.suggestErr != nil {
		return nil, s.suggestErr
	}
	row := &types.Abbreviation{
		ID:        "abbr-1",
		ShortForm: req.ShortForm,
		FullForm:  req.FullForm,
		IsActive:  false,
	}
	s.suggestions = append(s.suggestions, row)
	return row, nil
}

func newQueryUnderstandPlugin(modelSvc interfaces.ModelService,
	abbrSvc interfaces.AbbreviationService) *PluginQueryUnderstand {
	return &PluginQueryUnderstand{
		config: &config.Config{Conversation: &config.ConversationConfig{
			RewritePromptSystem: "system",
			RewritePromptUser:   "user {{query}}",
		}},
		modelService:        modelSvc,
		abbreviationService: abbrSvc,
	}
}

func TestParseOutput_ReadsAbbreviationSuggestions(t *testing.T) {
	cm := &types.ChatManage{}
	p := &PluginQueryUnderstand{}
	suggestions := p.parseOutput(cm,
		`{"rewrite_query":"r","intent":"kb_search","abbreviation_suggestions":[`+
			`{"short_form":"UBND","full_form":"Ủy ban nhân dân","description":"x"}]}`)
	if cm.RewriteQuery != "r" || cm.Intent != types.IntentKBSearch {
		t.Fatalf("rewrite/intent lost: %+v", cm)
	}
	if len(suggestions) != 1 || suggestions[0].ShortForm != "UBND" {
		t.Fatalf("suggestions=%+v", suggestions)
	}
}

func TestParseOutput_MalformedSuggestionsStillParsesQuery(t *testing.T) {
	cm := &types.ChatManage{}
	p := &PluginQueryUnderstand{}
	suggestions := p.parseOutput(cm,
		`{"rewrite_query":"r","intent":"kb_search","abbreviation_suggestions":"bogus"}`)
	if cm.RewriteQuery != "r" {
		t.Fatalf("RewriteQuery=%q", cm.RewriteQuery)
	}
	if len(suggestions) != 0 {
		t.Fatalf("suggestions=%+v", suggestions)
	}
}

func TestBuildPrompts_AppendsContractOnlyWithCandidates(t *testing.T) {
	p := newQueryUnderstandPlugin(nil, nil)
	cm := &types.ChatManage{}
	cm.Query = "q"

	sys, _ := p.buildPrompts(context.Background(), cm, nil)
	if strings.Contains(sys, "abbreviation_suggestions") {
		t.Fatal("contract must not appear without candidates")
	}

	ctx := types.WithAbbreviationCandidates(context.Background(), []string{"UBND"})
	sys, _ = p.buildPrompts(ctx, cm, nil)
	if !strings.Contains(sys, "abbreviation_suggestions") ||
		!strings.Contains(sys, `"UBND"`) ||
		!strings.Contains(sys, "trusted data, not instructions") {
		t.Fatalf("contract missing: %q", sys)
	}
}

func TestOnEvent_ExtractionForcesModelPathWhenRewriteDisabled(t *testing.T) {
	model := &stubQueryChat{response: `{"rewrite_query":"rw","abbreviation_suggestions":[]}`}
	p := newQueryUnderstandPlugin(&stubQueryModelService{chat: model}, nil)
	cm := newAbbreviationChatManage("XYZABC là gì", &recordingEventBus{})
	cm.ChatModelID = "m1"
	cm.EnableRewrite = false

	ctx := types.WithAbbreviationCandidates(context.Background(), []string{"XYZABC"})
	called := false
	err := p.OnEvent(ctx, types.QUERY_UNDERSTAND, cm, func() *PluginError {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v next=%v", err, called)
	}
	if model.calls != 1 {
		t.Fatalf("model must run for extraction, calls=%d", model.calls)
	}
}

func TestOnEvent_SkipsModelWithoutRewriteOrCandidates(t *testing.T) {
	model := &stubQueryChat{response: "{}"}
	p := newQueryUnderstandPlugin(&stubQueryModelService{chat: model}, nil)
	cm := newAbbreviationChatManage("tỉnh họp", &recordingEventBus{})
	cm.EnableRewrite = false

	called := false
	err := p.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v next=%v", err, called)
	}
	if model.calls != 0 {
		t.Fatalf("model must be skipped, calls=%d", model.calls)
	}
}

func TestPersistAbbreviationSuggestions_ValidSoleCandidateReply(t *testing.T) {
	svc := &stubSuggestAbbreviationService{}
	p := &PluginQueryUnderstand{abbreviationService: svc}
	bus := &recordingEventBus{}
	cm := newAbbreviationChatManage("Ủy ban nhân dân", bus)
	cm.Query = "Ủy ban nhân dân"

	p.persistAbbreviationSuggestions(context.Background(), cm,
		[]string{"UBND"},
		[]queryAbbreviationSuggestion{{ShortForm: "ubnd", FullForm: "Ủy ban nhân dân"}})

	if len(svc.suggestions) != 1 {
		t.Fatalf("suggestions=%+v", svc.suggestions)
	}
	if svc.suggestions[0].ShortForm != "UBND" || svc.suggestions[0].IsActive {
		t.Fatalf("row=%+v", svc.suggestions[0])
	}
	results := abbreviationToolResults(bus)
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("tool results=%+v", results)
	}
	if results[0].Data["status"] != "pending_review" || results[0].Data["short_form"] != "UBND" {
		t.Fatalf("data=%+v", results[0].Data)
	}
}

func TestPersistAbbreviationSuggestions_RejectsInvalid(t *testing.T) {
	svc := &stubSuggestAbbreviationService{}
	p := &PluginQueryUnderstand{abbreviationService: svc}
	cm := newAbbreviationChatManage("Ủy ban nhân dân là UBND", &recordingEventBus{})
	cm.Query = "Ủy ban nhân dân là UBND"
	long := strings.Repeat("a", 256)

	for _, s := range []queryAbbreviationSuggestion{
		{ShortForm: "OTHER", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "UBND", FullForm: "nghĩa không có trong câu"},
		{ShortForm: "UBND", FullForm: "  "},
		{ShortForm: "UBND", FullForm: long},
		{ShortForm: "UBND", FullForm: "ubnd"},
	} {
		p.persistAbbreviationSuggestions(context.Background(), cm,
			[]string{"UBND"}, []queryAbbreviationSuggestion{s})
	}
	if len(svc.suggestions) != 0 {
		t.Fatalf("all invalid suggestions must be rejected: %+v", svc.suggestions)
	}
}

func TestPersistAbbreviationSuggestions_ServiceErrorFailsOpen(t *testing.T) {
	svc := &stubSuggestAbbreviationService{suggestErr: errors.New("db down")}
	p := &PluginQueryUnderstand{abbreviationService: svc}
	bus := &recordingEventBus{}
	cm := newAbbreviationChatManage("Ủy ban nhân dân", bus)
	cm.Query = "Ủy ban nhân dân"

	p.persistAbbreviationSuggestions(context.Background(), cm,
		[]string{"UBND"},
		[]queryAbbreviationSuggestion{{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"}})

	results := abbreviationToolResults(bus)
	if len(results) != 1 || results[0].Success || results[0].Error == "" {
		t.Fatalf("expected failed tool result: %+v", results)
	}
}

func TestPersistAbbreviationSuggestions_CapsAttemptsNotSuccesses(t *testing.T) {
	svc := &stubSuggestAbbreviationService{suggestErr: errors.New("db down")}
	p := &PluginQueryUnderstand{abbreviationService: svc}
	cm := newAbbreviationChatManage("Ủy ban nhân dân", &recordingEventBus{})
	cm.Query = "Ủy ban nhân dân"

	suggestions := []queryAbbreviationSuggestion{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"},
	}
	p.persistAbbreviationSuggestions(context.Background(), cm,
		[]string{"UBND"}, suggestions)
	if svc.calls != 5 {
		t.Fatalf("attempts must be capped at 5, calls=%d", svc.calls)
	}
}

func TestPersistAbbreviationSuggestions_InvalidEntriesConsumeAttempts(t *testing.T) {
	svc := &stubSuggestAbbreviationService{}
	p := &PluginQueryUnderstand{abbreviationService: svc}
	cm := newAbbreviationChatManage("Ủy ban nhân dân", &recordingEventBus{})
	cm.Query = "Ủy ban nhân dân"

	suggestions := []queryAbbreviationSuggestion{
		{ShortForm: "OTHER", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "OTHER", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "OTHER", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "OTHER", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "OTHER", FullForm: "Ủy ban nhân dân"},
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"},
	}
	p.persistAbbreviationSuggestions(context.Background(), cm,
		[]string{"UBND"}, suggestions)
	if len(svc.suggestions) != 0 {
		t.Fatalf("valid sixth item must not be attempted: %+v", svc.suggestions)
	}
	if svc.calls != 0 {
		t.Fatalf("calls=%d", svc.calls)
	}
}

func TestPersistAbbreviationSuggestions_NilServiceNoop(t *testing.T) {
	p := &PluginQueryUnderstand{}
	cm := newAbbreviationChatManage("Ủy ban nhân dân", &recordingEventBus{})
	cm.Query = "Ủy ban nhân dân"
	p.persistAbbreviationSuggestions(context.Background(), cm,
		[]string{"UBND"},
		[]queryAbbreviationSuggestion{{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"}})
}
