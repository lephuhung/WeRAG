// Package legalskillassets embeds the application-provided legal skill.
// Their Markdown files remain the single editable source of truth.
package legalskillassets

import "embed"

// FS contains exactly the read-only SKILL.md files shipped to agents.
//
//go:embed legal-document-summary/SKILL.md
var FS embed.FS
