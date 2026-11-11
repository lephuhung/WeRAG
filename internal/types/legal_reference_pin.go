package types

import "context"

// LegalReferencePin is a bare legal reference ("Nghị định 53") the user
// resolved earlier in the same conversation by picking one document from
// the candidates card: the turn reads it as that document instead of
// showing the card again.
type LegalReferencePin struct {
	// Reference is the reference as written in the current question.
	Reference string
	// DocumentNumber is the full số hiệu picked ("53/2022/NĐ-CP").
	DocumentNumber string
	// DocType is the catalogue doc_type ("Nghị định").
	DocType string
	// Title is the picked document's title, "" when unknown.
	Title string
}

type legalReferencePinKey struct{}

// WithLegalReferencePins attaches the turn's pinned references.
func WithLegalReferencePins(ctx context.Context, pins []LegalReferencePin) context.Context {
	if len(pins) == 0 {
		return ctx
	}
	return context.WithValue(ctx, legalReferencePinKey{}, append([]LegalReferencePin(nil), pins...))
}

// LegalReferencePinsFromContext returns the turn's pinned references.
func LegalReferencePinsFromContext(ctx context.Context) []LegalReferencePin {
	if ctx == nil {
		return nil
	}
	pins, _ := ctx.Value(legalReferencePinKey{}).([]LegalReferencePin)
	return append([]LegalReferencePin(nil), pins...)
}
