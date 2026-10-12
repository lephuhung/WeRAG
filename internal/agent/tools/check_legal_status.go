package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ToolCheckLegalStatus reports the validity of a Vietnamese legal document
// from the platform-wide legal catalogue.
const ToolCheckLegalStatus = "check_legal_status"

// checkLegalStatusMaxPerGroup caps how many documents one relation group
// lists; a Luật can be guided by hundreds of documents.
const checkLegalStatusMaxPerGroup = 8

var checkLegalStatusTool = BaseTool{
	name: ToolCheckLegalStatus,
	description: `Check whether a Vietnamese legal document is still in force, using the
platform's legal catalogue (metadata imported from vbpl.vn, the Ministry of
Justice portal — central-government documents and the local documents of
some provinces).

## When to Use

- The user asks whether a document is still valid, when it takes or took
  effect, or what replaced, repealed, amended or guides it.
- The user names a document by số ký hiệu that the knowledge bases do not
  hold: its title, issuer and dates are here.
- Before you base an answer on a legal document you found (search results,
  a citation in a document), check it here when its validity matters, and
  say so when it has expired, been replaced or been amended.

## Input

Pass the số ký hiệu in "document_number" (e.g. "31/2024/QH15", "30/2020/NĐ-CP";
a number without its suffix such as "30/2020" also works). Without a number,
pass words of the title in "title" (e.g. "Luật Đất đai").

## Output

For each match: type, issuer, issue / effective / expiry dates, the portal's
status with the snapshot date, then the documents that act on it (replace,
repeal, amend, guide…) and those it acts on. The status is a snapshot: quote
its date. A document missing from the catalogue is unknown here — never
conclude it was repealed.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "document_number": {
      "type": "string",
      "description": "Số ký hiệu, e.g. \"31/2024/QH15\""
    },
    "title": {
      "type": "string",
      "description": "Title words when no number is known, e.g. \"Luật Đất đai\""
    }
  }
}`),
}

// CheckLegalStatusTool wraps the legal catalogue for the agent.
type CheckLegalStatusTool struct {
	BaseTool
	svc interfaces.LegalCatalogService
}

// NewCheckLegalStatusTool creates the tool.
func NewCheckLegalStatusTool(svc interfaces.LegalCatalogService) *CheckLegalStatusTool {
	return &CheckLegalStatusTool{BaseTool: checkLegalStatusTool, svc: svc}
}

type checkLegalStatusInput struct {
	DocumentNumber string `json:"document_number"`
	Title          string `json:"title"`
}

func (t *CheckLegalStatusTool) fail(msg string) (*types.ToolResult, error) {
	return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
}

// Execute looks the document up and renders its validity report.
func (t *CheckLegalStatusTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input checkLegalStatusInput
	if err := json.Unmarshal(args, &input); err != nil {
		return t.fail(fmt.Sprintf("Failed to parse args: %v", err))
	}
	if t.svc == nil {
		return t.fail("legal catalogue is not available")
	}
	if strings.TrimSpace(input.DocumentNumber) == "" && strings.TrimSpace(input.Title) == "" {
		return t.fail("document_number or title is required")
	}
	reports, err := t.svc.Lookup(ctx, input.DocumentNumber, input.Title, 0)
	if err != nil {
		return t.fail(fmt.Sprintf("legal catalogue lookup failed: %v", err))
	}
	if len(reports) == 0 {
		return &types.ToolResult{
			Success: true,
			Output: "No document in the legal catalogue matches. The catalogue holds central-government " +
				"documents and few local ones, so this one is unknown here — do not conclude it was repealed " +
				"or does not exist.",
			Data: map[string]interface{}{"matches": 0},
		}, nil
	}
	var b strings.Builder
	if len(reports) > 1 {
		fmt.Fprintf(&b, "%d documents match; ask the user which one they mean if it is unclear.\n\n", len(reports))
	}
	for i, rep := range reports {
		if i > 0 {
			b.WriteString("\n")
		}
		writeLegalStatusReport(&b, rep)
	}
	return &types.ToolResult{
		Success: true,
		Output:  b.String(),
		Data:    map[string]interface{}{"matches": len(reports), "reports": reports},
	}, nil
}

