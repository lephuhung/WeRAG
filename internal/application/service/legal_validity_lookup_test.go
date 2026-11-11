package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newLegalLookupFixture(t *testing.T) (*legalValidityLookupService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{},
		&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)

	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	snap := day("2026-11-10")
	older := day("2026-11-01")
	doc := func(id, num, typ, status, issued, effective string) *types.LegalDocument {
		d := &types.LegalDocument{ID: id, Source: types.LegalDocumentSourceVBPL, DocumentNumber: num,
			NumberKey: vietnamese_legal.CatalogNumberKey(num), DocType: typ, Title: typ + " " + num + " về đất đai",
			Issuer: "Quốc hội", Status: status, IssuedOn: day(issued), SourceUpdatedAt: snap}
		if effective != "" {
			d.EffectiveOn = day(effective)
		}
		return d
	}
	feed := doc("feed", "99/2026/NĐ-CP", "Nghị định", "Còn hiệu lực", "2026-10-01", "")
	feed.Source = "tvpl"
	feed.SourceUpdatedAt = day("2026-11-11") // a feed push is not the vbpl snapshot
	stale := doc("ubnd-old", "53/QĐ-UBND", "Quyết định", "Còn hiệu lực", "2019-03-01", "")
	stale.SourceUpdatedAt = older
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		doc("law-old", "45/2013/QH13", "Luật", "Còn hiệu lực", "2013-11-29", "2014-07-01"),
		doc("law", "31/2024/QH15", "Luật", "Còn hiệu lực", "2024-01-18", "2024-08-01"),
		doc("amend", "43/2024/QH15", "Luật", "Còn hiệu lực", "2024-06-29", "2024-08-01"),
		doc("nd53", "53/2022/NĐ-CP", "Nghị định", "Còn hiệu lực", "2022-08-15", "2022-10-01"),
		doc("tt53", "53/2022/TT-BTC", "Thông tư", "Hết hiệu lực một phần", "2022-08-20", ""),
		// số hiệu is not unique: two provinces' decisions share one
		doc("qd-a", "12/2025/QĐ-UBND", "Quyết định", "Còn hiệu lực", "2025-02-01", ""),
		doc("qd-b", "12/2025/QĐ-UBND", "Quyết định", "Hết hiệu lực toàn bộ", "2025-01-10", ""),
		stale, feed,
	}))
	require.NoError(t, catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "law", ToID: "law-old", Relation: types.LegalRelationReplaces},
		{FromID: "amend", ToID: "law", Relation: types.LegalRelationAmends},
		{FromID: "nd53", ToID: "law", Relation: types.LegalRelationGuides}, // not a validity edge
	}))
	// The new law is in a public KB, the old one only in a private one.
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-pub", Name: "Luật", Visibility: types.KBVisibilityPublic}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-priv", Name: "Riêng", OwnerTenantID: 7,
		Visibility: types.KBVisibilityTenant}).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "k-law", KnowledgeBaseID: "kb-pub", Title: "luat-dat-dai.html"}).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "k-old", KnowledgeBaseID: "kb-priv", Title: "cu.docx"}).Error)
	require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: "k-law", KnowledgeBaseID: "kb-pub",
		LegalDocumentID: "law", ReviewStatus: types.LegalReviewApproved}).Error)
	require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: "k-old", KnowledgeBaseID: "kb-priv",
		LegalDocumentID: "law-old", ReviewStatus: types.LegalReviewApproved}).Error)

	svc := NewLegalValidityLookupService(catalog, repository.NewLegalReviewRepository(db),
		repository.NewKnowledgeBaseRepository(db)).(*legalValidityLookupService)
	svc.now = func() time.Time { return *day("2026-11-11") }
	return svc, db
}

