package domainpacks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/agent/tools"
	"gopkg.in/yaml.v3"
)

// ErrInvalidPack identifies a pack that breaks the format (§3.4). Packs ship
// in the binary, so like a broken built-in skill this stops startup.
var ErrInvalidPack = errors.New("invalid domain pack")

// Size and length limits of §3.4.
const (
	maxDescriptionRunes = 500
	maxSkillFileBytes   = 6 * 1024
	maxResourceBytes    = 32 * 1024
	maxReviewMonths     = 6
)

// Files a pack is made of (§3.2). references/ and assets/ hold Markdown and
// are what read_file serves; the rest is for tests and the catalogue check.
const (
	skillFile       = skills.SkillFileName
	catalogueFile   = "catalogue.yaml"
	attributionFile = "ATTRIBUTION.md"
	evalsDir        = "evals"
	evalsFile       = "evals/cases.jsonl"
	referencesDir   = "references"
	assetsDir       = "assets"
)

// AlwaysOnTools are registered for every agent whatever its allowlist
// (registerTools in agent_service.go strips them from allowed_tools and adds
// them back itself), so a pack does not declare them.
var AlwaysOnTools = []string{
	tools.ToolCheckLegalStatus,
	tools.ToolFindRelatedLegalDocuments,
	tools.ToolResolveAbbreviation,
	tools.ToolSearchDocumentSection,
}

var (
	packNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	semverPattern   = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	urlPattern      = regexp.MustCompile(`(?i)\b(?:https?|ftp)://|\bwww\.`)
)

// bannedSkillText may not appear in SKILL.md: packs never execute and never
// send the model to the web.
var bannedSkillText = []string{"shell_exec", "scripts/", "$WEKNORA_SKILL_DIR", "WEKNORA_SKILL_DIR"}

// injectionPhrases are lint, not a safety gate: review by hand stays
// mandatory. Matched case-insensitively.
var injectionPhrases = []string{
	"bỏ qua hướng dẫn",
	"bỏ qua các hướng dẫn",
	"bỏ qua mọi hướng dẫn",
	"lời nhắc hệ thống",
	"ignore previous",
	"ignore all previous",
	"ignore the above",
	"disregard previous",
	"system prompt",
}

// Catalogue is catalogue.yaml: the document numbers the pack relies on. It
// has no status column — validity only ever comes from legal_documents.
type Catalogue struct {
	Documents []CatalogueEntry `yaml:"documents"`
}

// CatalogueEntry is one document the pack relies on.
type CatalogueEntry struct {
	Number  string `yaml:"number"`
	Title   string `yaml:"title"`
	UsedFor string `yaml:"used_for"`
	// Articles are the provisions the pack cites ("Điều 85", "Điều 52
	// khoản 2"); optional.
	Articles []string `yaml:"articles"`
}

// statusKeys are refused anywhere in catalogue.yaml with a message that says
// why, before the strict decode reports them as merely unknown.
var statusKeys = map[string]bool{
	"status": true, "trang_thai": true, "hieu_luc": true, "in_force": true, "effective": true,
}

