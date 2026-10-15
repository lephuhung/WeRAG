package chatpipeline

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/compaction"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var regThinkTags = regexp.MustCompile(`(?s)<think>.*?</think>`)

// pipelineInfo logs pipeline info level entries.
func pipelineInfo(ctx context.Context, stage, action string, fields map[string]interface{}) {
	common.PipelineInfo(ctx, stage, action, fields)
}

// pipelineWarn logs pipeline warning level entries.
func pipelineWarn(ctx context.Context, stage, action string, fields map[string]interface{}) {
	common.PipelineWarn(ctx, stage, action, fields)
}

// pipelineError logs pipeline error level entries.
func pipelineError(ctx context.Context, stage, action string, fields map[string]interface{}) {
	common.PipelineError(ctx, stage, action, fields)
}

// prepareChatModel shared logic to prepare chat model and options
// it gets the chat model and sets up the chat options based on the chat manage.
func prepareChatModel(ctx context.Context, modelService interfaces.ModelService,
	chatManage *types.ChatManage,
) (chat.Chat, *chat.ChatOptions, error) {
	chatModel, err := modelService.GetChatModel(ctx, chatManage.ChatModelID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get chat model: %v", err)
		return nil, nil, err
	}

	opt := &chat.ChatOptions{
		Temperature:         chatManage.SummaryConfig.Temperature,
		TopP:                chatManage.SummaryConfig.TopP,
		Seed:                chatManage.SummaryConfig.Seed,
		MaxTokens:           chatManage.SummaryConfig.MaxTokens,
		MaxCompletionTokens: chatManage.SummaryConfig.MaxCompletionTokens,
		FrequencyPenalty:    chatManage.SummaryConfig.FrequencyPenalty,
		PresencePenalty:     chatManage.SummaryConfig.PresencePenalty,
		Thinking:            chatManage.SummaryConfig.Thinking,
		PromptCacheKey:      chatManage.SessionID,
	}
	if opt.Thinking != nil {
		pipelineInfo(ctx, "Stream", "thinking_option", map[string]interface{}{
			"enabled": *opt.Thinking,
		})
	}

	return chatModel, opt, nil
}

// prepareMessagesWithHistory prepare complete messages including history.
// When SystemPromptOverride is set (e.g. by intent-specific prompt logic),
// it takes precedence over the default SummaryConfig.Prompt.
func prepareMessagesWithHistory(chatManage *types.ChatManage) []chat.Message {
	base := chatManage.SummaryConfig.Prompt
	if chatManage.SystemPromptOverride != "" {
		base = chatManage.SystemPromptOverride
	}
	systemPrompt := types.RenderPromptPlaceholders(base, types.PlaceholderValues{
		"query":    chatManage.Query,
		"language": chatManage.Language,
		"contexts": chatManage.RenderedContexts,
	})
	systemPrompt += "\n\n" + types.SourceDataBoundaryPrompt + "\n\n" + types.SourcedAnswerOutputPrompt
	// Memory goes at the end of the system prompt, after the retrieved-context
	// placeholders have been rendered, so a remembered sentence can never be
	// substituted into prompt structure.
	systemPrompt += chatManage.UserContextPrompt + chatManage.LegalCatalogPrompt + chatManage.MemoryPrompt

	chatMessages := []chat.Message{
		{Role: "system", Content: systemPrompt},
	}

	// The session's context summary stands in for the turns before History,
	// in the envelope the agent's history uses.
	if chatManage.HistorySummary != "" {
		chatMessages = append(chatMessages, compaction.SummaryMessage(chatManage.HistorySummary))
	}
	chatMessages = AppendHistoryMessages(chatMessages, chatManage.History)

	// Add current user message. Only include images when the chat model supports
	// vision; non-vision models rely on the text description in UserContent.
	userMsg := chat.Message{Role: "user", Content: chatManage.UserContent}
	if chatManage.ChatModelSupportsVision && len(chatManage.Images) > 0 {
		userMsg.Images = chatManage.Images
	}
	chatMessages = append(chatMessages, userMsg)

	return chatMessages
}

func withPromptCacheMetadata(
	ctx context.Context,
	chatModel chat.Chat,
	messages []chat.Message,
	opts *chat.ChatOptions,
	purpose string,
) context.Context {
	prefixFingerprint := chat.PromptPrefixFingerprint(messages, opts)
	_ = chatModel // model identity is already captured by the usage sink
	return types.WithLLMCallMetadata(ctx, purpose, prefixFingerprint)
}

// AppendHistoryMessages appends prior Q&A rounds in chronological order.
// History is already filtered and truncated upstream by the load_history plugin.
func AppendHistoryMessages(messages []chat.Message, history []*types.History) []chat.Message {
	for _, history := range history {
		messages = append(messages, chat.Message{Role: "user", Content: history.Query})
		messages = append(messages, chat.Message{Role: "assistant", Content: history.Answer})
	}
	return messages
}

// contextSummarySource is the part of the message service that knows a
// session's context summary (service.messageService.LatestContextSummary).
// Asserted rather than added to interfaces.MessageService so the many
// message-service fakes need not grow a method.
type contextSummarySource interface {
	LatestContextSummary(ctx context.Context, sessionID string) (string, time.Time, error)
}

// summarizedHistoryRounds is how many raw rounds follow a context summary.
// The summary already carries the conversation's state; the raw rounds only
// keep the newest exchanges verbatim, so fewer are needed than without one.
const summarizedHistoryRounds = 3

