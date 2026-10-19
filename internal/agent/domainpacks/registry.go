package domainpacks

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/skills"
)

// Pack is one validated domain pack.
type Pack struct {
	Skill    *skills.Skill
	Manifest *Manifest
	// files are the resources read_file may serve: references/** and
	// assets/** only. catalogue.yaml, evals/ and ATTRIBUTION.md are never
	// given to a model.
	files     map[string]string
	catalogue *Catalogue
	formulas  *Formulas
	scope     *Scope
	raw       string // SKILL.md as written
	// citedIn maps a number key cited in SKILL.md, references or assets to
	// the files citing it; citedWritten keeps the number as first written.
	citedIn      map[string][]string
	citedWritten map[string]string
}

// Name is the pack's skill name.
func (p *Pack) Name() string {
	return p.Manifest.Name
}

// Files lists the servable resource paths, sorted.
func (p *Pack) Files() []string {
	return sortedKeys(p.files)
}

// Catalogue is the pack's catalogue.yaml.
func (p *Pack) Catalogue() *Catalogue {
	return p.catalogue
}

// Overdue reports whether now (a Vietnam time, types.PromptNow) is past the
// pack's review_by date. Being overdue never disables a pack: a SuperAdmin
// removes it from the agent if it should stop.
func (p *Pack) Overdue(now time.Time) bool {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return today.After(p.Manifest.Metadata.ReviewBy.Time)
}

// Registry is the set of packs shipped in this build. It is loaded once at
// startup and read-only afterwards.
type Registry struct {
	packs map[string]*Pack
	names []string
}

// Load validates every pack directory at the root of fsys. One invalid pack
// fails the whole load, like a broken built-in skill: what ships is what was
// reviewed. Files at the root are not packs and are ignored.
func Load(fsys fs.FS) (*Registry, error) {
	if fsys == nil {
		return nil, fmt.Errorf("%w: pack filesystem is nil", ErrInvalidPack)
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPack, err)
	}
	reg := &Registry{packs: map[string]*Pack{}}
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pack, err := validatePack(fsys, entry.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		reg.packs[pack.Name()] = pack
		reg.names = append(reg.names, pack.Name())
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	sort.Strings(reg.names)
	return reg, nil
}

// Get returns a pack by name.
func (r *Registry) Get(name string) (*Pack, bool) {
	if r == nil {
		return nil, false
	}
	pack, ok := r.packs[name]
	return pack, ok
}

// Has reports whether a pack of that name ships in this build.
func (r *Registry) Has(name string) bool {
	_, ok := r.Get(name)
	return ok
}

// Names lists the shipped packs, sorted.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.names...)
}

// Resolve returns the packs named, in the given order and without
// duplicates, and the names that are not shipped.
func (r *Registry) Resolve(names []string) (packs []*Pack, unknown []string) {
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if pack, ok := r.Get(name); ok {
			packs = append(packs, pack)
		} else {
			unknown = append(unknown, name)
		}
	}
	return packs, unknown
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
