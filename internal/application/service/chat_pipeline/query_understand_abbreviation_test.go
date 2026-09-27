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
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
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

func newQueryUnderstandPlugin(modelSvc interfaces.ModelService) *PluginQueryUnderstand {
	return &PluginQueryUnderstand{
		config: &config.Config{Conversation: &config.ConversationConfig{
			RewritePromptSystem: "system",
			RewritePromptUser:   "user {{query}}",
		}},
		modelService: modelSvc,
	}
}

func boundAbbreviationManage(t *testing.T, query string) *types.ChatManage {
	t.Helper()
	r := readyAbbreviation(t, query)
	cm := newAbbreviationChatManage(query, &recordingEventBus{})
	res := r
	cm.AbbreviationResolution = &res
	cm.ChatModelID = "m1"
	return cm
}

func markerPromptQuery(t *testing.T, model *stubQueryChat) string {
	t.Helper()
	if len(model.messages) != 2 {
		t.Fatalf("messages=%d", len(model.messages))
	}
	return model.messages[1].Content
}

func TestParseOutput_IgnoresSuggestionPayload(t *testing.T) {
	cm := &types.ChatManage{}
	p := &PluginQueryUnderstand{}
	p.parseOutput(cm,
		`{"rewrite_query":"r","intent":"kb_search","abbreviation_suggestions":[`+
			`{"short_form":"UBND","full_form":"Ủy ban nhân dân","description":"x"}],`+
			`"status":"ready"}`)
	if cm.RewriteQuery != "r" || cm.Intent != types.IntentKBSearch {
		t.Fatalf("rewrite/intent lost: %+v", cm)
	}
	if cm.AbbreviationResolution != nil {
		t.Fatal("model output must never mint a resolution")
	}
}

func TestOnEvent_RewriteDisabledKeepsEffectiveQuery(t *testing.T) {
	model := &stubQueryChat{response: "{}"}
	p := newQueryUnderstandPlugin(&stubQueryModelService{chat: model})
	cm := boundAbbreviationManage(t, "ATTT có yêu cầu gì")
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
		t.Fatalf("model must be skipped when rewrite is disabled, calls=%d", model.calls)
	}
	if cm.RewriteQuery != cm.AbbreviationResolution.EffectiveQuery {
		t.Fatalf("RewriteQuery=%q, want effective %q",
			cm.RewriteQuery, cm.AbbreviationResolution.EffectiveQuery)
	}
}

func TestOnEvent_ProtectedRewriteRestoresMarkers(t *testing.T) {
	r := abbreviation.Inspect("ATTT có yêu cầu gì", []*types.Abbreviation{
		{ID: "m1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
	})
	p, err := abbreviation.ProtectRewrite(r)
	if err != nil {
		t.Fatalf("ProtectRewrite: %v", err)
	}
	// The model faithfully rewrites while keeping the marker verbatim.
	model := &stubQueryChat{
		response: `{"rewrite_query":"các yêu cầu của ` + p.Text()[:len(p.Text())-len(" có yêu cầu gì")] + ` là gì","intent":"kb_search"}`,
	}
	plugin := newQueryUnderstandPlugin(&stubQueryModelService{chat: model})
	cm := boundAbbreviationManage(t, "ATTT có yêu cầu gì")
	cm.EnableRewrite = true

	called := false
	perr := plugin.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError {
		called = true
		return nil
	})
	if perr != nil || !called {
		t.Fatalf("err=%v next=%v", perr, called)
	}
	if strings.Contains(cm.RewriteQuery, "⟦ABBR-") {
		t.Fatalf("markers must be restored, not leaked: %q", cm.RewriteQuery)
	}
	if !strings.Contains(cm.RewriteQuery, "An toàn thông tin (ATTT)") {
		t.Fatalf("validated expansion missing: %q", cm.RewriteQuery)
	}
	// The model must have seen the marker, never the raw abbreviation.
	if strings.Contains(markerPromptQuery(t, model), "ATTT") {
		t.Fatal("model must not see the raw abbreviation")
	}
}

func TestOnEvent_MarkerTamperingFallsBackToEffective(t *testing.T) {
	model := &stubQueryChat{
		// The model replaced the marker with a different expansion.
		response: `{"rewrite_query":"An toàn thực phẩm có yêu cầu gì","intent":"kb_search"}`,
	}
	p := newQueryUnderstandPlugin(&stubQueryModelService{chat: model})
	cm := boundAbbreviationManage(t, "ATTT có yêu cầu gì")
	cm.EnableRewrite = true

	called := false
	err := p.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v next=%v", err, called)
	}
	want := cm.AbbreviationResolution.EffectiveQuery
	if cm.RewriteQuery != want {
		t.Fatalf("RewriteQuery=%q, want validated fallback %q", cm.RewriteQuery, want)
	}
}

func TestOnEvent_NotReadyResolutionBlocks(t *testing.T) {
	model := &stubQueryChat{response: "{}"}
	p := newQueryUnderstandPlugin(&stubQueryModelService{chat: model})
	cm := newAbbreviationChatManage("XYZABC là gì", &recordingEventBus{})
	cm.ChatModelID = "m1"
	cm.EnableRewrite = true
	bad := abbreviation.Inspect("XYZABC là gì", nil)
	cm.AbbreviationResolution = &bad

	called := false
	err := p.OnEvent(context.Background(), types.QUERY_UNDERSTAND, cm, func() *PluginError {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("non-ready resolution must fail closed: err=%v next=%v", err, called)
	}
	if model.calls != 0 {
		t.Fatalf("model must not be invoked, calls=%d", model.calls)
	}
}
