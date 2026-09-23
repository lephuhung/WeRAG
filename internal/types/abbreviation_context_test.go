package types

import (
	"context"
	"strings"
	"testing"
)

func TestAbbreviationCandidates_NormalizeDedupeCap(t *testing.T) {
	ctx := WithAbbreviationCandidates(context.Background(), []string{
		"  UBND ", "ubnd", "BCH", "", "   ", strings.Repeat("x", 51),
		"C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10", "C11",
	})
	got := AbbreviationCandidatesFromContext(ctx)
	if len(got) != maxAbbreviationCandidates {
		t.Fatalf("len=%d, want %d: %v", len(got), maxAbbreviationCandidates, got)
	}
	if got[0] != "UBND" || got[1] != "BCH" {
		t.Fatalf("dedupe/trim failed: %v", got[:3])
	}
	for _, c := range got {
		if c == strings.Repeat("x", 51) || c == "" {
			t.Fatalf("invalid candidate survived: %v", got)
		}
	}
}

func TestAbbreviationCandidates_EmptyAndAbsent(t *testing.T) {
	ctx := WithAbbreviationCandidates(context.Background(), nil)
	if got := AbbreviationCandidatesFromContext(ctx); got != nil {
		t.Fatalf("empty input must not set the key: %v", got)
	}
	if got := AbbreviationCandidatesFromContext(context.Background()); got != nil {
		t.Fatalf("unset key must return nil: %v", got)
	}
	if got := AbbreviationCandidatesFromContext(nil); got != nil {
		t.Fatalf("nil ctx must return nil: %v", got)
	}
}

func TestAbbreviationCandidates_DefensiveCopy(t *testing.T) {
	ctx := WithAbbreviationCandidates(context.Background(), []string{"UBND"})
	got := AbbreviationCandidatesFromContext(ctx)
	got[0] = "MUTATED"
	again := AbbreviationCandidatesFromContext(ctx)
	if again[0] != "UBND" {
		t.Fatalf("context slice must be isolated from callers: %v", again)
	}
}

func TestAbbreviationCandidates_CloneDecision(t *testing.T) {
	clone, declared := ContextCloneDecision(AbbreviationCandidatesContextKey)
	if !declared || !clone {
		t.Fatalf("clone decision = (%v, %v), want (true, true)", clone, declared)
	}
}