// loadHistoryAndSummary is loadAndProcessHistory for a session that may
// have a context summary (an agent compaction checkpoint, or the seed of a
// continued session). With one, history is the summary plus at most
// summarizedHistoryRounds raw rounds, none of them a round the summary
// already covers; without one, it is maxRounds raw rounds as before.
func loadHistoryAndSummary(
	ctx context.Context,
	messageService interfaces.MessageService,
	sessionID string,
	maxRounds int,
	fetchCount int,
) ([]*types.History, string, error) {
	summary, coveredThrough := "", time.Time{}
	if src, ok := messageService.(contextSummarySource); ok {
		s, at, err := src.LatestContextSummary(ctx, sessionID)
		if err != nil {
			pipelineWarn(ctx, "LoadHistory", "context_summary", map[string]interface{}{
				"session_id": sessionID,
				"error":      err.Error(),
			})
		} else {
			summary, coveredThrough = strings.TrimSpace(s), at
		}
	}
	if summary != "" {
		maxRounds = min(maxRounds, summarizedHistoryRounds)
	}
	history, err := loadAndProcessHistory(ctx, messageService, sessionID, maxRounds, fetchCount)
	if err != nil {
		return nil, "", err
	}
	if summary != "" && !coveredThrough.IsZero() {
		// rounds are newest first before the cut, so dropping the covered
		// ones never lets an older uncovered round in
		history = slices.DeleteFunc(history, func(h *types.History) bool {
			return !h.CreateAt.After(coveredThrough)
		})
	}
	return history, summary, nil
}

// loadAndProcessHistory fetches recent messages, groups them into Q&A pairs,
// strips <think> tags from assistant answers, sorts by recency, and limits to maxRounds.
// fetchCount controls how many raw messages to fetch (typically maxRounds*2+10).
func loadAndProcessHistory(
	ctx context.Context,
	messageService interfaces.MessageService,
	sessionID string,
	maxRounds int,
	fetchCount int,
) ([]*types.History, error) {
	history, err := messageService.GetRecentMessagesBySession(ctx, sessionID, fetchCount)
	if err != nil {
		return nil, err
	}

	historyMap := make(map[string]*types.History)
	for _, message := range history {
		h, ok := historyMap[message.RequestID]
		if !ok {
			h = &types.History{}
		}
		if message.Role == "user" {
			// RenderedContent is a snapshot of the prompt/context format used by
			// the original turn. Replaying it would mix legacy <context id="…">
			// envelopes and old citation instructions into the current protocol.
			// Historical references are carried separately in KnowledgeReferences
			// and can be re-merged into this turn's freshly rendered context.
			h.Query = message.Content
			h.CreateAt = message.CreatedAt
			if desc := extractImageCaptions(message.Images); desc != "" {
				h.Query += "\n\n" + types.UserImageContentLabel + "\n" + desc
			}
			if len(message.Attachments) > 0 {
				h.Query += message.Attachments.BuildPrompt()
			}
		} else {
			h.Answer = regThinkTags.ReplaceAllString(message.Content, "")
			h.KnowledgeReferences = message.KnowledgeReferences
		}
		historyMap[message.RequestID] = h
	}

	historyList := make([]*types.History, 0, len(historyMap))
	for _, h := range historyMap {
		if h.Answer != "" && h.Query != "" {
			historyList = append(historyList, h)
		}
	}

	sort.Slice(historyList, func(i, j int) bool {
		return historyList[i].CreateAt.After(historyList[j].CreateAt)
	})

	if len(historyList) > maxRounds {
		historyList = historyList[:maxRounds]
	}

	slices.Reverse(historyList)
	return historyList, nil
}

// extractImageCaptions concatenates non-empty Caption fields from stored
// message images. Used when loading history so that previous turns' image
// descriptions are visible to the model.
func extractImageCaptions(images types.MessageImages) string {
	var parts []string
	for _, img := range images {
		if img.Caption != "" {
			parts = append(parts, img.Caption)
		}
	}
	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------------------
// Concurrency utilities
// ---------------------------------------------------------------------------

// ParallelTask represents a named unit of concurrent work.
type ParallelTask struct {
	Name string
	Run  func() *PluginError
}

// RunParallel executes tasks concurrently.
// Returns a map of task name → error for tasks that returned non-nil errors.
func RunParallel(tasks ...ParallelTask) map[string]*PluginError {
	errs := make(map[string]*PluginError)
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(len(tasks))
	for _, task := range tasks {
		go func(t ParallelTask) {
			defer wg.Done()
			if err := t.Run(); err != nil {
				mu.Lock()
				errs[t.Name] = err
				mu.Unlock()
			}
		}(task)
	}
	wg.Wait()
	return errs
}

// ParallelMap applies fn to each element of items concurrently (up to
// maxWorkers goroutines) and returns results in the same order as items.
// If maxWorkers <= 0, concurrency is unbounded (one goroutine per item).
func ParallelMap[T, R any](items []T, maxWorkers int, fn func(int, T) R) []R {
	n := len(items)
	if n == 0 {
		return nil
	}
	results := make([]R, n)

	if maxWorkers <= 0 || maxWorkers > n {
		maxWorkers = n
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)

	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, it T) {
			defer func() { <-sem; wg.Done() }()
			results[idx] = fn(idx, it)
		}(i, item)
	}
	wg.Wait()
	return results
}
