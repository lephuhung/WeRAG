package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNoFullTextCandidates(t *testing.T) {
	docs := []*types.LegalDocument{
		{ID: "1", NumberKey: "1/2026/ND-CP", Status: "Còn hiệu lực"},
		{ID: "2", NumberKey: "", Status: "Còn hiệu lực"},
		{ID: "3", NumberKey: "3/2026/ND-CP", Status: "Hết hiệu lực toàn bộ"},
		{ID: "4", NumberKey: "4/2026/ND-CP", Status: "Hết hiệu lực một phần"},
		{ID: "5", NumberKey: "5/2026/ND-CP", Status: "Còn hiệu lực"}, // has its text
	}
	got := noFullTextCandidates(docs, map[string]bool{"1": true, "2": true, "3": true, "4": true})
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "4" {
		t.Fatalf("candidates = %+v", got)
	}
}

func queueTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, metadata TEXT, deleted_at DATETIME)`,
		`CREATE TABLE legal_document_links (knowledge_id TEXT, legal_document_id TEXT, review_status TEXT)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&types.LegalDocumentRequest{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func queueDoc(id, key string) *types.LegalDocument {
	return &types.LegalDocument{ID: id, NumberKey: key, DocumentNumber: key, Status: "Còn hiệu lực"}
}

func TestEnqueueNoFullText(t *testing.T) {
	db := queueTestDB(t)
	ctx := context.Background()
	now := time.Now()
	for _, r := range []types.LegalDocumentRequest{
		{ID: "r1", NumberKey: "rejected", LegalDocumentID: "20", Status: types.LegalRequestRejected, FirstRequestedAt: now, LastRequestedAt: now},
		{ID: "r2", NumberKey: "chat-no-id", Status: types.LegalRequestRequested, FirstRequestedAt: now, LastRequestedAt: now},
	} {
		r := r
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{
		`INSERT INTO knowledge_bases (id, owner_tenant_id) VALUES ('kb', 0), ('private', 7)`,
		`INSERT INTO knowledges (id, knowledge_base_id, metadata) VALUES
			('k1', 'kb', '{}'), ('k2', 'kb', '{"external_id":"50"}'), ('k3', 'private', '{"external_id":"60"}')`,
		`INSERT INTO legal_document_links VALUES ('k1', '40', 'approved')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	docs := []*types.LegalDocument{
		queueDoc("10", "new"), queueDoc("20", "rejected"), queueDoc("30", "chat-no-id"),
		queueDoc("40", "linked"), queueDoc("50", "in-public-kb"), queueDoc("60", "only-private-copy"),
	}
	noText := map[string]bool{"10": true, "20": true, "30": true, "40": true, "50": true, "60": true}

	n, err := enqueueNoFullText(ctx, db, docs, nil, noText, now)
	if err != nil || n != 2 {
		t.Fatalf("added = %d, err = %v, want 2", n, err)
	}
	var rows []types.LegalDocumentRequest
	if err := db.Where("source = ?", types.LegalRequestSourceNoFullText).Order("legal_document_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].LegalDocumentID != "10" || rows[1].LegalDocumentID != "60" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].RequestCount != 0 || rows[0].Status != types.LegalRequestRequested {
		t.Errorf("row = %+v, want an open request with count 0", rows[0])
	}
	// The next night adds nothing and leaves the rejected request alone.
	if n, err := enqueueNoFullText(ctx, db, docs, nil, noText, now); err != nil || n != 0 {
		t.Fatalf("second run added = %d, err = %v", n, err)
	}
	var rejected types.LegalDocumentRequest
	if err := db.Take(&rejected, "id = ?", "r1").Error; err != nil || rejected.Status != types.LegalRequestRejected {
		t.Errorf("rejected request = %+v, err = %v", rejected, err)
	}
}

func TestEnqueueNoFullTextSkipsACopyHeldUnderAnotherID(t *testing.T) {
	db := queueTestDB(t)
	ctx := context.Background()
	day := func(d string) *time.Time {
		v, _ := time.Parse("2006-01-02", d)
		return &v
	}
	row := func(id, typ, on string) *types.LegalDocument {
		return &types.LegalDocument{ID: id, NumberKey: "25/2026/TT-BGDDT", DocumentNumber: "25/2026/TT-BGDĐT",
			DocType: typ, Issuer: "Bộ Giáo dục và Đào tạo", Status: "Còn hiệu lực", IssuedOn: day(on)}
	}
	for _, stmt := range []string{
		`INSERT INTO knowledge_bases (id, owner_tenant_id) VALUES ('kb', 0)`,
		`INSERT INTO knowledges (id, knowledge_base_id, metadata) VALUES ('k1', 'kb', '{"external_id":"old"}')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	held := row("old", "Thông tư", "2026-04-09")
	same := row("new", "Thông tư", "2026-04-09")
	other := row("other", "Nghị quyết", "2026-05-01") // the number reused by another document
	n, err := enqueueNoFullText(ctx, db, []*types.LegalDocument{same, other},
		[]*types.LegalDocument{held, same, other}, map[string]bool{"new": true, "other": true}, time.Now())
	if err != nil || n != 1 {
		t.Fatalf("added = %d, err = %v, want only the other document", n, err)
	}
	var row1 types.LegalDocumentRequest
	if err := db.Take(&row1).Error; err != nil || row1.LegalDocumentID != "other" {
		t.Fatalf("queued = %+v, err = %v", row1, err)
	}
}

func TestEnqueueNoFullTextCapsANightKeepingTheNewest(t *testing.T) {
	db := queueTestDB(t)
	noText := map[string]bool{}
	var docs []*types.LegalDocument
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= maxNoFullTextRequests; i++ {
		id := fmt.Sprintf("%d", i)
		on := base.AddDate(0, 0, i)
		d := queueDoc(id, "key-"+id)
		d.IssuedOn = &on
		docs = append(docs, d)
		noText[id] = true
	}
	n, err := enqueueNoFullText(context.Background(), db, docs, docs, noText, time.Now())
	if err != nil || n != maxNoFullTextRequests {
		t.Fatalf("added = %d, err = %v", n, err)
	}
	var oldest int64
	db.Model(&types.LegalDocumentRequest{}).Where("legal_document_id = ?", "0").Count(&oldest)
	if oldest != 0 {
		t.Error("the oldest document should wait for a later night")
	}
}
