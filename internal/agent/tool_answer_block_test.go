package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taxCall(ok bool, block string) types.ToolCall {
	return types.ToolCall{Name: "compare_tax_methods", Result: &types.ToolResult{
		Success: ok, Output: "…", Data: map[string]interface{}{types.ToolDataAnswerBlock: block},
	}}
}

// two calls: the block of the last successful one, once
func taxSteps() []types.AgentStep {
	return []types.AgentStep{
		{ToolCalls: []types.ToolCall{taxCall(true, "Lần 1\nGiả định:\n- a")}},
		{ToolCalls: []types.ToolCall{taxCall(true, "Lần 2\nGiả định:\n- b"), taxCall(false, "lỗi")}},
		{ToolCalls: []types.ToolCall{{Name: "search_knowledge", Result: &types.ToolResult{Success: true}}}},
	}
}

const wantTaxBlock = types.ToolAnswerBlockMarker + "Lần 2\nGiả định:\n- b\n"

func TestToolAnswerBlockOnNaturalStopBeforeFooter(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Kê khai có lợi hơn.", Done: true, FinishReason: "stop"},
	}}}}
	engine := newTestEngine(t, model)
	engine.toolRegistry = agentRegistryForTest(t, "counting_tool")
	withDomainFooter(engine)
	events := recordAnswerEvents(t, engine)

	state := &types.AgentState{RoundSteps: taxSteps()}
	messages := []chat.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "tính thuế"}}
	_, err := engine.executeLoop(context.Background(), state, "tính thuế", messages,
		engine.buildToolsForLLM(), "sess", "msg")
	require.NoError(t, err)

	assert.Equal(t, "Kê khai có lợi hơn."+wantTaxBlock+testDomainFooter, state.FinalAnswer)
	assert.Equal(t, 1, strings.Count(state.FinalAnswer, "Giả định:"))
	assertFooterLastBeforeDone(t, events())
}

func TestToolAnswerBlockOnSynthesis(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Content: "Tổng hợp."},
		{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "stop"},
	}}}}
	engine := newTestEngine(t, model)
	withDomainFooter(engine)
	events := recordAnswerEvents(t, engine)

	state := &types.AgentState{RoundSteps: taxSteps()}
	require.NoError(t, engine.streamFinalAnswerToEventBus(context.Background(), "q", state, "sess", emptyMessages()))
	assert.Equal(t, "Tổng hợp."+wantTaxBlock+testDomainFooter, state.FinalAnswer)
	assertFooterLastBeforeDone(t, events())
}

func TestToolAnswerBlockSkips(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	content := collectAnswer(t, engine)
	for name, state := range map[string]*types.AgentState{
		"no evidence":   {FinalAnswer: ungroundedAnswer, RoundSteps: taxSteps()},
		"already there": {FinalAnswer: "x" + wantTaxBlock, RoundSteps: taxSteps()},
		"no tool":       {FinalAnswer: "x"},
		"failed only":   {FinalAnswer: "x", RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{taxCall(false, "b")}}}},
	} {
		before := state.FinalAnswer
		engine.appendToolAnswerBlocks(context.Background(), state, "sess", "answer-1")
		assert.Equal(t, before, state.FinalAnswer, name)
	}
	assert.Empty(t, *content)
}

func TestToolAnswerBlocksKeepDistinctCalculations(t *testing.T) {
	call := func(args map[string]interface{}, block string) types.ToolCall {
		c := taxCall(true, block)
		c.Args = args
		return c
	}
	a := map[string]interface{}{"doanh_thu": 8e9, "thue_suat": 0.2}
	b := map[string]interface{}{"thue_suat": 0.17, "doanh_thu": 8e9}
	steps := []types.AgentStep{
		{ToolCalls: []types.ToolCall{call(a, "A cũ")}},
		{ToolCalls: []types.ToolCall{call(b, "B")}},
		// same arguments as the first (key order differs): counts once, the last
		{ToolCalls: []types.ToolCall{call(map[string]interface{}{"thue_suat": 0.2, "doanh_thu": 8e9}, "A")}},
	}
	got := toolAnswerBlocks(steps)
	assert.Equal(t, "**Lần tính 1/2**\nB\n\n**Lần tính 2/2**\nA", got)
}

func TestToolAnswerBlockOnSynthesisFallbackBringsTheFooter(t *testing.T) {
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{
		{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "length"},
	}}}}
	engine := newTestEngine(t, model)
	withDomainFooter(engine)
	state := &types.AgentState{RoundSteps: taxSteps()}
	require.NoError(t, engine.streamFinalAnswerToEventBus(context.Background(), "q", state, "sess", emptyMessages()))
	assert.Contains(t, state.FinalAnswer, wantTaxBlock)
	assert.True(t, strings.HasSuffix(state.FinalAnswer, testDomainFooter), state.FinalAnswer)
}
