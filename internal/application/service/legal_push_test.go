package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vbpl"
)

// nd68Push is NĐ 68 as a feed pushes it: Điều 3 and 8 with marks.
const nd68Push = `<html><body>
<p class="prov-article" id="tv-68-dieu:3">Điều 3. Đối tượng</p>
<p class="prov-clause" id="tv-68-dieu:3/khoan:1"
   data-amend='[{"op":"sua_doi_bo_sung","doc_number":"150/2026/NĐ-CP","path":"dieu:1/khoan:1","note":"sửa bởi khoản 1 Điều 1 NĐ 150"}]'>1. Hộ kinh doanh.</p>
<p class="prov-clause" id="tv-68-dieu:3/khoan:2"
   data-amend='[{"op":"bai_bo","doc_number":"141/2026/NĐ-CP","issue_date":"2026-04-29","path":"dieu:1/khoan:2","eff_from":"2026-01-01"}]'>2. Cá nhân.</p>
<p class="prov-article" id="tv-68-dieu:8">Điều 8. Hóa đơn</p>
<p class="prov-clause" id="tv-68-dieu:8/khoan:1"
   data-amend='[{"op":"thay_the","doc_number":"141/2026/NĐ-CP"},{"op":"sua_doi","doc_number":"999/2026/NĐ-CP","path":"dieu:2"}]'>1. Doanh thu.</p>
<p class="prov-clause" id="tv-68-dieu:8/khoan:9"
   data-amend='[{"op":"bo_sung","doc_number":"150/2026/NĐ-CP","path":"dieu:1/khoan:7"}]'>9. Khoản không có trong bản đang có.</p>
<p class="prov-content" id="tv-68-c1" data-amend='[{"op":"giai_thich","doc_number":"150/2026/NĐ-CP"}]'>Nội dung.</p>
</body></html>`

func legalPushTestService(t *testing.T) (*LegalPushService, func() int64) {
	db := legalEditorTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.LegalProvisionLinkCandidate{}, &types.LegalDocumentRequest{}))
	svc := NewLegalPushService(db, nil, nil, repository.NewLegalRequestRepository(db))
	count := func() int64 {
		var n int64
		require.NoError(t, db.Model(&types.LegalProvisionLinkCandidate{}).Count(&n).Error)
		return n
	}
	return svc, count
}

func legalPushMetaFor(docNumber string) map[string]string {
	return map[string]string{"source": "tvpl", "source_doc_id": "68", "source_url": "https://example.test/68",
		"format": vbpl.LegalHTMLFormatV1, "doc_number": docNumber, "doc_type": "Nghị định"}
}

