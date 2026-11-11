package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// ToolCheckDocumentLegalBasis runs the legal basis check ("Rà soát căn cứ
// pháp lý", CheckLegalBasis) on an open working document from the chat, the
// check the editor's button runs. The model gets a compact summary; the
// full report goes to the client in Data["legal_basis_report"], where a
// card lists the findings with their "Gạch chân" buttons. Nothing is
// underlined until the user presses one: the result carries no top-level
// document_ops, so no client applies marks on its own.
const ToolCheckDocumentLegalBasis = "check_document_legal_basis"

// LegalBasisReportKey is the Data key of the full report.
const LegalBasisReportKey = "legal_basis_report"

// LegalBasisRunnerFailedKey flags (Data, true) a call whose check itself
// failed, not its arguments or document reference.
const LegalBasisRunnerFailedKey = "runner_failed"

// LegalBasisRunner checks one open working document of a session (the
// service's DocumentFormatPrecheck.LegalBasisCheck: it saves the editor
// first and refuses anything but an open target).
type LegalBasisRunner func(ctx context.Context, tenantID uint64, sessionID, documentID string) (*LegalBasisReport, error)

var checkDocumentLegalBasisTool = BaseTool{
	name: ToolCheckDocumentLegalBasis,
	description: `Checks every legal document the open working document cites (its căn cứ and other references) against the legal catalogue: in force, amended, expired, suspended, needs review, ambiguous (candidates listed), or not in the catalogue. Use when the user asks to review the document's legal basis or whether the documents it cites are still in force. For one named law use check_legal_status; for what the law says use search_knowledge.

Present the result as it is. A citation not in the catalogue is "chưa rõ hiệu lực", never expired; for an ambiguous one list the candidates and ask which is meant. Nothing is underlined or changed: the card under your answer lets the user underline the citations.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + documentParamSchema + `
  }
}`),
}

// CheckDocumentLegalBasisTool is the chat side of the legal basis check.
type CheckDocumentLegalBasisTool struct {
	BaseTool
	workspace DocumentWorkspaceSource
	run       LegalBasisRunner
	sessionID string
}

// NewCheckDocumentLegalBasisTool builds the tool for one session.
func NewCheckDocumentLegalBasisTool(workspace DocumentWorkspaceSource, run LegalBasisRunner, sessionID string) *CheckDocumentLegalBasisTool {
	return &CheckDocumentLegalBasisTool{BaseTool: checkDocumentLegalBasisTool, workspace: workspace, run: run, sessionID: sessionID}
}

func (t *CheckDocumentLegalBasisTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var in struct {
		Document string `json:"document"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return &types.ToolResult{Success: false, Error: "invalid arguments: " + err.Error()}, nil
		}
	}
	// a Guest of the public portal has no working document; the check is
	// never theirs to run, whatever the session holds
	if access.IsPublicPortalGuest(ctx, types.CallerFromContext(ctx)) {
		return &types.ToolResult{Success: false, Error: "Rà soát căn cứ pháp lý chỉ dùng được với tài khoản đã đăng nhập vào một tổ chức."}, nil
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return &types.ToolResult{Success: false, Error: "Không xác định được tổ chức của người dùng; không rà soát được căn cứ pháp lý."}, nil
	}
	if t.workspace == nil || t.run == nil || t.sessionID == "" {
		return &types.ToolResult{Success: false, Error: errNoWorkspace}, nil
	}
	target, err := resolveTargetDocument(ctx, t.workspace, t.sessionID, in.Document, false)
	if err != nil {
		return &types.ToolResult{Success: false, Error: err.Error()}, nil
	}
	report, err := t.run(ctx, tenantID, t.sessionID, target.ID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		// the check itself failed (catalogue, timeout, closed document, an
		// internal error): flagged so a pure check ends with why
		// (agent.legalBasisFailureMessage); resolve and argument errors
		// above are left to the model to retry
		return &types.ToolResult{Success: false,
			Error: "Không rà soát được căn cứ pháp lý của " + DocumentLabel(target) + ": " + legalBasisFailureReason(err) + ".",
			Data:  map[string]interface{}{LegalBasisRunnerFailedKey: true}}, nil
	}
	sel := types.DocumentSelectionFromContext(ctx)
	if !selectionIn(sel, target) {
		sel = nil
	}
	summary := legalBasisHistoryLine(target, report)
	return &types.ToolResult{
		Success: true,
		Output:  legalBasisToolOutput(target, report, sel),
		Data: map[string]interface{}{
			LegalBasisReportKey: report,
			"document_id":       target.ID,
			"document":          DocumentLabel(target),
			"file_name":         target.FileName,
			"summary_line":      summary,
		},
	}, nil
}

