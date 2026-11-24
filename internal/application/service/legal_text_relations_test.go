package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLegalTextCitations(t *testing.T) {
	text := `Quy định số 117-QĐ/TW của Bộ Chính trị về xin lỗi

BAN CHẤP HÀNH TRUNG ƯƠNG * Số 117-QĐ/TW

- Căn cứ Điều lệ Đảng;
- Căn cứ Quy định số 24-QĐ/TW, ngày 30/7/2021 của Ban Chấp hành Trung ương; Quy định số 22-QĐ/TW, ngày 28/7/2021;
Thực hiện Kết luận số 107-KL/TW ngày 15/3/2026 của Bộ Chính trị, Ban Tổ chức Trung ương hướng dẫn như sau:
Điều 5. Quy định này thay thế Quy định số 102-QĐ/TW ngày 15/11/2017 và bãi bỏ Hướng dẫn số 04-HD/UBKTTW.
Các nội dung đã nêu tại Nghị quyết số 18-NQ/TW và Nghị định số 30/2020/NĐ-CP được giữ nguyên.
Việc kiểm theo dõi Kết luận số 107-KL/TW tiếp tục.`
	got := LegalTextCitations(text, "117-QD/TW")
	want := map[string]types.LegalRelation{
		"24-QD/TW":      types.LegalRelationBasedOn,
		"22-QD/TW":      types.LegalRelationBasedOn,
		"107-KL/TW":     types.LegalRelationBasedOn,
		"102-QD/TW":     types.LegalRelationReplaces,
		"04-HD/UBKTTW":  types.LegalRelationRepeals,
		"18-NQ/TW":      types.LegalRelationReferences,
		"30/2020/ND-CP": types.LegalRelationReferences,
	}
	have := map[string]types.LegalRelation{}
	for _, c := range got {
		_, dup := have[c.Key]
		require.False(t, dup, "key %s listed twice", c.Key)
		have[c.Key] = c.Relation
	}
	require.Equal(t, want, have)
}

func TestLegalTextRelationOf(t *testing.T) {
	cases := map[string]types.LegalRelation{
		"- Căn cứ ": types.LegalRelationBasedOn,
		"Bãi bỏ Quyết định 1; thay thế ":                types.LegalRelationReplaces,
		"sửa đổi, bổ sung một số điều của ":             types.LegalRelationAmends,
		"Điều 5. Quy định này thay thế ":                types.LegalRelationReplaces,
		"1. Quyết định này thay thế ":                   types.LegalRelationReplaces,
		"thực hiện theo dõi ":                           types.LegalRelationBasedOn,
		"đã căn cứ vào đó nhưng dẫn chiếu ":             types.LegalRelationBasedOn,
		"Trên cơ sở Kết luận này, Ban Tổ chức sửa đổi ": types.LegalRelationReferences,
		"Quy định này được thay thế bởi ":               types.LegalRelationReferences,
		"(vừa được bổ sung một số điều theo Luật số ":   types.LegalRelationReferences,
		"hoàn thiện các dự thảo sửa đổi, bổ sung ":      types.LegalRelationReferences,
		"việc số hoá thực hiện theo ":                   types.LegalRelationReferences,
		"được quy định tại ":                            types.LegalRelationReferences,
		// an issuer's name ends where the document's description begins
		", ngày 15-02-2012 của Bộ Giáo dục và Đào tạo về việc sửa đổi, bổ sung một số điều của Quy chế kèm theo ": types.LegalRelationReferences,
		", ngày 21/9/2026 của Văn phòng Trung ương Đảng về việc thông báo kết luận về sửa đổi, bổ sung ":          types.LegalRelationReferences,
	}
	for before, want := range cases {
		got, found := legalTextRelationOf(before, false)
		require.Equal(t, want, got, before)
		require.Equal(t, want != types.LegalRelationReferences, found, before)
	}
	// in the header the title's own wording counts
	got, found := legalTextRelationOf("Quy định số 232-QĐ/TW của Bộ Chính trị về sửa đổi, bổ sung một số điều của Quy định số ", true)
	require.True(t, found)
	require.Equal(t, types.LegalRelationAmends, got)
}

