package service

import (
	"container/list"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/compaction"
	agenttoken "github.com/Tencent/WeKnora/internal/agent/token"
	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// contextUsageLatestRows is how many of the newest rows are read to find the
// latest completed turn: a turn is two rows plus one per steered message,
// and a running turn or a few steered messages can sit above it.
const contextUsageLatestRows = 20

// The history estimate cache: entries live contextUsageCacheTTL, and at
// most contextUsageCacheSize sessions are kept (least recently used out).
const (
	contextUsageCacheTTL  = 60 * time.Second
	contextUsageCacheSize = 512
)

// contextUsageCache is a small bounded LRU of history estimates by key
// (historyCacheKey), each kept for a TTL. In memory per backend process:
// a miss only costs one estimate.
type contextUsageCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	size    int
	now     func() time.Time
	order   *list.List // front = most recently used; values are *contextUsageEntry
	entries map[string]*list.Element
	// sessions maps a session to its one live key, so a superseded state
	// is dropped instead of waiting out its TTL.
	sessions map[string]string
}

type contextUsageEntry struct {
	key     string
	session string
	value   contextHistory
	expires time.Time
}

func newContextUsageCache(ttl time.Duration, size int) *contextUsageCache {
	return &contextUsageCache{
		ttl: ttl, size: size, now: time.Now, order: list.New(),
		entries: make(map[string]*list.Element), sessions: make(map[string]string),
	}
}

func (c *contextUsageCache) get(key string) (contextHistory, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return contextHistory{}, false
	}
	entry := el.Value.(*contextUsageEntry)
	if !c.now().Before(entry.expires) {
		c.remove(el)
		return contextHistory{}, false
	}
	c.order.MoveToFront(el)
	return entry.value, true
}

// put stores value under key for session (the key's first field) and drops
// that session's previous key.
func (c *contextUsageCache) put(key string, value contextHistory) {
	session, _, _ := strings.Cut(key, "|")
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.sessions[session]; ok && old != key {
		if el, ok := c.entries[old]; ok {
			c.remove(el)
		}
	}
	if el, ok := c.entries[key]; ok {
		c.remove(el)
	}
	c.entries[key] = c.order.PushFront(&contextUsageEntry{
		key: key, session: session, value: value, expires: c.now().Add(c.ttl),
	})
	c.sessions[session] = key
	for c.order.Len() > c.size {
		c.remove(c.order.Back())
	}
}

func (c *contextUsageCache) remove(el *list.Element) {
	entry := el.Value.(*contextUsageEntry)
	c.order.Remove(el)
	delete(c.entries, entry.key)
	if c.sessions[entry.session] == entry.key {
		delete(c.sessions, entry.session)
	}
}

