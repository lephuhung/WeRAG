package abbreviation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestInspectUnknownATTT(t *testing.T) {
	r := Inspect("ATTT có yêu cầu gì", nil)
	if r.Status != "needs_definition" {
		t.Fatalf("status = %q, want needs_definition", r.Status)
	}
	if len(r.UnknownTerms) != 1 || r.UnknownTerms[0] != "ATTT" {
		t.Fatalf("unknown = %v, want [ATTT]", r.UnknownTerms)
	}
	if r.OriginalQuery != "ATTT có yêu cầu gì" {
		t.Fatalf("original = %q", r.OriginalQuery)
	}
}

func TestInspectProtectedDocumentCode(t *testing.T) {
	r := Inspect("172/GM-UBND", nil)
	if len(r.UnknownTerms) != 0 {
		t.Fatalf("unknown = %v, want empty", r.UnknownTerms)
	}
	if r.Status != "ready" {
		t.Fatalf("status = %q, want ready", r.Status)
	}
}

func TestBindTurnRejectsUnknown(t *testing.T) {
	b := types.AbbreviationBinding{RawQuery: "ATTT có yêu cầu gì"}
	_, err := BindTurn(context.Background(), b, Inspect(b.RawQuery, nil))
	if !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("err = %v, want ErrAbbreviationNotReady", err)
	}
}

// ATTT twice must yield one deduped unknown entry and two occurrences-free
// unknown bookkeeping (unknowns carry no spans; terms carry spans).
func TestInspectUnknownATTTTwice(t *testing.T) {
	r := Inspect("ATTT là gì, ATTT ở đâu", nil)
	if r.Status != "needs_definition" {
		t.Fatalf("status = %q, want needs_definition", r.Status)
	}
	if len(r.UnknownTerms) != 1 || r.UnknownTerms[0] != "ATTT" {
		t.Fatalf("unknown = %v, want single [ATTT]", r.UnknownTerms)
	}
}

// Nil and inactive (pending) rows are filtered: only active rows participate.
func TestInspectFiltersNilAndInactiveRows(t *testing.T) {
	active := &types.Abbreviation{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true}
	pending := &types.Abbreviation{ID: "m-2", ShortForm: "ATTT", FullForm: "Chờ duyệt", IsActive: false}
	r := Inspect("ATTT có yêu cầu gì", []*types.Abbreviation{nil, pending, active})
	if r.Status != "ready" {
		t.Fatalf("status = %q, want ready (single active meaning applies)", r.Status)
	}
	if len(r.Terms) != 1 {
		t.Fatalf("terms = %d, want 1", len(r.Terms))
	}
	term := r.Terms[0]
	if term.SelectedID != "m-1" || term.FullForm != "An toàn thực phẩm" || term.Source != "dictionary_active" {
		t.Fatalf("term not preselected from the single active row: %+v", term)
	}
	if len(term.Meanings) != 1 || term.Meanings[0].ID != "m-1" {
		t.Fatalf("meanings must contain only the active row: %+v", term.Meanings)
	}
	if r.EffectiveQuery != "An toàn thực phẩm (ATTT) có yêu cầu gì" {
		t.Fatalf("effective = %q", r.EffectiveQuery)
	}
}