// The title's wording counts (an amending document names its subject
// there); the same words further down are someone else's act.
func TestLegalTextCitationsHeader(t *testing.T) {
	text := "Quy định số 232-QĐ/TW của Bộ Chính trị về sửa đổi, bổ sung một số điều của Quy định số 24-QĐ/TW\n\n" +
		"BAN CHẤP HÀNH TRUNG ƯƠNG * Số 232-QĐ/TW\n" +
		"(2) Hoàn thiện dự thảo Quy định về thi hành Điều lệ Đảng (thay thế Quy định số 29-QĐ/TW, ngày 20/01/2025).\n"
	have := map[string]types.LegalRelation{}
	for _, c := range LegalTextCitations(text, "232-QD/TW") {
		have[c.Key] = c.Relation
	}
	require.Equal(t, map[string]types.LegalRelation{
		"24-QD/TW": types.LegalRelationAmends, "29-QD/TW": types.LegalRelationReferences,
	}, have)
}

// A list carries its wording to every member; other words between two
// citations start afresh.
func TestLegalTextCitationsList(t *testing.T) {
	got := LegalTextCitations("Bãi bỏ Quyết định số 1-QĐ/TW, ngày 1/2/2020 của Bộ Chính trị, Quyết định số 2-QĐ/TW và Kết luận 3-KL/TW; "+
		"xem thêm Nghị quyết 4-NQ/TW.\nCăn cứ Hướng dẫn 5-HD/TW\nNghị quyết 6-NQ/TW (đã nêu)", "")
	have := map[string]types.LegalRelation{}
	for _, c := range got {
		have[c.Key] = c.Relation
	}
	require.True(t, legalTextListGap(", ngày 1/2/2020 của Ban Chấp hành Trung ương Đảng; Quyết định số "))
	require.False(t, legalTextListGap(", ngày 1/2/2020 của Bộ Chính trị về việc sửa đổi "))
	require.Equal(t, map[string]types.LegalRelation{
		"1-QD/TW": types.LegalRelationRepeals, "2-QD/TW": types.LegalRelationRepeals, "3-KL/TW": types.LegalRelationRepeals,
		"4-NQ/TW": types.LegalRelationReferences, "5-HD/TW": types.LegalRelationBasedOn, "6-NQ/TW": types.LegalRelationReferences,
	}, have)
}

type legalTextFixture struct {
	t  *testing.T
	db *gorm.DB
}

func newLegalTextFixture(t *testing.T) *legalTextFixture {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{}, &types.LegalDocument{},
		&types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))
	return &legalTextFixture{t: t, db: db}
}

func (f *legalTextFixture) doc(id, source, num, issued string) {
	require.NoError(f.t, f.db.Create(&types.LegalDocument{ID: id, Source: source, DocumentNumber: num,
		NumberKey: num, DocType: "Quy định", Title: num, IssuedOn: legalFocusDay(issued)}).Error)
}

// hold gives doc an approved copy whose text is the chunks given.
func (f *legalTextFixture) hold(doc string, chunks ...string) {
	kid := "k-" + doc
	require.NoError(f.t, f.db.Create(&types.Knowledge{ID: kid, KnowledgeBaseID: "kb", Title: doc}).Error)
	require.NoError(f.t, f.db.Create(&types.LegalDocumentLink{KnowledgeID: kid, KnowledgeBaseID: "kb",
		LegalDocumentID: doc, ReviewStatus: types.LegalReviewApproved}).Error)
	for i, c := range chunks {
		require.NoError(f.t, f.db.Create(&types.Chunk{ID: uuid.NewString(), KnowledgeID: kid, KnowledgeBaseID: "kb",
			Content: c, ChunkIndex: i, ChunkType: types.ChunkTypeText}).Error)
	}
	// a summary chunk may invent a số hiệu: never read
	require.NoError(f.t, f.db.Create(&types.Chunk{ID: uuid.NewString(), KnowledgeID: kid, KnowledgeBaseID: "kb",
		Content: "Tóm tắt: liên quan Quyết định 999-QĐ/TW", ChunkIndex: 99, ChunkType: types.ChunkTypeSummary}).Error)
}

func (f *legalTextFixture) rels(from string) map[string]types.LegalRelation {
	var rows []types.LegalDocumentRelation
	require.NoError(f.t, f.db.Where("from_id = ?", from).Find(&rows).Error)
	out := map[string]types.LegalRelation{}
	for _, r := range rows {
		out[r.ToID] = r.Relation
	}
	return out
}