func TestLegalPushLinks(t *testing.T) {
	svc, count := legalPushTestService(t)
	ctx := context.Background()
	caller := types.Caller{TenantID: types.PlatformTenantID}
	res, err := svc.Push(ctx, LegalPushInput{Content: []byte(nd68Push), Metadata: legalPushMetaFor("68/2026/NĐ-CP")}, caller, LegalFeedActor{})
	require.NoError(t, err)
	require.Equal(t, "nd68", res.LegalDocumentID)
	require.Equal(t, LegalPushLinks, res.Mode, "A is held: auto → links")
	require.Equal(t, "k68", res.KnowledgeID)
	// khoản 1 Điều 3 / NĐ 150 is an editor link already: duplicate;
	// khoản 2 Điều 3 / NĐ 141: matched on B's provision id;
	// khoản 1 Điều 8: NĐ 141 without path → doc_only, NĐ 999 → unresolved;
	// khoản 9 Điều 8: not in A's copy → target_missing
	require.Equal(t, map[string]int{"duplicate": 1, "matched": 1, "doc_only": 1, "unresolved": 1, "target_missing": 1}, res.Links)
	require.Equal(t, 4, res.Pending)
	require.EqualValues(t, 5, count())
	var problemOnContent bool
	for _, p := range res.Problems {
		if p.ElementID == "tv-68-c1" {
			problemOnContent = true
		}
	}
	require.True(t, problemOnContent, "a mark on prov-content is reported")

	// mark_results resolves EVERY mark, including the good outcomes, so a
	// client that draws the link graph does not see a matched mark as unknown.
	statuses := map[string]bool{}
	for _, m := range res.MarkResults {
		statuses[m.Status] = true
	}
	for _, want := range []string{"matched", "duplicate", "doc_only", "unresolved", "target_missing"} {
		require.True(t, statuses[want], "mark_results must carry status %q", want)
	}
	for _, m := range res.MarkResults {
		require.NotEmpty(t, m.ElementID, "every mark names its element")
	}

	items, total, counts, err := svc.ListCandidates(ctx, LegalLinkCandidateFilter{MatchStatus: "matched"}, 0, 50)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.EqualValues(t, 1, counts["pending"], "counts follow the match filter")
	m := items[0]
	require.Equal(t, "p141-2", m.SourceProvisionID, "B's vbpl provision id at dieu:1/khoan:2")
	require.Equal(t, "nd141", m.AmenderDocID)
	require.Equal(t, "dieu:3/khoan:2", m.TargetPath)
	require.NotNil(t, m.EffFrom)
	require.Equal(t, "141/2026/NĐ-CP", m.Amender.DocumentNumber)
	require.Len(t, m.Others, 1, "the editors' bãi bỏ by NĐ 150 on the same khoản is shown")

	// the unknown NĐ 999 is filed as a legal document request by the feed
	reqs, err := svc.requests.ListByNumberKey(ctx, "999/2026/nđ-cp")
	require.NoError(t, err)
	if len(reqs) == 0 { // number keys fold case differently: look it up by list
		all, _, err := svc.requests.List(ctx, "", 0, 10)
		require.NoError(t, err)
		reqs = all
	}
	require.Len(t, reqs, 1)
	require.Equal(t, "feed:tvpl", reqs[0].Source)

	// approve the matched one: copied into legal_provision_links as tvpl
	got, err := svc.DecideCandidate(ctx, m.ID, LegalLinkDecision{Action: "approve"}, types.Caller{UserID: "sa"})
	require.NoError(t, err)
	require.Equal(t, types.LegalLinkReviewApproved, got.ReviewStatus)
	require.NotNil(t, got.LinkID)
	var link types.LegalProvisionLink
	require.NoError(t, svc.db.First(&link, *got.LinkID).Error)
	require.Equal(t, "tvpl", link.Source)
	require.Equal(t, "nd141", link.SourceDocID)
	require.Equal(t, "p141-2", link.SourceProvisionID)
	require.Equal(t, "dieu:3/khoan:2", link.TargetPath)
	require.Equal(t, 1, link.RelationCode)

	// the unresolved one cannot be approved until B is chosen; with B and a
	// path it can
	unres, _, _, err := svc.ListCandidates(ctx, LegalLinkCandidateFilter{MatchStatus: "unresolved"}, 0, 50)
	require.NoError(t, err)
	_, err = svc.DecideCandidate(ctx, unres[0].ID, LegalLinkDecision{Action: "approve"}, types.Caller{UserID: "sa"})
	require.Error(t, err)
	got, err = svc.DecideCandidate(ctx, unres[0].ID, LegalLinkDecision{Action: "approve", AmenderDocID: "nd150", SourcePath: "dieu:1/khoan:3"},
		types.Caller{UserID: "sa"})
	require.NoError(t, err)
	require.Equal(t, types.LegalLinkMatched, got.MatchStatus, "the status is recomputed after the correction")
	var link2 types.LegalProvisionLink
	require.NoError(t, svc.db.First(&link2, *got.LinkID).Error)
	require.Equal(t, "p150-3", link2.SourceProvisionID)
	require.Equal(t, 16, link2.RelationCode)

	// reject one; a second push replaces the pending ones and keeps the decisions
	tm, _, _, err := svc.ListCandidates(ctx, LegalLinkCandidateFilter{MatchStatus: "target_missing"}, 0, 50)
	require.NoError(t, err)
	_, err = svc.DecideCandidate(ctx, tm[0].ID, LegalLinkDecision{Action: "reject", Note: "khoản 9 không có"}, types.Caller{UserID: "sa"})
	require.NoError(t, err)
	_, err = svc.DecideCandidate(ctx, tm[0].ID, LegalLinkDecision{Action: "approve"}, types.Caller{UserID: "sa"})
	require.Error(t, err, "a decided candidate is not decided again")

	res, err = svc.Push(ctx, LegalPushInput{Content: []byte(nd68Push), Metadata: legalPushMetaFor("68/2026/NĐ-CP")}, caller, LegalFeedActor{})
	require.NoError(t, err)
	require.Equal(t, 3, res.KeptDecisions)
	require.Equal(t, 1, res.Pending, "only the doc_only mark is still asked (the duplicate needs no review)")
	require.EqualValues(t, 5, count(), "3 decided + doc_only + duplicate")
}

