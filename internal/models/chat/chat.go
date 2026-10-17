package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/types"
)

// Tool represents a function/tool definition
type Tool struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef represents a function definition
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChatOptions là các tuỳ chọn trò chuyện
type ChatOptions struct {
	Temperature float64 `json:"temperature"` // tham số temperature
	TopP        float64 `json:"top_p"`       // Top P Parameters
	Seed        int     `json:"seed"`        // seed ngẫu nhiên
	// MaxTokens and MaxCompletionTokens are aliases for one completion budget.
	// Callers may set either; CompletionBudget() prefers MaxCompletionTokens.
	// The outbound Chat Completions JSON carries exactly one of max_tokens or
	// max_completion_tokens, chosen per provider (see wireCompletionTokenField).
	MaxTokens           int             `json:"max_tokens"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	FrequencyPenalty    float64         `json:"frequency_penalty"`             // phạt tần suất
	PresencePenalty     float64         `json:"presence_penalty"`              // phạt hiện diện
	Thinking            *bool           `json:"thinking"`                      // có bật suy luận (thinking) không
	Tools               []Tool          `json:"tools,omitempty"`               // danh sách công cụ khả dụng
	ToolChoice          string          `json:"tool_choice,omitempty"`         // "auto", "required", "none", or specific tool
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"` // có cho phép gọi công cụ song song không (nil = để mô hình quyết định)
	Format              json.RawMessage `json:"format,omitempty"`              // định nghĩa định dạng phản hồi
	// PromptCacheKey is the provider routing key (OpenAI prompt_cache_key).
	// Empty falls back to the session ID on the call context.
	PromptCacheKey string `json:"-"`
	// CacheRetention controls provider prompt-cache TTL. none disables cache
	// markers; empty/short is the default 5-minute cache; long requests 1h/24h
	// where the provider accepts it.
	CacheRetention CacheRetention `json:"-"`
}

// MessageContentPart represents a part of multi-content message
type MessageContentPart struct {
	Type     string    `json:"type"`                // "text" or "image_url"
	Text     string    `json:"text,omitempty"`      // For type="text"
	ImageURL *ImageURL `json:"image_url,omitempty"` // For type="image_url"
}

// ImageURL represents the image URL structure
type ImageURL struct {
	URL    string `json:"url"`              // URL or base64 data URI
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// MessageKind marks messages the engine synthesized rather than received from
// the user or the model. Compaction needs to tell its own summary apart from a
// real user turn: a summary that looks like ordinary history gets fed back into
// the next summarization pass and degrades into a summary of a summary.
type MessageKind string

// MessageKindCompactionSummary marks the message that replaces compacted
// history. It carries the `user` role because that is where providers expect
// conversation history, so the role alone cannot identify it.
const MessageKindCompactionSummary MessageKind = "compaction_summary"

// Message biểu diễn một tin nhắn trò chuyện
type Message struct {
	Role         string               `json:"role"`                    // vai trò: system, user, assistant, tool
	Content      string               `json:"content"`                 // nội dung tin nhắn
	MultiContent []MessageContentPart `json:"multi_content,omitempty"` // tin nhắn nhiều phần (văn bản + ảnh)
	Name         string               `json:"name,omitempty"`          // Function/tool name (for tool role)
	ToolCallID   string               `json:"tool_call_id,omitempty"`  // Tool call ID (for tool role)
	ToolCalls    []ToolCall           `json:"tool_calls,omitempty"`    // Tool calls (for assistant role)
	Images       []string             `json:"images,omitempty"`        // Image URLs for multimodal (only for current user message)
	// ReasoningContent là nội dung suy luận mà các mô hình reasoning (DeepSeek thinking, Xiaomi MiMo, vLLM reasoning, ...)
	// đã xuất ra ở lượt trước của assistant. Một số nhà cung cấp (MiMo, DeepSeek V3.2/V4 chế độ thinking) yêu cầu trong hội thoại nhiều lượt
	// phải gửi lại nguyên vẹn reasoning_content của assistant, nếu không sẽ từ chối với 400; các nhà cung cấp khác
	// bỏ qua trường lạ, không có tác dụng phụ.
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// Kind is engine-internal bookkeeping. `json:"-"` keeps it off the wire:
	// providers reject unknown message fields on some endpoints, and this one
	// means nothing to them anyway.
	Kind MessageKind `json:"-"`
	// TurnID is the stored assistant message whose turn this history message
	// was replayed from. It is empty for the live turn and for messages the
	// engine synthesized. Compaction uses it to tell whether a summary ends
	// exactly on a stored turn, the only kind it can persist for later turns.
	// Engine-internal like Kind, and kept off the wire for the same reason.
	TurnID string `json:"-"`
}

// ToolCall represents a tool call in a message
type ToolCall struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"` // "function"
	Function         FunctionCall           `json:"function"`
	ProviderMetadata types.ToolCallMetadata `json:"provider_metadata,omitempty"`
}

