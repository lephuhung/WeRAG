package compaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	agenttoken "github.com/Tencent/WeKnora/internal/agent/token"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// maxSummarizationAttempts bounds the retries for one compaction. Each
	// attempt is a full LLM round-trip, and a compaction that keeps failing is
	// better served by the raw archive than by stalling the turn.
	maxSummarizationAttempts = 2

	// llmCallLabel identifies compaction traffic in tracing and usage
	// accounting, separating it from the agent's own reasoning rounds.
	llmCallLabel = "agent_context_compaction"
)

// Reason records what asked for a compaction, for logging and for the UI.
type Reason string

const (
	// ReasonThreshold is the routine case: context crossed the budget.
	ReasonThreshold Reason = "threshold"
	// ReasonOverflow is repair after the provider rejected or truncated a
	// request because the window was full.
	ReasonOverflow Reason = "overflow"
	// ReasonHistoryBudget is the turn-start case: the stored history is
	// larger than the history budget of the caller's plan, which is set
	// apart from (and below) the model window.
	ReasonHistoryBudget Reason = "history_budget"
)

// ErrNothingToCompact means the context cannot be made smaller: everything
// outside the keep-recent budget is already gone. Callers must treat this as a
// stop signal rather than retrying, or they reproduce the every-round
// summarization loop this package exists to prevent.
var ErrNothingToCompact = errors.New("compaction: nothing outside the keep-recent budget")

// Result describes what one compaction did.
type Result struct {
	Messages       []chat.Message
	Summary        string
	Reason         Reason
	TokensBefore   int
	TokensAfter    int
	MessagesBefore int
	MessagesAfter  int
	// Degraded reports that at least one summary came from the raw archive
	// because the summarizer failed. The context still shrank; its memory is
	// just coarser.
	Degraded bool
	// SplitTurn reports that the cut divided a single turn, so a turn-prefix
	// summary was generated alongside the history summary.
	SplitTurn bool
	// Checkpoint is the part of this compaction a later turn can start from,
	// or nil when there is none (see Preparation.checkpoint).
	Checkpoint *Checkpoint
	// Omitted counts the oldest messages left out of the summarization
	// requests because they did not fit (see fitSummarizerInput).
	Omitted int
}

// Checkpoint is a summary that ends exactly on a stored turn. Persisted onto
// that turn, it lets the next turn load the summary in place of everything up
// to and including the turn, instead of summarizing the same history again.
type Checkpoint struct {
	// TurnID is the stored turn the summary covers through, inclusive.
	TurnID  string
	Summary string
	// Degraded reports that Summary holds a raw archive because the
	// summarizer failed. It is still a checkpoint: the next compaction
	// folds it in as the previous summary.
	Degraded bool
}

// Freed is how much room the compaction actually recovered. A non-positive
// value means the summary cost as much as the history it replaced.
func (r *Result) Freed() int {
	return r.TokensBefore - r.TokensAfter
}

// Compactor summarizes conversation history into a checkpoint.
type Compactor struct {
	chatModel chat.Chat
	// fallback is the answering model when a separate summarizer was set
	// (WithSummarizer): one more attempt runs on it before a summary
	// degrades to the raw archive. Nil when chatModel is the answering model.
	fallback  chat.Chat
	estimator *agenttoken.Estimator
	settings  Settings
}

// Process-wide compaction counts, logged with every degraded summary so a
// summarizer that keeps failing shows up as a rising number, not as a
// scattering of identical warnings.
var (
	compactionsRun      atomic.Int64
	degradedCompactions atomic.Int64
)

// Stats returns how many compactions this process ran and how many of them
// produced a degraded (raw archive) summary.
func Stats() (compactions, degraded int64) {
	return compactionsRun.Load(), degradedCompactions.Load()
}

// WithSummarizer returns a copy of the compactor whose summaries are written
// by model (window tokens of context, modelID for logs), keeping the current
// model as a fallback. A nil model returns the compactor unchanged.
func (c *Compactor) WithSummarizer(model chat.Chat, modelID string, window int) *Compactor {
	if c == nil || model == nil {
		return c
	}
	cp := *c
	cp.fallback = c.chatModel
	cp.chatModel = model
	cp.settings.SummarizerModelID = modelID
	cp.settings.SummarizerWindow = window
	return &cp
}

// New builds a compactor. It returns nil when compaction cannot run, so the
// caller can treat a nil compactor as "feature off" without a second flag.
func New(chatModel chat.Chat, estimator *agenttoken.Estimator, settings Settings) *Compactor {
	if chatModel == nil || estimator == nil || settings.MaxContextTokens <= 0 {
		return nil
	}
	return &Compactor{chatModel: chatModel, estimator: estimator, settings: settings.Normalize()}
}

// Settings returns the normalized settings, including the derived threshold.
func (c *Compactor) Settings() Settings {
	if c == nil {
		return Settings{}
	}
	return c.settings
}

