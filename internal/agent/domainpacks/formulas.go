package domainpacks

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/domaintools"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// formulasFile describes the pack's fixed formulas (docs/domain-pack-
// standard.md §3.5). It is optional for a pack without a domain tool and is
// never served to a model: the tool's output carries what the model needs.
const formulasFile = "formulas.yaml"

// Variable sources: a value the user gives, or one looked up in the
// knowledge base (a rate, a threshold) — never a constant in the formula.
const (
	VariableSourceInput = "input"
	VariableSourceKB    = "kb"
)

// Formulas is formulas.yaml.
type Formulas struct {
	Formulas []Formula `yaml:"formulas"`
}

// Formula is one fixed formula of the pack; its id names the Go
// implementation in internal/agent/domaintools.
type Formula struct {
	ID         string            `yaml:"id"`
	Name       string            `yaml:"name"`
	Expression string            `yaml:"expression"`
	Variables  []FormulaVariable `yaml:"variables"`
	Basis      []FormulaBasis    `yaml:"basis"`
	Tool       string            `yaml:"tool"`
	Examples   []FormulaExample  `yaml:"examples"`
}

// FormulaVariable is one input of a formula.
type FormulaVariable struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Unit        string `yaml:"unit"`
	Source      string `yaml:"source"`
}

// FormulaBasis is a provision the formula rests on; Number is in the
// pack's catalogue.yaml.
type FormulaBasis struct {
	Number  string `yaml:"number"`
	Article string `yaml:"article"`
}

// FormulaExample is a worked example the implementation must reproduce.
type FormulaExample struct {
	Inputs   map[string]float64 `yaml:"inputs"`
	Expected map[string]float64 `yaml:"expected"`
}

var (
	snakeCasePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	// a percentage or rate written into an expression ("× 20 %")
	constantRatePattern = regexp.MustCompile(`\d+(?:[.,]\d+)?\s*%`)
	// a number as written ("100.000.000", "0,015", "12")
	numberPattern = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
	// numbers that name a provision or a document, not a value: "Điều 7",
	// "khoản 2", "68/2026/NĐ-CP", "năm 2026"
	provisionRefPattern = regexp.MustCompile(`(?i)(?:điều|khoản|điểm|chương|mục|phụ lục|năm)\s+\d+[a-zđ]?\b`)
	docNumberRefPattern = regexp.MustCompile(`\d{1,4}/(?:19|20)\d\d(?:/[0-9]*[A-Za-zĐđ][A-Za-zĐđ0-9]*(?:[-–—‐][A-Za-zĐđ0-9]+)*)?`)
	// the Điều a basis article names ("Điều 10 khoản 2 điểm đ" → "10")
	articleDieuPattern = regexp.MustCompile(`(?i)điều\s+(\d+[a-zđ]?)`)
)

// allowedFormulaNumbers are the numbers a formula's name or expression may
// write: unit conversions ("÷ 100", "× 12 tháng") and bounds ("0", "1 −
// tỷ lệ"). An amount, threshold or rate is a variable: the name and the
// expression go to the model and the answer next to the Go results
// (FormulaBlock), so a constant there would contradict them.
var allowedFormulaNumbers = map[string]bool{"0": true, "1": true, "12": true, "100": true}

// numericLiterals are the numbers text writes beyond allowedFormulaNumbers,
// provision and document references aside.
func numericLiterals(text string) []string {
	text = docNumberRefPattern.ReplaceAllString(text, " ")
	text = provisionRefPattern.ReplaceAllString(text, " ")
	var out []string
	for _, n := range numberPattern.FindAllString(text, -1) {
		if !allowedFormulaNumbers[n] {
			out = append(out, n)
		}
	}
	return out
}

// catalogueArticles maps each catalogue number key to its articles: list.
func catalogueArticles(c *Catalogue) map[string][]string {
	out := map[string][]string{}
	if c == nil {
		return out
	}
	for _, d := range c.Documents {
		key := vietnamese_legal.CatalogNumberKey(d.Number)
		out[key] = append(out[key], d.Articles...)
	}
	return out
}

// articleListed reports whether article is one of listed (catalogue.yaml
// articles: of its document): the same Điều ("Điều 10" and "Điều 10
// khoản 2" are one), or the same text when it names no Điều.
func articleListed(article string, listed []string) bool {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	dieu := func(s string) string {
		if m := articleDieuPattern.FindStringSubmatch(s); m != nil {
			return strings.ToLower(m[1])
		}
		return ""
	}
	want, wantDieu := norm(article), dieu(article)
	for _, a := range listed {
		if wantDieu != "" && dieu(a) == wantDieu || norm(a) == want {
			return true
		}
	}
	return false
}

