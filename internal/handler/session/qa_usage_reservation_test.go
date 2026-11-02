package session

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// reservingGuard grants every turn with a reservation it counts releases of.
type reservingGuard struct {
	interfaces.UsageGuard
	released atomic.Int32
}

func (g *reservingGuard) CheckTurn(context.Context, uint64, string, string, types.SessionCapMode) (*types.UsageTurnGrant, error) {
	return &types.UsageTurnGrant{Release: func() { g.released.Add(1) }}, nil
}

// busyLiveRun reports another turn running in the session.
type busyLiveRun struct{ liveRunStreams }

func (busyLiveRun) GetLiveRun(context.Context, string) (string, string, error) {
	return "other-assistant", "", nil
}

// A turn refused because another one is running (a Guest's double click)
// gives back the question the usage check reserved, at once: it would
// otherwise hold a place under the questions cap until the reservation
// expired.
func TestExecuteQAReleasesTheReservationWhenAnotherTurnRuns(t *testing.T) {
	h, _, _, streams := newAbbrevGateHandler()
	h.streamManager = busyLiveRun{liveRunStreams{streams}}
	guard := &reservingGuard{}
	h.usageGuard = guard
	reqCtx := newAbbrevReqCtx("Mức phạt vượt đèn đỏ?")
	reqCtx.skipSSE = true

	h.executeQA(reqCtx, qaModeAgent, false)
	require.EqualValues(t, 1, guard.released.Load())
}
