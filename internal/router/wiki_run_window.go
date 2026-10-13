package router

import (
	"context"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// wikiRunWindowTick bounds how late the queue reacts to the window edges and
// to SuperAdmin edits of the setting.
const wikiRunWindowTick = 30 * time.Second

// queuePauser is the slice of *asynq.Inspector the scheduler needs.
type queuePauser interface {
	PauseQueue(queue string) error
	UnpauseQueue(queue string) error
}

// wikiRunWindowGate pauses the wiki queue outside the run window and resumes
// it inside. Pausing stops new dequeues only: a batch already running
// finishes, and enqueued/scheduled wiki tasks wait in Redis for the window.
// It only acts on transitions, so a manual pause set mid-window survives until
// the next edge. Every replica runs a gate; pause/unpause are idempotent
// Redis writes, so they agree without coordination.
type wikiRunWindowGate struct {
	pauser   queuePauser
	window   func(ctx context.Context) types.WikiRunWindow
	now      func() time.Time
	applied  bool
	lastOpen bool
}

func (g *wikiRunWindowGate) tick(ctx context.Context) {
	open := g.window(ctx).Open(g.now())
	if g.applied && open == g.lastOpen {
		return
	}
	var err error
	if open {
		err = g.pauser.UnpauseQueue(types.QueueWiki)
		if err != nil && strings.Contains(err.Error(), "not paused") {
			err = nil
		}
	} else {
		err = g.pauser.PauseQueue(types.QueueWiki)
		if err != nil && strings.Contains(err.Error(), "already paused") {
			err = nil
		}
	}
	if err != nil {
		logger.Warnf(ctx, "[WikiRunWindow] set wiki queue open=%v failed: %v", open, err)
		return // retried next tick
	}
	if open {
		logger.Infof(ctx, "[WikiRunWindow] wiki queue resumed")
	} else {
		logger.Infof(ctx, "[WikiRunWindow] wiki queue paused until the run window opens")
	}
	g.applied, g.lastOpen = true, open
}

// StartWikiRunWindow runs the gate until the returned stop func is called.
func StartWikiRunWindow(pauser queuePauser, settings interfaces.SystemSettingService) (stop func()) {
	g := &wikiRunWindowGate{
		pauser: pauser,
		window: func(ctx context.Context) types.WikiRunWindow { return types.ResolveWikiRunWindow(ctx, settings) },
		now:    time.Now,
	}
	done := make(chan struct{})
	go func() {
		ctx := context.Background()
		g.tick(ctx)
		ticker := time.NewTicker(wikiRunWindowTick)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				g.tick(ctx)
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
