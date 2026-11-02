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
// examples (ExampleValueMatches); every implementation is described.
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
					assert.True(t, ExampleValueMatches(want, v), "%s/%s example %d output %s: %v, not %v", name, fm.ID, i, key, v, want)
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
		{"amount written in the expression", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "Thuế = Lợi nhuận × Thuế suất",
				"Thuế = (Lợi nhuận − 100.000.000) × Thuế suất", 1))
		}, "the expression writes the number(s) 100.000.000"},
		{"rate written in the name", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "name: Thuế kê khai", "name: Thuế kê khai 0,015", 1))
		}, "the name writes the number(s) 0,015"},
		{"rate written as a percent", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "× Thuế suất", "× 100 %", 1))
		}, "the expression writes the number(s) 100 %"},
		{"article not in the catalogue", func(f fstest.MapFS) {
			setFile(f, "formulas.yaml", strings.Replace(sampleFormulas, "article: Điều 21", "article: Điều 22", 1))
		}, "basis[0] Điều 22 is not among the articles of 01/2021/NĐ-CP in catalogue.yaml (listed: Điều 21)"},
		{"article of a document listing none", func(f fstest.MapFS) {
			setFile(f, "catalogue.yaml", "documents:\n  - number: 01/2021/NĐ-CP\n    used_for: x\n")
		}, "(listed: none)"},
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
	reg, err := Load(fsys)
	require.NoError(t, err)
	pack, _ := reg.Get(samplePack)
	assert.Equal(t, []string{"assets/checklist.md", "references/a.md"}, pack.CitedIn("01/2021/NĐ-CP"),
		"numbers are matched by catalogue key")
	findings := strings.Join(pack.Lint(), "\n")
	assert.Contains(t, findings, "assets/checklist.md: 59/2020/QH14 is cited but not in catalogue.yaml")

	setFile(fsys, "references/a.md", "Bình luận tham khảo — không phải căn cứ.\n")
	setFile(fsys, "assets/checklist.md", "Bình luận tham khảo — không phải căn cứ.\n")
	reg, err = Load(fsys)
	require.NoError(t, err)
	pack, _ = reg.Get(samplePack)
	assert.Contains(t, strings.Join(pack.Lint(), "\n"), "01/2021/NĐ-CP is cited nowhere")
	assert.Empty(t, pack.CitedIn("01/2021/NĐ-CP"))
}

func TestNumericLiterals(t *testing.T) {
	cases := map[string][]string{
		"Thuế TNCN = (Doanh thu − 100.000.000) × 0,015":                      {"100.000.000", "0,015"},
		"Thuế sau giảm = Thuế × (1 − Tỷ lệ giảm ÷ 100)":                      nil,
		"Thuế tháng = Thuế năm ÷ 12 (0 nếu không vượt ngưỡng)":               nil,
		"Theo Điều 7 khoản 2 của 68/2026/NĐ-CP, kỳ tính thuế năm 2026":       nil,
		"Biên = (Tỷ lệ % doanh thu ÷ 100) ÷ Thuế suất; nhân 100 để đọc ra %": nil,
		"Ngưỡng 500 triệu": {"500 triệu"},
		"Tỷ lệ 1.5":        {"1.5"},
		"Biên = Tỷ lệ ÷ 100 ÷ Thuế suất × 0.2": {"0.2"},
		// allowed numbers and provision/year words do not shield a value
		"Thuế = 0 nếu doanh thu năm 500 triệu trở xuống": {"500 triệu"},
		"Doanh thu năm 2 tỷ":       {"2 tỷ"},
		"Theo khoản 500 triệu":     {"500 triệu"},
		"Ngưỡng 1 tỷ đồng":         {"1 tỷ"},
		"Ngưỡng 100 triệu đồng":    {"100 triệu"},
		"Giảm 12 nghìn":            {"12 nghìn"},
		"Thuế = Doanh thu × 100 %": {"100 %"},
		"Mức 0 đồng":               {"0 đồng"},
		"năm 500":                  {"500"},
		"khoản 1000":               {"1000"},
		// non-ASCII digits and fractions, a bare ratio
		"Thuế = Doanh thu × １５ %": {"１", "５"},
		"Tỷ lệ ٥":                 {"٥"},
		"Thuế × ½":                {"½"},
		"Tỷ lệ 1000/2000":         {"1000", "2000"},
		"1 đơn vị":                nil,
	}
	for text, want := range cases {
		assert.Equal(t, want, numericLiterals(text), text)
	}
}

func TestArticleListed(t *testing.T) {
	listed := []string{"Điều 10 khoản 2 điểm đ", "Điều 3", "Phụ lục I"}
	assert.True(t, articleListed("Điều 10", listed))
	assert.True(t, articleListed("điều  3 khoản 1", listed))
	assert.True(t, articleListed("phụ lục i", listed))
	assert.False(t, articleListed("Điều 30", listed))
	assert.False(t, articleListed("Phụ lục II", listed))
	assert.False(t, articleListed("Điều 3", nil))
}

func TestExampleValueMatches(t *testing.T) {
	assert.True(t, ExampleValueMatches(120000000, 120000000.4), "money ±1 đ")
	assert.True(t, ExampleValueMatches(46666666.67, 46666666.666666664), "money written to the đồng")
	assert.False(t, ExampleValueMatches(120000000, 120000002))
	assert.True(t, ExampleValueMatches(833333, 833333.33), "a whole amount written rounded")
	assert.False(t, ExampleValueMatches(833333, 833334))
	assert.False(t, ExampleValueMatches(833333.5, 833333.33), "a written fraction is not rounding")
	assert.True(t, ExampleValueMatches(0.075, 0.075000000000001))
	assert.False(t, ExampleValueMatches(0.075, 0.08))
	assert.False(t, ExampleValueMatches(5, 5.4), "a rate is not money: no ±1")
	assert.False(t, ExampleValueMatches(1.5, 2))
	assert.False(t, ExampleValueMatches(500, 500.4), "below 1000 a whole number is not an amount")
	assert.False(t, ExampleValueMatches(math.NaN(), 0))
	assert.False(t, ExampleValueMatches(1, math.Inf(1)))
}

func TestShippedFormulaHash(t *testing.T) {
	reg, err := Load(formulaFS(t))
	require.NoError(t, err)
	pack, _ := reg.Get(samplePack)
	h := pack.ShippedFormulaHash("thue_theo_loi_nhuan")
	require.Len(t, h, 16)
	assert.Equal(t, h, pack.ShippedFormulaHash("thue_theo_loi_nhuan"), "stable")
	assert.Empty(t, pack.ShippedFormulaHash("khong_co"))

	fsys := formulaFS(t)
	setFile(fsys, "formulas.yaml", strings.Replace(sampleFormulas, "name: Thuế kê khai", "name: Thuế kê khai (mới)", 1))
	reg2, err := Load(fsys)
	require.NoError(t, err)
	pack2, _ := reg2.Get(samplePack)
	assert.NotEqual(t, h, pack2.ShippedFormulaHash("thue_theo_loi_nhuan"), "a new shipped wording has a new hash")
}
