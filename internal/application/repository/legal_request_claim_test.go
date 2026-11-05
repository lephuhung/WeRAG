package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func seedRequest(t *testing.T, repo *legalRequestRepository) *types.LegalDocumentRequest {
	t.Helper()
	now := time.Now()
	row, err := repo.Record(context.Background(), &types.LegalDocumentRequest{
		NumberKey: "70/2025/ND-CP", DocumentNumber: "70/2025/NĐ-CP",
		Source: "feed:tvpl", Status: types.LegalRequestRequested,
		FirstRequestedAt: now, LastRequestedAt: now,
	})
	if err != nil || row == nil {
		t.Fatalf("seed: %v %v", row, err)
	}
	return row
}

func TestLegalRequestClaimLease(t *testing.T) {
	repo, db := newLegalRequestRepo(t)
	ctx := context.Background()
	row := seedRequest(t, repo)

	claimed, err := repo.Claim(ctx, row.ID, "u-1", true)
	if err != nil || claimed == nil {
		t.Fatalf("claim free lease: %v %v", claimed, err)
	}
	if claimed.ClaimedBy != "u-1" || claimed.ClaimedAt == nil {
		t.Fatalf("claim not recorded: %+v", claimed)
	}

	// Another operator cannot take it while the lease is live.
	if other, err := repo.Claim(ctx, row.ID, "u-2", true); err != nil || other != nil {
		t.Fatalf("second operator should be blocked, got %+v %v", other, err)
	}

	// The holder refreshing its own claim is fine.
	if again, err := repo.Claim(ctx, row.ID, "u-1", true); err != nil || again == nil {
		t.Fatalf("re-claim by holder: %v %v", again, err)
	}

	// An expired lease is free again.
	if err := db.Model(&types.LegalDocumentRequest{}).Where("id = ?", row.ID).
		Update("claimed_at", time.Now().Add(-2*types.LegalRequestClaimLease)).Error; err != nil {
		t.Fatal(err)
	}
	if taken, err := repo.Claim(ctx, row.ID, "u-2", true); err != nil || taken == nil || taken.ClaimedBy != "u-2" {
		t.Fatalf("expired lease should be claimable, got %+v %v", taken, err)
	}

	// Releasing someone else's lease does nothing.
	if rel, err := repo.Claim(ctx, row.ID, "u-1", false); err != nil || rel != nil {
		t.Fatalf("foreign release must not clear the lease, got %+v %v", rel, err)
	}
	if current, _ := repo.Get(ctx, row.ID); current == nil || current.ClaimedBy != "u-2" {
		t.Fatalf("lease changed by a foreign release: %+v", current)
	}

	// The holder releases its own lease.
	if rel, err := repo.Claim(ctx, row.ID, "u-2", false); err != nil || rel == nil || rel.ClaimedBy != "" {
		t.Fatalf("own release: %+v %v", rel, err)
	}
}

func TestLegalRequestDecisionClearsClaim(t *testing.T) {
	repo, _ := newLegalRequestRepo(t)
	ctx := context.Background()
	row := seedRequest(t, repo)
	if _, err := repo.Claim(ctx, row.ID, "u-1", true); err != nil {
		t.Fatal(err)
	}
	decided, err := repo.Decide(ctx, row.ID, types.LegalRequestAdded, "u-1", "nạp từ TVPL", "k1", "kb1")
	if err != nil || decided == nil {
		t.Fatalf("decide: %v %v", decided, err)
	}
	if decided.ClaimedBy != "" || decided.ClaimedAt != nil {
		t.Fatalf("decision must clear the lease: %+v", decided)
	}
}

func TestLegalRequestClaimRefusesClosedRequest(t *testing.T) {
	repo, _ := newLegalRequestRepo(t)
	ctx := context.Background()
	row := seedRequest(t, repo)

	// Someone finishes the queue entry, which clears the lease.
	if _, err := repo.Decide(ctx, row.ID, types.LegalRequestAdded, "u-1", "done", "k1", "kb1"); err != nil {
		t.Fatal(err)
	}
	// A stale queue view must not restart the pipeline on a closed request.
	if claimed, err := repo.Claim(ctx, row.ID, "u-2", true); err != nil || claimed != nil {
		t.Fatalf("closed request must not be claimable, got %+v %v", claimed, err)
	}
}

func TestLegalRequestMarkAddedClearsLease(t *testing.T) {
	repo, db := newLegalRequestRepo(t)
	ctx := context.Background()
	row := seedRequest(t, repo)
	if err := db.Model(&types.LegalDocumentRequest{}).Where("id = ?", row.ID).
		Update("legal_document_id", "155366").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Claim(ctx, row.ID, "u-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MarkAddedByDocument(ctx, "155366", "k1", "kb1", "feed:tvpl"); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(ctx, row.ID)
	if err != nil || stored == nil {
		t.Fatalf("get: %v %v", stored, err)
	}
	if stored.ClaimedBy != "" || stored.ClaimedAt != nil {
		t.Fatalf("ingest of the document must clear the lease: %+v", stored)
	}
}