// FunctionCall represents a function call
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// Chat định nghĩa giao diện trò chuyện
type Chat interface {
	// Chat thực hiện trò chuyện không stream
	Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*types.ChatResponse, error)

	// ChatStream thực hiện trò chuyện dạng stream
	ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan types.StreamResponse, error)

	// GetModelName trả về tên mô hình
	GetModelName() string

	// GetModelID trả về ID mô hình
	GetModelID() string
}

type ChatConfig struct {
	Source    types.ModelSource
	BaseURL   string
	ModelName string
	APIKey    string
	ModelID   string
	Provider  string
	// MaxConcurrency caps concurrent background calls to this model; 0 falls
	// back to the process-wide default (see limiter.GateN).
	MaxConcurrency int
	ExtraConfig    map[string]string
	// CustomHeaders cho phép đính kèm header HTTP tuỳ chỉnh khi gọi API tương thích OpenAI từ xa (giống extra_headers của OpenAI Python SDK).
	CustomHeaders map[string]string
	AppID         string
	AppSecret     string // giá trị đã mã hoá, do bên gọi factory truyền vào, đã được giải mã trước khi dùng trong NewWeKnoraCloudChat
}

// ConfigFromModel dựng ChatConfig từ types.Model.
// Đảm bảo đường production (tầng service tạo instance từ cấu hình mô hình trong DB) và đường kiểm thử
// (tầng handler tạo instance tạm từ form frontend) dùng cùng một ánh xạ trường, tránh lặp boilerplate.
// appID / appSecret là thông tin xác thực WeKnoraCloud đã giải mã/phân tích sẵn, bên gọi chịu trách nhiệm truyền vào.
func ConfigFromModel(m *types.Model, appID, appSecret string) *ChatConfig {
	if m == nil {
		return nil
	}
	return &ChatConfig{
		ModelID:        m.ID,
		APIKey:         m.Parameters.APIKey,
		BaseURL:        m.Parameters.BaseURL,
		ModelName:      m.Name,
		Source:         m.Source,
		Provider:       m.Parameters.Provider,
		MaxConcurrency: m.Parameters.MaxConcurrency,
		ExtraConfig:    m.Parameters.ExtraConfig,
		CustomHeaders:  m.Parameters.CustomHeaders,
		AppID:          appID,
		AppSecret:      appSecret,
	}
}

// NewChat tạo instance trò chuyện
func NewChat(config *ChatConfig, ollamaService *ollama.OllamaService) (Chat, error) {
	var c Chat
	var err error
	switch strings.ToLower(string(config.Source)) {
	case string(types.ModelSourceLocal):
		c, err = NewOllamaChat(config, ollamaService)
	case string(types.ModelSourceRemote):
		c, err = NewRemoteChat(config)
	default:
		return nil, fmt.Errorf("unsupported chat model source: %s", config.Source)
	}
	c, err = wrapChatDebug(c, err)
	c, err = wrapChatLangfuse(c, err)
	// Outermost: hold the per-model concurrency slot only around the real
	// provider round-trip, so the wait is excluded from debug/langfuse timing.
	return wrapChatConcurrency(c, config.MaxConcurrency, err)
}

// NewRemoteChat creates a remote chat instance. Every supported provider is
// OpenAI-compatible and handled by RemoteAPIChat; provider quirks are resolved
// through providerAdapter at construction time.
func NewRemoteChat(config *ChatConfig) (Chat, error) {
	return NewRemoteAPIChat(config)
}