// Exact-duplicate rows (same ID) collapse; distinct IDs stay distinct even
// when the full form text matches, so no real meaning is hidden.
func TestInspectDuplicateActiveRows(t *testing.T) {
	dup := &types.Abbreviation{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true}
	r := Inspect("ATTT là gì", []*types.Abbreviation{dup, dup})
	if r.Status != "ready" {
		t.Fatalf("status = %q, want ready (same ID deduplicated)", r.Status)
	}
	if len(r.Terms[0].Meanings) != 1 {
		t.Fatalf("meanings = %+v, want single deduped meaning", r.Terms[0].Meanings)
	}

	other := &types.Abbreviation{ID: "m-2", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true}
	r2 := Inspect("ATTT là gì", []*types.Abbreviation{
		{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
		other,
	})
	if r2.Status != "blocked_error" || r2.ErrorCode != "selection_required" {
		t.Fatalf("status = %q/%q, want blocked_error/selection_required", r2.Status, r2.ErrorCode)
	}
	if len(r2.Terms[0].Meanings) != 2 {
		t.Fatalf("distinct IDs must both survive: %+v", r2.Terms[0].Meanings)
	}
	if len(r2.UnknownTerms) != 0 {
		t.Fatalf("ambiguous term must not leak into unknown: %v", r2.UnknownTerms)
	}
}

// A token with both a document-code occurrence and a plain occurrence only
// contributes the plain occurrence.
func TestInspectMixedProtectedAndPlainOccurrences(t *testing.T) {
	active := &types.Abbreviation{ID: "m-1", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true}
	r := Inspect("172/GM-UBND và UBND phường", []*types.Abbreviation{active})
	if r.Status != "ready" {
		t.Fatalf("status = %q, want ready", r.Status)
	}
	if len(r.Terms) != 1 {
		t.Fatalf("terms = %d, want 1", len(r.Terms))
	}
	occs := r.Terms[0].Occurrences
	if len(occs) != 1 {
		t.Fatalf("occurrences = %+v, want exactly the plain one", occs)
	}
	plain := strings.Index("172/GM-UBND và UBND phường", "UBND phường")
	if occs[0].Start != plain || occs[0].End != plain+len("UBND") {
		t.Fatalf("occurrence span = %+v, want plain occurrence at %d", occs[0], plain)
	}
	if r.EffectiveQuery != "172/GM-UBND và Ủy ban nhân dân (UBND) phường" {
		t.Fatalf("effective = %q", r.EffectiveQuery)
	}
}

// Lowercase vowel-poor tokens are still candidates; the known `attt`
// limitation (vowel ratio keeps it invisible) is documented, not fixed.
func TestInspectLowercaseToken(t *testing.T) {
	r := Inspect("bmnn là gì", nil)
	if len(r.UnknownTerms) != 1 || r.UnknownTerms[0] != "bmnn" {
		t.Fatalf("unknown = %v, want [bmnn]", r.UnknownTerms)
	}
}

func TestInspectLowercaseAtttLimitation(t *testing.T) {
	// Existing heuristic: "attt" carries a vowel ratio of 0.25, so
	// IsLikelyAbbreviation misses it. Recorded as a known limitation; the
	// general detector is intentionally not widened in this task.
	r := Inspect("attt là gì", nil)
	if r.Status != "ready" || len(r.UnknownTerms) != 0 {
		t.Fatalf("status = %q unknown = %v, want ready with no unknowns (known attt gap)", r.Status, r.UnknownTerms)
	}
}

// Caller mutating returned slices must not corrupt later results.
func TestInspectResultsAreFreshCopies(t *testing.T) {
	first := Inspect("ATTT là gì", nil)
	first.UnknownTerms[0] = "MUTATED"
	second := Inspect("ATTT là gì", nil)
	if len(second.UnknownTerms) != 1 || second.UnknownTerms[0] != "ATTT" {
		t.Fatalf("second inspect affected by caller mutation: %v", second.UnknownTerms)
	}
}

// Term budget is enforced by blocking, never by truncating unknowns to ready.
func TestInspectTermBudgetExceeded(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < types.MaxAbbreviationTerms+1; i++ {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString("ZZ" + strings.Repeat("Q", i%3) + string(rune('A'+i%26)) + string(rune('A'+(i/26)%26)))
	}
	r := Inspect(sb.String(), nil)
	if r.Status != "blocked_error" || r.ErrorCode != "candidate_budget_exceeded" {
		t.Fatalf("status = %q/%q, want blocked_error/candidate_budget_exceeded", r.Status, r.ErrorCode)
	}
}

// A >50-rune candidate token blocks instead of truncating.
func TestInspectOversizeTokenBlocked(t *testing.T) {
	big := strings.Repeat("B", 51)
	r := Inspect("hỏi về "+big, nil)
	if r.Status != "blocked_error" || r.ErrorCode != "candidate_budget_exceeded" {
		t.Fatalf("status = %q/%q, want blocked_error/candidate_budget_exceeded", r.Status, r.ErrorCode)
	}
}

// Selection round-trip: ambiguous term resolved by a valid ID becomes ready;
// unknown terms cannot be resolved by selection; foreign IDs are rejected.
func TestApplySelectionsRoundTrip(t *testing.T) {
	actives := []*types.Abbreviation{
		{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
		{ID: "m-2", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
	}
	r := Inspect("ATTT là gì", actives)
	if r.Status != "blocked_error" {
		t.Fatalf("precondition status = %q", r.Status)
	}
	got, err := ApplySelections(r, map[string]string{"attt": "m-2"})
	if err != nil {
		t.Fatalf("ApplySelections err = %v", err)
	}
	if got.Status != "ready" {
		t.Fatalf("status = %q, want ready", got.Status)
	}
	if got.Terms[0].SelectedID != "m-2" || got.Terms[0].FullForm != "An toàn thông tin" || got.Terms[0].Source != "dictionary_active" {
		t.Fatalf("term = %+v", got.Terms[0])
	}
	if got.EffectiveQuery != "An toàn thông tin (ATTT) là gì" {
		t.Fatalf("effective = %q", got.EffectiveQuery)
	}

	if _, err := ApplySelections(r, map[string]string{"attt": "m-9"}); !errors.Is(err, types.ErrAbbreviationBadSelection) {
		t.Fatalf("foreign ID err = %v, want ErrAbbreviationBadSelection", err)
	}
	unknown := Inspect("ZZZQ là gì", nil)
	if _, err := ApplySelections(unknown, map[string]string{"zzzq": "m-1"}); !errors.Is(err, types.ErrAbbreviationBadSelection) {
		t.Fatalf("unknown-via-selection err = %v, want ErrAbbreviationBadSelection", err)
	}
}

// Definition round-trip: unknown term defined by the user becomes ready with
// user_current_request source; rendered query uses the defined full form.
func TestApplyDefinitionsRoundTrip(t *testing.T) {
	r := Inspect("ATTT có yêu cầu gì", nil)
	defs := []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: "An toàn thực phẩm", SourceMessageID: "msg-cur", Start: 0, End: len("An toàn thực phẩm")},
	}
	got, err := ApplyDefinitions(r, defs)
	if err != nil {
		t.Fatalf("ApplyDefinitions err = %v", err)
	}
	if got.Status != "ready" {
		t.Fatalf("status = %q, want ready", got.Status)
	}
	term := got.Terms[0]
	if term.Source != "user_current_request" || term.FullForm != "An toàn thực phẩm" {
		t.Fatalf("term = %+v", term)
	}
	if term.Definition == nil || term.Definition.SourceMessageID != "msg-cur" {
		t.Fatalf("definition evidence missing: %+v", term.Definition)
	}
	if got.EffectiveQuery != "An toàn thực phẩm (ATTT) có yêu cầu gì" {
		t.Fatalf("effective = %q", got.EffectiveQuery)
	}
	// Redefining an already-sourced term conflicts; unknown short forms are not found.
	if _, err := ApplyDefinitions(got, defs); !errors.Is(err, types.ErrAbbreviationConflict) {
		t.Fatalf("redefine err = %v, want ErrAbbreviationConflict", err)
	}
	if _, err := ApplyDefinitions(r, []types.AbbreviationDefinition{
		{ShortForm: "ZZZQ", FullForm: "Không có", SourceMessageID: "msg-cur", Start: 0, End: len("Không có")},
	}); !errors.Is(err, types.ErrAbbreviationNotFound) {
		t.Fatalf("missing term err = %v, want ErrAbbreviationNotFound", err)
	}
	// Caller mutating the defs slice afterwards must not affect the stored copy.
	defs[0].FullForm = "MUTATED"
	if got.Terms[0].Definition.FullForm != "An toàn thực phẩm" {
		t.Fatalf("definition aliases caller slice")
	}
}

// A validated explicit user definition supersedes a dictionary selection for
// the same candidate; active meanings stay as snapshot data.
func TestApplyDefinitionsOverrideSingleActive(t *testing.T) {
	r := Inspect("ATTT có yêu cầu gì", []*types.Abbreviation{
		{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
	})
	if r.Status != "ready" {
		t.Fatalf("precondition status = %q", r.Status)
	}
	full := "An toàn thực phẩm"
	got, err := ApplyDefinitions(r, []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: full, SourceMessageID: "msg-u1", Start: 0, End: len(full)},
	})
	if err != nil {
		t.Fatalf("ApplyDefinitions err = %v", err)
	}
	term := got.Terms[0]
	if term.Source != "user_current_request" || term.FullForm != full {
		t.Fatalf("override did not win: %+v", term)
	}
	if term.SelectedID != "" {
		t.Fatalf("override must clear the dictionary selection: %+v", term)
	}
	if len(term.Meanings) != 1 || term.Meanings[0].ID != "m-1" {
		t.Fatalf("active meanings must stay as snapshot data: %+v", term.Meanings)
	}
	if got.EffectiveQuery != full+" (ATTT) có yêu cầu gì" {
		t.Fatalf("effective = %q", got.EffectiveQuery)
	}
}

