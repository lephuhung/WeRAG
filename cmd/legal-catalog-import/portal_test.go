package main

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReconcileReviewedAppliesThePortalToVBPLRows(t *testing.T) {
	on := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	reviewedAt := on
	cur := &types.LegalDocument{ID: "1", Source: types.LegalDocumentSourceVBPL, DocumentNumber: "1/2025/QH15", NumberKey: "k",
		Title: "Luật A", Issuer: "Quốc hội", Status: "Còn hiệu lực", ExpiredOn: &on, ReviewedAt: &reviewedAt,
		SourcePending: types.JSON(`{"status":"x"}`)}
	in := &types.LegalDocument{ID: "1", Source: types.LegalDocumentSourceVBPL, DocumentNumber: "1/2025/QH15", NumberKey: "k",
		Title: "LUẬT A", Issuer: "", Status: "Hết hiệu lực một phần"} // issuer and expiry blank on the portal
	manual := &types.LegalDocument{ID: "2", Source: "manual", Status: "Còn hiệu lực"}
	unreviewed := &types.LegalDocument{ID: "3", Source: types.LegalDocumentSourceVBPL}

	got, err := reconcileReviewed([]*types.LegalDocument{in, {ID: "2", Status: "Hết hiệu lực toàn bộ"}, unreviewed},
		map[string]*types.LegalDocument{"1": cur, "2": manual})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.fresh) != 2 || got.fresh[0].ID != "1" || got.fresh[1].ID != "3" {
		t.Fatalf("fresh = %+v", got.fresh)
	}
	if in.Title != "LUẬT A" || in.Status != "Hết hiệu lực một phần" || in.Issuer != "Quốc hội" || in.ExpiredOn == nil {
		t.Errorf("row = %+v, want portal values with blanks kept", in)
	}
	if len(got.overwritten) != 1 {
		t.Fatalf("overwritten = %+v", got.overwritten)
	}
	before := got.overwritten[0].Before
	if before["title"] != "Luật A" || before["status"] != "Còn hiệu lực" || len(before) != 2 {
		t.Errorf("before = %v", before)
	}
	if p, ok := got.pending["1"]; !ok || p != nil {
		t.Errorf("the vbpl row's pending values should be cleared: %v %v", p, ok)
	}
	if string(got.pending["2"]) == "" || got.parked != 1 {
		t.Errorf("the manual row should keep its value and park the portal's: %+v", got)
	}
}

func portalTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, metadata TEXT, parse_status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE legal_documents (id TEXT PRIMARY KEY, source TEXT)`,
		`CREATE TABLE legal_document_links (knowledge_id TEXT PRIMARY KEY, knowledge_base_id TEXT, legal_document_id TEXT,
			review_status TEXT, suggestion TEXT, reviewed_by TEXT, reviewed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE legal_feed_log (id INTEGER PRIMARY KEY, action TEXT, vbpl_id TEXT, document_number TEXT, reason TEXT, detail TEXT)`,
		`INSERT INTO knowledge_bases VALUES ('pub', 0, NULL), ('priv', 7, NULL)`,
		`INSERT INTO legal_documents VALUES ('v1', 'vbpl'), ('v2', 'vbpl'), ('v3', 'vbpl'), ('v4', 'vbpl'), ('t1', 'tvpl')`,
		`INSERT INTO knowledges VALUES
			('new', 'pub', '{"external_id":"v1"}', 'completed', NULL),
			('parsing', 'pub', '{"external_id":"v2"}', 'pending', NULL),
			('private', 'priv', '{"external_id":"v3"}', 'completed', NULL),
			('feed', 'pub', '{"external_id":"t1"}', 'completed', NULL),
			('linked', 'pub', '{"external_id":"v4"}', 'completed', NULL)`,
		`INSERT INTO legal_document_links (knowledge_id, legal_document_id, review_status) VALUES ('linked', 'v4', 'pending')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestLinkPortalKnowledge(t *testing.T) {
	db := portalTestDB(t)
	ctx := context.Background()
	n, err := linkPortalKnowledge(ctx, db)
	if err != nil || n != 1 {
		t.Fatalf("added = %d, err = %v, want only the parsed public vbpl document", n, err)
	}
	var status, by string
	if err := db.Raw(`SELECT review_status, reviewed_by FROM legal_document_links WHERE knowledge_id = 'new'`).Row().Scan(&status, &by); err != nil {
		t.Fatal(err)
	}
	if status != string(types.LegalReviewApproved) || by != types.LegalReviewerVBPL {
		t.Errorf("link = %s by %s", status, by)
	}
	if n, err := linkPortalKnowledge(ctx, db); err != nil || n != 0 {
		t.Errorf("second run added = %d, err = %v", n, err)
	}
}

func TestLogPortalOverwritesKeepsTheOldValues(t *testing.T) {
	db := portalTestDB(t)
	err := logPortalOverwrites(context.Background(), db, []portalOverwrite{{ID: "v1", DocumentNumber: "1/2025/QH15", Before: map[string]string{"status": "Còn hiệu lực"}}})
	if err != nil {
		t.Fatal(err)
	}
	var action, detail string
	if err := db.Raw(`SELECT action, detail FROM legal_feed_log WHERE vbpl_id = 'v1'`).Row().Scan(&action, &detail); err != nil {
		t.Fatal(err)
	}
	if action != portalOverwriteAction || detail != `{"before":{"status":"Còn hiệu lực"}}` {
		t.Errorf("logged %s %s", action, detail)
	}
}
