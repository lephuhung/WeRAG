package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// NĐ 150/2026: operations on NĐ 68 the vbpl parser did not read (no
// amends metadata); only the editors' tags on NĐ 68 name them.
const nd150Dieu1 = "#### Điều 1. Sửa đổi Nghị định số 68/2026/NĐ-CP\n\n" +
	"1. Sửa đổi khoản 1 Điều 3 như sau:\n\n> “1. Khoản một mới theo Nghị định 150.”.\n\n" +
	"2. Bãi bỏ khoản 2 Điều 3.\n\n" +
	"3. Khoản 1 Điều 8 được hiểu là doanh thu của cả năm.\n\n" +
	"4. Thay cụm từ “tờ khai” bằng cụm từ “bản khai” tại điểm a khoản 2 Điều 8.\n\n" +
	"5. Các trường hợp khác thực hiện theo quy định của Bộ Tài chính."

// legalEditorTestDB is legalAmendTestDB with provision ids, NĐ 150 and
// the editors' links; NĐ 200 (unknown date) is left out so states show.
func legalEditorTestDB(t *testing.T) *gorm.DB {
	db := legalAmendTestDB(t)
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&types.LegalProvisionLink{}))
	require.NoError(t, db.Exec(`DELETE FROM legal_document_relations WHERE from_id = 'nd200'`).Error)
	require.NoError(t, db.Exec(`DELETE FROM chunks WHERE id = 'b2'`).Error)
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	require.NoError(t, repository.NewLegalCatalogRepository(db).UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "nd150", Source: "vbpl", DocumentNumber: "150/2026/NĐ-CP", NumberKey: vietnamese_legal.CatalogNumberKey("150/2026/NĐ-CP"), DocType: "Nghị định",
			Title: "Nghị định 150/2026/NĐ-CP", IssuedOn: day("2026-05-01"), EffectiveOn: day("2026-06-01")},
	}))
	setIDs := func(chunkID string, ids map[string]string) {
		var c types.Chunk
		require.NoError(t, db.First(&c, "id = ?", chunkID).Error)
		var meta types.DocumentChunkMetadata
		require.NoError(t, json.Unmarshal(c.Metadata, &meta))
		meta.Legal.ProvisionIDs = ids
		b, _ := json.Marshal(meta)
		require.NoError(t, db.Model(&types.Chunk{}).Where("id = ?", chunkID).Update("metadata", b).Error)
	}
	setIDs("b1", map[string]string{"dieu": "p141", "khoan:1": "p141-1", "khoan:2": "p141-2", "khoan:3": "p141-3"})
	meta, _ := json.Marshal(map[string]string{"doc_number": "150/2026/NĐ-CP", "external_id": "nd150", "doc_type": "Nghị định"})
	require.NoError(t, db.Create(&types.Knowledge{ID: "k150", KnowledgeBaseID: "kb-pub", Title: "150/2026/NĐ-CP", ParseStatus: types.ParseStatusCompleted, Metadata: meta}).Error)
	legal := vietnamese_legal.LegalMetadata{Source: "vbpl", ArticleNos: []string{"1"}, KhoanNos: []string{"1", "2", "3", "4", "5"}, SourceDocID: "nd150", Quoted: true,
		SubdivisionRefs: []string{"khoan:1", "khoan:2", "khoan:3", "khoan:4", "khoan:5"},
		ProvisionIDs:    map[string]string{"dieu": "p150", "khoan:1": "p150-1", "khoan:2": "p150-2", "khoan:3": "p150-3", "khoan:4": "p150-4", "khoan:5": "p150-5"}}
	cm, _ := json.Marshal(types.DocumentChunkMetadata{Legal: &legal})
	require.NoError(t, db.Create(&types.Chunk{ID: "b150", KnowledgeID: "k150", KnowledgeBaseID: "kb-pub", ChunkIndex: 1,
		ChunkType: types.ChunkTypeText, IsEnabled: true, Content: nd150Dieu1, Metadata: cm}).Error)
	link := func(path string, code int, op, src, srcDoc, srcPath string, eff *time.Time) types.LegalProvisionLink {
		return types.LegalProvisionLink{TargetDocID: "nd68", TargetProvisionID: "p68-" + path + "-" + src, TargetPath: path, RelationCode: code, Op: op,
			SourceDocID: srcDoc, SourceProvisionID: src, SourcePath: srcPath, EffFrom: eff, Source: types.LegalProvisionLinkSourceEditor}
	}
	links := []types.LegalProvisionLink{
		// agrees with the parser's quoted rewrite of khoản 5 Điều 8
		link("dieu:8/khoan:5", 10, "sua_doi_bo_sung", "p141-2", "nd141", "dieu:1/khoan:2", day("2026-01-01")),
		// agrees with the parser's phrase change of Điều 3 (code 10)
		link("dieu:3", 10, "sua_doi_bo_sung", "p141-1", "nd141", "dieu:1/khoan:1", nil),
		// editor only
		link("dieu:3/khoan:1", 10, "sua_doi_bo_sung", "p150-1", "nd150", "dieu:1/khoan:1", nil),
		link("dieu:3/khoan:2", 1, "bai_bo", "p150-2", "nd150", "dieu:1/khoan:2", nil),
		link("dieu:8/khoan:1", 14, "giai_thich", "p150-3", "nd150", "dieu:1/khoan:3", nil),
		link("dieu:8/khoan:2/diem:a", 10, "sua_doi_bo_sung", "p150-4", "nd150", "dieu:1/khoan:4", nil),
		link("dieu:8/khoan:2/diem:b", 12, "thay_the", "p150-5", "nd150", "dieu:1/khoan:5", nil),
		// another Điều than the retrieved ones, an appendix, no path: not read
		link("dieu:9", 10, "sua_doi_bo_sung", "p150-1", "nd150", "dieu:1/khoan:1", nil),
		link("phuluc/dieu:8", 10, "sua_doi_bo_sung", "p150-1", "nd150", "", nil),
	}
	require.NoError(t, db.Create(&links).Error)
	return db
}

