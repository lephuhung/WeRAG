package skills

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrInvalidBuiltin identifies an invalid application asset. Unlike an
// unavailable tenant skill, a broken bundled instruction cannot be skipped.
var ErrInvalidBuiltin = errors.New("invalid built-in skill")

var builtinNames = []string{
	"legal-document-summary",
	"legal-document-comparison",
	"legal-latest-guidance",
	"legal-question-abbreviations",
}

// IsBuiltinName reports whether a name is reserved by the application.
func IsBuiltinName(name string) bool {
	for _, builtin := range builtinNames {
		if name == builtin {
			return true
		}
	}
	return false
}

// BuiltinSource holds immutable, instruction-only resources embedded in the
// application binary. It never exposes a host or sandbox execution path.
type BuiltinSource struct {
	skills map[string]*Skill
	files  map[string]string
}

var _ SkillSource = (*BuiltinSource)(nil)

// NewBuiltinSource validates every bundled skill before advertising any of
// them, so a bad application build cannot silently lose a legal skill.
func NewBuiltinSource(fsys fs.FS) (*BuiltinSource, error) {
	if fsys == nil {
		return nil, fmt.Errorf("%w: asset filesystem is nil", ErrInvalidBuiltin)
	}
	src := &BuiltinSource{skills: make(map[string]*Skill, len(builtinNames)), files: make(map[string]string, len(builtinNames))}
	for _, name := range builtinNames {
		filePath := name + "/SKILL.md"
		data, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidBuiltin, filePath, err)
		}
		skill, err := ParseSkillFile(string(data))
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidBuiltin, filePath, err)
		}
		declaredName, err := declaredBuiltinName(string(data))
		if err != nil || declaredName != name || skill.Name != name || skill.FrontmatterRepaired || strings.TrimSpace(skill.Instructions) == "" {
			return nil, fmt.Errorf("%w: %s: expected matching literal name, unmodified frontmatter and nonempty instructions: %v", ErrInvalidBuiltin, filePath, err)
		}
		skill.BasePath = "skill://" + name
		skill.FilePath = skill.BasePath + "/SKILL.md"
		src.skills[name] = skill
		src.files[name] = string(data)
	}
	return src, nil
}

// declaredBuiltinName checks the literal frontmatter identity before the
// third-party parser can replace an invalid display name with its slug.
func declaredBuiltinName(content string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(content, "\ufeff")))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", errors.New("missing frontmatter")
	}
	var lines []string
	closed := false
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "---" {
			closed = true
			break
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if !closed {
		return "", errors.New("unclosed frontmatter")
	}
	var frontmatter struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &frontmatter); err != nil {
		return "", err
	}
	return frontmatter.Name, nil
}

// Has checks membership in this source, never a tenant-provided name.
func (s *BuiltinSource) Has(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.skills[name]
	return ok
}

func (s *BuiltinSource) DiscoverSkills() ([]*SkillMetadata, error) {
	metadata := make([]*SkillMetadata, 0, len(builtinNames))
	for _, name := range builtinNames {
		if skill := s.skills[name]; skill != nil {
			metadata = append(metadata, skill.ToMetadata())
		}
	}
	return metadata, nil
}

func (s *BuiltinSource) LoadSkillInstructions(name string) (*Skill, error) {
	if !s.Has(name) {
		return nil, fmt.Errorf("built-in skill %q is unavailable", name)
	}
	copy := *s.skills[name]
	return &copy, nil
}

func (s *BuiltinSource) LoadSkillFile(name, relativePath string) (*SkillFile, error) {
	if !s.Has(name) || relativePath != SkillFileName {
		return nil, fmt.Errorf("built-in skill resource %q/%q is unavailable", name, relativePath)
	}
	return &SkillFile{Name: SkillFileName, Path: s.skills[name].FilePath, Content: s.files[name]}, nil
}

func (s *BuiltinSource) ListSkillFiles(name string) ([]string, error) {
	if !s.Has(name) {
		return nil, fmt.Errorf("built-in skill %q is unavailable", name)
	}
	return []string{}, nil
}

func (s *BuiltinSource) GetSkillBasePath(name string) (string, error) {
	if !s.Has(name) {
		return "", fmt.Errorf("built-in skill %q is unavailable", name)
	}
	return s.skills[name].BasePath, nil
}
