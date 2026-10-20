package vietnamese_legal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"
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
	runeSpan := func(in string, c Citation) string { return string([]rune(in)[c.Start:c.End]) }
	in := "Theo NĐ 68/2026 và Thông tư số 01/2020/TT- BNNPTNT, ngày 15/11/2018"
	cits := FindCitations(in)
	if len(cits) != 2 {
		t.Fatalf("got %+v", cits)
	}
	for _, c := range cits {
		if runeSpan(in, c) != c.Raw {
			t.Errorf("span %d:%d = %q, raw %q", c.Start, c.End, runeSpan(in, c), c.Raw)
		}
	}
	if cits[0].Raw != "NĐ 68/2026" || cits[1].Raw != "01/2020/TT- BNNPTNT" {
		t.Errorf("raw = %q, %q", cits[0].Raw, cits[1].Raw)
	}
	// offsets stay in the original text across folded runes of other widths
	in = "QĐ–TTg: Nghị định 53/2022/NÐ–CP."
	cits = FindCitations(in)
	if len(cits) != 1 || cits[0].Raw != "53/2022/NÐ–CP" || runeSpan(in, cits[0]) != cits[0].Raw {
		t.Fatalf("got %+v", cits)
	}
	// review #9: decomposed input (NFD) — offsets are runes of the input as given
	in = norm.NFD.String("Căn cứ Nghị định số 68/2026/NĐ-CP và Luật Đấu thầu.")
	cits = FindCitations(in)
	if len(cits) != 2 {
		t.Fatalf("NFD: got %+v", cits)
	}
	for _, c := range cits {
		if runeSpan(in, c) != c.Raw || norm.NFC.String(c.Raw) == "" {
			t.Errorf("NFD span %d:%d = %q, raw %q", c.Start, c.End, runeSpan(in, c), c.Raw)
		}
	}
	if norm.NFC.String(cits[0].Raw) != "68/2026/NĐ-CP" || norm.NFC.String(cits[1].Raw) != "Luật Đấu thầu" {
		t.Errorf("NFD raw = %q, %q", cits[0].Raw, cits[1].Raw)
	}
}

