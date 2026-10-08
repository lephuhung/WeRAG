package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// The document assistant writes in văn phong hành chính, lịch sự, one
// version, and asks only when a request cannot be carried out as stated.
func TestDocumentAssistantPromptDefaultsToOneAdministrativeVersion(t *testing.T) {
	data, err := os.ReadFile("../../config/prompt_templates/agent_system_prompt.yaml")
	require.NoError(t, err)
	var file struct {
		Templates []config.PromptTemplate `yaml:"templates"`
	}
	require.NoError(t, yaml.Unmarshal(data, &file))
	var content string
	for _, tpl := range file.Templates {
		if tpl.ID == "document_assistant" {
			content = tpl.Content
		}
	}
	require.NotEmpty(t, content)
	for _, want := range []string{
		"Whenever you write or propose text for the document (a rewrite, a new paragraph, a draft reply), use văn phong hành chính, lịch sự by default and give one version",
		"ask back only for a fact you cannot know or find",
		"apply it at once and report what changed",
		"use dry_run=true only when the user explicitly asks to see the plan first",
		"force=true", // the structure question stays
		"Propose exactly ONE version by default",
	} {
		require.Contains(t, content, want)
	}
	require.NotContains(t, content, "When it would change more than 10 paragraphs")
	// the general rule sits right after the REVIEW default
	review := strings.Index(content, "Your default behaviour is to REVIEW")
	rule := strings.Index(content, "Whenever you write or propose text for the document")
	require.True(t, review >= 0 && rule > review)
}
