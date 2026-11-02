package types

import "context"

type documentAssistantTurnKey struct{}

// WithDocumentAssistantTurn marks a turn run by the built-in document
// assistant. The agent then lists the bound knowledge bases briefly unless
// the turn retrieves from them (see agent.compactKnowledgeDirectory).
func WithDocumentAssistantTurn(ctx context.Context) context.Context {
	return context.WithValue(ctx, documentAssistantTurnKey{}, true)
}

// DocumentAssistantTurnFromContext reports whether WithDocumentAssistantTurn marked ctx.
func DocumentAssistantTurnFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(documentAssistantTurnKey{}).(bool)
	return v
}
