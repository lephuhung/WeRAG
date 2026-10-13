package types

import "testing"

func TestPromptNowUsesVietnamTime(t *testing.T) {
	if _, off := PromptNow().Zone(); off != 7*3600 {
		t.Fatalf("prompt time offset = %d s, want +07:00", off)
	}
}
