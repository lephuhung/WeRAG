package main

import (
	"context"
	"fmt"
	"strings"
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

func TestQueueAutoIngest(t *testing.T) {
	db := queueTestDB(t)
	ctx := context.Background()
	now := time.Now()
	for _, r := range []types.LegalDocumentRequest{
		{ID: "wait", NumberKey: "waiting", LegalDocumentID: "20", Source: types.LegalRequestSourceNoFullText, Status: types.LegalRequestRequested, FirstRequestedAt: now, LastRequestedAt: now},
		{ID: "busy", NumberKey: "claimed", LegalDocumentID: "30", Status: types.LegalRequestRequested, ClaimedBy: "u1", ClaimedAt: &now, FirstRequestedAt: now, LastRequestedAt: now},
		{ID: "gave", NumberKey: "skipped", LegalDocumentID: "40", Status: types.LegalRequestRequested, Detail: types.JSON(`{"auto_ingest_skip":"x"}`), FirstRequestedAt: now, LastRequestedAt: now},
		{ID: "no", NumberKey: "rejected", LegalDocumentID: "50", Status: types.LegalRequestRejected, FirstRequestedAt: now, LastRequestedAt: now},
		{ID: "back", NumberKey: "reinstated", LegalDocumentID: "60", Status: types.LegalRequestAdded, KnowledgeID: "gone", KnowledgeBaseID: "kb", DecidedBy: "x", DecidedAt: &now, FirstRequestedAt: now, LastRequestedAt: now},
	} {
		r := r
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	docs := []*types.LegalDocument{queueDoc("10", "new"), queueDoc("20", "waiting"), queueDoc("30", "claimed"), queueDoc("40", "skipped"), queueDoc("50", "rejected"), queueDoc("60", "reinstated")}
	html := map[string]bool{"10": true, "20": true, "30": true, "40": true, "50": true, "60": true}
	added, moved, err := queueAutoIngest(ctx, db, docs, nil, html, now)
	if err != nil || added != 1 || moved != 2 {
		t.Fatalf("added = %d, moved = %d, err = %v", added, moved, err)
	}
	var waiting types.LegalDocumentRequest
	db.Take(&waiting, "id = ?", "wait")
	if waiting.Status != types.LegalRequestAutoIngest || waiting.Source != types.LegalRequestSourceHTML {
		t.Errorf("waiting = %+v", waiting)
	}
	var back types.LegalDocumentRequest
	db.Take(&back, "id = ?", "back")
	if back.Status != types.LegalRequestAutoIngest || back.KnowledgeID != "" || back.DecidedAt != nil {
		t.Errorf("reinstated = %+v", back)
	}
	var fresh types.LegalDocumentRequest
	db.Take(&fresh, "legal_document_id = ?", "10")
	if fresh.Status != types.LegalRequestAutoIngest || fresh.RequestCount != 0 {
		t.Errorf("fresh = %+v", fresh)
	}
	for _, id := range []string{"busy", "gave"} {
		var r types.LegalDocumentRequest
		db.Take(&r, "id = ?", id)
		if r.Status != types.LegalRequestRequested {
			t.Errorf("%s = %s", id, r.Status)
		}
	}
}

func TestQueueChangedText(t *testing.T) {
	db := queueTestDB(t)
	ctx := context.Background()
	now := time.Now()
	for _, stmt := range []string{
		`ALTER TABLE knowledges ADD COLUMN parse_status TEXT DEFAULT 'completed'`,
		`ALTER TABLE knowledges ADD COLUMN file_hash TEXT DEFAULT ''`,
		`INSERT INTO knowledge_bases (id, owner_tenant_id) VALUES ('kb', 0), ('private', 7)`,
		`INSERT INTO knowledges (id, knowledge_base_id, metadata, file_hash, parse_status) VALUES
			('new', 'kb', '{"external_id":"10","source":"vbpl"}', 'old', 'completed'),
			('same', 'kb', '{"external_id":"20","source":"vbpl"}', 'h20', 'completed'),
			('feed', 'kb', '{"external_id":"30","source":"tvpl"}', 'old', 'completed'),
			('parsing', 'kb', '{"external_id":"40","source":"vbpl"}', 'old', 'processing'),
			('priv', 'private', '{"external_id":"50","source":"vbpl"}', 'old', 'completed'),
			('closed', 'kb', '{"external_id":"60","source":"vbpl"}', 'old', 'completed'),
			('no', 'kb', '{"external_id":"70","source":"vbpl"}', 'old', 'completed'),
			('gone', 'kb', '{"external_id":"80","source":"vbpl"}', 'old', 'completed')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []types.LegalDocumentRequest{
		{ID: "was-added", NumberKey: "k60", LegalDocumentID: "60", Status: types.LegalRequestAdded, KnowledgeID: "closed", KnowledgeBaseID: "kb", DecidedBy: "x", DecidedAt: &now, FirstRequestedAt: now, LastRequestedAt: now},
		{ID: "rejected", NumberKey: "k70", LegalDocumentID: "70", Status: types.LegalRequestRejected, Detail: types.JSON(`{"auto_ingest_skip":"x","keep":1}`), DecisionNote: "n", FirstRequestedAt: now, LastRequestedAt: now},
	} {
		r := r
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	var docs []*types.LegalDocument
	sums := map[string]string{}
	for _, id := range []string{"10", "20", "30", "40", "50", "60", "70"} {
		docs = append(docs, queueDoc(id, "k"+id))
		sums[id] = "h" + id
	}
	expired := queueDoc("80", "k80")
	expired.Status = "Hết hiệu lực toàn bộ"
	docs = append(docs, expired)
	sums["80"] = "h80"

	n, err := queueChangedText(ctx, db, docs, sums, now)
	if err != nil || n != 3 {
		t.Fatalf("queued = %d, err = %v, want the changed, the added and the rejected one", n, err)
	}
	var rejected types.LegalDocumentRequest
	db.Take(&rejected, "id = ?", "rejected")
	if rejected.Status != types.LegalRequestAutoIngest || autoIngestSkipped(rejected.Detail) || rejected.DecisionNote != "" ||
		!strings.Contains(string(rejected.Detail), `"keep"`) {
		t.Errorf("a rejection must not stop a text update: %+v", rejected)
	}
	var fresh types.LegalDocumentRequest
	if err := db.Take(&fresh, "legal_document_id = ?", "10").Error; err != nil {
		t.Fatal(err)
	}
	if fresh.Status != types.LegalRequestAutoIngest || fresh.KnowledgeBaseID != "kb" || fresh.RequestCount != 0 {
		t.Errorf("fresh = %+v", fresh)
	}
	var added types.LegalDocumentRequest
	db.Take(&added, "id = ?", "was-added")
	if added.Status != types.LegalRequestAutoIngest || added.KnowledgeBaseID != "kb" || added.KnowledgeID != "" || added.DecidedAt != nil {
		t.Errorf("added = %+v", added)
	}
	var count int64
	db.Model(&types.LegalDocumentRequest{}).Where("status = ?", types.LegalRequestAutoIngest).Count(&count)
	if count != 3 {
		t.Errorf("%d rows queued, want 3", count)
	}
	// Once queued, the next night adds nothing.
	if n, err := queueChangedText(ctx, db, docs, sums, now); err != nil || n != 0 {
		t.Errorf("second run = %d, %v", n, err)
	}
}
