package types

import "context"

type groundingRequiredKey struct{}

// WithGroundingRequired marks a turn whose answer must rest on what the
// tools retrieved this turn: the agent may not answer it from the model's
// own memory (see agent.ungroundedRound).
func WithGroundingRequired(ctx context.Context) context.Context {
	return context.WithValue(ctx, groundingRequiredKey{}, true)
}

// GroundingRequiredFromContext reports whether WithGroundingRequired marked ctx.
func GroundingRequiredFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(groundingRequiredKey{}).(bool)
	return v
}
