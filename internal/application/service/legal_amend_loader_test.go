package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// NĐ 141/2026 Điều 1 as stored on dev (chunk 80cafa39), shortened.
const nd141Dieu1 = "#### Điều 1. Sửa đổi, bổ sung một số điều của Nghị định số 68/2026/NĐ-CP ngày 05 tháng 3 năm 2026 của Chính phủ quy định về chính sách thuế và quản lý thuế đối với hộ kinh doanh, cá nhân kinh doanh\n\n" +
	"1. Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ đồng” tại Điều 3, Điều 4, khoản 1 Điều 8 Nghị định số 68/2026/NĐ-CP.\n\n" +
	"2. Sửa đổi, bổ sung khoản 5 Điều 8 Nghị định số 68/2026/NĐ-CP như sau:\n\n" +
	"> \"5. Sử dụng hóa đơn điện tử\n\n" +
	"> a) Hộ kinh doanh có doanh thu năm trên 01 tỷ đồng thì phải áp dụng hóa đơn điện tử;\n\n" +
	"> b) Hộ kinh doanh có doanh thu năm từ 01 tỷ đồng trở xuống được đăng ký sử dụng hóa đơn điện tử.\".\n\n" +
	"3. Bổ sung điểm c vào khoản 2 Điều 8 Nghị định số 68/2026/NĐ-CP như sau:\n\n" +
	"> “c) Hộ kinh doanh mới thành lập.”."

func TestLegalQuoteGroups(t *testing.T) {
	groups := legalQuoteGroups(nd141Dieu1)
	require.Len(t, groups, 2)
	require.True(t, strings.HasPrefix(groups[0].leadIn, "2. Sửa đổi, bổ sung khoản 5 Điều 8"))
	require.Len(t, groups[0].blocks, 1)
	blk := groups[0].blocks[0]
	require.Equal(t, "5.", blk.label)
	require.Equal(t, "5. Sử dụng hóa đơn điện tử\n\na) Hộ kinh doanh có doanh thu năm trên 01 tỷ đồng thì phải áp dụng hóa đơn điện tử;\n\nb) Hộ kinh doanh có doanh thu năm từ 01 tỷ đồng trở xuống được đăng ký sử dụng hóa đơn điện tử.", blk.text)
	// the span is rune offsets into the stored content, between the marks
	runes := []rune(nd141Dieu1)
	inner := string(runes[blk.span.Start:blk.span.End])
	require.True(t, strings.HasPrefix(inner, "5. Sử dụng"), inner)
	require.True(t, strings.HasSuffix(inner, "hóa đơn điện tử."), inner)
	lead := string(runes[groups[0].leadInSpan.Start:groups[0].leadInSpan.End])
	require.Equal(t, groups[0].leadIn, lead)

	_, b, _, ok := legalQuoteFor(groups, LegalProvisionRef{Article: "8", Khoan: "5"}, "")
	require.True(t, ok)
	require.Equal(t, blk.text, b.text)
	_, b, _, ok = legalQuoteFor(groups, LegalProvisionRef{Article: "8", Khoan: "2"}, "c")
	require.True(t, ok)
	require.Equal(t, "c) Hộ kinh doanh mới thành lập.", b.text)
	_, _, reason, ok := legalQuoteFor(groups, LegalProvisionRef{Article: "8", Khoan: "4"}, "")
	require.False(t, ok)
	require.Contains(t, reason, "không tìm thấy")

	// one lead-in, two targets: one block each
	multi := "1. Sửa đổi khoản 1 và khoản 3 Điều 5 như sau:\n\n> “1. Khoản một mới.\n\n> a) điểm a mới.”;\n\n> “3. Khoản ba mới.”."
	g := legalQuoteGroups(multi)
	require.Len(t, g, 1)
	require.Len(t, g[0].blocks, 2)
	_, b1, _, ok := legalQuoteFor(g, LegalProvisionRef{Article: "5", Khoan: "1"}, "")
	require.True(t, ok)
	require.Equal(t, "1. Khoản một mới.\n\na) điểm a mới.", b1.text)
	_, b3, _, ok := legalQuoteFor(g, LegalProvisionRef{Article: "5", Khoan: "3"}, "")
	require.True(t, ok)
	require.Equal(t, "3. Khoản ba mới.", b3.text)
	require.Equal(t, "3. Khoản ba mới.", string([]rune(multi)[b3.span.Start:b3.span.End]))
	require.Equal(t, utf8.RuneCountInString(multi[:strings.Index(multi, "3. Khoản ba")]), b3.span.Start)

	// NĐ 266/2025: "khoản 4" quoted under another Điều and under a parent
	// lead-in naming Điều 27 — the parent decides
	nested := "15. Sửa đổi, bổ sung khoản 4 Điều 24 như sau:\n\n> “4. Khoản bốn của Điều 24.”.\n\n" +
		"17. Sửa đổi, bổ sung khoản 1, khoản 4 Điều 27 như sau:\n\na) Sửa đổi, bổ sung khoản 1 như sau:\n\n> “1. Khoản một của Điều 27.”.\n\n" +
		"b) Sửa đổi, bổ sung khoản 4 như sau:\n\n> “4. Khoản bốn của Điều 27.”."
	ng := legalQuoteGroups(nested)
	_, b4, reason, ok := legalQuoteFor(ng, LegalProvisionRef{Article: "27", Khoan: "4"}, "")
	require.True(t, ok, reason)
	require.Equal(t, "4. Khoản bốn của Điều 27.", b4.text)
	_, b4, _, ok = legalQuoteFor(ng, LegalProvisionRef{Article: "24", Khoan: "4"}, "")
	require.True(t, ok)
	require.Equal(t, "4. Khoản bốn của Điều 24.", b4.text)
}

