package types

import (
	"context"
	"strings"
)

type userQueryKey struct{}

// WithUserQuery carries the request text of the turn, as the user wrote it,
// so a tool can read what was asked: whether the user agreed to a change
// ("đồng ý", "sửa luôn") or asked for several versions ("2 phương án").
// Empty text leaves ctx unchanged.
func WithUserQuery(ctx context.Context, query string) context.Context {
	if strings.TrimSpace(query) == "" {
		return ctx
	}
	return context.WithValue(ctx, userQueryKey{}, query)
}

// UserQueryFromContext is the text WithUserQuery stored, "" when none.
func UserQueryFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	q, _ := ctx.Value(userQueryKey{}).(string)
	return q
}
