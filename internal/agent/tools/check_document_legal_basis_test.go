package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

func lbToolCtx() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(5))
	return context.WithValue(ctx, types.UserIDContextKey, "u-1")
}

// lbToolReport is a report with one finding per kind the summary groups.
func lbToolReport() *LegalBasisReport {
	pos := func(text string) []LegalBasisPosition {
		return []LegalBasisPosition{{Paragraph: 3, Text: text, Anchor: &OpAnchor{Text: "Căn cứ " + text}, TextOccurrence: 1}}
	}
	return &LegalBasisReport{
		DocumentID: "ws-1", Revision: 3,
		Findings: []LegalBasisFinding{
			{Key: "a", Citation: "29/2018/QH14", Status: LegalBasisExpired, Note: "Luật 29/2018/QH14 đã hết hiệu lực.",
				Document: &LegalBasisDocument{DocumentNumber: "29/2018/QH14", DocType: "Luật"}, Positions: pos("29/2018/QH14")},
			{Key: "b", Citation: "Nghị định 68", Status: LegalBasisAmbiguous, Note: "Viện dẫn chưa đủ số hiệu.",
				Candidates:      []LegalBasisDocument{{DocumentNumber: "68/2026/NĐ-CP", DocType: "Nghị định", IssuedOn: "2026-03-01", CatalogStatus: "Còn hiệu lực"}},
				CandidatesTotal: 1, Positions: pos("Nghị định 68")},
			{Key: "c", Citation: "99/2099/NĐ-CP", Status: LegalBasisNotFound, Note: "Chưa có trong danh mục văn bản pháp luật: chưa rõ hiệu lực, cần tự kiểm tra.",
				Positions: pos("99/2099/NĐ-CP")},
			{Key: "d", Citation: "30/2020/NĐ-CP", Status: LegalBasisInForce, Note: "Nghị định 30/2020/NĐ-CP còn hiệu lực.",
				Document: &LegalBasisDocument{DocumentNumber: "30/2020/NĐ-CP", DocType: "Nghị định"}, Positions: pos("30/2020/NĐ-CP")},
		},
		Summary:     map[LegalBasisStatus]int{LegalBasisExpired: 1, LegalBasisAmbiguous: 1, LegalBasisNotFound: 1, LegalBasisInForce: 1},
		DocumentOps: []DocumentOp{{Op: OpMark, Anchor: &OpAnchor{Text: "Căn cứ 29/2018/QH14"}, Text: "29/2018/QH14", Style: "underline"}},
	}
}

func TestCheckDocumentLegalBasisToolResult(t *testing.T) {
	fw := newFakeWorkspace(nil)
	var ran string
	run := func(_ context.Context, tenantID uint64, sessionID, documentID string) (*LegalBasisReport, error) {
		if tenantID != 5 || sessionID != "s-1" {
			t.Errorf("ran for tenant %d session %q", tenantID, sessionID)
		}
		ran = documentID
		return lbToolReport(), nil
	}
	tool := NewCheckDocumentLegalBasisTool(fw, run, "s-1")
	res, err := tool.Execute(lbToolCtx(), json.RawMessage(`{}`))
	if err != nil || !res.Success {
		t.Fatalf("execute: %v %+v", err, res)
	}
	if ran != "ws-1" {
		t.Errorf("checked %q", ran)
	}
	if _, ok := res.Data[LegalBasisReportKey].(*LegalBasisReport); !ok {
		t.Errorf("no report in Data: %#v", res.Data)
	}
	// marks only on the user's click: nothing a client would apply by itself
	for _, k := range []string{"document_ops", "ops_batch_id", "proposal"} {
		if _, ok := res.Data[k]; ok {
			t.Errorf("Data carries %q", k)
		}
	}
	if res.Data["document_id"] != "ws-1" || res.Data["document"] == "" {
		t.Errorf("document fields: %#v", res.Data)
	}
	if s, _ := res.Data["summary_line"].(string); !strings.Contains(s, "29/2018/QH14 (hết hiệu lực)") || !strings.Contains(s, "Nghị định 68") {
		t.Errorf("summary line %q", s)
	}
	out := res.Output
	for _, want := range []string{
		"4 văn bản được viện dẫn", "hết hiệu lực 1",
		"Cần chú ý (1)", "Luật 29/2018/QH14 đã hết hiệu lực.",
		"Có thể là: Nghị định 68/2026/NĐ-CP (ban hành 01/03/2026; Còn hiệu lực)",
		"KHÔNG phải hết hiệu lực", "99/2099/NĐ-CP",
		"KHÔNG nói đã gạch chân",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Trong đoạn đã bôi đen") {
		t.Errorf("selection section without a selection")
	}
}

