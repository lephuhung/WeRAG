package abbreviation

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// completeBindingFor builds a full turn binding for an arbitrary current reply.
func completeBindingFor(rawQuery string) types.AbbreviationBinding {
	b := readyBinding()
	b.RawQuery = rawQuery
	return b
}

func readyBinding() types.AbbreviationBinding {
	return types.AbbreviationBinding{
		Owner: types.AbbreviationOwner{
			TenantID:    7,
			SessionID:   "sess-1",
			OwnerID:     "owner-1",
			PrincipalID: "principal-1",
		},
		UserMessageID:      "msg-u1",
		AssistantMessageID: "msg-a1",
		RawQuery:           "ATTT có yêu cầu gì",
	}
}

func readyResolutionForBind(t *testing.T) types.AbbreviationResolution {
	t.Helper()
	r := Inspect("ATTT có yêu cầu gì", nil)
	applied, err := ApplyDefinitions(r, []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: "An toàn thực phẩm", SourceMessageID: "msg-u1", Start: 0, End: len("An toàn thực phẩm")},
	})
	if err != nil {
		t.Fatalf("setup ApplyDefinitions err = %v", err)
	}
	if applied.Status != "ready" {
		t.Fatalf("setup status = %q, want ready", applied.Status)
	}
	return applied
}

// Binding.RawQuery is the CURRENT user reply; the resolution may carry the
// ROOT question as OriginalQuery on resume. They must not be equated.
func TestBindTurnAcceptsResumedRootQuestion(t *testing.T) {
	r := readyResolutionForBind(t)
	b := readyBinding()
	// The current user reply differs from the root question carried by the
	// resolution; binding must still succeed with terms/spans intact.
	b.RawQuery = "cho hỏi ATTT là viết tắt của gì"
	b.UserMessageID = "msg-u9"
	ctx, err := BindTurn(context.Background(), b, r)
	if err != nil {
		t.Fatalf("BindTurn err = %v, want nil (root/original may differ from raw reply)", err)
	}
	if err := RequireTurn(ctx, b); err != nil {
		t.Fatalf("RequireTurn err = %v", err)
	}
	got, ok := ResolutionFromContext(ctx)
	if !ok {
		t.Fatalf("ResolutionFromContext ok = false")
	}
	if got.OriginalQuery != "ATTT có yêu cầu gì" {
		t.Fatalf("original = %q", got.OriginalQuery)
	}
	if len(got.Terms) != 1 || got.Terms[0].Source != "user_current_request" {
		t.Fatalf("terms = %+v", got.Terms)
	}
}

func TestBindTurnRoundTripAndIsolation(t *testing.T) {
	b := readyBinding()
	r := readyResolutionForBind(t)
	ctx, err := BindTurn(context.Background(), b, r)
	if err != nil {
		t.Fatalf("BindTurn err = %v", err)
	}
	if err := RequireTurn(ctx, b); err != nil {
		t.Fatalf("RequireTurn err = %v", err)
	}
	first, ok := ResolutionFromContext(ctx)
	if !ok {
		t.Fatalf("ResolutionFromContext ok = false")
	}
	// Mutating the fetched copy — including nested definition evidence and
	// occurrence spans — must not corrupt the sealed payload.
	first.Terms[0].FullForm = "MUTATED"
	first.Terms[0].Meanings = append(first.Terms[0].Meanings, types.AbbreviationMeaning{ID: "x"})
	first.Terms[0].Occurrences[0].Start = 999
	first.Terms[0].Definition.FullForm = "MUTATED"
	first.Terms[0].Definition.Start = 999
	first.UnknownTerms = append(first.UnknownTerms, "MUTATED")
	second, ok := ResolutionFromContext(ctx)
	if !ok {
		t.Fatalf("second ResolutionFromContext ok = false")
	}
	if second.Terms[0].FullForm != "An toàn thực phẩm" {
		t.Fatalf("sealed payload shares term storage with fetched copy")
	}
	if len(second.Terms[0].Meanings) != 0 {
		t.Fatalf("sealed meanings mutated via fetched copy: %+v", second.Terms[0].Meanings)
	}
	if second.Terms[0].Occurrences[0].Start != 0 {
		t.Fatalf("sealed occurrences mutated via fetched copy: %+v", second.Terms[0].Occurrences)
	}
	if second.Terms[0].Definition == nil || second.Terms[0].Definition.FullForm != "An toàn thực phẩm" ||
		second.Terms[0].Definition.Start != 0 {
		t.Fatalf("sealed definition mutated via fetched copy: %+v", second.Terms[0].Definition)
	}
}