// validatePack reads and checks one pack directory of fsys. Every problem
// is reported, not only the first, so a pack can be fixed in one pass.
func validatePack(fsys fs.FS, name string) (*Pack, error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	pack := &Pack{files: map[string]string{}}
	seen := map[string]bool{}
	var skillData, catalogueData, evalsData, formulasData []byte

	walkErr := fs.WalkDir(fsys, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == name {
			return nil
		}
		rel := strings.TrimPrefix(p, name+"/")
		base := path.Base(rel)
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
			// go:embed would drop it silently: what is reviewed must be what ships
			fail("%s: hidden or underscore name is not allowed", rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			fail("%s: symbolic links are not allowed", rel)
			return nil
		}
		top, _, nested := strings.Cut(rel, "/")
		if d.IsDir() {
			switch {
			case top == referencesDir || top == assetsDir:
			case rel == evalsDir:
			default:
				fail("%s/: directory is not allowed (only references/, assets/, evals/)", rel)
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o111 != 0 {
			fail("%s: executable files are not allowed", rel)
		}
		switch {
		case rel == skillFile, rel == catalogueFile, rel == attributionFile, rel == evalsFile, rel == formulasFile:
		case nested && (top == referencesDir || top == assetsDir) && strings.HasSuffix(rel, ".md"):
		default:
			fail("%s: file is not allowed in a pack", rel)
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		seen[rel] = true
		switch rel {
		case skillFile:
			skillData = data
		case catalogueFile:
			catalogueData = data
		case evalsFile:
			evalsData = data
		case formulasFile:
			formulasData = data
		case attributionFile:
		default:
			if len(data) > maxResourceBytes {
				fail("%s: %d bytes; a reference file is at most %d", rel, len(data), maxResourceBytes)
			}
			if !utf8.Valid(data) {
				fail("%s: not UTF-8 text", rel)
			}
			if urlPattern.Match(data) {
				// sources belong in ATTRIBUTION.md; a link invites a web fetch
				fail("%s: contains a URL", rel)
			}
			pack.files[rel] = string(data)
		}
		return nil
	})
	if walkErr != nil {
		fail("reading the pack: %v", walkErr)
	}

	for _, required := range []string{skillFile, catalogueFile, attributionFile} {
		if !seen[required] {
			fail("%s is missing", required)
		}
	}
	if skillData != nil {
		for _, err := range checkSkill(pack, name, skillData) {
			errs = append(errs, err)
		}
	}
	if catalogueData != nil {
		catalogue, err := parseCatalogue(catalogueData)
		if err != nil {
			fail("%s: %v", catalogueFile, err)
		}
		pack.catalogue = catalogue
	}
	if formulasData != nil {
		formulas, ferrs := parseFormulas(formulasData, pack.Manifest, pack.catalogue)
		errs = append(errs, ferrs...)
		pack.formulas = formulas
	}
	if pack.Manifest != nil {
		errs = append(errs, checkDomainToolsDescribed(pack.Manifest, pack.formulas)...)
		texts := map[string]string{skillFile: pack.raw}
		for rel, content := range pack.files {
			texts[rel] = content
		}
		pack.citedIn, pack.citedWritten = citations(texts)
	}
	if evalsData != nil {
		if err := checkEvals(evalsData); err != nil {
			fail("%s: %v", evalsFile, err)
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w %q: %w", ErrInvalidPack, name, errors.Join(errs...))
	}
	return pack, nil
}

// checkSkill parses SKILL.md into the pack and applies the manifest rules.
func checkSkill(pack *Pack, dir string, data []byte) []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(skillFile+": "+format, args...))
	}
	if len(data) > maxSkillFileBytes {
		fail("%d bytes; at most %d", len(data), maxSkillFileBytes)
	}
	skill, manifest, err := ParseManifest(data)
	if err != nil {
		fail("%v", err)
		return errs
	}
	if skill.Instructions == "" {
		fail("instructions are empty")
	}

	name := manifest.Name
	if name != dir {
		fail("name %q must equal the directory name %q", name, dir)
	}
	if skills.IsBuiltinName(name) {
		fail("name %q is a built-in skill", name)
	} else if strings.HasPrefix(name, "legal-") {
		fail("name %q: the legal- prefix is reserved for built-in skills", name)
	}
	if !packNamePattern.MatchString(name) {
		fail("name %q must be kebab-case (a-z, 0-9, -)", name)
	}
	if n := utf8.RuneCountInString(manifest.Description); n > maxDescriptionRunes {
		fail("description is %d runes; at most %d", n, maxDescriptionRunes)
	}

	m := manifest.Metadata
	for field, value := range map[string]string{
		"metadata.version":       m.Version,
		"metadata.domain":        m.Domain,
		"metadata.licence":       m.Licence,
		"metadata.attribution":   m.Attribution,
		"metadata.origin":        m.Origin,
		"metadata.answer_footer": m.AnswerFooter,
	} {
		if strings.TrimSpace(value) == "" {
			fail("%s is required", field)
		}
	}
	if m.Version != "" && !semverPattern.MatchString(m.Version) {
		fail("metadata.version %q is not semver (x.y.z)", m.Version)
	}
	switch {
	case m.AsOf.IsZero() || m.ReviewBy.IsZero():
		fail("metadata.as_of and metadata.review_by are required")
	case !m.ReviewBy.After(m.AsOf.Time):
		fail("metadata.review_by %s must be after as_of %s", m.ReviewBy.Format(dateLayout), m.AsOf.Format(dateLayout))
	case m.ReviewBy.After(m.AsOf.AddDate(0, maxReviewMonths, 0)):
		fail("metadata.review_by %s is more than %d months after as_of %s",
			m.ReviewBy.Format(dateLayout), maxReviewMonths, m.AsOf.Format(dateLayout))
	}
	for i, draft := range m.DraftDocuments {
		switch {
		case strings.TrimSpace(draft.Title) == "" || strings.TrimSpace(draft.Version) == "":
			fail("metadata.draft_documents[%d] needs title and version", i)
		case draft.Date.IsZero() && !draft.VersionUnknown():
			// a date is only left out when the source does not know which
			// draft it is; inventing one would be cited as the draft's date
			fail("metadata.draft_documents[%d] needs a date unless its version is %q", i, DraftVersionUnknown)
		case !draft.Date.IsZero() && draft.VersionUnknown():
			fail("metadata.draft_documents[%d]: version %q must not carry a date", i, DraftVersionUnknown)
		}
	}

	available := map[string]bool{}
	for _, def := range tools.AvailableToolDefinitions() {
		available[def.Name] = true
	}
	declared := map[string]bool{}
	for _, tool := range m.RequiredTools {
		switch {
		case declared[tool]:
			fail("required_tools lists %q twice", tool)
		case isAlwaysOn(tool):
			fail("required_tools: %q is always on and is not declared", tool)
		case !available[tool]:
			fail("required_tools: %q is not an available tool", tool)
		}
		declared[tool] = true
	}

	// The body and every text the model or the user sees; origin is the one
	// field that is a URL by design.
	texts := []string{skill.Instructions, manifest.Description, m.AnswerFooter, m.Attribution}
	for _, draft := range m.DraftDocuments {
		texts = append(texts, draft.Title)
	}
	for _, text := range texts {
		if urlPattern.MatchString(text) {
			fail("contains a URL")
			break
		}
	}
	for _, banned := range bannedSkillText {
		if strings.Contains(string(data), banned) {
			fail("contains %q", banned)
		}
	}

	skill.BasePath = "skill://" + name
	skill.FilePath = skill.BasePath + "/" + skillFile
	pack.Skill = skill
	pack.Manifest = manifest
	pack.raw = string(data)
	return errs
}

