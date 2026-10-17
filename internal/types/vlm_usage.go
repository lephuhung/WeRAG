package types

import (
	"context"
	"sync"
)

// VLMUsage is the token usage one vision-model call reported. OCR models
// served by vLLM (senocr-vi) return it like any chat completion; the prompt
// side includes the image tokens, which is where most of an OCR call's cost
// is.
type VLMUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Add folds another call's usage in.
func (u *VLMUsage) Add(o VLMUsage) {
	u.PromptTokens += o.PromptTokens
	u.CompletionTokens += o.CompletionTokens
	u.TotalTokens += o.TotalTokens
}

// Total is the total token count, computed when the provider left it out.
func (u VLMUsage) Total() int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

// VLMUsageSink collects the usage of every vision-model call made under a
// context. VLM.Predict returns text only; a caller that must bill those
// calls (the chat-attachment OCR) installs a sink and reads it afterwards.
type VLMUsageSink struct {
	mu    sync.Mutex
	calls int
	usage VLMUsage
}

// Report adds one call. It is safe to call from several goroutines (page
// OCR runs with bounded concurrency).
func (s *VLMUsageSink) Report(u VLMUsage) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.usage.Add(u)
}

// Snapshot returns the number of calls reported and their summed usage.
func (s *VLMUsageSink) Snapshot() (calls int, usage VLMUsage) {
	if s == nil {
		return 0, VLMUsage{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.usage
}

type vlmUsageSinkKey struct{}

// WithVLMUsageSink installs sink for the vision-model calls made under ctx.
func WithVLMUsageSink(ctx context.Context, sink *VLMUsageSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, vlmUsageSinkKey{}, sink)
}

// ReportVLMUsage records one vision-model call's usage on the sink of ctx,
// if any. Providers call it after a successful completion; a context
// without a sink makes it a no-op.
func ReportVLMUsage(ctx context.Context, u VLMUsage) {
	if ctx == nil || u.Total() <= 0 {
		return
	}
	if sink, ok := ctx.Value(vlmUsageSinkKey{}).(*VLMUsageSink); ok {
		sink.Report(u)
	}
}
