package domainpacks

import (
	"math"
	"strings"
	"testing"
	"testing/fstest"

	domainpackassets "github.com/Tencent/WeKnora/domain-packs"
	"github.com/Tencent/WeKnora/internal/agent/domaintools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every formula a shipped pack describes is implemented and reproduces its
// examples (money ±1 đ, ratios ±1e-9); every implementation is described.
func TestShippedFormulasAreImplementedAndExamplesHold(t *testing.T) {
	reg, err := Load(domainpackassets.FS)
	require.NoError(t, err)
	described := map[string]bool{}
	for _, name := range reg.Names() {
		pack, _ := reg.Get(name)
		for _, fm := range pack.Formulas() {
			described[fm.ID] = true
			impl, ok := domaintools.Lookup(fm.ID)
			require.True(t, ok, "%s: formula %s has no implementation", name, fm.ID)
			for i, ex := range fm.Examples {
				got, err := impl.Func(ex.Inputs)
				require.NoError(t, err, "%s/%s example %d", name, fm.ID, i)
				for key, want := range ex.Expected {
					v, ok := got[key]
					require.True(t, ok, "%s/%s example %d: no output %q", name, fm.ID, i, key)
					tol := 1e-9
					if math.Abs(want) >= 1 {
						tol = 1 // money: ±1 đ
					}
					assert.InDelta(t, want, v, tol, "%s/%s example %d output %s", name, fm.ID, i, key)
				}
			}
		}
	}
	for _, id := range domaintools.IDs() {
		assert.True(t, described[id], "implementation %s is described in no formulas.yaml", id)
	}
}

func TestFormulaImplementationsRefuseMissingInputs(t *testing.T) {
	for _, id := range domaintools.IDs() {
		impl, _ := domaintools.Lookup(id)
		_, err := impl.Func(map[string]float64{})
		assert.Error(t, err, id)
	}
}

const sampleFormulas = `formulas:
  - id: thue_theo_loi_nhuan
    name: Thuế kê khai
    expression: Thuế = Lợi nhuận × Thuế suất
    variables:
      - {name: loi_nhuan, description: Lợi nhuận, unit: đồng, source: input}
      - {name: thue_suat, description: Thuế suất, unit: phân số, source: kb}
    basis:
      - {number: 01/2021/NĐ-CP, article: Điều 21}
    tool: compare_tax_methods
    examples:
      - inputs: {loi_nhuan: 100, thue_suat: 0.2}
        expected: {thue_theo_loi_nhuan: 20}
`

// formulaFS is the sample pack with a domain tool and formulas.yaml.
func formulaFS(t *testing.T) fstest.MapFS {
	t.Helper()
	fsys := sampleFS(t)
	replaceInSkill(t, fsys, "required_tools: [thinking]", "required_tools: [thinking, compare_tax_methods]")
	setFile(fsys, "formulas.yaml", sampleFormulas)
	return fsys
}

func TestLoadFormulas(t *testing.T) {
	reg, err := Load(formulaFS(t))
	require.NoError(t, err)
	pack, _ := reg.Get(samplePack)
	require.Len(t, pack.Formulas(), 1)
	assert.Equal(t, "kb", pack.Formulas()[0].Variables[1].Source)
	assert.NotContains(t, pack.Files(), "formulas.yaml", "formulas are never served by read_file")
	block := reg.FormulaBlock("compare_tax_methods")
	assert.Equal(t, "Công thức áp dụng:\n- Thuế kê khai: Thuế = Lợi nhuận × Thuế suất (căn cứ: Điều 21 01/2021/NĐ-CP)\n", block)
	assert.Empty(t, reg.FormulaBlock("thinking"))
	assert.Empty(t, pack.Lint())
}

func TestLoadRejectsBrokenFormulas(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(f fstest.MapFS)
		want   string
	}{
		{"domain tool without formulas", func(f fstest.MapFS) { delete(f, samplePack+"/formulas.yaml") },
			`"compare_tax_methods" runs domain formulas but no formula describes it`},
		{"unknown field", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "    tool:", "    tools:", 1))
		}, "field tools not found"},
		{"bad id", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "id: thue_theo_loi_nhuan", "id: Thue-Loi-Nhuan", 1))
		}, "id must be snake_case"},
		{"unimplemented", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "id: thue_theo_loi_nhuan", "id: thue_moi", 1))
		}, "no Go implementation"},
		{"tool not required", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "tool: compare_tax_methods", "tool: todo_write", 1))
		}, `tool "todo_write" is not in required_tools`},
		{"basis not catalogued", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "number: 01/2021/NĐ-CP", "number: 9/2099/QH99", 1))
		}, "is not in catalogue.yaml"},
		{"bad source", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "source: kb", "source: constant", 1))
		}, `source must be "input" or "kb"`},
		{"example lacks input", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, ", thue_suat: 0.2}", "}", 1))
		}, `lacks input "thue_suat"`},
		{"no example", func(f fstest.MapFS) {
			s := sampleFormulas[:strings.Index(sampleFormulas, "    examples:")]
			setFile(f, "formulas.yaml", s)
		}, "at least one example"},
		{"empty article", func(f fstest.MapFS) {
			setFile(f, "catalogue.yaml", "documents:\n  - number: 01/2021/NĐ-CP\n    used_for: x\n    articles: [\"\"]\n")
		}, "articles[0] is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := formulaFS(t)
			tc.break_(fsys)
			_, err := Load(fsys)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCitationsAndFormulaLint(t *testing.T) {
	fsys := formulaFS(t)
	setFile(fsys, "assets/checklist.md", "Bình luận tham khảo — không phải căn cứ.\n\nXem Luật 59/2020/QH14 và Nghị định 01/2021/nđ-cp.\n")
	setFile(fsys, "formulas.yaml", strings.Replace(sampleFormulas, "× Thuế suất", "× 20 %", 1))
	reg, err := Load(fsys)
	require.NoError(t, err)
	pack, _ := reg.Get(samplePack)
	assert.Equal(t, []string{"assets/checklist.md", "references/a.md"}, pack.CitedIn("01/2021/NĐ-CP"),
		"numbers are matched by catalogue key")
	findings := strings.Join(pack.Lint(), "\n")
	assert.Contains(t, findings, "assets/checklist.md: 59/2020/QH14 is cited but not in catalogue.yaml")
	assert.Contains(t, findings, "thue_theo_loi_nhuan writes a rate as a constant")

	setFile(fsys, "references/a.md", "Bình luận tham khảo — không phải căn cứ.\n")
	setFile(fsys, "assets/checklist.md", "Bình luận tham khảo — không phải căn cứ.\n")
	reg, err = Load(fsys)
	require.NoError(t, err)
	pack, _ = reg.Get(samplePack)
	assert.Contains(t, strings.Join(pack.Lint(), "\n"), "01/2021/NĐ-CP is cited nowhere")
	assert.Empty(t, pack.CitedIn("01/2021/NĐ-CP"))
}
