package tools

import (
	"context"
	"encoding/json"
	"testing"

	legalskillassets "github.com/Tencent/WeKnora/examples/skills"
	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/stretchr/testify/require"
)

func TestBuiltinLegalReadFileNeverAdvertisesShellOrSandbox(t *testing.T) {
	builtins, err := skills.NewBuiltinSource(legalskillassets.FS)
	require.NoError(t, err)
	mgr := skills.NewManager(&skills.ManagerConfig{Enabled: true}, nil).WithBuiltinSource(builtins)
	require.NoError(t, mgr.Initialize(context.Background()))
	for _, shell := range []bool{false, true} {
		t.Run(map[bool]string{true: "shell-present", false: "no-shell"}[shell], func(t *testing.T) {
			reader := NewReadFileTool(nil).WithSkills(mgr, shell)
			result, err := reader.Execute(context.Background(), json.RawMessage(`{"path":"skill://legal-document-summary/SKILL.md"}`))
			require.NoError(t, err)
			require.True(t, result.Success, result.Error)
			require.Contains(t, result.Output, "Read-only")
			require.NotContains(t, result.Output, `shell_exec(skill_name=`)
			require.NotContains(t, result.Output, "configuring a shell-capable sandbox")
			for _, address := range []string{"skill://legal-document-summary/../other/SKILL.md", "skill://unknown/SKILL.md", "skill://legal-document-summary/scripts/run.sh"} {
				args, err := json.Marshal(ReadFileInput{Path: address})
				require.NoError(t, err)
				bad, err := reader.Execute(context.Background(), args)
				require.NoError(t, err)
				require.False(t, bad.Success, address)
			}
		})
	}
}
