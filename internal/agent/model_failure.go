package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// When the answering model fails (refused call, non-2xx, broken stream, or
// empty completions after the nudges) the turn ends with a code-written
// notice instead of an empty answer (types/turn_failure.go):
//
//   - nothing shown yet: an error event carries types.ModelUnavailableMessage
//     (or ...AfterSearchMessage when tools had returned results; their steps
//     and references stay on the message), before the completion event, so
//     the client receives it while the stream is still open;
//   - part of the answer shown: the partial text is kept, the
//     types.ModelInterruptedNote is appended to the same answer stream, and
//     an error event with the interrupted code marks the turn.
//
// The raw cause goes to the server log and the Langfuse span only. A user
// stop never comes here (the callers check ctx first).

// partialAnswerError is a model call that failed after part of its answer
// had been streamed to the answer area under answerID. Such a call is never
// retried or followed by a synthesis: either would show a second answer
// after the first one's text.
type partialAnswerError struct {
	err      error
	answerID string
	// text is exactly what reached the event bus under answerID.
	text string
}

func (e *partialAnswerError) Error() string { return e.err.Error() }
func (e *partialAnswerError) Unwrap() error { return e.err }

// withPartialAnswer wraps err when text was streamed; otherwise err as is.
func withPartialAnswer(err error, answerID, text string) error {
	if err == nil || answerID == "" || strings.TrimSpace(text) == "" {
		return err
	}
	return &partialAnswerError{err: err, answerID: answerID, text: text}
}

// partialAnswerOf returns the partial answer err carries, if any.
func partialAnswerOf(err error) (*partialAnswerError, bool) {
	var p *partialAnswerError
	if errors.As(err, &p) && p != nil {
		return p, true
	}
	return nil, false
}

// clientErrorStatus matches the 4xx status of a provider error as the chat
// clients word it ("API request failed with status 400: …", go-openai's
// "error, status code: 400, …").
var clientErrorStatus = regexp.MustCompile(`status(?: code)?:? (4\d\d)\b`)

// isClientError reports a 4xx refusal other than 408 (timeout) and 429
// (rate limit): the request itself is wrong, so the same request fails again.
func isClientError(err error) bool {
	if err == nil {
		return false
	}
	m := clientErrorStatus.FindStringSubmatch(strings.ToLower(err.Error()))
	return m != nil && m[1] != "408" && m[1] != "429"
}

// retryableModelError reports whether a failed model call may be retried:
// a transient error, not a 4xx, and nothing of its answer shown yet.
func retryableModelError(err error) bool {
	if _, partial := partialAnswerOf(err); partial {
		return false
	}
	return isTransientError(err) && !isClientError(err)
}

// endTurnOnModelFailure ends the turn on a model failure: it sets
// state.FinalAnswer (the partial text and the note, or ""), emits the
// note / error events, and records the failure for the completion event and
// the span. The caller returns normally afterwards, so the deferred
// completion event follows the error event.
func (e *AgentEngine) endTurnOnModelFailure(
	ctx context.Context, state *types.AgentState, sessionID string, cause error,
) {
	code := types.TurnFailureModelUnavailable
	if p, ok := partialAnswerOf(cause); ok {
		code = types.TurnFailureModelInterrupted
		_ = e.eventBus.Emit(ctx, event.Event{
			ID: p.answerID, Type: event.EventAgentFinalAnswer, SessionID: sessionID,
			Data: event.AgentFinalAnswerData{Content: types.ModelInterruptedNote},
		})
		e.closeAnswerStream(ctx, sessionID, p.answerID)
		state.FinalAnswer = p.text + types.ModelInterruptedNote
	} else {
		if countTotalToolCalls(state.RoundSteps) > 0 {
			code = types.TurnFailureModelUnavailableAfterSearch
		}
		state.FinalAnswer = ""
	}
	e.turnFailure = code
	e.turnFailureCause = cause
	state.Ungrounded = false

	logger.Errorf(ctx, "[Agent] Model failure ends the turn (%s) after %d rounds: %v",
		code, state.CurrentRound, cause)
	common.PipelineError(ctx, "Agent", "model_failure", map[string]interface{}{
		"session_id": sessionID,
		"code":       code,
		"rounds":     state.CurrentRound,
		"error":      cause.Error(),
	})
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        generateEventID("error"),
		Type:      event.EventError,
		SessionID: sessionID,
		Data: event.ErrorData{
			Error:     types.TurnFailureMessage(code),
			ErrorCode: code,
			Stage:     "agent_model",
			SessionID: sessionID,
		},
	})
}

// endsTurnAsModelFailure reports whether err from a model call ends the turn
// with the failure notice: a model error while the turn was not stopped.
func endsTurnAsModelFailure(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() == nil && types.IsModelCallError(err)
}
