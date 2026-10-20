package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// A (id 100) tagged by its editors; B (id 200) holds the source provisions.
const pageA = `<html><body>
<p id="a1" class="prov-article" type="10:b-k1">Điều 1. Phạm vi</p>
<p id="a1k1" class="prov-clause" type="1:b-k2" new-types="13:b-k3,12:b-k4">1. Khoản một.</p>
<p id="a1k1a" class="prov-item" type="14:b-k5">a) điểm a;</p>
<p id="a2" class="prov-article" type="6:b-k6,11:b-k7,5:b-k8">Điều 2. Khác</p>
<p id="a2k1" class="prov-clause" type="10:missing-id">1. Nguồn không có ở đâu.</p>
<p id="a2k2" class="prov-clause" type="10:pt-only">2. Nguồn chỉ có trong provision-target.</p>
<p id="a2k3" class="prov-clause" type="10:PHU-LUC">3. Phụ lục.</p>
</body></html>`

const pageB = `<html><body>
<p id="b1" class="prov-article">Điều 1. Sửa đổi Nghị định số 100/2020/NĐ-CP</p>
<p id="b-k1" class="prov-clause">1. Sửa đổi Điều 1 như sau:</p>
<p id="b-k2" class="prov-clause">2. Bãi bỏ khoản 1 Điều 1.</p>
<p id="b-k3" class="prov-clause">3. Bổ sung khoản 1a.</p>
<p id="b-k4" class="prov-clause">4. Thay thế.</p>
<p id="b-k5" class="prov-clause">5. Giải thích.</p>
<p id="b-k6" class="MsoNormal prov-clause">6. Đính chính.</p>
<p id="b-k7" class="prov-clause">7. Tạm ngưng.</p>
<div id="b-k8" class="prov-clause">8. Đình chỉ.</div>
</body></html>`

func TestBuildRows(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "100.html"), []byte(pageA), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "200.html"), []byte(pageB), 0o644))
	pages, err := readPages(dir, 2)
	require.NoError(t, err)
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	catalog := map[string]catalogRow{"100": {ID: "100"}, "200": {ID: "200", EffectiveOn: day("2024-01-01")}, "300": {ID: "300"}}
	targets := map[string]provTarget{
		"a1|b-k1":      {DocumentID: "200", EffFrom: day("2024-07-01")},
		"a2|b-k7":      {DocumentID: "200", EffFrom: day("2024-02-01"), EffTo: day("2024-12-31")},
		"a2k2|pt-only": {DocumentID: "300"},
	}
	rows, st := buildRows(pages, catalog, targets, true)
	by := map[string]types.LegalProvisionLink{}
	for _, r := range rows {
		by[r.TargetProvisionID+"|"+r.SourceProvisionID] = r
	}
	require.Len(t, rows, 10, "PHU-LUC skipped")
	require.Equal(t, 1, st.phuLuc)

	r := by["a1|b-k1"]
	require.Equal(t, "100", r.TargetDocID)
	require.Equal(t, "dieu:1", r.TargetPath)
	require.Equal(t, "article", r.TargetLevel)
	require.Equal(t, 10, r.RelationCode)
	require.Equal(t, "sua_doi_bo_sung", r.Op)
	require.Equal(t, "200", r.SourceDocID)
	require.Equal(t, types.LegalLinkFromHTML, r.SourceDocFrom)
	require.Equal(t, "dieu:1/khoan:1", r.SourcePath)
	require.Equal(t, "2024-07-01", r.EffFrom.Format("2006-01-02"), "vbpl's per-provision date first")
	require.Equal(t, types.LegalLinkFromProvisionTarget, r.EffFromSource)
	require.Equal(t, types.LegalProvisionLinkSourceEditor, r.Source)

	// several tags on one provision: type + new-types
	require.Equal(t, "bai_bo", by["a1k1|b-k2"].Op)
	require.Equal(t, "type", by["a1k1|b-k2"].TagAttr)
	require.Equal(t, "bo_sung", by["a1k1|b-k3"].Op)
	require.Equal(t, "new-types", by["a1k1|b-k3"].TagAttr)
	require.Equal(t, "thay_the", by["a1k1|b-k4"].Op)
	require.Equal(t, "2024-01-01", by["a1k1|b-k2"].EffFrom.Format("2006-01-02"), "else B's catalogue date")
	require.Equal(t, types.LegalLinkFromCatalogue, by["a1k1|b-k2"].EffFromSource)
	require.Equal(t, "dieu:1/khoan:1/diem:a", by["a1k1a|b-k5"].TargetPath)
	require.Equal(t, "giai_thich", by["a1k1a|b-k5"].Op)
	require.Equal(t, "dinh_chinh", by["a2|b-k6"].Op)
	require.Equal(t, "dieu:1/khoan:6", by["a2|b-k6"].SourcePath, "a class list holding prov-clause")
	sus := by["a2|b-k7"]
	require.Equal(t, "tam_ngung", sus.Op)
	require.Equal(t, "2024-12-31", sus.EffTo.Format("2006-01-02"), "a suspension keeps its end")
	require.Equal(t, "dinh_chi", by["a2|b-k8"].Op)
	require.Equal(t, "200", by["a2|b-k8"].SourceDocID, "B found on a div too")

	// B nowhere: kept, unresolved, no date
	miss := by["a2k1|missing-id"]
	require.Empty(t, miss.SourceDocID)
	require.Nil(t, miss.EffFrom)
	require.Empty(t, miss.EffFromSource)
	// B's page not crawled: vbpl's provision-target answer names it
	pt := by["a2k2|pt-only"]
	require.Equal(t, "300", pt.SourceDocID)
	require.Equal(t, types.LegalLinkFromProvisionTarget, pt.SourceDocFrom)
	require.Nil(t, pt.EffFrom, "300 has no catalogue date")

	require.Equal(t, 1, st.bFrom["unresolved"])
	require.Equal(t, 1, st.bFrom[types.LegalLinkFromProvisionTarget])
	require.Len(t, st.unresolved, 1)
}