func TestValidateLegalAmendEdge(t *testing.T) {
	structure := legalStructureFrom([]*vietnamese_legal.LegalMetadata{
		{ArticleNos: []string{"5"}, KhoanNos: []string{"1", "2"}, SubdivisionRefs: []string{"khoan:1", "khoan:2", "khoan:2/diem:a"}},
	})
	base := func(op LegalAmendOp, prov LegalProvisionRef, lead string) LegalAmendEdge {
		return LegalAmendEdge{Op: op, Provision: prov, LeadInText: lead, Tier: LegalTierLegacyParserPhrase}
	}
	cases := []struct {
		name               string
		e                  LegalAmendEdge
		rejected, unresolv string
	}{
		{"ok sua_doi", LegalAmendEdge{Op: LegalOpSuaDoi, Provision: LegalProvisionRef{Article: "5", Khoan: "2"}, QuoteText: "2. Mới", LeadInText: "Sửa đổi khoản 2 Điều 5 như sau:"}, "", ""},
		{"văn bản này không áp dụng (B's own scope)", base(LegalOpKhongApDung, LegalProvisionRef{Article: "5", Khoan: "1"},
			"Nghị định này không áp dụng đối với đối tượng quy định tại khoản 1 Điều 5 Luật Đầu tư công"), "phạm vi của chính văn bản sửa đổi", ""},
		{"Thông tư này không điều chỉnh", base(LegalOpKhongApDung, LegalProvisionRef{Article: "5"}, "2. Thông tư này không điều chỉnh hoạt động quy định tại Điều 5 Luật X."), "phạm vi", ""},
		{"quy định của A không áp dụng: an operation", base(LegalOpKhongApDung, LegalProvisionRef{Article: "5", Khoan: "1"},
			"Các quy định tại khoản 1 Điều 5 của Thông tư liên tịch số 27/2013/TTLT-BTC-BKHĐT không áp dụng đối với dự án ODA."), "", ""},
		{"verb mismatch", base(LegalOpBaiBo, LegalProvisionRef{Article: "5", Khoan: "1"}, "Sửa đổi khoản 1 Điều 5 như sau:"), "", "động từ"},
		{"phrase verb ok", base(LegalOpThayCumTu, LegalProvisionRef{Article: "5"}, "1. Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ đồng” tại Điều 5"), "", ""},
		{"target missing", base(LegalOpBaiBo, LegalProvisionRef{Article: "5", Khoan: "7"}, ""), "", "không thấy khoản 7 Điều 5"},
		{"phrase target missing from the metadata: left to the words check", base(LegalOpThayCumTu, LegalProvisionRef{Article: "5", Khoan: "7"}, ""), "", ""},
		{"bo_sung label already there", LegalAmendEdge{Op: LegalOpBoSung, Provision: LegalProvisionRef{Article: "5", Khoan: "2"}, NewLabel: "a", QuoteText: "a) x"}, "", "đã có trong văn bản"},
		{"bo_sung new label ok", LegalAmendEdge{Op: LegalOpBoSung, Provision: LegalProvisionRef{Article: "5", Khoan: "2"}, NewLabel: "b", QuoteText: "b) x", LeadInText: "Bổ sung điểm b vào khoản 2 Điều 5"}, "", ""},
		{"quote label mismatch", LegalAmendEdge{Op: LegalOpSuaDoi, Provision: LegalProvisionRef{Article: "5", Khoan: "2"}, QuoteText: "3. Khác"}, "", "không khớp khoản 2 Điều 5"},
		{"quote without label", LegalAmendEdge{Op: LegalOpSuaDoi, Provision: LegalProvisionRef{Article: "5", Khoan: "2"}, QuoteText: "Nội dung"}, "", "không mở đầu bằng nhãn"},
		{"Điều quote", LegalAmendEdge{Op: LegalOpSuaDoi, Provision: LegalProvisionRef{Article: "5"}, QuoteText: "Điều 5. Hồ sơ"}, "", ""},
		{"rank: a Thông tư cannot amend a Nghị định", LegalAmendEdge{Op: LegalOpSuaDoi, Amender: LegalAmendDoc{Type: "Thông tư"}, Target: LegalAmendDoc{Type: "Nghị định"},
			Provision: LegalProvisionRef{Article: "5", Khoan: "2"}, QuoteText: "2. x"}, "không thể sửa đổi Nghị định", ""},
		{"rank: a Nghị định cannot amend a Luật", LegalAmendEdge{Op: LegalOpThayCumTu, Amender: LegalAmendDoc{Type: "Nghị định"}, Target: LegalAmendDoc{Type: "Luật"},
			Provision: LegalProvisionRef{Article: "5"}}, "không thể sửa đổi Luật", ""},
		{"rank: a Luật may amend a Nghị định", LegalAmendEdge{Op: LegalOpThayCumTu, Amender: LegalAmendDoc{Type: "Luật"}, Target: LegalAmendDoc{Type: "Nghị định"},
			Provision: LegalProvisionRef{Article: "5"}}, "", ""},
		{"rank: Nghị quyết not ranked", LegalAmendEdge{Op: LegalOpThayCumTu, Amender: LegalAmendDoc{Type: "Nghị quyết"}, Target: LegalAmendDoc{Type: "Luật"},
			Provision: LegalProvisionRef{Article: "5"}}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.e
			ValidateLegalAmendEdge(&e, structure)
			if tc.rejected == "" {
				require.Empty(t, e.Rejected)
			} else {
				require.Contains(t, e.Rejected, tc.rejected)
				require.Equal(t, LegalTierUnverified, e.Tier)
			}
			if tc.unresolv == "" {
				require.Empty(t, e.Unresolved)
			} else {
				require.Contains(t, e.Unresolved, tc.unresolv)
				require.Equal(t, LegalTierUnverified, e.Tier, "a failing edge drops to tier 2")
			}
		})
	}
}

