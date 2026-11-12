package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// legalLinkReviewPush pushes nd68Push: NĐ 999 (dieu:2 on khoản 1 Điều 8)
// is not in the catalogue, so its candidate waits unresolved.
func legalLinkReviewPush(t *testing.T, svc *LegalPushService) {
	t.Helper()
	_, err := svc.Push(context.Background(), LegalPushInput{Content: []byte(nd68Push), Metadata: legalPushMetaFor("68/2026/NĐ-CP")},
		types.Caller{TenantID: types.PlatformTenantID}, LegalFeedActor{})
	require.NoError(t, err)
}

func legalLinkUnresolved(t *testing.T, svc *LegalPushService) *types.LegalProvisionLinkCandidate {
	t.Helper()
	var c types.LegalProvisionLinkCandidate
	require.NoError(t, svc.db.Where("amender_doc_number = ?", "999/2026/NĐ-CP").First(&c).Error)
	return &c
}

// legalLinkAddNd999 adds NĐ 999 to the catalogue; held, a public KB copy
// with its Điều 2.
func legalLinkAddNd999(t *testing.T, svc *LegalPushService, id string, held bool) {
	t.Helper()
	require.NoError(t, repository.NewLegalCatalogRepository(svc.db).UpsertDocuments(context.Background(), []*types.LegalDocument{
		{ID: id, Source: "tvpl", DocumentNumber: "999/2026/NĐ-CP", NumberKey: vietnamese_legal.CatalogNumberKey("999/2026/NĐ-CP"),
			DocType: "Nghị định", Title: "Nghị định 999/2026/NĐ-CP"},
	}))
	if !held {
		return
	}
	meta, _ := json.Marshal(map[string]string{"doc_number": "999/2026/NĐ-CP", "external_id": id, "source": "tvpl"})
	require.NoError(t, svc.db.Create(&types.Knowledge{ID: "k-" + id, KnowledgeBaseID: "kb-pub", Title: "999/2026/NĐ-CP",
		ParseStatus: types.ParseStatusCompleted, Metadata: meta}).Error)
	legal := vietnamese_legal.LegalMetadata{Source: "tvpl", ArticleNos: []string{"2"}, SourceDocID: id}
	cm, _ := json.Marshal(types.DocumentChunkMetadata{Legal: &legal})
	require.NoError(t, svc.db.Create(&types.Chunk{ID: "c-" + id, KnowledgeID: "k-" + id, KnowledgeBaseID: "kb-pub", ChunkIndex: 1,
		ChunkType: types.ChunkTypeText, IsEnabled: true, Content: "#### Điều 2. Sửa đổi khoản 1 Điều 8\n\nKhoản 1 Điều 8 được sửa đổi.",
		Metadata: cm}).Error)
}

