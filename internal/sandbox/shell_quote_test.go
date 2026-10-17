package sandbox

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellQuoteNeutralisesShellMetacharacters(t *testing.T) {
	quoted := ShellQuote("/opt/weknora/tenant/skills/sk-1/scripts/x$(id).py")

	require.Equal(t, `'/opt/weknora/tenant/skills/sk-1/scripts/x$(id).py'`, quoted,
		"single quotes are the only form that keeps $ and backticks inert in sh")
}

func TestShellQuoteEscapesEmbeddedSingleQuote(t *testing.T) {
	require.Equal(t, `'a'\''b'`, ShellQuote("a'b"))
}

func TestShellQuoteKeepsNonASCIILiteral(t *testing.T) {
	quoted := ShellQuote("/opt/skills/sk-1/kịch bản.py")

	require.Equal(t, `'/opt/skills/sk-1/kịch bản.py'`, quoted,
		"a non-ASCII path must reach the shell as itself, not as a \\u escape")
}