// legalBasisFailureReason words why the check failed for the user (the
// service's errors are in English).
func legalBasisFailureReason(err error) string {
	var app *apperrors.AppError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "tra danh mục văn bản pháp luật quá thời gian, vui lòng thử lại"
	case errors.As(err, &app) && app.Code == apperrors.ErrServiceUnavailable:
		return "danh mục văn bản pháp luật tạm thời không khả dụng"
	case errors.As(err, &app) && app.Code == apperrors.ErrBadRequest:
		return "chỉ rà soát được văn bản làm việc đang mở trong trình soạn thảo"
	case errors.As(err, &app) && app.Code == apperrors.ErrNotFound:
		return "không tìm thấy văn bản trong cuộc hội thoại này"
	case strings.Contains(err.Error(), "documents are disabled"):
		return "tính năng soạn thảo văn bản đang tắt"
	}
	return "lỗi hệ thống khi đọc văn bản hoặc danh mục, vui lòng thử lại"
}

// legalBasisStatusLabels name the statuses in the model's summary, in the
// report order.
var legalBasisStatusLabels = []struct {
	status LegalBasisStatus
	label  string
}{
	{LegalBasisExpired, "hết hiệu lực"},
	{LegalBasisSuspended, "ngưng hiệu lực"},
	{LegalBasisPartlyExpired, "hết hiệu lực một phần"},
	{LegalBasisNeedsReview, "cần kiểm tra"},
	{LegalBasisNotYetInForce, "chưa có hiệu lực"},
	{LegalBasisAmbiguous, "chưa xác định được văn bản"},
	{LegalBasisAmended, "còn hiệu lực, đã được sửa đổi"},
	{LegalBasisUnknown, "danh mục chưa ghi hiệu lực"},
	{LegalBasisNotFound, "không có trong danh mục (chưa rõ hiệu lực)"},
	{LegalBasisInForce, "còn hiệu lực"},
}

// legalBasisCounts words the report's counts: "hết hiệu lực 2, còn hiệu lực 5".
func legalBasisCounts(r *LegalBasisReport) string {
	var parts []string
	for _, s := range legalBasisStatusLabels {
		if n := r.Summary[s.status]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s.label, n))
		}
	}
	return strings.Join(parts, "; ")
}

// legalBasisSummaryLine is the one line a later turn replays.
func legalBasisSummaryLine(ws *types.DocumentWorkspace, r *LegalBasisReport) string {
	if len(r.Findings) == 0 {
		return "Đã rà soát căn cứ pháp lý của " + DocumentLabel(ws) + ": không tìm thấy văn bản pháp luật nào được viện dẫn để kiểm tra."
	}
	return fmt.Sprintf("Đã rà soát căn cứ pháp lý của %s: %d văn bản được viện dẫn — %s.", DocumentLabel(ws), len(r.Findings), legalBasisCounts(r))
}

// legalBasisHistoryMax bounds the citations a later turn's replay names.
const legalBasisHistoryMax = 10

