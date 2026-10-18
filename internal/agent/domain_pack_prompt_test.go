package agent

import (
	"strings"
	"testing"
)

func findSection(sections []SystemPromptSection, name string) (string, bool) {
	for _, s := range sections {
		if s.Name == name {
			return s.Content, true
		}
	}
	return "", false
}

func TestDomainPackBodiesAreInTheSystemPrompt(t *testing.T) {
	opts := &BuildSystemPromptOptions{
		SelectedTools: []string{"search_knowledge", "read_file"},
		DomainPacks: []DomainPackPrompt{
			{Name: "opc-vn-business-law", Version: "1.1.0", Body: "# Cẩm nang\n\n## Quy trình A — Chọn mô hình\n"},
		},
	}
	sections := BuildSystemPromptSections(nil, false, opts)
	content, ok := findSection(sections, "domain_packs")
	if !ok {
		t.Fatal("no domain_packs section")
	}
	for _, want := range []string{
		`<domain_pack name="opc-vn-business-law" version="1.1.0">`,
		"## Quy trình A — Chọn mô hình",
		"</domain_pack>",
		"read_file(path=skill://<pack>/<file>)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("domain_packs section lacks %q:\n%s", want, content)
		}
	}
	if !strings.Contains(renderSystemPromptSections(sections), "Quy trình A") {
		t.Error("rendered prompt lacks the pack body")
	}

	opts.SkillInstallMode = true
	if _, ok := findSection(BuildSystemPromptSections(nil, false, opts), "domain_packs"); ok {
		t.Error("an install run gets no domain pack body")
	}
	if _, ok := findSection(BuildSystemPromptSections(nil, false, &BuildSystemPromptOptions{}), "domain_packs"); ok {
		t.Error("no packs, no section")
	}
}

func TestDomainPackBodyEscapesAttributes(t *testing.T) {
	got := formatDomainPacks([]DomainPackPrompt{{Name: `a"b`, Version: "1.0.0", Body: "x"}})
	if strings.Contains(got, `name="a"b"`) {
		t.Errorf("attribute not escaped: %s", got)
	}
}

func TestDomainPackWithoutBodyIsReadWithReadFile(t *testing.T) {
	got := formatDomainPacks([]DomainPackPrompt{{Name: "pack-a", Version: "1.0.0", Body: "Quy trình A"},
		{Name: "pack-b", Version: "2.0.0"}})
	if !strings.Contains(got, "Quy trình A") {
		t.Error("inlined body missing")
	}
	if !strings.Contains(got, `<domain_pack name="pack-b" version="2.0.0">`) ||
		!strings.Contains(got, "read_file(path=skill://pack-b/SKILL.md)") {
		t.Errorf("deferred pack must say how to read it:\n%s", got)
	}
}
