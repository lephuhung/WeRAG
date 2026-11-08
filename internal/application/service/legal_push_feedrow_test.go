package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

const nd777Push = `<html><head><title>Nghị định 777/2026/NĐ-CP quy định chi tiết về thuế hộ kinh doanh</title></head><body>
<p class="prov-article" id="tvpl-777-dieu:1">Điều 1. Phạm vi</p>
<p class="prov-clause" id="tvpl-777-dieu:1/khoan:1">1. Phạm vi điều chỉnh.</p>
</body></html>`

func legalNewDocMeta() map[string]string {
	return map[string]string{"source": "tvpl", "source_doc_id": "777", "source_url": "https://example.test/777",
		"format": vbpl.LegalHTMLFormatV1, "doc_number": "777/2026/NĐ-CP", "doc_type": "Nghị định",
		"issuer": "Chính phủ", "issue_date": "2026-11-05",
		"title": "Nghị định 777/2026/NĐ-CP quy định chi tiết về thuế hộ kinh doanh"}
}

func legalTestMeta(t *testing.T, m map[string]string) *legalPushMeta {
	meta, err := readLegalPushMeta(m)
	require.NoError(t, err)
	return meta
}

func TestLegalFeedRow(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.Local)
	m := legalNewDocMeta()
	row, missing := legalFeedRow(legalTestMeta(t, m), []byte(nd777Push), now)
	require.Empty(t, missing)
	require.Equal(t, "tvpl-777", row.ID)
	require.Equal(t, "tvpl", row.Source)
	require.True(t, row.IsFeedRow())
	require.True(t, row.Unverified())
	require.Equal(t, vietnamese_legal.CatalogNumberKey("777/2026/NĐ-CP"), row.NumberKey)
	require.Equal(t, legalFeedStatusUnknown, row.Status, "no status from the feed: never called in force")
	require.Equal(t, "2026-11-05", row.IssuedOn.Format("2006-01-02"))

	m["eff_from"] = "2026-12-01"
	row, _ = legalFeedRow(legalTestMeta(t, m), nil, now)
	require.Equal(t, legalFeedStatusPending, row.Status)
	require.Equal(t, "2026-12-01", row.EffectiveOn.Format("2006-01-02"))

	m["eff_from"] = "2026-01-01"
	row, _ = legalFeedRow(legalTestMeta(t, m), nil, now)
	require.Equal(t, legalFeedStatusUnknown, row.Status, "a past eff_from does not make it in force")

	m["eff_status"] = "Còn hiệu lực"
	row, _ = legalFeedRow(legalTestMeta(t, m), nil, now)
	require.Equal(t, "Còn hiệu lực", row.Status, "the feed's own status is kept")
	m["eff_status"] = "Hết hiệu lực"
	row, _ = legalFeedRow(legalTestMeta(t, m), nil, now)
	require.Equal(t, LegalExpiredStatusPrefix, row.Status, "TVPL's expiry reads as the portal's: never ingested")
	m["eff_status"] = "Đã biết"
	row, _ = legalFeedRow(legalTestMeta(t, m), nil, now)
	require.Equal(t, legalFeedStatusUnknown, row.Status, "TVPL's hidden-value placeholder is no status")

	// title from the HTML when the metadata has none; a bare number is no title
	delete(m, "title")
	row, _ = legalFeedRow(legalTestMeta(t, m), []byte(nd777Push), now)
	require.Equal(t, "Nghị định 777/2026/NĐ-CP quy định chi tiết về thuế hộ kinh doanh", row.Title)
	m["title"] = "Nghị định 777/2026/NĐ-CP"
	row, missing = legalFeedRow(legalTestMeta(t, m), nil, now)
	require.Nil(t, row)
	require.Contains(t, missing[0], "title")

	manual := legalNewDocMeta()
	manual["source"] = "manual"
	_, err := readLegalPushMeta(manual)
	require.Error(t, err, "a feed cannot pose as the reviewers' manual rows")

	bare := legalNewDocMeta()
	delete(bare, "issuer")
	delete(bare, "issue_date")
	_, missing = legalFeedRow(legalTestMeta(t, bare), nil, now)
	require.Len(t, missing, 2)

	require.Equal(t, "tvpl-123", legalFeedRowID("tvpl", "123"))
	long := legalFeedRowID("tvpl", string(make([]byte, 80)))
	require.LessOrEqual(t, len(long), 64)
}

