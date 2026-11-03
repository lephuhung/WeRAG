package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
)

type fixedCompleter struct {
	reply string
	err   error
	calls int
}

func (f *fixedCompleter) Complete(context.Context, []docformat.Message) (string, error) {
	f.calls++
	return f.reply, f.err
}

// The intent model answers when it can; on an error the answering model
// labels the turn, so a down intent model never turns the grounding gate off.
func TestIntentModelFallsBackToTheAnsweringModel(t *testing.T) {
	ok := &fixedCompleter{reply: `{"lookup": true}`}
	answering := &fixedCompleter{reply: `{"lookup": false}`}
	got, err := fallbackCompleter{primary: ok, fallback: answering, primaryID: "gemma"}.Complete(context.Background(), nil)
	if err != nil || got != `{"lookup": true}` || answering.calls != 0 {
		t.Fatalf("intent model reply not used: %q %v (answering calls %d)", got, err, answering.calls)
	}

	down := &fixedCompleter{err: errors.New("connection refused")}
	got, err = fallbackCompleter{primary: down, fallback: answering, primaryID: "gemma"}.Complete(context.Background(), nil)
	if err != nil || got != `{"lookup": false}` || answering.calls != 1 {
		t.Fatalf("no fallback: %q %v (answering calls %d)", got, err, answering.calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = (fallbackCompleter{primary: down, fallback: answering, primaryID: "gemma"}).Complete(ctx, nil); err == nil || answering.calls != 1 {
		t.Fatalf("a cancelled turn must not call the answering model again (calls %d)", answering.calls)
	}
}

// A stalled intent model gets its own short budget, so the answering model
// still labels the turn inside the caller's deadline.
func TestStalledIntentModelLeavesTimeForTheFallback(t *testing.T) {
	answering := &fixedCompleter{reply: `{"lookup": true}`}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	got, err := fallbackCompleter{primary: blockingCompleter{}, fallback: answering, primaryID: "gemma",
		timeout: 50 * time.Millisecond}.Complete(ctx, nil)
	if err != nil || got != `{"lookup": true}` || answering.calls != 1 {
		t.Fatalf("fallback skipped after a stall: %q %v", got, err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("waited %v on a stalled model", time.Since(start))
	}
}

type blockingCompleter struct{}

func (blockingCompleter) Complete(ctx context.Context, _ []docformat.Message) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// Without the setting the answering model labels the turn, as before.
func TestIntentCompleterDefaultsToTheAnsweringModel(t *testing.T) {
	s := &sessionService{}
	if _, wrapped := s.intentCompleter(context.Background(), nil, "qwen").(fallbackCompleter); wrapped {
		t.Fatal("no settings: no intent model")
	}
}