// Review findings 3, 5–8, 11: each scenario as reported.
func TestFindCitationsReview(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// 3: real names are not cut at "không", "hiện", "có", "theo"
		{"Luật Hàng không dân dụng Việt Nam;", []string{"name Luật Hàng không dân dụng Việt Nam"}},
		{"theo Luật Thực hiện dân chủ ở cơ sở.", []string{"name Luật Thực hiện dân chủ ở cơ sở"}},
		{"Luật Phòng không nhân dân.", []string{"name Luật Phòng không nhân dân"}},
		{"Pháp lệnh Ưu đãi người có công với cách mạng;", []string{"name Pháp lệnh Ưu đãi người có công với cách mạng"}},
		{"Luật Đầu tư theo phương thức đối tác công tư;", []string{"name Luật Đầu tư theo phương thức đối tác công tư"}},
		{"Luật Đấu thầu quy định", []string{"name Luật Đấu thầu"}},
		// 5: a tail after a second slash stays in the key
		{"Thông tư liên tịch 44/2007/TTLT/BTC-BKH&CN.", []string{"full 44/2007/TTLT/BTC-BKH&CN 44/2007/TTLT/BTC-BKH&CN Thông tư liên tịch"}},
		// 6: a partial with a year, followed by ngày / điều / khoản
		{"Nghị định 86/2013 ngày 29/7/2013", []string{"partial 86/2013  Nghị định"}},
		{"Nghị định 100/2019 điều 5 quy định gì", []string{"partial 100/2019  Nghị định"}},
		{"NĐ 100/2019 khoản 2", []string{"partial 100/2019  Nghị định"}},
		{"Theo NĐ 15 điều 3", []string{"partial 15  Nghị định"}},
		// 7: quantities and years are not numbers
		{"Tòa án quyết định 3 năm tù", nil},
		{"Hội đồng quyết định 2 phương án", nil},
		{"nghị quyết 3 nội dung", nil},
		{"thông tư 2 bản", nil},
		{"Bộ luật 2015 quy định", nil},
		{"Luật 2013 và Nghị định 2020", nil},
		{"quyết định số 3 của UBND", []string{"partial 3  Quyết định"}},
		// 8: a dash with spaces after a full code is not part of it
		{"Quyết định 15/2023/QĐ-UBND - UBND tỉnh Huế", []string{"full 15/2023/QĐ-UBND 15/2023/QD-UBND Quyết định"}},
		{"Quyết định 123/QĐ-UBND - UBND tỉnh", []string{"admin 123/QĐ-UBND 123/QD-UBND Quyết định"}},
		// re-review C: lower-case chat questions; only verb-like types need "số" or a year
		{"nghị định 53 quy định gì", []string{"partial 53  Nghị định"}},
		{"thông tư 15 của bộ công an", []string{"partial 15  Thông tư"}},
		{"thông tư liên tịch 02 còn hiệu lực không", []string{"partial 02  Thông tư liên tịch"}},
		{"pháp lệnh 3 về gì", []string{"partial 3  Pháp lệnh"}},
		{"nghị định 53/2022 thay thế văn bản nào", []string{"partial 53/2022  Nghị định"}},
		{"quyết định 1461 năm 2022", []string{"partial 1461/2022  Quyết định"}},
		{"chỉ thị 05 về phòng dịch", nil},
		// re-review NIT 1: no slash tail after a hyphenated code
		{"Nghị định 01/2021/NĐ-CP/NĐ", []string{"full 01/2021/NĐ-CP 01/2021/ND-CP Nghị định"}},
		// re-review A: a name stops where a capitalised type starts
		{"Căn cứ Luật Đất đai Nghị định 43/2014/NĐ-CP", []string{"name Luật Đất đai", "full 43/2014/NĐ-CP 43/2014/ND-CP Nghị định"}},
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
	// 11: the law named inside an amending law's title is not cited as itself
	cits := FindCitations("Luật sửa đổi, bổ sung một số điều của Luật Đất đai số 43/2024/QH15")
	if len(cits) != 1 || cits[0].Kind != CitationFull || cits[0].Number != "43/2024/QH15" {
		t.Fatalf("amending law with its number: %+v", cits)
	}
	cits = FindCitations("Căn cứ Luật sửa đổi, bổ sung một số điều của Luật Đất đai;")
	if len(cits) != 1 || cits[0].Name != "Luật Đất đai" || cits[0].Role != CitationRoleInAmendingTitle {
		t.Fatalf("amending title: %+v", cits)
	}
	// re-review D: the role carries along a list joined by "," and "và"
	cits = FindCitations("Căn cứ Luật sửa đổi, bổ sung một số điều của Luật Quy hoạch, Luật Đầu tư và Luật Đấu thầu; Luật Nhà ở.")
	if len(cits) != 4 {
		t.Fatalf("list: %+v", cits)
	}
	for i, want := range []string{CitationRoleInAmendingTitle, CitationRoleInAmendingTitle, CitationRoleInAmendingTitle, ""} {
		if cits[i].Role != want {
			t.Errorf("list %d %q role %q, want %q", i, cits[i].Name, cits[i].Role, want)
		}
	}
	cits = FindCitations("Điều 1. Sửa đổi, bổ sung một số điều của Luật Đất đai như sau:")
	if len(cits) != 1 || cits[0].Role != CitationRoleAmendTarget {
		t.Fatalf("amend target: %+v", cits)
	}
	if cits := FindCitations("theo Luật Đất đai."); len(cits) != 1 || cits[0].Role != "" {
		t.Fatalf("plain citation: %+v", cits)
	}
}

// review #12: linear on a ~1 MB text
func BenchmarkFindCitationsMegabyte(b *testing.B) {
	unit := "Căn cứ Luật Đấu thầu, Luật Hàng không dân dụng Việt Nam và Nghị định số 68/2026/NĐ-CP ngày 15/11/2018; quyết định 3 năm tù, khoản 2 Điều 5. "
	text := strings.Repeat(unit, 1<<20/len(unit))
	b.SetBytes(int64(len(text)))
	for i := 0; i < b.N; i++ {
		if len(FindCitations(text)) == 0 {
			b.Fatal("no citations")
		}
	}
}

func TestFindCitationsMegabyteIsFast(t *testing.T) {
	unit := "Căn cứ Luật Đấu thầu, Luật Hàng không dân dụng Việt Nam và Nghị định số 68/2026/NĐ-CP ngày 15/11/2018; quyết định 3 năm tù, khoản 2 Điều 5. "
	text := strings.Repeat(unit, 1<<20/len(unit))
	start := time.Now()
	n := len(FindCitations(text))
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("1 MB took %v", d)
	}
	if n < 3*(1<<20/len(unit)) {
		t.Fatalf("found %d", n)
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
