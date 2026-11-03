package database

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Unit tests run on SQLite, which has no chk_usage_orders_kind, so a new
// order kind can pass every service test and still fail on Postgres with a
// CHECK violation (seats orders did: HTTP 500 on dev). This reads the last
// CHECK the versioned migrations put on usage_orders.kind and requires every
// UsageOrderKind* constant in internal/types to be allowed by it.
func TestUsageOrderKindCheckAllowsEveryKind(t *testing.T) {
	root := sqliteRepoRoot(t)
	allowed := latestUsageOrderKindCheck(t, filepath.Join(root, "migrations", "versioned"))
	kinds := usageOrderKindConstants(t, filepath.Join(root, "internal", "types"))
	require.NotEmpty(t, kinds, "no UsageOrderKind* constants found in internal/types")
	for name, v := range kinds {
		require.Containsf(t, allowed, v,
			"types.%s = %q is not allowed by chk_usage_orders_kind (allowed %v): add a migration widening the constraint", name, v, allowed)
	}
}

var (
	usageOrderKindCheckRe = regexp.MustCompile(`(?is)chk_usage_orders_kind\s+CHECK\s*\(\s*kind\s+IN\s*\(([^)]*)\)`)
	sqlStringRe           = regexp.MustCompile(`'([^']*)'`)
)

// latestUsageOrderKindCheck returns the values of the highest-numbered
// .up.sql migration that adds chk_usage_orders_kind.
func latestUsageOrderKindCheck(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	require.NoError(t, err)
	sort.Strings(files) // zero-padded version prefixes sort numerically
	var allowed []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		sql := regexp.MustCompile(`--[^\n]*`).ReplaceAllString(string(raw), "")
		for _, m := range usageOrderKindCheckRe.FindAllStringSubmatch(sql, -1) {
			allowed = allowed[:0:0]
			for _, s := range sqlStringRe.FindAllStringSubmatch(m[1], -1) {
				allowed = append(allowed, s[1])
			}
		}
	}
	require.NotEmpty(t, allowed, "no chk_usage_orders_kind CHECK found in %s", dir)
	return allowed
}

// usageOrderKindConstants parses internal/types for string constants named
// UsageOrderKind*.
func usageOrderKindConstants(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NoError(t, err)
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "UsageOrderKind") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					out[name.Name] = v
				}
			}
		}
	}
	return out
}
