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

// Fable re-review: one number, one day, one type of two issuers or places
// is two documents; issuers spelled or listed differently are one.
func TestSameDocumentIssuerAndPlace(t *testing.T) {
	row := func(id, num, issuer, title string) *types.LegalDocument {
		return &types.LegalDocument{ID: id, DocumentNumber: num, NumberKey: vietnamese_legal.CatalogNumberKey(num), DocType: "Quyết định",
			Issuer: issuer, Title: title, IssuedOn: twinDay("2020-03-01")}
	}
	for _, c := range []struct {
		name string
		a, b *types.LegalDocument
		want bool
	}{
		{"two provinces by issuer",
			row("1", "05/2020/QĐ-UBND", "Ủy ban nhân dân tỉnh Hà Giang", "Quyết định số 05/2020/QĐ-UBND ban hành Quy định giá đất trên địa bàn tỉnh Hà Giang"),
			row("2", "05/2020/QÐ-UBND", "Ủy ban nhân dân tỉnh Hà Nam", "Quyết định số 05/2020/QÐ-UBND ban hành Quy định giá đất trên địa bàn tỉnh Hà Nam"), false},
		{"two provinces by title only",
			row("1", "05/2020/QĐ-UBND", "", "Quyết định số 05/2020/QĐ-UBND ban hành Quy định giá đất tỉnh Hà Giang"),
			row("2", "05/2020/QÐ-UBND", "", "Quyết định số 05/2020/QÐ-UBND ban hành Quy định giá đất tỉnh Hà Nam"), false},
		{"two districts",
			row("1", "12/2020/QĐ-UBND", "UBND huyện Mường Khương", "Quyết định số 12/2020/QĐ-UBND về phí chợ huyện Mường Khương"),
			row("2", "12/2020/QÐ-UBND", "Ủy ban nhân dân huyện Mường Tè", "Quyết định số 12/2020/QÐ-UBND về phí chợ huyện Mường Tè"), false},
		{"one province, UBND / Uỷ ban spelling",
			row("1", "05/2020/QĐ-UBND", "UBND tỉnh Hà Giang", "Quyết định số 05/2020/QĐ-UBND ban hành Quy định giá đất tỉnh Hà Giang Về việc"),
			row("2", "05/2020/QÐ-UBND", "Uỷ ban nhân dân tỉnh Hà Giang", "Quyết định 05/2020/QÐ-UBND Ban hành quy định giá đất tỉnh Hà Giang"), true},
		{"a joint circular listing one issuer or all, in any order",
			row("1", "02/1998/TTLT-BTC-TCBĐ", "Bộ Tài chính", "Quyết định số 02/1998/TTLT-BTC-TCBĐ hướng dẫn cước phí"),
			row("2", "02/1998/TTLT-BTC-TCBÐ", "Tổng cục Bưu điện, Bộ Tài chính", "Quyết định số 02/1998/TTLT-BTC-TCBÐ Hướng dẫn cước phí"), true},
	} {
		if got := SameDocument(c.a, c.b); got != c.want {
			t.Errorf("%s: SameDocument = %v, want %v", c.name, got, c.want)
		}
	}
}

