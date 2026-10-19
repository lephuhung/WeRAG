package vietnamese_legal

import (
	"encoding/json"
	"os"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// documentNumberParityFile pins FindDocumentNumbers for
// scripts/legal_eval/pack_eval.py, whose --self-check reads it: the eval
// must see the same số hiệu as the gate, the cited_in scanner and the
// answer number check. Regenerate with UPDATE_DOCUMENT_NUMBERS=1.
const documentNumberParityFile = "testdata/document_numbers.json"

type documentNumberCase struct {
	Input   string   `json:"input"`
	Numbers []string `json:"numbers"`
	Keys    []string `json:"keys"`
}

func documentNumberCases() []documentNumberCase {
	inputs := []string{
		"Nghị định 53/2022/NĐ-CP và Thông tư 01/2024/TT-BCA.",
		"Công văn 01/GT và 41/VPQH không phải số hiệu đầy đủ.",
		"Ngày 15/11/2018 không phải số hiệu.",
		"Luật 59/2020/QH14, 59/2020/qh14 (trùng khoá).",
		"Số 1153/2022/NĐ-CP không phải 53/2022/NĐ-CP; 12345/2022/NĐ-CP.",
		"Gạch khác: 168/2025/NĐ–CP, 76/2025/QH15.",
		"Quyết định 12/2024/QĐ-UBND tỉnh; nghị quyết 198/2025/QH15.",
		"Văn bản 123/QĐ-UBND của tỉnh (không có năm).",
		norm.NFD.String("Thông tư 58/2026/TT-BTC (NFD)."),
		"x58/2026/TT-BTC/y và 58/2026/TT-BTC.",
	}
	out := make([]documentNumberCase, 0, len(inputs))
	for _, in := range inputs {
		nums := FindDocumentNumbers(in)
		keys := make([]string, 0, len(nums))
		for _, n := range nums {
			keys = append(keys, CatalogNumberKey(n))
		}
		if nums == nil {
			nums = []string{}
		}
		out = append(out, documentNumberCase{Input: in, Numbers: nums, Keys: keys})
	}
	return out
}

func TestDocumentNumberParityFile(t *testing.T) {
	want, err := json.MarshalIndent(documentNumberCases(), "", " ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("UPDATE_DOCUMENT_NUMBERS") == "1" {
		if err := os.WriteFile(documentNumberParityFile, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(documentNumberParityFile)
	if err != nil {
		t.Fatalf("%v (run with UPDATE_DOCUMENT_NUMBERS=1)", err)
	}
	if string(got) != string(want) {
		t.Fatal("FindDocumentNumbers changed: rerun with UPDATE_DOCUMENT_NUMBERS=1 and check pack_eval")
	}
}
