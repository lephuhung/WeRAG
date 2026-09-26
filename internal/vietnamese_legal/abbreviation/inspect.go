package abbreviation

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// Inspect scans text for abbreviation candidates and builds a typed
// resolution against the active dictionary rows.
//
//   - Tokens are scanned with UTF-8 byte offsets. The existing
//     IsLikelyAbbreviation heuristic and isExpansionBoundary are reused on
//     every occurrence; neither is modified here.
//   - Occurrences inside document codes (172/GM-UBND) are protected and
//     contribute nothing: a token with both protected and plain occurrences
//     is grouped from its plain occurrences only.
//   - Nil and inactive (pending) rows are filtered. Exact-duplicate rows
//     (same ID) collapse; distinct IDs stay distinct even when their full
//     forms match, so no real meaning is hidden.
//   - No active meaning: the key joins UnknownTerms (deduped, first-appearance
//     order). Exactly one: preselected with source dictionary_active.
//     Several: left unselected; the resolution reports blocked_error with
//     selection_required (an intermediate coordinator state, never a user
//     question).
//   - Status precedence: candidate_budget_exceeded (term budget or >50-rune
//     token) > needs_definition (unknowns) > selection_required (ambiguous)
//     > ready. Budgets are structural invariants of the content, re-derived on
//     every path; over-budget input is never truncated into ready.
//   - EffectiveQuery is filled by RenderResolvedQuery only on ready.
//     Lifecycle fields (RequestID, message IDs, Version, ExpiresAt) stay zero;
//     the coordinator assigns them.
func Inspect(text string, active []*types.Abbreviation) types.AbbreviationResolution {
	r := types.AbbreviationResolution{OriginalQuery: text}
	if text == "" {
		r.Status = types.AbbreviationStatusReady
		return r
	}

	type group struct {
		short string
		key   string
		occs  []types.AbbreviationOccurrence
	}
	groups := map[string]*group{}
	var order []string

	for _, span := range extractWordTokenSpans(text) {
		if !IsLikelyAbbreviation(span.token) {
			continue
		}
		if !isExpansionBoundary(text, span.start, span.end) {
			continue
		}
		key := termKey(span.token)
		g, ok := groups[key]
		if !ok {
			g = &group{short: span.token, key: key}
			groups[key] = g
			order = append(order, key)
		}
		g.occs = append(g.occs, types.AbbreviationOccurrence{Start: span.start, End: span.end})
	}

	byShort := map[string][]*types.Abbreviation{}
	for _, a := range active {
		if a == nil || !a.IsActive {
			continue
		}
		k := termKey(a.ShortForm)
		if k == "" {
			continue
		}
		byShort[k] = append(byShort[k], a)
	}

	for _, key := range order {
		g := groups[key]
		rows := byShort[key]
		seen := map[string]struct{}{}
		var meanings []types.AbbreviationMeaning
		for _, row := range rows {
			if _, dup := seen[row.ID]; dup {
				continue
			}
			seen[row.ID] = struct{}{}
			meanings = append(meanings, types.AbbreviationMeaning{
				ID:          row.ID,
				FullForm:    row.FullForm,
				Description: row.Description,
			})
		}
		sort.SliceStable(meanings, func(i, j int) bool { return meanings[i].ID < meanings[j].ID })
		term := types.AbbreviationTerm{
			ShortForm:   g.short,
			Key:         key,
			Occurrences: append([]types.AbbreviationOccurrence(nil), g.occs...),
			Meanings:    meanings,
		}
		if len(meanings) == 1 {
			term.SelectedID = meanings[0].ID
			term.FullForm = meanings[0].FullForm
			term.Source = types.AbbreviationSourceDictionaryActive
		}
		r.Terms = append(r.Terms, term)
		if len(meanings) == 0 {
			r.UnknownTerms = append(r.UnknownTerms, g.short)
		}
	}

	finalizeResolution(&r)
	return r
}

