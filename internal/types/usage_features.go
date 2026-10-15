package types

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

// Plan features (usage_plans.features): per-turn and per-session caps that
// bound what one question or one conversation may cost, next to the
// day/month limits. The keys are flat, dotted names so a migration can merge
// and remove them with jsonb || and -. A key that is absent or 0 leaves the
// current behaviour (no cap); keys this release does not know are kept as
// they are.
const (
	// UsageFeatureTurnTokenCap caps the summed prompt+completion tokens of
	// one agent turn. Crossing it ends the loop with a synthesized answer.
	UsageFeatureTurnTokenCap = "agent.turn_token_cap"
	// UsageFeatureMaxIterations caps the agent's ReAct rounds; the smaller
	// of it and the agent's own setting applies.
	UsageFeatureMaxIterations = "agent.max_iterations"
	// UsageFeatureHistoryTokens is how much stored history a turn may carry
	// before older turns are summarized (clamped, see EffectiveHistoryTokens).
	UsageFeatureHistoryTokens = "context.history_tokens"
	// UsageFeatureSessionMaxTurns caps the questions of one session.
	UsageFeatureSessionMaxTurns = "session.max_turns"
	// UsageFeatureSessionMaxTokens caps the tokens of one session.
	UsageFeatureSessionMaxTokens = "session.max_tokens"
)

// MinHistoryTokens is the floor of a plan's history budget: below it a
// summary plus the newest turn no longer fit, and every turn would compact.
const MinHistoryTokens = 32768

// UsagePlanFeatures is the typed view of the feature keys above. Zero means
// unlimited for every field.
type UsagePlanFeatures struct {
	TurnTokenCap     int64 `json:"agent.turn_token_cap,omitempty"`
	MaxIterations    int64 `json:"agent.max_iterations,omitempty"`
	HistoryTokens    int64 `json:"context.history_tokens,omitempty"`
	SessionMaxTurns  int64 `json:"session.max_turns,omitempty"`
	SessionMaxTokens int64 `json:"session.max_tokens,omitempty"`
}

// usageFeatureFields maps each known key to its field, for parsing.
func (f *UsagePlanFeatures) fields() map[string]*int64 {
	return map[string]*int64{
		UsageFeatureTurnTokenCap:     &f.TurnTokenCap,
		UsageFeatureMaxIterations:    &f.MaxIterations,
		UsageFeatureHistoryTokens:    &f.HistoryTokens,
		UsageFeatureSessionMaxTurns:  &f.SessionMaxTurns,
		UsageFeatureSessionMaxTokens: &f.SessionMaxTokens,
	}
}

// ParseUsagePlanFeatures reads the known keys of a plan's features object.
// Empty or null input is no features. A known key must be a non-negative
// whole number; unknown keys are ignored here and kept in storage.
func ParseUsagePlanFeatures(raw JSON) (UsagePlanFeatures, error) {
	var out UsagePlanFeatures
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return out, fmt.Errorf("features must be a JSON object: %w", err)
	}
	for key, dst := range out.fields() {
		v, ok := obj[key]
		if !ok || string(v) == "null" {
			continue
		}
		var n float64
		if err := json.Unmarshal(v, &n); err != nil {
			return UsagePlanFeatures{}, fmt.Errorf("features.%s must be a number", key)
		}
		if n < 0 || n != math.Trunc(n) || n > math.MaxInt64/2 {
			return UsagePlanFeatures{}, fmt.Errorf("features.%s must be a non-negative whole number", key)
		}
		*dst = int64(n)
	}
	return out, nil
}

// IsZero reports a plan without any cap.
func (f UsagePlanFeatures) IsZero() bool {
	return f == UsagePlanFeatures{}
}

// EffectiveMaxIterations combines the plan's round cap with the agent's
// own: the smaller wins, and an unlimited (negative) or unset (0) agent
// value takes the plan's.
func (f UsagePlanFeatures) EffectiveMaxIterations(agentMax int) int {
	if f.MaxIterations <= 0 {
		return agentMax
	}
	planMax := int(f.MaxIterations)
	if agentMax <= 0 || agentMax > planMax {
		return planMax
	}
	return agentMax
}

// EffectiveHistoryTokens is the history budget of one turn on a model with
// the given window: the plan's value clamped to [MinHistoryTokens, window],
// or the window itself when the plan sets none.
func (f UsagePlanFeatures) EffectiveHistoryTokens(window int) int {
	if f.HistoryTokens <= 0 {
		return window
	}
	budget := int(min(f.HistoryTokens, math.MaxInt32))
	budget = max(budget, MinHistoryTokens)
	if window > 0 && budget > window {
		budget = window
	}
	return budget
}

// UsageTurnGrant is what the guard lets a turn through with: the caps the
// chat paths apply to it. A nil grant, or one with zero features, applies
// no cap (enforcement off, SuperAdmin, or a plan without features).
type UsageTurnGrant struct {
	Features UsagePlanFeatures `json:"features"`
}

// usageTurnGrantContextKey carries the turn's usage grant to the chat paths.
const usageTurnGrantContextKey ContextKey = "UsageTurnGrant"

// WithUsageTurnGrant returns ctx carrying grant for the chat paths. Nil
// leaves ctx unchanged.
func WithUsageTurnGrant(ctx context.Context, grant *UsageTurnGrant) context.Context {
	if grant == nil {
		return ctx
	}
	return context.WithValue(ctx, usageTurnGrantContextKey, grant)
}

// UsageTurnGrantFromContext is the grant WithUsageTurnGrant stored, or nil.
func UsageTurnGrantFromContext(ctx context.Context) *UsageTurnGrant {
	if ctx == nil {
		return nil
	}
	grant, _ := ctx.Value(usageTurnGrantContextKey).(*UsageTurnGrant)
	return grant
}

// UsageFeaturesFromContext is the plan features of the turn's grant, zero
// when there is none.
func UsageFeaturesFromContext(ctx context.Context) UsagePlanFeatures {
	if grant := UsageTurnGrantFromContext(ctx); grant != nil {
		return grant.Features
	}
	return UsagePlanFeatures{}
}
