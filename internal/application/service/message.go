package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// regThinkIndex matches <think>...</think> blocks for stripping from KB index content.
var regThinkIndex = regexp.MustCompile(`(?s)<think>.*?</think>`)

// messageService implements the MessageService interface for managing messaging operations
// It handles creating, retrieving, updating, and deleting messages within sessions.
// It reads the chat history knowledge base configuration from the tenant's ChatHistoryConfig,
// which is managed via the settings UI.
type messageService struct {
	messageRepo    interfaces.MessageRepository    // Repository for message storage operations
	sessionRepo    interfaces.SessionRepository    // Repository for session validation
	tenantService  interfaces.TenantService        // Service for tenant operations (read ChatHistoryConfig)
	kbService      interfaces.KnowledgeBaseService // Service for knowledge base operations (search chat history KB)
	knowService    interfaces.KnowledgeService     // Service for knowledge operations (index/delete passages)
	modelService   interfaces.ModelService         // Service for model operations (rerank model)
	suggestionRepo interfaces.MessageSuggestionRepository
	// usageMeter counts questions and tokens for usage packages; nil in
	// unit tests that build the struct directly.
	usageMeter interfaces.UsageMeter
	// kbAccessGrantService resolves KB scopes, tenant grants and invites
	// for hiding citations the caller can no longer read; nil hides
	// nothing.
	kbAccessGrantService interfaces.KBAccessGrantService
}

// NewMessageService creates a new message service instance with the required repositories
func NewMessageService(messageRepo interfaces.MessageRepository,
	sessionRepo interfaces.SessionRepository,
	tenantService interfaces.TenantService,
	kbService interfaces.KnowledgeBaseService,
	knowService interfaces.KnowledgeService,
	modelService interfaces.ModelService,
	suggestionRepo interfaces.MessageSuggestionRepository,
	usageMeter interfaces.UsageMeter,
	kbAccessGrantService interfaces.KBAccessGrantService,
) interfaces.MessageService {
	return &messageService{
		kbAccessGrantService: kbAccessGrantService,
		messageRepo:          messageRepo,
		sessionRepo:          sessionRepo,
		tenantService:        tenantService,
		kbService:            kbService,
		knowService:          knowService,
		modelService:         modelService,
		suggestionRepo:       suggestionRepo,
		usageMeter:           usageMeter,
	}
}

// sessionTenantIDForLookup returns the tenant ID to use for session lookup.
// When SessionTenantIDContextKey is set (e.g. pipeline with shared agent), use it so session/message belong to session owner.
func sessionTenantIDForLookup(ctx context.Context) (uint64, bool) {
	if v := ctx.Value(types.SessionTenantIDContextKey); v != nil {
		if tid, ok := v.(uint64); ok && tid != 0 {
			return tid, true
		}
	}
	if v := ctx.Value(types.TenantIDContextKey); v != nil {
		if tid, ok := v.(uint64); ok {
			return tid, true
		}
	}
	return 0, false
}

func sessionUserIDForLookup(ctx context.Context) string {
	if ctx.Value(types.SessionTenantIDContextKey) != nil {
		// Shared-agent pipelines resolve the session owner tenant first; keep that internal lookup tenant-scoped.
		return ""
	}
	return types.SessionOwnerIDFromContext(ctx)
}

// CreateMessage creates a new message within an existing session
func (s *messageService) CreateMessage(ctx context.Context, message *types.Message) (*types.Message, error) {
	logger.Info(ctx, "Start creating message")
	logger.Infof(ctx, "Creating message for session ID: %s", message.SessionID)

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Checking if session exists, tenant ID: %d, session ID: %s", tenantID, message.SessionID)
	_, err := s.sessionRepo.Get(ctx, tenantID, sessionUserIDForLookup(ctx), message.SessionID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get session: %v", err)
		return nil, err
	}

	logger.Info(ctx, "Session exists, creating message")
	createdMessage, err := s.messageRepo.CreateMessage(ctx, message)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id": message.SessionID,
		})
		return nil, err
	}

	logger.Infof(ctx, "Message created successfully, ID: %s", createdMessage.ID)
	s.meterMessage(ctx, tenantID, createdMessage, true)
	return createdMessage, nil
}

// meterMessage feeds the usage counters. A user message is one question
// (counted on create, so a cancelled turn still spent it); an assistant
// message contributes its token usage once, whenever it is first saved
// with usage attached. Both are best-effort and never fail the write.
func (s *messageService) meterMessage(ctx context.Context, tenantID uint64, message *types.Message, created bool) {
	if s.usageMeter == nil || message == nil {
		return
	}
	switch message.Role {
	case "user":
		if created {
			userID, _ := types.UserIDFromContext(ctx)
			// A steered row (a message injected into a running turn) does
			// not open a turn, so it does not count toward the session's
			// turn cap. The day/month question counters still count every
			// user row, as they always have: only the session scope skips it.
			sessionID := message.SessionID
			if types.IsSteeredMessage(ctx) {
				sessionID = ""
			}
			s.usageMeter.RecordQuestion(ctx, tenantID, userID, sessionID)
		}
	case "assistant":
		s.usageMeter.RecordMessageUsage(ctx, message)
	}
}

// GetMessage retrieves a specific message by its ID within a session
func (s *messageService) GetMessage(ctx context.Context, sessionID string, messageID string) (*types.Message, error) {
	logger.Info(ctx, "Start getting message")
	logger.Infof(ctx, "Getting message, session ID: %s, message ID: %s", sessionID, messageID)

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Checking if session exists, tenant ID: %d", tenantID)
	_, err := loadSessionForRead(ctx, s.sessionRepo, tenantID, sessionUserIDForLookup(ctx), sessionID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get session: %v", err)
		return nil, err
	}

	logger.Info(ctx, "Session exists, getting message")
	message, err := s.messageRepo.GetMessage(ctx, sessionID, messageID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id": sessionID,
			"message_id": messageID,
		})
		return nil, err
	}

	logger.Info(ctx, "Message retrieved successfully")
	return s.clarifyReadArtifactVersions(ctx, sessionID, []*types.Message{message})[0], nil
}

