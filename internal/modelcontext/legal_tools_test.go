package modelcontext

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLegalToolOutputsReachTheModel(t *testing.T) {
	const kid = "0b6a4a1e-6f1f-4c1e-9d7e-2f1f5f0b9a11"
	r := NewRegistry(true)

	// find_related_legal_documents: a held document's knowledge ID becomes a
	// dN handle the model can pass to read_document.
	out := r.ModelToolResultForTool("find_related_legal_documents", &types.ToolResult{Success: true,
		Output: `- Nghị định 12/2026/NĐ-CP — quy định chi tiết, hướng dẫn Luật 109/2025/QH15` + "\n" +
			`  - Có trong kho "Nghị định": knowledge_id="` + kid + `"`})
	require.NotContains(t, out, kid, "durable IDs never reach the model")
	require.Regexp(t, `knowledge_id="d\d+"`, out)

	// search_knowledge: the catalogue-only note is rendered from Data.
	out = r.ModelToolResultForTool("search_knowledge", &types.ToolResult{Success: true,
		Data: map[string]interface{}{
			"display_type":       "search_results",
			"mode":               "hybrid",
			"results":            []map[string]interface{}{},
			"legal_related_note": "Văn bản liên quan theo quan hệ pháp lý:\n- Nghị định 65/2013/NĐ-CP (Hết hiệu lực toàn bộ)",
		}})
	require.Contains(t, out, "Nghị định 65/2013/NĐ-CP (Hết hiệu lực toàn bộ)")
}