// legalBasisHistoryLine is what a later turn replays (history_compact): the
// counts and the citations needing attention or a choice, by status.
func legalBasisHistoryLine(ws *types.DocumentWorkspace, r *LegalBasisReport) string {
	line := legalBasisSummaryLine(ws, r)
	var named []string
	for i := range r.Findings {
		f := &r.Findings[i]
		if !LegalBasisMarkedByDefault(f.Status) && f.Status != LegalBasisAmbiguous {
			continue
		}
		if len(named) == legalBasisHistoryMax {
			named = append(named, "…")
			break
		}
		for _, s := range legalBasisStatusLabels {
			if s.status == f.Status {
				named = append(named, legalBasisFindingName(f)+" ("+s.label+")")
				break
			}
		}
	}
	if len(named) > 0 {
		line += " Cần chú ý: " + strings.Join(named, "; ") + "."
	}
	return line
}

const (
	// legalBasisToolMaxListed bounds the findings the model's summary lists
	// per group; the card shows them all.
	legalBasisToolMaxListed = 12
	// legalBasisToolMaxCandidates bounds the candidates named per ambiguous
	// citation.
	legalBasisToolMaxCandidates = 3
)

// legalBasisToolClosing ends the model's summary: what it may and may not
// say about the card and the document.
const legalBasisToolClosing = "CÁCH TRÌNH BÀY: nêu số liệu tổng hợp, rồi các viện dẫn cần chú ý kèm ghi chú như trên; viện dẫn chưa xác định được văn bản thì nêu các văn bản có thể là và hỏi người dùng muốn nói văn bản nào. " +
	"\"Không có trong danh mục\" là CHƯA RÕ hiệu lực, KHÔNG được nói là hết hiệu lực. " +
	"CHƯA có chỗ nào được gạch chân và văn bản không bị thay đổi: danh sách đầy đủ ở thẻ kết quả bên dưới câu trả lời, người dùng tự bấm \"Gạch chân\" nếu muốn. " +
	"KHÔNG nói đã gạch chân, KHÔNG đề nghị tự sửa văn bản."

