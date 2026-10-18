package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func emitTimelineStage(
	t *testing.T,
	bus *event.EventBus,
	toolCallID, toolName string,
	args map[string]any,
	result event.AgentToolResultData,
) {
	t.Helper()
	require.NoError(t, bus.Emit(context.Background(), event.Event{
		Type: event.EventAgentToolCall,
		Data: event.AgentToolCallData{ToolCallID: toolCallID, ToolName: toolName, Arguments: args},
	}))
	result.ToolCallID = toolCallID
	result.ToolName = toolName
	require.NoError(t, bus.Emit(context.Background(), event.Event{
		Type: event.EventAgentToolResult,
		Data: result,
	}))
}

// A turn that searched and cited nothing is exactly the case history replay
// could not reconstruct from knowledge_references, so it must be persisted.
func TestQuickAnswerTimelineRecorderPersistsSearchWithoutResults(t *testing.T) {
	bus := event.NewEventBus()
	msg := &types.Message{}
	registerQuickAnswerTimelineRecorder(bus, msg)

	emitTimelineStage(t, bus, "call-1", "knowledge_search",
		map[string]any{"query": "xin chào", "search_source": "knowledge"},
		event.AgentToolResultData{
			Output:   "Không tìm thấy nội dung liên quan",
			Success:  true,
			Duration: 12,
			Data:     map[string]interface{}{"count": 0, "doc_count": 0, "web_count": 0},
		})

	require.Len(t, msg.AgentSteps, 1)
	require.Len(t, msg.AgentSteps[0].ToolCalls, 1)
	call := msg.AgentSteps[0].ToolCalls[0]
	assert.Equal(t, "knowledge_search", call.Name)
	assert.Equal(t, types.PipelineToolCallIDPrefix+"call-1", call.ID)
	assert.Equal(t, "xin chào", call.Args["query"])
	require.NotNil(t, call.Result)
	assert.True(t, call.Result.Success)
	assert.Equal(t, "Không tìm thấy nội dung liên quan", call.Result.Output)
	assert.Equal(t, 0, call.Result.Data["count"])
	assert.Equal(t, int64(12), call.Duration)
}

func TestQuickAnswerTimelineRecorderKeepsStageOrderAndIgnoresOtherTools(t *testing.T) {
	bus := event.NewEventBus()
	msg := &types.Message{}
	registerQuickAnswerTimelineRecorder(bus, msg)

	emitTimelineStage(t, bus, "call-1", "query_understand", nil,
		event.AgentToolResultData{Output: "Đã hiểu xong câu hỏi", Success: true})
	emitTimelineStage(t, bus, "call-2", "web_search", nil,
		event.AgentToolResultData{Output: "ignored", Success: true})
	emitTimelineStage(t, bus, "call-3", "knowledge_search", nil,
		event.AgentToolResultData{Output: "Tìm thấy 3 nội dung liên quan", Success: true})

	require.Len(t, msg.AgentSteps, 1)
	names := make([]string, 0, len(msg.AgentSteps[0].ToolCalls))
	for _, call := range msg.AgentSteps[0].ToolCalls {
		names = append(names, call.Name)
	}
	assert.Equal(t, []string{"query_understand", "knowledge_search"}, names)
}

// A stage that never reported a result belongs to an interrupted turn. Keeping
// it out of history is what stops a reload from showing it as completed.
func TestQuickAnswerTimelineRecorderSkipsStagesWithoutResult(t *testing.T) {
	bus := event.NewEventBus()
	msg := &types.Message{}
	registerQuickAnswerTimelineRecorder(bus, msg)

	require.NoError(t, bus.Emit(context.Background(), event.Event{
		Type: event.EventAgentToolCall,
		Data: event.AgentToolCallData{ToolCallID: "call-1", ToolName: "knowledge_search"},
	}))

	assert.Empty(t, msg.AgentSteps)
}

// Reasoning and timeline stages share one step, so neither may clobber the other.
func TestQuickAnswerReasoningAndTimelineShareOneStep(t *testing.T) {
	bus := event.NewEventBus()
	msg := &types.Message{}
	registerQuickAnswerTimelineRecorder(bus, msg)

	appendQuickAnswerReasoning(msg, "Đang suy nghĩ")
	emitTimelineStage(t, bus, "call-1", "knowledge_search", nil,
		event.AgentToolResultData{Output: "Không tìm thấy nội dung liên quan", Success: true})
	appendQuickAnswerReasoning(msg, "tiếp tục")

	require.Len(t, msg.AgentSteps, 1)
	assert.Equal(t, "Đang suy nghĩtiếp tục", msg.AgentSteps[0].ReasoningContent)
	require.Len(t, msg.AgentSteps[0].ToolCalls, 1)
}

