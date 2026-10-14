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

// evalTemperature is the sampling Qwen-style reasoning models are tuned
// for; greedy decoding makes their thinking loop.
const evalTemperature = 0.6

// evalMaxTokens leaves room for the reasoning (~5-7k tokens on a công
// văn) plus the written evaluation.
const evalMaxTokens = 16384

// Evaluator runs the skill evaluation on a WeKnora chat model. Thinking
// is on by default: on the full format data (RenderData) the judgments
// (who signs for whom, whether Nơi nhận lists the head) were unreliable
// without it — measured on Qwen3.6-35B-A3B: 4-7 false alarms in 10 on a
// correct document without thinking, none with it. Whether the smaller
// judgment brief (RenderJudgment) changes that is measured by the live
// harness (live_eval_test.go). Temperature stays 0.6 with thinking off
// too: it is what the model is tuned for, and the harness can set it.
type Evaluator struct {
	Model       chat.Chat
	Thinking    bool
	Temperature float64
	MaxTokens   int
}

// ChatEvaluator adapts a workspace chat model for skill evaluation, with
// thinking on.
func ChatEvaluator(model chat.Chat) Completer { return ChatEvaluatorWithThinking(model, true) }

// ChatEvaluatorWithThinking is ChatEvaluator with the thinking switch.
func ChatEvaluatorWithThinking(model chat.Chat, thinking bool) *Evaluator {
	return &Evaluator{Model: model, Thinking: thinking, Temperature: evalTemperature, MaxTokens: evalMaxTokens}
}

// Complete runs one evaluation turn.
func (c *Evaluator) Complete(ctx context.Context, messages []Message) (string, error) {
	msgs := make([]chat.Message, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, chat.Message{Role: m.Role, Content: m.Content})
	}
	thinking := c.Thinking
	resp, err := c.Model.Chat(ctx, msgs, &chat.ChatOptions{
		Temperature: c.Temperature,
		MaxTokens:   c.MaxTokens,
		Thinking:    &thinking,
	})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("empty model response")
	}
	return resp.Content, nil
}
