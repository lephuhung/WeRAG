// Package domainpacks loads the domain packs embedded from domain-packs/:
// read-only skills with reference files that an agent opts into by name
// (CustomAgentConfig.DomainPacks). The format and its rules are in
// docs/domain-pack-standard.md §3; behaviour that must always happen (the
// footer, the review warning) is code, never text in the pack.
package domainpacks

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/skills"
	"gopkg.in/yaml.v3"
)

// dateLayout is how manifest dates are written (as_of, review_by).
const dateLayout = "2006-01-02"

// Date is a calendar date from the manifest, kept as written so an unquoted
// 2026-10-17 neither becomes a timestamp nor depends on a time zone.
type Date struct {
	time.Time
}

// UnmarshalYAML accepts only YYYY-MM-DD.
func (d *Date) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: date must be a YYYY-MM-DD scalar", node.Line)
	}
	t, err := time.Parse(dateLayout, strings.TrimSpace(node.Value))
	if err != nil {
		return fmt.Errorf("line %d: date %q is not YYYY-MM-DD", node.Line, node.Value)
	}
	d.Time = t
	return nil
}

// VN formats the date the way users read it (dd/mm/yyyy).
func (d Date) VN() string {
	return d.Format("02/01/2006")
}

// Manifest is the frontmatter of a pack's SKILL.md (§3.3). It is decoded
// with KnownFields, so a misspelt field is an error rather than a silently
// missing footer.
type Manifest struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Metadata    Metadata `yaml:"metadata"`
}

// Metadata is the pack's identity, provenance and runtime contract.
type Metadata struct {
	Version     string `yaml:"version"`
	AsOf        Date   `yaml:"as_of"`
	ReviewBy    Date   `yaml:"review_by"`
	Domain      string `yaml:"domain"`
	Licence     string `yaml:"licence"`
	Attribution string `yaml:"attribution"`
	Origin      string `yaml:"origin"`
	// RequiredTools must be in the agent's effective tool set; always-on
	// tools are not listed (validate.go).
	RequiredTools []string `yaml:"required_tools"`
	// AnswerFooter is appended by code to every answer of an agent that has
	// the pack, never left to the model.
	AnswerFooter string `yaml:"answer_footer"`
	// DraftDocuments are drafts the pack mentions; they must be labelled as
	// drafts wherever they are cited.
	DraftDocuments []DraftDocument `yaml:"draft_documents"`
}

// DraftVersionUnknown is the version of a draft whose round and date the
// source does not give; such a draft has no date.
const DraftVersionUnknown = "chưa rõ lần dự thảo"

// DraftDocument is one draft the pack talks about. Date is required unless
// Version is DraftVersionUnknown, and then it is absent.
type DraftDocument struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
	Date    Date   `yaml:"date"`
	// Aliases are phrases that refer to this draft ("doanh nghiệp một
	// người"); the title always counts. A mention in a paragraph that does
	// not say "dự thảo" gets the code-appended draft notice.
	Aliases []string `yaml:"aliases"`
}

// VersionUnknown reports whether the source does not say which draft it is.
func (d DraftDocument) VersionUnknown() bool {
	return strings.EqualFold(strings.TrimSpace(d.Version), DraftVersionUnknown)
}

// ParseManifest reads a pack's SKILL.md: the generic skill parser (name,
// description, body, Skill.Validate) and then the strict manifest decode.
// A frontmatter the generic parser had to repair is refused: packs are
// written in this repo, so there is nothing to be lenient about.
func ParseManifest(content []byte) (*skills.Skill, *Manifest, error) {
	skill, err := skills.ParseSkillFile(string(content))
	if err != nil {
		return nil, nil, err
	}
	if skill.FrontmatterRepaired {
		return nil, nil, errors.New("frontmatter is not valid YAML as written")
	}
	frontmatter, err := frontmatterOf(content)
	if err != nil {
		return nil, nil, err
	}
	var manifest Manifest
	if err := decodeStrict([]byte(frontmatter), &manifest); err != nil {
		return nil, nil, fmt.Errorf("manifest: %w", err)
	}
	return skill, &manifest, nil
}

// decodeStrict decodes one YAML document, rejecting unknown fields.
func decodeStrict(data []byte, dest any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(dest)
}

// frontmatterOf returns the text between the leading --- markers, the same
// span skills.ParseSkillFile reads.
func frontmatterOf(content []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(bytes.TrimPrefix(content, []byte("\ufeff"))))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", errors.New("SKILL.md must start with ---")
	}
	var lines []string
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "---" {
			return strings.Join(lines, "\n"), scanner.Err()
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("frontmatter is not closed with ---")
}
