package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"
	"time"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type queueRepo struct {
	interfaces.LegalRequestRepository
	rows    map[string]*types.LegalDocumentRequest
	decided []string
}

func (r *queueRepo) Get(_ context.Context, id string) (*types.LegalDocumentRequest, error) {
	return r.rows[id], nil
}

func (r *queueRepo) List(_ context.Context, status string, _, _ int) ([]*types.LegalDocumentRequest, int64, error) {
	var out []*types.LegalDocumentRequest
	for _, row := range r.rows {
		if status == "" || row.Status == status {
			out = append(out, row)
		}
	}
	return out, int64(len(out)), nil
}

func (r *queueRepo) CountByStatus(context.Context) (map[string]int64, error) {
	return map[string]int64{"requested": int64(len(r.rows))}, nil
}

func (r *queueRepo) Decide(_ context.Context, id, status, by, note, kid, kbID string) (*types.LegalDocumentRequest, error) {
	row := r.rows[id]
	if row == nil {
		return nil, nil
	}
	now := time.Now()
	row.Status, row.DecidedBy, row.DecisionNote, row.KnowledgeID, row.KnowledgeBaseID, row.DecidedAt = status, by, note, kid, kbID, &now
	r.decided = append(r.decided, id+":"+status)
	return row, nil
}

func (r *queueRepo) PublicKnowledgeBases(context.Context) ([]types.LegalPublicKB, error) {
	return []types.LegalPublicKB{
		{ID: "kb-law", Name: "VBPL – Hiến pháp, Bộ luật, Luật, Pháp lệnh"},
		{ID: "kb-nd", Name: "VBPL – Nghị định"},
		{ID: "kb-ttlt", Name: "VBPL – Thông tư liên tịch"},
		{ID: "kb-tt", Name: "VBPL – Thông tư"},
		{ID: "kb-qd", Name: "VBPL – Quyết định, Chỉ thị"},
		{ID: "other", Name: "Kho khác"},
	}, nil
}

type queueCatalog struct {
	interfaces.LegalCatalogRepository
}

func (queueCatalog) GetByIDs(_ context.Context, ids []string) ([]*types.LegalDocument, error) {
	issued := time.Date(2022, 8, 15, 0, 0, 0, 0, time.UTC)
	return []*types.LegalDocument{{ID: "155366", DocumentNumber: "53/2022/NĐ-CP", DocType: "Nghị định",
		Title: "Quy định chi tiết", Issuer: "Chính phủ", Status: "Còn hiệu lực", IssuedOn: &issued}}, nil
}

func httpStatus(err error) int {
	var app *apperrors.AppError
	if stderrors.As(err, &app) {
		return app.HTTPCode
	}
	return 0
}

func TestLegalRequestServiceQueue(t *testing.T) {
	repo := &queueRepo{rows: map[string]*types.LegalDocumentRequest{
		"r1": {ID: "r1", DocumentNumber: "53/2022/NĐ-CP", LegalDocumentID: "155366", Status: types.LegalRequestRequested},
		"r2": {ID: "r2", DocumentNumber: "12/2024/QĐ-UBND", DocType: "Quyết định", Status: types.LegalRequestRequested},
	}}
	svc := NewLegalRequestService(repo, queueCatalog{}, nil, nil)
	ctx := context.Background()

	items, total, counts, err := svc.List(ctx, "all", 0, 50)
	if err != nil || total != 2 || counts["requested"] != 2 {
		t.Fatalf("list: %v %d %v", err, total, counts)
	}
	byID := map[string]*types.LegalRequestItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if d := byID["r1"].Document; d == nil || d.URL != "https://vbpl.vn/van-ban/chi-tiet/155366" || d.IssuedOn != "2022-08-15" ||
		byID["r1"].SuggestedKBID != "kb-nd" {
		t.Fatalf("r1 = %+v %+v", byID["r1"], d)
	}
	if byID["r2"].Document != nil || byID["r2"].SuggestedKBID != "kb-qd" {
		t.Fatalf("r2 = %+v", byID["r2"])
	}
	if _, _, _, err := svc.List(ctx, "bogus", 0, 10); httpStatus(err) != http.StatusBadRequest {
		t.Fatalf("bad status: %v", err)
	}

	// a number the catalogue does not know cannot be ingested from vbpl.vn
	if _, err := svc.Decide(ctx, "r2", &types.LegalRequestDecision{Action: "ingest", KnowledgeBaseID: "kb-qd"}); httpStatus(err) != http.StatusBadRequest {
		t.Fatalf("ingest without vbpl id: %v", err)
	}
	if _, err := svc.Decide(ctx, "r1", &types.LegalRequestDecision{Action: "ingest"}); httpStatus(err) != http.StatusBadRequest {
		t.Fatalf("ingest without KB: %v", err)
	}
	if _, err := svc.Decide(ctx, "nope", &types.LegalRequestDecision{Action: "reject"}); httpStatus(err) != http.StatusNotFound {
		t.Fatalf("missing: %v", err)
	}
	if _, err := svc.Decide(ctx, "r1", &types.LegalRequestDecision{Action: "drop"}); httpStatus(err) != http.StatusBadRequest {
		t.Fatalf("bad action: %v", err)
	}
	it, err := svc.Decide(ctx, "r2", &types.LegalRequestDecision{Action: "reject", Note: " Văn bản địa phương "})
	if err != nil || it.Status != types.LegalRequestRejected || it.DecisionNote != "Văn bản địa phương" {
		t.Fatalf("reject: %+v %v", it, err)
	}
	it, err = svc.Decide(ctx, "r1", &types.LegalRequestDecision{Action: "added", KnowledgeID: "k9", KnowledgeBaseID: "kb-nd"})
	if err != nil || it.Status != types.LegalRequestAdded || it.KnowledgeID != "k9" || it.Document == nil {
		t.Fatalf("added: %+v %v", it, err)
	}
}

func TestLegalTypeTargetsSuggestKBs(t *testing.T) {
	kbs, _ := (&queueRepo{}).PublicKnowledgeBases(context.Background())
	targets := LegalTypeTargets(kbs)
	for docType, want := range map[string]string{
		"Luật": "kb-law", "Bộ luật": "kb-law", "Hiến pháp": "kb-law", "Pháp lệnh": "kb-law",
		"Nghị định": "kb-nd", "Thông tư": "kb-tt", "Thông tư liên tịch": "kb-ttlt", "Chỉ thị": "kb-qd",
	} {
		if targets[docType].ID != want {
			t.Errorf("%s → %q, want %q", docType, targets[docType].ID, want)
		}
	}
}