// Previously-selected multi-active terms accept an explicit override, but a
// repeated user definition still conflicts.
func TestApplyDefinitionsOverrideSelectedMulti(t *testing.T) {
	r := Inspect("ATTT là gì", []*types.Abbreviation{
		{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "m-2", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	})
	selected, err := ApplySelections(r, map[string]string{"attt": "m-1"})
	if err != nil {
		t.Fatalf("setup ApplySelections err = %v", err)
	}
	full := "An toàn lao động"
	overridden, err := ApplyDefinitions(selected, []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: full, SourceMessageID: "msg-u1", Start: 0, End: len(full)},
	})
	if err != nil {
		t.Fatalf("override err = %v", err)
	}
	if overridden.Terms[0].Source != "user_current_request" || overridden.Terms[0].FullForm != full ||
		overridden.Terms[0].SelectedID != "" {
		t.Fatalf("override did not win cleanly: %+v", overridden.Terms[0])
	}
	if _, err := ApplyDefinitions(overridden, []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: full, SourceMessageID: "msg-u2", Start: 0, End: len(full)},
	}); !errors.Is(err, types.ErrAbbreviationConflict) {
		t.Fatalf("repeated user definition err = %v, want ErrAbbreviationConflict", err)
	}
}

// Budget breaches are structural content invariants: no apply path —
// no-op or fully filled — may wash them into ready.
func TestBudgetSurvivesApply(t *testing.T) {
	long := strings.Repeat("B", 51)
	over := Inspect(long+" là gì", []*types.Abbreviation{
		{ID: "m-1", ShortForm: long, FullForm: "Full", IsActive: true},
	})
	if over.Status != "blocked_error" || over.ErrorCode != "candidate_budget_exceeded" {
		t.Fatalf("precondition status = %q/%q", over.Status, over.ErrorCode)
	}
	if got, err := ApplyDefinitions(over, nil); err != nil || got.Status == "ready" {
		t.Fatalf("no-op definitions washed an over-budget input: status=%q err=%v", got.Status, err)
	}
	if got, _ := ApplySelections(over, nil); got.Status == "ready" {
		t.Fatalf("no-op selections washed an over-budget input into ready")
	}
	filled, err := ApplyDefinitions(over, []types.AbbreviationDefinition{
		{ShortForm: long, FullForm: "Người dùng định nghĩa", SourceMessageID: "msg-u1", Start: 0, End: len("Người dùng định nghĩa")},
	})
	if err != nil {
		t.Fatalf("override on over-budget term err = %v", err)
	}
	if filled.Status == "ready" || filled.ErrorCode != "candidate_budget_exceeded" {
		t.Fatalf("filled over-budget input escaped the budget: %q/%q", filled.Status, filled.ErrorCode)
	}
	if _, err := BindTurn(context.Background(), completeBindingFor(long+" là gì"), filled); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("seal of over-budget input err = %v, want ErrAbbreviationNotReady", err)
	}
	if _, err := RenderResolvedQuery(filled); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("render of over-budget input err = %v, want ErrAbbreviationNotReady", err)
	}
}