func (c *contextUsageCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// completedTurnCounter is the optional repository method that counts the
// turns a checkpoint covers. Asserted rather than added to
// interfaces.MessageRepository so its many fakes need not grow a method.
type completedTurnCounter interface {
	CountCompletedTurnsThrough(ctx context.Context, sessionID string, at time.Time, id string) (int64, error)
}

// SessionContextUsageService answers how full a session's conversation
// history is for its next turn (GET /sessions/:id/context-usage). It reads,
// never writes: the history is estimated by the loaders' own dry runs and
// the plan is resolved by the usage guard's preview.
type SessionContextUsageService struct {
	cfg                *config.Config
	messageRepo        interfaces.MessageRepository
	messageService     interfaces.MessageService
	modelService       interfaces.ModelService
	customAgentService interfaces.CustomAgentService
	usageGuard         interfaces.UsageGuard
	documentWorkspaces interfaces.DocumentWorkspaceService
	cache              *contextUsageCache
}

// NewSessionContextUsageService creates the service. usageGuard and
// documentWorkspaces may be nil.
func NewSessionContextUsageService(
	cfg *config.Config,
	messageRepo interfaces.MessageRepository,
	messageService interfaces.MessageService,
	modelService interfaces.ModelService,
	customAgentService interfaces.CustomAgentService,
	usageGuard interfaces.UsageGuard,
	documentWorkspaces interfaces.DocumentWorkspaceService,
) *SessionContextUsageService {
	return &SessionContextUsageService{
		cfg:                cfg,
		messageRepo:        messageRepo,
		messageService:     messageService,
		modelService:       modelService,
		customAgentService: customAgentService,
		usageGuard:         usageGuard,
		documentWorkspaces: documentWorkspaces,
		cache:              newContextUsageCache(contextUsageCacheTTL, contextUsageCacheSize),
	}
}

// Usage reports session's context usage. The caller must own session
// (SessionService.GetOwnedSession); tenant and user come from ctx.
func (s *SessionContextUsageService) Usage(
	ctx context.Context, session *types.Session,
) (*types.SessionContextUsage, error) {
	if session == nil {
		return nil, fmt.Errorf("context usage: no session")
	}
	newest, err := s.messageRepo.ListMessagesBySessionBeforeCursor(
		ctx, session.ID, time.Time{}, "", contextUsageLatestRows,
	)
	if err != nil {
		return nil, fmt.Errorf("context usage: %w", err)
	}
	latest := latestCompletedAssistant(newest)
	customAgent := s.sessionAgent(ctx, session, latest)
	docs := s.sessionDocuments(ctx, session)

	out := &types.SessionContextUsage{
		ModelWindow: types.AgentMaxContextTokens(0, s.modelWindow(ctx, session, latest, customAgent)),
		LastTurn:    lastTurnUsage(latest),
	}

	preview := &types.UsageSessionPreview{}
	if s.usageGuard != nil {
		tenantID, _ := types.TenantIDFromContext(ctx)
		userID, _ := types.UserIDFromContext(ctx)
		capMode := types.SessionCapHard
		if isDocumentAssistant(customAgent) || hasWordDocument(docs) {
			capMode = types.SessionCapSoft
		}
		if p, err := s.usageGuard.PreviewSession(ctx, tenantID, userID, session.ID, capMode); err != nil {
			// the indicator still shows the history without the plan
			logger.Warnf(ctx, "context usage: plan preview of session %s failed: %v", session.ID, err)
		} else if p != nil {
			preview = p
		}
	}
	out.Enforced, out.PlanName = preview.Enforced, preview.PlanName
	out.Denied, out.DenialReason = preview.Denied, preview.DenialReason
	out.TurnTokenCap = preview.Features.TurnTokenCap
	out.HistoryBudget = preview.Features.EffectiveHistoryTokens(out.ModelWindow)
	out.Session = types.SessionContextSessionStatus{
		Turns: preview.Session.Turns, MaxTurns: preview.Session.MaxTurns,
		Tokens: preview.Session.Tokens, MaxTokens: preview.Session.MaxTokens,
		Warning: preview.Session.Warning, Reached: preview.Session.Reached, Soft: preview.Session.Soft,
	}

	// The history estimate is the costly part (it reads and prices up to
	// the window); it only changes with a new or updated row, a new
	// checkpoint, or the inputs that shape the replay.
	key := s.historyCacheKey(ctx, session, newest, latest, customAgent, docs, out.ModelWindow)
	if cached, ok := s.cache.get(key); ok {
		cached.applyTo(out)
		return out, nil
	}
	snapshot := &types.SessionContextUsage{ModelWindow: out.ModelWindow}
	var checkpoint *types.Message
	if customAgent == nil || customAgent.IsAgentMode() {
		snapshot.Mode = types.ContextUsageModeAgent
		checkpoint, err = s.estimateAgentHistory(ctx, session, customAgent, docs, snapshot)
	} else {
		snapshot.Mode = types.ContextUsageModeQuickAnswer
		checkpoint, err = s.estimateQuickAnswerHistory(ctx, session, customAgent, newest, snapshot)
	}
	if err != nil {
		return nil, err
	}
	if checkpoint != nil {
		snapshot.Compacted = true
		snapshot.CompactedTurns = s.countCompactedTurns(ctx, session.ID, checkpoint)
	}
	hist := contextHistory{
		mode: snapshot.Mode, historyTokens: snapshot.HistoryTokens, summaryTokens: snapshot.SummaryTokens,
		compacted: snapshot.Compacted, compactedTurns: snapshot.CompactedTurns, seeded: snapshot.Seeded,
		historyRounds: snapshot.HistoryRounds,
	}
	s.cache.put(key, hist)
	hist.applyTo(out)
	return out, nil
}

// contextHistory is the cached part of a SessionContextUsage: what the
// history estimate found.
type contextHistory struct {
	mode           string
	historyTokens  int
	summaryTokens  int
	compacted      bool
	compactedTurns int
	seeded         bool
	historyRounds  int
}

func (h contextHistory) applyTo(out *types.SessionContextUsage) {
	out.Mode, out.HistoryTokens, out.SummaryTokens = h.mode, h.historyTokens, h.summaryTokens
	out.Compacted, out.CompactedTurns, out.Seeded = h.compacted, h.compactedTurns, h.seeded
	out.HistoryRounds = h.historyRounds
}

// historyCacheKey names one state of a session's history: its newest row
// and newest completed turn (a new turn, a stop or an edit moves them),
// its newest checkpoint (a compaction writes one, possibly on an older
// turn), and what shapes the replay (agent, window, documents, seed).
func (s *SessionContextUsageService) historyCacheKey(
	ctx context.Context, session *types.Session, newest []*types.Message, latest *types.Message,
	customAgent *types.CustomAgent, docs []*types.DocumentWorkspace, window int,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|w%d|seed%d", session.ID, window, len(session.ContextSeed))
	if len(newest) > 0 {
		fmt.Fprintf(&b, "|n:%s@%d", newest[0].ID, newest[0].UpdatedAt.UnixNano())
	}
	if latest != nil {
		fmt.Fprintf(&b, "|a:%s@%d", latest.ID, latest.UpdatedAt.UnixNano())
	}
	if cp := loadContextCheckpoint(ctx, s.messageRepo, session.ID); cp != nil {
		fmt.Fprintf(&b, "|c:%s@%d", cp.ID, cp.ContextCheckpoint.CreatedAt.UnixNano())
	}
	if customAgent != nil {
		c := customAgent.Config
		fmt.Fprintf(&b, "|g:%s:%s:%t:%t:%d", customAgent.ID, c.AgentMode, c.MultiTurnEnabled,
			c.RetainRetrievalHistory, c.HistoryTurns)
	}
	for _, d := range docs {
		fmt.Fprintf(&b, "|d:%s:%s:%t", d.ID, d.AttachmentID, d.IsTarget())
	}
	return b.String()
}

// estimateAgentHistory fills out with what LoadAgentHistory would load for
// the next agent turn (read up to the window, as the turn reads it) and
// returns the checkpoint the history starts from.
func (s *SessionContextUsageService) estimateAgentHistory(
	ctx context.Context, session *types.Session, customAgent *types.CustomAgent,
	docs []*types.DocumentWorkspace, out *types.SessionContextUsage,
) (*types.Message, error) {
	retain := false
	if customAgent != nil {
		if !customAgent.Config.MultiTurnEnabled {
			return nil, nil
		}
		retain = customAgent.Config.RetainRetrievalHistory
	}
	opts := []AgentHistoryOption{WithHistorySeed(session.ContextSeed)}
	if len(docs) > 0 {
		opts = append(opts, WithHistoryDocuments(docs))
	}
	est, err := EstimateAgentHistory(ctx, s.messageRepo, session.ID, out.ModelWindow, retain, opts...)
	if err != nil {
		return nil, fmt.Errorf("context usage: %w", err)
	}
	out.HistoryTokens, out.SummaryTokens, out.Seeded = est.Tokens, est.SummaryTokens, est.Seeded
	out.Compacted = out.Seeded
	return est.Checkpoint, nil
}

// estimateQuickAnswerHistory fills out with what the quick-answer
// pipeline's LOAD_HISTORY stage would give the next turn: the summary plus
// the last rounds, priced with the session's token scale.
func (s *SessionContextUsageService) estimateQuickAnswerHistory(
	ctx context.Context, session *types.Session, customAgent *types.CustomAgent,
	newest []*types.Message, out *types.SessionContextUsage,
) (*types.Message, error) {
	maxRounds := 0
	if s.cfg != nil && s.cfg.Conversation != nil {
		maxRounds = s.cfg.Conversation.MaxRounds
	}
	if customAgent.Config.HistoryTurns > 0 {
		maxRounds = customAgent.Config.HistoryTurns
	}
	if !customAgent.Config.MultiTurnEnabled || maxRounds <= 0 {
		return nil, nil
	}
	history, summary, err := chatpipeline.LoadHistoryAndSummary(ctx, s.messageService, session.ID, maxRounds)
	if err != nil {
		return nil, fmt.Errorf("context usage: %w", err)
	}
	estimator, err := agenttoken.NewEstimator()
	if err != nil {
		return nil, fmt.Errorf("context usage: %w", err)
	}
	estimator.SetScale(latestContextTokenScale(newest))
	if strings.TrimSpace(summary) != "" {
		msg := compaction.SummaryMessage(summary)
		out.SummaryTokens = estimator.EstimateMessage(&msg)
	}
	msgs := make([]chat.Message, 0, len(history)*2)
	for _, h := range history {
		msgs = append(msgs, chat.Message{Role: "user", Content: h.Query},
			chat.Message{Role: "assistant", Content: h.Answer})
	}
	out.HistoryTokens = out.SummaryTokens + estimator.EstimateMessages(msgs)
	out.HistoryRounds = maxRounds
	if out.SummaryTokens == 0 {
		return nil, nil
	}
	// after a summary the pipeline keeps fewer raw rounds
	out.HistoryRounds = min(maxRounds, chatpipeline.SummarizedHistoryRounds)
	checkpoint := loadContextCheckpoint(ctx, s.messageRepo, session.ID)
	out.Seeded = checkpoint == nil
	out.Compacted = out.Seeded
	return checkpoint, nil
}

// countCompactedTurns is how many turns checkpoint covers, 0 when the
// repository cannot count them.
func (s *SessionContextUsageService) countCompactedTurns(
	ctx context.Context, sessionID string, checkpoint *types.Message,
) int {
	counter, ok := s.messageRepo.(completedTurnCounter)
	if !ok {
		return 0
	}
	n, err := counter.CountCompletedTurnsThrough(ctx, sessionID, checkpoint.CreatedAt, checkpoint.ID)
	if err != nil {
		logger.Warnf(ctx, "context usage: counting the compacted turns of session %s failed: %v", sessionID, err)
		return 0
	}
	return int(n)
}

// sessionAgent is the agent the session's next turn most likely runs: the
// latest turn's, else the one the session last asked for. Nil when neither
// resolves; the caller then assumes agent-mode history.
func (s *SessionContextUsageService) sessionAgent(
	ctx context.Context, session *types.Session, latest *types.Message,
) *types.CustomAgent {
	if s.customAgentService == nil {
		return nil
	}
	agentID, agentTenantID := "", uint64(0)
	if latest != nil {
		agentID, agentTenantID = latest.AgentID, latest.AgentTenantID
	}
	if agentID == "" && session.LastRequestState != nil {
		agentID = session.LastRequestState.AgentID
	}
	if agentID == "" {
		return nil
	}
	if a, err := s.customAgentService.GetAgentByID(ctx, agentID); err == nil && a != nil {
		return a
	}
	// a shared agent lives in its own workspace
	if agentTenantID != 0 {
		if a, err := s.customAgentService.GetAgentByIDAndTenant(ctx, agentID, agentTenantID); err == nil && a != nil {
			return a
		}
	}
	return nil
}

// modelWindow is the declared context window of the session's chat model,
// 0 when unknown (types.AgentMaxContextTokens then falls back).
func (s *SessionContextUsageService) modelWindow(
	ctx context.Context, session *types.Session, latest *types.Message, customAgent *types.CustomAgent,
) int {
	modelID := ""
	if latest != nil {
		modelID = latest.ModelID
	}
	if modelID == "" && session.LastRequestState != nil {
		modelID = session.LastRequestState.ModelID
	}
	if modelID == "" && customAgent != nil {
		modelID = customAgent.Config.ModelID
	}
	if modelID == "" || s.modelService == nil {
		return 0
	}
	m, err := s.modelService.GetModelByID(ctx, modelID)
	if err != nil || m == nil {
		return 0
	}
	return m.Parameters.ContextWindow
}

// sessionDocuments lists the session's documents, nil when there are none
// or they cannot be read (the estimate then replays attachments as stored).
func (s *SessionContextUsageService) sessionDocuments(
	ctx context.Context, session *types.Session,
) []*types.DocumentWorkspace {
	if s.documentWorkspaces == nil || !s.documentWorkspaces.DocumentsEnabled() {
		return nil
	}
	docs, err := s.documentWorkspaces.List(ctx, session.TenantID, session.ID)
	if err != nil {
		logger.Warnf(ctx, "context usage: documents of session %s: %v", session.ID, err)
		return nil
	}
	return docs
}

func isDocumentAssistant(a *types.CustomAgent) bool {
	return a != nil && a.ID == types.BuiltinDocumentAssistantID
}

func hasWordDocument(docs []*types.DocumentWorkspace) bool {
	for _, d := range docs {
		if d.IsWordAddin() {
			return true
		}
	}
	return false
}

// latestCompletedAssistant is the newest completed assistant row of rows.
func latestCompletedAssistant(rows []*types.Message) *types.Message {
	var newest *types.Message
	for _, msg := range rows {
		if msg.Role != "assistant" || !msg.IsCompleted {
			continue
		}
		if newest == nil || sortsAtOrBefore(newest, msg) {
			newest = msg
		}
	}
	return newest
}

// lastTurnUsage is the token usage of turn msg: what it sent and received
// over all its rounds, and how many rounds it ran (distinct iterations of
// its steps, leaving out the stages a quick-answer turn records for its
// timeline). Nil when msg is nil or recorded no usage.
func lastTurnUsage(msg *types.Message) *types.SessionContextLastTurn {
	if msg == nil || msg.Usage == nil {
		return nil
	}
	iterations := make(map[int]bool)
	for _, step := range msg.AgentSteps {
		pipelineOnly := len(step.ToolCalls) > 0
		for _, tc := range step.ToolCalls {
			pipelineOnly = pipelineOnly && types.IsPipelineToolCallID(tc.ID)
		}
		if !pipelineOnly {
			iterations[step.Iteration] = true
		}
	}
	return &types.SessionContextLastTurn{
		PromptTokens:     msg.Usage.PromptTokens,
		CompletionTokens: msg.Usage.CompletionTokens,
		Rounds:           max(len(iterations), 1),
	}
}