// GetMessagesBySession retrieves paginated messages for a specific session
func (s *messageService) GetMessagesBySession(ctx context.Context,
	sessionID string, page int, pageSize int,
) ([]*types.Message, error) {
	logger.Info(ctx, "Start getting messages by session")
	logger.Infof(ctx, "Getting messages for session ID: %s, page: %d, pageSize: %d", sessionID, page, pageSize)

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Checking if session exists, tenant ID: %d", tenantID)
	_, err := loadSessionForRead(ctx, s.sessionRepo, tenantID, sessionUserIDForLookup(ctx), sessionID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get session: %v", err)
		return nil, err
	}

	logger.Info(ctx, "Session exists, getting messages")
	messages, err := s.messageRepo.GetMessagesBySession(ctx, sessionID, page, pageSize)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id": sessionID,
			"page":       page,
			"page_size":  pageSize,
		})
		return nil, err
	}

	logger.Infof(ctx, "Retrieved %d messages successfully", len(messages))
	return s.clarifyReadArtifactVersions(ctx, sessionID, messages), nil
}

// GetRecentMessagesBySession retrieves the most recent messages from a session
func (s *messageService) GetRecentMessagesBySession(ctx context.Context,
	sessionID string, limit int,
) ([]*types.Message, error) {
	logger.Info(ctx, "Start getting recent messages by session")
	logger.Infof(ctx, "Getting recent messages for session ID: %s, limit: %d", sessionID, limit)

	tenantID, ok := sessionTenantIDForLookup(ctx)
	if !ok {
		logger.Error(ctx, "Organization ID not found in context for session lookup")
		return nil, errors.New("organization ID not found in context")
	}
	logger.Infof(ctx, "Checking if session exists, tenant ID: %d", tenantID)
	_, err := loadSessionForRead(ctx, s.sessionRepo, tenantID, sessionUserIDForLookup(ctx), sessionID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get session: %v", err)
		return nil, err
	}

	logger.Info(ctx, "Session exists, getting recent messages")
	messages, err := s.messageRepo.GetRecentMessagesBySession(ctx, sessionID, limit)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id": sessionID,
			"limit":      limit,
		})
		return nil, err
	}

	logger.Infof(ctx, "Retrieved %d recent messages successfully", len(messages))
	s.hideUnreadableReferences(ctx, messages)
	return s.clarifyReadArtifactVersions(ctx, sessionID, messages), nil
}

// GetMessagesBySessionBeforeTime retrieves messages sent before a specific time
func (s *messageService) GetMessagesBySessionBeforeTime(ctx context.Context,
	sessionID string, beforeTime time.Time, limit int,
) ([]*types.Message, error) {
	logger.Info(ctx, "Start getting messages before time")
	logger.Infof(ctx, "Getting messages before %v for session ID: %s, limit: %d", beforeTime, sessionID, limit)

	tenantID, ok := sessionTenantIDForLookup(ctx)
	if !ok {
		logger.Error(ctx, "Organization ID not found in context for session lookup")
		return nil, errors.New("organization ID not found in context")
	}
	logger.Infof(ctx, "Checking if session exists, tenant ID: %d", tenantID)
	_, err := loadSessionForRead(ctx, s.sessionRepo, tenantID, sessionUserIDForLookup(ctx), sessionID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get session: %v", err)
		return nil, err
	}

	logger.Info(ctx, "Session exists, getting messages before time")
	messages, err := s.messageRepo.GetMessagesBySessionBeforeTime(ctx, sessionID, beforeTime, limit)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id":  sessionID,
			"before_time": beforeTime,
			"limit":       limit,
		})
		return nil, err
	}

	logger.Infof(ctx, "Retrieved %d messages before time successfully", len(messages))
	s.hideUnreadableReferences(ctx, messages)
	return s.clarifyReadArtifactVersions(ctx, sessionID, messages), nil
}

// UpdateMessage updates an existing message's content or metadata
func (s *messageService) UpdateMessage(ctx context.Context, message *types.Message) error {
	logger.Info(ctx, "Start updating message")
	logger.Infof(ctx, "Updating message, ID: %s, session ID: %s", message.ID, message.SessionID)

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Checking if session exists, tenant ID: %d", tenantID)
	_, err := s.sessionRepo.Get(ctx, tenantID, sessionUserIDForLookup(ctx), message.SessionID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get session: %v", err)
		return err
	}

	logger.Info(ctx, "Session exists, updating message")
	err = s.messageRepo.UpdateMessage(ctx, message)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id": message.SessionID,
			"message_id": message.ID,
		})
		return err
	}

	logger.Info(ctx, "Message updated successfully")
	s.meterMessage(ctx, tenantID, message, false)
	return nil
}

// UpdateMessageImages updates only the images JSONB column for a message.
func (s *messageService) UpdateMessageImages(ctx context.Context, sessionID, messageID string, images types.MessageImages) error {
	return s.messageRepo.UpdateMessageImages(ctx, sessionID, messageID, images)
}

// UpdateMessageRenderedContent updates the rendered_content column for a user message.
func (s *messageService) UpdateMessageRenderedContent(ctx context.Context, sessionID, messageID string, renderedContent string) error {
	return s.messageRepo.UpdateMessageRenderedContent(ctx, sessionID, messageID, renderedContent)
}

// langfuseFeedbackScore is the Langfuse score name user votes are recorded under.
const langfuseFeedbackScore = "user-feedback"

