package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinPromptShellGuidanceDistinguishesReadOnlySkills(t *testing.T) {
	text := formatToolGuidanceForMode([]string{"read_file", "shell_exec"}, false)
	require.Contains(t, text, "Read-only skills have no executable scripts")
	require.Contains(t, text, "executable installed or host skills")
}
