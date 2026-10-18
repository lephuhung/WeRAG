package domainpacks

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/types"
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

// FooterMarker opens the footer block. It is stable so the block can be cut
// from an answer replayed as history (agent_history.go): a model that sees
// the footer in an earlier answer copies it, and the answer ends up with two.
const FooterMarker = "\n\n---\n**Lưu ý về cẩm nang chuyên môn**\n"

// Footer is the block code appends to every answer of an agent with these
// packs: each pack's answer_footer, and for a pack past review_by a line
// saying so. Text the answer already carries is not repeated. "" when
// nothing is left to add.
func (s *Source) Footer(now time.Time, answer string) string {
	var parts []string
	add := func(text string) {
		if text != "" && !strings.Contains(answer, text) {
			parts = append(parts, text)
		}
	}
	for _, pack := range s.Packs() {
		add(strings.TrimSpace(pack.Manifest.Metadata.AnswerFooter))
		if pack.Overdue(now) {
			add(fmt.Sprintf("Nội dung cẩm nang có mốc %s, đã quá hạn rà soát.", pack.Manifest.Metadata.AsOf.VN()))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return FooterMarker + strings.Join(parts, "\n\n") + "\n"
}

// StripFooter cuts the code-appended tail of an answer: the draft notice
// (DraftNoticeMarker), the tool answer block (types.ToolAnswerBlockMarker)
// or the footer, whichever comes first, and anything after it.
func StripFooter(answer string) string {
	cut := len(answer)
	for _, marker := range []string{DraftNoticeMarker, types.ToolAnswerBlockMarker, FooterMarker} {
		if i := strings.Index(answer, marker); i >= 0 && i < cut {
			cut = i
		}
	}
	return answer[:cut]
}
