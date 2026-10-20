package vbpl

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// A page of document A as vbpl.vn serves it, with the editors' tags.
const editorPage = header + `
<p id="ch1" class="prov-chapter" type="1:src-chapter">Chương I</p>
<p id="a1" class="prov-article" type="10:src-a1">Điều 1. Phạm vi</p>
<p id="a1k1" class="prov-clause" type="10:src-k1" new-types="1:src-later,13:src-add">1. Khoản một.</p>
<p id="a1k1a" class="MsoNormal prov-item" type="12:src-item">a) điểm a;</p>
<p id="a1k2" class="prov-clause">2. Khoản hai.</p>
<ol type="1"><li>một mục danh sách</li></ol>
<p id="a2" class="prov-article" type="14:src-14">Điều 2. Giải thích</p>
<p id="a2a" class="prov-item" type="6:src-6">a) điểm a ngay dưới Điều;</p>
<p id="a3" class="prov-article" type="11:src-11,5:src-5">Điều 3. Tạm ngưng</p>
<p id="a3k1" class="prov-clause" type="10:PHU-LUC">1. Phụ lục.</p>
<p id="a3k2" class="prov-clause" type="10:src-k1">2. Lặp lại một nguồn.</p>
<p class="prov-clause" type="10:no-id">3. Không có id.</p>
<p id="a3k4" class="prov-clause" type="99:src-x,abc">4. Mã lạ.</p>
<p id="a3k4" class="prov-clause">4. Bản lặp của id.</p>
<p class="prov-content" type="10:content">Nội dung thường.</p>
` + signature

func TestReadEditorPage(t *testing.T) {
	pg, err := ReadEditorPage([]byte("<html><body>" + editorPage + "</body></html>"))
	require.NoError(t, err)
	tags := pg.Tags
	require.Equal(t, []EditorTag{{Code: 10, SourceID: "src-a1", Attr: "type"}}, tags["a1"], "single tag")
	require.Equal(t, []EditorTag{
		{Code: 10, SourceID: "src-k1", Attr: "type"},
		{Code: 1, SourceID: "src-later", Attr: "new-types"},
		{Code: 13, SourceID: "src-add", Attr: "new-types"},
	}, tags["a1k1"], "type then new-types, comma separated")
	require.Equal(t, []EditorTag{{Code: 12, SourceID: "src-item", Attr: "type"}}, tags["a1k1a"], "class list with another class")
	require.Equal(t, []EditorTag{{Code: 11, SourceID: "src-11", Attr: "type"}, {Code: 5, SourceID: "src-5", Attr: "type"}}, tags["a3"], "several codes in type")
	require.Equal(t, 14, tags["a2"][0].Code)
	require.Equal(t, 6, tags["a2a"][0].Code)
	require.Equal(t, EditorSourcePhuLuc, tags["a3k1"][0].SourceID)
	require.Equal(t, "src-k1", tags["a3k2"][0].SourceID, "one B provision may act on two provisions of A")
	require.Equal(t, []EditorTag{{Code: 1, SourceID: "src-chapter", Attr: "type"}}, tags["ch1"])
	require.Empty(t, tags["a3k4"], "unknown code and unreadable part dropped")
	require.NotContains(t, tags, "content", "prov-content carries no provision")
	require.Equal(t, 2, pg.Stats.Malformed)
	require.Equal(t, 1, pg.Stats.NoID)
	require.Equal(t, 1, pg.Stats.PhuLuc)
	require.Equal(t, "chapter", pg.Levels["ch1"])
	require.Equal(t, "item", pg.Levels["a1k1a"])
	require.Equal(t, "clause", pg.Levels["a1k2"], "untagged provisions are listed: a source id may name them")

	// EditorTags keeps Điều / khoản / điểm only
	art, err := EditorTags([]byte("<html><body>" + editorPage + "</body></html>"))
	require.NoError(t, err)
	require.NotContains(t, art, "ch1")
	require.Contains(t, art, "a1k1a")
	none, err := EditorTags([]byte(`<p id="x" class="prov-article">Điều 1. Không thẻ</p>`))
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestEditorCodeOp(t *testing.T) {
	for code, op := range map[int]string{1: "bai_bo", 5: "dinh_chi", 6: "dinh_chinh", 10: "sua_doi_bo_sung",
		11: "tam_ngung", 12: "thay_the", 13: "bo_sung", 14: "giai_thich", 2: ""} {
		require.Equal(t, op, EditorCodeOp(code), code)
	}
}

func TestProvisionPlacesAndChunkLinks(t *testing.T) {
	raw := []byte("<html><body>" + editorPage + "</body></html>")
	d, err := ConvertBytes(raw)
	require.NoError(t, err)
	places := ProvisionPlaces(d)
	require.Equal(t, "dieu:1", places["a1"].Path)
	require.Equal(t, "article", places["a1"].Level)
	require.Equal(t, "dieu:1/khoan:1", places["a1k1"].Path)
	require.Equal(t, "dieu:1/khoan:1/diem:a", places["a1k1a"].Path)
	require.Equal(t, "dieu:2/diem:a", places["a2a"].Path, "an điểm right under its Điều")
	require.NotContains(t, places, "ch1", "headings are not provisions")

	tags, err := EditorTags(raw)
	require.NoError(t, err)
	chunks := Plan(d, 2500)
	var all []vietnamese_legal.EditorLink
	for _, c := range chunks {
		links := EditorLinks(d, c, tags, places)
		for _, l := range links {
			require.Equal(t, places[l.ProvisionID].Path, l.Path)
		}
		all = append(all, links...)
		if c.ArticleNo == "1" {
			require.Len(t, links, 5, "Điều 1, khoản 1 (three tags), điểm a")
		}
	}
	require.Contains(t, all, vietnamese_legal.EditorLink{ProvisionID: "a1k1", Path: "dieu:1/khoan:1", Code: 13, SourceProvisionID: "src-add"})
	require.Contains(t, all, vietnamese_legal.EditorLink{ProvisionID: "a3", Path: "dieu:3", Code: 5, SourceProvisionID: "src-5"})
	require.Len(t, all, 11)
	require.Nil(t, EditorLinks(d, chunks[0], nil, places))
}
