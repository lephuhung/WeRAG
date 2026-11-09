package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type autoRepo struct {
	queueRepo
	returned []string
}

func (r *autoRepo) NextAutoIngest(context.Context, int) ([]*types.LegalDocumentRequest, error) {
	var out []*types.LegalDocumentRequest
	for _, id := range []string{"ok", "pdf", "expired", "flaky", "nokb", "skipped"} {
		if row := r.rows[id]; row != nil && row.Status == types.LegalRequestAutoIngest {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *autoRepo) Claim(_ context.Context, id, user string, claim bool) (*types.LegalDocumentRequest, error) {
	row := r.rows[id]
	if claim {
		row.ClaimedBy = user
	} else {
		row.ClaimedBy = ""
	}
	return row, nil
}

func (r *autoRepo) ReturnToOperators(_ context.Context, id, source string) error {
	r.rows[id].Status, r.rows[id].Source = types.LegalRequestRequested, source
	r.returned = append(r.returned, id)
	return nil
}

func (r *autoRepo) Decide(_ context.Context, id, status, by, note, _, _ string) (*types.LegalDocumentRequest, error) {
	row := r.rows[id]
	row.Status, row.DecidedBy, row.DecisionNote, row.ClaimedBy = status, by, note, ""
	return row, nil
}

type autoTenants struct{ interfaces.TenantRepository }

func (autoTenants) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: id}, nil
}

type autoKBs struct {
	interfaces.KnowledgeBaseService
}

func (autoKBs) GetKnowledgeBaseByIDOnly(_ context.Context, id string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: id, TenantID: 10000}, nil
}

func TestLegalAutoIngestOutcomes(t *testing.T) {
	row := func(id, typ string) *types.LegalDocumentRequest {
		return &types.LegalDocumentRequest{ID: id, LegalDocumentID: id, DocType: typ, Status: types.LegalRequestAutoIngest}
	}
	repo := &autoRepo{queueRepo: queueRepo{rows: map[string]*types.LegalDocumentRequest{
		"ok": row("ok", "Nghị định"), "pdf": row("pdf", "Nghị định"), "expired": row("expired", "Nghị định"),
		"flaky": row("flaky", "Nghị định"), "nokb": row("nokb", "Công văn"),
	}}}
	var grantedKB, tenantSet bool
	w := &LegalAutoIngest{requests: repo, kbs: autoKBs{}, tenants: autoTenants{}, ingest: func(ctx context.Context, id string, kb *types.KnowledgeBase,
		_ LegalFeedActor, _ string) (*LegalIngestResult, error) {
		switch id {
		case "pdf":
			return nil, &LegalFeedRefusal{msg: "chỉ có PDF", PDFOnly: true, Portal: &LegalPortalAvailability{}}
		case "expired":
			return nil, refuse("fully expired")
		case "flaky":
			return nil, errors.New("portal 503")
		}
		grantedKB = ctx.Value(types.KBGrantsContextKey) != nil
		// CreateKnowledgeFromFile asserts this value and panics without it.
		tenant, _ := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
		tenantSet = tenant != nil && tenant.ID == kb.TenantID
		repo.rows[id].Status, repo.rows[id].ClaimedBy = types.LegalRequestAdded, "" // what MarkAddedByDocument does
		return &LegalIngestResult{Result: "created", KnowledgeID: "k"}, nil
	}}

	require.Equal(t, 1, w.Tick(context.Background()))
	require.True(t, grantedKB, "the ingest runs with a write grant on the KB")
	require.True(t, tenantSet, "the ingest runs with the KB owner's tenant info")
	require.Equal(t, types.LegalRequestAdded, repo.rows["ok"].Status)
	require.Equal(t, []string{"pdf"}, repo.returned, "no HTML goes back to the operators")
	require.Equal(t, types.LegalRequestSourceNoFullText, repo.rows["pdf"].Source)
	require.Equal(t, types.LegalRequestRejected, repo.rows["expired"].Status, "another refusal closes the row")
	require.Contains(t, repo.rows["expired"].DecisionNote, "fully expired")
	require.False(t, legalAutoIngestGivenUp(repo.rows["flaky"]), "a transient failure is retried")
	require.Contains(t, string(repo.rows["flaky"].Detail), types.LegalRequestDetailAutoIngestError)
	require.Equal(t, types.LegalRequestAutoIngest, repo.rows["nokb"].Status, "a type without a KB waits for one")
	require.False(t, legalAutoIngestGivenUp(repo.rows["nokb"]))
	for _, r := range repo.rows {
		require.Empty(t, r.ClaimedBy, "%s keeps no lease", r.ID)
	}
}

