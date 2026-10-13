package types

import "context"

type answerButtonGuardKey struct{}

// WithAnswerButtonGuard marks a turn whose answers may only invite the user
// to press a card button ("Thay vào văn bản", "Chèn vào văn bản", "Áp dụng
// các sửa thể thức này") when the turn produced that card. The document
// assistant kept offering buttons that did not exist (see
// agent.answerGuard).
func WithAnswerButtonGuard(ctx context.Context) context.Context {
	return context.WithValue(ctx, answerButtonGuardKey{}, true)
}

// AnswerButtonGuardFromContext reports whether WithAnswerButtonGuard marked ctx.
func AnswerButtonGuardFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(answerButtonGuardKey{}).(bool)
	return v
}