// WithKeepRecentTokens returns a copy of the compactor that keeps n recent
// tokens verbatim, for a compaction sized by a budget other than the window.
// The copy is normalized like any settings, so the quarter rule still holds.
func (c *Compactor) WithKeepRecentTokens(n int) *Compactor {
	if c == nil || n <= 0 {
		return c
	}
	cp := *c
	cp.settings.KeepRecentTokens = n
	cp.settings = cp.settings.Normalize()
	return &cp
}

// Compact replaces history older than the keep-recent budget with a summary.
// It returns ErrNothingToCompact when no such history exists.
func (c *Compactor) Compact(
	ctx context.Context, messages []chat.Message, reason Reason,
) (*Result, error) {
	if c == nil {
		return nil, ErrNothingToCompact
	}
	prep := Prepare(messages, c.settings, c.estimator)
	if prep == nil {
		return nil, ErrNothingToCompact
	}

	history, historyDegraded := c.summarizeHistory(ctx, prep)
	summary, prefixDegraded := c.appendTurnPrefix(ctx, prep, history)
	summary += prep.fileOps.format()
	compacted := Apply(messages, prep, summary)

	compactionsRun.Add(1)
	if historyDegraded || prefixDegraded {
		n := degradedCompactions.Add(1)
		logger.Warnf(ctx, "[Compaction] Degraded summary (%s, summarizer=%q, history=%v, turn_prefix=%v): "+
			"%d of %d compactions in this process degraded", reason, c.summarizerLabel(),
			historyDegraded, prefixDegraded, n, compactionsRun.Load())
	}

	return &Result{
		Messages:       compacted,
		Summary:        summary,
		Reason:         reason,
		TokensBefore:   prep.TokensBefore,
		TokensAfter:    c.estimator.EstimateMessages(compacted),
		MessagesBefore: len(messages),
		MessagesAfter:  len(compacted),
		Degraded:       historyDegraded || prefixDegraded,
		SplitTurn:      prep.IsSplitTurn,
		Checkpoint:     prep.checkpoint(history, historyDegraded),
		Omitted:        prep.OmittedHistory + prep.OmittedTurnPrefix,
	}, nil
}

// summarizeHistory folds the complete turns being dropped into the previous
// summary, falling back to a raw archive when the summarizer fails.
func (c *Compactor) summarizeHistory(ctx context.Context, p *Preparation) (string, bool) {
	if len(p.MessagesToSummarize) == 0 {
		return p.PreviousSummary, false
	}
	instructions := initialSummarizationInstructions
	if p.PreviousSummary != "" {
		instructions = updateSummarizationInstructions
	}
	text, err := c.summarize(
		ctx, p.MessagesToSummarize, p.OmittedHistory, p.PreviousSummary, instructions, c.settings.summaryBudget(),
	)
	if err != nil {
		// The previous summary is still the best record of everything
		// before this span, so the archive is appended to it rather than
		// replacing it.
		return joinNonEmpty(p.PreviousSummary,
			rawArchive(p.MessagesToSummarize, c.settings.summaryBudget(), c.estimator)), true
	}
	return text, false
}

// appendTurnPrefix adds the summary of a split turn's discarded head, so the
// retained tail still has its originating request.
func (c *Compactor) appendTurnPrefix(ctx context.Context, p *Preparation, history string) (string, bool) {
	if !p.IsSplitTurn || len(p.TurnPrefixMessages) == 0 {
		return history, false
	}
	degraded := false
	prefix, err := c.summarize(
		ctx, p.TurnPrefixMessages, p.OmittedTurnPrefix, "", turnPrefixInstructions, c.settings.turnPrefixBudget(),
	)
	if err != nil {
		degraded = true
		prefix = rawArchive(p.TurnPrefixMessages, c.settings.turnPrefixBudget(), c.estimator)
	}
	if history == "" {
		history = "No prior history."
	}
	return history + splitTurnSeparator + prefix, degraded
}

// summarize runs one summarization call with retries.
//
// The call streams and is cancelled only when it stops producing output for
// the stall timeout, the rule the engine applies to its own rounds. A fixed
// total budget was wrong for exactly the calls that matter: the history being
// summarized can be most of the window, and a request that was prefilling and
// writing normally was cut off at 60 seconds, degraded, and started over on
// the next turn. The overall ceiling belongs to the provider transport.
func (c *Compactor) summarize(
	ctx context.Context,
	messages []chat.Message,
	omitted int,
	previousSummary, instructions string,
	maxTokens int,
) (string, error) {
	request := []chat.Message{
		{Role: "system", Content: summarizationSystemPrompt},
		{Role: "user", Content: buildSummarizationPrompt(messages, omitted, previousSummary, instructions)},
	}
	// Thinking is off explicitly: a summary is a transcription, and a
	// reasoning model left to its default spent the completion budget on
	// thoughts and stopped on the length cap before writing the summary.
	thinking := false
	opts := &chat.ChatOptions{
		Temperature:    0.3, // low temperature for factual summarization
		MaxTokens:      maxTokens,
		CacheRetention: chat.CacheRetentionNone,
		Thinking:       &thinking,
	}
	var lastErr error

	// The summarizer gets its attempts; a separate summarizer that fails
	// them all leaves one more to the answering model before degrading.
	models := []chat.Chat{c.chatModel}
	if c.fallback != nil {
		models = append(models, c.fallback)
	}
	attempts := 0
	for i, model := range models {
		tries := maxSummarizationAttempts
		if i > 0 {
			tries = 1
			logger.Warnf(ctx, "[Compaction] Summarizer %q failed (%v); retrying on the answering model",
				c.summarizerLabel(), lastErr)
		}
		for try := 0; try < tries; try++ {
			attempts++
			content, finishReason, err := c.streamSummary(ctx, model, request, opts)
			if err == nil {
				err = validateSummary(content, finishReason)
			}
			if err == nil {
				return strings.TrimSpace(content), nil
			}
			lastErr = err
			if ctx.Err() != nil {
				// the turn itself was stopped; another attempt cannot help
				return "", fmt.Errorf("summarization failed after %d attempts: %w", attempts, lastErr)
			}
		}
	}

	return "", fmt.Errorf("summarization failed after %d attempts: %w", attempts, lastErr)
}

