package im

import (
	"context"
	"strings"
	"testing"
)

// imStreamDisplayState mirrors the display buffers in handleMessageStream for lifecycle tests.
type imStreamDisplayState struct {
	useAgent          bool
	agentDone         bool
	agentInner        streamSection
	agentLiveAnswer   strings.Builder
	answerOuter       strings.Builder
	agentToolSteps    []IMToolStep
	agentToolIdx      map[string]int
	pipelineToolSteps []IMToolStep
	pipelineIdx       map[string]int
}

func newIMStreamDisplayState(useAgent bool) *imStreamDisplayState {
	return &imStreamDisplayState{
		useAgent:     useAgent,
		agentToolIdx: make(map[string]int),
		pipelineIdx:  make(map[string]int),
	}
}

func (s *imStreamDisplayState) retractAgentLiveAnswer() {
	if s.agentLiveAnswer.Len() == 0 {
		return
	}
	if s.agentInner.text.Len() > 0 {
		s.agentInner.ensureNewlineBefore()
	}
	s.agentInner.write(s.agentLiveAnswer.String())
	s.agentLiveAnswer.Reset()
}

func (s *imStreamDisplayState) parts() IMStreamParts {
	mode := IMStreamModeQuickQA
	if s.useAgent {
		mode = IMStreamModeAgent
	}
	return IMStreamParts{
		Mode:              mode,
		PipelineToolSteps: s.pipelineToolSteps,
		AgentInner:        s.agentInner.text.String(),
		AgentToolSteps:    s.agentToolSteps,
		LiveAnswer:        s.agentLiveAnswer.String(),
		Answer:            s.answerOuter.String(),
	}
}

func (s *imStreamDisplayState) intermediate() string {
	return FormatIMIntermediateFromParts(s.parts(), s.useAgent && !s.agentDone)
}

func (s *imStreamDisplayState) final() string {
	return FormatIMFinalFromParts(s.parts())
}

// TestIMStreamLifecycle_agentToolRetract simulates handleMessageStream display assembly:
// live answer → tool retract → tool result → final answer → complete.
func TestIMStreamLifecycle_agentToolRetract(t *testing.T) {
	state := newIMStreamDisplayState(true)

	state.agentLiveAnswer.WriteString("Được, để tôi tra cứu kho tri thức trước.")
	liveOnly := state.intermediate()
	if liveOnly != "Được, để tôi tra cứu kho tri thức trước." {
		t.Fatalf("live answer phase = %q", liveOnly)
	}
	if strings.Contains(liveOnly, "Quá trình suy nghĩ") {
		t.Fatal("think header must not appear before tool retract")
	}

	state.retractAgentLiveAnswer()
	upsertIMToolStep(&state.agentToolSteps, state.agentToolIdx, "tool-1", func(step *IMToolStep) {
		step.ToolName = "grep_chunks"
		step.Pending = true
	})
	duringTools := state.intermediate()
	if !strings.Contains(duringTools, "Quá trình suy nghĩ") {
		t.Fatalf("after retract should show think block, got: %q", duringTools)
	}
	if !strings.Contains(duringTools, "Được, để tôi tra cứu kho tri thức trước") {
		t.Fatalf("retracted preamble missing, got: %q", duringTools)
	}

	upsertIMToolStep(&state.agentToolSteps, state.agentToolIdx, "tool-1", func(step *IMToolStep) {
		step.ToolName = "grep_chunks"
		step.Pending = false
		step.Success = true
	})
	state.agentLiveAnswer.WriteString("Theo kết quả tra cứu, Nghị định 30/2020 quy định về công tác văn thư.")
	withLiveAnswer := state.intermediate()
	if !strings.Contains(withLiveAnswer, "Theo kết quả tra cứu") {
		t.Fatalf("live answer after tools missing, got: %q", withLiveAnswer)
	}

	state.agentDone = true
	state.answerOuter.WriteString("Nghị định 30/2020 quy định về công tác văn thư.")
	final := state.final()
	if !strings.Contains(final, "Nghị định 30/2020 quy định về công tác văn thư.") {
		t.Fatalf("final answer missing, got: %q", final)
	}

	rec := &recordingStreamSender{}
	ctx := context.Background()
	incoming := &IncomingMessage{Platform: PlatformWeCom, UserID: "u1"}
	streamID, err := rec.StartStream(ctx, incoming)
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	if err := rec.UpdateStreamContent(ctx, incoming, streamID, duringTools); err != nil {
		t.Fatalf("UpdateStreamContent: %v", err)
	}
	if err := rec.FinalizeStream(ctx, incoming, streamID, final); err != nil {
		t.Fatalf("FinalizeStream: %v", err)
	}
	if err := rec.EndStream(ctx, incoming, streamID); err != nil {
		t.Fatalf("EndStream: %v", err)
	}

	chunks, finalized, ended := rec.snapshot()
	if !ended {
		t.Fatal("stream should end")
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 intermediate update, got %d", len(chunks))
	}
	if chunks[0] == finalized {
		t.Fatal("intermediate and final should differ")
	}
}

// TestIMStreamLifecycle_quickQAPipeline verifies quick-QA pipeline steps collapse to answer-only final.
func TestIMStreamLifecycle_quickQAPipeline(t *testing.T) {
	state := newIMStreamDisplayState(false)

	upsertIMToolStep(&state.pipelineToolSteps, state.pipelineIdx, "qu-1", func(step *IMToolStep) {
		step.ToolName = "query_understand"
		step.Pending = false
		step.Success = true
	})
	upsertIMToolStep(&state.pipelineToolSteps, state.pipelineIdx, "ks-1", func(step *IMToolStep) {
		step.ToolName = "knowledge_search"
		step.Pending = true
		step.Arguments = map[string]any{"query": "Nghị định 30"}
	})

	intermediate := state.intermediate()
	if intermediate == "" {
		t.Fatal("pipeline progress should be visible")
	}
	if strings.Contains(intermediate, "Quá trình suy nghĩ") {
		t.Fatalf("quick QA must not use agent think header, got: %q", intermediate)
	}

	state.answerOuter.WriteString("Đáp án là B.")
	final := state.final()
	if final != "Đáp án là B." {
		t.Fatalf("final = %q, want answer only", final)
	}
}
