package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestLegalDraftRepositoryAndRequestStates(t *testing.T) {
	repo, db := newLegalRequestRepo(t)
	if err := db.AutoMigrate(&types.LegalDocumentDraft{}); err != nil {
		t.Fatal(err)
	}
	drafts := NewLegalDraftRepository(db)
	ctx := context.Background()

	d := &types.LegalDocumentDraft{LegalDocumentID: "3003", DocumentNumber: "3/2021/TT-BTC", OCRMarkdown: "Điều 1. Phạm vi"}
	if err := drafts.Create(ctx, d); err != nil || d.ID == "" || d.Status != types.LegalDraftFetching {
		t.Fatalf("create: %+v %v", d, err)
	}
	if err := drafts.Create(ctx, &types.LegalDocumentDraft{LegalDocumentID: "3003"}); err == nil {
		t.Fatal("a second draft of the same document was accepted")
	}
	if err := drafts.Update(ctx, d.ID, map[string]any{"status": types.LegalDraftReady, "ocr_pages": 4}); err != nil {
		t.Fatal(err)
	}
	got, err := drafts.GetByDocumentID(ctx, "3003")
	if err != nil || got.ID != d.ID || got.Status != types.LegalDraftReady || got.OCRPages != 4 || got.OCRMarkdown == "" {
		t.Fatalf("get: %+v %v", got, err)
	}
	rows, total, err := drafts.List(ctx, types.LegalDraftReady, 0, 10)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].OCRMarkdown != "" || rows[0].OCRPages != 4 {
		t.Fatalf("list: %+v %d %v", rows, total, err)
	}
	if rows, _ := drafts.ListByDocumentIDs(ctx, []string{"3003", "x"}); len(rows) != 1 || rows[0].OCRMarkdown != "" {
		t.Fatalf("by ids: %+v", rows)
	}
	if missing, err := drafts.Get(ctx, "nope"); missing != nil || err != nil {
		t.Fatalf("missing: %+v %v", missing, err)
	}

	// request states around a draft
	req, _ := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "3/2021/TT-BTC", LegalDocumentID: "3003", LastSessionID: "s1"})
	if n, err := repo.SetStatusByDocument(ctx, "3003", []string{types.LegalRequestRequested}, types.LegalRequestFetched); n != 1 || err != nil {
		t.Fatalf("set status: %d %v", n, err)
	}
	again, _ := repo.Record(ctx, &types.LegalDocumentRequest{NumberKey: "3/2021/TT-BTC", LegalDocumentID: "3003", LastSessionID: "s2"})
	if again.Status != types.LegalRequestFetched || again.RequestCount != 2 {
		t.Fatalf("asked again while fetched: %+v", again)
	}
	counts, _ := repo.CountByStatus(ctx)
	if counts[types.LegalRequestFetched] != 1 || counts[types.LegalRequestReviewing] != 0 {
		t.Fatalf("counts = %v", counts)
	}
	if n, _ := repo.MarkAddedByDocument(ctx, "3003", "k1", "kb", "u"); n != 1 {
		t.Fatalf("a fetched request was not closed by the ingest")
	}
	if row, _ := repo.Get(ctx, req.ID); row.Status != types.LegalRequestAdded {
		t.Fatalf("status = %q", row.Status)
	}
	if err := drafts.Delete(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if gone, _ := drafts.Get(ctx, d.ID); gone != nil {
		t.Fatal("draft not deleted")
	}
}