func TestTraceLegalAmendChain(t *testing.T) {
	a := LegalAmendDoc{CatalogID: "a", Number: "1/2020/NĐ-CP", Type: "Nghị định"}
	b := LegalAmendDoc{CatalogID: "b", Number: "2/2022/NĐ-CP", Type: "Nghị định"}
	c := LegalAmendDoc{CatalogID: "c", Number: "3/2024/NĐ-CP", Type: "Nghị định"}
	bEdges := []LegalAmendEdge{
		{Op: LegalOpSuaDoi, Amender: b, Target: a, Provision: LegalProvisionRef{Article: "8", Khoan: "5"}, Origin: LegalProvisionRef{Article: "1", Khoan: "2"},
			QuoteText: "5. Doanh thu trên 500 triệu đồng.", EffFrom: amendDay("2022-07-01")},
		{Op: LegalOpThayCumTu, Amender: b, Target: a, Provision: LegalProvisionRef{Article: "3"}, Origin: LegalProvisionRef{Article: "1", Khoan: "1"},
			From: "100 triệu đồng", To: "200 triệu đồng", EffFrom: amendDay("2022-07-01")},
	}
	cEdges := []LegalAmendEdge{
		// C changes words inside B's khoản 2 Điều 1 (the quoted text B wrote into A)
		{Op: LegalOpThayCumTu, Amender: c, Target: b, Provision: LegalProvisionRef{Article: "1", Khoan: "2"}, From: "500 triệu đồng", To: "01 tỷ đồng", EffFrom: amendDay("2024-01-01"), Sources: []string{LegalAmendSourceParser}},
		// C changes B's phrase change itself
		{Op: LegalOpThayCumTu, Amender: c, Target: b, Provision: LegalProvisionRef{Article: "1", Khoan: "1"}, From: "200 triệu đồng", To: "300 triệu đồng", EffFrom: amendDay("2024-01-01")},
		// C repeals a provision of B: not traced mechanically
		{Op: LegalOpBaiBo, Amender: c, Target: b, Provision: LegalProvisionRef{Article: "1", Khoan: "2"}, EffFrom: amendDay("2024-01-01")},
	}
	got := TraceLegalAmendChain(cEdges, bEdges)
	require.Len(t, got, 3)
	require.Equal(t, LegalOpThayCumTu, got[0].Op)
	require.Equal(t, LegalProvisionRef{Article: "8", Khoan: "5"}, got[0].Provision)
	require.Equal(t, a, got[0].Target)
	require.Equal(t, c, got[0].Amender)
	require.Equal(t, LegalTierUnverified, got[0].Tier)
	require.Contains(t, got[0].Sources, LegalAmendSourceChain)
	require.Equal(t, "200 triệu đồng", got[1].From)
	require.Equal(t, "300 triệu đồng", got[1].To)
	require.Equal(t, LegalProvisionRef{Article: "3"}, got[1].Provision)
	require.Equal(t, LegalOpKhac, got[2].Op)
	require.NotEmpty(t, got[2].Unresolved)

	// resolved on A: khoản 5 Điều 8 after B, then C's change (tier 2)
	ch := LegalProvisionChunk{ID: "a8", Content: "#### Điều 8. X\n\n5. Doanh thu trên 100 triệu đồng.", Doc: a,
		Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{"8"}, KhoanNos: []string{"5"}}}
	edges := append([]LegalAmendEdge{bEdges[0]}, got[0])
	edges[0].Tier = LegalTierVerified
	r := ResolveLegalProvisions(ch, edges, amendAt("2026-01-01"), LegalResolveOptions{})
	u := unitByProv(t, r, LegalProvisionRef{Article: "8", Khoan: "5"})
	require.Equal(t, LegalStateUnverified, u.State, u.Reasons)
	require.Contains(t, u.Text, "[Lời văn hiện hành theo Nghị định 2/2022/NĐ-CP, từ 01/07/2022]\n\n5. Doanh thu trên 500 triệu đồng.")
	require.Contains(t, u.Text, "cụm từ “500 triệu đồng” được thay bằng “01 tỷ đồng” tại khoản 5 Điều 8 theo Nghị định 3/2024/NĐ-CP")
}

