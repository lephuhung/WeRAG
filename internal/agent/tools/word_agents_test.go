package tools

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestWordAgentCapabilities(t *testing.T) {
	reasoning := WordAgentCapabilities(types.BuiltinSmartReasoningID)
	quick := WordAgentCapabilities(types.BuiltinQuickAnswerID)
	for name, block := range map[string]string{"reasoning": reasoning, "quick": quick} {
		if !strings.Contains(block, "<agent_capabilities>") || !strings.Contains(block, "không có công cụ") ||
			!strings.Contains(block, "Soạn thảo văn bản") || !strings.Contains(block, "Không suy đoán") &&
			!strings.Contains(block, "không suy đoán") {
			t.Errorf("%s block: %s", name, block)
		}
	}
	if !strings.Contains(quick, "bôi đen") || !strings.Contains(reasoning, "CHỈ BÁO CÁO") {
		t.Fatal("quick answer works from the selection; reasoning only reports the format check")
	}
	if WordAgentCapabilities(types.BuiltinDocumentAssistantID) != "" || WordAgentCapabilities("custom") != "" {
		t.Fatal("other agents get no block")
	}
}

func TestReportOnlyFormatCheckNeverMarks(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, [4]int{20, 15, 30, 20}))
	tool := NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-ro").ReportOnly()
	res := runTool(t, tool, `{"mark":true}`)
	if !res.Success {
		t.Fatalf("result: %+v", res)
	}
	if ops, ok := res.Data["document_ops"]; ok && ops != nil {
		if list, _ := ops.([]DocumentOp); len(list) > 0 {
			t.Fatalf("a report-only check marks nothing: %+v", ops)
		}
	}
	if !strings.Contains(tool.Description(), "ONLY REPORTS") {
		t.Fatal("the description says it only reports")
	}
}

func TestFormatCheckMarksWithoutReportOnly(t *testing.T) {
	ws := newFakeWorkspace(testCongVan(t, [4]int{20, 15, 30, 20}))
	res := runTool(t, NewCheckDocumentFormatToolForWorkspace(ws, nil, "sess-m"), `{"mark":true}`)
	list, _ := res.Data["document_ops"].([]DocumentOp)
	if len(list) == 0 {
		t.Fatalf("the fixture has findings to mark: %+v", res.Data["document_ops"])
	}
}
