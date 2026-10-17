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
	data := map[string]interface{}{"matches": len(reports), "reports": reports}
	if len(reports) > 1 {
		fmt.Fprintf(&b, "%d documents match; ask the user which one they mean if it is unclear.\n\n", len(reports))
		// a bare number ("53"): the user picks on a card, the turn ends
		if !strings.Contains(input.DocumentNumber, "/") {
			docs := make([]*types.LegalDocument, 0, len(reports))
			for _, rep := range reports {
				if rep.Document != nil {
					docs = append(docs, rep.Document)
				}
			}
			if card := LegalCandidatesFromDocs(types.UserQueryFromContext(ctx), input.DocumentNumber, docs); card != nil {
				for k, v := range card.Data() {
					data[k] = v
				}
				data["final_message"] = card.Message()
			}
		}
	}
	var derived []map[string]interface{}
	for i, rep := range reports {
		if i > 0 {
			b.WriteString("\n")
		}
		writeLegalStatusReport(&b, rep)
		if by, on := derivedExpiry(rep, legalNow()); by != nil && rep.Document != nil {
			derived = append(derived, map[string]interface{}{
				"document_id":        rep.Document.ID,
				"document_number":    rep.Document.DocumentNumber,
				"derived_status":     derivedStatusFor(rep.Document),
				"derived_expired_on": on.Format("2006-01-02"),
				"derived_from":       by.DocumentNumber,
			})
		}
	}
	if len(derived) > 0 {
		data["derived_status"] = derived
	}
	return &types.ToolResult{
		Success: true,
		Output:  b.String(),
		Data:    data,
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
	note := ""
	if by, on := derivedExpiry(rep, legalNow()); by != nil {
		rel := derivedRelation(rep, by)
		if partlyExpired(doc) {
			// the portal judged it partly expired: the link may be partial too
			note = fmt.Sprintf(" — LƯU Ý: có thể đã hết hiệu lực toàn bộ — %s %s có hiệu lực từ %s; "+
				"kiểm tra điều khoản chuyển tiếp của %s trước khi kết luận",
				derivedExpiryVerb[rel], legalShortLabel(by), on.Format("02/01/2006"), legalShortLabel(by))
		} else {
			// both facts, so neither the agent nor a reader sees a contradiction
			note = fmt.Sprintf(" — LƯU Ý: %s %s có hiệu lực từ %s, nên văn bản này hết hiệu lực từ ngày đó "+
				"(suy ra từ quan hệ %s; %s chưa cập nhật trạng thái)",
				derivedExpiryVerb[rel], legalShortLabel(by), on.Format("02/01/2006"),
				derivedExpiryNoun[rel], legalOr(doc.Source, "nguồn"))
		}
	}
	if doc.SourceUpdatedAt != nil {
		fmt.Fprintf(b, "- Tình trạng hiệu lực: %s (theo %s, cập nhật %s)%s\n",
			status, doc.Source, doc.SourceUpdatedAt.Format("02/01/2006"), note)
	} else {
		fmt.Fprintf(b, "- Tình trạng hiệu lực: %s (theo %s)%s\n", status, doc.Source, note)
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

// legalNow is the clock of derivedExpiry (a var for tests).
var legalNow = time.Now

// derivedExpiredStatus is the status derivedExpiry implies for a document
// the source holds in force; derivedMaybeExpiredStatus for one it holds
// partly expired (the portal may have judged the replacement partial).
const (
	derivedExpiredStatus      = "Hết hiệu lực toàn bộ"
	derivedMaybeExpiredStatus = "Có thể đã hết hiệu lực toàn bộ"
	legalStatusInForce        = "Còn hiệu lực"
	legalStatusPartlyExpired  = "Hết hiệu lực một phần"
)

// derivableStatus reports whether a source status may be reconciled with
// the links: in force, unknown, or partly expired (worded softer). Any
// other status (expired, not yet in force, suspended) is left as is.
func derivableStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "chưa xác định", strings.ToLower(legalStatusInForce), strings.ToLower(legalStatusPartlyExpired):
		return true
	}
	return false
}

// partlyExpired reports whether the source holds doc partly expired.
func partlyExpired(doc *types.LegalDocument) bool {
	return strings.EqualFold(strings.TrimSpace(doc.Status), legalStatusPartlyExpired)
}

// derivedStatusFor is the status derivedExpiry implies for doc.
func derivedStatusFor(doc *types.LegalDocument) string {
	if partlyExpired(doc) {
		return derivedMaybeExpiredStatus
	}
	return derivedExpiredStatus
}

// derivedExpiryRelations are the incoming relations that end a document
// as a whole once the other document takes effect. Repeals is left out:
// vbpl files partial repeals under it too ("Bị bãi bỏ (toàn bộ hoặc một
// phần)"), and PartiallyExpires is partial by definition.
var derivedExpiryRelations = map[types.LegalRelation]bool{
	types.LegalRelationReplaces: true,
	types.LegalRelationExpires:  true,
}

var derivedExpiryVerb = map[types.LegalRelation]string{
	types.LegalRelationReplaces: "đã bị thay thế bởi",
	types.LegalRelationExpires:  "đã bị làm hết hiệu lực bởi",
}

var derivedExpiryNoun = map[types.LegalRelation]string{
	types.LegalRelationReplaces: "thay thế",
	types.LegalRelationExpires:  "làm hết hiệu lực",
}

// derivedExpiry reconciles a stale portal status with the catalogue's own
// links: the source has no expiry date and does not say the document ended,
// but an incoming replaces/expires link points at a document already in
// force (EffectiveOn <= now). It returns the earliest such document and its
// effective date, the day this one ended; nil when there is none. vbpl
// still listed 29/2018/QH14 as "Còn hiệu lực" months after 117/2025/QH15
// replaced it (2026-10-16).
func derivedExpiry(rep *types.LegalStatusReport, now time.Time) (*types.LegalDocument, *time.Time) {
	if rep == nil || rep.Document == nil || rep.Document.ExpiredOn != nil || !derivableStatus(rep.Document.Status) {
		return nil, nil
	}
	var by *types.LegalDocument
	for _, l := range rep.Links {
		if !l.Incoming || !derivedExpiryRelations[l.Relation] || l.Other == nil || l.Other.EffectiveOn == nil {
			continue
		}
		if l.Other.EffectiveOn.After(now) {
			continue
		}
		if by == nil || l.Other.EffectiveOn.Before(*by.EffectiveOn) {
			by = l.Other
		}
	}
	if by == nil {
		return nil, nil
	}
	return by, by.EffectiveOn
}

// derivedRelation is the relation by which `by` ended rep's document.
func derivedRelation(rep *types.LegalStatusReport, by *types.LegalDocument) types.LegalRelation {
	for _, l := range rep.Links {
		if l.Other == by && l.Incoming && derivedExpiryRelations[l.Relation] {
			return l.Relation
		}
	}
	return types.LegalRelationReplaces
}

// legalShortLabel names a document by type and number ("Luật 117/2025/QH15"),
// falling back to legalDocLabel.
func legalShortLabel(doc *types.LegalDocument) string {
	if num := strings.TrimSpace(doc.DocumentNumber); num != "" {
		return strings.TrimSpace(doc.DocType + " " + num)
	}
	return legalDocLabel(doc)
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
