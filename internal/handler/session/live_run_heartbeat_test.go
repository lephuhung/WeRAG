package session

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

type interruptedMessages struct {
	interfaces.MessageService
	msgs map[string]*types.Message
}

func (m *interruptedMessages) GetMessage(_ context.Context, _ string, id string) (*types.Message, error) {
	return m.msgs[id], nil
}

func (m *interruptedMessages) UpdateMessage(_ context.Context, msg *types.Message) error {
	m.msgs[msg.ID] = msg
	return nil
}

func TestClearDeadLiveRun(t *testing.T) {
	mini := miniredis.RunT(t)
	mgr, err := stream.NewRedisStreamManager(mini.Addr(), "", "", 0, "test", time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close() })
	msgs := &interruptedMessages{msgs: map[string]*types.Message{
		"dead": {ID: "dead", SessionID: "s", Role: "assistant"},
	}}
	h := &Handler{streamManager: mgr, messageService: msgs}
	ctx := context.Background()

	// a run killed with its process: marker left, no heartbeat
	require.NoError(t, mgr.SetLiveRun(ctx, "s", "dead", "r"))
	require.True(t, h.clearDeadLiveRun(ctx, "s"))
	live, _, _ := mgr.GetLiveRun(ctx, "s")
	require.Empty(t, live)
	require.True(t, msgs.msgs["dead"].IsCompleted)
	require.Equal(t, interruptedTurnContent, msgs.msgs["dead"].Content)

	// a run still generating keeps the session
	require.NoError(t, mgr.SetLiveRun(ctx, "s", "alive", "r2"))
	h.startLiveRunHeartbeat(ctx, "s", "alive")
	require.False(t, h.clearDeadLiveRun(ctx, "s"))
	live, _, _ = mgr.GetLiveRun(ctx, "s")
	require.Equal(t, "alive", live)

	require.False(t, h.clearDeadLiveRun(ctx, "no-run"))
}
