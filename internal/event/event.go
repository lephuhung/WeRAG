package event

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/Tencent/WeKnora/internal/logger"
)

// EventType represents the type of event in the system
type EventType string

const (
	// Query processing events
	EventQueryReceived   EventType = "query.received"   // nhận truy vấn của người dùng
	EventQueryValidated  EventType = "query.validated"  // truy vấn đã được kiểm tra
	EventQueryPreprocess EventType = "query.preprocess" // tiền xử lý truy vấn
	EventQueryRewrite    EventType = "query.rewrite"    // viết lại truy vấn
	EventQueryRewritten  EventType = "query.rewritten"  // viết lại truy vấn xong

	// Retrieval events
	EventRetrievalStart    EventType = "retrieval.start"    // bắt đầu truy xuất
	EventRetrievalVector   EventType = "retrieval.vector"   // truy xuất vector
	EventRetrievalKeyword  EventType = "retrieval.keyword"  // truy xuất từ khoá
	EventRetrievalEntity   EventType = "retrieval.entity"   // truy xuất thực thể
	EventRetrievalComplete EventType = "retrieval.complete" // truy xuất xong

	// Rerank events
	EventRerankStart    EventType = "rerank.start"    // bắt đầu rerank
	EventRerankComplete EventType = "rerank.complete" // rerank xong

	// Merge events
	EventMergeStart    EventType = "merge.start"    // bắt đầu gộp
	EventMergeComplete EventType = "merge.complete" // gộp xong

	// Chat completion events
	EventChatStart    EventType = "chat.start"    // bắt đầu sinh câu trả lời
	EventChatComplete EventType = "chat.complete" // sinh câu trả lời xong
	EventChatStream   EventType = "chat.stream"   // streaming câu trả lời

	// Agent events
	EventAgentQuery    EventType = "agent.query"    // trợ lý bắt đầu xử lý truy vấn
	EventAgentPlan     EventType = "agent.plan"     // trợ lý lập kế hoạch
	EventAgentStep     EventType = "agent.step"     // trợ lý thực hiện bước
	EventAgentTool     EventType = "agent.tool"     // trợ lý gọi công cụ
	EventAgentComplete EventType = "agent.complete" // trợ lý hoàn tất

	// Agent streaming events (for real-time feedback)
	EventAgentThought       EventType = "thought"        // quá trình suy nghĩ của trợ lý
	EventAgentCommandOutput EventType = "command_output" // bounded command output
	EventAgentToolCall      EventType = "tool_call"      // thông báo gọi công cụ
	EventAgentToolResult    EventType = "tool_result"    // kết quả công cụ
	EventAgentReflection    EventType = "reflection"     // trợ lý phản tư
	EventAgentReferences    EventType = "references"     // trích dẫn tài liệu
	EventAgentFinalAnswer   EventType = "final_answer"   // câu trả lời cuối cùng

	// MCP tool human approval (issue #1173)
	EventToolApprovalRequired EventType = "tool_approval_required"
	EventToolApprovalResolved EventType = "tool_approval_resolved"

	// MCP OAuth in-conversation authorization prompt: emitted when an
	// OAuth-enabled MCP service is invoked but the current user has not
	// authorized it yet. The agent pauses until the user authorizes (or the
	// wait times out / is canceled).
	EventMCPOAuthRequired EventType = "mcp_oauth_required"
	EventMCPOAuthResolved EventType = "mcp_oauth_resolved"

	// Error events
	EventError EventType = "error" // sự kiện lỗi

	// Long-term memory recalled for this turn. Emitted once, before the answer
	// streams, so the UI can show which memories the answer saw.
	EventMemoryRecalled EventType = "memory_recalled"

	// Memories the background extraction stored since the user was last
	// told. Emitted once at the start of a turn so the UI can say "I just
	// saved ..." and let the user take a wrong one back.
	EventMemorySaved EventType = "memory_saved"

	// EventContextCompacted is emitted when older conversation was summarized
	// away to fit the context window.
	EventContextCompacted EventType = "context_compacted"

	// EventAgentTurnLimit is emitted when the agent loop stopped on a cap
	// (round count or the plan's per-turn token budget) rather than on a
	// natural finish; the answer that follows is synthesized from what the
	// turn gathered so far.
	EventAgentTurnLimit EventType = "turn_limit"

	// EventUserMessageInjected is emitted when a message the user appended
	// while the run was in flight was accepted into the running turn (see
	// agent drainSteerMessages).
	EventUserMessageInjected EventType = "user_message_injected"

	// Session events
	EventSessionTitle EventType = "session_title" // cập nhật tiêu đề cuộc trò chuyện

	// Control events
	EventStop EventType = "stop" // dừng sinh câu trả lời
)