func TestBuildLegalTextRelations(t *testing.T) {
	f := newLegalTextFixture(t)
	ctx := context.Background()
	f.doc("dcs-a", "dangcongsan", "117-QD/TW", "2023-08-18")
	f.doc("dcs-b", "dangcongsan", "24-QD/TW", "2021-07-30")
	f.doc("dcs-c", "dangcongsan", "102-QD/TW", "2017-11-15")
	f.doc("dcs-x", "dangcongsan", "999-QD/TW", "2020-01-01")
	// the same số hiệu on vbpl and on the feed: the feed's row wins; among
	// vbpl rows the newest one issued before the citing document wins
	f.doc("vbpl-b", "vbpl", "24-QD/TW", "2010-01-01")
	f.doc("vbpl-n1", "vbpl", "30/2020/ND-CP", "2020-03-05")
	f.doc("vbpl-n2", "vbpl", "30/2020/ND-CP", "2026-01-01")
	f.hold("dcs-a",
		"Số 117-QĐ/TW\n- Căn cứ Quy định số 24-QĐ/TW ngày 30/7/2021;\n",
		"Điều 5. Quy định này thay thế Quy định số 102-QĐ/TW; xem Nghị định số 30/2020/NĐ-CP và Kết luận số 1-KL/TW.")
	f.doc("dcs-nohold", "dangcongsan", "1-HD/TW", "2022-01-01")
	// a vbpl edge of the same pair stays vbpl's
	require.NoError(t, f.db.Create(&types.LegalDocumentRelation{FromID: "dcs-a", ToID: "dcs-b",
		Relation: types.LegalRelationBasedOn, SourceLabel: "Căn cứ"}).Error)
	// a stale text edge of dcs-a goes away on rebuild
	require.NoError(t, f.db.Create(&types.LegalDocumentRelation{FromID: "dcs-a", ToID: "dcs-x",
		Relation: types.LegalRelationReferences, SourceLabel: LegalTextRelationLabel}).Error)

	stats, err := BuildLegalTextRelations(ctx, f.db, "dangcongsan", nil)
	require.NoError(t, err)
	require.Equal(t, 5, stats.Documents)
	require.Equal(t, 4, stats.WithoutText)
	require.Equal(t, 4, stats.Citations, "24, 102, 30/2020, 1-KL (self skipped)")
	require.Equal(t, 1, stats.Unresolved, "1-KL/TW is nobody's")
	require.Equal(t, 3, stats.Relations)
	require.Equal(t, map[string]types.LegalRelation{
		"dcs-b":   types.LegalRelationBasedOn,
		"dcs-c":   types.LegalRelationReplaces,
		"vbpl-n1": types.LegalRelationReferences,
	}, f.rels("dcs-a"))
	var kept types.LegalDocumentRelation
	require.NoError(t, f.db.Where("from_id = ? AND to_id = ?", "dcs-a", "dcs-b").First(&kept).Error)
	require.Equal(t, "Căn cứ", kept.SourceLabel, "vbpl's edge is kept, not relabelled")

	// one document only, by id
	stats, err = BuildLegalTextRelations(ctx, f.db, "", []string{"dcs-a"})
	require.NoError(t, err)
	require.Equal(t, 1, stats.Documents)
	require.Equal(t, 3, stats.Relations)
	_, err = BuildLegalTextRelations(ctx, f.db, "", nil)
	require.Error(t, err)
}

// A catalogue import replaces vbpl.vn's relations and leaves the ones read
// from the texts alone (the 2026-11-20 sync had wiped them).
func TestReplaceRelationsKeepsTextEdges(t *testing.T) {
	f := newLegalTextFixture(t)
	ctx := context.Background()
	require.NoError(t, f.db.Create(&types.LegalDocumentRelation{FromID: "a", ToID: "b", Relation: types.LegalRelationBasedOn, SourceLabel: LegalTextRelationLabel}).Error)
	require.NoError(t, f.db.Create(&types.LegalDocumentRelation{FromID: "c", ToID: "d", Relation: types.LegalRelationReplaces, SourceLabel: "Thay thế"}).Error)
	repo := repository.NewLegalCatalogRepository(f.db)
	require.NoError(t, repo.ReplaceRelations(ctx, []*types.LegalDocumentRelation{{FromID: "e", ToID: "f", Relation: types.LegalRelationAmends, SourceLabel: "Sửa đổi, bổ sung"}}))
	var rows []types.LegalDocumentRelation
	require.NoError(t, f.db.Order("from_id").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, "a", rows[0].FromID)
	require.Equal(t, "e", rows[1].FromID)
}

