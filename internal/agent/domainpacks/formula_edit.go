package domainpacks

import (
	"fmt"
	"math"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/domaintools"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// A SuperAdmin may edit a shipped formula's wording at runtime: its name,
// expression, variable descriptions, units and sources, basis and examples.
// The id, tool and variable names stay those of formulas.yaml, as the Go
// implementation is bound to them; the calculation itself never changes.
// An edit is stored outside the pack (see the domain pack formula override
// repository) and applied over the shipped formula until it is reset.

// Limits of an edit: the formula block goes into every output of its tool.
const (
	maxFormulaNameRunes       = 200
	maxFormulaExpressionRunes = 1000
	maxFormulaTextRunes       = 300
	maxFormulaExamples        = 20
)

// ShippedFormula is formula id as formulas.yaml writes it.
func (p *Pack) ShippedFormula(id string) (Formula, bool) {
	if p.formulas == nil {
		return Formula{}, false
	}
	for _, fm := range p.formulas.Formulas {
		if fm.ID == id {
			return fm, true
		}
	}
	return Formula{}, false
}

// FormulaEdited reports whether formula id is served from an edit.
func (p *Pack) FormulaEdited(id string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.overrides[id]
	return ok
}

// CheckFormulaEdit validates edit as the replacement of the shipped formula
// edit.ID: the checks formulas.yaml passes, the same tool and variable
// names, no rate written as a constant, and every example reproduced by
// the Go implementation (money ±1 đ, ratios ±1e-9). It returns the edit
// with the tool set from the shipped formula and text fields trimmed.
func (p *Pack) CheckFormulaEdit(edit Formula) (Formula, []string) {
	shipped, ok := p.ShippedFormula(edit.ID)
	if !ok {
		return Formula{}, []string{fmt.Sprintf("formula %q is not in %s", edit.ID, formulasFile)}
	}
	edit.Tool = shipped.Tool
	edit.Name = strings.TrimSpace(edit.Name)
	edit.Expression = strings.TrimSpace(edit.Expression)
	for i := range edit.Variables {
		v := &edit.Variables[i]
		v.Name, v.Description, v.Unit = strings.TrimSpace(v.Name), strings.TrimSpace(v.Description), strings.TrimSpace(v.Unit)
	}
	for i := range edit.Basis {
		b := &edit.Basis[i]
		b.Number, b.Article = strings.TrimSpace(b.Number), strings.TrimSpace(b.Article)
	}

	catalogued := map[string]bool{}
	if p.catalogue != nil {
		for _, d := range p.catalogue.Documents {
			catalogued[vietnamese_legal.CatalogNumberKey(d.Number)] = true
		}
	}
	required := map[string]bool{}
	for _, t := range p.Manifest.Metadata.RequiredTools {
		required[t] = true
	}
	problems := checkFormula(edit, catalogued, required)

	want := map[string]bool{}
	for _, v := range shipped.Variables {
		want[v.Name] = true
	}
	got := map[string]bool{}
	for _, v := range edit.Variables {
		got[v.Name] = true
	}
	for _, name := range sortedSet(want) {
		if !got[name] {
			problems = append(problems, fmt.Sprintf("variable %q is missing: variable names are fixed by the Go implementation", name))
		}
	}
	for _, name := range sortedSet(got) {
		if !want[name] {
			problems = append(problems, fmt.Sprintf("variable %q is not a variable of the Go implementation", name))
		}
	}
	// FormulaBlock writes each formula on one line of every tool output
	for field, text := range map[string]string{"name": edit.Name, "expression": edit.Expression} {
		if strings.ContainsAny(text, "\r\n") {
			problems = append(problems, fmt.Sprintf("the %s must be one line", field))
		}
	}
	if n := len([]rune(edit.Name)); n > maxFormulaNameRunes {
		problems = append(problems, fmt.Sprintf("the name has %d characters, at most %d", n, maxFormulaNameRunes))
	}
	if n := len([]rune(edit.Expression)); n > maxFormulaExpressionRunes {
		problems = append(problems, fmt.Sprintf("the expression has %d characters, at most %d", n, maxFormulaExpressionRunes))
	}
	for _, v := range edit.Variables {
		if len([]rune(v.Description)) > maxFormulaTextRunes || len([]rune(v.Unit)) > maxFormulaTextRunes {
			problems = append(problems, fmt.Sprintf("variable %q: description and unit are at most %d characters", v.Name, maxFormulaTextRunes))
		}
	}
	seenBasis := map[string]bool{}
	for _, b := range edit.Basis {
		key := vietnamese_legal.CatalogNumberKey(b.Number) + "|" + strings.ToLower(b.Article)
		if seenBasis[key] {
			problems = append(problems, fmt.Sprintf("basis %s %s is listed twice", b.Article, b.Number))
		}
		seenBasis[key] = true
	}
	if len(edit.Examples) > maxFormulaExamples {
		problems = append(problems, fmt.Sprintf("at most %d examples", maxFormulaExamples))
	}
	if constantRatePattern.MatchString(edit.Expression) {
		problems = append(problems, "the expression writes a rate as a constant; make it a variable with source kb")
	}
	if len(problems) > 0 {
		return Formula{}, problems
	}
	return edit, checkExamples(edit)
}

// checkExamples runs every example through the Go implementation.
func checkExamples(fm Formula) []string {
	impl, ok := domaintools.Lookup(fm.ID)
	if !ok {
		return []string{"no Go implementation in internal/agent/domaintools"}
	}
	var problems []string
	for i, ex := range fm.Examples {
		out, err := impl.Func(ex.Inputs)
		if err != nil {
			problems = append(problems, fmt.Sprintf("examples[%d]: the implementation refuses the inputs: %v", i, err))
			continue
		}
		for _, key := range sortedKeys(ex.Expected) {
			want := ex.Expected[key]
			v, ok := out[key]
			if !ok {
				problems = append(problems, fmt.Sprintf("examples[%d]: the implementation has no output %q", i, key))
				continue
			}
			tol := 1e-9
			if math.Abs(want) >= 1 {
				tol = 1 // money: ±1 đ
			}
			if math.IsNaN(want) || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v-want) > tol {
				problems = append(problems, fmt.Sprintf("examples[%d]: %s is %v, not %v", i, key, v, want))
			}
		}
	}
	return problems
}

// SetFormulaOverrides replaces the edits of the pack (formula id → edit).
// An edit that no longer passes CheckFormulaEdit (the build changed the
// formula, its variables or the catalogue) is not applied; its id is
// returned with the reasons so the caller can log it.
func (p *Pack) SetFormulaOverrides(edits map[string]Formula) map[string][]string {
	applied := map[string]Formula{}
	rejected := map[string][]string{}
	for id, edit := range edits {
		edit.ID = id
		checked, problems := p.CheckFormulaEdit(edit)
		if len(problems) > 0 {
			rejected[id] = problems
			continue
		}
		applied[id] = checked
	}
	p.mu.Lock()
	p.overrides = applied
	p.mu.Unlock()
	return rejected
}

// SetFormulaOverride applies one edit already checked by CheckFormulaEdit.
func (p *Pack) SetFormulaOverride(fm Formula) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.overrides == nil {
		p.overrides = map[string]Formula{}
	}
	p.overrides[fm.ID] = fm
}

// ClearFormulaOverride drops the edit of formula id (back to formulas.yaml).
func (p *Pack) ClearFormulaOverride(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.overrides, id)
}
