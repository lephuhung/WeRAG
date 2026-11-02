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

func TestResolveTwins(t *testing.T) {
	// the catalogue as migration 000175 left it
	existing := []*types.LegalDocument{
		{ID: "22897", DocumentNumber: "136/2001/QĐ-BTC", NumberKey: "136/2001/QD-BTC", DocType: "Quyết định",
			Title: "Quyết định số 136/2001/QĐ-BTC Về việc bổ sung Danh mục", IssuedOn: twinDay("2001-12-18")},
		// same document, left with its Ð key
		{ID: "109088", DocumentNumber: "136/2001/QÐ-BTC", NumberKey: "136/2001/QÐ-BTC", DocType: "Quyết định",
			Title: "Quyết định số 136/2001/QÐ-BTC về việc bổ sung danh mục", IssuedOn: twinDay("2001-12-18")},
		{ID: "61017", DocumentNumber: "17-TC/TCĐN", NumberKey: "17-TC/TCDN", DocType: "Thông tư",
			Title: "Thông tư số 17-TC/TCĐN Hướng dẫn việc thu nộp lệ phí", IssuedOn: twinDay("1989-05-23")},
		// an older number reused by another document: not the same one
		{ID: "109500", DocumentNumber: "17-TC/TCÐN", NumberKey: "17-TC/TCÐN", DocType: "Thông tư",
			Title: "Thông tư số 17-TC/TCÐN hướng dẫn cơ chế quản lý tài chính", IssuedOn: twinDay("1997-04-02")},
		// Ð with no twin (re-written by an import that did not fold)
		{ID: "142783", DocumentNumber: "02/2020/TT-BGDÐT", NumberKey: "02/2020/TT-BGDÐT", DocType: "Thông tư",
			Title: "Thông tư số 02/2020/TT-BGDÐT Quy định", IssuedOn: twinDay("2020-02-05")},
	}
	docs := []*types.LegalDocument{
		imported("22897", "136/2001/QĐ-BTC", "Quyết định", "Quyết định số 136/2001/QĐ-BTC Về việc bổ sung Danh mục", "2001-12-18"),
		imported("109088", "136/2001/QÐ-BTC", "Quyết định", "Quyết định số 136/2001/QÐ-BTC về việc bổ sung danh mục", "2001-12-18"),
		imported("109500", "17-TC/TCÐN", "Thông tư", "Thông tư số 17-TC/TCÐN hướng dẫn cơ chế quản lý tài chính", "1997-04-02"),
		imported("142783", "02/2020/TT-BGDÐT", "Thông tư", "Thông tư số 02/2020/TT-BGDÐT Quy định", "2020-02-05"),
		// a new vbpl id for a catalogued document, written with Ð
		imported("200001", "Quyết định số: 136/2001/QÐ-BTC", "Quyết định", "Quyết định số 136/2001/QÐ-BTC Về việc bổ sung danh mục", "2001-12-18"),
		// a new id under a known key, another document (other day)
		imported("200002", "136/2001/QĐ-BTC", "Quyết định", "Quyết định số 136/2001/QĐ-BTC Về việc khác", "2002-12-18"),
		// two new ids of one new document in the same import
		imported("200003", "5/2026/QĐ-UBND", "Quyết định", "Quyết định số 5/2026/QĐ-UBND", "2026-02-01"),
		imported("200004", "5/2026/QÐ-UBND", "Quyết định", "Quyết định số 5/2026/QÐ-UBND", "2026-02-01"),
	}
	keep, alias := ResolveTwins(existing, docs)

	got := map[string]*types.LegalDocument{}
	for _, d := range keep {
		got[d.ID] = d
	}
	for _, id := range []string{"200001", "200004"} {
		if got[id] != nil {
			t.Errorf("twin %s imported as a second row", id)
		}
	}
	wantAlias := map[string]string{"109088": "22897", "200001": "22897", "200004": "200003"}
	if len(alias) != len(wantAlias) {
		t.Errorf("alias = %v, want %v", alias, wantAlias)
	}
	for id, s := range wantAlias {
		if alias[id] != s {
			t.Errorf("alias[%s] = %q, want %q", id, alias[id], s)
		}
	}
	// catalogued twins keep their Ð key, same document or not
	for _, id := range []string{"109088", "109500"} {
		if d := got[id]; d == nil || d.NumberKey != existing[idx(existing, id)].NumberKey ||
			d.DocumentNumber != existing[idx(existing, id)].DocumentNumber {
			t.Errorf("catalogued twin %s rekeyed: %+v", id, d)
		}
	}
	if d := got["142783"]; d == nil || d.NumberKey != "02/2020/TT-BGDDT" || d.DocumentNumber != "02/2020/TT-BGDĐT" {
		t.Errorf("Ð row without a twin not rekeyed: %+v", d)
	}
	if got["200002"] == nil || got["200003"] == nil || got["22897"] == nil {
		t.Error("distinct documents must be imported")
	}
}

func idx(rows []*types.LegalDocument, id string) int {
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}
