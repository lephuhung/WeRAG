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
// look-alike "Ð" and migration 000174 rekeyed the Ð rows without a twin, a
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
	// as migration 000174 leaves them
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
