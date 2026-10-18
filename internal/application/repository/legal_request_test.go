package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

func newLegalRequestRepo(t *testing.T) (*legalRequestRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.LegalDocumentRequest{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`CREATE TABLE knowledges (id TEXT, knowledge_base_id TEXT, title TEXT, file_name TEXT, deleted_at DATETIME, metadata TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE knowledge_bases (id TEXT, name TEXT, owner_tenant_id INTEGER, deleted_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	return &legalRequestRepository{db: db}, db
}

func TestLegalRequestRecordCountsSessionsOnce(t *testing.T) {
	repo, _ := newLegalRequestRepo(t)
	ctx := context.Background()
	ask := func(session string) *types.LegalDocumentRequest {
		t.Helper()
		row, err := repo.Record(ctx, &types.LegalDocumentRequest{
			NumberKey: "53/2022/ND-CP", DocumentNumber: "53/2022/NĐ-CP", LegalDocumentID: "155366",
			Source: types.LegalRequestSourceCatalogue, LastSessionID: session, LastQuery: "Nghị định 53/2022/NĐ-CP quy định gì?",
		})
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		return row
	}
	first := ask("s1")
	if first.RequestCount != 1 || first.Status != types.LegalRequestRequested {
		t.Fatalf("first = %+v", first)
	}
	if again := ask("s1"); again.RequestCount != 1 || again.ID != first.ID {
		t.Fatalf("same session must not bump: %+v", again)
	}
	if other := ask("s2"); other.RequestCount != 2 || other.LastSessionID != "s2" {
		t.Fatalf("another session bumps: %+v", other)
	}
	// a number the catalogue does not know is its own row
	unknown, err := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "53/2022/ND-CP", LastSessionID: "s3"})
	if err != nil || unknown.ID == first.ID || unknown.RequestCount != 1 {
		t.Fatalf("unknown row = %+v %v", unknown, err)
	}

	// rejected rows stay rejected and keep their count
	if _, err := repo.Decide(ctx, first.ID, types.LegalRequestRejected, "admin", "không thuộc phạm vi", "", ""); err != nil {
		t.Fatal(err)
	}
	if rej := ask("s9"); rej.Status != types.LegalRequestRejected || rej.RequestCount != 2 || rej.DecisionNote != "không thuộc phạm vi" {
		t.Fatalf("rejected = %+v", rej)
	}

	// added rows flip with MarkAddedByDocument and reopen when asked again
	if _, err := repo.Decide(ctx, first.ID, types.LegalRequestRequested, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	n, err := repo.MarkAddedByDocument(ctx, "155366", "k1", "kb1", "ep")
	if err != nil || n != 1 {
		t.Fatalf("mark added = %d %v", n, err)
	}
	got, _ := repo.Get(ctx, first.ID)
	if got.Status != types.LegalRequestAdded || got.KnowledgeID != "k1" || got.DecidedAt == nil {
		t.Fatalf("added = %+v", got)
	}
	if reopened := ask("s10"); reopened.Status != types.LegalRequestRequested || reopened.RequestCount != 3 || reopened.KnowledgeID != "" {
		t.Fatalf("reopened = %+v", reopened)
	}

	counts, err := repo.CountByStatus(ctx)
	if err != nil || counts[types.LegalRequestRequested] != 2 || counts[types.LegalRequestRejected] != 0 {
		t.Fatalf("counts = %v %v", counts, err)
	}
	rows, total, err := repo.List(ctx, types.LegalRequestRequested, 0, 10)
	if err != nil || total != 2 || len(rows) != 2 || rows[0].ID != first.ID {
		t.Fatalf("list = %d %v (%v)", total, rows, err)
	}
}

func TestLegalRequestKnowledgeNamed(t *testing.T) {
	repo, db := newLegalRequestRepo(t)
	ctx := context.Background()
	db.Exec(`INSERT INTO knowledges (id, knowledge_base_id, title, file_name, deleted_at) VALUES ('k1','kb1','Nghị định 53-2022-ND-CP về an ninh mạng','x.pdf',NULL),
		('k2','kb2','53/2022/NĐ-CP','y.pdf',NULL), ('k3','kb1','01/2024/TT-BCA','z.pdf','2026-01-01')`)
	db.Exec(`INSERT INTO knowledge_bases VALUES ('kb1','VBPL – Nghị định',0,NULL),('kb2','Kho riêng',10003,NULL)`)
	ok, err := repo.KnowledgeNamed(ctx, []string{"kb1"}, []string{"53/2022/NĐ-CP", "53-2022-nd-cp"})
	if err != nil || !ok {
		t.Fatalf("dash-folded file name must match: %v %v", ok, err)
	}
	if ok, _ := repo.KnowledgeNamed(ctx, []string{"kb1"}, []string{"01/2024/TT-BCA"}); ok {
		t.Fatal("deleted knowledge must not count")
	}
	if ok, _ := repo.KnowledgeNamed(ctx, []string{"kb3"}, []string{"53/2022/NĐ-CP"}); ok {
		t.Fatal("other KBs must not count")
	}
	db.Exec(`UPDATE knowledges SET metadata = '{"external_id":"155366"}' WHERE id = 'k1'`)
	db.Exec(`UPDATE knowledges SET metadata = '{"external_id":"777"}' WHERE id = 'k2'`)
	db.Exec(`UPDATE knowledges SET metadata = '{"external_id":"888"}' WHERE id = 'k3'`)
	held, err := repo.PublicKnowledgeByExternalIDs(ctx, []string{"155366", "777", "888", "999"})
	// k2 is in a tenant KB, k3 is deleted
	if err != nil || len(held) != 1 || !held["155366"] {
		t.Fatalf("public by external id = %v %v", held, err)
	}
	kbs, err := repo.PublicKnowledgeBases(ctx)
	if err != nil || len(kbs) != 1 || kbs[0].ID != "kb1" {
		t.Fatalf("public KBs = %v %v", kbs, err)
	}
}
