package vbpl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// v1Sample is a small page in the "WeRAG legal HTML v1" format.
const v1Sample = `<html><body>
<p class="prov-article" id="t-1-dieu:3">Điều 3. Đối tượng áp dụng</p>
<p class="prov-clause" id="t-1-dieu:3/khoan:1"
   data-amend='[{"op":"sua_doi_bo_sung","doc_number":"150/2026/NĐ-CP","path":"dieu:1/khoan:1","eff_from":"2026-06-01","note":"Khoản này được sửa đổi"}]'>1. Hộ kinh doanh.</p>
<p class="prov-clause" id="t-1-dieu:3/khoan:2"
   data-amend='[{"op":"bai_bo","doc_number":"141/2026/NĐ-CP","path":"dieu:1/khoan:2"},{"op":"lam_sai","doc_number":"1/2026/NĐ-CP"}]'>2. Cá nhân kinh doanh.</p>
<p class="prov-item" id="t-1-dieu:3/khoan:2/diem:a" data-amend='not json'>a) cá nhân;</p>
<p class="prov-content" data-amend='[{"op":"bo_sung","doc_number":"150/2026/NĐ-CP"}]'>Nội dung không có id.</p>
<p class="prov-article" id="t-1-dieu:4">Điều 4. Hiệu lực</p>
<p class="prov-clause" id="t-1-dieu:4/khoan:1" data-amend='[{"op":"thay_the","path":"dieu:2"}]'>1. Nghị định có hiệu lực.</p>
</body></html>`

func TestReadAmendMarkup(t *testing.T) {
	m, err := ReadAmendMarkup([]byte(v1Sample))
	require.NoError(t, err)
	require.Len(t, m.Provisions, 2)
	require.Equal(t, "t-1-dieu:3/khoan:1", m.Provisions[0].ElementID)
	require.Equal(t, AmendMark{Op: "sua_doi_bo_sung", DocNumber: "150/2026/NĐ-CP", Path: "dieu:1/khoan:1",
		EffFrom: "2026-06-01", Note: "Khoản này được sửa đổi"}, m.Provisions[0].Marks[0])
	// the unknown op is dropped, its sibling kept
	require.Len(t, m.Provisions[1].Marks, 1)
	require.Equal(t, "bai_bo", m.Provisions[1].Marks[0].Op)

	reasons := map[string]bool{}
	for _, p := range m.Problems {
		reasons[p.ElementID+": "+p.Reason[:12]] = true
	}
	require.Len(t, m.Problems, 4, "%v", m.Problems)
	require.True(t, reasons["t-1-dieu:3/khoan:2: mark 2: unkn"])
	require.True(t, reasons["t-1-dieu:3/khoan:2/diem:a: data-amend i"])
	require.True(t, reasons[": data-amend o"])
	require.True(t, reasons["t-1-dieu:4/khoan:1: mark 1: doc_"])
}

// The converter reads the v1 page like a vbpl page: the element ids become
// provision ids at their Điều/khoản/điểm paths.
func TestAmendMarkupPlaces(t *testing.T) {
	raw := []byte(v1Sample)
	require.True(t, LooksLikeVBPL(raw))
	doc, err := ConvertBytes(raw)
	require.NoError(t, err)
	require.True(t, doc.Structured)
	places := ProvisionPlaces(doc)
	require.Equal(t, "dieu:3/khoan:1", places["t-1-dieu:3/khoan:1"].Path)
	require.Equal(t, "clause", places["t-1-dieu:3/khoan:1"].Level)
	require.Equal(t, "dieu:3/khoan:2/diem:a", places["t-1-dieu:3/khoan:2/diem:a"].Path)
	require.Equal(t, "dieu:4", places["t-1-dieu:4"].Path)
}
