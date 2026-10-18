package domainpacks

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
)

// paragraphParityFile pins splitParagraphs for scripts/legal_eval/pack_eval.py
// (md_paragraphs), whose --self-check reads it: the eval must cut answers
// exactly as the notice does. Regenerate with UPDATE_PARAGRAPHS=1.
const paragraphParityFile = "testdata/paragraphs.json"

type paragraphCase struct {
	Name       string      `json:"name"`
	Input      string      `json:"input"`
	Paragraphs []paragraph `json:"paragraphs"`
}

func paragraphCases() []paragraphCase {
	inputs := []struct{ name, input string }{
		{"plain", "Một.\nHai dòng.\n\nBa."},
		{"heading joins next", "### 2. Điều 20 khoản 8 là gì?\n\nTheo dự thảo Lần 2, …"},
		{"two headings then text", "# A\n## B\n\nNội dung."},
		{"trailing heading", "Nội dung.\n\n## Cuối"},
		{"list with labelled lead-in", "Theo dự thảo:\n\n- bỏ Chủ tịch\n- bỏ kế toán trưởng\n\nHết."},
		{"list with unlabelled lead-in", "Tóm tắt:\n- Ý một\n1. Ý hai\n2) Ý ba"},
		{"lead-in without colon", "Tóm tắt\n- Ý một"},
		{"heading opens a list", "## Điểm mới\n\n- x\n- y\n\nĐoạn sau."},
		{"list item continuation", "- mục một\n  tiếp tục\n- mục hai"},
		{"table", "Bảng:\n| a | b |\n|---|---|\n| c | d |"},
		{"NFD text", norm.NFD.String("Theo dự thảo:\n- Luật Phát triển DNNVV")},
		{"blank lines and spaces", "\n\n  \nA\n\n\nB\n"},
	}
	out := make([]paragraphCase, 0, len(inputs))
	for _, in := range inputs {
		ps := splitParagraphsDetailed(in.input)
		if ps == nil {
			ps = []paragraph{}
		}
		out = append(out, paragraphCase{Name: in.name, Input: in.input, Paragraphs: ps})
	}
	return out
}

func TestParagraphParityFile(t *testing.T) {
	want, err := json.MarshalIndent(paragraphCases(), "", " ")
	require.NoError(t, err)
	want = append(want, '\n')
	if os.Getenv("UPDATE_PARAGRAPHS") == "1" {
		require.NoError(t, os.WriteFile(paragraphParityFile, want, 0o644))
	}
	got, err := os.ReadFile(paragraphParityFile)
	require.NoError(t, err, "run with UPDATE_PARAGRAPHS=1 to create it")
	require.Equal(t, string(want), string(got), "splitParagraphs changed: rerun with UPDATE_PARAGRAPHS=1 and check pack_eval md_paragraphs")
}
