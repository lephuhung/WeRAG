package chatpipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
	"github.com/google/uuid"
)

// PluginChatCompletionStream implements streaming chat completion functionality
// as a plugin that can be registered to EventManager
type PluginChatCompletionStream struct {
	modelService interfaces.ModelService // Interface for model operations
}

// NewPluginChatCompletionStream creates a new PluginChatCompletionStream instance
// and registers it with the EventManager
func NewPluginChatCompletionStream(eventManager *EventManager,
	modelService interfaces.ModelService,
) *PluginChatCompletionStream {
	res := &PluginChatCompletionStream{
		modelService: modelService,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginChatCompletionStream) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHAT_COMPLETION_STREAM}
}

// OnEvent handles streaming chat completion events
// It prepares the chat model, messages, and initiates streaming response
func (p *PluginChatCompletionStream) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	pipelineInfo(ctx, "Stream", "input", map[string]interface{}{
		"session_id":     chatManage.SessionID,
		"user_question":  chatManage.UserContent,
		"history_rounds": len(chatManage.History),
		"chat_model":     chatManage.ChatModelID,
	})

	// Prepare chat model and options
	chatModel, opt, err := prepareChatModel(ctx, p.modelService, chatManage)
	if err != nil {
		return ErrGetChatModel.WithError(err)
	}

	// Prepare base messages without history

	chatMessages, modelContext := prepareMessagesWithModelContext(ctx, chatManage)
	chatMessages = modelContext.EncodeMessages(chatMessages)
	reportModelContextLeaks(ctx, "Stream", modelContext, chatMessages)
	ctx = withPromptCacheMetadata(ctx, chatModel, chatMessages, opt, "knowledge_qa")
	pipelineInfo(ctx, "Stream", "messages_ready", map[string]interface{}{
		"message_count": len(chatMessages),
		"system_prompt": chatMessages[0].Content,
	})
	pipelineInfo(ctx, "Stream", "user_message", map[string]interface{}{
		"content": chatMessages[len(chatMessages)-1].Content,
	})
	// EventBus is required for event-driven streaming
	if chatManage.EventBus == nil {
		pipelineError(ctx, "Stream", "eventbus_missing", map[string]interface{}{
			"session_id": chatManage.SessionID,
		})
		return ErrModelCall.WithError(errors.New("EventBus is required for streaming"))
	}
	eventBus := chatManage.EventBus

	pipelineInfo(ctx, "Stream", "eventbus_ready", map[string]interface{}{
		"session_id": chatManage.SessionID,
	})

	// Defensive gate check: a request carrying an abbreviation binding must
	// still match the sealed turn before the answer model is invoked.
	if b := chatManage.AbbreviationBinding; b.UserMessageID != "" {
		if err := abbreviation.RequireTurn(ctx, b); err != nil {
			pipelineError(ctx, "Stream", "abbreviation_turn_invalid", map[string]interface{}{
				"session_id": chatManage.SessionID,
				"error":      err.Error(),
			})
			return ErrAbbreviationGate.WithError(err)
		}
	}

	// Initiate streaming chat model call with independent context
	pipelineInfo(ctx, "Stream", "model_call", map[string]interface{}{
		"chat_model": chatManage.ChatModelID,
	})
	retriesLeft := quickAnswerModelRetries
	responseChan, err := chatModel.ChatStream(ctx, chatMessages, opt)
	for err != nil && retriesLeft > 0 && quickAnswerRetryable(err) && waitQuickAnswerRetry(ctx, err) {
		retriesLeft--
		responseChan, err = chatModel.ChatStream(ctx, chatMessages, opt)
	}
	if err != nil {
		pipelineError(ctx, "Stream", "model_call", map[string]interface{}{
			"chat_model": chatManage.ChatModelID,
			"error":      err.Error(),
		})
		if ctx.Err() != nil {
			// a user stop: the stop path finishes the turn
			return ErrModelCall.WithError(err)
		}
		// The model refused the call: the turn ends with the failure
		// notice and a Done that persists it (quickAnswerModelFailure).
		emitQuickAnswerModelFailure(ctx, chatManage, "", err)
		_ = eventBus.Emit(ctx, types.Event{
			ID:        fmt.Sprintf("%s-answer", uuid.New().String()[:8]),
			Type:      types.EventType(event.EventAgentFinalAnswer),
			SessionID: chatManage.SessionID,
			Data:      event.AgentFinalAnswerData{Done: true},
		})
		return next()
	}
	if responseChan == nil {
		pipelineError(ctx, "Stream", "model_call", map[string]interface{}{
			"chat_model": chatManage.ChatModelID,
			"error":      "nil_channel",
		})
		return ErrModelCall.WithError(errors.New("chat stream returned nil channel"))
	}

	pipelineInfo(ctx, "Stream", "model_started", map[string]interface{}{
		"session_id": chatManage.SessionID,
	})

	// Start goroutine to consume channel and emit events directly.
	// reasoning_content is routed to EventAgentThought (SSE response_type=thinking)
	// and plain answer text to EventAgentFinalAnswer, matching the Agent pipeline.
	// The goroutine monitors ctx.Done() to avoid leaking when the context is cancelled
	// and the upstream channel is not closed promptly.
	go func() {
		answerDecoder := modelContext.StreamDecoder()
		thinkingDecoder := modelContext.StreamDecoder()
		thinkingID := fmt.Sprintf("%s-thinking", uuid.New().String()[:8])
		answerID := fmt.Sprintf("%s-answer", uuid.New().String()[:8])
		thinkingOpen := false
		answerCompleted := false
		// answerShown: answer text reached the bus; emittedAny: anything
		// did (thinking included), which rules out a retry
		answerShown := false
		emittedAny := false
		// pendingDone holds the terminal answer event back while the provider
		// has not reported usage yet. OpenAI-compatible streams send
		// finish_reason first and the usage block on a trailing chunk, and
		// the Done event is what persists the message, so emitting it on
		// finish_reason would store the quick answer without its token cost.
		pendingDone := false

		emitPendingDone := func(usage *types.TokenUsage) {
			if !pendingDone {
				return
			}
			pendingDone = false
			eventBus.Emit(ctx, types.Event{
				ID:        answerID,
				Type:      types.EventType(event.EventAgentFinalAnswer),
				SessionID: chatManage.SessionID,
				Data: event.AgentFinalAnswerData{
					Done:  true,
					Usage: usage,
				},
			})
		}

		closeThinking := func() {
			if !thinkingOpen {
				return
			}
			eventBus.Emit(ctx, types.Event{
				ID:        thinkingID,
				Type:      types.EventType(event.EventAgentThought),
				SessionID: chatManage.SessionID,
				Data: event.AgentThoughtData{
					Done: true,
				},
			})
			thinkingOpen = false
		}

		// flushDecoders drains any handle suffix the stream decoders held back to
		// bridge references split across provider chunks. Both the normal close
		// and the cancellation path must call this, otherwise a resource
		// reference in flight at teardown is silently dropped (and never
		// persisted, since the assistant message is saved from these events).
		flushDecoders := func() {
			thinkingTail := thinkingDecoder.Flush()
			if thinkingTail != "" {
				_ = eventBus.Emit(ctx, types.Event{
					ID:        thinkingID,
					Type:      types.EventType(event.EventAgentThought),
					SessionID: chatManage.SessionID,
					Data:      event.AgentThoughtData{Content: thinkingTail},
				})
			}
			answerTail := answerDecoder.Flush()
			if answerTail != "" {
				answerShown, emittedAny = true, true
				_ = eventBus.Emit(ctx, types.Event{
					ID:        answerID,
					Type:      types.EventType(event.EventAgentFinalAnswer),
					SessionID: chatManage.SessionID,
					Data:      event.AgentFinalAnswerData{Content: answerTail},
				})
			}
		}

		for {
			select {
			case <-ctx.Done():
				flushDecoders()
				closeThinking()
				emitPendingDone(nil)
				pipelineInfo(ctx, "Stream", "context_cancelled", map[string]interface{}{
					"session_id": chatManage.SessionID,
				})
				return

			case response, ok := <-responseChan:
				if !ok {
					flushDecoders()
					closeThinking()
					emitPendingDone(nil)
					pipelineInfo(ctx, "Stream", "channel_close", map[string]interface{}{
						"session_id": chatManage.SessionID,
					})
					return
				}

				if response.ResponseType == types.ResponseTypeError {
					pipelineError(ctx, "Stream", "stream_error", map[string]interface{}{
						"session_id": chatManage.SessionID,
						"error":      response.Content,
					})
					if answerCompleted || ctx.Err() != nil {
						continue
					}
					cause := errors.New(response.Content)
					// broken before any output: one quick retry, a fresh stream
					if !emittedAny && retriesLeft > 0 && quickAnswerRetryable(cause) && waitQuickAnswerRetry(ctx, cause) {
						retriesLeft--
						if again, err := chatModel.ChatStream(ctx, chatMessages, opt); err == nil && again != nil {
							responseChan = again
							continue
						} else if err != nil {
							cause = err
						}
					}
					// The model broke off: a partial answer keeps its text
					// and gets the note; otherwise the failure notice.
					flushDecoders()
					closeThinking()
					if answerShown {
						eventBus.Emit(ctx, types.Event{
							ID:        answerID,
							Type:      types.EventType(event.EventAgentFinalAnswer),
							SessionID: chatManage.SessionID,
							Data:      event.AgentFinalAnswerData{Content: types.ModelInterruptedNote},
						})
						emitQuickAnswerModelFailure(ctx, chatManage, types.TurnFailureModelInterrupted, cause)
					} else {
						emitQuickAnswerModelFailure(ctx, chatManage, "", cause)
					}
					// the Done persists the turn (usage of the broken call, if any)
					answerCompleted, pendingDone = true, false
					eventBus.Emit(ctx, types.Event{
						ID:        answerID,
						Type:      types.EventType(event.EventAgentFinalAnswer),
						SessionID: chatManage.SessionID,
						Data:      event.AgentFinalAnswerData{Done: true, Usage: response.Usage},
					})
					continue
				}

				if pendingDone && response.Usage != nil {
					// The trailing usage chunk (or the EOF sentinel carrying it).
					emitPendingDone(response.Usage)
					continue
				}

				if response.ResponseType == types.ResponseTypeThinking {
					response.Content = thinkingDecoder.Feed(response.Content)
					if response.Done {
						response.Content += thinkingDecoder.Flush()
					}
					if response.Content != "" {
						thinkingOpen, emittedAny = true, true
						eventBus.Emit(ctx, types.Event{
							ID:        thinkingID,
							Type:      types.EventType(event.EventAgentThought),
							SessionID: chatManage.SessionID,
							Data: event.AgentThoughtData{
								Content: response.Content,
								Done:    false,
							},
						})
					}
					if response.Done {
						closeThinking()
					}
					continue
				}

				if response.ResponseType == types.ResponseTypeAnswer {
					// Providers can emit a completion once for finish_reason and again
					// for their EOF sentinel. A final answer is a terminal event for a
					// single stream, so forwarding a later duplicate would put an answer
					// after the session's complete event.
					if answerCompleted {
						continue
					}
					response.Content = answerDecoder.Feed(response.Content)
					if response.Done {
						response.Content += answerDecoder.Flush()
						answerCompleted = true
					}
					closeThinking()
					if response.Content != "" {
						answerShown, emittedAny = true, true
					}
					if response.Done && !answerShown && ctx.Err() == nil {
						// a completion with no text: no answer was produced
						emitQuickAnswerModelFailure(ctx, chatManage, "",
							errors.New("model returned an empty completion"))
					}
					if response.Done && response.Usage == nil {
						// Stream the last text now; the Done flag waits for usage.
						if response.Content != "" {
							eventBus.Emit(ctx, types.Event{
								ID:        answerID,
								Type:      types.EventType(event.EventAgentFinalAnswer),
								SessionID: chatManage.SessionID,
								Data:      event.AgentFinalAnswerData{Content: response.Content},
							})
						}
						pendingDone = true
						continue
					}
					eventBus.Emit(ctx, types.Event{
						ID:        answerID,
						Type:      types.EventType(event.EventAgentFinalAnswer),
						SessionID: chatManage.SessionID,
						Data: event.AgentFinalAnswerData{
							Content: response.Content,
							Done:    response.Done,
							Usage:   response.Usage,
						},
					})
				}
			}
		}
	}()

	return next()
}

