package docformat

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

// The evaluation skills: Markdown guidance (SKILL.md with name/description
// frontmatter) for the format rules that need judgment rather than
// measurement — signing authority, Nơi nhận, required parts, wording
// (spelling is checked by the spellcheck model). the-thuc-chung applies to every document; the-thuc-<type>
// adds one document type's requirements. They are handed to the agent
// together with the judgment brief (RenderJudgment), so editing a
// rule is editing a Markdown file.
//
//go:embed skills/*/SKILL.md
var skillsFS embed.FS

// GeneralSkill is the skill that applies to every document.
const GeneralSkill = "the-thuc-chung"

// Skill is one evaluation skill.
type Skill struct {
	Name        string
	Description string
	Body        string // Markdown without the frontmatter
}

func loadSkill(name string) (Skill, bool) {
	data, err := skillsFS.ReadFile("skills/" + name + "/SKILL.md")
	if err != nil {
		return Skill{}, false
	}
	s := Skill{Name: name, Body: string(data)}
	if rest, ok := strings.CutPrefix(s.Body, "---\n"); ok {
		if fm, body, ok := strings.Cut(rest, "\n---\n"); ok {
			s.Body = strings.TrimLeft(body, "\n")
			for _, line := range strings.Split(fm, "\n") {
				if v, ok := strings.CutPrefix(line, "description:"); ok {
					s.Description = strings.TrimSpace(v)
				}
			}
		}
	}
	return s, true
}

// TypeSkillName is the skill of a document type ("cong_van" →
// "the-thuc-cong-van").
func TypeSkillName(docType string) string {
	return "the-thuc-" + strings.ReplaceAll(docType, "_", "-")
}

// SkillsFor returns the skills to evaluate a document of docType: the
// general skill, then the type's own skill when one exists.
func SkillsFor(docType string) []Skill {
	var out []Skill
	if s, ok := loadSkill(GeneralSkill); ok {
		out = append(out, s)
	}
	if docType != "" && docType != "unknown" {
		if s, ok := loadSkill(TypeSkillName(docType)); ok {
			out = append(out, s)
		}
	}
	return out
}

// SkillNames lists every shipped skill.
func SkillNames() []string {
	entries, err := skillsFS.ReadDir("skills")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

var skillsDigest = sync.OnceValue(func() string {
	h := sha256.New()
	add := func(fsys fs.FS, pattern string) {
		names, _ := fs.Glob(fsys, pattern)
		sort.Strings(names)
		for _, n := range names {
			data, _ := fs.ReadFile(fsys, n)
			h.Write([]byte(n + "\x00"))
			h.Write(data)
			h.Write([]byte{0})
		}
	}
	add(skillsFS, "skills/*/SKILL.md")
	add(rulesFS, "rules/*.json")
	h.Write([]byte(evaluatorPrompt + "\x00" + judgmentRenderVersion))
	return hex.EncodeToString(h.Sum(nil))[:16]
})

// SkillsDigest fingerprints what a skill evaluation depends on besides the
// document and the model: every skill, the rule sets (measured findings,
// anchors), the evaluator prompt and the judgment brief's layout version.
// Cached evaluations keyed with it are dropped when any of them changes.
func SkillsDigest() string { return skillsDigest() }
