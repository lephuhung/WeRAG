package im

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// A message that is never run, or fails before its user message is saved,
// gives back the question its preflight reserved; a context without a
// preflight, or a preflight that was refused (no grant), is a no-op.
func TestReleaseUsagePreflight(t *testing.T) {
	released := 0
	ctx := withUsagePreflight(context.Background(), imUsagePreflight{
		grant: &types.UsageTurnGrant{Release: func() { released++ }},
	})
	releaseUsagePreflight(ctx)
	if released != 1 {
		t.Fatalf("released %d times, want 1", released)
	}
	releaseUsagePreflight(context.Background())
	releaseUsagePreflight(withUsagePreflight(context.Background(), imUsagePreflight{}))
	// the continuation of a rotated session keeps the same reservation
	pre := freshSessionPreflight(imUsagePreflight{grant: &types.UsageTurnGrant{Release: func() { released++ }}}, "s2")
	releaseUsagePreflight(withUsagePreflight(context.Background(), pre))
	if released != 2 {
		t.Fatalf("released %d times, want 2", released)
	}
}
