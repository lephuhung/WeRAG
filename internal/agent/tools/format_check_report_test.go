package tools

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
)

func TestSessionFormatCheckReport(t *testing.T) {
	ctx := context.Background()
	if SessionFormatCheckReport(ctx, "ws-none") != nil {
		t.Fatal("no result kept: want nil")
	}

	formatChecks.put(ctx, &formatCheckResult{
		Output:       "# Đánh giá thể thức văn bản a.docx\n(đã thẩm định)\n\nKết luận.\n\n---\nTrình bày đánh giá trên cho người dùng.",
		FileName:     "a.docx",
		DocumentType: &docformat.DocumentTypeInfo{Used: "quy_che", RuleSet: "Quy chế (NĐ30/2020, Phụ lục I)"},
		Summary:      &docformat.Summary{Pass: 3, Fail: 1},
		Evaluated:    true,
		At:           time.Now(),
	}, formatCheckDocumentKey("ws-old"))
	r := SessionFormatCheckReport(ctx, "ws-old")
	if r == nil {
		t.Fatal("want a report")
	}
	if r.Evaluation != "Kết luận." {
		t.Errorf("evaluation from an older result = %q, want the judgment without the agent framing", r.Evaluation)
	}
	if r.DocumentTypeLabel != "Quy chế" || r.Summary == nil || r.Summary.Fail != 1 {
		t.Errorf("report = %+v", r)
	}

	formatChecks.put(ctx, &formatCheckResult{Output: "x", Evaluation: "Đúng thể thức.", Evaluated: true, At: time.Now()},
		formatCheckDocumentKey("ws-new"))
	if got := SessionFormatCheckReport(ctx, "ws-new").Evaluation; got != "Đúng thể thức." {
		t.Errorf("evaluation = %q", got)
	}

	formatChecks.put(ctx, &formatCheckResult{Output: "data", Evaluated: false, At: time.Now()}, formatCheckDocumentKey("ws-raw"))
	if SessionFormatCheckReport(ctx, "ws-raw") != nil {
		t.Error("an unevaluated result is not shown")
	}
}