// More than MaxAbbreviationTerms stays blocked even when fully defined.
func TestTermCountBudgetSurvivesFilledDefinitions(t *testing.T) {
	const n = types.MaxAbbreviationTerms + 1
	var sb strings.Builder
	toks := make([]string, 0, n)
	for i := 0; i < n; i++ {
		tok := "ZZ" + strings.Repeat("Q", i%3) + string(rune('A'+i%26)) + string(rune('A'+(i/26)%26))
		toks = append(toks, tok)
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(tok)
	}
	r := Inspect(sb.String(), nil)
	if r.Status != "blocked_error" || r.ErrorCode != "candidate_budget_exceeded" {
		t.Fatalf("precondition status = %q/%q", r.Status, r.ErrorCode)
	}
	defs := make([]types.AbbreviationDefinition, 0, n)
	for _, tok := range toks {
		full := "Nghĩa của " + tok
		defs = append(defs, types.AbbreviationDefinition{
			ShortForm: tok, FullForm: full, SourceMessageID: "msg-u1", Start: 0, End: len(full),
		})
	}
	filled, err := ApplyDefinitions(r, defs)
	if err != nil {
		t.Fatalf("filled definitions err = %v", err)
	}
	if filled.Status == "ready" || filled.ErrorCode != "candidate_budget_exceeded" {
		t.Fatalf("filled %d-term input escaped the budget: %q/%q", n, filled.Status, filled.ErrorCode)
	}
}

