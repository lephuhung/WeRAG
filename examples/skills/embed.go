// Package legalskillassets embeds the four application-provided legal skills.
// Their Markdown files remain the single editable source of truth.
package legalskillassets

import "embed"

// FS contains exactly the four read-only SKILL.md files shipped to agents.
//
//go:embed legal-document-summary/SKILL.md legal-document-comparison/SKILL.md legal-latest-guidance/SKILL.md legal-question-abbreviations/SKILL.md
var FS embed.FS
