package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLegalReviewFlow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{},
		&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))

	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "admin-1")
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalReviewService(catalog, repository.NewLegalReviewRepository(db), nil, nil)

	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "bmnn-2018", Source: "vbpl", DocumentNumber: "29/2018/QH14", NumberKey: "29/2018/QH14",
			Title: "Luật Bảo vệ bí mật nhà nước số 29/2018/QH14", IssuedOn: day("2018-11-15"), Status: "Còn hiệu lực"},
		{ID: "bmnn-2025", Source: "vbpl", DocumentNumber: "117/2025/QH15", NumberKey: "117/2025/QH15",
			Title: "Luật Bảo vệ bí mật nhà nước số 117/2025/QH15", IssuedOn: day("2025-12-10"), Status: "Còn hiệu lực"},
	}))
	require.NoError(t, catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "bmnn-2025", ToID: "bmnn-2018", Relation: types.LegalRelationReplaces},
	}))

	// A platform public KB holding only the 2018 law, plus a tenant KB the
	// review must ignore.
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-pub", Name: "Luật", Visibility: types.KBVisibilityPublic}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-ten", Name: "Đơn vị", TenantID: 7, OwnerTenantID: 7, Visibility: types.KBVisibilityTenant}).Error)
	for _, k := range []types.Knowledge{
		{ID: "k-2018", KnowledgeBaseID: "kb-pub", Title: "29.2018QH14.pdf", ParseStatus: types.ParseStatusCompleted},
		{ID: "k-ten", KnowledgeBaseID: "kb-ten", TenantID: 7, Title: "x.pdf", ParseStatus: types.ParseStatusCompleted},
	} {
		require.NoError(t, db.Create(&k).Error)
	}
	require.NoError(t, db.Create(&types.Chunk{ID: "c1", KnowledgeID: "k-2018", KnowledgeBaseID: "kb-pub", ChunkIndex: 0,
		ChunkType: types.ChunkTypeText, IsEnabled: true,
		Content: "QUỐC HỘI CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM Luật số: 29/2018/QH14\n\nLUẬT\nBẢO VỆ BÍ MẬT NHÀ NƯỚC"}).Error)

	created, err := svc.Scan(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, created, "only the public KB document is scanned")
	created, err = svc.Scan(ctx)
	require.NoError(t, err)
	require.Zero(t, created, "a scan never duplicates a suggestion")

	items, total, err := svc.List(ctx, "pending", "", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "29/2018/QH14", items[0].Suggestion.DocumentNumber)
	require.NotNil(t, items[0].Document)
	require.Equal(t, "bmnn-2018", items[0].Document.ID, "a single catalogue match is pre-linked")

	// The reviewer fixes the stale portal status while approving.
	item, err := svc.Decide(ctx, "k-2018", &types.LegalReviewDecision{
		Action: "approve", LegalDocumentID: "bmnn-2018",
		Document: &types.LegalDocumentFields{DocumentNumber: "29/2018/QH14", Title: "Luật Bảo vệ bí mật nhà nước",
			DocType: "Luật", IssuedOn: "2018-11-15", EffectiveOn: "2020-01-01", ExpiredOn: "2026-03-01",
			Status: "Hết hiệu lực toàn bộ"},
	})
	require.NoError(t, err)
	require.Equal(t, types.LegalReviewApproved, item.Link.ReviewStatus)
	require.Equal(t, "admin-1", item.Document.ReviewedBy)
	require.Equal(t, "Hết hiệu lực toàn bộ", item.Document.Status)
	require.Len(t, item.SupersededBy, 1)
	require.Equal(t, "bmnn-2025", item.SupersededBy[0].Document.ID)
	require.False(t, item.SupersededBy[0].InKB, "the 2025 law is not in any public KB")

	alerts, total, err := svc.List(ctx, LegalReviewStatusAlerts, "", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "29.2018QH14.pdf", alerts[0].KnowledgeTitle)

	// Search results from the reviewed law carry its validity and rank lower.
	results := []*types.SearchResult{
		{KnowledgeID: "k-2018", KnowledgeCustomMetadata: "nguồn: Công báo"},
		{KnowledgeID: "k-ten"},
	}
	svc.AnnotateSearchResults(ctx, results)
	require.True(t, results[0].LegalSuperseded)
	require.Contains(t, results[0].KnowledgeCustomMetadata, "Luật Bảo vệ bí mật nhà nước")
	require.Contains(t, results[0].KnowledgeCustomMetadata, "Bị thay thế bởi Luật Bảo vệ bí mật nhà nước số 117/2025/QH15")
	require.Contains(t, results[0].KnowledgeCustomMetadata, "CHƯA có trong kho tài liệu")
	require.Contains(t, results[0].KnowledgeCustomMetadata, "\nnguồn: Công báo", "user metadata is kept after the legal note")
	require.False(t, results[1].LegalSuperseded)
	require.Empty(t, results[1].KnowledgeCustomMetadata, "unreviewed documents are untouched")

	_, err = svc.Decide(ctx, "k-2018", &types.LegalReviewDecision{Action: "approve"})
	require.Error(t, err, "approving without a catalogue row or fields fails")
}

