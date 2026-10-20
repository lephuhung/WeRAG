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

// TestCatalogNumberKeyFoldCallers: once CatalogNumberKey folds the
// look-alike "Ð" and migration 000173 rekeyed the catalogue, a number
// written with Đ, Ð or D finds the same rows through the catalogue lookup,
// the candidates list and the request gate's exact filter — including the
// old row that was stored with Ð (the same Quyết định imported twice).
func TestCatalogNumberKeyFoldCallers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	svc := NewLegalCatalogService(catalog, repository.NewLegalReviewRepository(db))
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	// as migration 000173 leaves them: 107780 was "02/2000/QÐ-BTC"
	rows := []*types.LegalDocument{
		{ID: "5583", DocumentNumber: "02/2000/QĐ-BTC", DocType: "Quyết định", Title: "Quyết định số 02/2000/QĐ-BTC", IssuedOn: day("2000-01-06")},
		{ID: "107780", DocumentNumber: "02/2000/QĐ-BTC", DocType: "Quyết định", Title: "Quyết định số 02/2000/QÐ-BTC", IssuedOn: day("2000-01-05")},
		{ID: "other", DocumentNumber: "02/2000/TT-BTC", DocType: "Thông tư", Title: "Thông tư số 02/2000/TT-BTC", IssuedOn: day("2000-01-07")},
	}
	for _, r := range rows {
		r.Source = types.LegalDocumentSourceVBPL
		r.NumberKey = vietnamese_legal.CatalogNumberKey(r.DocumentNumber)
	}
	require.NoError(t, catalog.UpsertDocuments(ctx, rows))

	for _, in := range []string{"02/2000/QĐ-BTC", "02/2000/QÐ-BTC", "02/2000/QD-BTC", "02/2000/qđ-btc"} {
		key := vietnamese_legal.CatalogNumberKey(in)
		require.Equal(t, "02/2000/QD-BTC", key, in)

		reports, err := svc.Lookup(ctx, in, "", 10)
		require.NoError(t, err)
		var ids []string
		for _, r := range reports {
			ids = append(ids, r.Document.ID)
		}
		require.Equal(t, []string{"5583", "107780"}, ids, "Lookup(%q): newest first", in)

		exact := legalExactReports(reports, key, "Quyết định")
		require.Len(t, exact, 2, "request gate exact rows for %q", in)
	}
	// a bare number lists both copies of the Quyết định, typed or not
	docs, total, err := svc.Candidates(ctx, "02", "Quyết định", 10)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, docs, 2)
	_, total, err = svc.Candidates(ctx, "02", "", 10)
	require.NoError(t, err)
	require.Equal(t, 3, total)
}
