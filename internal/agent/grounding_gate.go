package agent

import (
	"context"
	"strings"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// A turn marked types.WithGroundingRequired (a question the open document
// does not answer) may not be answered from the model's memory. The
// grounding guidance in the system prompt asks for that already, and Qwen
// ignored it: "iPhone 18 Pro" came back as "not released yet, the latest
// is iPhone 15" with no tool call. So until a tool has returned evidence,
// a round is not streamed: a tool call goes ahead as usual, a plain answer
// is replaced by ungroundedAnswer.

// ungroundedAnswer is what the user gets instead of an answer from memory.
const ungroundedAnswer = "Tôi không tìm thấy căn cứ cho câu hỏi này trong kho tri thức, " +
	"nên không trả lời theo hiểu biết riêng của mô hình (có thể đã cũ hoặc sai). " +
	"Bạn có thể bổ sung tài liệu liên quan vào kho tri thức, nêu rõ văn bản cần tra, " +
	"hoặc bật tìm kiếm web rồi hỏi lại."

// countedEvidenceTools report their hits in Data["count"].
var countedEvidenceTools = map[string]bool{
	agenttools.ToolSearchKnowledge: true,
	agenttools.ToolWebSearch:       true,
}

// readerEvidenceTools return source text whenever they succeed.
var readerEvidenceTools = map[string]bool{
	agenttools.ToolReadDocument:          true,
	agenttools.ToolSearchDocumentSection: true,
	agenttools.ToolQueryKnowledgeGraph:   true,
	agenttools.ToolWebFetch:              true,
}

type ungroundedKey struct{}

// markUngrounded flags the round's ctx when the turn needs grounding and no
// tool has returned evidence yet.
func markUngrounded(ctx context.Context, steps []types.AgentStep) context.Context {
	if !types.GroundingRequiredFromContext(ctx) || hasEvidence(steps) {
		return ctx
	}
	// a format check of the open document is answered from its tool's
	// report, not from the knowledge base (see format_intent.go)
	if isFormatCheckRequest(types.UserQueryFromContext(ctx)) {
		return ctx
	}
	return context.WithValue(ctx, ungroundedKey{}, true)
}

func isUngrounded(ctx context.Context) bool {
	v, _ := ctx.Value(ungroundedKey{}).(bool)
	return v
}

// hasEvidence reports whether a tool of this turn returned source content.
func hasEvidence(steps []types.AgentStep) bool {
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			r := tc.Result
			if r == nil || !r.Success {
				continue
			}
			name := tc.ExecutionName()
			if countedEvidenceTools[name] && evidenceCount(r.Data) > 0 {
				return true
			}
			if readerEvidenceTools[name] && strings.TrimSpace(r.Output) != "" {
				return true
			}
		}
	}
	return false
}

func evidenceCount(data map[string]interface{}) int {
	switch n := data["count"].(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// ungroundedRound runs the round without streaming: tool calls are kept
// (shown as pending, as a streamed round does), a plain answer becomes
// ungroundedAnswer. analyzeResponse emits that content, since it was not
// streamed.
func (e *AgentEngine) ungroundedRound(
	ctx context.Context, messages []chat.Message, opts *chat.ChatOptions, iteration int, sessionID string,
) (*types.ChatResponse, error) {
	messages = e.modelContext.EncodeMessages(messages)
	reportModelContextLeaks(ctx, "Agent", e.modelContext, messages)
	callCtx := types.WithLLMCallMetadata(ctx, "agent_round_ungrounded", chat.PromptPrefixFingerprint(messages, opts))
	resp, err := e.chatModel.Chat(callCtx, messages, opts)
	if err != nil {
		return nil, err
	}
	if len(resp.ToolCalls) > 0 {
		resp.Content = ""
		e.emitForcedToolCalls(ctx, resp.ToolCalls, iteration, sessionID)
		return resp, nil
	}
	logger.Warnf(ctx, "[Agent][Grounding] Round %d answered without evidence (%d chars); replaced",
		iteration+1, len(resp.Content))
	resp.Content = ungroundedAnswer
	resp.ReasoningContent = ""
	resp.AnswerStreamed = false
	resp.AnswerEventID = ""
	return resp, nil
}