// ApplyDefinitions resolves terms from explicit user-supplied definitions.
// Validation is all-or-nothing: any failure returns the input unchanged.
//
//   - Each definition must name an existing term whose key matches
//     (case-insensitive); an unmatched short form is not found.
//   - A validated explicit definition supersedes a dictionary selection for
//     the same candidate: incompatible selection fields are cleared while the
//     active meanings stay as snapshot data. Repeating a user definition for
//     an already user-sourced term still conflicts.
//   - Definition values are validated (non-empty short/full form within the
//     50/255-rune budgets, non-empty source message, non-empty span whose
//     byte length covers the full form). The source message is deliberately
//     NOT required to equal the current message: definitions may span several
//     persisted partial-reply messages, whose provenance the parser and
//     coordinator own.
//   - Stored definitions are deep copies; later caller mutation of defs has
//     no effect.
func ApplyDefinitions(r types.AbbreviationResolution, defs []types.AbbreviationDefinition) (types.AbbreviationResolution, error) {
	out := cloneResolution(r)
	for i := range defs {
		def := &defs[i]
		key := termKey(def.ShortForm)
		if key == "" {
			return r, types.ErrAbbreviationBadSelection
		}
		// An over-long short form is not an entry error: the term exists,
		// the definition applies, and the structural budget keeps the
		// resolution blocked with candidate_budget_exceeded at finalize.
		if !meaningfulText(def.FullForm) || utf8.RuneCountInString(def.FullForm) > types.MaxAbbreviationFullFormRunes {
			return r, types.ErrAbbreviationBadSelection
		}
		if def.SourceMessageID == "" || def.Start < 0 || def.End <= def.Start ||
			def.End-def.Start != len(def.FullForm) {
			return r, types.ErrAbbreviationBadSelection
		}
		term := findTerm(&out, key)
		if term == nil {
			return r, types.ErrAbbreviationNotFound
		}
		if term.Source == types.AbbreviationSourceUserCurrent {
			return r, types.ErrAbbreviationConflict
		}
		stored := *def
		term.Definition = &stored
		term.SelectedID = ""
		term.FullForm = def.FullForm
		term.Source = types.AbbreviationSourceUserCurrent
		// The sourced key leaves the unknown list; unknowns only ever name
		// terms that still lack any source.
		kept := out.UnknownTerms[:0]
		for _, u := range out.UnknownTerms {
			if termKey(u) != key {
				kept = append(kept, u)
			}
		}
		out.UnknownTerms = kept
	}
	finalizeResolution(&out)
	return out, nil
}

// ApplySelections resolves ambiguous terms by dictionary meaning ID.
// Validation is all-or-nothing: any failure returns the input unchanged.
//
//   - Map keys are normalized (lowercase/trim) term keys or short forms.
//   - The selected ID must belong to that term's own meanings; unknown terms
//     cannot be resolved by selection, foreign IDs are rejected, and existing
//     user-current-request mappings are never overwritten.
//   - Every still-unselected ambiguous term must be covered by the map;
//     a partial map is rejected with ErrAbbreviationBadSelection.
//   - Re-selecting the already-selected ID of a dictionary-sourced term is a
//     no-op; selecting a different ID for a sourced term conflicts.
func ApplySelections(r types.AbbreviationResolution, ids map[string]string) (types.AbbreviationResolution, error) {
	// Normalized duplicate keys (same or different IDs) are rejected before
	// any mutation, keeping the apply atomic and the input immutable.
	seenKeys := map[string]struct{}{}
	for rawKey := range ids {
		key := termKey(rawKey)
		if key == "" {
			return r, types.ErrAbbreviationBadSelection
		}
		if _, dup := seenKeys[key]; dup {
			return r, types.ErrAbbreviationBadSelection
		}
		seenKeys[key] = struct{}{}
	}
	out := cloneResolution(r)
	for rawKey, selectedID := range ids {
		term := findTerm(&out, termKey(rawKey))
		if term == nil {
			return r, types.ErrAbbreviationNotFound
		}
		if len(term.Meanings) == 0 {
			return r, types.ErrAbbreviationBadSelection
		}
		var meaning *types.AbbreviationMeaning
		for i := range term.Meanings {
			if term.Meanings[i].ID == selectedID {
				meaning = &term.Meanings[i]
				break
			}
		}
		if meaning == nil {
			return r, types.ErrAbbreviationBadSelection
		}
		if term.Source != "" {
			if term.Source == types.AbbreviationSourceDictionaryActive && term.SelectedID == selectedID {
				continue
			}
			return r, types.ErrAbbreviationConflict
		}
		term.SelectedID = meaning.ID
		term.FullForm = meaning.FullForm
		term.Source = types.AbbreviationSourceDictionaryActive
	}
	for i := range out.Terms {
		if len(out.Terms[i].Meanings) > 0 && out.Terms[i].Source == "" {
			return r, types.ErrAbbreviationBadSelection
		}
	}
	finalizeResolution(&out)
	return out, nil
}

