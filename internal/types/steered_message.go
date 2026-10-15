package types

import "context"

// steeredMessageContextKey marks the creation of a user row that was
// injected into a running turn (mid-run steering) rather than opening one.
const steeredMessageContextKey ContextKey = "SteeredMessage"

// WithSteeredMessage returns ctx for persisting a steered user row. Such a
// row shares its turn's request ID and is otherwise indistinguishable from
// the row that opened the turn, so the writer says so here.
func WithSteeredMessage(ctx context.Context) context.Context {
	return context.WithValue(ctx, steeredMessageContextKey, true)
}

// IsSteeredMessage reports a ctx made by WithSteeredMessage.
func IsSteeredMessage(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(steeredMessageContextKey).(bool)
	return v
}
