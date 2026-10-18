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
)

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
	catalogued := map[string]bool{}
	if catalogue != nil {
		for _, d := range catalogue.Documents {
			catalogued[vietnamese_legal.CatalogNumberKey(d.Number)] = true
		}
	}
	required := map[string]bool{}
	if m != nil {
		for _, t := range m.Metadata.RequiredTools {
			required[t] = true
		}
	}
	ids := map[string]bool{}
	for i, fm := range f.Formulas {
		at := fmt.Sprintf("formulas[%d] (%s)", i, fm.ID)
		switch {
		case !snakeCasePattern.MatchString(fm.ID):
			fail("%s: id must be snake_case", at)
		case ids[fm.ID]:
			fail("%s: id is listed twice", at)
		}
		ids[fm.ID] = true
		if strings.TrimSpace(fm.Name) == "" || strings.TrimSpace(fm.Expression) == "" {
			fail("%s: name and expression are required", at)
		}
		if !required[fm.Tool] {
			fail("%s: tool %q is not in required_tools", at, fm.Tool)
		}
		if impl, ok := domaintools.Lookup(fm.ID); !ok {
			fail("%s: no Go implementation in internal/agent/domaintools", at)
		} else if impl.Tool != fm.Tool {
			fail("%s: implemented for tool %q, not %q", at, impl.Tool, fm.Tool)
		}
		vars := map[string]bool{}
		if len(fm.Variables) == 0 {
			fail("%s: variables are required", at)
		}
		for j, v := range fm.Variables {
			switch {
			case !snakeCasePattern.MatchString(v.Name):
				fail("%s: variables[%d] name %q must be snake_case", at, j, v.Name)
			case vars[v.Name]:
				fail("%s: variable %q is listed twice", at, v.Name)
			case strings.TrimSpace(v.Description) == "" || strings.TrimSpace(v.Unit) == "":
				fail("%s: variable %q needs description and unit", at, v.Name)
			case v.Source != VariableSourceInput && v.Source != VariableSourceKB:
				fail("%s: variable %q source must be %q or %q", at, v.Name, VariableSourceInput, VariableSourceKB)
			}
			vars[v.Name] = true
		}
		for j, b := range fm.Basis {
			if !catalogued[vietnamese_legal.CatalogNumberKey(b.Number)] {
				fail("%s: basis[%d] number %q is not in %s", at, j, b.Number, catalogueFile)
			}
		}
		if len(fm.Examples) == 0 {
			fail("%s: at least one example is required", at)
		}
		for j, ex := range fm.Examples {
			if len(ex.Expected) == 0 {
				fail("%s: examples[%d] has no expected values", at, j)
			}
			for name := range vars {
				if _, ok := ex.Inputs[name]; !ok {
					fail("%s: examples[%d] lacks input %q", at, j, name)
				}
			}
			for name := range ex.Inputs {
				if !vars[name] {
					fail("%s: examples[%d] input %q is not a variable", at, j, name)
				}
			}
		}
	}
	return &f, errs
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

// Formulas is the pack's formulas.yaml, nil when it has none.
func (p *Pack) Formulas() []Formula {
	if p.formulas == nil {
		return nil
	}
	return p.formulas.Formulas
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

// documentNumberPattern finds full số hiệu (number/year/symbol) in pack text.
var documentNumberPattern = regexp.MustCompile(`\b\d{1,4}/\d{4}/[\p{L}\p{N}]+(?:-[\p{L}\p{N}]+)*`)

// citations maps each number key cited in the pack's SKILL.md, references
// and assets to the files citing it, and the key to the number as written.
func citations(texts map[string]string) (map[string][]string, map[string]string) {
	files := map[string][]string{}
	written := map[string]string{}
	for _, rel := range sortedKeys(texts) {
		seen := map[string]bool{}
		for _, n := range documentNumberPattern.FindAllString(texts[rel], -1) {
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
