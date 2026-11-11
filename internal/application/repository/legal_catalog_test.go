package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newLegalCatalogRepo(t *testing.T) *legalCatalogRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}); err != nil {
		t.Fatalf("migrate legal catalog: %v", err)
	}
	return &legalCatalogRepository{db: db}
}

func legalDay(s string) *time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return &d
}

func TestLegalCatalogRepository(t *testing.T) {
	repo := newLegalCatalogRepo(t)
	ctx := context.Background()

	docs := []*types.LegalDocument{
		{ID: "old", DocumentNumber: "45/2013/QH13", NumberKey: "45/2013/QH13", Title: "Luật Đất đai số 45/2013/QH13", IssuedOn: legalDay("2013-11-29"), Status: "Hết hiệu lực toàn bộ"},
		{ID: "new", DocumentNumber: "31/2024/QH15", NumberKey: "31/2024/QH15", Title: "Đất đai", IssuedOn: legalDay("2024-01-18"), Status: "Còn hiệu lực"},
		{ID: "nd", DocumentNumber: "102/2024/NĐ-CP", NumberKey: "102/2024/ND-CP", Title: "Nghị định quy định chi tiết Luật Đất đai", IssuedOn: legalDay("2024-07-30"), Status: "Còn hiệu lực"},
	}
	if err := repo.UpsertDocuments(ctx, docs); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// A re-import refreshes the row instead of failing on the primary key.
	docs[1].Title = "Luật Đất đai số 31/2024/QH15"
	if err := repo.UpsertDocuments(ctx, docs[1:2]); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	got, err := repo.FindByNumberKey(ctx, "31/2024/QH15", 5)
	if err != nil || len(got) != 1 || got[0].Title != "Luật Đất đai số 31/2024/QH15" {
		t.Fatalf("exact key: %+v %v", got, err)
	}
	// A number typed without its suffix falls back to the prefix match.
	got, err = repo.FindByNumberKey(ctx, "102/2024", 5)
	if err != nil || len(got) != 1 || got[0].ID != "nd" {
		t.Fatalf("prefix key: %+v %v", got, err)
	}
	got, err = repo.SearchByTitle(ctx, []string{"Đất", "đai"}, 5) // SQLite lower() folds ASCII only; Postgres uses ILIKE
	if err != nil || len(got) != 3 || got[0].ID != "nd" {
		t.Fatalf("title search newest first: %+v %v", got, err)
	}

	rels := []*types.LegalDocumentRelation{
		{FromID: "new", ToID: "old", Relation: types.LegalRelationExpires},
		{FromID: "nd", ToID: "new", Relation: types.LegalRelationGuides},
		{FromID: "nd", ToID: "new", Relation: types.LegalRelationBasedOn},
	}
	if err := repo.ReplaceRelations(ctx, rels); err != nil {
		t.Fatalf("relations: %v", err)
	}
	links, err := repo.ListLinks(ctx, "new",
		[]types.LegalRelation{types.LegalRelationExpires},
		[]types.LegalRelation{types.LegalRelationGuides})
	if err != nil || len(links) != 2 {
		t.Fatalf("links: %+v %v", links, err)
	}
	for _, l := range links {
		switch {
		case !l.Incoming && l.Other.ID == "old" && l.Relation == types.LegalRelationExpires:
		case l.Incoming && l.Other.ID == "nd" && l.Relation == types.LegalRelationGuides:
		default:
			t.Errorf("unexpected link %+v", l)
		}
	}
	// A full snapshot replaces the previous relation set.
	if err := repo.ReplaceRelations(ctx, rels[:1]); err != nil {
		t.Fatalf("replace: %v", err)
	}
	links, _ = repo.ListLinks(ctx, "new", nil, []types.LegalRelation{types.LegalRelationGuides})
	if len(links) != 0 {
		t.Fatalf("stale relations survived: %+v", links)
	}
}

// A bare number of one type: the prefix is escaped, the type matched
// whatever its case, newest first, at most limit rows.
func TestLegalCatalogFindByNumberPrefixTypes(t *testing.T) {
	repo := newLegalCatalogRepo(t)
	ctx := context.Background()
	docs := []*types.LegalDocument{
		{ID: "nd19", DocumentNumber: "68/2019/NĐ-CP", NumberKey: "68/2019/ND-CP", DocType: "Nghị định", IssuedOn: legalDay("2019-08-14")},
		{ID: "nd26", DocumentNumber: "68/2026/NĐ-CP", NumberKey: "68/2026/ND-CP", DocType: "nghị định", IssuedOn: legalDay("2026-03-01")},
		{ID: "tt26", DocumentNumber: "68/2026/TT-BTC", NumberKey: "68/2026/TT-BTC", DocType: "Thông tư", IssuedOn: legalDay("2026-04-01")},
		{ID: "ttlt", DocumentNumber: "68/2010/TTLT-BTC-BNV", NumberKey: "68/2010/TTLT-BTC-BNV", DocType: "Thông tư liên tịch", IssuedOn: legalDay("2010-01-01")},
		{ID: "x168", DocumentNumber: "168/2020/NĐ-CP", NumberKey: "168/2020/ND-CP", DocType: "Nghị định", IssuedOn: legalDay("2020-12-31")},
		// "%" / "_" in a key must not act as wildcards
		{ID: "pct", DocumentNumber: "6%/2020/NĐ-CP", NumberKey: "6%/2020/ND-CP", DocType: "Nghị định", IssuedOn: legalDay("2020-01-01")},
		{ID: "u", DocumentNumber: "6_/2020/NĐ-CP", NumberKey: "6_/2020/ND-CP", DocType: "Nghị định", IssuedOn: legalDay("2020-01-01")},
	}
	if err := repo.UpsertDocuments(ctx, docs); err != nil {
		t.Fatal(err)
	}
	ids := func(key string, docTypes []string, limit int) []string {
		t.Helper()
		got, err := repo.FindByNumberPrefixTypes(ctx, key, docTypes, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range got {
			out = append(out, d.ID)
		}
		return out
	}
	eq := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	if got := ids("68", []string{"Nghị định"}, 10); !eq(got, "nd26", "nd19") {
		t.Errorf("Nghị định 68: %v", got)
	}
	if got := ids("68", []string{"nghị định"}, 10); !eq(got, "nd26", "nd19") {
		t.Errorf("lower-case type: %v", got)
	}
	if got := ids("68", []string{"Thông tư", "Thông tư liên tịch"}, 10); !eq(got, "tt26", "ttlt") {
		t.Errorf("Thông tư 68: %v", got)
	}
	if got := ids("68", []string{"Nghị định"}, 1); !eq(got, "nd26") {
		t.Errorf("limit: %v", got)
	}
	if got := ids("68/2026", []string{"Nghị định"}, 10); !eq(got, "nd26") {
		t.Errorf("68/2026: %v", got)
	}
	if got := ids("6%", []string{"Nghị định"}, 10); !eq(got, "pct") {
		t.Errorf("escaped %%: %v", got)
	}
	if got := ids("6_", []string{"Nghị định"}, 10); !eq(got, "u") {
		t.Errorf("escaped _: %v", got)
	}
	if got := ids("68", nil, 10); len(got) != 0 {
		t.Errorf("no type: %v", got)
	}
}
