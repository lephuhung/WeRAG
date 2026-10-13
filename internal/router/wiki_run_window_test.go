package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type fakePauser struct {
	paused         bool
	pauses, resume int
}

func (f *fakePauser) PauseQueue(string) error {
	f.pauses++
	if f.paused {
		return errors.New(`queue "wiki" is already paused`)
	}
	f.paused = true
	return nil
}

func (f *fakePauser) UnpauseQueue(string) error {
	f.resume++
	if !f.paused {
		return errors.New(`queue "wiki" is not paused`)
	}
	f.paused = false
	return nil
}

func TestWikiRunWindowGateActsOnTransitions(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, loc)
	p := &fakePauser{paused: true} // left paused by another replica
	g := &wikiRunWindowGate{
		pauser: p,
		window: func(context.Context) types.WikiRunWindow { return types.DefaultWikiRunWindow() },
		now:    func() time.Time { return now },
	}
	ctx := context.Background()

	g.tick(ctx) // daytime: pause, "already paused" is fine
	if !p.paused || !g.applied || g.lastOpen {
		t.Fatalf("daytime should pause: %+v %+v", p, g)
	}
	g.tick(ctx)
	if p.pauses != 1 {
		t.Fatalf("steady state must not touch the queue, pauses=%d", p.pauses)
	}

	now = time.Date(2026, 10, 12, 19, 0, 30, 0, loc)
	g.tick(ctx)
	if p.paused || !g.lastOpen {
		t.Fatal("evening should resume the queue")
	}

	p.paused = true // operator pauses manually during the window
	g.tick(ctx)
	if !p.paused {
		t.Fatal("manual pause inside the window must survive until the next edge")
	}
}