// legalAmendTestDB is NĐ 68/2026 ← NĐ 141/2026 as on dev, in SQLite.
func legalAmendTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{},
		&types.LegalDocument{}, &types.LegalDocumentRelation{}))
	ctx := context.Background()
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	catalog := repository.NewLegalCatalogRepository(db)
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "nd68", Source: "vbpl", DocumentNumber: "68/2026/NĐ-CP", NumberKey: vietnamese_legal.CatalogNumberKey("68/2026/NĐ-CP"), DocType: "Nghị định",
			Title: "Nghị định 68/2026/NĐ-CP", IssuedOn: day("2026-03-05"), EffectiveOn: day("2026-03-05")},
		{ID: "nd141", Source: "vbpl", DocumentNumber: "141/2026/NĐ-CP", NumberKey: vietnamese_legal.CatalogNumberKey("141/2026/NĐ-CP"), DocType: "Nghị định",
			Title: "Nghị định 141/2026/NĐ-CP", IssuedOn: day("2026-04-29"), EffectiveOn: day("2026-01-01")},
		// issued, no effective date: an unknown date
		{ID: "nd200", Source: "vbpl", DocumentNumber: "200/2026/NĐ-CP", NumberKey: vietnamese_legal.CatalogNumberKey("200/2026/NĐ-CP"), DocType: "Nghị định",
			Title: "Nghị định 200/2026/NĐ-CP", IssuedOn: day("2026-08-01")},
	}))
	require.NoError(t, catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "nd141", ToID: "nd68", Relation: types.LegalRelationAmends},
		{FromID: "nd200", ToID: "nd68", Relation: types.LegalRelationAmends},
	}))
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-pub", Name: "VBPL", Visibility: types.KBVisibilityPublic}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-ten", Name: "Đơn vị", TenantID: 7, OwnerTenantID: 7, Visibility: types.KBVisibilityTenant}).Error)
	knowledge := func(id, kb, number, ext, eff, issue string) {
		meta, _ := json.Marshal(map[string]string{"doc_number": number, "external_id": ext, "doc_type": "Nghị định", "eff_from": eff, "issue_date": issue})
		require.NoError(t, db.Create(&types.Knowledge{ID: id, KnowledgeBaseID: kb, Title: number, ParseStatus: types.ParseStatusCompleted, Metadata: meta}).Error)
	}
	chunk := func(id, kid, kb string, idx int, content string, legal vietnamese_legal.LegalMetadata) {
		meta, _ := json.Marshal(types.DocumentChunkMetadata{Legal: &legal})
		require.NoError(t, db.Create(&types.Chunk{ID: id, KnowledgeID: kid, KnowledgeBaseID: kb, ChunkIndex: idx,
			ChunkType: types.ChunkTypeText, IsEnabled: true, Content: content, Metadata: meta}).Error)
	}
	knowledge("k68", "kb-pub", "68/2026/NĐ-CP", "nd68", "2026-03-05", "2026-03-05")
	knowledge("k141", "kb-pub", "141/2026/NĐ-CP", "nd141", "2026-01-01", "2026-04-29")
	knowledge("k200", "kb-ten", "200/2026/NĐ-CP", "nd200", "", "2026-08-01")
	chunk("a3", "k68", "kb-pub", 3, nd68Dieu3, vietnamese_legal.LegalMetadata{Source: "vbpl", ArticleNos: []string{"3"}, KhoanNos: []string{"1", "2"},
		SubdivisionRefs: []string{"khoan:1", "khoan:2"}, SourceDocID: "nd68"})
	chunk("a8", "k68", "kb-pub", 8, "#### Điều 8. Hóa đơn\n\n1. Doanh thu trên 500 triệu đồng phải khai.\n\n2. Hồ sơ:\n\na) tờ khai;\n\nb) giấy phép.\n\n5. Sử dụng hóa đơn điện tử\n\na) Doanh thu từ 01 tỷ đồng trở lên dùng hóa đơn có mã.",
		vietnamese_legal.LegalMetadata{Source: "vbpl", ArticleNos: []string{"8"}, KhoanNos: []string{"1", "2", "5"},
			SubdivisionRefs: []string{"khoan:1", "khoan:2", "khoan:2/diem:a", "khoan:2/diem:b", "khoan:5", "khoan:5/diem:a"}, SourceDocID: "nd68"})
	chunk("b1", "k141", "kb-pub", 1, nd141Dieu1, vietnamese_legal.LegalMetadata{Source: "vbpl", ArticleNos: []string{"1"}, SourceDocID: "nd141", Quoted: true,
		Amends: []vietnamese_legal.AmendRef{
			{Document: "Nghị định số 68/2026/NĐ-CP", Article: "3", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
			{Document: "Nghị định số 68/2026/NĐ-CP", Article: "4", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
			{Document: "Nghị định số 68/2026/NĐ-CP", Article: "8", Khoan: "1", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
			{Document: "Nghị định số 68/2026/NĐ-CP", Article: "8", Khoan: "5"},
			{Document: "Nghị định số 68/2026/NĐ-CP", Article: "8", Khoan: "2", Diem: "c"},
		}})
	// NĐ 200 in a tenant KB, issue date only: its rewrite of khoản 1 Điều 3
	chunk("b2", "k200", "kb-ten", 1, "#### Điều 1. Sửa đổi Nghị định số 68/2026/NĐ-CP\n\n1. Sửa đổi khoản 1 Điều 3 Nghị định số 68/2026/NĐ-CP như sau:\n\n> “1. Bí mật nội bộ.”.",
		vietnamese_legal.LegalMetadata{Source: "vbpl", ArticleNos: []string{"1"}, SourceDocID: "nd200", Quoted: true,
			Amends: []vietnamese_legal.AmendRef{{Document: "Nghị định số 68/2026/NĐ-CP", Article: "3", Khoan: "1"}}})
	return db
}

func TestLoadLegalAmendments(t *testing.T) {
	db := legalAmendTestDB(t)
	ctx := context.Background()
	load, err := LoadLegalAmendments(ctx, db, []string{"kb-pub"}, []string{"a3", "a8"}, []string{"kb-pub"}, time.UTC)
	require.NoError(t, err)
	require.Len(t, load.Chunks, 2)
	require.Equal(t, "nd68", load.Chunks[0].Doc.CatalogID)
	require.Empty(t, load.Chunks[0].IdentityProblem)
	edges := load.Edges["nd68"]
	byKind := map[string]LegalAmendEdge{}
	for _, e := range edges {
		byKind[string(e.Op)+" "+e.Provision.String()+" "+e.Amender.Number] = e
	}
	require.Contains(t, byKind, "thay_cum_tu Điều 3 141/2026/NĐ-CP")
	ph := byKind["thay_cum_tu Điều 3 141/2026/NĐ-CP"]
	require.Equal(t, LegalTierLegacyParserPhrase, ph.Tier)
	require.Equal(t, amendDay("2026-01-01"), ph.EffFrom, "effective date from the catalogue")
	require.Equal(t, []string{LegalAmendSourceParser}, ph.Sources)
	require.Contains(t, ph.LeadInText, "Sửa đổi cụm từ “500 triệu đồng”")

	sd := byKind["sua_doi khoản 5 Điều 8 141/2026/NĐ-CP"]
	require.Equal(t, LegalTierUnverified, sd.Tier)
	require.True(t, strings.HasPrefix(sd.QuoteText, "5. Sử dụng hóa đơn điện tử"))
	require.NotNil(t, sd.Quote)
	require.Equal(t, string([]rune(sd.QuoteText)[:10]), string([]rune(nd141Dieu1)[sd.Quote.Start:sd.Quote.Start+10]))
	require.Equal(t, LegalProvisionRef{Article: "1", Khoan: "2"}, sd.Origin, "where in B the operation is written")

	bs := byKind["bo_sung khoản 2 Điều 8 141/2026/NĐ-CP"]
	require.Equal(t, "c", bs.NewLabel)
	require.Equal(t, "c) Hộ kinh doanh mới thành lập.", bs.QuoteText)

	// NĐ 200: tenant KB (text withheld) and only an issue date
	n200 := byKind["sua_doi khoản 1 Điều 3 200/2026/NĐ-CP"]
	require.True(t, n200.TextWithheld)
	require.Empty(t, n200.QuoteText, "§10.2: B's words never leave the loader")
	require.False(t, n200.EffFrom.Known, "an issue date is not an effective date")

	require.Len(t, load.Catalogue["nd68"], 2)

	allowed := map[string]bool{"kb-pub": true}
	after := load.Resolve(amendAt("2026-10-20"), allowed)
	a3 := after["a3"]
	require.Contains(t, a3.ModelContent, "từ 01 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] trở xuống")
	k1 := unitByProv(t, a3, LegalProvisionRef{Article: "3", Khoan: "1"})
	require.Equal(t, LegalStateUnresolved, k1.State, "NĐ 200 rewrites khoản 1 with an unknown date: %v", k1.Reasons)
	require.NotContains(t, a3.ModelContent, "Bí mật")
	// NĐ 200 (catalogue amender, date unknown) may touch khoản 2 too
	k2 := unitByProv(t, a3, LegalProvisionRef{Article: "3", Khoan: "2"})
	require.Equal(t, LegalStateUnresolved, k2.State, k2.Reasons)
	require.Equal(t, []string{"danh mục ghi văn bản này bị sửa đổi bởi Nghị định 200/2026/NĐ-CP, chưa rõ ngày hiệu lực"}, k2.Reasons)
	require.Contains(t, k2.Text, "trên 01 tỷ đồng [thay cho “500 triệu đồng”", "the verified change is still applied")
	require.True(t, strings.HasSuffix(a3.ModelContent, "\n\n[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: danh mục ghi văn bản này bị sửa đổi bởi Nghị định 200/2026/NĐ-CP, chưa rõ ngày hiệu lực]"),
		"the chunk note comes after the text")

	a8 := after["a8"]
	require.Contains(t, a8.ModelContent, "Doanh thu trên 01 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] phải khai.")
	require.Contains(t, a8.ModelContent, "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: khoản 5 Điều 8 có thể đã được sửa đổi theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026 (nguồn: bộ đọc vbpl). Lời văn mới theo nguồn này: “5. Sử dụng hóa đơn điện tử")
	require.Contains(t, a8.ModelContent, "điểm c khoản 2 Điều 8 có thể đã được bổ sung")
	k5 := unitByProv(t, a8, LegalProvisionRef{Article: "8", Khoan: "5"})
	require.Equal(t, LegalStateUnresolved, k5.State, "UNVERIFIED by NĐ 141, UNRESOLVED by NĐ 200: the worse wins")
	require.Equal(t, LegalStateCurrentVerified, k5.Underlying)
	require.Contains(t, strings.Join(k5.Reasons, "|"), "khoản 5 Điều 8 có thể đã được sửa đổi")
	require.Equal(t, LegalStateUnresolved, unitByProv(t, a8, LegalProvisionRef{Article: "8", Khoan: "1"}).State,
		"NĐ 200 (catalogue amender, unknown date) leaves khoản 1 unchecked")

	before := load.Resolve(amendAt("2025-12-31"), allowed)
	require.Contains(t, before["a3"].ModelContent, "từ 500 triệu đồng [sẽ được thay bằng “01 tỷ đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] trở xuống")
	require.NotContains(t, before["a3"].ModelContent, "[thay cho")
	require.Contains(t, before["a3"].ModelContent, "chưa rõ ngày hiệu lực", "an unknown date is unresolved at any date")

	// 10.2 at the resolver too: the public KB not allowed → text missing
	strict := load.Resolve(amendAt("2026-10-20"), map[string]bool{})
	sk2 := unitByProv(t, strict["a3"], LegalProvisionRef{Article: "3", Khoan: "2"})
	require.Contains(t, strings.Join(sk2.Reasons, "|"), "lời văn của văn bản sửa đổi không thuộc phạm vi được dùng")
	require.NotContains(t, strict["a3"].ModelContent, "01 tỷ")

	// a chunk outside the search scope is not read
	none, err := LoadLegalAmendments(ctx, db, []string{"kb-ten"}, []string{"a3"}, []string{"kb-ten"}, time.UTC)
	require.NoError(t, err)
	require.Empty(t, none.Chunks)
}

func TestLoadLegalAmendmentsIdentity(t *testing.T) {
	db := legalAmendTestDB(t)
	// the knowledge claims another number than its catalogue row
	require.NoError(t, db.Exec(`UPDATE knowledges SET metadata = ? WHERE id = 'k68'`,
		`{"doc_number":"68/2025/NĐ-CP","external_id":"nd68","doc_type":"Nghị định"}`).Error)
	load, err := LoadLegalAmendments(context.Background(), db, []string{"kb-pub"}, []string{"a3"}, []string{"kb-pub"}, time.UTC)
	require.NoError(t, err)
	require.Len(t, load.Chunks, 1)
	require.Contains(t, load.Chunks[0].IdentityProblem, "không khớp danh mục")
	r := load.Resolve(amendAt("2026-10-20"), nil)["a3"]
	require.Equal(t, LegalStateUnresolved, r.State)
	require.Contains(t, r.ModelContent, "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: chưa xác định được văn bản này trong danh mục")
}

// Review S3, S5, S6 on the loader: a document-level entry is a catalogue
// fact, an amender expiring before it took effect is UNRESOLVED, and a
// shared số hiệu still names this document when one of its rows is it.
func TestLoadLegalAmendmentsCatalogueFacts(t *testing.T) {
	db := legalAmendTestDB(t)
	ctx := context.Background()
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	catalog := repository.NewLegalCatalogRepository(db)
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		// NĐ 300: amends NĐ 68 "as a whole" only (no provision), expired the day it took effect
		{ID: "nd300", Source: "vbpl", DocumentNumber: "300/2026/NĐ-CP", NumberKey: vietnamese_legal.CatalogNumberKey("300/2026/NĐ-CP"), DocType: "Nghị định",
			Title: "Nghị định 300/2026/NĐ-CP", IssuedOn: day("2026-05-01"), EffectiveOn: day("2026-06-01"), ExpiredOn: day("2026-06-01")},
		// a second catalogue row sharing NĐ 68's number
		{ID: "nd68-dup", Source: "vbpl", DocumentNumber: "68/2026/NĐ-CP", NumberKey: vietnamese_legal.CatalogNumberKey("68/2026/NĐ-CP"), DocType: "Nghị định",
			Title: "Nghị định 68/2026/NĐ-CP (bản khác)", IssuedOn: day("2026-03-05")},
	}))
	meta, _ := json.Marshal(map[string]string{"doc_number": "300/2026/NĐ-CP", "external_id": "nd300", "doc_type": "Nghị định"})
	require.NoError(t, db.Create(&types.Knowledge{ID: "k300", KnowledgeBaseID: "kb-pub", Title: "300", ParseStatus: types.ParseStatusCompleted, Metadata: meta}).Error)
	legal := vietnamese_legal.LegalMetadata{Source: "vbpl", ArticleNos: []string{"1"}, SourceDocID: "nd300",
		Amends: []vietnamese_legal.AmendRef{
			{Document: "Nghị định số 68/2026/NĐ-CP"},
			{Document: "Nghị định số 68/2026/NĐ-CP"},
			{Document: "Nghị định số 68/2026/NĐ-CP", Article: "3", Phrase: true, From: "trở xuống", To: "trở xuống (mới)"},
		}}
	cm, _ := json.Marshal(types.DocumentChunkMetadata{Legal: &legal})
	require.NoError(t, db.Create(&types.Chunk{ID: "b3", KnowledgeID: "k300", KnowledgeBaseID: "kb-pub", ChunkIndex: 1,
		ChunkType: types.ChunkTypeText, IsEnabled: true, Content: "#### Điều 1. Sửa đổi Nghị định số 68/2026/NĐ-CP\n\n1. Sửa đổi cụm từ “trở xuống” thành “trở xuống (mới)” tại Điều 3.", Metadata: cm}).Error)

	load, err := LoadLegalAmendments(ctx, db, []string{"kb-pub"}, []string{"a3"}, []string{"kb-pub"}, time.UTC)
	require.NoError(t, err)
	var whole, phrase int
	for _, e := range load.Edges["nd68"] {
		if e.Amender.CatalogID == "nd300" {
			if e.Provision.Article == "" {
				whole++
			}
			if e.phrase() {
				phrase++
				require.Contains(t, e.Unresolved, "hết hiệu lực trước hoặc ngay khi có hiệu lực")
			}
		}
	}
	require.Zero(t, whole, "a document-level entry is no rewrite of the whole document")
	require.Equal(t, 1, phrase, "the shared số hiệu still names NĐ 68 (one of its rows)")
	var facts []LegalCatalogAmendment
	for _, f := range load.Catalogue["nd68"] {
		if f.Amender.CatalogID == "nd300" {
			facts = append(facts, f)
		}
	}
	require.Len(t, facts, 1, "one fact for two document-level entries")
	require.True(t, facts[0].NeverInForce)

	r := load.Resolve(amendAt("2026-10-20"), map[string]bool{"kb-pub": true})["a3"]
	require.NotContains(t, r.ModelContent, "xung đột")
	require.NotContains(t, r.ModelContent, "trở xuống (mới) [thay cho")
	require.Equal(t, LegalStateUnresolved, unitByProv(t, r, LegalProvisionRef{Article: "3", Khoan: "1"}).State)
}