func TestLegalReviewApprovesVBPLImports(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.Chunk{},
		&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalReviewService(catalog, repository.NewLegalReviewRepository(db), nil, nil)

	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "179941", Source: types.LegalDocumentSourceVBPL, DocumentNumber: "67/2025/QH15", NumberKey: "67/2025/QH15",
			Title: "Luật Thuế thu nhập doanh nghiệp", Status: "Còn hiệu lực"},
		{ID: "manual-1", Source: types.LegalDocumentSourceManual, DocumentNumber: "57-NQ/TW", NumberKey: "57-NQ/TW"},
	}))
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-pub", Name: "Luật", Visibility: types.KBVisibilityPublic}).Error)
	for _, k := range []types.Knowledge{
		// Imported by the vbpl crawler: no readable header needed.
		{ID: "k-vbpl", KnowledgeBaseID: "kb-pub", Title: "a.html", ParseStatus: types.ParseStatusCompleted,
			Metadata: types.JSON(`{"external_id":"179941"}`)},
		// An external id that is not a vbpl row stays for the reviewer.
		{ID: "k-manual", KnowledgeBaseID: "kb-pub", Title: "b.pdf", ParseStatus: types.ParseStatusCompleted,
			Metadata: types.JSON(`{"external_id":"manual-1"}`)},
		{ID: "k-unknown", KnowledgeBaseID: "kb-pub", Title: "c.pdf", ParseStatus: types.ParseStatusCompleted,
			Metadata: types.JSON(`{"external_id":"no-such-row"}`)},
	} {
		require.NoError(t, db.Create(&k).Error)
	}

	created, err := svc.Scan(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, created)

	approved, total, err := svc.List(ctx, string(types.LegalReviewApproved), "", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "k-vbpl", approved[0].Link.KnowledgeID)
	require.Equal(t, "179941", approved[0].Document.ID)
	require.Equal(t, types.LegalReviewerVBPL, approved[0].Link.ReviewedBy)
	require.Nil(t, approved[0].Document.ReviewedAt, "the catalogue row stays open to portal updates")

	_, total, err = svc.List(ctx, string(types.LegalReviewPending), "", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
}

func TestSuggestedIssuer(t *testing.T) {
	require.Equal(t, "", suggestedIssuer("## LUẬT"))
	require.Equal(t, "", suggestedIssuer("## CÔNG BÁO/Số 1133 + 1134/Ngày 22-12-2018"))
	require.Equal(t, "", suggestedIssuer("**QUỐC HỘI CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM**"))
	require.Equal(t, "BỘ CÔNG AN", suggestedIssuer("**BỘ CÔNG AN**"))
}

func TestPickCatalogMatch(t *testing.T) {
	luat := &types.LegalDocument{ID: "25701", DocType: "Luật"}
	nq := &types.LegalDocument{ID: "25684", DocType: "Nghị quyết"}
	dupA := &types.LegalDocument{ID: "142881", DocType: "Luật"}
	dupB := &types.LegalDocument{ID: "142847", DocType: "Luật"}
	require.Equal(t, "25701", pickCatalogMatch([]*types.LegalDocument{nq, luat}, "", "Luật"), "type disambiguates")
	require.Equal(t, "142847", pickCatalogMatch([]*types.LegalDocument{dupA, dupB}, "142847", "Luật"), "external id wins")
	require.Equal(t, "", pickCatalogMatch([]*types.LegalDocument{dupA, dupB}, "", "Luật"), "true duplicates stay open")
	require.Equal(t, "25684", pickCatalogMatch([]*types.LegalDocument{nq}, "", "Luật"), "a single match is kept")
}
