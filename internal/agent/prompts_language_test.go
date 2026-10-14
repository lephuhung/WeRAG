package agent

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// Qwen copies the language of its examples: a Chinese example in a prompt run
// for Vietnamese users comes back as Chinese labels and aliases.
func TestWikiPromptsCarryNoChinese(t *testing.T) {
	for name, prompt := range map[string]string{
		"WikiTaxonomyPlanPrompt":     WikiTaxonomyPlanPrompt,
		"WikiSummaryPrompt":          WikiSummaryPrompt,
		"WikiKnowledgeExtractPrompt": WikiKnowledgeExtractPrompt,
		"WikiCandidateSlugPrompt":    WikiCandidateSlugPrompt,
		"WikiChunkCitationPrompt":    WikiChunkCitationPrompt,
		"WikiPageModifySystemPrompt": WikiPageModifySystemPrompt,
		"WikiPageModifyUserPrompt":   WikiPageModifyUserPrompt,
		"WikiIndexIntroPrompt":       WikiIndexIntroPrompt,
		"WikiIndexIntroUpdatePrompt": WikiIndexIntroUpdatePrompt,
		"WikiDeduplicationPrompt":    WikiDeduplicationPrompt,
	} {
		if types.ContainsHan(prompt) {
			t.Errorf("%s contains Chinese", name)
		}
	}
}