// A phrase change's lead-in is the sentence quoting the words, not the
// first place they occur (a heading may name them: "… của Bộ Công
// Thương"); with the heading, the verb check demoted the change.
func TestParserEdgePhraseLeadIn(t *testing.T) {
	content := "#### Điều 10. Sửa đổi Thông tư số 1/2024/TT-BCT của Bộ trưởng Bộ Công Thương\n\n" +
		"1. Thay thế cụm từ “Bộ Công Thương” bằng cụm từ “Ủy ban nhân dân cấp tỉnh” tại Điều 10."
	l := &legalAmendLoader{allowed: map[string]bool{"kb": true}}
	legal := &vietnamese_legal.LegalMetadata{ArticleNos: []string{"10"}}
	a := vietnamese_legal.AmendRef{Article: "10", Phrase: true, From: "Bộ Công Thương", To: "Ủy ban nhân dân cấp tỉnh"}
	e := l.parserEdge(legalAmendRow{ID: "c", KnowledgeBaseID: "kb", Content: content}, legal, newProvisionScope(content), a,
		legalQuoteGroups(content), LegalAmendDoc{KnowledgeBaseID: "kb"}, LegalAmendDoc{}, amendDay("2025-07-01"), nil)
	require.True(t, strings.HasPrefix(e.LeadInText, "1. Thay thế cụm từ"), e.LeadInText)
	require.Equal(t, LegalProvisionRef{Article: "10", Khoan: "1"}, e.Origin)
	ValidateLegalAmendEdge(&e, nil)
	require.Empty(t, e.Unresolved)
	require.Equal(t, LegalTierLegacyParserPhrase, e.Tier)
}

