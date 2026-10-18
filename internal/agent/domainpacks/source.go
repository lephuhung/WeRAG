package domainpacks

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/skills"
)

// Source offers one agent's packs to the skills manager. Like the built-in
// source it is read-only: no pack has a sandbox path or anything to execute.
type Source struct {
	packs  []*Pack
	byName map[string]*Pack
}

var _ skills.ReadOnlySource = (*Source)(nil)

// NewSource builds the source for the given packs, in order.
func NewSource(packs []*Pack) *Source {
	src := &Source{byName: make(map[string]*Pack, len(packs))}
	for _, pack := range packs {
		if pack == nil || src.byName[pack.Name()] != nil {
			continue
		}
		src.packs = append(src.packs, pack)
		src.byName[pack.Name()] = pack
	}
	return src
}

// Has reports whether this agent has the pack.
func (s *Source) Has(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.byName[name]
	return ok
}

// Packs are this agent's packs, in configured order.
func (s *Source) Packs() []*Pack {
	if s == nil {
		return nil
	}
	return append([]*Pack(nil), s.packs...)
}

func (s *Source) DiscoverSkills() ([]*skills.SkillMetadata, error) {
	metadata := make([]*skills.SkillMetadata, 0, len(s.Packs()))
	for _, pack := range s.Packs() {
		metadata = append(metadata, pack.Skill.ToMetadata())
	}
	return metadata, nil
}

func (s *Source) LoadSkillInstructions(name string) (*skills.Skill, error) {
	if !s.Has(name) {
		return nil, fmt.Errorf("domain pack %q is unavailable", name)
	}
	copy := *s.byName[name].Skill
	return &copy, nil
}

// LoadSkillFile serves SKILL.md and the pack's references/** and assets/**;
// catalogue.yaml, evals/ and ATTRIBUTION.md are not readable.
func (s *Source) LoadSkillFile(name, relativePath string) (*skills.SkillFile, error) {
	if !s.Has(name) {
		return nil, fmt.Errorf("domain pack %q is unavailable", name)
	}
	pack := s.byName[name]
	if relativePath == skills.SkillFileName {
		return &skills.SkillFile{Name: skills.SkillFileName, Path: pack.Skill.FilePath, Content: pack.raw}, nil
	}
	content, ok := pack.files[relativePath]
	if !ok {
		return nil, fmt.Errorf("domain pack resource %q/%q is unavailable", name, relativePath)
	}
	return &skills.SkillFile{Name: relativePath, Path: pack.Skill.BasePath + "/" + relativePath, Content: content}, nil
}

func (s *Source) ListSkillFiles(name string) ([]string, error) {
	if !s.Has(name) {
		return nil, fmt.Errorf("domain pack %q is unavailable", name)
	}
	return s.byName[name].Files(), nil
}

func (s *Source) GetSkillBasePath(name string) (string, error) {
	if !s.Has(name) {
		return "", fmt.Errorf("domain pack %q is unavailable", name)
	}
	return s.byName[name].Skill.BasePath, nil
}

// Footer is the block code appends to every answer of an agent with these
// packs: each pack's answer_footer, and for a pack past review_by a line
// saying so. "" when there are no packs.
func (s *Source) Footer(now time.Time) string {
	var parts []string
	for _, pack := range s.Packs() {
		if footer := strings.TrimSpace(pack.Manifest.Metadata.AnswerFooter); footer != "" {
			parts = append(parts, footer)
		}
		if pack.Overdue(now) {
			parts = append(parts, fmt.Sprintf("Nội dung cẩm nang có mốc %s, đã quá hạn rà soát.",
				pack.Manifest.Metadata.AsOf.VN()))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n---\n" + strings.Join(parts, "\n\n") + "\n"
}
