package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type auxSettings struct {
	interfaces.SystemSettingService
	value string
}

func (s auxSettings) GetString(_ context.Context, key, _ string, def string) string {
	if key == types.AuxiliaryModelSettingKey {
		return s.value
	}
	return def
}

// replyChat answers with a fixed reply or fails.
type replyChat struct {
	reply string
	err   error
	calls *[]string
	id    string
}

func (c replyChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("not used")
}
func (c replyChat) GetModelName() string { return c.id }
func (c replyChat) GetModelID() string   { return c.id }

func (c replyChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	*c.calls = append(*c.calls, c.id)
	if c.err != nil {
		return nil, c.err
	}
	return &types.ChatResponse{Content: c.reply}, nil
}

type auxModelService struct {
	interfaces.ModelService
	chats map[string]chat.Chat
}

func (s auxModelService) GetChatModel(_ context.Context, id string) (chat.Chat, error) {
	if c, ok := s.chats[id]; ok {
		return c, nil
	}
	return nil, errors.New("no model " + id)
}

type titleRepo struct {
	interfaces.SessionRepository
	saved string
}

func (r *titleRepo) Update(_ context.Context, s *types.Session, _ string) (int64, error) {
	r.saved = s.Title
	return 1, nil
}

func titleService(aux string, chats map[string]chat.Chat) (*sessionService, *titleRepo) {
	repo := &titleRepo{}
	cfg := &config.Config{Conversation: &config.ConversationConfig{GenerateSessionTitlePrompt: "title in {{language}}"}}
	s := &sessionService{cfg: cfg, sessionRepo: repo, modelService: auxModelService{chats: chats}}
	s.SetAuxiliaryModels(auxSettings{value: aux})
	return s, repo
}

func TestTitleUsesAuxiliaryModel(t *testing.T) {
	var calls []string
	s, repo := titleService("gemma", map[string]chat.Chat{
		"gemma": replyChat{reply: "Thời hạn bảo quản tài liệu mật", calls: &calls, id: "gemma"},
		"qwen":  replyChat{reply: "other", calls: &calls, id: "qwen"},
	})
	title, err := s.GenerateTitle(context.Background(), &types.Session{ID: "s"},
		[]types.Message{{Role: "user", Content: "thời hạn bảo quản tài liệu BMNN"}}, "qwen")
	require.NoError(t, err)
	require.Equal(t, "Thời hạn bảo quản tài liệu mật", title)
	require.Equal(t, title, repo.saved)
	require.Equal(t, []string{"gemma"}, calls)
}

func TestTitleFallsBackWhenAuxiliaryModelFails(t *testing.T) {
	var calls []string
	s, _ := titleService("gemma", map[string]chat.Chat{
		"gemma": replyChat{err: errors.New("vllm down"), calls: &calls, id: "gemma"},
		"qwen":  replyChat{reply: "Thời hạn bảo quản tài liệu", calls: &calls, id: "qwen"},
	})
	title, err := s.GenerateTitle(context.Background(), &types.Session{ID: "s"},
		[]types.Message{{Role: "user", Content: "thời hạn bảo quản"}}, "qwen")
	require.NoError(t, err)
	require.Equal(t, "Thời hạn bảo quản tài liệu", title)
	require.Equal(t, []string{"gemma", "qwen"}, calls)
}

func TestTitleWithoutAuxiliaryModel(t *testing.T) {
	var calls []string
	s, _ := titleService("", map[string]chat.Chat{"qwen": replyChat{reply: "Tiêu đề", calls: &calls, id: "qwen"}})
	_, err := s.GenerateTitle(context.Background(), &types.Session{ID: "s"},
		[]types.Message{{Role: "user", Content: "câu hỏi"}}, "qwen")
	require.NoError(t, err)
	require.Equal(t, []string{"qwen"}, calls)
}

func TestSuggestionModelsOrder(t *testing.T) {
	s := &messageSuggestionService{}
	msg := &types.Message{ModelID: "qwen"}
	require.Equal(t, []string{"qwen"}, s.suggestionModels(context.Background(), msg, types.FollowUpSuggestionConfig{}))
	s.SetAuxiliaryModels(auxSettings{value: "gemma"})
	require.Equal(t, []string{"gemma", "qwen"}, s.suggestionModels(context.Background(), msg, types.FollowUpSuggestionConfig{}))
	// the agent's own follow-up model wins
	require.Equal(t, []string{"custom"}, s.suggestionModels(context.Background(), msg, types.FollowUpSuggestionConfig{ModelID: "custom"}))
}