func TestLegalPushResolveByNumber(t *testing.T) {
	svc := &LegalPushService{}
	day := func(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }
	twin := &types.LegalDocument{ID: "v1", Source: "vbpl", DocumentNumber: "777/2026/NĐ-CP", DocType: "Nghị định",
		Issuer: "Chính phủ", IssuedOn: day("2026-11-05"),
		Title: "Nghị định số 777/2026/NĐ-CP Quy định chi tiết về thuế hộ kinh doanh"}
	other := &types.LegalDocument{ID: "v2", Source: "vbpl", DocumentNumber: "777/2026/NĐ-CP", DocType: "Nghị định",
		Issuer: "Chính phủ", IssuedOn: day("2026-11-05"), Title: "Nghị định số 777/2026/NĐ-CP về quản lý cảng biển"}
	older := &types.LegalDocument{ID: "v3", Source: "vbpl", DocumentNumber: "777/2026/NĐ-CP", DocType: "Quyết định",
		Issuer: "UBND tỉnh Hà Giang", IssuedOn: day("2016-03-01"), Title: "Quyết định số 777 về giá đất"}
	meta := legalTestMeta(t, legalNewDocMeta())

	a, row, _ := svc.resolveByNumber(meta, nil, LegalPushAuto, nil)
	require.Nil(t, a)
	require.Equal(t, "tvpl-777", row.ID, "no row of the number: a new feed row")

	a, row, _ = svc.resolveByNumber(meta, nil, LegalPushAuto, []*types.LegalDocument{other, twin})
	require.Nil(t, row)
	require.Equal(t, "v1", a.ID, "the row that is the same document is A")

	a, row, why := svc.resolveByNumber(meta, nil, LegalPushAuto, []*types.LegalDocument{other})
	require.Nil(t, a)
	require.Nil(t, row)
	require.Contains(t, why, "gần giống", "same number, day and type, another title: a person decides")

	typo := *twin
	typo.ID, typo.IssuedOn = "v4", day("2026-11-15")
	_, row, why = svc.resolveByNumber(meta, nil, LegalPushAuto, []*types.LegalDocument{&typo})
	require.Nil(t, row, "same title and issuer on another day (a date typo): a person decides")
	require.Contains(t, why, "gần giống")

	a, row, _ = svc.resolveByNumber(meta, nil, LegalPushFull, []*types.LegalDocument{older})
	require.Nil(t, a)
	require.NotNil(t, row, "a document of another day reusing the number does not block a new row")

	_, row, why = svc.resolveByNumber(meta, nil, LegalPushLinks, nil)
	require.Nil(t, row)
	require.Contains(t, why, "links")

	withID := legalNewDocMeta()
	withID["legal_document_id"] = "missing-id"
	a, row, _ = svc.resolveByNumber(legalTestMeta(t, withID), nil, LegalPushAuto, nil)
	require.Nil(t, a)
	require.Nil(t, row, "an id names one row: never replaced by a new one")
}