func TestLegalValidityLookupByNumber(t *testing.T) {
	svc, _ := newLegalLookupFixture(t)
	ctx := context.Background()

	got, err := svc.LookupValidity(ctx, "Luật số 45/2013/QH13,", 0)
	require.NoError(t, err)
	require.Equal(t, "number", got.By)
	require.Equal(t, "2026-11-10", got.CatalogSnapshotDate, "vbpl snapshot only, not the feed row")
	require.Len(t, got.Matches, 1)
	m := got.Matches[0]
	require.Equal(t, "45/2013/QH13", m.DocumentNumber)
	require.Equal(t, "Còn hiệu lực", m.StatusText)
	// the portal still says in force, but the replacing law is in force
	require.Equal(t, types.LegalStatusKindExpired, m.Status)
	require.NotNil(t, m.Derived)
	require.Equal(t, types.LegalStatusExpired, m.Derived.StatusText)
	require.Equal(t, "2024-08-01", m.Derived.ExpiredOn)
	require.Equal(t, "law", m.Derived.By.ID)
	require.Len(t, m.ReplacedBy, 1)
	require.Equal(t, 1, m.ReplacedByTotal)
	require.Equal(t, "31/2024/QH15", m.ReplacedBy[0].DocumentNumber)
	require.Equal(t, types.LegalRelationReplaces, m.ReplacedBy[0].Relation)
	require.Equal(t, "2013-11-29", m.IssuedOn)
	require.Equal(t, "2014-07-01", m.EffectiveOn)
	require.Empty(t, m.Knowledge, "a private KB's copy is not linked")

	got, err = svc.LookupValidity(ctx, "31/2024/qh15", 0)
	require.NoError(t, err)
	require.Len(t, got.Matches, 1)
	m = got.Matches[0]
	require.Equal(t, types.LegalStatusKindInForce, m.Status)
	require.Nil(t, m.Derived)
	require.Len(t, m.AmendedBy, 1)
	require.Equal(t, "43/2024/QH15", m.AmendedBy[0].DocumentNumber)
	require.Empty(t, m.ReplacedBy)
	require.Empty(t, m.EndedBy)
	require.Equal(t, []types.LegalKnowledgeRef{{KnowledgeID: "k-law", KnowledgeBaseID: "kb-pub",
		KnowledgeBaseName: "Luật", Title: "luat-dat-dai.html"}}, m.Knowledge)

	// a shared số hiệu returns every row
	got, err = svc.LookupValidity(ctx, "12/2025/QĐ-UBND", 0)
	require.NoError(t, err)
	require.Len(t, got.Matches, 2)
	require.Equal(t, "qd-a", got.Matches[0].ID, "newest first")
	require.Equal(t, types.LegalStatusKindExpired, got.Matches[1].Status)
}

func TestLegalValidityLookupBareNumberAndTitle(t *testing.T) {
	svc, _ := newLegalLookupFixture(t)
	ctx := context.Background()

	// a bare number: the exact local number and every 53/…
	got, err := svc.LookupValidity(ctx, "53", 0)
	require.NoError(t, err)
	ids := []string{}
	for _, m := range got.Matches {
		ids = append(ids, m.ID)
	}
	require.Equal(t, []string{"tt53", "nd53", "ubnd-old"}, ids)
	require.Equal(t, types.LegalStatusKindPartlyExpired, got.Matches[0].Status)

	// the words around it narrow by type
	got, err = svc.LookupValidity(ctx, "Nghị định 53", 0)
	require.NoError(t, err)
	require.Len(t, got.Matches, 1)
	require.Equal(t, "nd53", got.Matches[0].ID)

	// title words
	got, err = svc.LookupValidity(ctx, "luật đất đai", 0)
	require.NoError(t, err)
	require.Equal(t, "title", got.By)
	require.Len(t, got.Matches, 3)

	// an unknown number falls back to the title, then to nothing
	got, err = svc.LookupValidity(ctx, "777/2099/NĐ-CP", 0)
	require.NoError(t, err)
	require.Equal(t, "", got.By)
	require.Empty(t, got.Matches)
	require.NotNil(t, got.Matches)

	// the feed row is flagged
	got, err = svc.LookupValidity(ctx, "99/2026/NĐ-CP", 0)
	require.NoError(t, err)
	require.Len(t, got.Matches, 1)
	require.True(t, got.Matches[0].Unverified)
	require.NotEmpty(t, got.Matches[0].UnverifiedNote)
	require.Equal(t, "thuvienphapluat.vn", got.Matches[0].SourceLabel)

	// limit
	got, err = svc.LookupValidity(ctx, "53", 2)
	require.NoError(t, err)
	require.Len(t, got.Matches, 2)

	empty, err := svc.LookupValidity(ctx, "   ", 0)
	require.NoError(t, err)
	require.Empty(t, empty.Matches)
}

func TestLegalLookupNumberToken(t *testing.T) {
	cases := []struct {
		in   string
		num  string
		rest []string
	}{
		{"30/2020/NĐ-CP", "30/2020/NĐ-CP", nil},
		{"Nghị định số 30/2020/NĐ-CP", "30/2020/NĐ-CP", []string{"Nghị", "định"}},
		{"(30/2020/NĐ-CP),", "30/2020/NĐ-CP", nil},
		{"Nghị định 53", "53", []string{"Nghị", "định"}},
		{"Luật Đất đai", "", nil},
	}
	for _, c := range cases {
		num, rest := legalLookupNumberToken(c.in)
		require.Equal(t, c.num, num, c.in)
		require.Equal(t, c.rest, rest, c.in)
	}
}