// A "Điều khoản chuyển tiếp" names the document the new one takes over
// from without "thay thế" wording (Nghị định 331/2026/NĐ-CP → 85/2016/NĐ-CP,
// which vbpl.vn still listed "Còn hiệu lực" on 2026-11-23): a plain mention
// there reads as succeeds; "căn cứ" wording and the next article do not.
func TestLegalTextCitationsTransitional(t *testing.T) {
	text := `Nghị định số 331/2026/NĐ-CP Về bảo vệ an ninh mạng đối với hệ thống thông tin

Căn cứ Luật An ninh mạng số 116/2025/QH15;
#### Điều 38. Hiệu lực thi hành
Nghị định này có hiệu lực thi hành kể từ ngày 19 tháng 8 năm 2026.
#### Điều 39. Điều khoản chuyển tiếp
1. Đối với hệ thống thông tin đang đầu tư, trong thời hạn 06 tháng kể từ ngày Luật An ninh mạng số 116/2025/QH15 có hiệu lực thi hành, hoàn thành việc thẩm định cấp độ theo quy định tại Nghị định số 85/2016/NĐ-CP.
2. Giấy phép cấp theo Thông tư số 12/2022/TT-BTTTT có giá trị đến hết thời hạn.
#### Điều 40. Tổ chức thực hiện
Bộ Công an hướng dẫn Nghị định này; xem thêm Nghị định số 53/2022/NĐ-CP.`
	have := map[string]types.LegalRelation{}
	for _, c := range LegalTextCitations(text, "331/2026/ND-CP") {
		have[c.Key] = c.Relation
	}
	require.Equal(t, map[string]types.LegalRelation{
		"116/2025/QH15":    types.LegalRelationBasedOn, // the căn cứ line wins over the transitional mention
		"85/2016/ND-CP":    types.LegalRelationSucceeds,
		"12/2022/TT-BTTTT": types.LegalRelationSucceeds, // resolve drops it: another kind of document
		"53/2022/ND-CP":    types.LegalRelationReferences,
	}, have)

	// an annex or a chapter heading ends the transitional article
	lines := legalTextTransitionalLines([]rune("Điều 9. Điều khoản chuyển tiếp\nxem A\nPhụ lục I\nxem B\n"))
	require.True(t, lines[0] && lines[len([]rune("Điều 9. Điều khoản chuyển tiếp\n"))])
	require.False(t, lines[len([]rune("Điều 9. Điều khoản chuyển tiếp\nxem A\nPhụ lục I\n"))])
}

