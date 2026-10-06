package docformat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

// labelTemperature is effectively greedy decoding.
const labelTemperature = 0.01

// chatCompleter runs labeling on a WeKnora chat model: thinking off (the
// answer is a lookup, and reasoning models are several times slower with
// it), near-greedy sampling, JSON output.
type chatCompleter struct{ model chat.Chat }

// ChatCompleter adapts a workspace chat model to the Completer interface.
func ChatCompleter(model chat.Chat) Completer { return chatCompleter{model} }

func (c chatCompleter) Complete(ctx context.Context, messages []Message) (string, error) {
	msgs := make([]chat.Message, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, chat.Message{Role: m.Role, Content: m.Content})
	}
	thinking := false
	resp, err := c.model.Chat(ctx, msgs, &chat.ChatOptions{
		// 0 would be dropped by the OpenAI request's omitempty and the
		// server default (~0.7 for Qwen) would apply instead
		Temperature: labelTemperature,
		MaxTokens:   8192,
		Thinking:    &thinking,
		Format:      json.RawMessage(`{"type":"object"}`),
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("empty model response")
	}
	return resp.Content, nil
}
