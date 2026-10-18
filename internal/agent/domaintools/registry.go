// Package domaintools is the registry of the fixed-formula calculations of
// the domain packs (docs/domain-pack-standard.md §4.1, §3.5). Each pack has
// its own pure package (opcvn, …); this one maps the formula ids written in
// the packs' formulas.yaml to their implementations, so the registry and
// the tests can check that every formula described is implemented, every
// implementation is described, and the examples hold.
package domaintools

import (
	"sort"

	"github.com/Tencent/WeKnora/internal/agent/domaintools/opcvn"
)

// Func is one formula: named inputs to named outputs, pure, no defaults.
type Func func(inputs map[string]float64) (map[string]float64, error)

// Formula is an implemented formula and the agent tool that runs it.
type Formula struct {
	ID   string
	Tool string
	Func Func
}

var formulas = func() map[string]Formula {
	out := map[string]Formula{}
	add := func(tool string, impls map[string]func(map[string]float64) (map[string]float64, error)) {
		for id, f := range impls {
			if _, dup := out[id]; dup {
				panic("domaintools: formula id " + id + " is implemented twice")
			}
			out[id] = Formula{ID: id, Tool: tool, Func: f}
		}
	}
	add(opcvn.ToolCompareTaxMethods, opcvn.Formulas())
	return out
}()

// Lookup returns the implementation of a formula id.
func Lookup(id string) (Formula, bool) {
	f, ok := formulas[id]
	return f, ok
}

// IDs lists every implemented formula id, sorted.
func IDs() []string {
	ids := make([]string, 0, len(formulas))
	for id := range formulas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// IsDomainTool reports whether tool runs domain formulas: a pack that
// requires it must describe it in formulas.yaml.
func IsDomainTool(tool string) bool {
	for _, f := range formulas {
		if f.Tool == tool {
			return true
		}
	}
	return false
}
