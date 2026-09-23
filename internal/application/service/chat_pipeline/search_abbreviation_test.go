package chatpipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

type stubPipelineAbbreviationService struct {
	interfaces.AbbreviationService
	actives []*types.Abbreviation
	err     error
	calls   int
}

func (s *stubPipelineAbbreviationService) ListActive(context.Context) ([]*types.Abbreviation, error) {
	s.calls++
	return s.actives, s.err
}

func newAbbreviationChatManage(query string, bus types.EventBusInterface) *types.ChatManage {
	cm := &types.ChatManage{}
	cm.RewriteQuery = query
	cm.SessionID = "sess-1"
	cm.EventBus = bus
	return cm
}

func abbreviationToolResults(bus *recordingEventBus) []event.AgentToolResultData {
	var out []event.AgentToolResultData
	for _, evt := range bus.events {
		if data, ok := evt.Data.(event.AgentToolResultData); ok && data.ToolName == "resolve_abbreviation" {
			out = append(out, data)
		}
	}
	return out
}

func TestResolveAbbreviationQuery_SingleMeaningRewrites(t *testing.T) {
	svc := &stubPipelineAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	p := &PluginSearch{abbreviationService: svc}
	bus := &recordingEventBus{}
	chatManage := newAbbreviationChatManage("UBND tỉnh họp", bus)
	p.resolveAbbreviationQuery(context.Background(), chatManage)
	if chatManage.RewriteQuery != "Ủy ban nhân dân (UBND) tỉnh họp" {
		t.Fatalf("RewriteQuery=%q", chatManage.RewriteQuery)
	}
	if got := abbreviationToolResults(bus); len(got) != 0 {
		t.Fatalf("applied-only resolution must not emit tool events: %+v", got)
	}
}

func TestResolveAbbreviationQuery_AmbiguousStaysUntouched(t *testing.T) {
	svc := &stubPipelineAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "BCH", FullForm: "Ban chấp hành", IsActive: true},
		{ShortForm: "BCH", FullForm: "Bệnh viện C Hòa", IsActive: true},
	}}
	p := &PluginSearch{abbreviationService: svc}
	chatManage := newAbbreviationChatManage("BCH đã họp", &recordingEventBus{})
	p.resolveAbbreviationQuery(context.Background(), chatManage)
	if chatManage.RewriteQuery != "BCH đã họp" {
		t.Fatalf("ambiguous query must be unchanged: %q", chatManage.RewriteQuery)
	}
}

func TestResolveAbbreviationQuery_ErrorFailsOpen(t *testing.T) {
	svc := &stubPipelineAbbreviationService{err: errors.New("dictionary unavailable")}
	p := &PluginSearch{abbreviationService: svc}
	chatManage := newAbbreviationChatManage("UBND tỉnh họp", &recordingEventBus{})
	p.resolveAbbreviationQuery(context.Background(), chatManage)
	if chatManage.RewriteQuery != "UBND tỉnh họp" {
		t.Fatalf("error must keep the original query: %q", chatManage.RewriteQuery)
	}
}

func TestResolveAbbreviationQuery_NoCandidateSkipsDictionary(t *testing.T) {
	svc := &stubPipelineAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	p := &PluginSearch{abbreviationService: svc}
	chatManage := newAbbreviationChatManage("tỉnh họp sáng nay", &recordingEventBus{})
	p.resolveAbbreviationQuery(context.Background(), chatManage)
	if chatManage.RewriteQuery != "tỉnh họp sáng nay" {
		t.Fatalf("query must be unchanged: %q", chatManage.RewriteQuery)
	}
	if svc.calls != 0 {
		t.Fatalf("ListActive must not run without candidates, calls=%d", svc.calls)
	}
}

func TestResolveAbbreviationQuery_UnknownCandidateEmitsStructuredEvent(t *testing.T) {
	svc := &stubPipelineAbbreviationService{actives: []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}}
	p := &PluginSearch{abbreviationService: svc}
	bus := &recordingEventBus{}
	chatManage := newAbbreviationChatManage("XYZABC tỉnh họp", bus)
	p.resolveAbbreviationQuery(context.Background(), chatManage)

	results := abbreviationToolResults(bus)
	if len(results) != 1 {
		t.Fatalf("expected one resolve_abbreviation result, got %d", len(results))
	}
	res, ok := results[0].Data["result"].(*abbreviation.ExpandResult)
	if !ok {
		t.Fatalf("result data = %#v", results[0].Data["result"])
	}
	if len(res.Potential) != 1 || res.Potential[0] != "XYZABC" {
		t.Fatalf("potential = %+v", res.Potential)
	}
	if !results[0].Success {
		t.Fatal("resolution result must succeed")
	}
	var callSeen bool
	for _, evt := range bus.events {
		if data, ok := evt.Data.(event.AgentToolCallData); ok && data.ToolName == "resolve_abbreviation" {
			callSeen = true
			if data.Arguments["action"] != "expand" || data.Arguments["text"] != "XYZABC tỉnh họp" {
				t.Fatalf("call args = %+v", data.Arguments)
			}
		}
	}
	if !callSeen {
		t.Fatal("expected a resolve_abbreviation tool_call event")
	}
}
