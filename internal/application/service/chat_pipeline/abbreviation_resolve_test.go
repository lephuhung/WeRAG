package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

func readyAbbreviation(t *testing.T, query string) types.AbbreviationResolution {
	t.Helper()
	r := abbreviation.Inspect(query, []*types.Abbreviation{
		{ID: "m1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "m2", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	})
	if r.Status != types.AbbreviationStatusReady {
		t.Fatalf("resolution not ready: %+v", r)
	}
	return r
}

func testBinding() types.AbbreviationBinding {
	return types.AbbreviationBinding{
		Owner: types.AbbreviationOwner{
			TenantID: 7, SessionID: "sess-1", OwnerID: "u1", PrincipalID: "user:u1",
		},
		UserMessageID:      "um-1",
		AssistantMessageID: "am-1",
		RawQuery:           "ATTT có yêu cầu gì",
	}
}

func sealedCtx(t *testing.T, r types.AbbreviationResolution, b types.AbbreviationBinding) context.Context {
	t.Helper()
	ctx, err := abbreviation.BindTurn(context.Background(), b, r)
	if err != nil {
		t.Fatalf("BindTurn: %v", err)
	}
	return ctx
}

func TestPluginAbbreviationResolve_ReadyTurnPasses(t *testing.T) {
	b := testBinding()
	r := readyAbbreviation(t, b.RawQuery)
	ctx := sealedCtx(t, r, b)

	cm := newAbbreviationChatManage(b.RawQuery, &recordingEventBus{})
	res := r
	cm.AbbreviationResolution = &res
	cm.AbbreviationBinding = b
	cm.RewriteQuery = r.EffectiveQuery

	p := &PluginAbbreviationResolve{}
	called := false
	err := p.OnEvent(ctx, types.ABBREVIATION_RESOLVE, cm, func() *PluginError {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v next=%v", err, called)
	}
	if cm.RewriteQuery != r.EffectiveQuery {
		t.Fatalf("RewriteQuery=%q", cm.RewriteQuery)
	}
	if len(cm.EventBus.(*recordingEventBus).events) != 0 {
		t.Fatal("resolve stage must not emit abbreviation events")
	}
}

func TestPluginAbbreviationResolve_BindingMismatchBlocks(t *testing.T) {
	b := testBinding()
	r := readyAbbreviation(t, b.RawQuery)
	ctx := sealedCtx(t, r, b)

	cm := newAbbreviationChatManage(b.RawQuery, &recordingEventBus{})
	res := r
	cm.AbbreviationResolution = &res
	forged := b
	forged.UserMessageID = "um-other"
	cm.AbbreviationBinding = forged

	p := &PluginAbbreviationResolve{}
	called := false
	err := p.OnEvent(ctx, types.ABBREVIATION_RESOLVE, cm, func() *PluginError {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("forged binding must block: err=%v next=%v", err, called)
	}
	if err.ErrorType != ErrAbbreviationGate.ErrorType {
		t.Fatalf("err=%v", err)
	}
}

func TestPluginAbbreviationResolve_UnsealedContextBlocks(t *testing.T) {
	b := testBinding()
	r := readyAbbreviation(t, b.RawQuery)
	cm := newAbbreviationChatManage(b.RawQuery, &recordingEventBus{})
	res := r
	cm.AbbreviationResolution = &res
	cm.AbbreviationBinding = b

	p := &PluginAbbreviationResolve{}
	called := false
	err := p.OnEvent(context.Background(), types.ABBREVIATION_RESOLVE, cm, func() *PluginError {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("unsealed binding must block: err=%v next=%v", err, called)
	}
}

func TestPluginAbbreviationResolve_NotReadyBlocks(t *testing.T) {
	r := abbreviation.Inspect("XYZABC là gì", nil)
	cm := newAbbreviationChatManage("XYZABC là gì", &recordingEventBus{})
	res := r
	cm.AbbreviationResolution = &res

	p := &PluginAbbreviationResolve{}
	called := false
	err := p.OnEvent(context.Background(), types.ABBREVIATION_RESOLVE, cm, func() *PluginError {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("non-ready resolution must block: err=%v next=%v", err, called)
	}
}

func TestPluginAbbreviationResolve_NoResolutionPasses(t *testing.T) {
	p := &PluginAbbreviationResolve{}
	cm := newAbbreviationChatManage("tỉnh họp sáng nay", &recordingEventBus{})
	called := false
	err := p.OnEvent(context.Background(), types.ABBREVIATION_RESOLVE, cm, func() *PluginError {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v next=%v", err, called)
	}
}
