package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAbbrevRepo(t *testing.T) (*gorm.DB, *abbreviationRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.Abbreviation{}); err != nil {
		t.Fatalf("migrate abbreviations: %v", err)
	}
	return db, &abbreviationRepository{db: db}
}

func TestAbbreviationRepositoryCRUD(t *testing.T) {
	_, repo := newAbbrevRepo(t)
	ctx := context.Background()

	row := &types.Abbreviation{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.ID == "" {
		t.Fatal("expected BeforeCreate to assign a UUID")
	}

	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("get by id: %v %v", got, err)
	}
	if got.FullForm != "Ủy ban nhân dân" {
		t.Errorf("full_form = %q", got.FullForm)
	}

	got.ShortForm = "ubnd"
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	// case-insensitive short-form lookup must find the lowercase row
	byShort, err := repo.ListByShortForm(ctx, "UBND")
	if err != nil || len(byShort) != 1 {
		t.Fatalf("ListByShortForm: %v %v", byShort, err)
	}

	if err := repo.Delete(ctx, row.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = repo.GetByID(ctx, row.ID)
	if err != nil || got != nil {
		t.Fatalf("soft-deleted row must be invisible: %v %v", got, err)
	}
}

func TestAbbreviationRepositoryListFilters(t *testing.T) {
	_, repo := newAbbrevRepo(t)
	ctx := context.Background()
	rows := []*types.Abbreviation{
		{ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
		{ShortForm: "BMNN", FullForm: "Bộ Nông nghiệp", IsActive: false},
		{ShortForm: "TTHT", FullForm: "Trợ giúp tương hỗ", IsActive: true},
	}
	for _, r := range rows {
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	// no filter → all 3
	list, total, err := repo.List(ctx, "", nil, 0, 20)
	if err != nil || total != 3 || len(list) != 3 {
		t.Fatalf("list: total=%d len=%d err=%v", total, len(list), err)
	}

	// active filter
	tru := true
	list, total, err = repo.List(ctx, "", &tru, 0, 20)
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("active list: total=%d len=%d err=%v", total, len(list), err)
	}

	// search matches short_form OR full_form case-insensitively (sqlite path)
	list, total, err = repo.List(ctx, "nông nghiệp", nil, 0, 20)
	if err != nil || total != 1 || list[0].ShortForm != "BMNN" {
		t.Fatalf("search: %+v total=%d err=%v", list, total, err)
	}

	// ListActive returns only active rows
	active, err := repo.ListActive(ctx)
	if err != nil || len(active) != 2 {
		t.Fatalf("ListActive: %v %v", active, err)
	}
}