// legalLinkGroups orders the relation groups of a report: first what was
// done to the document (its validity), then what it does to others.
var legalLinkGroups = []struct {
	rel      types.LegalRelation
	incoming bool
	label    string
}{
	{types.LegalRelationReplaces, true, "Bị thay thế bởi"},
	{types.LegalRelationRepeals, true, "Bị bãi bỏ (toàn bộ hoặc một phần) bởi"},
	{types.LegalRelationExpires, true, "Bị làm hết hiệu lực bởi"},
	{types.LegalRelationPartiallyExpires, true, "Bị hết hiệu lực một phần bởi"},
	{types.LegalRelationSuspends, true, "Bị đình chỉ, tạm ngưng hiệu lực bởi"},
	{types.LegalRelationAmends, true, "Được sửa đổi, bổ sung bởi"},
	{types.LegalRelationCorrects, true, "Được đính chính bởi"},
	{types.LegalRelationConsolidates, true, "Được hợp nhất trong"},
	{types.LegalRelationInterprets, true, "Được giải thích bởi"},
	{types.LegalRelationGuides, true, "Được quy định chi tiết, hướng dẫn bởi"},
	{types.LegalRelationReplaces, false, "Thay thế"},
	{types.LegalRelationRepeals, false, "Bãi bỏ"},
	{types.LegalRelationExpires, false, "Làm hết hiệu lực"},
	{types.LegalRelationPartiallyExpires, false, "Làm hết hiệu lực một phần"},
	{types.LegalRelationSuspends, false, "Đình chỉ, tạm ngưng hiệu lực"},
	{types.LegalRelationAmends, false, "Sửa đổi, bổ sung"},
	{types.LegalRelationConsolidates, false, "Hợp nhất"},
	{types.LegalRelationGuides, false, "Quy định chi tiết, hướng dẫn"},
	{types.LegalRelationBasedOn, false, "Căn cứ"},
}

func writeLegalStatusReport(b *strings.Builder, rep *types.LegalStatusReport) {
	doc := rep.Document
	fmt.Fprintf(b, "## %s\n", legalDocLabel(doc))
	if doc.Issuer != "" {
		fmt.Fprintf(b, "- Cơ quan ban hành: %s\n", doc.Issuer)
	}
	fmt.Fprintf(b, "- Ngày ban hành: %s; có hiệu lực: %s; hết hiệu lực: %s\n",
		legalDate(doc.IssuedOn), legalDate(doc.EffectiveOn), legalDate(doc.ExpiredOn))
	status := doc.Status
	if status == "" {
		status = "chưa xác định"
	}
	if doc.SourceUpdatedAt != nil {
		fmt.Fprintf(b, "- Tình trạng hiệu lực: %s (theo %s, cập nhật %s)\n",
			status, doc.Source, doc.SourceUpdatedAt.Format("02/01/2006"))
	} else {
		fmt.Fprintf(b, "- Tình trạng hiệu lực: %s (theo %s)\n", status, doc.Source)
	}
	for _, g := range legalLinkGroups {
		var others []*types.LegalDocument
		for _, l := range rep.Links {
			if l.Relation == g.rel && l.Incoming == g.incoming && l.Other != nil {
				others = append(others, l.Other)
			}
		}
		if len(others) == 0 {
			continue
		}
		sort.SliceStable(others, func(i, j int) bool { return legalDateAfter(others[i].IssuedOn, others[j].IssuedOn) })
		fmt.Fprintf(b, "- %s:\n", g.label)
		for i, o := range others {
			if i == checkLegalStatusMaxPerGroup {
				fmt.Fprintf(b, "  - … và %d văn bản khác\n", len(others)-i)
				break
			}
			fmt.Fprintf(b, "  - %s (ban hành %s; %s)\n", legalDocLabel(o), legalDate(o.IssuedOn), legalOr(o.Status, "chưa rõ tình trạng"))
		}
	}
}

func legalDocLabel(doc *types.LegalDocument) string {
	title := strings.TrimSpace(doc.Title)
	num := strings.TrimSpace(doc.DocumentNumber)
	switch {
	case num != "" && strings.Contains(title, num):
		return title
	case num != "" && title != "":
		return fmt.Sprintf("%s %s — %s", doc.DocType, num, title)
	case num != "":
		return strings.TrimSpace(doc.DocType + " " + num)
	default:
		return title
	}
}

func legalDate(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Format("02/01/2006")
}

func legalDateAfter(a, b *time.Time) bool {
	if a == nil {
		return false
	}
	return b == nil || a.After(*b)
}

func legalOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
