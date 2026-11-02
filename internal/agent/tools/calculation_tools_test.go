package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var setsAnswerBlockRe = regexp.MustCompile(`types\.ToolDataAnswerBlock\s*:`)

// Every tool file that sets types.ToolDataAnswerBlock names its tool in
// calculationTools.
func TestCalculationToolsListAnswerBlockTools(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	nameRe := regexp.MustCompile(`(?m)^const (Tool\w+) = `)
	known := map[string]string{"ToolCalculateHouseholdTax": ToolCalculateHouseholdTax, "ToolCompareTaxMethods": ToolCompareTaxMethods}
	found := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		if !setsAnswerBlockRe.MatchString(string(src)) {
			continue
		}
		m := nameRe.FindStringSubmatch(string(src))
		require.NotNil(t, m, "%s sets an answer block but declares no Tool… name", f)
		name, ok := known[m[1]]
		require.True(t, ok, "%s: add %s to calculationTools (and to this test)", f, m[1])
		require.True(t, IsCalculationTool(name), name)
		found++
	}
	require.GreaterOrEqual(t, found, 2)
	require.False(t, IsCalculationTool("search_knowledge"))
}