// A candidate pushed while its amender was unknown is resolved once the
// catalogue has the document: matched both ends, it is approved under the
// push's setting, its mark key names the amender, and a second recheck
// finds nothing left to do.
func TestLegalLinkRecheckResolvesAndAutoApproves(t *testing.T) {
	svc, _ := legalPushTestService(t)
	svc.settings = legalPushSettings{on: true}
	ctx := context.Background()
	legalLinkReviewPush(t, svc)
	before := legalLinkUnresolved(t, svc)
	require.Equal(t, types.LegalLinkUnresolved, before.MatchStatus)
	require.Empty(t, before.AmenderDocID)

	res, err := svc.RecheckCandidates(ctx, "")
	require.NoError(t, err)
	require.Equal(t, LegalLinkRecheckResult{Checked: 1, StillMissing: 1}, *res, "not in the catalogue yet")

	legalLinkAddNd999(t, svc, "nd999", true)
	res, err = svc.RecheckCandidates(ctx, "nd68")
	require.NoError(t, err)
	require.Equal(t, LegalLinkRecheckResult{Checked: 1, Resolved: 1, Matched: 1, AutoApproved: 1}, *res)

	after := legalLinkUnresolved(t, svc)
	require.Equal(t, "nd999", after.AmenderDocID)
	require.Equal(t, types.LegalLinkMatched, after.MatchStatus)
	require.Equal(t, types.LegalLinkReviewApproved, after.ReviewStatus)
	require.Equal(t, types.LegalLinkAutoReviewer, after.ReviewedBy)
	require.NotNil(t, after.LinkID)
	require.Contains(t, legalMarkKey(after), "|nd999|dieu:2", "the next push recognizes the decided mark")
	var link types.LegalProvisionLink
	require.NoError(t, svc.db.First(&link, *after.LinkID).Error)
	require.Equal(t, "tvpl", link.Source)
	require.Equal(t, "nd999", link.SourceDocID)

	res, err = svc.RecheckCandidates(ctx, "")
	require.NoError(t, err)
	require.Equal(t, LegalLinkRecheckResult{}, *res, "idempotent")

	// the next push of A keeps the decision instead of asking again
	legalLinkReviewPush(t, svc)
	var n int64
	require.NoError(t, svc.db.Model(&types.LegalProvisionLinkCandidate{}).
		Where("amender_doc_number = ? AND review_status = ?", "999/2026/NĐ-CP", types.LegalLinkReviewPending).Count(&n).Error)
	require.Zero(t, n)
}

// With the setting off, or B held without the provision, the candidate is
// resolved but left to a person; a decided candidate is never touched.
func TestLegalLinkRecheckLeavesDecisionsAndRespectsSetting(t *testing.T) {
	svc, _ := legalPushTestService(t)
	svc.settings = legalPushSettings{on: false}
	ctx := context.Background()
	legalLinkReviewPush(t, svc)
	legalLinkAddNd999(t, svc, "nd999", true)
	res, err := svc.RecheckCandidates(ctx, "")
	require.NoError(t, err)
	require.Equal(t, LegalLinkRecheckResult{Checked: 1, Resolved: 1, Matched: 1}, *res)
	c := legalLinkUnresolved(t, svc)
	require.Equal(t, types.LegalLinkReviewPending, c.ReviewStatus)
	require.Equal(t, types.LegalLinkMatched, c.MatchStatus)

	// a rejected unresolved candidate stays as it is
	svc2, _ := legalPushTestService(t)
	svc2.settings = legalPushSettings{on: true}
	legalLinkReviewPush(t, svc2)
	u := legalLinkUnresolved(t, svc2)
	_, err = svc2.DecideCandidate(ctx, u.ID, LegalLinkDecision{Action: "reject"}, types.Caller{UserID: "sa"})
	require.NoError(t, err)
	legalLinkAddNd999(t, svc2, "nd999", false)
	res, err = svc2.RecheckCandidates(ctx, "")
	require.NoError(t, err)
	require.Equal(t, 0, res.Checked)
	u = legalLinkUnresolved(t, svc2)
	require.Equal(t, types.LegalLinkReviewRejected, u.ReviewStatus)
	require.Empty(t, u.AmenderDocID)
}

// Two catalogue documents with the number and no issue date in the mark:
// never guessed.
func TestLegalLinkRecheckNeverGuessesBetweenDocuments(t *testing.T) {
	svc, _ := legalPushTestService(t)
	svc.settings = legalPushSettings{on: true}
	legalLinkReviewPush(t, svc)
	legalLinkAddNd999(t, svc, "nd999a", false)
	legalLinkAddNd999(t, svc, "nd999b", false)
	res, err := svc.RecheckCandidates(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, LegalLinkRecheckResult{Checked: 1, Ambiguous: 1}, *res)
	require.Empty(t, legalLinkUnresolved(t, svc).AmenderDocID)
}

