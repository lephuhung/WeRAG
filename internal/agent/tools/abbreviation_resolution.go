package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

// Abbreviation gate error codes carried by blocked ToolResults, both as an
// Error prefix and in Data["error_code"]. They are the only way a direct
// tool reports that the abbreviation policy refused the query.
const (
	AbbreviationCodeDefinitionRequired = "abbreviation_definition_required"
	AbbreviationCodeSelectionRequired  = "abbreviation_selection_required"
	AbbreviationCodeDictionaryError    = "abbreviation_dictionary_error"
	AbbreviationCodeBlocked            = "abbreviation_blocked"
)

// ResolveToolQuery enforces the abbreviation-resolution gate on a tool query.
//
// Bound QA turn (a sealed resolution lives in ctx): the validated mapping is
// applied to the query with no dictionary I/O — the model's selection and
// literal flags cannot override the sealed mapping, and candidates that were
// never part of the user's own request stay literal and teach nothing.
//
// Standalone call (no sealed turn, e.g. the MCP endpoint): the query is
// inspected against the active dictionary. Unknown candidates block with
// abbreviation_definition_required and no retrieval may run; ambiguous terms
// resolve only through `selection` (validated against the resolver-provided
// meaning IDs) or explicit `literal` mode, which leaves known multi-meaning
// terms unexpanded and never bypasses unknowns.
//
// A non-nil *types.ToolResult return means the caller MUST surface it
// instead of executing the tool.
func ResolveToolQuery(
	ctx context.Context,
	query string,
	svc interfaces.AbbreviationService,
	selection map[string]string,
	literal bool,
) (string, *types.ToolResult, error) {
	if sealed, ok := abbreviation.ResolutionFromContext(ctx); ok {
		return applySealedAbbreviationMapping(query, &sealed), nil, nil
	}
	if len(abbreviation.FindCandidates(query)) == 0 {
		return query, nil, nil
	}
	if svc == nil {
		return "", blockedAbbreviationResult(AbbreviationCodeDictionaryError,
				"the abbreviation dictionary is not available",
				types.AbbreviationResolution{}), fmt.Errorf("%s: dictionary unavailable",
				AbbreviationCodeDictionaryError)
	}
	actives, err := svc.ListActive(ctx)
	if err != nil {
		return "", blockedAbbreviationResult(AbbreviationCodeDictionaryError,
				fmt.Sprintf("the abbreviation dictionary failed: %v", err),
				types.AbbreviationResolution{}), fmt.Errorf("%s: %v",
				AbbreviationCodeDictionaryError, err)
	}
	res := abbreviation.Inspect(query, actives)
	switch {
	case res.Status == types.AbbreviationStatusReady:
		out, err := abbreviation.RenderResolvedQuery(res)
		if err != nil {
			return "", blockedAbbreviationResult(AbbreviationCodeBlocked,
				"the resolved query failed to render", res), err
		}
		return out, nil, nil
	case len(res.UnknownTerms) > 0:
		msg := "the query contains abbreviations with no dictionary meaning: " +
			strings.Join(res.UnknownTerms, ", ") +
			"; ask the user for their full forms instead of guessing"
		return "", blockedAbbreviationResult(AbbreviationCodeDefinitionRequired, msg, res),
			fmt.Errorf("%s: %s", AbbreviationCodeDefinitionRequired,
				strings.Join(res.UnknownTerms, ", "))
	case res.ErrorCode == types.AbbreviationErrorSelectionRequired:
		return resolveAmbiguousToolQuery(query, res, selection, literal)
	default:
		msg := "abbreviation resolution is blocked"
		if res.ErrorCode != "" {
			msg += ": " + res.ErrorCode
		}
		return "", blockedAbbreviationResult(AbbreviationCodeBlocked, msg, res),
			fmt.Errorf("%s: %s", AbbreviationCodeBlocked, res.ErrorCode)
	}
}

// resolveAmbiguousToolQuery handles a standalone query whose terms all have
// dictionary meanings but at least one term offers several. Literal mode
// renders only the unambiguous terms; selection must name a listed active
// meaning ID for every ambiguous term.
func resolveAmbiguousToolQuery(
	query string,
	res types.AbbreviationResolution,
	selection map[string]string,
	literal bool,
) (string, *types.ToolResult, error) {
	if literal {
		return renderResolvedAbbreviationTerms(query, res.Terms), nil, nil
	}
	if len(selection) > 0 {
		applied, applyErr := abbreviation.ApplySelections(res, selection)
		if applyErr == nil {
			out, renderErr := abbreviation.RenderResolvedQuery(applied)
			if renderErr == nil {
				return out, nil, nil
			}
			applyErr = renderErr
		}
		msg := fmt.Sprintf("abbreviation_meaning_ids were rejected: %v", applyErr)
		return "", blockedAbbreviationResult(AbbreviationCodeSelectionRequired, msg, res),
			fmt.Errorf("%s: %v", AbbreviationCodeSelectionRequired, applyErr)
	}
	var shorts []string
	for i := range res.Terms {
		if res.Terms[i].Source == "" {
			shorts = append(shorts, res.Terms[i].ShortForm)
		}
	}
	msg := "these abbreviations have multiple active meanings; supply " +
		"abbreviation_meaning_ids for each or set literal_abbreviations: " +
		strings.Join(shorts, ", ")
	return "", blockedAbbreviationResult(AbbreviationCodeSelectionRequired, msg, res),
		fmt.Errorf("%s: %s", AbbreviationCodeSelectionRequired, strings.Join(shorts, ", "))
}

