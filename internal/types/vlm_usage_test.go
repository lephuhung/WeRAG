package types

import (
	"context"
	"sync"
	"testing"
)

func TestVLMUsageSinkCollectsCallsUnderContext(t *testing.T) {
	sink := &VLMUsageSink{}
	ctx := WithVLMUsageSink(context.Background(), sink)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ReportVLMUsage(ctx, VLMUsage{PromptTokens: 900, CompletionTokens: 50, TotalTokens: 950})
		}()
	}
	wg.Wait()
	calls, usage := sink.Snapshot()
	if calls != 8 || usage.TotalTokens != 8*950 || usage.PromptTokens != 8*900 {
		t.Fatalf("calls=%d usage=%+v", calls, usage)
	}
	// Empty usage and a context without a sink are no-ops.
	ReportVLMUsage(ctx, VLMUsage{})
	ReportVLMUsage(context.Background(), VLMUsage{TotalTokens: 1})
	if calls, _ := sink.Snapshot(); calls != 8 {
		t.Fatalf("empty usage must not count, calls=%d", calls)
	}
	if (VLMUsage{PromptTokens: 2, CompletionTokens: 3}).Total() != 5 {
		t.Fatal("Total must fall back to prompt+completion")
	}
}