// parseFormulas decodes formulas.yaml strictly and checks it against the
// manifest and the catalogue.
func parseFormulas(data []byte, m *Manifest, catalogue *Catalogue) (*Formulas, []error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(formulasFile+": "+format, args...))
	}
	var f Formulas
	if err := decodeStrict(data, &f); err != nil {
		fail("%v", err)
		return nil, errs
	}
	if len(f.Formulas) == 0 {
		fail("formulas is empty")
	}
	catalogued := catalogueArticles(catalogue)
	required := map[string]bool{}
	if m != nil {
		for _, t := range m.Metadata.RequiredTools {
			required[t] = true
		}
	}
	ids := map[string]bool{}
	for i, fm := range f.Formulas {
		at := fmt.Sprintf("formulas[%d] (%s)", i, fm.ID)
		if ids[fm.ID] {
			fail("%s: id is listed twice", at)
		}
		ids[fm.ID] = true
		for _, problem := range checkFormula(fm, catalogued, required) {
			fail("%s: %s", at, problem)
		}
	}
	return &f, errs
}

// checkFormula is the check of one formula: snake_case id with a Go
// implementation for its tool, name and expression, variables, basis in
// the catalogue (catalogued: number key → articles) with its article among
// that document's articles, no number in the name or expression beyond
// allowedFormulaNumbers, examples naming every variable. It returns one
// line per problem.
func checkFormula(fm Formula, catalogued map[string][]string, required map[string]bool) []string {
	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if !snakeCasePattern.MatchString(fm.ID) {
		fail("id must be snake_case")
	}
	if strings.TrimSpace(fm.Name) == "" || strings.TrimSpace(fm.Expression) == "" {
		fail("name and expression are required")
	}
	for _, field := range []struct{ name, text string }{{"name", fm.Name}, {"expression", fm.Expression}} {
		if nums := numericLiterals(field.text); len(nums) > 0 {
			fail("the %s writes the number(s) %s: an amount, threshold or rate is a variable (source input or kb); only 0, 1, 12 and 100 may be written",
				field.name, strings.Join(nums, ", "))
		}
	}
	if !required[fm.Tool] {
		fail("tool %q is not in required_tools", fm.Tool)
	}
	if impl, ok := domaintools.Lookup(fm.ID); !ok {
		fail("no Go implementation in internal/agent/domaintools")
	} else if impl.Tool != fm.Tool {
		fail("implemented for tool %q, not %q", impl.Tool, fm.Tool)
	}
	vars := map[string]bool{}
	if len(fm.Variables) == 0 {
		fail("variables are required")
	}
	for j, v := range fm.Variables {
		switch {
		case !snakeCasePattern.MatchString(v.Name):
			fail("variables[%d] name %q must be snake_case", j, v.Name)
		case vars[v.Name]:
			fail("variable %q is listed twice", v.Name)
		case strings.TrimSpace(v.Description) == "" || strings.TrimSpace(v.Unit) == "":
			fail("variable %q needs description and unit", v.Name)
		case v.Source != VariableSourceInput && v.Source != VariableSourceKB:
			fail("variable %q source must be %q or %q", v.Name, VariableSourceInput, VariableSourceKB)
		}
		vars[v.Name] = true
	}
	for j, b := range fm.Basis {
		articles, ok := catalogued[vietnamese_legal.CatalogNumberKey(b.Number)]
		switch {
		case !ok:
			fail("basis[%d] number %q is not in %s", j, b.Number, catalogueFile)
		case strings.TrimSpace(b.Article) != "" && !articleListed(b.Article, articles):
			fail("basis[%d] %s is not among the articles of %s in %s", j, strings.TrimSpace(b.Article), b.Number, catalogueFile)
		}
	}
	if len(fm.Examples) == 0 {
		fail("at least one example is required")
	}
	for j, ex := range fm.Examples {
		if len(ex.Expected) == 0 {
			fail("examples[%d] has no expected values", j)
		}
		for _, name := range sortedSet(vars) {
			if _, ok := ex.Inputs[name]; !ok {
				fail("examples[%d] lacks input %q", j, name)
			}
		}
		for _, name := range sortedKeys(ex.Inputs) {
			if !vars[name] {
				fail("examples[%d] input %q is not a variable", j, name)
			}
		}
	}
	return problems
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkDomainToolsDescribed requires every domain tool the pack needs to be
// described by at least one formula.
func checkDomainToolsDescribed(m *Manifest, f *Formulas) []error {
	if m == nil {
		return nil
	}
	described := map[string]bool{}
	if f != nil {
		for _, fm := range f.Formulas {
			described[fm.Tool] = true
		}
	}
	var errs []error
	for _, tool := range m.Metadata.RequiredTools {
		if domaintools.IsDomainTool(tool) && !described[tool] {
			errs = append(errs, fmt.Errorf("%s: required tool %q runs domain formulas but no formula describes it", formulasFile, tool))
		}
	}
	return errs
}

// Formulas is the pack's formulas.yaml with the SuperAdmin's edits applied
// (formula_edit.go), nil when it has none.
func (p *Pack) Formulas() []Formula {
	if p.formulas == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.overrides) == 0 {
		return p.formulas.Formulas
	}
	out := make([]Formula, len(p.formulas.Formulas))
	for i, fm := range p.formulas.Formulas {
		if edit, ok := p.overrides[fm.ID]; ok {
			fm = edit
		}
		out[i] = fm
	}
	return out
}

