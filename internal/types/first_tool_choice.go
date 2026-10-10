package types

import "context"

type firstToolChoiceKey struct{}

// WithFirstToolChoice returns ctx asking the agent to start the turn with
// the named tool: its first model round must call it (thinking off for that
// round). A later round chooses freely. "" leaves ctx unchanged.
func WithFirstToolChoice(ctx context.Context, tool string) context.Context {
	if tool == "" {
		return ctx
	}
	return context.WithValue(ctx, firstToolChoiceKey{}, tool)
}

// FirstToolChoiceFromContext is the tool WithFirstToolChoice asked for, "".
func FirstToolChoiceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	tool, _ := ctx.Value(firstToolChoiceKey{}).(string)
	return tool
}