// Every still-unselected ambiguous term must be covered atomically; partial
// maps fail without mutating the input.
func TestApplySelectionsRequiresAllAmbiguous(t *testing.T) {
	rows := []*types.Abbreviation{
		{ID: "a-1", ShortForm: "ATTT", FullForm: "Full A1", IsActive: true},
		{ID: "a-2", ShortForm: "ATTT", FullForm: "Full A2", IsActive: true},
		{ID: "b-1", ShortForm: "UBND", FullForm: "Full B1", IsActive: true},
		{ID: "b-2", ShortForm: "UBND", FullForm: "Full B2", IsActive: true},
	}
	r := Inspect("ATTT và UBND", rows)
	if r.Status != "blocked_error" {
		t.Fatalf("precondition status = %q", r.Status)
	}
	if _, err := ApplySelections(r, map[string]string{"attt": "a-1"}); !errors.Is(err, types.ErrAbbreviationBadSelection) {
		t.Fatalf("partial map err = %v, want ErrAbbreviationBadSelection", err)
	}
	if r.Status != "blocked_error" || r.Terms[0].Source != "" {
		t.Fatalf("failed apply must leave the input unchanged: %+v", r)
	}
	got, err := ApplySelections(r, map[string]string{"attt": "a-1", "ubnd": "b-2"})
	if err != nil {
		t.Fatalf("full map err = %v", err)
	}
	if got.Status != "ready" {
		t.Fatalf("status = %q, want ready", got.Status)
	}
	if got.Terms[0].FullForm != "Full A1" || got.Terms[1].FullForm != "Full B2" {
		t.Fatalf("terms = %+v", got.Terms)
	}
	// Selections never overwrite an existing user mapping: overriding a
	// dictionary term by definition first makes any later selection for it
	// a conflict, while the other term still resolves.
	overBase, err := ApplyDefinitions(Inspect("ATTT là gì", []*types.Abbreviation{
		{ID: "a-1", ShortForm: "ATTT", FullForm: "Full A1", IsActive: true},
	}), []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: "Nghĩa người dùng", SourceMessageID: "msg-u1", Start: 0, End: len("Nghĩa người dùng")},
	})
	if err != nil {
		t.Fatalf("setup override err = %v", err)
	}
	if _, err := ApplySelections(overBase, map[string]string{"attt": "a-1"}); !errors.Is(err, types.ErrAbbreviationConflict) {
		t.Fatalf("selection over user mapping err = %v, want ErrAbbreviationConflict", err)
	}
}

// Rendering rejects unverified term contents: invented dictionary full
// forms, empty definition evidence, and mismatched definition fields.
// mustUserDefinedATTT builds a fresh, proven-valid user-defined resolution.
// Every mutation case starts from its own copy so cases cannot inherit an
// already-invalid state and pass for the wrong reason.
func mustUserDefinedATTT(t *testing.T) types.AbbreviationResolution {
	t.Helper()
	r, err := ApplyDefinitions(Inspect("ATTT là gì", nil), []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: "An toàn thực phẩm", SourceMessageID: "msg-u1", Start: 0, End: len("An toàn thực phẩm")},
	})
	if err != nil {
		t.Fatalf("setup err = %v", err)
	}
	if r.Status != "ready" {
		t.Fatalf("baseline status = %q, want ready", r.Status)
	}
	if _, err := RenderResolvedQuery(r); err != nil {
		t.Fatalf("baseline does not render: %v", err)
	}
	return r
}

func mustDictSelectedATTT(t *testing.T) types.AbbreviationResolution {
	t.Helper()
	r, err := ApplySelections(Inspect("ATTT là gì", []*types.Abbreviation{
		{ID: "m-1", ShortForm: "ATTT", FullForm: "An toàn thông tin", IsActive: true},
		{ID: "m-2", ShortForm: "ATTT", FullForm: "An toàn thực phẩm", IsActive: true},
	}), map[string]string{"attt": "m-2"})
	if err != nil {
		t.Fatalf("setup err = %v", err)
	}
	if r.Status != "ready" {
		t.Fatalf("baseline status = %q, want ready", r.Status)
	}
	if _, err := RenderResolvedQuery(r); err != nil {
		t.Fatalf("baseline does not render: %v", err)
	}
	return r
}