// legalBasisToolOutput is the compact summary the model reads. sel is the
// turn's highlighted passage in the checked document (nil when none): the
// citations written inside it come first.
func legalBasisToolOutput(ws *types.DocumentWorkspace, r *LegalBasisReport, sel *types.DocumentSelection) string {
	var b strings.Builder
	b.WriteString(legalBasisSummaryLine(ws, r))
	b.WriteString(" Kiểm tra bằng danh mục văn bản pháp luật, không dùng mô hình.")
	if r.Unsaved {
		b.WriteString(" Lưu ý: chưa lưu được thay đổi mới nhất trong trình soạn thảo, đã kiểm tra bản lưu gần nhất.")
	}
	if r.Truncated {
		b.WriteString(" Văn bản viện dẫn quá nhiều văn bản: chỉ kiểm tra phần đầu.")
	}
	b.WriteString("\n")
	if sel != nil && strings.TrimSpace(sel.Text) != "" {
		in := legalBasisInSelection(r, sel.Text)
		if len(in) == 0 {
			b.WriteString("\nTrong đoạn đã bôi đen: không có văn bản pháp luật nào được viện dẫn (hoặc chỉ có văn bản bỏ qua khi kiểm tra).\n")
		} else {
			b.WriteString("\nTrong đoạn đã bôi đen:\n")
			for _, f := range in {
				writeLegalBasisFinding(&b, f)
			}
		}
	}
	attention := func(f *LegalBasisFinding) bool {
		return LegalBasisMarkedByDefault(f.Status)
	}
	groups := []struct {
		title string
		keep  func(*LegalBasisFinding) bool
	}{
		{"Cần chú ý", attention},
		{"Chưa xác định được văn bản (cần ghi rõ số hiệu, ngày ban hành)", func(f *LegalBasisFinding) bool { return f.Status == LegalBasisAmbiguous }},
		{"Không có trong danh mục — chưa rõ hiệu lực, cần tự kiểm tra (KHÔNG phải hết hiệu lực)", func(f *LegalBasisFinding) bool { return f.Status == LegalBasisNotFound }},
		{"Danh mục chưa ghi hiệu lực", func(f *LegalBasisFinding) bool { return f.Status == LegalBasisUnknown }},
		{"Còn hiệu lực", func(f *LegalBasisFinding) bool { return f.Status == LegalBasisInForce || f.Status == LegalBasisAmended }},
	}
	for _, g := range groups {
		var list []*LegalBasisFinding
		for i := range r.Findings {
			if g.keep(&r.Findings[i]) {
				list = append(list, &r.Findings[i])
			}
		}
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s (%d):\n", g.title, len(list))
		for i, f := range list {
			if i == legalBasisToolMaxListed {
				fmt.Fprintf(&b, "- … và %d viện dẫn khác (xem thẻ kết quả)\n", len(list)-i)
				break
			}
			if g.title == "Còn hiệu lực" {
				// a short line: nothing to act on
				fmt.Fprintf(&b, "- %s\n", legalBasisFindingName(f))
				continue
			}
			writeLegalBasisFinding(&b, f)
		}
	}
	if s := r.Skipped; s.NamesWithoutYear+s.OwnNumber+s.Paperwork > 0 {
		var parts []string
		if s.NamesWithoutYear > 0 {
			parts = append(parts, fmt.Sprintf("%d tên luật không ghi năm (hiểu là văn bản đang có hiệu lực)", s.NamesWithoutYear))
		}
		if s.OwnNumber > 0 {
			parts = append(parts, "số hiệu của chính văn bản")
		}
		if s.Paperwork > 0 {
			parts = append(parts, fmt.Sprintf("%d số văn bản hành chính thông thường (kế hoạch, tờ trình…) không có trong danh mục", s.Paperwork))
		}
		b.WriteString("\nKhông kiểm tra: " + strings.Join(parts, "; ") + ".\n")
	}
	b.WriteString("\n" + legalBasisToolClosing)
	return b.String()
}

// legalBasisFindingName is a finding's citation, with the catalogue
// document's label when it reads differently.
func legalBasisFindingName(f *LegalBasisFinding) string {
	if f.Document != nil {
		if l := legalBasisLabel(f.Document); l != "" && !strings.Contains(f.Citation, f.Document.DocumentNumber) {
			return f.Citation + " (" + l + ")"
		}
	}
	return f.Citation
}

func writeLegalBasisFinding(b *strings.Builder, f *LegalBasisFinding) {
	fmt.Fprintf(b, "- %s: %s", legalBasisFindingName(f), f.Note)
	if f.Status == LegalBasisAmbiguous && len(f.Candidates) > 0 {
		var names []string
		for i, c := range f.Candidates {
			if i == legalBasisToolMaxCandidates {
				break
			}
			names = append(names, fmt.Sprintf("%s (ban hành %s; %s)", legalBasisLabel(&c), legalOr(legalBasisDay(c.IssuedOn), "chưa rõ ngày"),
				legalOr(c.CatalogStatus, "chưa rõ tình trạng")))
		}
		more := ""
		if n := f.CandidatesTotal - len(names); n > 0 || f.CandidatesMore {
			more = "; còn văn bản khác"
		}
		fmt.Fprintf(b, " Có thể là: %s%s.", strings.Join(names, "; "), more)
	}
	b.WriteString("\n")
}

// legalBasisInSelection keeps the findings written inside the highlighted
// passage (one of their places is in its text).
func legalBasisInSelection(r *LegalBasisReport, selection string) []*LegalBasisFinding {
	text := normalizeDocText(selection)
	var out []*LegalBasisFinding
	for i := range r.Findings {
		for _, p := range r.Findings[i].Positions {
			if t := normalizeDocText(p.Text); t != "" && strings.Contains(text, t) {
				out = append(out, &r.Findings[i])
				break
			}
		}
	}
	return out
}
