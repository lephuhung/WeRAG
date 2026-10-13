package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

type recordBus struct{ events []types.Event }

func (r *recordBus) On(types.EventType, types.EventHandler) {}
func (r *recordBus) Emit(_ context.Context, evt types.Event) error {
	r.events = append(r.events, evt)
	return nil
}

func answerEvent(content string, done bool) types.Event {
	return types.Event{ID: "a1", Type: types.EventType(event.EventAgentFinalAnswer),
		Data: event.AgentFinalAnswerData{Content: content, Done: done}}
}

func TestLegalHistoryBusAppendsBeforeDone(t *testing.T) {
	inner := &recordBus{}
	var gotAnswer string
	bus := newLegalHistoryBus(inner, func(_ context.Context, ids []string, answer string) string {
		gotAnswer = answer
		return "\n\n---\n**Lịch sử sửa đổi**\n- x"
	}, func() []string { return []string{"c1"} })

	_ = bus.Emit(context.Background(), answerEvent("Phạt ", false))
	_ = bus.Emit(context.Background(), answerEvent("10 triệu.", true))

	if gotAnswer != "Phạt 10 triệu." {
		t.Fatalf("history saw %q", gotAnswer)
	}
	var contents []string
	for _, e := range inner.events {
		d := e.Data.(event.AgentFinalAnswerData)
		contents = append(contents, d.Content)
		if d.Done && e.ID != "a1" {
			t.Fatalf("done on wrong id %s", e.ID)
		}
	}
	if len(inner.events) != 4 || contents[1] != "10 triệu." || contents[2] == "" || contents[3] != "" ||
		!inner.events[3].Data.(event.AgentFinalAnswerData).Done {
		t.Fatalf("unexpected events %q", contents)
	}
}

func TestLegalHistoryBusPassesThrough(t *testing.T) {
	inner := &recordBus{}
	calls := 0
	bus := newLegalHistoryBus(inner, func(context.Context, []string, string) string {
		calls++
		return ""
	}, func() []string { return []string{"c1"} })

	_ = bus.Emit(context.Background(), answerEvent("Không có.", true))
	fallback := answerEvent("fallback", true)
	fallback.Data = event.AgentFinalAnswerData{Content: "fallback", Done: true, IsFallback: true}
	_ = bus.Emit(context.Background(), fallback)

	if calls != 1 || len(inner.events) != 2 {
		t.Fatalf("calls=%d events=%d", calls, len(inner.events))
	}
}