// A placeholder ("(tạm nhập - chờ toàn văn)") never keeps out the full
// copy: a new full copy is imported and the placeholder's edges go to it;
// a new placeholder of a catalogued full copy is attached to it. Attached
// twins whose status differs are reported.
func TestResolveTwinsPlaceholder(t *testing.T) {
	existing := []*types.LegalDocument{
		{ID: "46690", DocumentNumber: "05/2007/TT-BCT", NumberKey: "05/2007/TT-BCT", DocType: "Thông tư", Issuer: "Bộ Công Thương",
			Title: "Thông tư số 05/2007/TT-BCT (tạm nhập - chờ toàn văn) Hướng dẫn xuất khẩu than", IssuedOn: twinDay("2007-10-22"), Status: "Hết hiệu lực toàn bộ"},
		{ID: "105834", DocumentNumber: "26/2009/TT-BCT", NumberKey: "26/2009/TT-BCT", DocType: "Thông tư", Issuer: "Bộ Công Thương",
			Title: "Thông tư số 26/2009/TT-BCT Quy định quy trình nghiệp vụ kiểm tra", IssuedOn: twinDay("2009-08-26"), Status: "Còn hiệu lực"},
	}
	full := imported("82314", "05/2007/TT-BCT", "Thông tư", "Thông tư số 05/2007/TT-BCT Hướng dẫn xuất khẩu than", "2007-10-22")
	full.Status = "Hết hiệu lực toàn bộ"
	ph := imported("31740", "26/2009/TT-BCT", "Thông tư", "Thông tư số 26/2009/TT-BCT (tạm nhập - chờ toàn văn) Quy định quy trình nghiệp vụ kiểm tra", "2009-08-26")
	ph.Status = "Hết hiệu lực toàn bộ"
	res := ResolveTwins(existing, []*types.LegalDocument{full, ph})
	if len(res.Keep) != 1 || res.Keep[0].ID != "82314" {
		t.Fatalf("keep = %v, want the full copy only", res.Keep)
	}
	if res.Alias["46690"] != "82314" || res.Alias["31740"] != "105834" {
		t.Errorf("alias = %v", res.Alias)
	}
	if len(res.StatusDiffers) != 1 || res.StatusDiffers[0].Twin.ID != "31740" {
		t.Errorf("status differs = %+v", res.StatusDiffers)
	}
}

