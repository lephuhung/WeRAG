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

type turnIDKey struct{}

// WithTurnID carries the id of the turn (its assistant message), so a tool
// can tell a plan it showed in an earlier turn from one shown in this one:
// apply_format_fixes applies only a plan the user saw before answering.
// Empty id leaves ctx unchanged.
func WithTurnID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, turnIDKey{}, id)
}

// TurnIDFromContext is the id WithTurnID stored, "" when none.
func TurnIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(turnIDKey{}).(string)
	return id
}