func TestRequireTurnRejectsWrongBinding(t *testing.T) {
	b := readyBinding()
	ctx, err := BindTurn(context.Background(), b, readyResolutionForBind(t))
	if err != nil {
		t.Fatalf("BindTurn err = %v", err)
	}
	wrong := b
	wrong.RawQuery = "câu hỏi khác"
	if err := RequireTurn(ctx, wrong); !errors.Is(err, types.ErrAbbreviationConflict) {
		t.Fatalf("wrong raw query err = %v, want ErrAbbreviationConflict", err)
	}
	wrong = b
	wrong.Owner.PrincipalID = "principal-2"
	if err := RequireTurn(ctx, wrong); !errors.Is(err, types.ErrAbbreviationConflict) {
		t.Fatalf("wrong principal err = %v, want ErrAbbreviationConflict", err)
	}
	wrong = b
	wrong.UserMessageID = "msg-u2"
	if err := RequireTurn(ctx, wrong); !errors.Is(err, types.ErrAbbreviationConflict) {
		t.Fatalf("wrong message id err = %v, want ErrAbbreviationConflict", err)
	}
	if err := RequireTurn(context.Background(), b); !errors.Is(err, types.ErrAbbreviationNotFound) {
		t.Fatalf("missing payload err = %v, want ErrAbbreviationNotFound", err)
	}
}

// A generic map or DTO stored under the same key must not be accepted.
func TestResolutionFromContextRejectsForeignPayload(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.AbbreviationResolutionContextKey, map[string]string{"x": "y"})
	if _, ok := ResolutionFromContext(ctx); ok {
		t.Fatalf("map payload under resolution key was accepted")
	}
	if err := RequireTurn(ctx, readyBinding()); !errors.Is(err, types.ErrAbbreviationNotFound) {
		t.Fatalf("foreign payload err = %v, want ErrAbbreviationNotFound", err)
	}
}

// The sealed payload must survive logger.CloneContext (clone decision true)
// without becoming re-deserializable into a forged payload.
func TestSealedTurnSurvivesCloneContext(t *testing.T) {
	b := readyBinding()
	ctx, err := BindTurn(context.Background(), b, readyResolutionForBind(t))
	if err != nil {
		t.Fatalf("BindTurn err = %v", err)
	}
	cloned := logger.CloneContext(ctx)
	if err := RequireTurn(cloned, b); err != nil {
		t.Fatalf("RequireTurn on cloned ctx err = %v", err)
	}
	if _, ok := ResolutionFromContext(cloned); !ok {
		t.Fatalf("ResolutionFromContext on cloned ctx ok = false")
	}
}

// A seal requires a positive tenant, both actual message IDs, session,
// principal and the current raw input. OwnerID keeps caller-specific
// semantics and is compared exactly, not invented.
func TestBindTurnRequiresCompleteBinding(t *testing.T) {
	r := readyResolutionForBind(t)
	cases := map[string]func(*types.AbbreviationBinding){
		"tenant":    func(b *types.AbbreviationBinding) { b.Owner.TenantID = 0 },
		"assistant": func(b *types.AbbreviationBinding) { b.AssistantMessageID = "" },
		"user":      func(b *types.AbbreviationBinding) { b.UserMessageID = "" },
		"session":   func(b *types.AbbreviationBinding) { b.Owner.SessionID = "" },
		"principal": func(b *types.AbbreviationBinding) { b.Owner.PrincipalID = "" },
		"raw":       func(b *types.AbbreviationBinding) { b.RawQuery = "" },
	}
	for name, mutate := range cases {
		b := readyBinding()
		mutate(&b)
		if _, err := BindTurn(context.Background(), b, r); !errors.Is(err, types.ErrAbbreviationConflict) {
			t.Fatalf("%s: err = %v, want ErrAbbreviationConflict", name, err)
		}
	}
}

