package legalcatalog

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

func twinDay(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }

// imported builds an import row the way cmd/legal-catalog-import does.
func imported(id, soKyHieu, docType, title, issued string) *types.LegalDocument {
	num := CleanVBPLNumber(soKyHieu)
	return &types.LegalDocument{ID: id, DocumentNumber: num, NumberKey: vietnamese_legal.CatalogNumberKey(num),
		DocType: docType, Title: title, IssuedOn: twinDay(issued)}
}

func catalogued(id, number, key, docType, title, issued string) *types.LegalDocument {
	return &types.LegalDocument{ID: id, DocumentNumber: number, NumberKey: key, DocType: docType, Title: title, IssuedOn: twinDay(issued)}
}

func TestResolveTwins(t *testing.T) {
	now := time.Now()
	existing := []*types.LegalDocument{
		catalogued("22897", "136/2001/QĐ-BTC", "136/2001/QD-BTC", "Quyết định", "Quyết định số 136/2001/QĐ-BTC Về việc bổ sung Danh mục", "2001-12-18"),
		// same document, left with its Ð key; the title words differ
		catalogued("109088", "136/2001/QÐ-BTC", "136/2001/QÐ-BTC", "Quyết định", "Quyết định số 136/2001/QÐ-BTC của bộ trưởng bộ tài chính về việc bổ sung danh mục", "2001-12-18"),
		catalogued("61017", "17-TC/TCĐN", "17-TC/TCDN", "Thông tư", "Thông tư số 17-TC/TCĐN Hướng dẫn việc thu nộp lệ phí", "1989-05-23"),
		// an older number reused by another document: not the same one
		catalogued("109500", "17-TC/TCÐN", "17-TC/TCÐN", "Thông tư", "Thông tư số 17-TC/TCÐN hướng dẫn cơ chế quản lý tài chính", "1997-04-02"),
		// Ð with no twin (re-written by an import that did not fold)
		catalogued("142783", "02/2020/TT-BGDÐT", "02/2020/TT-BGDÐT", "Thông tư", "Thông tư số 02/2020/TT-BGDÐT Quy định", "2020-02-05"),
		// Cyrillic С, a Latin twin with "số" dropped and an OCR typo
		catalogued("abace830", "110/2025/TT-BTC", "110/2025/TT-BTC", "Thông tư", "Thông tư 110/2025/TT-BTC Quy định mức thu, chế độ thu, nộp phí thẩm định", "2025-11-19"),
		catalogued("185841", "110/2025/TT-BTС", "110/2025/TT-BTС", "Thông tư", "Thông tư số 110/2025/TT-BTС Quy định mức thu, chế độ thu, nộp phí thầm định", "2025-11-19"),
		// Cyrillic, a Latin row of another document
		catalogued("900", "51/2025/TT-BTC", "51/2025/TT-BTC", "Thông tư", "Thông tư số 51/2025/TT-BTC Hướng dẫn kế toán", "2025-01-02"),
		catalogued("179043", "51/2025/TT-BTС", "51/2025/TT-BTС", "Thông tư", "Thông tư số 51/2025/TT-BTС Quy định phí", "2025-06-24"),
		// a reviewed Cyrillic row is not hidden behind an unreviewed holder
		catalogued("700", "65/2025/TT-BTC", "65/2025/TT-BTC", "Thông tư", "Thông tư số 65/2025/TT-BTC Sửa đổi", "2025-06-30"),
		{ID: "179086", DocumentNumber: "65/2025/TT-BTС", NumberKey: "65/2025/TT-BTС", DocType: "Thông tư",
			Title: "Thông tư số 65/2025/TT-BTС Sửa đổi", IssuedOn: twinDay("2025-06-30"), ReviewedAt: &now},
	}
	docs := []*types.LegalDocument{
		imported("22897", "136/2001/QĐ-BTC", "Quyết định", "Quyết định số 136/2001/QĐ-BTC Về việc bổ sung Danh mục", "2001-12-18"),
		imported("109088", "136/2001/QÐ-BTC", "Quyết định", "Quyết định số 136/2001/QÐ-BTC của bộ trưởng bộ tài chính về việc bổ sung danh mục", "2001-12-18"),
		imported("109500", "17-TC/TCÐN", "Thông tư", "Thông tư số 17-TC/TCÐN hướng dẫn cơ chế quản lý tài chính", "1997-04-02"),
		imported("142783", "02/2020/TT-BGDÐT", "Thông tư", "Thông tư số 02/2020/TT-BGDÐT Quy định", "2020-02-05"),
		imported("185841", "110/2025/TT-BTС", "Thông tư", "Thông tư số 110/2025/TT-BTС Quy định mức thu, chế độ thu, nộp phí thầm định", "2025-11-19"),
		imported("179043", "51/2025/TT-BTС", "Thông tư", "Thông tư số 51/2025/TT-BTС Quy định phí", "2025-06-24"),
		imported("179086", "65/2025/TT-BTС", "Thông tư", "Thông tư số 65/2025/TT-BTС Sửa đổi", "2025-06-30"),
		// a new vbpl id for a catalogued document, written with Ð
		imported("200001", "Quyết định số: 136/2001/QÐ-BTC", "Quyết định", "Quyết định số 136/2001/QÐ-BTC V/v bổ sung danh mục", "2001-12-18"),
		// a new id under a known key, another document (other day)
		imported("200002", "136/2001/QĐ-BTC", "Quyết định", "Quyết định số 136/2001/QĐ-BTC Về việc khác", "2002-12-18"),
		// two new ids of one new document in the same import
		imported("200003", "5/2026/QĐ-UBND", "Quyết định", "Quyết định số 5/2026/QĐ-UBND về phí", "2026-02-01"),
		imported("200004", "5/2026/QÐ-UBND", "Quyết định", "Quyết định 5/2026/QÐ-UBND Về phí", "2026-02-01"),
		// an English copy of the portal: same number and day, not aliased
		imported("200005", "136/2001/QD-BTC", "Quyết định", "Quyết định số 136/2001/QD-BTC The Decision on adding to the list", "2001-12-18"),
	}
	res := ResolveTwins(existing, docs)

	got := map[string]*types.LegalDocument{}
	for _, d := range res.Keep {
		got[d.ID] = d
	}
	for _, id := range []string{"200001", "200004"} {
		if got[id] != nil {
			t.Errorf("twin %s imported as a second row", id)
		}
	}
	wantAlias := map[string]string{"109088": "22897", "200001": "22897", "200004": "200003", "185841": "abace830"}
	if len(res.Alias) != len(wantAlias) {
		t.Errorf("alias = %v, want %v", res.Alias, wantAlias)
	}
	for id, s := range wantAlias {
		if res.Alias[id] != s {
			t.Errorf("alias[%s] = %q, want %q", id, res.Alias[id], s)
		}
	}
	keyOf := map[string]string{}
	for _, e := range existing {
		keyOf[e.ID] = e.NumberKey
	}
	// aliased twins and a Ð twin of another document keep their key
	for _, id := range []string{"109088", "109500", "185841"} {
		if d := got[id]; d == nil || d.NumberKey != keyOf[id] {
			t.Errorf("twin %s rekeyed: %+v", id, d)
		}
	}
	if len(res.Unreachable) != 1 || res.Unreachable[0].Row.ID != "109500" || res.Unreachable[0].Holders[0] != "61017" {
		t.Errorf("unreachable = %+v, want 109500 (holder 61017)", res.Unreachable)
	}
	// rekeyed: Ð without a twin, Cyrillic of another document, reviewed Cyrillic
	for id, want := range map[string]string{"142783": "02/2020/TT-BGDDT", "179043": "51/2025/TT-BTC", "179086": "65/2025/TT-BTC"} {
		if d := got[id]; d == nil || d.NumberKey != want {
			t.Errorf("%s not rekeyed to %s: %+v", id, want, d)
		}
	}
	if got["200002"] == nil || got["200003"] == nil || got["200005"] == nil || got["22897"] == nil {
		t.Error("distinct documents must be imported")
	}
}

