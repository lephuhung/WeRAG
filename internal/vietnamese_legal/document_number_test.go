package vietnamese_legal

import (
	"reflect"
	"testing"
)

func TestFindDocumentNumbers(t *testing.T) {
	cases := map[string][]string{
		"Nghị định 53/2022/NĐ-CP quy định gì?":               {"53/2022/NĐ-CP"},
		"theo Thông tư số 01/2024/TT-BCA và 12/2024/QĐ-UBND": {"01/2024/TT-BCA", "12/2024/QĐ-UBND"},
		"Luật 104/2016/QH13, ngày 15/11/2018.":               {"104/2016/QH13"},
		"nghị định 53/2022/nđ-cp và 53/2022/ND-CP":           {"53/2022/nđ-cp"},
		"Thông tư liên tịch 02/2015/TTLT-BTP-BTC.":           {"02/2015/TTLT-BTP-BTC"},
		"số 1153/2022/NĐ-CP":                                 {"1153/2022/NĐ-CP"},
		"Nghị định 53 về an ninh mạng, ngày 15/08/2022":      nil,
		"Nghị quyết 30/2023/NQ-HĐND tỉnh":                    {"30/2023/NQ-HĐND"},
		"văn bản 5/2020/":                                    nil,
	}
	for in, want := range cases {
		if got := FindDocumentNumbers(in); !reflect.DeepEqual(got, want) {
			t.Errorf("FindDocumentNumbers(%q) = %q, want %q", in, got, want)
		}
	}
	n, start, end := FindDocumentNumberIndex("Hỏi về Nghị định số 53/2022/NĐ-CP nhé")
	if n != "53/2022/NĐ-CP" || start < 0 || end <= start {
		t.Fatalf("index = %q %d %d", n, start, end)
	}
	if n, s, _ := FindDocumentNumberIndex("không có"); n != "" || s != -1 {
		t.Fatalf("no number: %q %d", n, s)
	}
}

func TestIsNormativeDocumentNumber(t *testing.T) {
	for number, want := range map[string]bool{
		"53/2022/NĐ-CP": true, "01/2024/TT-BCA": true, "02/2015/TTLT-BTP-BTC": true, "12/2024/QĐ-UBND": true,
		"30/2023/NQ-HĐND": true, "01/2020/NQLT-CP-UBTWMTTQVN": true, "05/2023/CT-TTg": true, "01/2016/PL-UBTVQH14": true,
		"01/2020/L-CTN": true, "104/2016/QH13": true, "1023/2015/UBTVQH13": true, "12/2024/qđ-ubnd": true,
		"45/2025/TTr-CAT": false, "123/2025/CV-PV01": false, "10/2025/KH-UBND": false, "7/2025/BC-CAT": false,
		"3/2025/TB-VP": false, "2/2025/GM-UBND": false, "1/2025/HD-BTC": false, "53/2022": false,
	} {
		if got := IsNormativeDocumentNumber(number); got != want {
			t.Errorf("IsNormativeDocumentNumber(%q) = %v, want %v", number, got, want)
		}
	}
}

func TestCitedNumberSpans(t *testing.T) {
	text := "Theo Nghị định 132/2023/NĐ-CP, Quyết định 123/QĐ-UBND và công văn 01/GT, 41/VPQH; ngày 15/11/2018; 1153/2022/NĐ–CP."
	var got []string
	for _, s := range CitedNumberSpans(text) {
		got = append(got, text[s[0]:s[1]])
	}
	want := []string{"132/2023/NĐ-CP", "123/QĐ-UBND", "1153/2022/NĐ–CP"}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
