package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// streamFinalAnswerToEventBus streams the final answer generation through EventBus
func (e *AgentEngine) streamFinalAnswerToEventBus(
	ctx context.Context,
	query string,
	state *types.AgentState,
	sessionID string,
	conversation []chat.Message,
) error {
	// Final-answer synthesis is a model call and an answer-emission point;
	// it must not run for a turn whose sealed abbreviation state is absent
	// or invalid. The natural-stop path and the max-iteration fallback both
	// funnel through here.
	if err := e.checkAbbreviationTurn(ctx); err != nil {
		return err
	}
	totalToolCalls := countTotalToolCalls(state.RoundSteps)
	logger.Infof(ctx, "[Agent][FinalAnswer] Synthesizing from %d steps, %d tool calls",
		len(state.RoundSteps), totalToolCalls)
	common.PipelineInfo(ctx, "Agent", "final_answer_start", map[string]interface{}{
		"session_id":   sessionID,
		"query":        query,
		"steps":        len(state.RoundSteps),
		"tool_results": totalToolCalls,
	})

	// a turn that must rest on evidence and got none is not synthesized
	// from the model's memory either (see grounding_gate.go)
	if isUngrounded(markUngrounded(ctx, state.RoundSteps)) {
		answerID := generateEventID("answer")
		answer := ungroundedAnswerFor(ctx)
		for _, d := range []event.AgentFinalAnswerData{{Content: answer}, {Done: true}} {
			e.eventBus.Emit(ctx, event.Event{
				ID: answerID, Type: event.EventAgentFinalAnswer, SessionID: sessionID, Data: d,
			})
		}
		state.FinalAnswer = answer
		return nil
	}

	// Reuse the live transcript, including history, images, compaction and steer
	// messages. Tool output must retain its role and call ID; never promote it
	// into user instructions during error recovery or iteration-limit synthesis.
	messages := append([]chat.Message(nil), conversation...)
	messages = append(messages, chat.Message{
		Role: "user",
		Content: "Tool execution has ended for this run. Respond to the current task, including " +
			"the latest user corrections and source restrictions in the conversation. Base claims on " +
			"the evidence actually obtained; distinguish completed work from remaining work and explain " +
			"any missing evidence. Use the user's requested language and format. Do not claim that an " +
			"unperformed action succeeded.",
	})

	// Generate a single ID for this entire final answer stream
	answerID := generateEventID("answer")
	logger.Debugf(ctx, "[Agent][FinalAnswer] AnswerID: %s", answerID)
	ctx = withTurnButtons(ctx, state.RoundSteps)
	ctx = e.withNumberCheck(ctx, state, query)
	emitBus := func(content string) {
		e.eventBus.Emit(ctx, event.Event{
			ID:        answerID,
			Type:      event.EventAgentFinalAnswer,
			SessionID: sessionID,
			Data:      event.AgentFinalAnswerData{Content: content},
		})
	}
	// the button guard emits through the number guard (answer_numbers.go)
	emitChunk := emitBus
	numbers := newNumberGuard(ctx, emitBus)
	if numbers != nil {
		emitChunk = numbers.Write
	}
	guard := newAnswerGuard(ctx, emitChunk)

	budget := e.clampCompletionBudgetToContext(e.tokenEstimator.EstimateMessages(messages))
	noThinking := false
	llmResult, err := e.streamLLMToEventBus(
		ctx,
		messages,
		&chat.ChatOptions{
			Temperature:         e.config.Temperature,
			MaxCompletionTokens: budget,
			PromptCacheKey:      sessionID,
			ToolChoice:          "none",
			// Thinking disabled for final answer synthesis: left to the
			// model default, Qwen3.6 spent the whole budget reasoning and
			// answered nothing (2026-10-16).
			Thinking: &noThinking,
		},
		func(chunk *types.StreamResponse, fullContent string) {
			// Defensive filter: only emit answer content, skip thinking chunks
			if chunk.ResponseType == types.ResponseTypeThinking {
				return
			}
			// The terminal event waits for the amendment history below.
			if chunk.Content != "" {
				logger.Debugf(ctx, "[Agent][FinalAnswer] Emitting answer chunk: %d chars", len(chunk.Content))
				if guard != nil {
					guard.Write(chunk.Content)
				} else {
					emitChunk(chunk.Content)
				}
			}
			if chunk.Done && guard != nil {
				guard.Flush()
			}
			if chunk.Done && numbers != nil {
				numbers.Flush()
			}
		},
	)
	if guard != nil {
		guard.Flush()
	}
	if numbers != nil {
		numbers.Flush()
	}
	if err != nil {
		logger.Errorf(ctx, "[Agent][FinalAnswer] Final answer generation failed: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_stream_failed", map[string]interface{}{
			"session_id": sessionID,
			"error":      err.Error(),
		})
		return err
	}

	// Safety net: strip any residual <think> blocks that may have leaked through
	state.FinalAnswer = cleanAnswerNumbers(ctx, cleanAnswerInContext(ctx, agenttools.StripThinkBlocks(llmResult.Content)))
	synthesized := strings.TrimSpace(state.FinalAnswer) != ""
	if !synthesized {
		// never end a turn on an empty answer: say what was gathered
		logger.Warnf(ctx, "[Agent][FinalAnswer] Synthesis returned no text; emitting the fallback")
		state.FinalAnswer = e.synthesisFallback(state)
		emitChunk(state.FinalAnswer)
	}
	modelText := state.FinalAnswer
	e.appendLegalHistory(ctx, state, sessionID, answerID)
	e.appendWikiCaveat(ctx, state, sessionID, answerID)
	e.appendSourceEditNote(ctx, state, sessionID, answerID, query)
	if synthesized {
		// the code-built fallback names no draft of its own
		e.appendDraftNotice(ctx, state, sessionID, answerID, modelText)
		e.appendScopeNotice(ctx, state, sessionID, answerID, query)
	}
	// computed results reach the user even on the fallback answer, and
	// then the pack's footer goes with them
	withResult := e.appendToolAnswerBlocks(ctx, state, sessionID, answerID)
	if synthesized || withResult {
		// the code-built fallback is not advice from a pack
		e.appendDomainFooter(ctx, state, sessionID, answerID)
	}
	e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data: event.AgentFinalAnswerData{
			Content: "",
			Done:    true,
		},
	})

	// The synthesis call is often the largest of the turn — fold its usage
	// into the turn aggregate like every ReAct round.
	if llmResult.Usage != nil {
		state.TurnUsage.Accumulate(*llmResult.Usage)
	}

	logger.Infof(ctx, "[Agent][FinalAnswer] Final answer generated: %d characters", len(state.FinalAnswer))
	common.PipelineInfo(ctx, "Agent", "final_answer_done", map[string]interface{}{
		"session_id": sessionID,
		"answer_len": len(state.FinalAnswer),
	})
	return nil
}

