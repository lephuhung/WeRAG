package mcpserver

import (
	"context"
	"strings"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

type cappedGuard struct{ err error }

func (g cappedGuard) CheckTurn(context.Context, uint64, string, string, types.SessionCapMode) (*types.UsageTurnGrant, error) {
	return nil, g.err
}
func (g cappedGuard) Entitlement(context.Context, uint64, string) (*types.UsageEntitlement, error) {
	return nil, nil
}
func (g cappedGuard) Enforced(context.Context) bool { return true }
func (g cappedGuard) PreviewSession(
	context.Context, uint64, string, string, types.SessionCapMode,
) (*types.UsageSessionPreview, error) {
	return &types.UsageSessionPreview{}, nil
}

// A used-up session tells the MCP client to start a new one: there is no
// button to press, so the error says to call ask without session_id.
func TestAskSessionLimitTellsClientToStartANewSession(t *testing.T) {
	denial := apperrors.NewTooManyRequestsError("session limit reached: 15 questions per session").
		WithDetails(types.UsageDenial{Reason: types.UsageDenialSessionLimit, SessionID: "s1"})
	s := &Server{usageGuard: cappedGuard{err: denial}}
	_, _, err := s.runQA(context.Background(), &types.Session{ID: "s1", TenantID: 1}, nil, "q", nil)
	if err == nil || !strings.Contains(err.Error(), "without session_id") {
		t.Fatalf("want a new-session hint, got %v", err)
	}

	other := apperrors.NewTooManyRequestsError("usage limit reached").
		WithDetails(types.UsageDenial{Reason: types.UsageDenialLimit})
	s = &Server{usageGuard: cappedGuard{err: other}}
	_, _, err = s.runQA(context.Background(), &types.Session{ID: "s1", TenantID: 1}, nil, "q", nil)
	if err == nil || strings.Contains(err.Error(), "session_id") {
		t.Fatalf("other refusals keep their own message, got %v", err)
	}
}
