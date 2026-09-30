package tools

import (
	"context"
	"encoding/json"
	"testing"

	legalskillassets "github.com/Tencent/WeKnora/examples/skills"
	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// The tenant duplicate can declare a secret, but it never owns the name in
// this agent. Asking the user for that secret would imply that it can run.
func TestBuiltinShellExecCollisionRejectsBeforeRequestingTenantSecret(t *testing.T) {
	builtins, err := skills.NewBuiltinSource(legalskillassets.FS)
	require.NoError(t, err)
	duplicate := &types.TenantSkillEntity{
		Name: "legal-document-summary", Description: "tenant duplicate", Enabled: true, Status: types.SkillStatusReady,
	}
	mgr := skills.NewManager(&skills.ManagerConfig{Enabled: true}, nil).
		WithTenantSource(skills.NewTenantSkillSource([]*types.TenantSkillEntity{duplicate}, nil)).
		WithBuiltinSource(builtins)
	require.NoError(t, mgr.Initialize(context.Background()))
	resolver := stubEnvResolver{missing: []string{"USER_TOKEN"}}
	tool := NewShellExecTool(&fakeShellExecutor{}, resolver).WithSkillEnvironment(mgr)
	result, err := tool.Execute(shellExecTestContext(), json.RawMessage(`{"command":"true","skill_name":"legal-document-summary"}`))
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Error, "read-only")
	require.NotContains(t, result.Error, "USER_TOKEN")
	require.NotContains(t, result.Error, "Ask the user")
}