func TestTitleMatch(t *testing.T) {
	for _, c := range []struct {
		a, na, b, nb string
		want         bool
	}{
		{"Thông tư số 06/2026/TT-BTС Sửa đổi, bố sung một số điều của Thông tư số 1", "06/2026/TT-BTС",
			"Thông tư 06/2026/TT-BTC Sửa đổi, bổ sung một số điều của Thông tư số 1", "06/2026/TT-BTC", true},
		{"Quyết định số 129/2002/QÐ-BTC của bộ trưởng bộ tài chính về việc bổ sung mức thu phí, lệ phí hàng hải", "129/2002/QÐ-BTC",
			"Quyết định số 129/2002/QĐ-BTC Về việc bổ sung mức thu phí, lệ phí hàng hải", "129/2002/QĐ-BTC", true},
		{"Thông tư liên tịch số 22/2003/TTLT-BQP-BLÐTBXH-BYT-BTC Thông tư hướng dẫn thực hiện Nghị định số 63/2002/NÐ-CP", "22/2003/TTLT-BQP-BLÐTBXH-BYT-BTC",
			"Thông tư liên tịch số 22/2003/TTLT-BQP-BLĐTBXH-BYT-BTC hướng dẫn Nghị định 63/2002/NĐ-CP", "22/2003/TTLT-BQP-BLĐTBXH-BYT-BTC", true},
		{"Quyết định số 41/2000/QÐ-BTC về việc sửa đổi mức thuế suất thuế nhập khẩu một số mặt hàng thuộc nhóm 2710 trong biểu thuế", "41/2000/QÐ-BTC",
			"Quyết định số 41/2000/QĐ-BTC Về việc sửa đổi, bổ sung tên và thuế suất của một số nhóm mặt hàng trong Biểu thuế", "41/2000/QĐ-BTC", false},
		{"Quyết định số 45/2002/QÐ-BTC về việc thay thế biểu thuế thuế xuất khẩu", "45/2002/QÐ-BTC",
			"Quyết định số 45/2002/QD-BTC The Decision of the Minister of Finance on replacing the export tariff", "45/2002/QD-BTC", false},
	} {
		if got := TitleMatch(c.a, c.na, c.b, c.nb); got != c.want {
			t.Errorf("TitleMatch(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