// blockedAbbreviationResult builds the ToolResult a caller returns without
// executing the tool. The public projection carries terms and unknowns but
// none of the private evidence (definition spans, message IDs, owner scope).
func blockedAbbreviationResult(
	code, msg string, res types.AbbreviationResolution,
) *types.ToolResult {
	data := map[string]interface{}{"error_code": code}
	if res.OriginalQuery != "" || len(res.Terms) > 0 || len(res.UnknownTerms) > 0 {
		data["abbreviation_resolution"] = res.PublicState()
	}
	return &types.ToolResult{Success: false, Error: code + ": " + msg, Data: data}
}

// applySealedAbbreviationMapping renders the tool query with the validated
// turn mapping. Candidates unknown to the sealed resolution stay literal:
// they are model-produced, never user intent confirmations, and teach the
// dictionary nothing.
func applySealedAbbreviationMapping(query string, res *types.AbbreviationResolution) string {
	actives := sealedAbbreviationActives(res)
	if len(actives) == 0 {
		return query
	}
	inspected := abbreviation.Inspect(query, actives)
	return renderResolvedAbbreviationTerms(query, inspected.Terms)
}

// sealedAbbreviationActives projects the validated term mapping into
// single-meaning rows for Inspect. The synthetic IDs keep each term
// unambiguous; they never touch the real dictionary.
func sealedAbbreviationActives(res *types.AbbreviationResolution) []*types.Abbreviation {
	if res == nil {
		return nil
	}
	out := make([]*types.Abbreviation, 0, len(res.Terms))
	for i := range res.Terms {
		term := &res.Terms[i]
		if term.FullForm == "" || term.Source == "" {
			continue
		}
		out = append(out, &types.Abbreviation{
			ID:        "sealed-" + term.Key,
			ShortForm: term.ShortForm,
			FullForm:  term.FullForm,
			IsActive:  true,
		})
	}
	return out
}

// renderResolvedAbbreviationTerms splices `Full (SHORT)` for terms that carry
// a resolved full form and leaves unresolved terms literal. An occurrence
// already written as `Full (SHORT)` inside the model's argument is skipped so
// a pre-expanded argument cannot produce `Full (Full (SHORT))`.
func renderResolvedAbbreviationTerms(query string, terms []types.AbbreviationTerm) string {
	type span struct {
		start, end int
		repl       string
	}
	var spans []span
	for i := range terms {
		term := &terms[i]
		if term.FullForm == "" || term.Source == "" {
			continue
		}
		marker := term.FullForm + " ("
		for _, occ := range term.Occurrences {
			if occ.Start < 0 || occ.End > len(query) || occ.End <= occ.Start {
				continue
			}
			prefix := query[:occ.Start]
			if len(prefix) >= len(marker) &&
				strings.EqualFold(prefix[len(prefix)-len(marker):], marker) &&
				occ.End < len(query) && query[occ.End] == ')' {
				continue
			}
			spans = append(spans, span{occ.Start, occ.End,
				term.FullForm + " (" + term.ShortForm + ")"})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	out := query
	for _, s := range spans {
		out = out[:s.start] + s.repl + out[s.end:]
	}
	return out
}

// sealedUserDefinitionMatches reports whether short/full reproduce a
// user-supplied definition sealed into this turn. Normalization mirrors the
// definition parser's: both sides compare case-insensitively after trim, and
// the definition must carry its source message so model arguments alone can
// never be authoritative user intent.
func sealedUserDefinitionMatches(res *types.AbbreviationResolution, short, full string) bool {
	if res == nil {
		return false
	}
	short = strings.TrimSpace(short)
	full = strings.TrimSpace(full)
	for i := range res.Terms {
		term := &res.Terms[i]
		def := term.Definition
		if term.Source != types.AbbreviationSourceUserCurrent || def == nil || def.SourceMessageID == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(term.ShortForm), short) &&
			strings.EqualFold(strings.TrimSpace(def.FullForm), full) {
			return true
		}
	}
	return false
}