// RenderResolvedQuery renders OriginalQuery, replacing resolved spans from
// right to left with `FullForm (ShortForm)`. The single pass over original
// offsets never recursively expands replacement text. Anything less than a
// fully verified resolution (budget breach, unknowns, unverified term
// contents, inexact span coverage) is an error, never silent raw passthrough.
func RenderResolvedQuery(r types.AbbreviationResolution) (string, error) {
	if overBudget(r) {
		return "", types.ErrAbbreviationNotReady
	}
	if len(r.UnknownTerms) > 0 {
		return "", types.ErrAbbreviationNotReady
	}
	for i := range r.Terms {
		if err := validateTermStructure(&r.Terms[i]); err != nil {
			return "", types.ErrAbbreviationNotReady
		}
	}
	if err := validateCoverage(r.OriginalQuery, r.Terms); err != nil {
		return "", types.ErrAbbreviationNotReady
	}
	type span struct {
		start, end int
		repl       string
	}
	var spans []span
	for i := range r.Terms {
		term := &r.Terms[i]
		for _, occ := range term.Occurrences {
			spans = append(spans, span{
				start: occ.Start,
				end:   occ.End,
				repl:  term.FullForm + " (" + term.ShortForm + ")",
			})
		}
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start > spans[j].start
		}
		return spans[i].end > spans[j].end
	})
	out := r.OriginalQuery
	limit := len(out) + 1
	for _, s := range spans {
		if s.end > limit {
			return "", types.ErrAbbreviationNotReady
		}
		limit = s.start
		out = out[:s.start] + s.repl + out[s.end:]
	}
	return out, nil
}

// overBudget re-derives the hard candidate budgets from resolution content
// alone, so apply, render and seal paths cannot launder an over-budget input
// into ready by forgetting how the breach was first observed.
func overBudget(r types.AbbreviationResolution) bool {
	if len(r.Terms) > types.MaxAbbreviationTerms {
		return true
	}
	for i := range r.Terms {
		if utf8.RuneCountInString(r.Terms[i].ShortForm) > types.MaxAbbreviationShortFormRunes {
			return true
		}
	}
	for _, u := range r.UnknownTerms {
		if utf8.RuneCountInString(u) > types.MaxAbbreviationShortFormRunes {
			return true
		}
	}
	return false
}

// termKey normalizes a short form for grouping and lookup.
func termKey(short string) string {
	return strings.ToLower(strings.TrimSpace(short))
}

// meaningfulText reports whether s carries non-whitespace content. Evidence
// bytes are never rewritten by validation; blank semantics are rejected.
func meaningfulText(s string) bool {
	return strings.TrimSpace(s) != ""
}

func findTerm(r *types.AbbreviationResolution, key string) *types.AbbreviationTerm {
	if key == "" {
		return nil
	}
	for i := range r.Terms {
		if r.Terms[i].Key == key {
			return &r.Terms[i]
		}
	}
	return nil
}