// Review 2, S1: a phrase entry naming the target in any wording is read.
func TestNamesPhraseTarget(t *testing.T) {
	l := &legalAmendLoader{cites: map[string][]vietnamese_legal.Citation{}}
	keys := map[string]bool{
		vietnamese_legal.CatalogNumberKey("24/2019/TT-NHNN"):   true,
		vietnamese_legal.CatalogNumberKey("10 /2016/TT-BTNMT"): true,
		vietnamese_legal.CatalogNumberKey("34/2024/QH15"):      true,
	}
	arts := map[string]bool{"3": true}
	entry := func(doc string, phrase bool, article string) []byte {
		b, _ := json.Marshal([]vietnamese_legal.AmendRef{{Document: doc, Article: article, Phrase: phrase, From: "a", To: "b"}})
		return b
	}
	require.True(t, l.namesPhraseTarget(entry("Thông tư số 24/2019/TT-NHNN quy định về hoạt động ngoại hối", true, "3"), arts, keys))
	require.True(t, l.namesPhraseTarget(entry("Thông tư số 10/2016/TT-BTNMT", true, "3"), arts, keys), "A's number had a stray space")
	require.True(t, l.namesPhraseTarget(entry("Luật Tổ chức Tòa án nhân dân số 34/2024/QH15", true, "3"), arts, keys))
	require.False(t, l.namesPhraseTarget(entry("Thông tư số 24/2019/TT-NHNN", true, "4"), arts, keys), "another article")
	require.False(t, l.namesPhraseTarget(entry("Thông tư số 24/2019/TT-NHNN", false, "3"), arts, keys), "phrase entries only")
	require.False(t, l.namesPhraseTarget(entry("Thông tư số 25/2019/TT-NHNN", true, "3"), arts, keys))
}