// SetMessageFeedback records the session owner's vote on a completed
// assistant message. The database row is the source of truth; the Langfuse
// score is mirrored in the background, so an unreachable Langfuse never fails
// the vote.
func (s *messageService) SetMessageFeedback(
	ctx context.Context, sessionID, messageID string, feedback int, comment string,
) (*types.Message, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	if _, err := s.sessionRepo.Get(ctx, tenantID, sessionUserIDForLookup(ctx), sessionID); err != nil {
		return nil, err
	}
	msg, err := s.messageRepo.GetMessage(ctx, sessionID, messageID)
	if err != nil {
		return nil, err
	}
	if msg.Role != "assistant" || !msg.IsCompleted {
		return nil, apperrors.ErrFeedbackNotAllowed
	}

	var at *time.Time
	if feedback == 0 {
		comment = ""
	} else {
		now := time.Now()
		at = &now
	}
	if err := s.messageRepo.UpdateMessageFeedback(ctx, sessionID, messageID, feedback, comment, at); err != nil {
		return nil, err
	}
	msg.Feedback, msg.FeedbackComment, msg.FeedbackAt = feedback, comment, at

	if traceID := langfuse.TraceIDFromTraceparent(msg.ExecutionContext.LangfuseTraceparent); traceID != "" {
		bgCtx := context.WithoutCancel(ctx)
		go func() {
			mgr := langfuse.GetManager()
			scoreID := langfuseFeedbackScore + "-" + messageID
			var err error
			if feedback == 0 {
				err = mgr.DeleteScore(bgCtx, scoreID)
			} else {
				err = mgr.UpsertScore(bgCtx, langfuse.Score{
					ID:      scoreID,
					TraceID: traceID,
					Name:    langfuseFeedbackScore,
					Value:   float64(feedback),
					Comment: comment,
					Metadata: map[string]interface{}{
						"session_id": sessionID,
						"message_id": messageID,
					},
				})
			}
			if err != nil {
				logger.Warnf(bgCtx, "[feedback] mirror vote on message %s to Langfuse failed: %v", messageID, err)
			}
		}()
	}
	return msg, nil
}

// DeleteMessage removes a message from a session, also cleaning up its Knowledge entry in the chat history KB.
func (s *messageService) DeleteMessage(ctx context.Context, sessionID string, messageID string) error {
	logger.Info(ctx, "Start deleting message")
	logger.Infof(ctx, "Deleting message, session ID: %s, message ID: %s", sessionID, messageID)

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Checking if session exists, tenant ID: %d", tenantID)
	_, err := s.sessionRepo.Get(ctx, tenantID, sessionUserIDForLookup(ctx), sessionID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get session: %v", err)
		return err
	}

	// Get the message first to check if it has an associated Knowledge entry
	msg, err := s.messageRepo.GetMessage(ctx, sessionID, messageID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get message for deletion: %v", err)
		return err
	}

	// Delete the message from the repository
	logger.Info(ctx, "Session exists, deleting message")
	err = s.messageRepo.DeleteMessage(ctx, sessionID, messageID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id": sessionID,
			"message_id": messageID,
		})
		return err
	}
	if s.suggestionRepo != nil {
		if err := s.suggestionRepo.DeleteByMessageID(ctx, tenantID, sessionID, messageID); err != nil {
			logger.Warnf(ctx, "Failed to delete suggestions for message %s: %v", messageID, err)
		}
	}

	// Async cleanup: delete the associated Knowledge entry from the chat history KB.
	// Use WithoutCancel so the goroutine survives after the HTTP request context is done.
	if msg != nil && msg.KnowledgeID != "" {
		bgCtx := context.WithoutCancel(ctx)
		go s.DeleteMessageKnowledge(bgCtx, msg.KnowledgeID)
	}

	logger.Info(ctx, "Message deleted successfully")
	return nil
}