// summarizerLabel names the summarizer in logs.
func (c *Compactor) summarizerLabel() string {
	if c.settings.SummarizerModelID != "" {
		return c.settings.SummarizerModelID
	}
	if c.chatModel != nil {
		return c.chatModel.GetModelName()
	}
	return ""
}

// streamSummary collects one summarization stream. Output of any kind, the
// model's reasoning included, counts as progress and resets the stall timer.
//
// Summarization tokens are not metered against the caller's usage plan (they
// stay out of the turn's usage); they are logged under llmCallLabel so their
// cost is still visible.
func (c *Compactor) streamSummary(
	ctx context.Context, model chat.Chat, messages []chat.Message, opts *chat.ChatOptions,
) (content, finishReason string, err error) {
	callCtx, cancel := context.WithCancel(types.WithLLMCallMetadata(ctx, llmCallLabel, ""))
	defer cancel()
	var usage *types.TokenUsage
	defer func() {
		if usage != nil {
			logger.Infof(ctx, "[Compaction] LLM usage purpose=%s model=%s: prompt=%d completion=%d total=%d "+
				"(not metered)", llmCallLabel, model.GetModelName(),
				usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
		}
	}()
	stream, err := model.ChatStream(callCtx, messages, opts)
	if err != nil {
		return "", "", err
	}
	if stream == nil {
		return "", "", errors.New("summarization stream was not opened")
	}
	// Anything the provider still sends after this returns is discarded, so
	// its goroutine can finish once the cancelled context closes the stream.
	defer func() {
		go func() {
			for {
				if _, ok := <-stream; !ok {
					return
				}
			}
		}()
	}()

	timeout := c.settings.stallTimeout()
	stall := time.NewTimer(timeout)
	defer stall.Stop()
	var sb strings.Builder
	streamErr := ""
	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-stall.C:
			return "", "", fmt.Errorf("summarization stalled: no output for %s", timeout)
		case chunk, ok := <-stream:
			if !ok {
				// a stream that ends because the turn was stopped holds a cut
				// summary, not a finished one
				if err := ctx.Err(); err != nil {
					return "", "", err
				}
				if streamErr != "" {
					return "", finishReason, fmt.Errorf("summarization stream error: %s", streamErr)
				}
				return sb.String(), finishReason, nil
			}
			stall.Reset(timeout)
			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			switch chunk.ResponseType {
			case types.ResponseTypeError:
				streamErr = chunk.Content
			case types.ResponseTypeThinking:
				// progress, but not part of the summary
			default:
				sb.WriteString(chunk.Content)
			}
		}
	}
}

// validateSummary rejects responses that cannot serve as a checkpoint.
//
// The length stop is the one worth spelling out: a summary cut off by the token
// cap reads like a valid summary but silently ends mid-section, and every later
// round inherits that truncation as its only memory of the dropped history.
// A partial summary is a failure: it cannot serve as a checkpoint.
func validateSummary(content, finishReason string) error {
	if strings.TrimSpace(content) == "" {
		return errors.New("empty response from LLM")
	}
	switch strings.ToLower(strings.TrimSpace(finishReason)) {
	case "length", "max_tokens", "max_output_tokens":
		return errors.New("generation hit the token cap and the summary is incomplete")
	}
	return nil
}

// buildSummarizationPrompt wraps the transcript in a tag and puts the
// instructions last, so the summarizer cannot mistake conversation text for
// its own instructions.
func buildSummarizationPrompt(messages []chat.Message, omitted int, previousSummary, instructions string) string {
	var sb strings.Builder
	sb.WriteString("<conversation>\n")
	if omitted > 0 {
		fmt.Fprintf(&sb, "[%d earlier messages are not shown: they did not fit in one summarization "+
			"request, and no previous summary covers them. Do not guess at what they said.]\n\n", omitted)
	}
	sb.WriteString(serializeConversation(messages))
	sb.WriteString("\n</conversation>\n\n")
	if previousSummary != "" {
		sb.WriteString("<previous-summary>\n")
		sb.WriteString(previousSummary)
		sb.WriteString("\n</previous-summary>\n\n")
	}
	sb.WriteString(instructions)
	return sb.String()
}

func joinNonEmpty(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}
