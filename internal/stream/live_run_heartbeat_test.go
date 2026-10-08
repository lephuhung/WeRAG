package stream

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A turn whose process died kept its live-run marker forever: every 409
// check and every resume poll extended it. The heartbeat, renewed only by
// the running process, lapses instead.
func TestLiveRunHeartbeatLapsesWhenTheRunnerStops(t *testing.T) {
	mgr, mini := newTestRedisStreamManager(t, time.Hour)
	ctx := context.Background()
	require.NoError(t, mgr.SetLiveRun(ctx, "s1", "a1", "r1"))

	alive, err := mgr.LiveRunAlive(ctx, "s1", "a1")
	require.NoError(t, err)
	require.False(t, alive, "no heartbeat yet")

	ok, err := mgr.HeartbeatLiveRun(ctx, "s1", "a1")
	require.NoError(t, err)
	require.True(t, ok)
	alive, _ = mgr.LiveRunAlive(ctx, "s1", "a1")
	require.True(t, alive)

	// readers keep the marker, but the heartbeat lapses without its runner
	mini.FastForward(LiveRunHeartbeatTTL + time.Second)
	_, _, err = mgr.GetLiveRun(ctx, "s1")
	require.NoError(t, err)
	live, _, _ := mgr.GetLiveRun(ctx, "s1")
	require.Equal(t, "a1", live)
	alive, _ = mgr.LiveRunAlive(ctx, "s1", "a1")
	require.False(t, alive)
}

func TestLiveRunHeartbeatStopsWhenTheMarkerMoves(t *testing.T) {
	mgr, _ := newTestRedisStreamManager(t, time.Hour)
	ctx := context.Background()
	require.NoError(t, mgr.SetLiveRun(ctx, "s1", "a1", "r1"))
	ok, _ := mgr.HeartbeatLiveRun(ctx, "s1", "a1")
	require.True(t, ok)

	require.NoError(t, mgr.ClaimLiveRun(ctx, "s1", "a2", "r2"))
	ok, err := mgr.HeartbeatLiveRun(ctx, "s1", "a1")
	require.NoError(t, err)
	require.False(t, ok, "a handed-off run stops renewing")
	alive, _ := mgr.LiveRunAlive(ctx, "s1", "a2")
	require.False(t, alive, "the old heartbeat does not vouch for the new run")

	require.NoError(t, mgr.ClearLiveRun(ctx, "s1", "a2"))
	ok, _ = mgr.HeartbeatLiveRun(ctx, "s1", "a2")
	require.False(t, ok, "a finished run stops renewing")
}
