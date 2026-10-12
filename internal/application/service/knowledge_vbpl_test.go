package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestVBPLChunkSize(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, vbplDefaultChunkSize},
		{512, vbplDefaultChunkSize},
		{2000, 2000},
		{9000, vbplMaxChunkSize},
	} {
		if got := vbplChunkSize(types.ChunkingConfig{ChunkSize: tc.in}); got != tc.want {
			t.Errorf("vbplChunkSize(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