// A push that stores B (here: mode catalogue on a row the catalogue has)
// resolves the candidates naming B at once; B not held → doc_only, pending.
func TestLegalLinkPushOfAmenderRechecks(t *testing.T) {
	svc, _ := legalPushTestService(t)
	svc.settings = legalPushSettings{on: true}
	ctx := context.Background()
	legalLinkReviewPush(t, svc)
	legalLinkAddNd999(t, svc, "nd999", false)
	_, err := svc.Push(ctx, LegalPushInput{Mode: LegalPushCatalogue, Metadata: map[string]string{
		"source": "tvpl", "source_doc_id": "999", "source_url": "https://example.test/999", "format": vbpl.LegalHTMLFormatV1,
		"doc_number": "999/2026/NĐ-CP", "doc_type": "Nghị định",
	}}, types.Caller{TenantID: types.PlatformTenantID}, LegalFeedActor{})
	require.NoError(t, err)
	c := legalLinkUnresolved(t, svc)
	require.Equal(t, "nd999", c.AmenderDocID)
	require.Equal(t, types.LegalLinkDocOnly, c.MatchStatus, "B has no full text: its provision is not checked")
	require.Equal(t, types.LegalLinkReviewPending, c.ReviewStatus)
}

func TestLegalLinkBatchDecide(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	legalLinkReviewPush(t, svc)
	var pending []types.LegalProvisionLinkCandidate
	require.NoError(t, svc.db.Where("review_status = ?", types.LegalLinkReviewPending).Order("id").Find(&pending).Error)
	require.Len(t, pending, 4)
	ids := []int64{999999}
	var unresolved int64
	for _, c := range pending {
		ids = append(ids, c.ID)
		if c.AmenderDocID == "" {
			unresolved = c.ID
		}
	}
	ids = append(ids, pending[0].ID) // repeated: decided once
	res, err := svc.DecideCandidates(ctx, LegalLinkBatchDecision{IDs: ids, Action: "approve"}, types.Caller{UserID: "sa"})
	require.NoError(t, err)
	require.Len(t, res.Done, 3)
	require.Len(t, res.Failed, 2, "the unknown id and the unresolved candidate")
	failed := map[int64]string{}
	for _, f := range res.Failed {
		failed[f.ID] = f.Error
	}
	require.Contains(t, failed, int64(999999))
	require.Contains(t, failed[unresolved], "văn bản tác động")

	// a second batch: the decided ones are reported, not decided again
	res, err = svc.DecideCandidates(ctx, LegalLinkBatchDecision{IDs: []int64{pending[0].ID}, Action: "reject"}, types.Caller{UserID: "sa"})
	require.NoError(t, err)
	require.Empty(t, res.Done)
	require.Len(t, res.Failed, 1)

	_, err = svc.DecideCandidates(ctx, LegalLinkBatchDecision{IDs: make([]int64, LegalLinkBatchMax+1), Action: "approve"}, types.Caller{})
	require.Error(t, err)
	_, err = svc.DecideCandidates(ctx, LegalLinkBatchDecision{IDs: []int64{1}, Action: "maybe"}, types.Caller{})
	require.Error(t, err)
}