// Event represents an event in the system
type Event struct {
	ID        string                 // ID sự kiện (UUID tự sinh, dùng để theo dõi cập nhật streaming)
	Type      EventType              // loại sự kiện
	SessionID string                 // ID cuộc trò chuyện
	Data      interface{}            // dữ liệu sự kiện
	Metadata  map[string]interface{} // metadata sự kiện
	RequestID string                 // ID yêu cầu
}

// EventHandler is a function that handles events
type EventHandler func(ctx context.Context, event Event) error

// EventBus manages event publishing and subscription
type EventBus struct {
	mu        sync.RWMutex
	handlers  map[EventType][]EventHandler
	asyncMode bool // có xử lý sự kiện bất đồng bộ hay không
}

// NewEventBus creates a new EventBus instance
func NewEventBus() *EventBus {
	return &EventBus{
		handlers:  make(map[EventType][]EventHandler),
		asyncMode: false,
	}
}

// NewAsyncEventBus creates a new EventBus with async mode enabled
func NewAsyncEventBus() *EventBus {
	return &EventBus{
		handlers:  make(map[EventType][]EventHandler),
		asyncMode: true,
	}
}

// On registers an event handler for a specific event type
// Multiple handlers can be registered for the same event type
func (eb *EventBus) On(eventType EventType, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// Off removes all handlers for a specific event type
func (eb *EventBus) Off(eventType EventType) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	delete(eb.handlers, eventType)
}

// Emit publishes an event to all registered handlers
// Returns error if any handler fails (in sync mode)
// Automatically generates an ID for the event if not provided (from source)
func (eb *EventBus) Emit(ctx context.Context, event Event) error {
	// Auto-generate ID if not provided (from source)
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	eb.mu.RLock()
	handlers, exists := eb.handlers[event.Type]
	eb.mu.RUnlock()

	if !exists || len(handlers) == 0 {
		// No handlers registered for this event type
		return nil
	}

	if eb.asyncMode {
		// Async mode: fire and forget
		for _, handler := range handlers {
			h := handler // capture loop variable
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf(ctx, "event handler panic recovered (type=%s): %v", event.Type, r)
					}
				}()
				_ = h(ctx, event)
			}()
		}
		return nil
	}

	// Sync mode: execute handlers sequentially
	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			return fmt.Errorf("event handler failed for %s: %w", event.Type, err)
		}
	}

	return nil
}

// EmitAndWait publishes an event and waits for all handlers to complete
// This method works in both sync and async mode
// Automatically generates an ID for the event if not provided (from source)
func (eb *EventBus) EmitAndWait(ctx context.Context, event Event) error {
	// Auto-generate ID if not provided (from source)
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	eb.mu.RLock()
	handlers, exists := eb.handlers[event.Type]
	eb.mu.RUnlock()

	if !exists || len(handlers) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(handlers))

	for _, handler := range handlers {
		wg.Add(1)
		h := handler // capture loop variable

		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errChan <- fmt.Errorf("event handler panic (type=%s): %v", event.Type, r)
				}
			}()
			if err := h(ctx, event); err != nil {
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	for err := range errChan {
		if err != nil {
			return fmt.Errorf("event handler failed for %s: %w", event.Type, err)
		}
	}

	return nil
}

// HasHandlers checks if there are any handlers registered for an event type
func (eb *EventBus) HasHandlers(eventType EventType) bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	handlers, exists := eb.handlers[eventType]
	return exists && len(handlers) > 0
}

// GetHandlerCount returns the number of handlers for a specific event type
func (eb *EventBus) GetHandlerCount(eventType EventType) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if handlers, exists := eb.handlers[eventType]; exists {
		return len(handlers)
	}
	return 0
}

// Clear removes all event handlers
func (eb *EventBus) Clear() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.handlers = make(map[EventType][]EventHandler)
}