// The quick answer has no retry of its own: one quick retry covers an error
// that is clearly transient (refused or reset connection, 502/503/504, a
// timeout) before anything was shown. Never a 4xx, never after output.
const (
	quickAnswerModelRetries    = 1
	quickAnswerModelRetryDelay = time.Second
)

// quickAnswerTransientMarkers are the lower-cased error texts retried once.
var quickAnswerTransientMarkers = []string{
	"connection refused", "connection reset", "broken pipe",
	"status 502", "status 503", "status 504",
	"status code: 502", "status code: 503", "status code: 504",
	"timeout", "timed out", "deadline exceeded",
}

func quickAnswerRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range quickAnswerTransientMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// waitQuickAnswerRetry pauses before the retry; false when the turn was
// stopped meanwhile.
func waitQuickAnswerRetry(ctx context.Context, cause error) bool {
	if ctx.Err() != nil {
		return false
	}
	logger.Warnf(ctx, "[Stream] Model call failed, retrying once in %v: %v", quickAnswerModelRetryDelay, cause)
	select {
	case <-ctx.Done():
		return false
	case <-time.After(quickAnswerModelRetryDelay):
		return true
	}
}

// emitQuickAnswerModelFailure sends the failure notice of a quick-answer
// turn (types/turn_failure.go): an error event with the user-facing text
// and the failure code, which the stream handler records on the message.
// An empty code picks the unavailable notice, the after-search one when
// the turn had retrieval results. The raw cause goes to the log only.
func emitQuickAnswerModelFailure(ctx context.Context, chatManage *types.ChatManage, code string, cause error) {
	if code == "" {
		code = types.TurnFailureModelUnavailable
		if len(chatManage.MergeResult) > 0 {
			code = types.TurnFailureModelUnavailableAfterSearch
		}
	}
	logger.Errorf(ctx, "[Stream] Model failure ends the quick answer (%s): %v", code, cause)
	if chatManage.EventBus == nil {
		return
	}
	_ = chatManage.EventBus.Emit(ctx, types.Event{
		ID:        fmt.Sprintf("%s-error", uuid.New().String()[:8]),
		Type:      types.EventType(event.EventError),
		SessionID: chatManage.SessionID,
		Data: event.ErrorData{
			Error:     types.TurnFailureMessage(code),
			ErrorCode: code,
			Stage:     "chat_completion_stream",
			SessionID: chatManage.SessionID,
		},
	})
}