// ClearSessionMessages deletes all messages in a session, along with their chat history KB entries.
func (s *messageService) ClearSessionMessages(ctx context.Context, sessionID string) error {
	logger.Infof(ctx, "Start clearing all messages for session: %s", sessionID)

	tenantID := types.MustTenantIDFromContext(ctx)
	if _, err := s.sessionRepo.Get(ctx, tenantID, sessionUserIDForLookup(ctx), sessionID); err != nil {
		logger.Errorf(ctx, "Failed to get session: %v", err)
		return err
	}

	// Async cleanup: delete associated Knowledge entries from the chat history KB
	bgCtx := context.WithoutCancel(ctx)
	go s.DeleteSessionKnowledge(bgCtx, sessionID)

	if err := s.messageRepo.DeleteMessagesBySessionID(ctx, sessionID); err != nil {
		logger.Errorf(ctx, "Failed to delete messages for session %s: %v", sessionID, err)
		return err
	}
	if s.suggestionRepo != nil {
		if err := s.suggestionRepo.DeleteBySessionID(ctx, tenantID, sessionID); err != nil {
			logger.Warnf(ctx, "Failed to delete suggestions for session %s: %v", sessionID, err)
		}
	}
	// a cleared session starts over: its per-session caps count from zero
	// (the workspace and person counters keep what was spent), and a
	// continued session no longer replays the summary it was seeded with
	if s.usageMeter != nil {
		s.usageMeter.ResetSession(ctx, sessionID)
	}
	if err := s.sessionRepo.ClearContextSeed(ctx, tenantID, sessionID); err != nil {
		logger.Warnf(ctx, "Failed to clear the context seed of session %s: %v", sessionID, err)
	}

	logger.Infof(ctx, "All messages cleared for session: %s", sessionID)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Chat History Knowledge Base — Configuration-driven (via Tenant.ChatHistoryConfig)
// ─────────────────────────────────────────────────────────────────────────────

// getChatHistoryConfig returns the chat history binding to search, or nil
// when the feature is off or this workspace has nothing indexed yet. It never
// creates a knowledge base; see chatHistoryBinding.
func (s *messageService) getChatHistoryConfig(ctx context.Context) *types.ChatHistoryConfig {
	return s.chatHistoryBinding(ctx, false)
}

// chatHistoryLocks serializes the lazy creation of a workspace's chat
// history knowledge base, so a burst of first messages creates one, not many.
var chatHistoryLocks sync.Map // tenant id -> *sync.Mutex

// chatHistoryBinding resolves the chat history knowledge base for the
// workspace in ctx.
//
// The SuperAdmin turns chat history on for every workspace through the
// configuration profile, which holds only {enabled, embedding_model_id}. Each
// workspace indexes into its own hidden knowledge base, recorded on its own
// row; with create set, the first message after the feature is switched on
// (or after the embedding model changes) creates it. A workspace whose
// profile has no chat history config keeps its own legacy settings.
func (s *messageService) chatHistoryBinding(ctx context.Context, create bool) *types.ChatHistoryConfig {
	tenant, ok := types.TenantInfoFromContext(ctx)
	if !ok || tenant == nil {
		return nil
	}
	if s.tenantService == nil || tenant.ConfigProfileID == nil {
		return configuredChatHistory(tenant.ChatHistoryConfig)
	}
	profile, err := s.tenantService.GetTenantByID(ctx, *tenant.ConfigProfileID)
	if err != nil || profile == nil {
		logger.Warnf(ctx, "chat history: cannot load configuration profile %d: %v", *tenant.ConfigProfileID, err)
		return nil
	}
	platform := profile.ChatHistoryConfig
	if platform == nil {
		return configuredChatHistory(tenant.ChatHistoryConfig)
	}
	if !platform.Enabled || platform.EmbeddingModelID == "" {
		return nil
	}
	if bound := matchingChatHistory(tenant.ChatHistoryConfig, platform); bound != nil {
		return bound
	}
	if !create {
		return nil
	}

	lock, _ := chatHistoryLocks.LoadOrStore(tenant.ID, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	// Another message may have created it while this one waited.
	current, err := s.tenantService.GetTenantByID(ctx, tenant.ID)
	if err != nil || current == nil {
		return nil
	}
	if bound := matchingChatHistory(current.ChatHistoryConfig, platform); bound != nil {
		return bound
	}
	kb, err := s.kbService.CreateKnowledgeBase(ctx, &types.KnowledgeBase{
		Name:             "__chat_history__",
		Type:             types.KnowledgeBaseTypeDocument,
		IsTemporary:      true,
		Description:      "Auto-managed knowledge base for chat history message indexing",
		EmbeddingModelID: platform.EmbeddingModelID,
	})
	if err != nil {
		logger.Warnf(ctx, "chat history: cannot create knowledge base for organization %d: %v", tenant.ID, err)
		return nil
	}
	current.ChatHistoryConfig = &types.ChatHistoryConfig{
		Enabled:          true,
		EmbeddingModelID: platform.EmbeddingModelID,
		KnowledgeBaseID:  kb.ID,
	}
	if _, err := s.tenantService.UpdateTenant(ctx, current); err != nil {
		logger.Warnf(ctx, "chat history: cannot record knowledge base %s for organization %d: %v", kb.ID, tenant.ID, err)
		return nil
	}
	logger.Infof(ctx, "chat history: created knowledge base %s for organization %d", kb.ID, tenant.ID)
	return current.ChatHistoryConfig
}

// configuredChatHistory returns cfg when it can be indexed into and searched.
func configuredChatHistory(cfg *types.ChatHistoryConfig) *types.ChatHistoryConfig {
	if cfg == nil || !cfg.IsConfigured() {
		return nil
	}
	return cfg
}

// matchingChatHistory returns the workspace's binding when it points at a
// knowledge base built with the platform's embedding model. A binding built
// with another model is incompatible and is replaced, as the settings page
// did when the model changed.
func matchingChatHistory(own, platform *types.ChatHistoryConfig) *types.ChatHistoryConfig {
	if own == nil || own.KnowledgeBaseID == "" || own.EmbeddingModelID != platform.EmbeddingModelID {
		return nil
	}
	return &types.ChatHistoryConfig{
		Enabled:          true,
		EmbeddingModelID: own.EmbeddingModelID,
		KnowledgeBaseID:  own.KnowledgeBaseID,
	}
}

// getRetrievalConfig reads the global retrieval configuration from the tenant's settings.
// Returns an empty config (with defaults) if not configured.
func (s *messageService) getRetrievalConfig(ctx context.Context) *types.RetrievalConfig {
	tenant, ok := types.TenantInfoFromContext(ctx)
	if !ok {
		return &types.RetrievalConfig{}
	}
	if tenant.RetrievalConfig == nil {
		return &types.RetrievalConfig{}
	}
	return tenant.RetrievalConfig
}

// IndexMessageToKB indexes a message (Q&A pair) into the chat history knowledge base asynchronously.
// It creates a Knowledge entry (passage) containing both the user query and assistant answer,
// then links the message to the Knowledge entry via the knowledge_id field.
// The KB ID is read from the tenant's ChatHistoryConfig — if not configured, indexing is skipped.
func (s *messageService) IndexMessageToKB(ctx context.Context, userQuery string, assistantAnswer string, messageID string, sessionID string) {
	// Strip thinking content (<think>...</think>) before indexing to avoid
	// polluting the knowledge base with intermediate reasoning that would
	// degrade retrieval quality.
	assistantAnswer = regThinkIndex.ReplaceAllString(assistantAnswer, "")
	assistantAnswer = strings.TrimSpace(assistantAnswer)

	if strings.TrimSpace(userQuery) == "" && assistantAnswer == "" {
		return
	}

	cfg := s.chatHistoryBinding(ctx, true)
	if cfg == nil {
		// Say why, otherwise an operator who enabled indexing in the UI sees
		// "indexed messages: 0" with nothing in the logs to explain it: the
		// stats endpoint only checks Enabled, while indexing additionally
		// requires an embedding model and an auto-created KB.
		logger.Infof(ctx, "Skipping message index for message %s: %s",
			messageID, describeChatHistorySkip(ctx))
		return
	}

	logger.Infof(ctx, "Indexing message to chat history KB %s, message ID: %s, session ID: %s", cfg.KnowledgeBaseID, messageID, sessionID)

	// Build passage content: combine Q&A for better semantic search
	var passages []string
	passage := fmt.Sprintf("[Session: %s]\nQ: %s\nA: %s", sessionID, userQuery, assistantAnswer)
	passages = append(passages, passage)

	// Use async (non-sync) passage creation so it doesn't block the response
	knowledge, err := s.knowService.CreateKnowledgeFromPassage(ctx, cfg.KnowledgeBaseID, passages, "")
	if err != nil {
		logger.Warnf(ctx, "Failed to index message to chat history KB: %v", err)
		return
	}

	// Link the message to the knowledge entry
	if err := s.messageRepo.UpdateMessageKnowledgeID(ctx, messageID, knowledge.ID); err != nil {
		logger.Warnf(ctx, "Failed to update message knowledge_id: %v", err)
		return
	}

	logger.Infof(ctx, "Message indexed to chat history KB: knowledge_id=%s, message_id=%s", knowledge.ID, messageID)
}

// describeChatHistorySkip reports which chat-history precondition is missing,
// for the log line emitted when indexing is skipped. ChatHistoryConfig.
// IsConfigured requires Enabled plus a selected embedding model plus an
// auto-created KB, so "enabled" alone is not enough to index.
func describeChatHistorySkip(ctx context.Context) string {
	tenant, ok := types.TenantInfoFromContext(ctx)
	if !ok {
		return "no tenant in context"
	}
	cfg := tenant.ChatHistoryConfig
	switch {
	case cfg == nil:
		return "chat history config not set"
	case !cfg.Enabled:
		return "chat history indexing disabled"
	case cfg.EmbeddingModelID == "":
		return "enabled but no embedding model selected"
	case cfg.KnowledgeBaseID == "":
		return "enabled but chat history knowledge base not created yet"
	default:
		return "chat history config incomplete"
	}
}

// DeleteMessageKnowledge deletes the Knowledge entry associated with a message from the chat history KB.
func (s *messageService) DeleteMessageKnowledge(ctx context.Context, knowledgeID string) {
	if knowledgeID == "" {
		return
	}
	logger.Infof(ctx, "Deleting chat history knowledge entry: %s", knowledgeID)
	if err := deleteReferencedKnowledge(ctx, s.knowService, "", []string{knowledgeID}); err != nil {
		logger.Warnf(ctx, "Failed to delete chat history knowledge %s: %v", knowledgeID, err)
	}
}

// DeleteSessionKnowledge deletes all Knowledge entries for messages in a session from the chat history KB.
func (s *messageService) DeleteSessionKnowledge(ctx context.Context, sessionID string) {
	logger.Infof(ctx, "Deleting all chat history knowledge entries for session: %s", sessionID)

	knowledgeIDs, err := s.messageRepo.GetKnowledgeIDsBySessionID(ctx, sessionID)
	if err != nil {
		logger.Warnf(ctx, "Failed to get knowledge IDs for session %s: %v", sessionID, err)
		return
	}

	if len(knowledgeIDs) == 0 {
		return
	}

	logger.Infof(ctx, "Deleting %d chat history knowledge entries for session %s", len(knowledgeIDs), sessionID)
	if err := deleteReferencedKnowledge(ctx, s.knowService, "", knowledgeIDs); err != nil {
		logger.Warnf(ctx, "Failed to batch delete chat history knowledge for session %s: %v", sessionID, err)
	}
}

// GetChatHistoryKBStats returns statistics about the chat history knowledge base.
func (s *messageService) GetChatHistoryKBStats(ctx context.Context) (*types.ChatHistoryKBStats, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	tenant, err := s.tenantService.GetTenantByID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	stats := &types.ChatHistoryKBStats{}
	cfg := tenant.ChatHistoryConfig
	// A workspace on a profile follows the profile's switch and model; its
	// own row only records which knowledge base it indexes into.
	if tenant.ConfigProfileID != nil {
		if profile, err := s.tenantService.GetTenantByID(ctx, *tenant.ConfigProfileID); err == nil &&
			profile != nil && profile.ChatHistoryConfig != nil {
			platform := profile.ChatHistoryConfig
			if !platform.Enabled {
				return stats, nil
			}
			cfg = &types.ChatHistoryConfig{Enabled: true, EmbeddingModelID: platform.EmbeddingModelID}
			if bound := matchingChatHistory(tenant.ChatHistoryConfig, platform); bound != nil {
				cfg.KnowledgeBaseID = bound.KnowledgeBaseID
			}
		}
	}
	if cfg == nil || !cfg.Enabled {
		return stats, nil
	}

	stats.Enabled = true
	stats.EmbeddingModelID = cfg.EmbeddingModelID
	stats.KnowledgeBaseID = cfg.KnowledgeBaseID

	if cfg.KnowledgeBaseID == "" {
		return stats, nil
	}

	// Fetch KB info and fill counts (KnowledgeCount is gorm:"-", needs FillKnowledgeBaseCounts)
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, cfg.KnowledgeBaseID)
	if err != nil {
		logger.Warnf(ctx, "Failed to get chat history KB %s: %v", cfg.KnowledgeBaseID, err)
		return stats, nil
	}
	if err := s.kbService.FillKnowledgeBaseCounts(ctx, kb); err != nil {
		logger.Warnf(ctx, "Failed to fill chat history KB counts %s: %v", cfg.KnowledgeBaseID, err)
	}
	stats.KnowledgeBaseName = kb.Name
	stats.IndexedMessageCount = kb.KnowledgeCount
	stats.HasIndexedMessages = kb.KnowledgeCount > 0

	return stats, nil
}

// GetSessionArtifacts returns every skill-produced artifact recorded against
// any assistant message of the session. Thin pass-through to the repository:
// the collector and session cleanup both need it, and centralising it here
// keeps tests able to inject a stub MessageService.
func (s *messageService) GetSessionArtifacts(
	ctx context.Context, sessionID string,
) (types.MessageArtifacts, error) {
	if sessionID == "" {
		return types.MessageArtifacts{}, nil
	}
	return s.messageRepo.GetSessionArtifacts(ctx, sessionID)
}

// artifactLibraryMaxPageSize caps one library page; the page renders a card
// per row, so larger pages only cost bandwidth.
const artifactLibraryMaxPageSize = 100

// ListArtifactLibrary lists the latest version of every artifact in the
// sessions the caller can see. Scope follows ListSessions for its default
// (unfiltered) view: the caller's own sessions plus legacy tenant-level ones.
func (s *messageService) ListArtifactLibrary(
	ctx context.Context, query *types.ArtifactLibraryQuery,
) (*types.PageResult, error) {
	if query == nil {
		query = &types.ArtifactLibraryQuery{}
	}
	query.TenantID = types.MustTenantIDFromContext(ctx)
	query.UserID = types.SessionOwnerIDFromContext(ctx)
	pagination := &types.Pagination{Page: query.Page, PageSize: query.PageSize}
	query.Page = pagination.GetPage()
	query.PageSize = min(pagination.GetPageSize(), artifactLibraryMaxPageSize)
	pagination.PageSize = query.PageSize
	query.FileTypes = normalizeArtifactFileTypes(query.FileTypes)

	items, total, err := s.messageRepo.ListArtifactLibrary(ctx, query)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": query.TenantID,
			"user_id":   query.UserID,
		})
		return nil, err
	}
	for _, item := range items {
		if handle, ok := types.ParseResourcePath(item.URL); ok {
			item.Handle = types.BuildResourcePath(handle)
		}
	}
	return types.NewPageResult(total, pagination, items), nil
}