func TestLegalAutoIngestReplacesInTheKBHoldingTheCopy(t *testing.T) {
	row := func(id string) *types.LegalDocumentRequest {
		return &types.LegalDocumentRequest{ID: id, LegalDocumentID: id, DocType: "Công văn", KnowledgeBaseID: "kb-copy", Status: types.LegalRequestAutoIngest}
	}
	repo := &autoRepo{queueRepo: queueRepo{rows: map[string]*types.LegalDocumentRequest{"ok": row("ok"), "pdf": row("pdf")}}}
	var usedKB string
	w := &LegalAutoIngest{requests: repo, kbs: autoKBs{}, tenants: autoTenants{}, ingest: func(_ context.Context, id string, kb *types.KnowledgeBase,
		_ LegalFeedActor, _ string) (*LegalIngestResult, error) {
		if id == "pdf" {
			return nil, &LegalFeedRefusal{msg: "chỉ có PDF", PDFOnly: true, Portal: &LegalPortalAvailability{}}
		}
		usedKB = kb.ID
		repo.rows[id].Status, repo.rows[id].ClaimedBy = types.LegalRequestAdded, ""
		return &LegalIngestResult{Result: "replaced", KnowledgeID: "k"}, nil
	}}
	require.Equal(t, 1, w.Tick(context.Background()))
	require.Equal(t, "kb-copy", usedKB, "the KB of the copy, whatever the type's KB")
	require.Empty(t, repo.returned, "a refused update does not go to the operators")
	require.Equal(t, types.LegalRequestRejected, repo.rows["pdf"].Status)
}

func TestLegalAutoIngestSurvivesAPanic(t *testing.T) {
	repo := &autoRepo{queueRepo: queueRepo{rows: map[string]*types.LegalDocumentRequest{
		"ok": {ID: "ok", LegalDocumentID: "ok", DocType: "Nghị định", Status: types.LegalRequestAutoIngest},
	}}}
	w := &LegalAutoIngest{requests: repo, kbs: autoKBs{}, tenants: autoTenants{}, ingest: func(context.Context, string, *types.KnowledgeBase, LegalFeedActor, string) (*LegalIngestResult, error) {
		panic("boom")
	}}
	require.NotPanics(t, func() { w.Tick(context.Background()) })
}

func TestLegalAutoIngestBatchSkipsRowsGivenUp(t *testing.T) {
	done := &types.LegalDocumentRequest{ID: "skipped", LegalDocumentID: "skipped", DocType: "Nghị định", Status: types.LegalRequestAutoIngest,
		Detail: types.JSON(`{"auto_ingest_skip":"x"}`)}
	repo := &autoRepo{queueRepo: queueRepo{rows: map[string]*types.LegalDocumentRequest{"skipped": done}}}
	w := &LegalAutoIngest{requests: repo, kbs: autoKBs{}, ingest: func(context.Context, string, *types.KnowledgeBase, LegalFeedActor, string) (*LegalIngestResult, error) {
		t.Fatal("a row given up on is not tried again")
		return nil, nil
	}}
	require.Equal(t, 0, w.Tick(context.Background()))
}
