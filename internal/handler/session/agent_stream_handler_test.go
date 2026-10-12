package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// capturingStreamManager records every appended event so a handler's output
// can be asserted without a real stream backend. Embedding the interface keeps
// everything else nil-panicky so an un-stubbed call fails loudly.
type capturingStreamManager struct {
	interfaces.StreamManager
	events []interfaces.StreamEvent
}

func (s *capturingStreamManager) AppendEvent(
	_ context.Context, _, _ string, evt interfaces.StreamEvent,
) error {
	s.events = append(s.events, evt)
	return nil
}

// A failed tool execution is still a tool result: the SSE event must keep
// response_type=tool_result with success=false in its metadata, because
// response_type=error is reserved for internal agent failures (handleError).
func TestAgentStreamHandlerToolResultFailureKeepsToolResultType(t *testing.T) {
	streams := &capturingStreamManager{}
	h := NewAgentStreamHandler(
		context.Background(), "sess-1", "msg-1", "req-1", 1, time.Now(),
		&types.Message{}, streams, nil, nil, nil, nil,
	)

	err := h.handleToolResult(context.Background(), event.Event{
		ID: "evt-1", Type: event.EventAgentToolResult,
		Data: event.AgentToolResultData{
			ToolName:   "shell_exec",
			ToolCallID: "call-1",
			Success:    false,
			Error:      "exit status 1",
		},
	})
	require.NoError(t, err)
	require.Len(t, streams.events, 1)

	evt := streams.events[0]
	require.Equal(t, types.ResponseTypeToolResult, evt.Type)
	require.Equal(t, false, evt.Data["success"])
	require.Equal(t, "exit status 1", evt.Data["error"])
	require.Equal(t, "call-1", evt.Data["tool_call_id"])
}

// Internal error events keep response_type=error with done=true, so clients
// can still distinguish a crashed run from a failed tool call.
func TestAgentStreamHandlerInternalErrorKeepsErrorType(t *testing.T) {
	streams := &capturingStreamManager{}
	h := NewAgentStreamHandler(
		context.Background(), "sess-1", "msg-1", "req-1", 1, time.Now(),
		&types.Message{}, streams, nil, nil, nil, nil,
	)

	err := h.handleError(context.Background(), event.Event{
		ID: "evt-err", Type: event.EventError,
		Data: event.ErrorData{Stage: "think", Error: "llm unavailable"},
	})
	require.NoError(t, err)
	require.Len(t, streams.events, 1)

	evt := streams.events[0]
	require.Equal(t, types.ResponseTypeError, evt.Type)
	require.True(t, evt.Done)
}

// Several searches in one agent turn often return the same chunk: the answer
// keeps (and streams) each reference once.
func TestAgentStreamHandlerReferencesDeduplicated(t *testing.T) {
	streams := &capturingStreamManager{}
	msg := &types.Message{}
	h := NewAgentStreamHandler(
		context.Background(), "sess-1", "msg-1", "req-1", 1, time.Now(),
		msg, streams, nil, nil, nil, nil,
	)
	send := func(ids ...string) {
		refs := make([]*types.SearchResult, 0, len(ids))
		for _, id := range ids {
			refs = append(refs, &types.SearchResult{ID: id, KnowledgeTitle: "T" + id})
		}
		require.NoError(t, h.handleReferences(context.Background(), event.Event{
			ID: "r", Type: event.EventAgentReferences, Data: event.AgentReferencesData{References: refs},
		}))
	}
	send("c1", "c2")
	send("c2", "c3")

	require.Len(t, streams.events, 2)
	last := streams.events[1]
	require.Equal(t, types.ResponseTypeReferences, last.Type)
	refs := last.Data["references"].(types.References)
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	require.Equal(t, []string{"c1", "c2", "c3"}, ids)
	require.Len(t, msg.KnowledgeReferences, 3)
}