// Of a vbpl.vn document (the portal lists its relations itself) only
// succeeds is stored, and only towards a document of the same type issued
// earlier; a feed document keeps every text relation.
func TestBuildLegalTextRelationsSucceeds(t *testing.T) {
	f := newLegalTextFixture(t)
	ctx := context.Background()
	typed := func(id, source, num, docType, issued string) {
		require.NoError(t, f.db.Create(&types.LegalDocument{ID: id, Source: source, DocumentNumber: num,
			NumberKey: num, DocType: docType, Title: num, IssuedOn: legalFocusDay(issued)}).Error)
	}
	typed("nd331", "vbpl", "331/2026/ND-CP", "Nghị định", "2026-08-19")
	typed("nd85", "vbpl", "85/2016/ND-CP", "Nghị định", "2016-07-01")
	for _, d := range []struct{ id, title string }{
		{"nd331", "Nghị định số 331/2026/NĐ-CP Về bảo vệ an ninh mạng đối với hệ thống thông tin"},
		{"nd85", "Nghị định số 85/2016/NĐ-CP Về bảo đảm an toàn hệ thống thông tin theo cấp độ"},
	} {
		require.NoError(t, f.db.Model(&types.LegalDocument{}).Where("id = ?", d.id).Update("title", d.title).Error)
	}
	typed("nd999", "vbpl", "999/2027/ND-CP", "Nghị định", "2027-01-01") // issued later: not a predecessor
	typed("tt12", "vbpl", "12/2022/TT-BTTTT", "Thông tư", "2022-08-12")
	typed("l116", "vbpl", "116/2025/QH15", "Luật", "2025-06-27")
	f.hold("nd331",
		"Số 331/2026/NĐ-CP\nCăn cứ Luật số 116/2025/QH15;\n",
		"#### Điều 39. Điều khoản chuyển tiếp\n1. Thẩm định theo Nghị định số 85/2016/NĐ-CP và Nghị định số 999/2027/NĐ-CP; giấy phép theo Thông tư số 12/2022/TT-BTTTT còn giá trị.",
		"#### Điều 40. Tổ chức thực hiện\nXem Nghị định số 53/2022/NĐ-CP.")
	stats, err := BuildLegalTextRelations(ctx, f.db, "", []string{"nd331"})
	require.NoError(t, err)
	require.Equal(t, 1, stats.Relations)
	require.Equal(t, map[string]types.LegalRelation{"nd85": types.LegalRelationSucceeds}, f.rels("nd331"))

	// the same text pushed by a feed keeps its căn cứ and mentions too
	typed("dcs331", "dangcongsan", "331-QD/TW", "Quyết định", "2026-08-19")
	typed("dcs85", "dangcongsan", "85-QD/TW", "Quyết định", "2016-07-01")
	f.hold("dcs331", "Số 331-QĐ/TW\nCăn cứ Luật số 116/2025/QH15;\n",
		"Điều 9. Điều khoản chuyển tiếp\nViệc đang làm theo Quyết định số 85-QĐ/TW tiếp tục; xem Nghị định số 53/2022/NĐ-CP.")
	_, err = BuildLegalTextRelations(ctx, f.db, "", []string{"dcs331"})
	require.NoError(t, err)
	require.Equal(t, map[string]types.LegalRelation{
		"l116": types.LegalRelationBasedOn, "dcs85": types.LegalRelationSucceeds,
	}, f.rels("dcs331"))
}

// The transitional clause also names documents the new one merely applies;
// the titles must share a subject-word pair (the 2026-11-23 vbpl backfill
// had read "Luật Công an nhân dân" as taking over from "Luật Tổ chức chính
// quyền địa phương").
func TestLegalTextSameSubject(t *testing.T) {
	yes := [][2]string{
		{"Nghị định số 331/2026/NĐ-CP Về bảo vệ an ninh mạng đối với hệ thống thông tin",
			"Nghị định số 85/2016/NĐ-CP Về bảo đảm an toàn hệ thống thông tin theo cấp độ"},
		{"Nghị định số 96/2016/NĐ-CP Quy định điều kiện về an ninh, trật tự đối với một số ngành, nghề đầu tư kinh doanh có điều kiện",
			"Nghị định số 72/2009/NĐ-CP Quy định điều kiện về an ninh, trật tự đối với một số ngành, nghề kinh doanh có điều kiện"},
		{"Thông tư số 20/2026/TT-BNV Quy định chi tiết và hướng dẫn thi hành", "Thông tư số 20/2025/TT-BNV Quy định chi tiết thi hành một số điều"},
	}
	for _, p := range yes {
		require.True(t, legalTextSameSubject(p[0], p[1]), p[0])
	}
	no := [][2]string{
		{"Luật Công an nhân dân số 37/2018/QH14", "Luật Tổ chức chính quyền địa phương số 77/2015/QH13"},
		{"Nghị quyết số 41/2017/QH14 Về việc thi hành Bộ luật Hình sự", "Nghị quyết số 19/2003/QH11 Về nhiệm vụ năm 2004"},
		{"Nghị định số 39/2019/NĐ-CP Về tổ chức và hoạt động của Quỹ Phát triển doanh nghiệp nhỏ và vừa",
			"Nghị định số 51/2016/NĐ-CP Quy định quản lý lao động, tiền lương và tiền thưởng"},
	}
	for _, p := range no {
		require.False(t, legalTextSameSubject(p[0], p[1]), p[0])
	}
}