// A new document reaches the ingest step: without an ingest service the
// push fails there and leaves no catalogue row behind.
func TestLegalPushNewDocumentLeavesNoRowOnFailure(t *testing.T) {
	svc, count := legalPushTestService(t)
	ctx := context.Background()
	_, err := svc.Push(ctx, LegalPushInput{Content: []byte(nd777Push), Metadata: legalNewDocMeta()},
		types.Caller{TenantID: types.PlatformTenantID}, LegalFeedActor{})
	var app *apperrors.AppError
	require.True(t, stderrors.As(err, &app), "%v", err)
	require.Equal(t, http.StatusServiceUnavailable, app.HTTPCode, "past resolution: the ingest step is reached")
	var n int64
	require.NoError(t, svc.db.Model(&types.LegalDocument{}).Where("id = ?", "tvpl-777").Count(&n).Error)
	require.Zero(t, n)
	require.EqualValues(t, 0, count())

	// missing metadata: refused at resolution, naming what is missing
	m := legalNewDocMeta()
	delete(m, "issue_date")
	_, err = svc.Push(ctx, LegalPushInput{Content: []byte(nd777Push), Metadata: m}, types.Caller{}, LegalFeedActor{})
	require.True(t, stderrors.As(err, &app))
	require.Equal(t, http.StatusUnprocessableEntity, app.HTTPCode)
	require.Contains(t, app.Message, "issue_date")
}

func TestLegalFeedRowInsertAndDrop(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	row, _ := legalFeedRow(legalTestMeta(t, legalNewDocMeta()), nil, time.Now())
	now := time.Now()
	open := &types.LegalDocumentRequest{NumberKey: row.NumberKey, DocumentNumber: row.DocumentNumber, Source: "feed:tvpl",
		Status: types.LegalRequestRequested, FirstRequestedAt: now, LastRequestedAt: now}
	require.NoError(t, svc.db.Create(open).Error)

	created, err := svc.insertFeedRow(ctx, row)
	require.NoError(t, err)
	require.True(t, created)
	var got types.LegalDocumentRequest
	require.NoError(t, svc.db.First(&got, open.ID).Error)
	require.Equal(t, row.ID, got.LegalDocumentID, "the open request of the number is attached to the new row")

	again := *row
	created, err = svc.insertFeedRow(ctx, &again)
	require.NoError(t, err)
	require.False(t, created, "a second push of the same document finds the row")

	clash := *row
	clash.NumberKey = "OTHER"
	_, err = svc.insertFeedRow(ctx, &clash)
	require.Error(t, err)

	svc.dropFeedRow(ctx, row)
	var n int64
	require.NoError(t, svc.db.Model(&types.LegalDocument{}).Where("id = ?", row.ID).Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, svc.db.First(&got, open.ID).Error)
	require.Empty(t, got.LegalDocumentID, "the request is detached again")
}

// legalPushKBs serves the public KB of one type to the ingest step.
type legalPushKBs struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (k legalPushKBs) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return k.kb, nil
}

// The row is inserted only once the KB write is allowed, and removed again
// when the ingest itself fails (here: no knowledge service).
func TestLegalPushNewDocumentRollsBackOnIngestFailure(t *testing.T) {
	svc, _ := legalPushTestService(t)
	kb := &types.KnowledgeBase{ID: "kb-nd", Name: LegalKBNamePrefix + "Nghị định"}
	require.NoError(t, svc.db.Create(kb).Error)
	svc.kbs = legalPushKBs{kb: kb}
	svc.feed = &LegalFeedService{db: svc.db}
	caller := types.Caller{TenantID: types.PlatformTenantID, UserID: "sa-user"}
	ctx := context.WithValue(types.WithCaller(context.Background(), caller), types.SystemAdminContextKey, true)

	_, err := svc.Push(ctx, LegalPushInput{Content: []byte(nd777Push), Metadata: legalNewDocMeta()}, caller, LegalFeedActor{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "knowledge service is not available")
	var n int64
	require.NoError(t, svc.db.Model(&types.LegalDocument{}).Where("id = ?", "tvpl-777").Count(&n).Error)
	require.Zero(t, n, "a failed ingest leaves no catalogue row")

	// an expired status from the feed is refused before any row is made
	m := legalNewDocMeta()
	m["eff_status"] = "Hết hiệu lực"
	_, err = svc.Push(ctx, LegalPushInput{Content: []byte(nd777Push), Metadata: m}, caller, LegalFeedActor{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "hết hiệu lực toàn bộ")
	require.NoError(t, svc.db.Model(&types.LegalDocument{}).Where("id = ?", "tvpl-777").Count(&n).Error)
	require.Zero(t, n)
}