func TestLegalPushRefusals(t *testing.T) {
	svc, count := legalPushTestService(t)
	ctx := context.Background()
	push := func(meta map[string]string, mode, content string) error {
		_, err := svc.Push(ctx, LegalPushInput{Content: []byte(content), Metadata: meta, Mode: mode}, types.Caller{}, LegalFeedActor{})
		return err
	}
	status := func(err error) int {
		var app *apperrors.AppError
		require.True(t, stderrors.As(err, &app), "%v", err)
		return app.HTTPCode
	}
	meta := legalPushMetaFor("68/2026/NĐ-CP")

	bad := map[string]string{}
	for k, v := range meta {
		bad[k] = v
	}
	bad["format"] = "html"
	require.Equal(t, http.StatusBadRequest, status(push(bad, "", nd68Push)))
	bad["format"], bad["source"] = vbpl.LegalHTMLFormatV1, "vbpl"
	require.Equal(t, http.StatusBadRequest, status(push(bad, "", nd68Push)))

	// unknown A
	require.Equal(t, http.StatusUnprocessableEntity, status(push(legalPushMetaFor("1/2000/NĐ-CP"), "", nd68Push)))
	// no prov-* structure
	require.Equal(t, http.StatusUnprocessableEntity, status(push(meta, "", "<html><body><p>Điều 1. A</p></body></html>")))
	// full while A is held
	require.Equal(t, http.StatusConflict, status(push(meta, LegalPushFull, nd68Push)))
	require.EqualValues(t, 0, count())
}

func TestLegalLinkSourceFilter(t *testing.T) {
	ed := types.LegalProvisionLink{Source: types.LegalProvisionLinkSourceEditor}
	tv := types.LegalProvisionLink{Source: "tvpl"}
	require.True(t, legalLinkWanted(ed, LegalAmendLoadOptions{EditorSource: true}))
	require.False(t, legalLinkWanted(tv, LegalAmendLoadOptions{EditorSource: true}))
	require.True(t, legalLinkWanted(tv, LegalAmendLoadOptions{FeedSource: true}))
	require.False(t, legalLinkWanted(ed, LegalAmendLoadOptions{FeedSource: true}))
	require.True(t, legalHasTagSource([]string{LegalAmendSourceParser, "tvpl"}))
	require.False(t, legalHasTagSource([]string{LegalAmendSourceParser, LegalAmendSourceCatalogue}))
}

// links on an A no public KB holds (NĐ 200 sits in a tenant KB): the
// positions are the pushed file's; nothing panics, nothing is ingested.
func TestLegalPushLinksUnheld(t *testing.T) {
	svc, count := legalPushTestService(t)
	ctx := context.Background()
	long := strings.Repeat("x", 70)
	content := `<html><body>
<p class="prov-article" id="tv-200-dieu:1">Điều 1. Phạm vi</p>
<p class="prov-clause" id="tv-200-dieu:1/khoan:1"
   data-amend='[{"op":"sua_doi","doc_number":"150/2026/NĐ-CP","path":"dieu:1/khoan:2"}]'>1. Phạm vi.</p>
<p class="prov-clause" id="` + long + `" data-amend='[{"op":"bai_bo","doc_number":"150/2026/NĐ-CP"}]'>2. Khác.</p>
</body></html>`
	meta := legalPushMetaFor("200/2026/NĐ-CP")
	meta["source_doc_id"] = "200"
	res, err := svc.Push(ctx, LegalPushInput{Content: []byte(content), Metadata: meta, Mode: LegalPushLinks},
		types.Caller{TenantID: types.PlatformTenantID}, LegalFeedActor{})
	require.NoError(t, err)
	require.Equal(t, LegalPushLinks, res.Mode)
	require.Empty(t, res.KnowledgeID)
	require.Equal(t, map[string]int{"matched": 1}, res.Links)
	require.EqualValues(t, 1, count())
	var tooLong bool
	for _, p := range res.Problems {
		tooLong = tooLong || strings.Contains(p.Reason, "dài quá")
	}
	require.True(t, tooLong, "an element id longer than 64 characters is reported, not stored")
	items, _, _, err := svc.ListCandidates(ctx, LegalLinkCandidateFilter{}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, "p150-2", items[0].SourceProvisionID)
	require.True(t, strings.HasPrefix(items[0].TargetProvisionID, "fp-"))
}

func TestLegalHeldProvisionsDiemUnderDieu(t *testing.T) {
	h := &legalHeldProvisions{ids: map[string]string{}, structure: LegalDocStructure{LegalProvisionRef{Article: "5"}.key(): true}}
	require.True(t, h.has("dieu:5/diem:a"))
	require.False(t, h.has("dieu:6/diem:a"))
	require.False(t, h.has("dieu:5/khoan:9"))
	require.Equal(t, "item", legalPathLevel("dieu:5/khoan:2/diem:a"))
	require.Equal(t, "clause", legalPathLevel("dieu:5/khoan:2"))
	require.Equal(t, "article", legalPathLevel("dieu:5"))
}
