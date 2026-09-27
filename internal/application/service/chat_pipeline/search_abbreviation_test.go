package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func newAbbreviationChatManage(query string, bus types.EventBusInterface) *types.ChatManage {
	cm := &types.ChatManage{}
	cm.Query = query
	cm.RewriteQuery = query
	cm.SessionID = "sess-1"
	cm.EventBus = bus
	return cm
}

// The search stage no longer resolves or expands abbreviations: the entry
// gate owns that decision, and its validated effective query arrives through
// RewriteQuery. These tests pin that the search path consumes the bound
// resolution untouched.

func TestSearch_UsesBoundRewriteQueryUnchanged(t *testing.T) {
	b := testBinding()
	r := readyAbbreviation(t, b.RawQuery)
	ctx := sealedCtx(t, r, b)

	cm := newAbbreviationChatManage(b.RawQuery, &recordingEventBus{})
	res := r
	cm.AbbreviationResolution = &res
	cm.AbbreviationBinding = b
	cm.RewriteQuery = r.EffectiveQuery

	p := &PluginSearch{}
	p.OnEvent(ctx, types.CHUNK_SEARCH, cm, func() *PluginError { return nil })
	// No KB targets and no web search: OnEvent returns early, but the query
	// must still be the gate-provided effective query, not a re-expansion.
	if cm.RewriteQuery != r.EffectiveQuery {
		t.Fatalf("RewriteQuery=%q, want %q", cm.RewriteQuery, r.EffectiveQuery)
	}
}

func TestSearchParallel_ClonePreservesAbbreviationState(t *testing.T) {
	b := testBinding()
	r := readyAbbreviation(t, b.RawQuery)

	cm := newAbbreviationChatManage(b.RawQuery, &recordingEventBus{})
	res := r
	cm.AbbreviationResolution = &res
	cm.AbbreviationBinding = b
	cm.RewriteQuery = r.EffectiveQuery

	clone := cm.Clone()
	if clone.AbbreviationBinding != cm.AbbreviationBinding {
		t.Fatalf("binding lost in clone: %+v", clone.AbbreviationBinding)
	}
	if clone.AbbreviationResolution == nil ||
		clone.AbbreviationResolution.EffectiveQuery != r.EffectiveQuery {
		t.Fatal("resolution lost in clone")
	}
	if clone.AbbreviationResolution == cm.AbbreviationResolution {
		t.Fatal("clone must deep-copy the resolution, not share the pointer")
	}
	if len(clone.AbbreviationResolution.Terms) == 0 ||
		clone.AbbreviationResolution.Terms[0].Occurrences == nil {
		t.Fatal("cloned terms lost their occurrences")
	}
}