// FormulaBlock renders, for a tool's output, the formulas the shipped packs
// describe for that tool: name, expression and basis (số hiệu + điều). It
// is the single text of the formulas; Go code does not repeat it. "" when
// no pack describes the tool.
func (r *Registry) FormulaBlock(tool string) string {
	if r == nil {
		return ""
	}
	seen := map[string]bool{}
	var b strings.Builder
	for _, name := range r.names {
		for _, fm := range r.packs[name].Formulas() {
			if fm.Tool != tool || seen[fm.ID] {
				continue
			}
			seen[fm.ID] = true
			if b.Len() == 0 {
				b.WriteString("Công thức áp dụng:\n")
			}
			fmt.Fprintf(&b, "- %s: %s", fm.Name, fm.Expression)
			if basis := basisText(fm.Basis); basis != "" {
				fmt.Fprintf(&b, " (căn cứ: %s)", basis)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func basisText(basis []FormulaBasis) string {
	parts := make([]string, 0, len(basis))
	for _, bs := range basis {
		part := strings.TrimSpace(bs.Number)
		if a := strings.TrimSpace(bs.Article); a != "" {
			part = a + " " + part
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

// citations maps each number key cited in the pack's SKILL.md, references
// and assets to the files citing it, and the key to the number as written.
func citations(texts map[string]string) (map[string][]string, map[string]string) {
	files := map[string][]string{}
	written := map[string]string{}
	for _, rel := range sortedKeys(texts) {
		seen := map[string]bool{}
		// the shape the legal request gate and the answer number check use
		for _, n := range vietnamese_legal.FindDocumentNumbers(texts[rel]) {
			key := vietnamese_legal.CatalogNumberKey(n)
			if seen[key] {
				continue
			}
			seen[key] = true
			files[key] = append(files[key], rel)
			if _, ok := written[key]; !ok {
				written[key] = n
			}
		}
	}
	return files, written
}

// CitedIn lists the pack files (SKILL.md, references/…, assets/…) that cite
// number, sorted; computed at load, never written by hand.
func (p *Pack) CitedIn(number string) []string {
	return append([]string(nil), p.citedIn[vietnamese_legal.CatalogNumberKey(number)]...)
}

// citationFindings are the lint lines for numbers cited but not catalogued
// and catalogue entries cited nowhere.
func (p *Pack) citationFindings() []string {
	var findings []string
	catalogued := map[string]bool{}
	if p.catalogue != nil {
		for _, d := range p.catalogue.Documents {
			key := vietnamese_legal.CatalogNumberKey(d.Number)
			catalogued[key] = true
			if len(p.citedIn[key]) == 0 {
				findings = append(findings, fmt.Sprintf("%s: %s is cited nowhere in the pack text", catalogueFile, d.Number))
			}
		}
	}
	keys := make([]string, 0, len(p.citedIn))
	for key := range p.citedIn {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !catalogued[key] {
			findings = append(findings, fmt.Sprintf("%s: %s is cited but not in %s",
				strings.Join(p.citedIn[key], ", "), p.citedWritten[key], catalogueFile))
		}
	}
	return findings
}

// formulaFindings flag an expression that writes a rate as a constant: a
// statutory rate is a variable with source kb.
func (p *Pack) formulaFindings() []string {
	var findings []string
	for _, fm := range p.Formulas() {
		if constantRatePattern.MatchString(fm.Expression) {
			findings = append(findings, fmt.Sprintf("%s: %s writes a rate as a constant; make it a variable with source kb",
				formulasFile, fm.ID))
		}
	}
	return findings
}