func TestQuickAnswerTimelineRecorderPersistsAbbreviationResolution(t *testing.T) {
	bus := event.NewEventBus()
	msg := &types.Message{}
	registerQuickAnswerTimelineRecorder(bus, msg)

	emitTimelineStage(t, bus, "call-1", "resolve_abbreviation",
		map[string]any{"action": "expand", "text": "XYZABC tỉnh họp"},
		event.AgentToolResultData{
			Output:  "unknown_candidate: XYZABC",
			Success: true,
			Data: map[string]interface{}{
				"result": map[string]interface{}{"potential_abbreviations": []string{"XYZABC"}},
			},
		})
	emitTimelineStage(t, bus, "call-2", "resolve_abbreviation",
		map[string]any{"action": "suggest", "short_form": "XYZABC", "full_form": "X Y Z"},
		event.AgentToolResultData{
			Output:  "recorded",
			Success: true,
			Data:    map[string]interface{}{"status": "pending_review"},
		})

	require.Len(t, msg.AgentSteps, 1)
	require.Len(t, msg.AgentSteps[0].ToolCalls, 2)
	resolve := msg.AgentSteps[0].ToolCalls[0]
	assert.Equal(t, "resolve_abbreviation", resolve.Name)
	assert.Equal(t, "expand", resolve.Args["action"])
	require.NotNil(t, resolve.Result)
	assert.Equal(t, []string{"XYZABC"},
		resolve.Result.Data["result"].(map[string]interface{})["potential_abbreviations"])
	suggest := msg.AgentSteps[0].ToolCalls[1]
	assert.Equal(t, "suggest", suggest.Args["action"])
	assert.Equal(t, "pending_review", suggest.Result.Data["status"])
}

func TestQuickAnswerTimelineRecorderPersistsPeopleLookup(t *testing.T) {
	bus := event.NewEventBus()
	msg := &types.Message{}
	registerQuickAnswerTimelineRecorder(bus, msg)

	emitTimelineStage(t, bus, "call-1", "people_lookup",
		map[string]any{"lookup_type": "phone", "query": "0989755968"},
		event.AgentToolResultData{
			Output:   "PERSON DISPLAY",
			Success:  true,
			Duration: 7,
			Data: map[string]interface{}{
				"found":       true,
				"lookup_type": "phone",
				"unavailable": false,
			},
		})

	require.Len(t, msg.AgentSteps, 1)
	require.Len(t, msg.AgentSteps[0].ToolCalls, 1)
	call := msg.AgentSteps[0].ToolCalls[0]
	assert.Equal(t, "people_lookup", call.Name)
	assert.Equal(t, types.PipelineToolCallIDPrefix+"call-1", call.ID)
	assert.Equal(t, "phone", call.Args["lookup_type"])
	assert.Equal(t, "0989755968", call.Args["query"])
	require.NotNil(t, call.Result)
	assert.True(t, call.Result.Success)
	assert.Equal(t, "PERSON DISPLAY", call.Result.Output)
	assert.Equal(t, true, call.Result.Data["found"])
	assert.Equal(t, "phone", call.Result.Data["lookup_type"])
}

// The agent hand-off card of a fast-answer turn is kept with the message, so
// it renders after a reload and history replays it as a card note.
func TestQuickAnswerTimelineRecorderPersistsAgentHandoff(t *testing.T) {
	bus := event.NewEventBus()
	msg := &types.Message{}
	registerQuickAnswerTimelineRecorder(bus, msg)

	emitTimelineStage(t, bus, "handoff-m1", "agent_handoff", map[string]any{"query": "Tạo file Word công văn"},
		event.AgentToolResultData{Output: "Agent đang dùng không tạo được file Word…", Success: true,
			Data: map[string]interface{}{"display_type": "agent_handoff", "target_agent_id": "builtin-document-assistant", "template_id": "cong-van"}})

	require.Len(t, msg.AgentSteps, 1)
	call := msg.AgentSteps[0].ToolCalls[0]
	assert.Equal(t, "agent_handoff", call.Name)
	assert.True(t, types.IsPipelineToolCallID(call.ID))
	assert.Equal(t, "cong-van", call.Result.Data["template_id"])
}
