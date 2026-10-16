package docformat

import (
	"strings"
	"testing"
)

func TestMissingComponentFindingNamesTheComponent(t *testing.T) {
	comp := "quoc_hieu"
	r := &Report{DocumentType: &DocumentTypeInfo{Used: "cong_van"}, Checks: []CheckResult{{
		ID: "component.quoc_hieu.present", Desc: "Văn bản phải có thành phần 'quoc_hieu'",
		Severity: "error", Component: &comp, Status: StatusFail, Actual: "missing",
	}}}
	got := measuredFindings(r, StatusFail)
	if len(got) != 1 {
		t.Fatalf("findings: %+v", got)
	}
	text := got[0].text
	if strings.Contains(text, "quoc_hieu") || strings.Contains(text, "missing") || !strings.Contains(text, "thành phần Quốc hiệu") {
		t.Fatalf("text: %s", text)
	}
}