func TestRenderRejectsUnverifiedTerms(t *testing.T) {
	forged := mustDictSelectedATTT(t)
	forged.Terms[0].FullForm = "Invented expansion"
	if _, err := RenderResolvedQuery(forged); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("invented full form rendered, err = %v", err)
	}

	emptied := mustUserDefinedATTT(t)
	emptied.Terms[0].Definition = &types.AbbreviationDefinition{}
	if _, err := RenderResolvedQuery(emptied); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("empty definition evidence rendered, err = %v", err)
	}
	mismatched := mustUserDefinedATTT(t)
	def := *mismatched.Terms[0].Definition
	def.FullForm = "Nghĩa khác"
	mismatched.Terms[0].Definition = &def
	if _, err := RenderResolvedQuery(mismatched); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("mismatched definition full form rendered, err = %v", err)
	}
	shortMismatch := mustUserDefinedATTT(t)
	defShort := *shortMismatch.Terms[0].Definition
	defShort.ShortForm = "ZZZQ"
	shortMismatch.Terms[0].Definition = &defShort
	if _, err := RenderResolvedQuery(shortMismatch); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("mismatched definition short form rendered, err = %v", err)
	}
	noSource := mustUserDefinedATTT(t)
	defSrc := *noSource.Terms[0].Definition
	defSrc.SourceMessageID = ""
	noSource.Terms[0].Definition = &defSrc
	if _, err := RenderResolvedQuery(noSource); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("sourceless definition evidence rendered, err = %v", err)
	}
	badSpan := mustUserDefinedATTT(t)
	def2 := *badSpan.Terms[0].Definition
	def2.End = def2.Start + 3
	badSpan.Terms[0].Definition = &def2
	if _, err := RenderResolvedQuery(badSpan); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("short definition span rendered, err = %v", err)
	}
	if _, err := ApplyDefinitions(Inspect("ATTT là gì", nil), []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: "An toàn thực phẩm", SourceMessageID: "msg-u1", Start: 0, End: 3},
	}); !errors.Is(err, types.ErrAbbreviationBadSelection) {
		t.Fatalf("short definition span accepted at entry, err = %v", err)
	}
}

// Whitespace-only full forms carry no meaning: rejected at definition
// entry and by shared source validation for both user and dictionary
// sources, without rewriting evidence bytes.
func TestRenderRejectsWhitespaceFullForms(t *testing.T) {
	for _, blank := range []string{"   ", " \t \n", " \u2003", "\u00a0\u2007"} {
		if _, err := ApplyDefinitions(Inspect("ATTT là gì", nil), []types.AbbreviationDefinition{
			{ShortForm: "ATTT", FullForm: blank, SourceMessageID: "msg-u1", Start: 0, End: len(blank)},
		}); !errors.Is(err, types.ErrAbbreviationBadSelection) {
			t.Fatalf("blank %q accepted at definition entry, err = %v", blank, err)
		}
		userBlank := mustUserDefinedATTT(t)
		userBlank.Terms[0].FullForm = blank
		def := *userBlank.Terms[0].Definition
		def.FullForm = blank
		def.End = def.Start + len(blank)
		userBlank.Terms[0].Definition = &def
		if _, err := RenderResolvedQuery(userBlank); !errors.Is(err, types.ErrAbbreviationNotReady) {
			t.Fatalf("blank user full form %q rendered, err = %v", blank, err)
		}
		dictBlank := mustDictSelectedATTT(t)
		dictBlank.Terms[0].FullForm = blank
		dictBlank.Terms[0].Meanings[1].FullForm = blank
		if _, err := RenderResolvedQuery(dictBlank); !errors.Is(err, types.ErrAbbreviationNotReady) {
			t.Fatalf("blank dictionary full form %q rendered, err = %v", blank, err)
		}
		rows := Inspect("ATTT là gì", []*types.Abbreviation{
			{ID: "m-1", ShortForm: "ATTT", FullForm: blank, IsActive: true},
		})
		if rows.Status == "ready" {
			t.Fatalf("blank dictionary meaning %q became ready", blank)
		}
	}
}

