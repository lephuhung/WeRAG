package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

type countingCompleter struct{ calls int }

func (c *countingCompleter) Complete(context.Context, []docformat.Message) (string, error) {
	c.calls++
	return "ok", nil
}

func TestLazyCompleterResolvesOnce(t *testing.T) {
	model := &countingCompleter{}
	resolved := 0
	l := &lazyCompleter{resolve: func(context.Context) (docformat.Completer, error) { resolved++; return model, nil }}
	for i := 0; i < 2; i++ {
		if reply, err := l.Complete(context.Background(), nil); err != nil || reply != "ok" {
			t.Fatalf("reply %q err %v", reply, err)
		}
	}
	if resolved != 1 || model.calls != 2 {
		t.Fatalf("resolved %d, calls %d", resolved, model.calls)
	}
	failing := &lazyCompleter{resolve: func(context.Context) (docformat.Completer, error) { return nil, errors.New("no model") }}
	if _, err := failing.Complete(context.Background(), nil); err == nil {
		t.Fatal("a resolve error must reach the caller")
	}
}

func TestBroadQueryCardNeedsAJudge(t *testing.T) {
	s := &sessionService{legalCatalog: &legalCatalogService{}}
	req := &types.QARequest{Session: &types.Session{ID: "s"}, Query: "luật đất đai"}
	asked, err := s.broadQueryClarification(context.Background(), req, event.NewEventBus(), true, nil)
	if asked || err != nil {
		t.Fatalf("asked %v err %v without a judge", asked, err)
	}
}
