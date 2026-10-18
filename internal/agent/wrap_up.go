package agent

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// wrapUpNudgeRound is the number of finished rounds after which the turn is
// asked to answer. Long turns were exhaustive reading (nine searches, eleven
// reads) rather than loops, and the answer rarely changed after round 8.
const wrapUpNudgeRound = 8

// wrapUpNudgeText is the one-off instruction added after wrapUpNudgeRound.
const wrapUpNudgeText = "Bạn đã dùng 8 vòng công cụ. Hãy trả lời ngay bằng bằng chứng đã thu được, " +
	"nêu rõ phần chưa tìm thấy nếu có. Chỉ gọi thêm công cụ khi còn thiếu đúng một điểm cụ thể " +
	"và nói rõ điểm đó."

// appendWrapUpNudge adds wrapUpNudgeText to the turn's messages once, at the
// first round boundary after wrapUpNudgeRound finished rounds, so the next
// call sees it. It is called after drainSteerMessages, so like a steered
// message it is the last user message and sits in the protected tail that
// compaction keeps. It lives only in this turn's message slice: it is not
// persisted, not emitted, and not part of the history later turns rebuild
// from the stored steps. Turns capped at wrapUpNudgeRound rounds or fewer,
// and unlimited ones, are not nudged.
func (e *AgentEngine) appendWrapUpNudge(ctx context.Context, state *types.AgentState, messagesPtr *[]chat.Message) bool {
	if e.wrapUpNudged || e.config == nil || e.config.UnlimitedIterations() ||
		e.config.MaxIterations <= wrapUpNudgeRound || state.CurrentRound < wrapUpNudgeRound {
		return false
	}
	e.wrapUpNudged = true
	msgs := *messagesPtr
	// chat templates such as Gemma's reject two user messages in a row:
	// after a steered message the nudge rides on it
	if last := len(msgs) - 1; last >= 0 && msgs[last].Role == "user" && len(msgs[last].MultiContent) == 0 {
		msgs[last].Content += "\n\n" + wrapUpNudgeText
	} else {
		*messagesPtr = append(msgs, chat.Message{Role: "user", Content: wrapUpNudgeText})
	}
	logger.Infof(ctx, "[Agent][Round-%d] %d rounds used; asked the model to answer now",
		state.CurrentRound+1, state.CurrentRound)
	return true
}
