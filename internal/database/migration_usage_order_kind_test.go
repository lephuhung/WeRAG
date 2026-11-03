package database

import (
	"fmt"
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

// The test reads the CHECK in this form only (whitespace and case free,
// "--" comments ignored):
//
//	ALTER TABLE usage_orders ADD CONSTRAINT chk_usage_orders_kind
//	    CHECK (kind IN ('plan', 'addon', 'seats'));
//
// optionally preceded by DROP CONSTRAINT [IF EXISTS] chk_usage_orders_kind.
// The newest *.up.sql that mentions chk_usage_orders_kind decides: if it
// cannot be parsed in that form, or drops the constraint without adding it
// back afterwards, the test fails rather than falling back to an older
// migration (write the new constraint in the form above).
var (
	usageOrderKindMentionRe = regexp.MustCompile(`(?i)chk_usage_orders_kind`)
	usageOrderKindAddRe     = regexp.MustCompile(`(?is)ADD\s+CONSTRAINT\s+chk_usage_orders_kind\s+CHECK\s*\(\s*kind\s+IN\s*\(([^)]*)\)\s*\)`)
	usageOrderKindDropRe    = regexp.MustCompile(`(?is)DROP\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?chk_usage_orders_kind\b`)
	sqlStringRe             = regexp.MustCompile(`'([^']*)'`)
	sqlLineCommentRe        = regexp.MustCompile(`--[^\n]*`)
)

// latestUsageOrderKindCheck returns the values allowed by the newest
// .up.sql migration that mentions chk_usage_orders_kind.
func latestUsageOrderKindCheck(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	require.NoError(t, err)
	sort.Strings(files) // zero-padded version prefixes sort numerically
	var newest, sql string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		body := sqlLineCommentRe.ReplaceAllString(string(raw), "")
		if usageOrderKindMentionRe.MatchString(body) {
			newest, sql = f, body
		}
	}
	require.NotEmpty(t, newest, "no migration in %s mentions chk_usage_orders_kind", dir)
	allowed, err := parseUsageOrderKindCheck(filepath.Base(newest), sql)
	require.NoError(t, err)
	return allowed
}

// parseUsageOrderKindCheck reads the allowed kinds from one migration's
// SQL (comments already removed), refusing any other form.
func parseUsageOrderKindCheck(name, sql string) ([]string, error) {
	adds := usageOrderKindAddRe.FindAllStringSubmatchIndex(sql, -1)
	if len(adds) == 0 {
		return nil, fmt.Errorf("%s mentions chk_usage_orders_kind but has no "+
			"ADD CONSTRAINT chk_usage_orders_kind CHECK (kind IN (...)) the test can read", name)
	}
	last := adds[len(adds)-1]
	for _, d := range usageOrderKindDropRe.FindAllStringIndex(sql, -1) {
		if d[0] > last[0] {
			return nil, fmt.Errorf("%s drops chk_usage_orders_kind without adding it back afterwards", name)
		}
	}
	var allowed []string
	for _, v := range sqlStringRe.FindAllStringSubmatch(sql[last[2]:last[3]], -1) {
		allowed = append(allowed, v[1])
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%s: chk_usage_orders_kind allows no kind", name)
	}
	return allowed, nil
}

func TestParseUsageOrderKindCheckForms(t *testing.T) {
	got, err := parseUsageOrderKindCheck("ok.up.sql", "ALTER TABLE usage_orders DROP CONSTRAINT IF EXISTS chk_usage_orders_kind;\n"+
		"ALTER TABLE usage_orders\n    ADD CONSTRAINT chk_usage_orders_kind CHECK (kind IN ('plan', 'addon', 'seats'));")
	require.NoError(t, err)
	require.Equal(t, []string{"plan", "addon", "seats"}, got)

	for name, sql := range map[string]string{
		"drop only":     "ALTER TABLE usage_orders DROP CONSTRAINT chk_usage_orders_kind;",
		"drop after":    "ALTER TABLE usage_orders ADD CONSTRAINT chk_usage_orders_kind CHECK (kind IN ('plan')); ALTER TABLE usage_orders DROP CONSTRAINT IF EXISTS chk_usage_orders_kind;",
		"other form":    "ALTER TABLE usage_orders ADD CONSTRAINT chk_usage_orders_kind CHECK (kind = ANY (ARRAY['plan']));",
		"empty IN list": "ALTER TABLE usage_orders ADD CONSTRAINT chk_usage_orders_kind CHECK (kind IN ());",
	} {
		_, err := parseUsageOrderKindCheck(name, sql)
		require.Errorf(t, err, "%s must be refused", name)
	}
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
