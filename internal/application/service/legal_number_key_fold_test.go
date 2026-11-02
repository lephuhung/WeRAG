package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestCatalogNumberKeyFoldCallers: once CatalogNumberKey folds the
// look-alike "Ð" and migration 000175 rekeyed the Ð rows without a twin, a
// number written with Đ, Ð or D finds the same rows through the catalogue
// lookup, the candidates list and the request gate's exact filter. A twin
// (an Ð row whose folded key a Đ/D row already holds) keeps its Ð key and
// stays out of exact-number lookups, as before the fix.
func TestCatalogNumberKeyFoldCallers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalCatalogService(catalog, repository.NewLegalReviewRepository(db))
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	// as migration 000175 leaves them
	rows := []*types.LegalDocument{
		{ID: "5583", DocumentNumber: "02/2000/QĐ-BTC", NumberKey: "02/2000/QD-BTC", DocType: "Quyết định",
			Title: "Quyết định số 02/2000/QĐ-BTC", IssuedOn: day("2000-01-06")},
		// twin of 5583: left with its Ð key
		{ID: "107780", DocumentNumber: "02/2000/QÐ-BTC", NumberKey: "02/2000/QÐ-BTC", DocType: "Quyết định",
			Title: "Quyết định số 02/2000/QÐ-BTC", IssuedOn: day("2000-01-05")},
		// was "25/1999/QÐ-BTC" with no twin: rekeyed
		{ID: "eth-only", DocumentNumber: "25/1999/QĐ-BTC", NumberKey: "25/1999/QD-BTC", DocType: "Quyết định",
			Title: "Quyết định số 25/1999/QÐ-BTC", IssuedOn: day("1999-03-01")},
		{ID: "other", DocumentNumber: "02/2000/TT-BTC", NumberKey: "02/2000/TT-BTC", DocType: "Thông tư",
			Title: "Thông tư số 02/2000/TT-BTC", IssuedOn: day("2000-01-07")},
	}
	for _, r := range rows {
		r.Source = types.LegalDocumentSourceVBPL
	}
	require.NoError(t, catalog.UpsertDocuments(ctx, rows))

	for _, c := range []struct {
		inputs []string
		key    string
		want   []string
	}{
		{[]string{"02/2000/QĐ-BTC", "02/2000/QÐ-BTC", "02/2000/QD-BTC", "02/2000/qđ-btc"}, "02/2000/QD-BTC", []string{"5583"}},
		{[]string{"25/1999/QĐ-BTC", "25/1999/QÐ-BTC", "25/1999/QD-BTC"}, "25/1999/QD-BTC", []string{"eth-only"}},
	} {
		for _, in := range c.inputs {
			key := vietnamese_legal.CatalogNumberKey(in)
			require.Equal(t, c.key, key, in)

			reports, err := svc.Lookup(ctx, in, "", 10)
			require.NoError(t, err)
			var ids []string
			for _, r := range reports {
				ids = append(ids, r.Document.ID)
			}
			require.Equal(t, c.want, ids, "Lookup(%q)", in)
			require.Len(t, legalExactReports(reports, key, "Quyết định"), len(c.want), "request gate exact rows for %q", in)
		}
	}
	// a bare number is a key prefix ("02/…"), which the twin's Ð key also
	// starts with: the candidates list shows both copies, as before the fix
	_, total, err := svc.Candidates(ctx, "02", "Quyết định", 10)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	_, total, err = svc.Candidates(ctx, "02", "", 10)
	require.NoError(t, err)
	require.Equal(t, 3, total)
}

// TestCatalogImportTwinStaysOneRow: a re-import of the catalogue (the
// importer's ResolveTwins, then UpsertDocuments) neither revives the Ð twin
// migration 000175 left nor adds a new vbpl id of a catalogued document
// written with Ð, so a number typed with Đ, Ð or D still finds one row;
// a Cyrillic-keyed row is rekeyed and found by its Latin number.
func TestCatalogImportTwinStaysOneRow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalCatalogService(catalog, repository.NewLegalReviewRepository(db))
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "5583", Source: types.LegalDocumentSourceVBPL, DocumentNumber: "02/2000/QĐ-BTC", NumberKey: "02/2000/QD-BTC",
			DocType: "Quyết định", Title: "Quyết định số 02/2000/QĐ-BTC", IssuedOn: day("2000-01-06")},
		{ID: "107780", Source: types.LegalDocumentSourceVBPL, DocumentNumber: "02/2000/QÐ-BTC", NumberKey: "02/2000/QÐ-BTC",
			DocType: "Quyết định", Title: "Quyết định số 02/2000/QÐ-BTC", IssuedOn: day("2000-01-05")},
		{ID: "179043", Source: types.LegalDocumentSourceVBPL, DocumentNumber: "51/2025/TT-BTС", NumberKey: "51/2025/TT-BTС",
			DocType: "Thông tư", Title: "Thông tư số 51/2025/TT-BTС", IssuedOn: day("2025-06-24")},
	}))

	row := func(id, so, title, issued string) *types.LegalDocument {
		num := legalcatalog.CleanVBPLNumber(so)
		return &types.LegalDocument{ID: id, Source: types.LegalDocumentSourceVBPL, DocumentNumber: num,
			NumberKey: vietnamese_legal.CatalogNumberKey(num), DocType: "Quyết định", Title: title, IssuedOn: day(issued)}
	}
	cyr := row("179043", "51/2025/TT-BTС", "Thông tư số 51/2025/TT-BTС", "2025-06-24")
	cyr.DocType = "Thông tư"
	var existing []*types.LegalDocument
	require.NoError(t, db.Select("id", "document_number", "number_key", "doc_type", "title", "issued_on", "reviewed_at").Order("id").Find(&existing).Error)
	res := legalcatalog.ResolveTwins(existing, []*types.LegalDocument{
		row("5583", "02/2000/QĐ-BTC", "Quyết định số 02/2000/QĐ-BTC", "2000-01-06"),
		row("107780", "02/2000/QÐ-BTC", "Quyết định số 02/2000/QÐ-BTC", "2000-01-05"),
		row("300001", "02/2000/QÐ-BTC", "Quyết định số 02/2000/QÐ-BTC", "2000-01-06"),
		cyr,
	})
	require.Equal(t, map[string]string{"300001": "5583"}, res.Alias)
	require.NoError(t, catalog.UpsertDocuments(ctx, res.Keep))

	var n int64
	require.NoError(t, db.Model(&types.LegalDocument{}).Where("id = ?", "300001").Count(&n).Error)
	require.Zero(t, n, "a new Ð twin must not become a row")
	for _, in := range []string{"02/2000/QĐ-BTC", "02/2000/QÐ-BTC", "02/2000/QD-BTC"} {
		reports, err := svc.Lookup(ctx, in, "", 10)
		require.NoError(t, err)
		require.Len(t, reports, 1, in)
		require.Equal(t, "5583", reports[0].Document.ID, in)
	}
	reports, err := svc.Lookup(ctx, "51/2025/TT-BTC", "", 10)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, "51/2025/TT-BTC", reports[0].Document.DocumentNumber)
}