func TestLegalLinkCandidateDocuments(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	legalLinkReviewPush(t, svc)
	groups, totals, err := svc.ListCandidateDocuments(ctx, "")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	g := groups[0]
	require.Equal(t, "nd68", g.TargetDocID)
	require.True(t, g.Held)
	require.Equal(t, "k68", g.KnowledgeID)
	require.Equal(t, "68/2026/NĐ-CP", g.Document.DocumentNumber)
	require.EqualValues(t, 3, g.PendingApprovable, "matched, doc_only, target_missing")
	require.EqualValues(t, 1, g.PendingBlocked, "NĐ 999 is not in the catalogue")
	require.EqualValues(t, 1, g.Auto, "the duplicate")
	require.Equal(t, LegalLinkTargetTotals{PendingApprovable: 3, PendingBlocked: 1}, totals)

	// the unresolved item carries the request the push filed for NĐ 999
	items, _, _, err := svc.ListCandidates(ctx, LegalLinkCandidateFilter{MatchStatus: types.LegalLinkUnresolved}, 0, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].AmenderRequest)
	require.Equal(t, types.LegalRequestRequested, items[0].AmenderRequest.Status)

	none, _, err := svc.ListCandidateDocuments(ctx, "other-feed")
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestLegalProvisionText(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()

	// B's khoản 2 from its held copy, the quoted text included
	got, err := svc.ProvisionText(ctx, "nd141", "dieu:1/khoan:2")
	require.NoError(t, err)
	require.True(t, got.Held)
	require.True(t, got.Found)
	require.Equal(t, "k141", got.KnowledgeID)
	require.True(t, strings.HasPrefix(got.Text, "2. Sửa đổi, bổ sung khoản 5 Điều 8"), got.Text)
	require.Contains(t, got.Text, "hóa đơn điện tử")
	require.NotContains(t, got.Text, "3. Bổ sung điểm c", "stops at the next khoản")
	require.Equal(t, "141/2026/NĐ-CP", got.Document.DocumentNumber)

	// a điểm of A
	got, err = svc.ProvisionText(ctx, "nd68", "dieu:8/khoan:2/diem:a")
	require.NoError(t, err)
	require.True(t, got.Found)
	require.Equal(t, "a) tờ khai;", got.Text)

	// the khoản A's copy lacks
	got, err = svc.ProvisionText(ctx, "nd68", "dieu:8/khoan:9")
	require.NoError(t, err)
	require.True(t, got.Held)
	require.False(t, got.Found)
	require.Equal(t, LegalTextNotFound, got.Reason)

	// in a tenant KB only: not held
	got, err = svc.ProvisionText(ctx, "nd200", "dieu:1")
	require.NoError(t, err)
	require.False(t, got.Held)
	require.Equal(t, LegalTextNotHeld, got.Reason)
	require.Equal(t, "200/2026/NĐ-CP", got.Document.DocumentNumber)

	for path, reason := range map[string]string{"": LegalTextNoPath, "phuluc/dieu:1": LegalTextAppendix} {
		got, err = svc.ProvisionText(ctx, "nd141", path)
		require.NoError(t, err)
		require.Equal(t, reason, got.Reason, path)
	}
	got, err = svc.ProvisionText(ctx, "nope", "dieu:1")
	require.NoError(t, err)
	require.Equal(t, LegalTextUnknownDoc, got.Reason)
	_, err = svc.ProvisionText(ctx, "", "dieu:1")
	require.Error(t, err)
}

func TestLegalProvisionTextTruncates(t *testing.T) {
	svc, _ := legalPushTestService(t)
	legalLinkAddNd999(t, svc, "nd999", true)
	long := "#### Điều 2. Dài\n\n" + strings.Repeat("chữ ", legalProvisionTextMaxRunes)
	require.NoError(t, svc.db.Model(&types.Chunk{}).Where("id = ?", "c-nd999").Update("content", long).Error)
	got, err := svc.ProvisionText(context.Background(), "nd999", "dieu:2")
	require.NoError(t, err)
	require.True(t, got.Found)
	require.True(t, got.Truncated)
	require.LessOrEqual(t, len([]rune(got.Text)), legalProvisionTextMaxRunes+1)
}

func TestLegalHeldProvisionList(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	got, err := svc.HeldProvisionList(ctx, "nd68")
	require.NoError(t, err)
	require.True(t, got.Held)
	var paths []string
	for _, p := range got.Provisions {
		paths = append(paths, p.Path)
	}
	require.Equal(t, []string{"dieu:3", "dieu:3/khoan:1", "dieu:3/khoan:2", "dieu:8", "dieu:8/khoan:1", "dieu:8/khoan:2",
		"dieu:8/khoan:2/diem:a", "dieu:8/khoan:2/diem:b", "dieu:8/khoan:5", "dieu:8/khoan:5/diem:a"}, paths)
	require.Equal(t, "điểm a khoản 2 Điều 8", got.Provisions[6].Label)

	got, err = svc.HeldProvisionList(ctx, "nd200")
	require.NoError(t, err)
	require.False(t, got.Held)
	require.Empty(t, got.Provisions)
}
