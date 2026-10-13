package modelcontext

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestModelOutputCarriesLegalCiteAndNote(t *testing.T) {
	registry := NewRegistry(true)
	out := registry.ModelToolResultForTool("search_document_section", &types.ToolResult{Success: true, Data: map[string]any{
		"display_type":    "knowledge_chunks_list",
		"knowledge_id":    "doc-real",
		"knowledge_title": "Luật 60/2020/QH14",
		"model_note":      "Điều 9 is not a provision of this document.",
		"chunks": []map[string]interface{}{{
			"chunk_id": "chunk-7", "chunk_index": 7, "content": "6. Sửa đổi, bổ sung Điều 9", "role": "amendment",
			"legal_cite": "khoản 6 Điều 1 (sửa đổi Điều 9 Luật A)",
			"source_url": "https://vbpl.vn/van-ban/chi-tiet/142846?go=k6",
		}},
	}})
	require.Contains(t, out, `role="amendment" cite="khoản 6 Điều 1 (sửa đổi Điều 9 Luật A)">`)
	require.NotContains(t, out, "vbpl.vn") // citations name the provision; no links
	require.Contains(t, out, "<note>Điều 9 is not a provision of this document.</note>\n</retrieval>")
}

func TestModelOutputLinksAppendixFiles(t *testing.T) {
	registry := NewRegistry(true)
	out := registry.ModelToolResultForTool("search_document_section", &types.ToolResult{Success: true, Data: map[string]any{
		"display_type":    "knowledge_chunks_list",
		"knowledge_id":    "doc-real",
		"knowledge_title": "Thông tư 148/2026/TT-BTC",
		"chunks": []map[string]interface{}{{
			"chunk_id": "chunk-3", "chunk_index": 3, "content": "theo Phụ lục II ban hành kèm theo Thông tư này",
			"legal_cite":     "Điều 3",
			"appendix_files": []map[string]interface{}{{"name": "Phụ lục II. Nội dung HTCT Xã", "url": "https://files.example/pl2.docx/download"}},
		}},
	}})
	require.Contains(t, out, `<appendix_file name="Phụ lục II. Nội dung HTCT Xã" url="https://files.example/pl2.docx/download"/>`)
}
