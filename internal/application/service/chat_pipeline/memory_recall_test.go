package chatpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

// stubMemoryService returns a fixed recall so the test can assert what the
// pipeline does with it, rather than re-testing the memory service.
type stubMemoryService struct {
	interfaces.MemoryService

	recall     interfaces.MemoryRecall
	lastQuery  string
	recallCall int
}

func (s *stubMemoryService) Recall(_ context.Context, query string) interfaces.MemoryRecall {
	s.recallCall++
	s.lastQuery = query
	return s.recall
}

func (s *stubMemoryService) ScheduleExtraction(context.Context, string, string, string) {}

func (s *stubMemoryService) TakeNewlySaved(context.Context) []*types.MemoryItem { return nil }

func (s *stubMemoryService) Handle(context.Context, *asynq.Task) error { return nil }

func newMemoryRecallPlugin(memoryService interfaces.MemoryService) *PluginMemoryRecall {
	return NewPluginMemoryRecall(NewEventManager(), memoryService)
}

// TestMemoryReachesTheMessagesSentToTheModel is the assertion that matters:
// recalling memory is pointless if it never lands in the request. It walks the
// recall stage and then the same message assembly the completion plugins use.
func TestMemoryReachesTheMessagesSentToTheModel(t *testing.T) {
	memoryService := &stubMemoryService{
		recall: interfaces.MemoryRecall{
			Prompt: types.WrapMemoryForPrompt("Preferences:\n- Trả lời hãy đưa thẳng kết luận", ""),
			Items: []*types.MemoryItem{
				{ID: "m1", Kind: types.MemoryKindPreference, Content: "Trả lời hãy đưa thẳng kết luận"},
			},
		},
	}
	plugin := newMemoryRecallPlugin(memoryService)

	chatManage := &types.ChatManage{}
	chatManage.Query = "Giúp tôi xem lỗi này"
	chatManage.UserContent = "Giúp tôi xem lỗi này"
	chatManage.SummaryConfig.Prompt = "Bạn là một trợ lý."

	nextCalled := false
	err := plugin.OnEvent(t.Context(), types.MEMORY_RECALL, chatManage, func() *PluginError {
		nextCalled = true
		return nil
	})
	require.Nil(t, err)
	require.True(t, nextCalled, "the recall stage must never stop the pipeline")
	require.Equal(t, "Giúp tôi xem lỗi này", memoryService.lastQuery)

	messages := prepareMessagesWithHistory(chatManage)
	require.NotEmpty(t, messages)
	require.Equal(t, "system", messages[0].Role)
	require.Contains(t, messages[0].Content, "Trả lời hãy đưa thẳng kết luận",
		"the recalled memory must be present in the system message")
	require.Contains(t, messages[0].Content, "<user_memory>")
	require.True(t, strings.HasPrefix(messages[0].Content, "Bạn là một trợ lý."),
		"memory must be appended after the configured prompt, not replace it")
}

func TestMemoryIsAbsentWhenNothingRecalled(t *testing.T) {
	plugin := newMemoryRecallPlugin(&stubMemoryService{})

	chatManage := &types.ChatManage{}
	chatManage.Query = "Hỏi gì đó bất kỳ"
	chatManage.SummaryConfig.Prompt = "Bạn là một trợ lý."

	require.Nil(t, plugin.OnEvent(t.Context(), types.MEMORY_RECALL, chatManage, func() *PluginError { return nil }))
	require.Empty(t, chatManage.MemoryPrompt)
	require.Empty(t, chatManage.UsedMemories)

	messages := prepareMessagesWithHistory(chatManage)
	require.NotContains(t, messages[0].Content, "<user_memory>")
}

func TestMemoryRecallEmitsWhatTheAnswerSaw(t *testing.T) {
	memoryService := &stubMemoryService{
		recall: interfaces.MemoryRecall{
			Prompt: types.WrapMemoryForPrompt("About the user:\n- Đang làm về chẩn đoán hình ảnh y tế", ""),
			Items: []*types.MemoryItem{
				{ID: "m1", Kind: types.MemoryKindProfile, Content: "Đang làm về chẩn đoán hình ảnh y tế"},
			},
		},
	}
	plugin := newMemoryRecallPlugin(memoryService)

	bus := event.NewEventBus()
	var received types.UsedMemories
	bus.On(event.EventMemoryRecalled, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.MemoryRecalledData)
		require.True(t, ok)
		received, ok = data.Memories.(types.UsedMemories)
		require.True(t, ok)
		return nil
	})

	chatManage := &types.ChatManage{}
	chatManage.Query = "Tiếp tục việc lần trước"
	chatManage.EventBus = bus.AsEventBusInterface()

	require.Nil(t, plugin.OnEvent(t.Context(), types.MEMORY_RECALL, chatManage, func() *PluginError { return nil }))

	// The chat UI promises "these are the memories this answer saw", so the
	// streamed list has to be the same one that was injected.
	require.Len(t, received, 1)
	require.Equal(t, "m1", received[0].ID)
	require.Equal(t, "Đang làm về chẩn đoán hình ảnh y tế", received[0].Content)
	require.Equal(t, received, chatManage.UsedMemories)
}

func TestMemoryRecallToleratesNoService(t *testing.T) {
	plugin := newMemoryRecallPlugin(nil)
	chatManage := &types.ChatManage{}
	chatManage.SummaryConfig.Prompt = "Bạn là một trợ lý."
	require.Nil(t, plugin.OnEvent(t.Context(), types.MEMORY_RECALL, chatManage, func() *PluginError { return nil }))
	require.Empty(t, chatManage.MemoryPrompt)
}

func TestMemoryRecallStageIsRegisteredInThePipeline(t *testing.T) {
	// A stage nobody runs is the failure mode this whole feature has had
	// before, so assert the plugin declares the event the assembler adds.
	plugin := newMemoryRecallPlugin(&stubMemoryService{})
	require.Equal(t, []types.EventType{types.MEMORY_RECALL}, plugin.ActivationEvents())
}