func editorEdgeOf(t *testing.T, load *LegalAmendLoad, op LegalAmendOp, prov LegalProvisionRef, amender string) LegalAmendEdge {
	t.Helper()
	for _, e := range load.Edges["nd68"] {
		if e.Op == op && e.Provision == prov && e.Amender.CatalogID == amender {
			return e
		}
	}
	t.Fatalf("no %s %s edge of %s", op, prov, amender)
	return LegalAmendEdge{}
}

func TestLegalEditorEdges(t *testing.T) {
	db := legalEditorTestDB(t)
	ctx := context.Background()
	on := LegalAmendLoadOptions{EditorSource: true}
	load, err := LoadLegalAmendmentsWith(ctx, db, []string{"kb-pub"}, []string{"a3", "a8"}, []string{"kb-pub"}, time.UTC, on)
	require.NoError(t, err)
	require.NotNil(t, load.Editor)
	require.Equal(t, LegalEditorMergeStats{Links: 7, Agreed: 2, EditorOnly: 5, WithText: 3}, *load.Editor)

	// agreement → tier 1
	sd := editorEdgeOf(t, load, LegalOpSuaDoi, LegalProvisionRef{Article: "8", Khoan: "5"}, "nd141")
	require.Equal(t, LegalTierVerified, sd.Tier)
	require.Equal(t, []string{LegalAmendSourceParser, LegalAmendSourceEditor}, sd.Sources)
	ph := editorEdgeOf(t, load, LegalOpThayCumTu, LegalProvisionRef{Article: "3"}, "nd141")
	require.Equal(t, LegalTierVerified, ph.Tier)

	// editor only → tier 2, B's text from its source provision
	q := editorEdgeOf(t, load, LegalOpSuaDoi, LegalProvisionRef{Article: "3", Khoan: "1"}, "nd150")
	require.Equal(t, LegalTierUnverified, q.Tier)
	require.Equal(t, "1. Khoản một mới theo Nghị định 150.", q.QuoteText)
	require.Equal(t, []string{LegalAmendSourceEditor}, q.Sources)
	require.Equal(t, LegalProvisionRef{Article: "1", Khoan: "1"}, q.Origin)
	require.Equal(t, "b150", q.Amender.ChunkID)
	require.Equal(t, amendDay("2026-06-01"), q.EffFrom, "B's catalogue date when the editors give none")
	require.Equal(t, q.QuoteText, string([]rune(nd150Dieu1)[q.Quote.Start:q.Quote.End]), "the span is in B's chunk")
	require.Empty(t, q.Unresolved)
	bb := editorEdgeOf(t, load, LegalOpBaiBo, LegalProvisionRef{Article: "3", Khoan: "2"}, "nd150")
	require.Empty(t, bb.Unresolved, bb.Unresolved)
	cum := editorEdgeOf(t, load, LegalOpThayCumTu, LegalProvisionRef{Article: "8", Khoan: "2", Diem: "a"}, "nd150")
	require.Equal(t, "tờ khai", cum.From)
	require.Equal(t, "bản khai", cum.To)
	words := editorEdgeOf(t, load, LegalOpSuaDoi, LegalProvisionRef{Article: "8", Khoan: "2", Diem: "b"}, "nd150")
	require.Empty(t, words.QuoteText)
	require.Equal(t, "5. Các trường hợp khác thực hiện theo quy định của Bộ Tài chính.", words.SourceText)
	editorEdgeOf(t, load, LegalOpGiaiThich, LegalProvisionRef{Article: "8", Khoan: "1"}, "nd150")

	res := load.Resolve(amendAt("2026-10-20"), map[string]bool{"kb-pub": true})
	a3, a8 := res["a3"], res["a8"]
	k5 := unitByProv(t, a8, LegalProvisionRef{Article: "8", Khoan: "5"})
	require.Equal(t, LegalStateCurrentVerified, k5.State, k5.Reasons)
	require.Contains(t, k5.Text, "[Lời văn hiện hành theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026]\n\n5. Sử dụng hóa đơn điện tử")
	k31 := unitByProv(t, a3, LegalProvisionRef{Article: "3", Khoan: "1"})
	require.Equal(t, LegalStateUnverified, k31.State, k31.Reasons)
	require.Contains(t, strings.Join(k31.Reasons, "|"), "khoản 1 Điều 3 có thể đã được sửa đổi theo Nghị định 150/2026/NĐ-CP, từ 01/06/2026 (nguồn: biên tập viên vbpl). Lời văn mới theo nguồn này: “1. Khoản một mới theo Nghị định 150.”")
	k32 := unitByProv(t, a3, LegalProvisionRef{Article: "3", Khoan: "2"})
	require.Equal(t, LegalStateUnverified, k32.State)
	require.Contains(t, strings.Join(k32.Reasons, "|"), "có thể khoản 2 Điều 3 bị bãi bỏ theo Nghị định 150/2026/NĐ-CP")
	k81 := unitByProv(t, a8, LegalProvisionRef{Article: "8", Khoan: "1"})
	require.Equal(t, LegalStateCurrentVerified, k81.State, "giải thích changes no state: %v", k81.Reasons)
	require.Contains(t, k81.Text, "[Được giải thích tại khoản 3 Điều 1 của Nghị định 150/2026/NĐ-CP]")
	d2a := unitByProv(t, a8, LegalProvisionRef{Article: "8", Khoan: "2", Diem: "a"})
	require.Equal(t, LegalStateUnverified, d2a.State)
	require.Contains(t, strings.Join(d2a.Reasons, "|"), "cụm từ “tờ khai” được thay bằng “bản khai”")
	d2b := unitByProv(t, a8, LegalProvisionRef{Article: "8", Khoan: "2", Diem: "b"})
	require.Equal(t, LegalStateUnverified, d2b.State)
	require.Contains(t, strings.Join(d2b.Reasons, "|"), "Nội dung văn bản sửa đổi: “5. Các trường hợp khác")
	require.Contains(t, d2b.Text, "b) giấy phép.", "the stored text stays")

	// B outside the KBs the question may read: its words never reach the model
	require.NoError(t, db.Exec(`UPDATE chunks SET knowledge_base_id = 'kb-ten' WHERE id = 'b150'`).Error)
	held, err := LoadLegalAmendmentsWith(ctx, db, []string{"kb-pub"}, []string{"a3"}, []string{"kb-pub"}, time.UTC, on)
	require.NoError(t, err)
	hq := editorEdgeOf(t, held, LegalOpSuaDoi, LegalProvisionRef{Article: "3", Khoan: "1"}, "nd150")
	require.True(t, hq.TextWithheld)
	require.Empty(t, hq.QuoteText)
	hr := held.Resolve(amendAt("2026-10-20"), map[string]bool{"kb-pub": true})["a3"]
	require.Equal(t, LegalStateAmendedTextMissing, unitByProv(t, hr, LegalProvisionRef{Article: "3", Khoan: "1"}).State)
	require.NotContains(t, hr.ModelContent, "Khoản một mới")
}