// A populated resolution CurrentUserMessageID must agree with the binding's
// user message; the root OriginalQuery may still differ (continuation).
func TestBindTurnRejectsCurrentMessageMismatch(t *testing.T) {
	r := readyResolutionForBind(t)
	r.CurrentUserMessageID = "another-user-message"
	if _, err := BindTurn(context.Background(), readyBinding(), r); !errors.Is(err, types.ErrAbbreviationConflict) {
		t.Fatalf("err = %v, want ErrAbbreviationConflict", err)
	}
	r.CurrentUserMessageID = "msg-u1"
	if _, err := BindTurn(context.Background(), readyBinding(), r); err != nil {
		t.Fatalf("matching current message err = %v", err)
	}
}

// Every binding dimension is compared exactly by RequireTurn.
func TestRequireTurnMismatchDimensions(t *testing.T) {
	b := readyBinding()
	ctx, err := BindTurn(context.Background(), b, readyResolutionForBind(t))
	if err != nil {
		t.Fatalf("BindTurn err = %v", err)
	}
	cases := map[string]func(*types.AbbreviationBinding){
		"tenant":    func(w *types.AbbreviationBinding) { w.Owner.TenantID = 99 },
		"session":   func(w *types.AbbreviationBinding) { w.Owner.SessionID = "sess-2" },
		"owner":     func(w *types.AbbreviationBinding) { w.Owner.OwnerID = "owner-2" },
		"principal": func(w *types.AbbreviationBinding) { w.Owner.PrincipalID = "principal-2" },
		"user":      func(w *types.AbbreviationBinding) { w.UserMessageID = "msg-u2" },
		"assistant": func(w *types.AbbreviationBinding) { w.AssistantMessageID = "msg-a2" },
		"raw":       func(w *types.AbbreviationBinding) { w.RawQuery = "câu hỏi khác" },
	}
	for name, mutate := range cases {
		wrong := b
		mutate(&wrong)
		if err := RequireTurn(ctx, wrong); !errors.Is(err, types.ErrAbbreviationConflict) {
			t.Fatalf("%s: err = %v, want ErrAbbreviationConflict", name, err)
		}
	}
}

// Forged ready payloads — unverified contents, inexact coverage, over-budget
// input — must not seal, even with a complete binding.
func TestBindTurnRejectsMalformedReadyPayloads(t *testing.T) {
	cases := map[string]func(*types.AbbreviationResolution){
		"wrong_dictionary_full_form": func(r *types.AbbreviationResolution) {
			r2, err := ApplySelections(Inspect("ATTT là gì", []*types.Abbreviation{
				{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
			}), map[string]string{"attt": "m-1"})
			if err != nil {
				t.Fatalf("setup err = %v", err)
			}
			r2.Terms[0].FullForm = "Invented expansion"
			*r = r2
		},
		"empty_definition_evidence": func(r *types.AbbreviationResolution) {
			r.Terms[0].Definition = &types.AbbreviationDefinition{}
		},
		"missing_occurrences": func(r *types.AbbreviationResolution) {
			r.Terms[0].Occurrences = nil
		},
		"missing_terms": func(r *types.AbbreviationResolution) {
			r.Terms = nil
		},
		"wrong_span_text": func(r *types.AbbreviationResolution) {
			r.Terms[0].Occurrences = []types.AbbreviationOccurrence{{Start: 5, End: 8}}
		},
	}
	for name, mutate := range cases {
		fresh := readyResolutionForBind(t)
		mutate(&fresh)
		if _, err := BindTurn(context.Background(), readyBinding(), fresh); err == nil {
			t.Fatalf("invalid %s was sealed ready", name)
		}
	}
}

// The sealed payload must survive both logger clone paths.
func TestSealedTurnSurvivesCloneWithoutTrace(t *testing.T) {
	b := readyBinding()
	ctx, err := BindTurn(context.Background(), b, readyResolutionForBind(t))
	if err != nil {
		t.Fatalf("BindTurn err = %v", err)
	}
	cloned := logger.CloneContextWithoutTrace(ctx)
	if err := RequireTurn(cloned, b); err != nil {
		t.Fatalf("RequireTurn on traceless clone err = %v", err)
	}
	if _, ok := ResolutionFromContext(cloned); !ok {
		t.Fatalf("ResolutionFromContext on traceless clone ok = false")
	}
}
