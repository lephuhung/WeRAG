package chatpipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

func TestPluginAbbreviationResolve_UnknownCandidateEmits(t *testing.T) {
	svc := &stubPipelineAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	p := &PluginAbbreviationResolve{abbreviationService: svc}
	bus := &recordingEventBus{}
	cm := newAbbreviationChatManage("XYZABC tỉnh họp", bus)

	called := false
	err := p.OnEvent(context.Background(), types.ABBREVIATION_RESOLVE, cm, func() *PluginError {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("OnEvent err=%v next=%v", err, called)
	}
	results := abbreviationToolResults(bus)
	if len(results) != 1 {
		t.Fatalf("expected one resolve_abbreviation result, got %d", len(results))
	}
	res, ok := results[0].Data["result"].(*abbreviation.ExpandResult)
	if !ok || len(res.Potential) != 1 || res.Potential[0] != "XYZABC" {
		t.Fatalf("result = %#v", results[0].Data["result"])
	}
	var callSeen bool
	for _, evt := range bus.events {
		if data, ok := evt.Data.(event.AgentToolCallData); ok && data.ToolName == "resolve_abbreviation" {
			callSeen = true
		}
	}
	if !callSeen {
		t.Fatal("expected a resolve_abbreviation tool_call event")
	}
}

func TestPluginAbbreviationResolve_KnownMeaningRewritesSilently(t *testing.T) {
	svc := &stubPipelineAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	p := &PluginAbbreviationResolve{abbreviationService: svc}
	bus := &recordingEventBus{}
	cm := newAbbreviationChatManage("UBND tỉnh họp", bus)

	err := p.OnEvent(context.Background(), types.ABBREVIATION_RESOLVE, cm, func() *PluginError { return nil })
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if cm.RewriteQuery != "Ủy ban nhân dân (UBND) tỉnh họp" {
		t.Fatalf("RewriteQuery=%q", cm.RewriteQuery)
	}
	if got := abbreviationToolResults(bus); len(got) != 0 {
		t.Fatalf("applied-only resolution must not emit events: %+v", got)
	}
}

func TestPluginAbbreviationResolve_FailsOpen(t *testing.T) {
	svc := &stubPipelineAbbreviationService{err: errors.New("dictionary unavailable")}
	p := &PluginAbbreviationResolve{abbreviationService: svc}
	bus := &recordingEventBus{}
	cm := newAbbreviationChatManage("UBND tỉnh họp", bus)

	called := false
	err := p.OnEvent(context.Background(), types.ABBREVIATION_RESOLVE, cm, func() *PluginError {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v next=%v", err, called)
	}
	if cm.RewriteQuery != "UBND tỉnh họp" {
		t.Fatalf("error must keep the original query: %q", cm.RewriteQuery)
	}
	if len(bus.events) != 0 {
		t.Fatalf("no events expected on failure: %+v", bus.events)
	}
}

func TestPluginAbbreviationResolve_RawQueryUnknownStillEmitted(t *testing.T) {
	svc := &stubPipelineAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	p := &PluginAbbreviationResolve{abbreviationService: svc}
	bus := &recordingEventBus{}
	cm := newAbbreviationChatManage("đơn vị này họp về gì", bus)
	cm.Query = "XYZABC tỉnh họp"

	called := false
	err := p.OnEvent(context.Background(), types.ABBREVIATION_RESOLVE, cm, func() *PluginError {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v next=%v", err, called)
	}
	if cm.RewriteQuery != "đơn vị này họp về gì" {
		t.Fatalf("effective rewrite must stay: %q", cm.RewriteQuery)
	}
	results := abbreviationToolResults(bus)
	if len(results) != 1 {
		t.Fatalf("expected one resolve_abbreviation result, got %d", len(results))
	}
	res, ok := results[0].Data["result"].(*abbreviation.ExpandResult)
	if !ok || len(res.Potential) != 1 || res.Potential[0] != "XYZABC" {
		t.Fatalf("result = %#v", results[0].Data["result"])
	}
	var callSeen bool
	for _, evt := range bus.events {
		if data, ok := evt.Data.(event.AgentToolCallData); ok && data.ToolName == "resolve_abbreviation" {
			callSeen = true
			if data.Arguments["text"] != "XYZABC tỉnh họp" {
				t.Fatalf("event provenance must be the raw query, got %v", data.Arguments["text"])
			}
		}
	}
	if !callSeen {
		t.Fatal("expected a resolve_abbreviation tool_call event")
	}
}

func TestResolveAbbreviationForTurn_OriginalErrorFailsOpen(t *testing.T) {
	svc := &stubPipelineAbbreviationService{err: errors.New("dictionary unavailable")}
	resolved, result, err := resolveAbbreviationForTurn(
		context.Background(), svc, "đơn vị này họp", "XYZABC tỉnh họp",
	)
	if err == nil {
		t.Fatal("original-query dictionary error must be returned")
	}
	if resolved != "đơn vị này họp" || result != nil {
		t.Fatalf("resolved=%q result=%+v", resolved, result)
	}
}

func TestPluginAbbreviationResolve_NoCandidateNoDictionary(t *testing.T) {
	svc := &stubPipelineAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	p := &PluginAbbreviationResolve{abbreviationService: svc}
	cm := newAbbreviationChatManage("tỉnh họp sáng nay", &recordingEventBus{})
	err := p.OnEvent(context.Background(), types.ABBREVIATION_RESOLVE, cm, func() *PluginError { return nil })
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if svc.calls != 0 {
		t.Fatalf("ListActive must not run without candidates, calls=%d", svc.calls)
	}
}