// Coverage is exact: dropped terms or occurrences, and spans over unrelated
// text, fail instead of emitting the raw query as ready.
// mustTwoTermReady builds a fresh, proven-valid two-term resolution per
// mutation case so coverage cases cannot share mutable term slices.
func mustTwoTermReady(t *testing.T) types.AbbreviationResolution {
	t.Helper()
	r, err := ApplyDefinitions(Inspect("ATTT và UBND họp", []*types.Abbreviation{
		{ID: "m-1", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	}), []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: "An toàn thực phẩm", SourceMessageID: "msg-u1", Start: 0, End: len("An toàn thực phẩm")},
	})
	if err != nil {
		t.Fatalf("setup err = %v", err)
	}
	if r.Status != "ready" {
		t.Fatalf("baseline status = %q, want ready", r.Status)
	}
	if _, err := RenderResolvedQuery(r); err != nil {
		t.Fatalf("baseline does not render: %v", err)
	}
	return r
}

func TestRenderRequiresExactCoverage(t *testing.T) {
	droppedTerm := mustTwoTermReady(t)
	droppedTerm.Terms = droppedTerm.Terms[:1]
	if _, err := RenderResolvedQuery(droppedTerm); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("removed term rendered, err = %v", err)
	}
	droppedOcc := mustTwoTermReady(t)
	droppedOcc.Terms[0].Occurrences = nil
	if _, err := RenderResolvedQuery(droppedOcc); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("removed occurrences rendered, err = %v", err)
	}
	movedSpan := mustTwoTermReady(t)
	movedSpan.Terms[0].Occurrences = []types.AbbreviationOccurrence{{Start: 14, End: 17}}
	if _, err := RenderResolvedQuery(movedSpan); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("span over unrelated text rendered, err = %v", err)
	}
	extraTerm := mustTwoTermReady(t)
	extraTerm.Terms = append(extraTerm.Terms, extraTerm.Terms[0])
	if _, err := RenderResolvedQuery(extraTerm); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("duplicated term rendered, err = %v", err)
	}
	// Repeated occurrences all survive: dropping one of two fails.
	twice, err := ApplyDefinitions(Inspect("ATTT là gì, ATTT ở đâu", nil), []types.AbbreviationDefinition{
		{ShortForm: "ATTT", FullForm: "An toàn thực phẩm", SourceMessageID: "msg-u1", Start: 0, End: len("An toàn thực phẩm")},
	})
	if err != nil {
		t.Fatalf("setup err = %v", err)
	}
	if len(twice.Terms[0].Occurrences) != 2 {
		t.Fatalf("precondition occurrences = %+v", twice.Terms[0].Occurrences)
	}
	halved := twice
	halved.Terms[0].Occurrences = halved.Terms[0].Occurrences[:1]
	if _, err := RenderResolvedQuery(halved); !errors.Is(err, types.ErrAbbreviationNotReady) {
		t.Fatalf("halved occurrences rendered, err = %v", err)
	}
}

// Normalized duplicate selection keys are rejected atomically whether they
// name the same or different IDs; the input stays untouched and a valid
// one-key selection still succeeds.
func TestApplySelectionsRejectsDuplicateNormalizedKeys(t *testing.T) {
	rows := []*types.Abbreviation{
		{ID: "m-1", ShortForm: "ATTT", FullForm: "Meaning one", IsActive: true},
		{ID: "m-2", ShortForm: "ATTT", FullForm: "Meaning two", IsActive: true},
	}
	r := Inspect("ATTT có yêu cầu gì", rows)
	for name, ids := range map[string]map[string]string{
		"same-id":       {"attt": "m-1", " ATTT ": "m-1"},
		"different-ids": {"attt": "m-1", " ATTT ": "m-2"},
	} {
		if _, err := ApplySelections(r, ids); !errors.Is(err, types.ErrAbbreviationBadSelection) {
			t.Fatalf("%s: err = %v, want ErrAbbreviationBadSelection", name, err)
		}
	}
	if r.Status != "blocked_error" {
		t.Fatalf("failed applies mutated the input: %+v", r)
	}
	got, err := ApplySelections(r, map[string]string{" ATTT ": "m-2"})
	if err != nil {
		t.Fatalf("valid one-key selection err = %v", err)
	}
	if got.Status != "ready" || got.Terms[0].SelectedID != "m-2" {
		t.Fatalf("one-key selection did not resolve: %+v", got)
	}
}
