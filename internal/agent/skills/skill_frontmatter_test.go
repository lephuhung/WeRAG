package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSkillFileRepairsKeysNestedUnderName(t *testing.T) {
	content := `---
name: trợ-lý-văn-thư
  version: 1.2.6
  description: |
    Trợ lý văn thư toàn diện — soạn thảo công văn theo Nghị định 30.
    Chỉ mang tính tham khảo.
metadata:
  displayName: "Trợ lý văn thư"
  version: 1.2.0
---
# Body
`
	skill, err := ParseSkillFile(content)
	require.NoError(t, err)
	require.True(t, skill.FrontmatterRepaired)
	require.Equal(t, "trợ-lý-văn-thư", skill.Name)
	require.Contains(t, skill.Description, "Trợ lý văn thư toàn diện")
	require.Contains(t, skill.Instructions, "# Body")
}

func TestParseSkillFileQuotesUnquotedColonInDescription(t *testing.T) {
	content := `---
name: university-applications
description: Apply to universities: resume, essays, and interviews
---
Body
`
	skill, err := ParseSkillFile(content)
	require.NoError(t, err)
	require.True(t, skill.FrontmatterRepaired)
	require.Equal(t, "university-applications", skill.Name)
	require.Equal(t, "Apply to universities: resume, essays, and interviews", skill.Description)
}

func TestRepairAccidentalNestedFrontmatterLeavesValidNestedMapping(t *testing.T) {
	src := strings.TrimSpace(`
compatibility:
  python: "3.11"
name: pdf-tools
description: Extract text
`)
	require.Equal(t, src, repairAccidentalNestedFrontmatter(src))

	var skill Skill
	repaired, err := UnmarshalSkillFrontmatter(src, &skill)
	require.NoError(t, err)
	require.False(t, repaired)
	require.Equal(t, "pdf-tools", skill.Name)
}

func TestUnmarshalSkillFrontmatterStillRejectsBrokenYAML(t *testing.T) {
	var skill Skill
	_, err := UnmarshalSkillFrontmatter(":\n  - oops", &skill)
	require.Error(t, err)
	require.Empty(t, skill.Name)
}

func TestUnmarshalSkillFrontmatterDoesNotPolluteDestOnFailedRepair(t *testing.T) {
	type dest struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Tags        []string `yaml:"tags"`
	}
	// tags would decode, but the nested name/description plus broken
	// metadata list is still invalid after the two repairs. dest must
	// stay zero — not a mix of a partial first attempt and a later one.
	fm := "tags:\n  - a\n  - b\nname: alpha\n  version: 9.9.9\n  description: |\n    hello\nmetadata:\n  openclaw:\n    install:\n      - kind: node\n      package: iztro\n"
	var d dest
	repaired, err := UnmarshalSkillFrontmatter(fm, &d)
	require.Error(t, err)
	require.False(t, repaired)
	require.Empty(t, d.Name)
	require.Empty(t, d.Tags)
}

func TestParseSkillFileUsesSlugWhenNameIsADisplayTitle(t *testing.T) {
	content := `---
name: Word / DOCX
slug: word-docx
description: Create and edit Microsoft Word documents.
---
Body
`
	skill, err := ParseSkillFile(content)
	require.NoError(t, err)
	require.Equal(t, "word-docx", skill.Name)
}

func TestParseSkillFileSlugifiesDisplayTitleWithoutSlug(t *testing.T) {
	content := `---
name: Word / DOCX
description: Create and edit Microsoft Word documents.
---
Body
`
	skill, err := ParseSkillFile(content)
	require.NoError(t, err)
	require.Equal(t, "word-docx", skill.Name)
}

func TestParseSkillFileKeepsUnicodeName(t *testing.T) {
	content := `---
name: trợ-lý-pháp-lý
description: Trợ lý soạn thảo văn bản pháp lý
---
Body
`
	skill, err := ParseSkillFile(content)
	require.NoError(t, err)
	require.Equal(t, "trợ-lý-pháp-lý", skill.Name)
}

func TestParseSkillFileRejectsSkillHubFrontmatterWithBrokenExtraYAML(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "skillhub_nested_name.md"))
	require.NoError(t, err)

	_, err = ParseSkillFile(string(content))
	require.Error(t, err, "nest repair is not enough when extra keys (metadata.openclaw.install) are still invalid YAML")
}