// Review 2, catalogue design: B held in an allowed KB and read whole is
// TextRead, with the articles of A its text cites; a withheld B is not.
func TestLoadLegalAmendmentsAmenderReads(t *testing.T) {
	db := legalAmendTestDB(t)
	load, err := LoadLegalAmendments(context.Background(), db, []string{"kb-pub"}, []string{"a3", "a8"}, []string{"kb-pub"}, time.UTC)
	require.NoError(t, err)
	by := map[string]LegalCatalogAmendment{}
	for _, f := range load.Catalogue["nd68"] {
		by[f.Amender.CatalogID] = f
	}
	require.True(t, by["nd141"].TextRead)
	require.Equal(t, map[string]bool{"3": true, "8": true}, by["nd141"].Cited)
	require.False(t, by["nd200"].TextRead, "NĐ 200 is held only in a tenant KB")
}

// Review 2, B4: a pending change from a withheld copy names no new words.
func TestLegalPendingWithheldNote(t *testing.T) {
	db := legalAmendTestDB(t)
	require.NoError(t, db.Exec(`UPDATE chunks SET knowledge_base_id='kb-ten' WHERE id='b1'`).Error)
	require.NoError(t, db.Exec(`UPDATE knowledges SET knowledge_base_id='kb-ten' WHERE id='k141'`).Error)
	now := time.Date(2025, 12, 1, 9, 0, 0, 0, time.UTC)
	w, err := LegalCurrentWordingResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3", "a8"}, now)
	require.NoError(t, err)
	require.Contains(t, w["a3"], "[Sắp có hiệu lực: cụm từ “500 triệu đồng” được sửa đổi tại Điều 3 theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026]")
	require.NotContains(t, w["a3"], "01 tỷ")
	for id, text := range w {
		require.NotContains(t, text, "“01 tỷ đồng”", id)
		require.NotContains(t, text, "5. Sử dụng hóa đơn điện tử\n\na) Hộ kinh doanh", "B's quoted text "+id)
	}
}

// Review 2 reach: the lead-in of "bưu điện" → "dịch vụ bưu chính" is the
// sentence making that change, not an earlier one quoting “bưu điện”.
func TestLegalPhraseLeadIn(t *testing.T) {
	content := "a) Bổ sung cụm từ “hoặc qua cổng dịch vụ công trực tuyến” sau từ “bưu điện” tại điểm b khoản 1 Điều 10;\n\n" +
		"b) Thay thế cụm từ “bưu điện” bằng cụm từ “dịch vụ bưu chính” tại khoản 1 Điều 25;\n\nc) Bãi bỏ cụm từ “hoặc đấu thầu” tại Điều 7."
	s, e, ok := legalPhraseLeadIn(content, "bưu điện", "dịch vụ bưu chính")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(content[s:e], "b) Thay thế cụm từ"))
	s, e, ok = legalPhraseLeadIn(content, "hoặc đấu thầu", "")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(content[s:e], "c) Bãi bỏ"))
	_, _, ok = legalPhraseLeadIn(content, "bưu điện", "bưu chính công ích")
	require.False(t, ok, "no line makes that change: no lead-in, no verb check")
}