// Setting off: the load and every text are exactly as without the source,
// links table or not.
func TestLegalEditorSourceOffIsIdentical(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	db := legalEditorTestDB(t)
	answer := "Theo khoản 5 Điều 8 Nghị định 68/2026/NĐ-CP, hộ kinh doanh sử dụng hóa đơn điện tử."
	kbs, ids := []string{"kb-pub"}, []string{"a3", "a8"}
	baseW, err := LegalCurrentWordingResolved(ctx, db, kbs, ids, now)
	require.NoError(t, err)
	baseH, err := LegalAmendmentHistoryResolved(ctx, db, kbs, ids, answer, now)
	require.NoError(t, err)
	offW, err := LegalCurrentWordingResolvedWith(ctx, db, kbs, ids, now, LegalAmendLoadOptions{})
	require.NoError(t, err)
	require.Equal(t, baseW, offW)
	offH, err := LegalAmendmentHistoryResolvedWith(ctx, db, kbs, ids, answer, now, LegalAmendLoadOptions{})
	require.NoError(t, err)
	require.Equal(t, baseH, offH)
	load, err := LoadLegalAmendmentsWith(ctx, db, kbs, ids, kbs, time.UTC, LegalAmendLoadOptions{})
	require.NoError(t, err)
	require.Nil(t, load.Editor)
	for _, list := range load.Edges {
		for _, e := range list {
			require.NotContains(t, e.Sources, LegalAmendSourceEditor)
		}
	}
	// on, the same call differs
	onW, err := LegalCurrentWordingResolvedWith(ctx, db, kbs, ids, now, LegalAmendLoadOptions{EditorSource: true})
	require.NoError(t, err)
	require.NotEqual(t, offW, onW)
	onH, err := LegalAmendmentHistoryResolvedWith(ctx, db, kbs, ids, answer, now, LegalAmendLoadOptions{EditorSource: true})
	require.NoError(t, err)
	require.NotContains(t, onH, "(Chưa xác minh.)", "agreed: the history line is verified")
	// without the table (migration not applied), off still reads nothing new
	require.NoError(t, db.Migrator().DropTable(&types.LegalProvisionLink{}))
	noTable, err := LegalCurrentWordingResolvedWith(ctx, db, kbs, ids, now, LegalAmendLoadOptions{})
	require.NoError(t, err)
	require.Equal(t, baseW, noTable)
	_, err = LegalCurrentWordingResolvedWith(ctx, db, kbs, ids, now, LegalAmendLoadOptions{EditorSource: true})
	require.Error(t, err, "on without the table: an error, the caller falls back to the old wording")
}

