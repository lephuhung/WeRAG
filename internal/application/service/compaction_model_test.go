package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
)

type compactionSetting struct {
	interfaces.SystemSettingService
	value string
}

func (s compactionSetting) GetString(_ context.Context, key, _, def string) string {
	if key == types.CompactionModelSettingKey {
		return s.value
	}
	return def
}

type compactionModels struct {
	interfaces.ModelService
	chatErr error
	window  int
}

type namedChat struct{ name string }

func (n namedChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return nil, errors.New("not used")
}
func (n namedChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("not used")
}
func (n namedChat) GetModelName() string { return n.name }
func (n namedChat) GetModelID() string   { return n.name }

func (m compactionModels) GetChatModel(_ context.Context, id string) (chat.Chat, error) {
	if m.chatErr != nil {
		return nil, m.chatErr
	}
	return namedChat{name: id}, nil
}

func (m compactionModels) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	return &types.Model{ID: id, Parameters: types.ModelParameters{ContextWindow: m.window}}, nil
}

type compactionEngine struct {
	calls  int
	model  chat.Chat
	id     string
	window int
}

func (e *compactionEngine) SetCompactionModel(model chat.Chat, id string, window int) {
	e.calls++
	e.model, e.id, e.window = model, id, window
}

// The setting names the summarizer; an empty setting, the answering model
// itself, or a model that cannot be loaded keeps the answering model.
func TestApplyCompactionModel(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		setting string
		models  compactionModels
		want    int
	}{
		{"unset keeps the answering model", "", compactionModels{window: 32000}, 0},
		{"same as answering model", "qwen", compactionModels{window: 32000}, 0},
		{"unloadable falls back to the answering model", "gemma", compactionModels{chatErr: errors.New("down")}, 0},
		{"separate summarizer", "gemma", compactionModels{window: 32000}, 1},
	}
	for _, c := range cases {
		engine := &compactionEngine{}
		svc := &sessionService{systemSettings: compactionSetting{value: c.setting}, modelService: c.models}
		svc.applyCompactionModel(ctx, engine, "qwen")
		assert.Equal(t, c.want, engine.calls, c.name)
		if c.want == 1 {
			assert.Equal(t, "gemma", engine.id)
			assert.Equal(t, 32000, engine.window)
			assert.Equal(t, "gemma", engine.model.(namedChat).name)
		}
	}
}