func TestCheckDocumentLegalBasisToolSelection(t *testing.T) {
	fw := newFakeWorkspace(nil)
	run := func(context.Context, uint64, string, string) (*LegalBasisReport, error) { return lbToolReport(), nil }
	ctx := types.WithDocumentSelection(lbToolCtx(), &types.DocumentSelection{Text: "Căn cứ Nghị định 68 về …", DocumentID: "ws-1"})
	res, _ := NewCheckDocumentLegalBasisTool(fw, run, "s-1").Execute(ctx, nil)
	if !res.Success {
		t.Fatalf("%+v", res)
	}
	head, _, _ := strings.Cut(res.Output, "Cần chú ý")
	if !strings.Contains(head, "Trong đoạn đã bôi đen:\n- Nghị định 68") || strings.Contains(head, "29/2018/QH14:") {
		t.Errorf("selection section:\n%s", head)
	}
}

func TestCheckDocumentLegalBasisToolRefusals(t *testing.T) {
	report := func(context.Context, uint64, string, string) (*LegalBasisReport, error) { return lbToolReport(), nil }

	// no document open in the editor
	res, _ := NewCheckDocumentLegalBasisTool(nil, report, "s-1").Execute(lbToolCtx(), nil)
	if res.Success || res.Error != errNoWorkspace {
		t.Errorf("no workspace: %+v", res)
	}
	// the only document is a source (a chat upload)
	src := newFakeWorkspace(nil)
	src.ws.Role = types.DocumentWorkspaceRoleSource
	res, _ = NewCheckDocumentLegalBasisTool(src, report, "s-1").Execute(lbToolCtx(), nil)
	if res.Success || !strings.Contains(res.Error, "tài liệu nguồn") {
		t.Errorf("source document: %+v", res)
	}
	// not signed in to an organisation
	res, _ = NewCheckDocumentLegalBasisTool(newFakeWorkspace(nil), report, "s-1").Execute(context.Background(), nil)
	if res.Success || !strings.Contains(res.Error, "tổ chức") {
		t.Errorf("no tenant: %+v", res)
	}
	// a Guest of the public portal
	guest := context.WithValue(lbToolCtx(), types.TenantInfoContextKey, &types.Tenant{ID: 5, Kind: types.TenantKindPublicPortal})
	ran := false
	res, _ = NewCheckDocumentLegalBasisTool(newFakeWorkspace(nil), func(context.Context, uint64, string, string) (*LegalBasisReport, error) {
		ran = true
		return lbToolReport(), nil
	}, "s-1").Execute(guest, nil)
	if res.Success || ran || !strings.Contains(res.Error, "đã đăng nhập") {
		t.Errorf("guest: ran=%v %+v", ran, res)
	}
	// the service refuses (a closed document, the catalogue down)
	failing := func(context.Context, uint64, string, string) (*LegalBasisReport, error) {
		return nil, apperrors.NewBadRequestError("only an open working document can be checked")
	}
	res, _ = NewCheckDocumentLegalBasisTool(newFakeWorkspace(nil), failing, "s-1").Execute(lbToolCtx(), nil)
	if res.Success || !strings.Contains(res.Error, "Không rà soát được căn cứ pháp lý") {
		t.Errorf("service error: %+v", res)
	}
	// a cancelled turn stops the run
	cancelled := func(context.Context, uint64, string, string) (*LegalBasisReport, error) { return nil, context.Canceled }
	if _, err := NewCheckDocumentLegalBasisTool(newFakeWorkspace(nil), cancelled, "s-1").Execute(lbToolCtx(), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}
