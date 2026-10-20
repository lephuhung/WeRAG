package vietnamese_legal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// citeSummary is "kind number key doctype" for one citation, the name for
// a name-only one.
func citeSummary(c Citation) string {
	if c.Kind == CitationName {
		return "name " + c.Name
	}
	s := string(c.Kind) + " " + c.Number + " " + c.Key + " " + c.DocType
	if c.Name != "" {
		s += " | " + c.Name
	}
	return strings.TrimSpace(s)
}

func TestFindCitations(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// research §2.1 failure classes
		{"Nghị định số 68/2026/NĐ-CP quy định gì?", []string{"full 68/2026/NĐ-CP 68/2026/ND-CP Nghị định"}},
		{"theo 68-2026-NĐ-CP", []string{"full 68/2026/NĐ-CP 68/2026/ND-CP Nghị định"}},
		{"Thông tư số 01/2020/TT- BNNPTNT", []string{"full 01/2020/TT-BNNPTNT 01/2020/TT-BNNPTNT Thông tư"}},
		{"Thông tư số 01/2020/TT -BNNPTNT", []string{"full 01/2020/TT-BNNPTNT 01/2020/TT-BNNPTNT Thông tư"}},
		{"Thông tư 01 / 2020 / TT-BNNPTNT", []string{"full 01/2020/TT-BNNPTNT 01/2020/TT-BNNPTNT Thông tư"}},
		{"Nghị định 36/2017/NĐ - CP về", []string{"full 36/2017/NĐ-CP 36/2017/ND-CP Nghị định"}},
		{"Nghị định 53/2022/NÐ-CP", []string{"full 53/2022/NĐ-CP 53/2022/ND-CP Nghị định"}},
		{"Quyết định 25/1999/QÐ-BTC.", []string{"full 25/1999/QĐ-BTC 25/1999/QD-BTC Quyết định"}},
		{"Nghị định 53/2022/NĐ–CP", []string{"full 53/2022/NĐ-CP 53/2022/ND-CP Nghị định"}},
		{"Nghị định 100/2015/NĐ-СР", []string{"full 100/2015/NĐ-CP 100/2015/ND-CP Nghị định"}}, // Cyrillic С, Р
		{"Nghị định 53/2022 NĐ-CP", []string{"full 53/2022/NĐ-CP 53/2022/ND-CP Nghị định"}},
		{"nghị định 53/2022/nđ-cp", []string{"full 53/2022/NĐ-CP 53/2022/ND-CP Nghị định"}},
		{"Nghị định 53/2O22/NĐ-CP", []string{"full 53/2022/NĐ-CP 53/2022/ND-CP Nghị định"}},
		{"QCVN 06:2022/BXD về an toàn cháy", []string{"standard QCVN 06:2022/BXD QCVN06:2022/BXD"}},
		{"theo QCVN 01-1:2018/BYT", []string{"standard QCVN 01-1:2018/BYT QCVN01-1:2018/BYT"}},
		{"QTKĐ 12-2017/BCT", []string{"standard QTKĐ 12-2017/BCT QTKD12-2017/BCT"}},
		{"Theo Quyết định 1461/QĐ-TTg, Bộ", []string{"admin 1461/QĐ-TTg 1461/QD-TTG Quyết định"}},
		{"Công văn 456/BTC-TCT", []string{"admin 456/BTC-TCT 456/BTC-TCT"}},
		{"Nghị quyết số 66.12/2026/NQ-CP", []string{"full 66.12/2026/NQ-CP 66.12/2026/NQ-CP Nghị quyết"}},
		{"Chỉ thị 05-CT/TW và 36-HD/VPTW", []string{"party 05-CT/TW 05-CT/TW Chỉ thị", "party 36-HD/VPTW 36-HD/VPTW Hướng dẫn"}},
		{"NĐ 68/2026 quy định gì?", []string{"partial 68/2026  Nghị định"}},
		{"Nghị định 68 về thuế", []string{"partial 68  Nghị định"}},
		{"Luật 59/2024 sửa đổi", []string{"partial 59/2024  Luật"}},
		{"Luật số 59/2024/QH15", []string{"full 59/2024/QH15 59/2024/QH15 Luật"}},
		{"thể thức NĐ30", []string{"partial 30  Nghị định"}},
		{"Thông tư liên tịch số 05", []string{"partial 05  Thông tư liên tịch"}},
		{"Nghị quyết 12 năm 2026", []string{"partial 12/2026  Nghị quyết"}},
		{"Luật Đấu thầu", []string{"name Luật Đấu thầu"}},
		{"Bộ luật Tố tụng hình sự quy định", []string{"name Bộ luật Tố tụng hình sự"}},
		{"sửa đổi Luật Khám bệnh, chữa bệnh; bổ sung", []string{"name Luật Khám bệnh, chữa bệnh"}},
		{"Luật Đầu tư công, Luật Đấu thầu và Luật Nhà ở", []string{"name Luật Đầu tư công", "name Luật Đấu thầu", "name Luật Nhà ở"}},
		{"Luật Bình đẳng giới số 73/2006/QH11", []string{"full 73/2006/QH11 73/2006/QH11 Luật | Luật Bình đẳng giới"}},
		{"Luật Bảo vệ sức khỏe nhân dân số 21-LCT/HĐNN8", []string{"party 21-LCT/HĐNN8 21-LCT/HDNN8 Luật | Luật Bảo vệ sức khỏe nhân dân"}},
		{"Nghị định 53/2022/NĐ-CP và 1153/2022/NĐ-CP", []string{"full 53/2022/NĐ-CP 53/2022/ND-CP Nghị định", "full 1153/2022/NĐ-CP 1153/2022/ND-CP Nghị định"}},
		// negatives
		{"ngày 15/11/2018", nil},
		{"ký ngày 15/11/2018 tại Hà Nội", nil},
		{"phạt tiền từ 1.000.000 đồng đến 2.000.000 đồng", nil},
		{"khoản 2 Điều 5", nil},
		{"Điều 5. Phạm vi", nil},
		{"gọi 0912/345 hoặc 0912 345 678", nil},
		{"Mẫu số 01/GLP", nil},
		{"theo quy định của pháp luật 1. Đối tượng", nil},
		{"Luật này có hiệu lực", nil},
		{"trong thời hạn quyết định 15 ngày", nil},
		{"giai đoạn 2021-2025", nil},
		{"văn bản 5/2020/", nil},
	}
	for _, c := range cases {
		var got []string
		for _, cit := range FindCitations(c.in) {
			got = append(got, citeSummary(cit))
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("FindCitations(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestFindCitationsSpans(t *testing.T) {
	in := "Theo NĐ 68/2026 và Thông tư số 01/2020/TT- BNNPTNT, ngày 15/11/2018"
	cits := FindCitations(in)
	if len(cits) != 2 {
		t.Fatalf("got %+v", cits)
	}
	for _, c := range cits {
		if in[c.Start:c.End] != c.Raw {
			t.Errorf("span %d:%d = %q, raw %q", c.Start, c.End, in[c.Start:c.End], c.Raw)
		}
	}
	if cits[0].Raw != "NĐ 68/2026" || cits[1].Raw != "01/2020/TT- BNNPTNT" {
		t.Errorf("raw = %q, %q", cits[0].Raw, cits[1].Raw)
	}
	// offsets stay in the original text across folded runes of other widths
	in = "QĐ–TTg: Nghị định 53/2022/NÐ–CP."
	cits = FindCitations(in)
	if len(cits) != 1 || cits[0].Raw != "53/2022/NÐ–CP" || in[cits[0].Start:cits[0].End] != cits[0].Raw {
		t.Fatalf("got %+v", cits)
	}
}

func TestNormalizeCitationNumber(t *testing.T) {
	for in, want := range map[string]string{
		"68-2026-NĐ-CP":              "68/2026/NĐ-CP",
		"01/2020/TT- BNNPTNT":        "01/2020/TT-BNNPTNT",
		"53/2022/NÐ–CP":              "53/2022/NĐ-CP",
		"nghị định số 53/2022/nđ-cp": "53/2022/NĐ-CP",
		"1461/QĐ-TTg":                "1461/QĐ-TTg",
		"Nghị định 68":               "",
		"ngày 15/11/2018":            "",
	} {
		if got := NormalizeCitationNumber(in); got != want {
			t.Errorf("NormalizeCitationNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

// goldNumberRe takes the số hiệu a gold label writes: after the last "số",
// else from the first digit (standards keep their prefix).
var goldNumberRe = regexp.MustCompile(`(?:QCVN|TCVN|QTKĐ)\s*\S+$|[^ ]*\d\S*(?:\s*-?\s*[A-ZĐ]+)?$`)

// goldHasNumberRe tells a numbered label from a name that holds a digit
// ("… tủ phân phối 6 kV phòng nổ").
var goldHasNumberRe = regexp.MustCompile(`\d\s*[/:-]|số \d`)

// TestFindCitationsGold runs the recogniser over the target documents
// hand-labelled in scripts/legal_amend_kg/data: every label that writes a
// number must yield a citation with the key of that number.
func TestFindCitationsGold(t *testing.T) {
	pattern := "../../scripts/legal_amend_kg/data/*gold*.jsonl"
	if p := os.Getenv("GOLD_FILES"); p != "" {
		pattern = p // e.g. a held-out set kept outside the repo
	}
	files, _ := filepath.Glob(pattern)
	if len(files) == 0 {
		t.Skip("no gold files")
	}
	seen := map[string]bool{}
	numbered, hit, names, nameHit := 0, 0, 0, 0
	var dump []string
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var row struct {
				Ops []struct {
					Doc string `json:"doc"`
				} `json:"ops"`
			}
			if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			for _, op := range row.Ops {
				doc := strings.TrimSpace(op.Doc)
				if doc == "" || seen[doc] {
					continue
				}
				seen[doc] = true
				cits := FindCitations(doc)
				if !goldHasNumberRe.MatchString(doc) {
					names++
					for _, c := range cits {
						if c.Kind == CitationName {
							nameHit++
							break
						}
					}
					continue
				}
				numbered++
				written := doc
				if i := strings.LastIndex(doc, " số "); i >= 0 {
					written = doc[i+len(" số "):]
				} else if m := goldNumberRe.FindString(doc); m != "" {
					written = m
				}
				wantKey := CatalogNumberKey(written)
				ok := false
				for _, c := range cits {
					if c.Key == wantKey || (c.Kind == CitationPartial && c.Number == written) {
						ok = true
						dump = append(dump, c.Key)
					}
				}
				if ok {
					hit++
				} else {
					t.Errorf("gold %q: want key %q, got %+v", doc, wantKey, cits)
				}
			}
		}
		fh.Close()
	}
	t.Logf("gold: %d/%d numbered labels recognised with their key; %d/%d name-only labels read as a name", hit, numbered, nameHit, names)
	if out := os.Getenv("GOLD_KEYS_OUT"); out != "" {
		_ = os.WriteFile(out, []byte(strings.Join(dump, "\n")+"\n"), 0o644)
	}
}
