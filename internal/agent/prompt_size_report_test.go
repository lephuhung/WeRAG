package agent

import (
	"fmt"
	"os"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
)

// TestPromptSizeReport prints the size of each runtime system-prompt section
// and of the knowledge-base directory for the document assistant's tool set.
// A measuring aid, not an assertion:
//
//	PROMPT_SIZE_REPORT=1 go test ./internal/agent -run TestPromptSizeReport -v
func TestPromptSizeReport(t *testing.T) {
	if os.Getenv("PROMPT_SIZE_REPORT") == "" {
		t.Skip("set PROMPT_SIZE_REPORT=1 to print prompt section sizes")
	}
	names := []string{"apply_format_fixes", "check_document_format", "check_legal_status", "check_spelling",
		"discover_mcp_tools", "draw_diagram", "find_in_documents", "find_related_legal_documents",
		"generate_illustration", "insert_paragraphs", "mark_passages", "query_knowledge_graph", "read_document",
		"read_document_outline", "resolve_abbreviation", "rewrite_paragraphs", "search_document_section",
		"search_knowledge", "web_search", "web_fetch"}
	protocol := modelcontext.NewRegistry(true).ProtocolPrompt()
	sections := BuildSystemPromptSections(nil, true, &BuildSystemPromptOptions{
		SelectedTools: names, ProtocolPrompt: protocol,
	}, "base")
	for _, s := range sections {
		fmt.Printf("section %-18s chars=%5d\n", s.Name, utf8.RuneCountInString(s.Content))
	}
	fmt.Printf("section %-18s chars=%5d\n", "steering", utf8.RuneCountInString(steerGuidance))

	kbs := make([]*KnowledgeBaseInfo, 0, 6)
	for i := 0; i < 6; i++ {
		kbs = append(kbs, &KnowledgeBaseInfo{
			ID: fmt.Sprintf("kb-%d", i), Name: fmt.Sprintf("Văn bản pháp luật nhóm %d", i), Type: "document",
			DocCount: 120, Capabilities: []string{"vector", "keyword", "graph"},
			Description: "Kho văn bản quy phạm pháp luật của trung ương và địa phương về lĩnh vực hành chính, đất đai, " +
				"xây dựng, tài chính, cán bộ công chức, kèm hướng dẫn thi hành và văn bản sửa đổi bổ sung còn hiệu lực.",
			Profile: &types.KnowledgeBaseProfile{
				Gist:   "Tập hợp luật, nghị định, thông tư và quyết định về quản lý nhà nước, thủ tục hành chính và tổ chức bộ máy.",
				Topics: []string{"thủ tục hành chính", "đất đai", "xây dựng", "tài chính công", "cán bộ công chức", "tổ chức bộ máy"},
				TypicalQuestions: []string{"Thời hạn giải quyết thủ tục cấp giấy chứng nhận là bao lâu?",
					"Điều kiện bổ nhiệm công chức lãnh đạo?", "Mức phạt vi phạm hành chính trong xây dựng?"},
			},
		})
	}
	full := formatKnowledgeBaseDirectory(kbs, false)
	compact := formatKnowledgeBaseDirectory(kbs, true)
	fmt.Printf("kb_directory full    chars=%5d\n", utf8.RuneCountInString(full))
	fmt.Printf("kb_directory compact chars=%5d\n", utf8.RuneCountInString(compact))
}
