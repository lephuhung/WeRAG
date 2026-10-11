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
