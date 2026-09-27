package abbreviation

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// Query rewriting can let the model paraphrase the user question, but the
// abbreviations resolved by the gate are load-bearing: a model that drops,
// rewrites or invents them silently changes what gets searched. ProtectedQuery
// wraps the original question with per-occurrence opaque markers so the model
// only sees placeholders; Restore verifies each issued marker survived exactly
// once and substitutes the validated `FullForm (ShortForm)` replacement back.
// Any violation falls back to the sealed EffectiveQuery — never the raw input.

const (
	rewriteMarkerPrefix = "⟦ABBR-"
	rewriteMarkerSuffix = "⟧"
	maxProtectAttempts  = 5
)

var foreignMarkerPattern = regexp.MustCompile(
	regexp.QuoteMeta(rewriteMarkerPrefix) + `[^⟧]*` + regexp.QuoteMeta(rewriteMarkerSuffix))

// ProtectedQuery is an opaque handle: the replacement map is private so no
// caller can forge markers or inject a different expansion.
type ProtectedQuery struct {
	text         string
	replacements map[string]string
	fallback     string
}

// ProtectRewrite replaces every resolved occurrence in the resolution's
// OriginalQuery with a unique per-call marker. It only accepts internally
// complete ready resolutions (same validation as BindTurn).
func ProtectRewrite(r types.AbbreviationResolution) (ProtectedQuery, error) {
	if err := validateReadyResolution(r); err != nil {
		return ProtectedQuery{}, err
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
		return spans[i].start > spans[j].start
	})
	for attempt := 0; attempt < maxProtectAttempts; attempt++ {
		ns := uuid.NewString()
		// The namespace must not collide with text the user actually typed,
		// otherwise a literal in the query could be mistaken for a marker.
		if strings.Contains(r.OriginalQuery, rewriteMarkerPrefix+ns) {
			continue
		}
		text := r.OriginalQuery
		replacements := make(map[string]string, len(spans))
		for i, s := range spans {
			marker := rewriteMarkerPrefix + ns + "-" + strconv.Itoa(i) + rewriteMarkerSuffix
			text = text[:s.start] + marker + text[s.end:]
			replacements[marker] = s.repl
		}
		return ProtectedQuery{text: text, replacements: replacements, fallback: r.EffectiveQuery}, nil
	}
	return ProtectedQuery{}, types.ErrAbbreviationNotReady
}

// Text returns the query with protected markers, safe to hand to the model.
func (p ProtectedQuery) Text() string {
	return p.text
}

// Restore maps the model rewrite back to plain text. It requires every issued
// marker to appear exactly once and no foreign marker tokens. On any violation
// it returns the validated effective query and false; callers must not fall
// back to the raw unresolved input.
func (p ProtectedQuery) Restore(modelRewrite string) (string, bool) {
	for marker := range p.replacements {
		if strings.Count(modelRewrite, marker) != 1 {
			return p.fallback, false
		}
	}
	for _, m := range foreignMarkerPattern.FindAllString(modelRewrite, -1) {
		if _, ok := p.replacements[m]; !ok {
			return p.fallback, false
		}
	}
	out := modelRewrite
	for marker, repl := range p.replacements {
		out = strings.Replace(out, marker, repl, 1)
	}
	return out, true
}