// An offline reader (importer rows) replaces the table read.
func TestLegalEditorLinksFromJSON(t *testing.T) {
	db := legalEditorTestDB(t)
	require.NoError(t, db.Exec(`DELETE FROM legal_provision_links`).Error)
	row, _ := json.Marshal(types.LegalProvisionLink{TargetDocID: "nd68", TargetProvisionID: "x", TargetPath: "dieu:3/khoan:2", RelationCode: 1, Op: "bai_bo",
		SourceDocID: "nd150", SourceProvisionID: "p150-2", SourcePath: "dieu:1/khoan:2"})
	unresolved, _ := json.Marshal(types.LegalProvisionLink{TargetDocID: "nd68", TargetProvisionID: "y", TargetPath: "dieu:3/khoan:1", RelationCode: 1, SourceProvisionID: "nowhere"})
	opt := LegalAmendLoadOptions{EditorSource: true, EditorLinks: legalEditorLinksFromJSON([][]byte{row, unresolved, []byte("not json")})}
	load, err := LoadLegalAmendmentsWith(context.Background(), db, []string{"kb-pub"}, []string{"a3"}, []string{"kb-pub"}, time.UTC, opt)
	require.NoError(t, err)
	require.Equal(t, 1, load.Editor.EditorOnly)
	editorEdgeOf(t, load, LegalOpBaiBo, LegalProvisionRef{Article: "3", Khoan: "2"}, "nd150")
}