// handleMaxIterations generates a final answer when the agent loop exhausted all iterations
// without the LLM producing a natural stop. It marks state.IsComplete = true.
func (e *AgentEngine) handleMaxIterations(
	ctx context.Context, query string, state *types.AgentState, sessionID string, messages []chat.Message,
) {
	reason := e.stopReason
	if reason == "" {
		reason = event.TurnLimitMaxIterations
		e.stopReason = reason // the fallback answer names the cap (synthesisFallback)
	}
	logger.Infof(ctx, "Agent loop stopped (%s), generating final answer", reason)
	common.PipelineWarn(ctx, "Agent", reason+"_reached", map[string]interface{}{
		"iterations": state.CurrentRound,
		"max":        e.config.MaxIterations,
		"tokens":     turnTokens(state.TurnUsage),
		"token_cap":  e.config.TurnTokenCap,
	})
	// The timeline says why the turn ended early: the answer below is
	// synthesized from what was gathered, not a natural finish.
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        generateEventID("turn-limit"),
		Type:      event.EventAgentTurnLimit,
		SessionID: sessionID,
		Data: event.AgentTurnLimitData{
			Reason:        reason,
			Rounds:        state.CurrentRound,
			MaxIterations: max(e.config.MaxIterations, 0),
			Tokens:        turnTokens(state.TurnUsage),
			TokenCap:      e.config.TurnTokenCap,
		},
	})

	// Stream final answer generation through EventBus
	if err := e.streamFinalAnswerToEventBus(ctx, query, state, sessionID, messages); err != nil {
		logger.Errorf(ctx, "Failed to synthesize final answer: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_failed", map[string]interface{}{
			"error": err.Error(),
		})
		state.FinalAnswer = e.synthesisFallback(state)
	}
	state.IsComplete = true
}

// synthesisFallbackMaxTools bounds the tool lines of synthesisFallback.
const synthesisFallbackMaxTools = 6

// synthesisFallbackLineRunes bounds each tool line of synthesisFallback.
const synthesisFallbackLineRunes = 160

// legalLookupTools are the tools of a legal lookup: the fallback then asks
// for the Điều to read.
var legalLookupTools = map[string]bool{
	agenttools.ToolCheckLegalStatus:      true,
	agenttools.ToolSearchDocumentSection: true,
	"find_related_legal_documents":       true,
}