// Review 2 reach: two copies of B parsed by different converter versions;
// the copy naming the target wins over one that could not be placed.
func TestLoadLegalAmendmentsPlacedCopyWins(t *testing.T) {
	db := legalAmendTestDB(t)
	meta, _ := json.Marshal(map[string]string{"doc_number": "141/2026/NĐ-CP", "external_id": "nd141", "doc_type": "Nghị định"})
	require.NoError(t, db.Create(&types.Knowledge{ID: "k0141", KnowledgeBaseID: "kb-pub", Title: "141 (bản cũ)", ParseStatus: types.ParseStatusCompleted, Metadata: meta}).Error)
	legal := vietnamese_legal.LegalMetadata{Source: "vbpl", ArticleNos: []string{"1"}, SourceDocID: "nd141",
		Amends: []vietnamese_legal.AmendRef{{Article: "3", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"}}}
	cm, _ := json.Marshal(types.DocumentChunkMetadata{Legal: &legal})
	require.NoError(t, db.Create(&types.Chunk{ID: "b0", KnowledgeID: "k0141", KnowledgeBaseID: "kb-pub", ChunkIndex: 1,
		ChunkType: types.ChunkTypeText, IsEnabled: true, Content: "1. Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ đồng” tại Điều 3.", Metadata: cm}).Error)
	out, err := LegalCurrentWordingResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3"}, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Contains(t, out["a3"], "trên 01 tỷ đồng [thay cho “500 triệu đồng”")
	require.NotContains(t, out["a3"], "thao tác không ghi văn bản bị sửa đổi")
}

// Final review S1: a "Điều N" an amender's chunk cites counts for A only
// when the next document cited after it is A, or the line cites none and
// the heading names A alone; never an insertion point or B's own Điều.
func TestLegalCitedArticlesOf(t *testing.T) {
	key := func(n string) string { return vietnamese_legal.CatalogNumberKey(n) }
	cases := []struct {
		name, content, heading, a, base string
		want                            []string
	}{
		{"NĐ 220/2025 → 14/2016: bare repeal under a heading naming A only", "8. Bãi bỏ Điều 16.",
			`["Điều 1. Sửa đổi, bổ sung một số điều của Nghị định số 14/2016/NĐ-CP"]`, "14/2016/NĐ-CP", "", []string{"16"}},
		{"NĐ 168/2024 → 100/2019: khoản list of one Điều", "Bãi bỏ khoản 1, khoản 2, khoản 9 Điều 24.",
			`["Điều 1. Sửa đổi Nghị định số 100/2019/NĐ-CP"]`, "100/2019/NĐ-CP", "", []string{"24"}},
		{"NĐ 193/2026: the Điều belongs to another decree", "3. Bãi bỏ Điều 9; khoản 2 Điều 10 Nghị định số 254/2025/NĐ-CP.",
			`["Điều 2. Sửa đổi Nghị định số 88/2025/NĐ-CP và Nghị định số 254/2025/NĐ-CP"]`, "88/2025/NĐ-CP", "", nil},
		{"TT 01/2025: khoản 2 Điều 5 of another circular", "Sửa đổi khoản 2 Điều 5 Thông tư số 36/2021/TT-BYT như sau:",
			`["Điều 1. Sửa đổi Thông tư số 30/2020/TT-BYT"]`, "30/2020/TT-BYT", "", nil},
		{"next citation is A", "Bãi bỏ Điều 7 Nghị định số 14/2016/NĐ-CP.", `[]`, "14/2016/NĐ-CP", "", []string{"7"}},
		{"NĐ 129/2021: insertion point (2a is new, A has no Điều 2a)", "Bổ sung Điều 2a vào sau Điều 2 như sau:",
			`["Điều 1. Sửa đổi Nghị định số 14/2016/NĐ-CP"]`, "14/2016/NĐ-CP", "", []string{"2a"}},
		{"insertion point before", "Bổ sung Điều 7a vào trước Điều 8.",
			`["Điều 1. Sửa đổi Nghị định số 14/2016/NĐ-CP"]`, "14/2016/NĐ-CP", "", []string{"7a"}},
		{"B's own Điều", "Hiệu lực theo Điều 5 Nghị định này.", `["Điều 1. Sửa đổi Nghị định số 14/2016/NĐ-CP"]`, "14/2016/NĐ-CP", "", nil},
		{"a heading naming two documents", "Bãi bỏ Điều 16.", `["Điều 1. Sửa đổi Nghị định số 14/2016/NĐ-CP và Nghị định số 15/2016/NĐ-CP"]`, "14/2016/NĐ-CP", "", nil},
		{"a law cited by name", "Bãi bỏ Điều 74 Luật Đầu tư công.", `[]`, "39/2019/QH14", "luật đầu tư công", []string{"74"}},
		{"TT 06/2023 → 41/2021: a bracketed amendment history is no operation on A",
			"b) Điểm a khoản 2 Điều 8 (đã được sửa đổi, bổ sung tại điểm b khoản 7 Điều 1 của Thông tư số 41/2021/TT-BGTVT ngày 31 tháng 12 năm 2021);",
			`["Điều 1. Sửa đổi Thông tư số 12/2020/TT-BGTVT và Thông tư số 41/2021/TT-BGTVT"]`, "41/2021/TT-BGTVT", "", nil},
		{"TT 42/2023 → 41/2020: an unbracketed amendment history",
			"1. Sửa đổi, bổ sung Điều 5 Thông tư số 13/2019/TT-BGTVT đã được sửa đổi, bổ sung bởi khoản 5, khoản 6 Điều 1 Thông tư số 41/2020/TT-BGTVT như sau:",
			`["Điều 1. Sửa đổi Thông tư số 13/2019/TT-BGTVT"]`, "41/2020/TT-BGTVT", "", nil},
		{"TT 60/2019 → 39/2018: repeal of a khoản of an amending circular",
			"2. Bãi bỏ khoản 15 Điều 1 Thông tư số 39/2018/TT-BTC ngày 20/4/2018 của Bộ trưởng Bộ Tài chính.",
			`[]`, "39/2018/TT-BTC", "", []string{"1"}},
		{"quoted text is A's new wording, not a citation", "> “Điều 9. Mới”", `["Điều 1. Sửa đổi Nghị định số 14/2016/NĐ-CP"]`, "14/2016/NĐ-CP", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := legalCitedArticlesOf(tc.content, tc.heading, key(tc.a), tc.base)
			var arts []string
			for a := range got {
				arts = append(arts, a)
			}
			require.ElementsMatch(t, tc.want, arts)
		})
	}
}

// Final review S2: a catalogue relation of a lower-ranked amender (TT
// 12/2025/TT-BTC "amending" NĐ 40/2017/NĐ-CP) is skipped: no info line,
// no UNVERIFIED.
func TestLoadLegalAmendmentsCatalogueRank(t *testing.T) {
	db := legalAmendTestDB(t)
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "tt12", Source: "vbpl", DocumentNumber: "12/2025/TT-BTC", NumberKey: vietnamese_legal.CatalogNumberKey("12/2025/TT-BTC"), DocType: "Thông tư",
			Title: "Thông tư 12/2025/TT-BTC", IssuedOn: day("2025-03-01"), EffectiveOn: day("2025-04-01")},
	}))
	require.NoError(t, db.Create(&types.LegalDocumentRelation{FromID: "tt12", ToID: "nd68", Relation: types.LegalRelationAmends}).Error)
	load, err := LoadLegalAmendments(ctx, db, []string{"kb-pub"}, []string{"a3"}, []string{"kb-pub"}, time.UTC)
	require.NoError(t, err)
	for _, f := range load.Catalogue["nd68"] {
		require.NotEqual(t, "tt12", f.Amender.CatalogID, "a Thông tư cannot amend a Nghị định")
	}
	r := load.Resolve(amendAt("2026-10-20"), map[string]bool{"kb-pub": true})["a3"]
	require.NotContains(t, r.ModelContent, "12/2025/TT-BTC")
}