// Codes 14 / 6 note without a change; 11 / 5 suspend over their interval.
func TestLegalEditorOpsWithoutText(t *testing.T) {
	c := LegalProvisionChunk{ID: "a", Content: "#### Điều 4. Thuế\n\n1. Mức thuế 5%.\n\n2. Mức thuế 10%.\n\n3. Mức thuế 15%.",
		Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{"4"}}, Doc: LegalAmendDoc{CatalogID: "a", Number: "1/2020/NĐ-CP"}}
	b := LegalAmendDoc{CatalogID: "b", Number: "2/2024/NĐ-CP", Type: "Nghị định"}
	edge := func(op LegalAmendOp, khoan string, from, to string) LegalAmendEdge {
		e := LegalAmendEdge{Op: op, Amender: b, Target: c.Doc, Provision: LegalProvisionRef{Article: "4", Khoan: khoan},
			Origin: LegalProvisionRef{Article: "2", Khoan: "1"}, Sources: []string{LegalAmendSourceEditor}, Tier: LegalTierVerified, EffFrom: amendDay(from)}
		if to != "" {
			e.EffTo = amendDay(to)
		}
		return e
	}
	edges := []LegalAmendEdge{
		edge(LegalOpGiaiThich, "1", "2024-01-01", ""),
		edge(LegalOpDinhChinh, "2", "2024-01-01", ""),
		edge(LegalOpTamNgung, "3", "2026-01-01", "2026-12-31"),
	}
	res := ResolveLegalProvisions(c, edges, amendAt("2026-10-20"), LegalResolveOptions{})
	k1 := unitByProv(t, res, LegalProvisionRef{Article: "4", Khoan: "1"})
	require.Equal(t, LegalStateOriginalInForce, k1.State, k1.Reasons)
	require.Contains(t, k1.Text, "[Được giải thích tại khoản 1 Điều 2 của Nghị định 2/2024/NĐ-CP]\n\n1. Mức thuế 5%.")
	k2 := unitByProv(t, res, LegalProvisionRef{Article: "4", Khoan: "2"})
	require.Equal(t, LegalStateOriginalInForce, k2.State)
	require.Contains(t, k2.Text, "[Được đính chính tại khoản 1 Điều 2 của Nghị định 2/2024/NĐ-CP]")
	k3 := unitByProv(t, res, LegalProvisionRef{Article: "4", Khoan: "3"})
	require.Equal(t, LegalStateNotApplicable, k3.State)
	require.Contains(t, k3.Text, "[Tạm ngưng hiệu lực từ 01/01/2026 đến 31/12/2026 theo Nghị định 2/2024/NĐ-CP]")
	// after the interval: the original again
	after := ResolveLegalProvisions(c, edges, amendAt("2027-01-02"), LegalResolveOptions{})
	require.Equal(t, LegalStateOriginalInForce, unitByProv(t, after, LegalProvisionRef{Article: "4", Khoan: "3"}).State)
	// đình chỉ without an end
	dc := ResolveLegalProvisions(c, []LegalAmendEdge{edge(LegalOpDinhChi, "3", "2026-01-01", "")}, amendAt("2026-10-20"), LegalResolveOptions{})
	d3 := unitByProv(t, dc, LegalProvisionRef{Article: "4", Khoan: "3"})
	require.Equal(t, LegalStateNotApplicable, d3.State)
	require.Contains(t, d3.Text, "[Bị đình chỉ thi hành từ 01/01/2026 theo Nghị định 2/2024/NĐ-CP]")
	// a catalogue relation of the explaining document is not answered by
	// its giải thích: other units keep the catalogue state
	cat := []LegalCatalogAmendment{{Amender: b, EffFrom: amendDay("2024-01-01")}}
	withCat := ResolveLegalProvisions(c, edges[:1], amendAt("2026-10-20"), LegalResolveOptions{Catalogue: cat})
	require.Equal(t, LegalStateAmendedTextMissing, unitByProv(t, withCat, LegalProvisionRef{Article: "4", Khoan: "1"}).State)
}