// validateTermStructure strictly checks one term's source contents: the
// normalized key/short agreement, bounded non-empty full form, selected
// ID/full-form agreement against the term's own meanings for dictionary
// source (with no definition authority attached), and matching short/full,
// non-empty source message and structurally sound span evidence with no
// selected authority for user source. It never consults message text the
// caller does not hold; provenance belongs to the parser/coordinator.
func validateTermStructure(term *types.AbbreviationTerm) error {
	if term == nil {
		return fmt.Errorf("nil term")
	}
	if term.ShortForm == "" || term.Key != termKey(term.ShortForm) {
		return fmt.Errorf("term key %q does not normalize short form %q", term.Key, term.ShortForm)
	}
	if utf8.RuneCountInString(term.ShortForm) > types.MaxAbbreviationShortFormRunes {
		return fmt.Errorf("term short form exceeds budget")
	}
	if !meaningfulText(term.FullForm) || utf8.RuneCountInString(term.FullForm) > types.MaxAbbreviationFullFormRunes {
		return fmt.Errorf("term full form is blank or out of bounds")
	}
	switch term.Source {
	case types.AbbreviationSourceDictionaryActive:
		if term.Definition != nil {
			return fmt.Errorf("dictionary term carries definition authority")
		}
		if term.SelectedID == "" || len(term.Meanings) == 0 {
			return fmt.Errorf("dictionary term lacks a selection")
		}
		for i := range term.Meanings {
			if term.Meanings[i].ID == term.SelectedID {
				if term.Meanings[i].FullForm != term.FullForm {
					return fmt.Errorf("dictionary full form disagrees with selected meaning")
				}
				return nil
			}
		}
		return fmt.Errorf("selected ID is not a listed meaning")
	case types.AbbreviationSourceUserCurrent:
		if term.SelectedID != "" {
			return fmt.Errorf("user term carries selected authority")
		}
		return checkDefinitionEvidence(term, term.Definition)
	default:
		return fmt.Errorf("term %q has no source", term.Key)
	}
}

// checkDefinitionEvidence validates the structural shape of user definition
// evidence: the definition names this term's short form, supplies this
// term's full form, cites a source message, and carries a non-empty span
// whose byte length covers exactly the full form bytes.
func checkDefinitionEvidence(term *types.AbbreviationTerm, def *types.AbbreviationDefinition) error {
	if def == nil {
		return fmt.Errorf("user term lacks definition evidence")
	}
	if termKey(def.ShortForm) != term.Key {
		return fmt.Errorf("definition short form %q does not match term %q", def.ShortForm, term.Key)
	}
	if def.FullForm == "" || def.FullForm != term.FullForm {
		return fmt.Errorf("definition full form does not match term full form")
	}
	if def.SourceMessageID == "" {
		return fmt.Errorf("definition lacks a source message")
	}
	if def.Start < 0 || def.End <= def.Start {
		return fmt.Errorf("definition span is empty or negative")
	}
	if def.End-def.Start != len(def.FullForm) {
		return fmt.Errorf("definition span length %d does not cover the %d full-form bytes",
			def.End-def.Start, len(def.FullForm))
	}
	return nil
}

// expectedCoverage re-derives the candidate spans of text with the same
// unchanged scanner, heuristic and per-occurrence protected boundaries the
// inspector uses, without dictionary I/O.
func expectedCoverage(text string) map[string][]types.AbbreviationOccurrence {
	groups := map[string][]types.AbbreviationOccurrence{}
	for _, span := range extractWordTokenSpans(text) {
		if !IsLikelyAbbreviation(span.token) {
			continue
		}
		if !isExpansionBoundary(text, span.start, span.end) {
			continue
		}
		key := termKey(span.token)
		groups[key] = append(groups[key], types.AbbreviationOccurrence{Start: span.start, End: span.end})
	}
	return groups
}

