package abbreviation

import (
	"context"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// Detector classifies query tokens as abbreviations. It is a yes/no judge
// over tokens the caller proposes: it can neither add tokens nor propose
// meanings — full forms always come from the dictionary or the user.
type Detector interface {
	// Name is persisted as the resolution's Detector.
	Name() string
	// Classify reports, for each token (same order), whether text uses it as
	// an abbreviation. The result must have exactly len(tokens) entries.
	Classify(ctx context.Context, text string, tokens []string) ([]bool, error)
}

// ProposeTokens lists the tokens a Detector is asked about: distinct word
// tokens (first-appearance surface form per key) with at least one
// occurrence on a safe expansion boundary, minus stop words, pure numbers
// and tokens over the short-form budget.
func ProposeTokens(text string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, span := range extractWordTokenSpans(text) {
		key := termKey(span.token)
		if _, ok := seen[key]; ok {
			continue
		}
		if !isExpansionBoundary(text, span.start, span.end) {
			continue
		}
		seen[key] = struct{}{}
		if _, stop := viStopWords[key]; stop {
			continue
		}
		if utf8.RuneCountInString(span.token) > types.MaxAbbreviationShortFormRunes || isAllDigits(span.token) {
			continue
		}
		out = append(out, span.token)
	}
	return out
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// Detect runs d over the proposed tokens of text. A nil detector yields the
// heuristic detection; a classifier failure also falls back to the heuristic
// and returns the error so the caller can log it — chat never blocks on it.
func Detect(ctx context.Context, d Detector, text string) (types.AbbreviationDetection, error) {
	if d == nil {
		return types.AbbreviationDetection{}, nil
	}
	det := types.AbbreviationDetection{Detector: d.Name()}
	tokens := ProposeTokens(text)
	if len(tokens) == 0 {
		return det, nil
	}
	flags, err := d.Classify(ctx, text, tokens)
	if err == nil && len(flags) != len(tokens) {
		err = fmt.Errorf("detector %s returned %d verdicts for %d tokens", d.Name(), len(flags), len(tokens))
	}
	if err != nil {
		return types.AbbreviationDetection{}, err
	}
	for i, flagged := range flags {
		if flagged {
			det.Keys = append(det.Keys, termKey(tokens[i]))
		}
	}
	return det, nil
}

// candidateMatcher returns the per-token candidate predicate of a detection:
// the heuristic when no detector ran, otherwise membership in its key set.
func candidateMatcher(d types.AbbreviationDetection) func(token string) bool {
	if d.Detector == types.AbbreviationDetectorHeuristic {
		return IsLikelyAbbreviation
	}
	keys := make(map[string]struct{}, len(d.Keys))
	for _, k := range d.Keys {
		keys[k] = struct{}{}
	}
	return func(token string) bool {
		_, ok := keys[termKey(token)]
		return ok
	}
}

// FindDetectedCandidates is FindCandidates under a detection.
func FindDetectedCandidates(text string, d types.AbbreviationDetection) []string {
	match := candidateMatcher(d)
	var candidates []string
	for _, token := range extractWordTokens(text) {
		if match(token) {
			candidates = append(candidates, token)
		}
	}
	return candidates
}