// synthesisFallback is the code-built answer of a turn whose synthesis
// produced no text: why it stopped, what was looked up (a Vietnamese line
// per successful tool, see fallbackToolLine) and how to ask again.
func (e *AgentEngine) synthesisFallback(state *types.AgentState) string {
	var sb strings.Builder
	steps := len(state.RoundSteps)
	switch e.stopReason {
	case event.TurnLimitTurnBudget, event.TurnLimitMaxIterations:
		fmt.Fprintf(&sb, "Lượt này đã dùng hết ngân sách xử lý sau %d bước nên tôi chưa tổng hợp được câu trả lời.", steps)
	default:
		fmt.Fprintf(&sb, "Tôi chưa tổng hợp được câu trả lời sau %d bước xử lý.", steps)
	}
	var lines []string
	legal := false
	for _, step := range state.RoundSteps {
		for _, tc := range step.ToolCalls {
			if tc.Result == nil || !tc.Result.Success {
				continue
			}
			legal = legal || legalLookupTools[tc.ExecutionName()]
			if line := fallbackToolLine(tc); line != "" {
				lines = append(lines, line)
			}
		}
	}
	if len(lines) > 0 {
		sb.WriteString(" Đã tra:")
		for i, l := range lines {
			if i == synthesisFallbackMaxTools {
				fmt.Fprintf(&sb, "\n- … và %d lần tra khác", len(lines)-i)
				break
			}
			sb.WriteString("\n- " + l)
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString(" ")
	}
	if legal {
		sb.WriteString("Bạn hãy hỏi gọn hơn hoặc nêu đúng Điều cần xem.")
	} else {
		sb.WriteString("Bạn hãy hỏi gọn hơn hoặc chia yêu cầu thành nhiều lượt.")
	}
	return sb.String()
}

// fallbackToolLine describes one successful call for the user, from its
// Data where the Output is markup or English meant for the model; "" for
// a call with nothing presentable.
func fallbackToolLine(tc types.ToolCall) string {
	r := tc.Result
	title := func() string {
		t, _ := r.Data["knowledge_title"].(string)
		return strings.TrimSpace(t)
	}
	switch tc.ExecutionName() {
	case agenttools.ToolSearchKnowledge:
		return fmt.Sprintf("tìm trong kho tri thức: %d kết quả", dataInt(r.Data, "count"))
	case agenttools.ToolReadDocument, agenttools.ToolSearchDocumentSection:
		t := title()
		if t == "" {
			return "đọc văn bản trong kho tri thức"
		}
		line := "đọc «" + clipRunes(t, synthesisFallbackLineRunes) + "»"
		if total := dataInt(r.Data, "total_chunks"); total > 0 {
			line += fmt.Sprintf(" (%d/%d đoạn)", dataInt(r.Data, "fetched_chunks"), total)
		}
		return line
	case agenttools.ToolCheckLegalStatus:
		for _, l := range strings.Split(r.Output, "\n") {
			if h := strings.TrimSpace(strings.TrimPrefix(l, "## ")); strings.HasPrefix(l, "## ") && h != "" {
				return "tra hiệu lực: " + clipRunes(h, synthesisFallbackLineRunes)
			}
		}
		return "tra hiệu lực văn bản"
	}
	for _, l := range strings.Split(r.Output, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		// markup, or plain ASCII (English notes such as "Listed 3/9 chunks …")
		if strings.HasPrefix(l, "<") || strings.HasPrefix(l, "[") || isASCII(l) {
			return ""
		}
		return clipRunes(l, synthesisFallbackLineRunes)
	}
	return ""
}

// isASCII reports whether s has no non-ASCII rune (no Vietnamese letter).
func isASCII(s string) bool {
	for _, r := range s {
		if r >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// clipRunes cuts s to n runes, marking the cut.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// dataInt reads a number from tool Data (int or a decoded JSON float).
func dataInt(data map[string]interface{}, key string) int {
	switch v := data[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// emitCompletionEvent emits the EventAgentComplete event with execution summary.
func (e *AgentEngine) emitCompletionEvent(
	ctx context.Context, state *types.AgentState, sessionID, messageID string, startTime time.Time,
) {
	steps := state.RoundSteps
	if len(state.PendingSteerMessages) > 0 {
		// A stop or model failure can arrive after delivery but before the next
		// response exists. Preserve that boundary without inventing an answer.
		steps = append(append([]types.AgentStep(nil), steps...), types.AgentStep{
			Iteration: state.CurrentRound, UserMessagesBefore: state.PendingSteerMessages,
		})
	}
	// Convert knowledge refs to interface{} slice for event data
	knowledgeRefsInterface := make([]interface{}, 0, len(state.KnowledgeRefs))
	for _, ref := range state.KnowledgeRefs {
		knowledgeRefsInterface = append(knowledgeRefsInterface, ref)
	}

	e.eventBus.Emit(ctx, event.Event{
		ID:        generateEventID("complete"),
		Type:      event.EventAgentComplete,
		SessionID: sessionID,
		Data: event.AgentCompleteData{
			FinalAnswer:     state.FinalAnswer,
			KnowledgeRefs:   knowledgeRefsInterface,
			AgentSteps:      steps,
			Usage:           turnUsage(state),
			TotalSteps:      len(state.RoundSteps),
			TotalDurationMs: time.Since(startTime).Milliseconds(),
			MessageID:       messageID, // Include message ID for proper message update
		},
	})

	logger.Infof(ctx, "Agent execution completed in %d rounds", state.CurrentRound)
}

// turnUsage returns the turn's aggregated LLM usage, or nil when no round
// reported usage so the field stays absent from the completion event and the
// persisted message alike.
func turnUsage(state *types.AgentState) *types.TokenUsage {
	// A token scale is worth persisting on its own: the next turn's history
	// loading is calibrated by it even when this provider reported no total.
	if state == nil || (state.TurnUsage.TotalTokens == 0 && state.TurnUsage.ContextTokenScale <= 0) {
		return nil
	}
	usage := state.TurnUsage
	return &usage
}
