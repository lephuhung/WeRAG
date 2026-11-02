package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ToolFindRelatedLegalDocuments walks the relation graph of the platform's
// legal catalogue from one Vietnamese legal document.
const ToolFindRelatedLegalDocuments = "find_related_legal_documents"

var findRelatedLegalDocumentsTool = BaseTool{
	name:        ToolFindRelatedLegalDocuments,
	description: `Find Vietnamese legal documents related to one document by walking the relation graph of the platform's legal catalogue (vbpl.vn, central-government documents), several hops deep, and tell which of them the knowledge bases hold. Use it when the user asks which documents detail or guide a law, what amended a document, its earlier or later versions, or what it implements, and when an answer needs a law's implementing rules; check_legal_status is for one document's validity. Related documents held in a knowledge base carry a handle for read_document or search_document_section; the others are known only from the catalogue: name them, do not quote their content.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "document_number": {
      "type": "string",
      "description": "Số ký hiệu, e.g. \"109/2025/QH15\""
    },
    "title": {
      "type": "string",
      "description": "Title words when no number is known, e.g. \"Luật Thuế thu nhập cá nhân\""
    },
    "mode": {
      "type": "string",
      "enum": ["guidance", "amendments", "history", "basis"],
      "description": "guidance (default): documents detailing or guiding it, transitively; amendments: amending, correcting or consolidating it; history: earlier and later versions; basis: what it is based on or implements"
    },
    "depth": {
      "type": "integer",
      "minimum": 1,
      "maximum": 3,
      "description": "Hops to follow (default: the mode's maximum)"
    },
    "include_expired": {
      "type": "boolean",
      "description": "Also list documents no longer in force (history always does)"
    }
  }
}`),
}

// FindRelatedLegalDocumentsTool wraps the catalogue graph walk.
type FindRelatedLegalDocumentsTool struct {
	BaseTool
	svc interfaces.LegalCatalogService
}

// NewFindRelatedLegalDocumentsTool creates the tool.
func NewFindRelatedLegalDocumentsTool(svc interfaces.LegalCatalogService) *FindRelatedLegalDocumentsTool {
	return &FindRelatedLegalDocumentsTool{BaseTool: findRelatedLegalDocumentsTool, svc: svc}
}

type findRelatedLegalDocumentsInput struct {
	DocumentNumber string `json:"document_number"`
	Title          string `json:"title"`
	Mode           string `json:"mode"`
	Depth          int    `json:"depth"`
	IncludeExpired bool   `json:"include_expired"`
}

func (t *FindRelatedLegalDocumentsTool) fail(msg string) (*types.ToolResult, error) {
	return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
}

// Execute resolves the document and renders the walk.
func (t *FindRelatedLegalDocumentsTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input findRelatedLegalDocumentsInput
	if err := json.Unmarshal(args, &input); err != nil {
		return t.fail(fmt.Sprintf("Failed to parse args: %v", err))
	}
	if t.svc == nil {
		return t.fail("legal catalogue is not available")
	}
	if strings.TrimSpace(input.DocumentNumber) == "" && strings.TrimSpace(input.Title) == "" {
		return t.fail("document_number or title is required")
	}
	mode := types.LegalRelatedMode(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = types.LegalRelatedGuidance
	}
	reports, err := t.svc.Related(ctx, input.DocumentNumber, input.Title, mode, input.Depth, input.IncludeExpired)
	if err != nil {
		return t.fail(fmt.Sprintf("legal catalogue walk failed: %v", err))
	}
	if len(reports) == 0 {
		return &types.ToolResult{
			Success: true,
			Output: "No document in the legal catalogue matches. The catalogue holds central-government " +
				"documents only, so this one is unknown here.",
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
		writeLegalRelatedReport(&b, rep)
	}
	return &types.ToolResult{
		Success: true,
		Output:  b.String(),
		Data:    map[string]interface{}{"matches": len(reports), "reports": reports},
	}, nil
}

// legalRelatedLabels words an edge as seen from the related document:
// incoming = it acts on its parent.
var legalRelatedLabels = map[types.LegalRelation][2]string{
	// {outgoing: the parent acts on it, incoming: it acts on the parent};
	// each reads "<related> — <label> <parent>".
	types.LegalRelationGuides:           {"được quy định chi tiết, hướng dẫn bởi", "quy định chi tiết, hướng dẫn"},
	types.LegalRelationInterprets:       {"được giải thích bởi", "giải thích"},
	types.LegalRelationAmends:           {"được sửa đổi, bổ sung bởi", "sửa đổi, bổ sung"},
	types.LegalRelationCorrects:         {"được đính chính bởi", "đính chính"},
	types.LegalRelationConsolidates:     {"được hợp nhất trong", "hợp nhất"},
	types.LegalRelationReplaces:         {"bị thay thế bởi", "thay thế"},
	types.LegalRelationRepeals:          {"bị bãi bỏ bởi", "bãi bỏ"},
	types.LegalRelationExpires:          {"bị làm hết hiệu lực bởi", "làm hết hiệu lực"},
	types.LegalRelationPartiallyExpires: {"bị làm hết hiệu lực một phần bởi", "làm hết hiệu lực một phần"},
	types.LegalRelationBasedOn:          {"là căn cứ của", "căn cứ vào"},
}

var legalRelatedModeTitles = map[types.LegalRelatedMode]string{
	types.LegalRelatedGuidance:   "Văn bản quy định chi tiết, hướng dẫn",
	types.LegalRelatedAmendments: "Văn bản sửa đổi, bổ sung, đính chính, hợp nhất",
	types.LegalRelatedHistory:    "Các phiên bản trước và sau (thay thế, bãi bỏ, hết hiệu lực)",
	types.LegalRelatedBasis:      "Văn bản là căn cứ, được thi hành",
}

func writeLegalRelatedReport(b *strings.Builder, rep *types.LegalRelatedReport) {
	fmt.Fprintf(b, "## %s\n", legalDocLabel(rep.Document))
	fmt.Fprintf(b, "- Tình trạng: %s\n", legalOr(rep.Document.Status, "chưa xác định"))
	fmt.Fprintf(b, "### %s\n", legalRelatedModeTitles[rep.Mode])
	if len(rep.Related) == 0 {
		b.WriteString("Không có văn bản nào trong danh mục.\n")
		return
	}
	labels := map[string]string{rep.Document.ID: legalDocLabel(rep.Document)}
	held := 0
	for _, r := range rep.Related {
		labels[r.Document.ID] = legalDocLabel(r.Document)
		doc := r.Document
		edge := legalRelatedLabels[r.Relation]
		verb := edge[0]
		if r.Incoming {
			verb = edge[1]
		}
		indent := strings.Repeat("  ", r.Depth-1)
		fmt.Fprintf(b, "%s- %s (ban hành %s; %s) — %s %s\n", indent, legalDocLabel(doc),
			legalDate(doc.IssuedOn), legalOr(doc.Status, "chưa rõ tình trạng"), verb, labels[r.ParentID])
		for _, k := range r.Knowledge {
			held++
			fmt.Fprintf(b, "%s  - Có trong kho \"%s\": knowledge_id=\"%s\"\n", indent, k.KnowledgeBaseName, k.KnowledgeID)
		}
	}
	if rep.Truncated {
		b.WriteString("- … danh sách đã được rút gọn (ưu tiên văn bản còn hiệu lực, mới nhất).\n")
	}
	if held == 0 {
		b.WriteString("Chưa văn bản nào trong số này có trong kho tài liệu: chỉ nêu tên, không trích nội dung.\n")
	}
}
