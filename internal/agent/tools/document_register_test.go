package tools

import (
	"strings"
	"testing"
)

// Tools that write text default to one version in văn phong hành chính,
// lịch sự; apply_format_fixes applies at once unless a plan is asked for.
func TestDocumentToolDescriptionsDefaultToOneAdministrativeVersion(t *testing.T) {
	insert := NewInsertParagraphsTool(nil, "s").Description()
	for _, want := range []string{"văn phong hành chính, lịch sự", "ONE version", "Never offer alternative wordings", "ask for that fact only"} {
		if !strings.Contains(insert, want) {
			t.Errorf("insert_paragraphs description lacks %q", want)
		}
	}
	rewrite := NewRewriteParagraphsTool(nil, "s").Description()
	if !strings.Contains(rewrite, "One version is the default") || !strings.Contains(rewrite, "options_requested") {
		t.Errorf("rewrite_paragraphs description: %s", rewrite)
	}
	fixes := applyFormatFixesTool.Description()
	// the assistant points out; the format changes only once the user agreed
	if strings.Contains(fixes, "apply at once") || !strings.Contains(fixes, "nothing is changed before the user agrees") ||
		!strings.Contains(fixes, "apply=true") || !strings.Contains(fixes, "chỉ xem kế hoạch") || !strings.Contains(fixes, "force") {
		t.Errorf("apply_format_fixes description: %s", fixes)
	}
}