// validateCoverage requires exact candidate/span agreement between the
// original text and the terms: every recognized unprotected candidate
// appears exactly once in its term's spans — no duplicate keys, absent
// spans, terms for non-candidates, spans over unrelated text, partial-rune
// offsets, or overlap. Sorted global spans prove non-overlap in one pass.
func validateCoverage(text string, terms []types.AbbreviationTerm) error {
	expected := expectedCoverage(text)
	if len(terms) != len(expected) {
		return fmt.Errorf("term count %d does not cover %d candidates", len(terms), len(expected))
	}
	type located struct {
		start, end int
	}
	var all []located
	seen := map[string]struct{}{}
	for i := range terms {
		term := &terms[i]
		if term.Key == "" {
			return fmt.Errorf("term %d has an empty key", i)
		}
		if _, dup := seen[term.Key]; dup {
			return fmt.Errorf("duplicate term key %q", term.Key)
		}
		seen[term.Key] = struct{}{}
		want, ok := expected[term.Key]
		if !ok {
			return fmt.Errorf("term %q names no candidate of the original query", term.Key)
		}
		if len(term.Occurrences) != len(want) {
			return fmt.Errorf("term %q spans %d occurrences, candidate has %d",
				term.Key, len(term.Occurrences), len(want))
		}
		for j := range want {
			got := term.Occurrences[j]
			if got != want[j] {
				return fmt.Errorf("term %q occurrence %d is %+v, candidate span is %+v",
					term.Key, j, got, want[j])
			}
			if got.Start < 0 || got.End > len(text) || got.End <= got.Start {
				return fmt.Errorf("term %q occurrence %+v is out of bounds", term.Key, got)
			}
			slice := text[got.Start:got.End]
			if !utf8.ValidString(slice) {
				return fmt.Errorf("term %q occurrence %+v splits a UTF-8 rune", term.Key, got)
			}
			if termKey(slice) != term.Key {
				return fmt.Errorf("term %q occurrence %+v covers unrelated text %q",
					term.Key, got, slice)
			}
			all = append(all, located{got.Start, got.End})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].start < all[j].start })
	for i := 1; i < len(all); i++ {
		if all[i].start < all[i-1].end {
			return fmt.Errorf("occurrences [%d,%d) and [%d,%d) overlap",
				all[i-1].start, all[i-1].end, all[i].start, all[i].end)
		}
	}
	return nil
}

// finalizeResolution recomputes status, error code and effective query.
// Budgets are re-derived from content on every call, so no path can wash an
// over-budget input ready. A would-be-ready resolution that still fails to
// render receives invalid_resolution, never a blank error code.
func finalizeResolution(r *types.AbbreviationResolution) {
	switch {
	case overBudget(*r):
		r.Status = types.AbbreviationStatusBlockedError
		r.ErrorCode = types.AbbreviationErrorCandidateBudgetExceeded
		r.EffectiveQuery = ""
	case len(r.UnknownTerms) > 0:
		r.Status = types.AbbreviationStatusNeedsDefinition
		r.ErrorCode = ""
		r.EffectiveQuery = ""
	default:
		ambiguous := false
		for i := range r.Terms {
			if r.Terms[i].Source == "" {
				ambiguous = true
				break
			}
		}
		if ambiguous {
			r.Status = types.AbbreviationStatusBlockedError
			r.ErrorCode = types.AbbreviationErrorSelectionRequired
			r.EffectiveQuery = ""
			return
		}
		r.Status = types.AbbreviationStatusReady
		r.ErrorCode = ""
		if rendered, err := RenderResolvedQuery(*r); err == nil {
			r.EffectiveQuery = rendered
		} else {
			r.Status = types.AbbreviationStatusBlockedError
			r.ErrorCode = types.AbbreviationErrorInvalidResolution
			r.EffectiveQuery = ""
		}
	}
}

// cloneResolution deep-copies a resolution so apply paths never alias the
// caller's slices or stored pointers.
func cloneResolution(r types.AbbreviationResolution) types.AbbreviationResolution {
	out := r
	out.Terms = make([]types.AbbreviationTerm, len(r.Terms))
	for i, term := range r.Terms {
		cp := term
		cp.Occurrences = append([]types.AbbreviationOccurrence(nil), term.Occurrences...)
		cp.Meanings = append([]types.AbbreviationMeaning(nil), term.Meanings...)
		if term.Definition != nil {
			def := *term.Definition
			cp.Definition = &def
		}
		out.Terms[i] = cp
	}
	out.UnknownTerms = append([]string(nil), r.UnknownTerms...)
	return out
}