func isAlwaysOn(tool string) bool {
	for _, name := range AlwaysOnTools {
		if name == tool {
			return true
		}
	}
	return false
}

// parseCatalogue decodes catalogue.yaml strictly; a status column is refused
// by name so the error says why.
func parseCatalogue(data []byte) (*Catalogue, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if key := findStatusKey(&root); key != "" {
		return nil, fmt.Errorf("field %q is not allowed: validity comes only from the legal_documents catalogue", key)
	}
	var catalogue Catalogue
	if err := decodeStrict(data, &catalogue); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i, doc := range catalogue.Documents {
		number := strings.TrimSpace(doc.Number)
		switch {
		case number == "":
			return nil, fmt.Errorf("documents[%d]: number is required", i)
		case strings.TrimSpace(doc.UsedFor) == "":
			return nil, fmt.Errorf("documents[%d] (%s): used_for is required", i, number)
		case seen[number]:
			return nil, fmt.Errorf("documents[%d]: number %q is listed twice", i, number)
		}
		seen[number] = true
		for j, a := range doc.Articles {
			if strings.TrimSpace(a) == "" {
				return nil, fmt.Errorf("documents[%d] (%s): articles[%d] is empty", i, number, j)
			}
		}
	}
	return &catalogue, nil
}

func findStatusKey(node *yaml.Node) string {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if key := strings.ToLower(node.Content[i].Value); statusKeys[key] {
				return node.Content[i].Value
			}
		}
	}
	for _, child := range node.Content {
		if key := findStatusKey(child); key != "" {
			return key
		}
	}
	return ""
}

// checkEvals requires one JSON object per non-empty line.
func checkEvals(data []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			return fmt.Errorf("line %d is not a JSON object: %v", line, err)
		}
	}
	return scanner.Err()
}

// Lint reports what review should look at in a valid pack: phrases that read
// like prompt injection, and tool names the pack mentions without declaring
// them. It is not a safety gate; the embedded-packs test fails on it so the
// findings are seen, but startup does not.
func (p *Pack) Lint() []string {
	var findings []string
	texts := map[string]string{skillFile: p.raw}
	for rel, content := range p.files {
		texts[rel] = content
	}
	for _, rel := range sortedKeys(texts) {
		lower := strings.ToLower(texts[rel])
		for _, phrase := range injectionPhrases {
			if strings.Contains(lower, phrase) {
				findings = append(findings, fmt.Sprintf("%s: injection-like phrase %q", rel, phrase))
			}
		}
	}

	findings = append(findings, p.citationFindings()...)
	findings = append(findings, p.formulaFindings()...)

	allowed := map[string]bool{tools.ToolReadFile: true}
	for _, name := range AlwaysOnTools {
		allowed[name] = true
	}
	for _, name := range p.Manifest.Metadata.RequiredTools {
		allowed[name] = true
	}
	for _, name := range knownToolNames() {
		if allowed[name] {
			continue
		}
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		if pattern.MatchString(p.Skill.Instructions) {
			findings = append(findings, fmt.Sprintf("%s: mentions tool %q that is not in required_tools", skillFile, name))
		}
	}
	return findings
}

// knownToolNames is every tool a pack could name: the selectable ones, the
// always-on ones and the runtime-granted ones.
func knownToolNames() []string {
	names := map[string]bool{}
	for _, def := range tools.AvailableToolDefinitions() {
		names[def.Name] = true
	}
	for _, name := range AlwaysOnTools {
		names[name] = true
	}
	for _, name := range []string{tools.ToolReadFile, tools.ToolShellExec, tools.ToolWebSearch,
		tools.ToolWebFetch, tools.ToolSearchMemory, tools.ToolPeopleLookup} {
		names[name] = true
	}
	return sortedKeys(names)
}
