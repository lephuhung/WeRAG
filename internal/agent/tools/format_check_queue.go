package tools

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// Each background format check makes two model calls (labelling, then a
// reasoning evaluation of up to evaluateTimeout). Opening four documents at
// once used to start four of each against the same model; formatCheckSlots
// lets only a few run, and the other documents show "queued" until a slot
// frees. The check_document_format call the agent makes for the user does
// not take a slot: the user is waiting for it, the prewarms are not.
var formatCheckSlots = make(chan struct{}, envPositiveInt("WEKNORA_DOCFORMAT_CHECK_CONCURRENCY", 2))

// formatCheckQueuePoll is how often a queued check looks whether its result
// was cached meanwhile and refreshes its queued state, which
// SessionFormatCheck otherwise drops as stale after formatCheckRunTimeout.
var formatCheckQueuePoll = 5 * time.Second

func envPositiveInt(key string, def int) int {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			return v
		}
	}
	return def
}

// tryFormatCheckSlot takes a free slot without waiting.
func tryFormatCheckSlot() (release func(), ok bool) {
	slots := formatCheckSlots
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		return nil, false
	}
}

// waitFormatCheckSlot blocks until a slot is free, recording state as
// queued meanwhile. It returns release (nil when no slot was taken because
// the result key got cached while waiting, e.g. by the agent's own call).
func (t *CheckDocumentFormatTool) waitFormatCheckSlot(ctx context.Context, key string, state *types.DocumentFormatCheck) (release func(), ok bool) {
	slots := formatCheckSlots
	release = func() { <-slots }
	queued := *state
	queued.Status = types.DocumentFormatCheckQueued
	ticker := time.NewTicker(formatCheckQueuePoll)
	defer ticker.Stop()
	for {
		// a fresh copy each time: without Redis the state is kept by pointer
		st := queued
		st.StartedAt = time.Now()
		formatChecks.storeState(ctx, t.documentID, &st)
		select {
		case slots <- struct{}{}:
			return release, true
		case <-ticker.C:
			if formatChecks.get(ctx, key) != nil {
				return nil, true
			}
		case <-ctx.Done():
			return nil, false
		}
	}
}
