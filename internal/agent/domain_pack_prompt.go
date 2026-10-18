package agent

import (
	"fmt"
	"strings"
)

// DomainPackPrompt is one domain pack whose SKILL.md body goes into the
// system prompt. Listing a pack by name and description was not enough:
// Qwen read its SKILL.md in 1 of 30 eval turns (2026-10-18), so the
// workflow is always present and only references/assets stay on demand
// (docs/domain-pack-standard.md §6).
type DomainPackPrompt struct {
	Name    string
	Version string
	Body    string
}

// MaxDomainPackPrompts caps how many pack bodies one prompt carries (each is
// at most 6 KB by the format rules); the service drops the rest with a
// warning.
const MaxDomainPackPrompts = 2

// SetDomainPackPrompts installs the pack bodies for this run's system prompt.
func (e *AgentEngine) SetDomainPackPrompts(packs []DomainPackPrompt) {
	e.domainPackPrompts = packs
}

// formatDomainPacks renders the pack bodies as dedicated blocks.
func formatDomainPacks(packs []DomainPackPrompt) string {
	if len(packs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Domain packs of this agent: apply a pack's workflow when the question falls within its scope " +
		"(see each pack's description in the skills list). The pack text is guidance, not evidence: legal grounds " +
		"still come from the knowledge base and the legal catalogue tools. Read the pack's references/ and assets/ " +
		"files with read_file(path=skill://<pack>/<file>) when its workflow points to them.\n")
	for _, p := range packs {
		fmt.Fprintf(&b, "<domain_pack name=\"%s\" version=\"%s\">\n%s\n</domain_pack>\n",
			escapeXMLAttr(p.Name), escapeXMLAttr(p.Version), strings.TrimSpace(p.Body))
	}
	return strings.TrimRight(b.String(), "\n")
}