func TestLegalAddedPlace(t *testing.T) {
	cases := []struct {
		at     LegalProvisionRef
		label  string
		parent LegalProvisionRef
		new    string
		ok     bool
	}{
		{LegalProvisionRef{Article: "5"}, "3a.", LegalProvisionRef{Article: "5"}, "3a", true},
		{LegalProvisionRef{Article: "5"}, "điều 5a.", LegalProvisionRef{}, "5a", true},
		{LegalProvisionRef{Article: "5", Khoan: "2"}, "c)", LegalProvisionRef{Article: "5", Khoan: "2"}, "c", true},
		{LegalProvisionRef{Article: "5", Khoan: "2"}, "2a.", LegalProvisionRef{Article: "5"}, "2a", true},
		{LegalProvisionRef{Article: "5", Khoan: "2", Diem: "a"}, "đ)", LegalProvisionRef{Article: "5", Khoan: "2"}, "đ", true},
		{LegalProvisionRef{Article: "5", Khoan: "2"}, "2.", LegalProvisionRef{}, "", false},
		{LegalProvisionRef{Article: "5", Khoan: "2", Diem: "a"}, "3.", LegalProvisionRef{}, "", false},
		{LegalProvisionRef{Article: "5"}, "", LegalProvisionRef{}, "", false},
	}
	for _, tc := range cases {
		parent, label, ok := legalAddedPlace(tc.at, tc.label)
		require.Equal(t, tc.ok, ok, "%v %q", tc.at, tc.label)
		if ok {
			require.Equal(t, tc.parent, parent)
			require.Equal(t, tc.new, label)
		}
	}
}

func TestLegalPathRef(t *testing.T) {
	p, ok := legalPathRef("dieu:5A/khoan:2/diem:đ")
	require.True(t, ok)
	require.Equal(t, LegalProvisionRef{Article: "5a", Khoan: "2", Diem: "đ"}, p)
	for _, bad := range []string{"", "phuluc/dieu:1", "khoan:2", "dieu:", "chuong:1"} {
		_, ok := legalPathRef(bad)
		require.False(t, ok, bad)
	}
}