// Fable LOW: a placeholder never wins over a full copy already under the
// key, and the outcome does not depend on the file's order.
func TestResolveTwinsPlaceholderOrder(t *testing.T) {
	const title = "Quyết định số 05/2007/QĐ-BCT Hướng dẫn xuất khẩu than"
	placeholder := func() *types.LegalDocument {
		return catalogued("46690", "05/2007/QĐ-BCT", "05/2007/QD-BCT", "Quyết định",
			"Quyết định số 05/2007/QĐ-BCT (tạm nhập - chờ toàn văn) Hướng dẫn xuất khẩu than", "2007-10-22")
	}
	keep := func(res TwinResolution) map[string]string {
		out := map[string]string{}
		for _, d := range res.Keep {
			out[d.ID] = d.NumberKey
		}
		return out
	}
	reverse := func(in []*types.LegalDocument) []*types.LegalDocument {
		out := make([]*types.LegalDocument, len(in))
		for i, d := range in {
			out[len(in)-1-i] = d
		}
		return out
	}

	t.Run("placeholder and two new full copies", func(t *testing.T) {
		for _, rev := range []bool{false, true} {
			docs := []*types.LegalDocument{
				imported("82314", "05/2007/QĐ-BCT", "Quyết định", title, "2007-10-22"),
				imported("82400", "05/2007/QÐ-BCT", "Quyết định", "Quyết định số 05/2007/QÐ-BCT Hướng dẫn xuất khẩu than", "2007-10-22"),
			}
			if rev {
				docs = reverse(docs)
			}
			res := ResolveTwins([]*types.LegalDocument{placeholder()}, docs)
			if k := keep(res); len(k) != 1 || k["82314"] == "" {
				t.Errorf("reverse=%v: keep = %v, want 82314 only", rev, k)
			}
			if res.Alias["46690"] != "82314" || res.Alias["82400"] != "82314" {
				t.Errorf("reverse=%v: alias = %v", rev, res.Alias)
			}
		}
	})

	t.Run("placeholder, catalogued Ð twin and a new full copy", func(t *testing.T) {
		for _, rev := range []bool{false, true} {
			twin := catalogued("109900", "05/2007/QÐ-BCT", "05/2007/QÐ-BCT", "Quyết định",
				"Quyết định số 05/2007/QÐ-BCT Hướng dẫn xuất khẩu than", "2007-10-22")
			existing := []*types.LegalDocument{placeholder(), twin}
			docs := []*types.LegalDocument{
				imported("109900", "05/2007/QÐ-BCT", "Quyết định", "Quyết định số 05/2007/QÐ-BCT Hướng dẫn xuất khẩu than", "2007-10-22"),
				imported("82314", "05/2007/QĐ-BCT", "Quyết định", title, "2007-10-22"),
			}
			if rev {
				docs = reverse(docs)
			}
			res := ResolveTwins(existing, docs)
			// the catalogued twin is rekeyed and survives; the new copy
			// is attached to it, the placeholder too
			if k := keep(res); len(k) != 1 || k["109900"] != "05/2007/QD-BCT" {
				t.Errorf("reverse=%v: keep = %v, want 109900 rekeyed only", rev, k)
			}
			if res.Alias["46690"] != "109900" || res.Alias["82314"] != "109900" {
				t.Errorf("reverse=%v: alias = %v", rev, res.Alias)
			}
		}
	})

	t.Run("an unreachable Ð twin attaches to its new Latin copy", func(t *testing.T) {
		for _, rev := range []bool{false, true} {
			existing := []*types.LegalDocument{
				catalogued("61017", "17-TC/TCĐN", "17-TC/TCDN", "Thông tư", "Thông tư số 17-TC/TCĐN Hướng dẫn việc thu nộp lệ phí", "1989-05-23"),
				catalogued("109500", "17-TC/TCÐN", "17-TC/TCÐN", "Thông tư", "Thông tư số 17-TC/TCÐN hướng dẫn cơ chế quản lý tài chính", "1997-04-02"),
			}
			docs := []*types.LegalDocument{
				imported("109500", "17-TC/TCÐN", "Thông tư", "Thông tư số 17-TC/TCÐN hướng dẫn cơ chế quản lý tài chính", "1997-04-02"),
				imported("200010", "17-TC/TCĐN", "Thông tư", "Thông tư số 17-TC/TCĐN Hướng dẫn cơ chế quản lý tài chính", "1997-04-02"),
			}
			if rev {
				docs = reverse(docs)
			}
			res := ResolveTwins(existing, docs)
			if len(res.Unreachable) != 0 || res.Alias["109500"] != "200010" {
				t.Errorf("reverse=%v: unreachable = %+v, alias = %v", rev, res.Unreachable, res.Alias)
			}
			if k := keep(res); k["109500"] != "17-TC/TCÐN" || k["200010"] != "17-TC/TCDN" {
				t.Errorf("reverse=%v: keep = %v", rev, k)
			}
		}
	})
}

// Fable LOW: a title-cased common noun after a unit word ("Xã Hội", "Xã
// Nông thôn mới") is not a place when the other title writes the same
// words as common words; real places still disagree.
func TestPlacesAgreeTitleCase(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"Quyết định về chính sách Xã Hội", "Quyết định về chính sách xã hội tỉnh Hà Giang", true},
		{"Đề án Xã Nông thôn mới giai đoạn 2021-2025", "Đề án xã nông thôn mới huyện Mường Tè giai đoạn 2021-2025", true},
		{"chính sách Xã Hội tỉnh Hà Giang", "chính sách xã hội tỉnh Hà Nam", false},
		{"Quyết định giá đất tỉnh Hà Giang", "Quyết định giá đất tỉnh Hà Nam", false},
		{"Kế hoạch thành phố Huế nhiệm kỳ 2020", "Kế hoạch thành phố Vinh nhiệm kỳ 2020", false},
		{"Quy định phí chợ huyện Mường Khương", "Quy định phí chợ Huyện Mường Khương", true},
	} {
		if got := PlacesAgree(c.a, c.b); got != c.want {
			t.Errorf("PlacesAgree(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := PlacesAgree(c.b, c.a); got != c.want {
			t.Errorf("PlacesAgree(%q, %q) = %v, want %v", c.b, c.a, got, c.want)
		}
	}
}