// normalizeArtifactFileTypes lower-cases extensions and adds the leading dot
// the collector stores ("pdf" and ".PDF" both become ".pdf").
func normalizeArtifactFileTypes(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		ext := strings.ToLower(strings.TrimSpace(raw))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if !seen[ext] {
			seen[ext] = true
			out = append(out, ext)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Message Search (Hybrid: Keyword + KB Vector Search)
// ─────────────────────────────────────────────────────────────────────────────

// SearchMessages searches messages by keyword and/or vector similarity across all sessions of the current tenant.
// Vector search is delegated to the chat history knowledge base's HybridSearch (configured via ChatHistoryConfig).
func (s *messageService) SearchMessages(ctx context.Context, params *types.MessageSearchParams) (*types.MessageSearchResult, error) {
	logger.Infof(ctx, "Start searching messages, query: %s, mode: %s", params.Query, params.Mode)

	tenantID := types.MustTenantIDFromContext(ctx)

	// Conversation search is scoped to the person asking, exactly as the
	// session list is. Sessions are per-user state, and a workspace-wide
	// keyword search over them let any viewer read a colleague's private
	// conversations, which is not something a search box should be able to do.
	if params.OwnerID == "" {
		params.OwnerID = types.SessionOwnerIDFromContext(ctx)
	}

	// Set defaults
	if params.Mode == "" {
		params.Mode = types.MessageSearchModeHybrid
	}
	if params.Limit <= 0 {
		params.Limit = 20
	}

	var keywordResults []*types.MessageWithSession
	var vectorResults []*types.MessageSearchResultItem
	var err error

	// Step 1: Keyword search (dialect-aware LIKE in the repository)
	if params.Mode == types.MessageSearchModeKeyword || params.Mode == types.MessageSearchModeHybrid {
		keywordResults, err = s.messageRepo.SearchMessagesByKeyword(
			ctx, tenantID, params.OwnerID, params.Query, params.SessionIDs, params.Limit*3)
		if err != nil {
			logger.Errorf(ctx, "Keyword search failed: %v", err)
			return nil, err
		}
		logger.Infof(ctx, "Keyword search found %d results", len(keywordResults))
	}

	// Step 2: Vector search via chat history knowledge base (if configured)
	if params.Mode == types.MessageSearchModeVector || params.Mode == types.MessageSearchModeHybrid {
		vectorResults, err = s.vectorSearchViaKB(ctx, params)
		if err != nil {
			logger.Warnf(ctx, "Vector search via KB failed, falling back to keyword-only: %v", err)
			if params.Mode == types.MessageSearchModeVector {
				return nil, err
			}
		} else {
			logger.Infof(ctx, "Vector search found %d results", len(vectorResults))
		}
	}

	// Step 3: Merge results based on mode
	var items []*types.MessageSearchResultItem

	switch params.Mode {
	case types.MessageSearchModeKeyword:
		items = convertKeywordResults(keywordResults)
	case types.MessageSearchModeVector:
		items = vectorResults
	case types.MessageSearchModeHybrid:
		items = rrfMerge(keywordResults, vectorResults)
	}

	// The vector path resolves hits through a shared knowledge base that does
	// not know who wrote a message, so ownership is re-checked on the results.
	items, err = s.restrictToOwnedSessions(ctx, tenantID, params.OwnerID, items)
	if err != nil {
		return nil, err
	}

	// Step 4: Fetch partner messages (Q&A counterparts) to ensure complete pairs
	items = s.fetchPartnerMessages(ctx, items)

	// Step 5: Group by request_id to merge Q&A pairs
	grouped := groupByRequestID(items)

	// Apply limit
	if len(grouped) > params.Limit {
		grouped = grouped[:params.Limit]
	}

	result := &types.MessageSearchResult{
		Items: grouped,
		Total: len(grouped),
	}

	logger.Infof(ctx, "Message search completed, returning %d grouped results", result.Total)
	return result, nil
}

// restrictToOwnedSessions drops results from sessions the caller does not own.
func (s *messageService) restrictToOwnedSessions(
	ctx context.Context, tenantID uint64, ownerID string, items []*types.MessageSearchResultItem,
) ([]*types.MessageSearchResultItem, error) {
	if ownerID == "" || len(items) == 0 {
		return items, nil
	}
	sessionIDs := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == nil || item.SessionID == "" {
			continue
		}
		if _, dup := seen[item.SessionID]; dup {
			continue
		}
		seen[item.SessionID] = struct{}{}
		sessionIDs = append(sessionIDs, item.SessionID)
	}
	owned, err := s.messageRepo.OwnedSessionIDs(ctx, tenantID, ownerID, sessionIDs)
	if err != nil {
		return nil, err
	}
	filtered := make([]*types.MessageSearchResultItem, 0, len(items))
	for _, item := range items {
		if item != nil && owned[item.SessionID] {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

// vectorSearchViaKB performs vector search using the chat history knowledge base's HybridSearch.
// The KB ID is read from ChatHistoryConfig, search params from RetrievalConfig.
func (s *messageService) vectorSearchViaKB(ctx context.Context, params *types.MessageSearchParams) ([]*types.MessageSearchResultItem, error) {
	cfg := s.getChatHistoryConfig(ctx)
	if cfg == nil {
		return nil, nil // Chat history KB not configured, skip vector search
	}

	// Read global retrieval config for search parameters
	rc := s.getRetrievalConfig(ctx)

	// Use KB HybridSearch with vector-only mode (keyword search is done separately on the messages table)
	searchParams := types.SearchParams{
		QueryText:            params.Query,
		MatchCount:           rc.GetEffectiveEmbeddingTopK(),
		VectorThreshold:      rc.GetEffectiveVectorThreshold(),
		DisableKeywordsMatch: true, // We handle keyword search separately on the messages table
	}

	kbResults, err := s.kbService.HybridSearch(ctx, cfg.KnowledgeBaseID, searchParams)
	if err != nil {
		return nil, fmt.Errorf("KB hybrid search failed: %w", err)
	}

	if len(kbResults) == 0 {
		return nil, nil
	}

	// Rerank results if a rerank model is configured
	kbResults = s.rerankResults(ctx, rc, params.Query, kbResults)
	if len(kbResults) == 0 {
		return nil, nil
	}

	// Map KB search results back to messages via knowledge_id
	knowledgeIDs := make([]string, 0, len(kbResults))
	scoreByKnowledgeID := make(map[string]float64)
	for _, r := range kbResults {
		knowledgeIDs = append(knowledgeIDs, r.KnowledgeID)
		scoreByKnowledgeID[r.KnowledgeID] = r.Score
	}

	// Look up messages by their knowledge_id
	messages, err := s.messageRepo.GetMessagesByKnowledgeIDs(ctx, knowledgeIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages by knowledge IDs: %w", err)
	}

	// Filter by session IDs if specified
	sessionFilter := make(map[string]bool)
	for _, sid := range params.SessionIDs {
		sessionFilter[sid] = true
	}

	var results []*types.MessageSearchResultItem
	for _, msg := range messages {
		if len(sessionFilter) > 0 && !sessionFilter[msg.SessionID] {
			continue
		}
		score := scoreByKnowledgeID[msg.KnowledgeID]
		results = append(results, &types.MessageSearchResultItem{
			MessageWithSession: *msg,
			Score:              score,
			MatchType:          "vector",
		})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// rerankResults applies rerank model to search results if configured.
// Returns reranked + filtered results, or original results if rerank is unavailable.
func (s *messageService) rerankResults(ctx context.Context, rc *types.RetrievalConfig, query string, results []*types.SearchResult) []*types.SearchResult {
	if rc == nil || rc.RerankModelID == "" || len(results) == 0 {
		return results
	}

	reranker, err := s.modelService.GetRerankModel(ctx, rc.RerankModelID)
	if err != nil {
		logger.Warnf(ctx, "Failed to get rerank model %s, skipping rerank: %v", rc.RerankModelID, err)
		return results
	}

	// Build documents for rerank
	documents := make([]string, len(results))
	for i, r := range results {
		documents[i] = r.Content
	}

	rankResults, err := reranker.Rerank(ctx, query, documents)
	if err != nil {
		logger.Warnf(ctx, "Rerank call failed, skipping: %v", err)
		return results
	}

	// Filter by threshold and topK, rebuild results with rerank scores
	threshold := rc.GetEffectiveRerankThreshold()
	topK := rc.GetEffectiveRerankTopK()

	var reranked []*types.SearchResult
	for _, rr := range rankResults {
		if rr.Index >= len(results) {
			continue
		}
		if rr.RelevanceScore < threshold {
			continue
		}
		item := *results[rr.Index]
		item.Score = rr.RelevanceScore
		reranked = append(reranked, &item)
		if len(reranked) >= topK {
			break
		}
	}

	logger.Infof(ctx, "Rerank: %d -> %d results (threshold=%.2f, topK=%d)", len(results), len(reranked), threshold, topK)
	return reranked
}

// convertKeywordResults converts keyword search results to MessageSearchResultItem
func convertKeywordResults(results []*types.MessageWithSession) []*types.MessageSearchResultItem {
	items := make([]*types.MessageSearchResultItem, 0, len(results))
	for i, msg := range results {
		items = append(items, &types.MessageSearchResultItem{
			MessageWithSession: *msg,
			Score:              float64(len(results)-i) / float64(len(results)),
			MatchType:          "keyword",
		})
	}
	return items
}

// rrfMerge merges keyword and vector search results using Reciprocal Rank Fusion (RRF)
func rrfMerge(keywordResults []*types.MessageWithSession, vectorResults []*types.MessageSearchResultItem) []*types.MessageSearchResultItem {
	const k = 60.0

	type scoredMsg struct {
		msg       *types.MessageWithSession
		rrfScore  float64
		matchType string
	}
	scoreMap := make(map[string]*scoredMsg)

	for rank, msg := range keywordResults {
		id := msg.ID
		rrfScore := 1.0 / (k + float64(rank+1))
		if existing, ok := scoreMap[id]; ok {
			existing.rrfScore += rrfScore
			existing.matchType = "hybrid"
		} else {
			scoreMap[id] = &scoredMsg{
				msg:       msg,
				rrfScore:  rrfScore,
				matchType: "keyword",
			}
		}
	}

	for rank, item := range vectorResults {
		id := item.ID
		rrfScore := 1.0 / (k + float64(rank+1))
		if existing, ok := scoreMap[id]; ok {
			existing.rrfScore += rrfScore
			existing.matchType = "hybrid"
		} else {
			scoreMap[id] = &scoredMsg{
				msg:       &item.MessageWithSession,
				rrfScore:  rrfScore,
				matchType: "vector",
			}
		}
	}

	items := make([]*types.MessageSearchResultItem, 0, len(scoreMap))
	for _, scored := range scoreMap {
		items = append(items, &types.MessageSearchResultItem{
			MessageWithSession: *scored.msg,
			Score:              scored.rrfScore,
			MatchType:          scored.matchType,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	return items
}

// fetchPartnerMessages looks at the search results and, for each request_id that
// has only one role (Q-only or A-only), fetches the partner message from DB so
// that groupByRequestID can produce complete Q&A pairs.
func (s *messageService) fetchPartnerMessages(ctx context.Context, items []*types.MessageSearchResultItem) []*types.MessageSearchResultItem {
	// Collect request_ids and track which roles we already have
	type roleSet struct {
		hasUser      bool
		hasAssistant bool
	}
	seen := make(map[string]*roleSet)
	existingIDs := make(map[string]bool)
	for _, item := range items {
		existingIDs[item.ID] = true
		if item.RequestID == "" || item.SessionID == "" {
			continue
		}
		key := searchPairKey(item.SessionID, item.RequestID)
		rs, ok := seen[key]
		if !ok {
			rs = &roleSet{}
			seen[key] = rs
		}
		if item.Role == "user" {
			rs.hasUser = true
		} else if item.Role == "assistant" {
			rs.hasAssistant = true
		}
	}

	needBySession := make(map[string][]string)
	for key, rs := range seen {
		if rs.hasUser && rs.hasAssistant {
			continue
		}
		sessionID, rid, ok := strings.Cut(key, "\x00")
		if !ok || sessionID == "" || rid == "" {
			continue
		}
		needBySession[sessionID] = append(needBySession[sessionID], rid)
	}
	if len(needBySession) == 0 {
		return items
	}

	for sessionID, rids := range needBySession {
		partners, err := s.messageRepo.GetMessagesByRequestIDs(ctx, sessionID, rids)
		if err != nil {
			logger.Warnf(ctx, "Failed to fetch partner messages: %v", err)
			return items
		}
		for _, p := range partners {
			if existingIDs[p.ID] {
				continue
			}
			existingIDs[p.ID] = true
			items = append(items, &types.MessageSearchResultItem{
				MessageWithSession: *p,
				Score:              0, // partner is not directly matched
				MatchType:          "",
			})
		}
	}

	return items
}

// groupByRequestID merges individual message search results into Q&A pairs
// grouped by request_id. Messages without a request_id become standalone items.
func groupByRequestID(items []*types.MessageSearchResultItem) []*types.MessageSearchGroupItem {
	type groupState struct {
		item  *types.MessageSearchGroupItem
		order int // preserve the order of first appearance
	}
	groups := make(map[string]*groupState)
	nextOrder := 0

	for _, item := range items {
		key := item.ID
		if item.RequestID != "" {
			key = searchPairKey(item.SessionID, item.RequestID)
		}

		g, exists := groups[key]
		if !exists {
			g = &groupState{
				item: &types.MessageSearchGroupItem{
					RequestID:    item.RequestID,
					SessionID:    item.SessionID,
					SessionTitle: item.SessionTitle,
					CreatedAt:    item.CreatedAt,
				},
				order: nextOrder,
			}
			nextOrder++
			groups[key] = g
		}

		// Assign content based on role
		switch item.Role {
		case "user":
			g.item.QueryContent = item.Content
		case "assistant":
			g.item.AnswerContent = item.Content
		}

		// Keep the best score and merge match types
		if item.Score > g.item.Score {
			g.item.Score = item.Score
		}
		if g.item.MatchType == "" {
			g.item.MatchType = item.MatchType
		} else if g.item.MatchType != item.MatchType {
			g.item.MatchType = "hybrid"
		}

		// Use earliest created_at
		if item.CreatedAt.Before(g.item.CreatedAt) {
			g.item.CreatedAt = item.CreatedAt
		}
	}

	// Collect and sort by original order (which reflects score ranking)
	result := make([]*types.MessageSearchGroupItem, 0, len(groups))
	ordered := make([]*groupState, 0, len(groups))
	for _, g := range groups {
		ordered = append(ordered, g)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].order < ordered[j].order
	})
	for _, g := range ordered {
		result = append(result, g.item)
	}

	return result
}

func searchPairKey(sessionID, requestID string) string {
	return sessionID + "\x00" + requestID
}

// hideUnreadableReferences drops persisted citations (knowledge references
// with their chunk snippets) whose KB the caller can no longer read under
// access.DecideKB: a KB switched to restricted, or a revoked invite/grant,
// stops showing its snippets in past answers on the next load. Only
// KB-bound references are checked: one with a KB ID, or with a knowledge ID
// resolved through its document. A reference with neither (chat
// attachments), or a web search result (whose KnowledgeID is its URL), is
// not KB-bound and is always kept. A
// knowledge ID whose document is gone (deleted document or KB) or cannot be
// resolved is hidden (fail closed). The answer text itself is unchanged.
// isWebSearchReference reports a web search result. Web results carry their
// URL as KnowledgeID (searchutil, agent webRowsAsReferences), so they must not be resolved as documents.
func isWebSearchReference(ref *types.SearchResult) bool {
	return ref.MatchType == types.MatchTypeWebSearch ||
		ref.ChunkType == string(types.ChunkTypeWebSearch) ||
		ref.KnowledgeSource == "web_search"
}

func (s *messageService) hideUnreadableReferences(ctx context.Context, messages []*types.Message) {
	if s.kbAccessGrantService == nil {
		return
	}
	permissions := access.NewKBPermissions(ctx, s.kbAccessGrantService)
	knowledgeKB := map[string]string{}
	// kbOf returns the KB a reference is bound to and whether it is
	// KB-bound at all. kbID == "" with bound == true means the reference
	// points at a document that no longer resolves to a KB.
	kbOf := func(ref *types.SearchResult) (kbID string, bound bool) {
		if isWebSearchReference(ref) {
			return "", false
		}
		if ref.KnowledgeBaseID != "" {
			return ref.KnowledgeBaseID, true
		}
		if ref.KnowledgeID == "" {
			return "", false
		}
		if s.knowService == nil {
			return "", true
		}
		if id, ok := knowledgeKB[ref.KnowledgeID]; ok {
			return id, true
		}
		id := ""
		if k, err := s.knowService.GetKnowledgeByIDOnly(ctx, ref.KnowledgeID); err == nil && k != nil {
			id = k.KnowledgeBaseID
		}
		knowledgeKB[ref.KnowledgeID] = id
		return id, true
	}
	for _, message := range messages {
		if message == nil || len(message.KnowledgeReferences) == 0 {
			continue
		}
		kept := message.KnowledgeReferences[:0:0]
		for _, ref := range message.KnowledgeReferences {
			if ref == nil {
				continue
			}
			kbID, bound := kbOf(ref)
			if !bound {
				kept = append(kept, ref)
				continue
			}
			if kbID == "" {
				continue
			}
			if ok, err := permissions.Check(kbID, 0, types.KBPermissionViewer); err == nil && ok {
				kept = append(kept, ref)
			}
		}
		message.KnowledgeReferences = kept
	}
}
