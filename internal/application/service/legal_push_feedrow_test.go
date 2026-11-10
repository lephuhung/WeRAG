package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"
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

// kl114Push is a Party Kết luận: numbered paragraphs, no Điều.
var kl114Push = `<html><head><title>Kết luận số 114-KL/TW</title></head><body>
<p>BAN CHẤP HÀNH TRUNG ƯƠNG</p><p>KẾT LUẬN của Bộ Chính trị về tiếp tục hoàn thiện bộ máy quản lý nhà nước về an toàn thực phẩm</p>
<p>1. Thống nhất một đầu mối quản lý nhà nước về an toàn thực phẩm từ Trung ương đến địa phương, bảo đảm hiệu lực, hiệu quả.</p>
<p>2. Giao Đảng uỷ Chính phủ chỉ đạo rà soát, sửa đổi các quy định pháp luật có liên quan, hoàn thành trong năm 2027.</p>
<p>3. Các cấp uỷ, tổ chức đảng tổ chức quán triệt, triển khai thực hiện Kết luận này.</p></body></html>`

func legalPartyMeta() map[string]string {
	return map[string]string{"source": "dangcongsan", "source_doc_id": "tulieu-9001", "source_url": "https://tulieuvankien.dangcongsan.vn/x-9001",
		"format": vbpl.LegalHTMLFormatV1, "doc_number": "114-KL/TW", "doc_type": "Kết luận",
		"issuer": "Bộ Chính trị", "issue_date": "2026-11-03", "signer": "Trần Cẩm Tú", "field": "Xây dựng Đảng",
		"title": "Kết luận của Bộ Chính trị về tiếp tục hoàn thiện bộ máy quản lý nhà nước về an toàn thực phẩm"}
}

// A document without Điều is taken as full text and, being numbered the
// Party's way, goes to the Party KB, never to a type KB of the State.
func TestLegalPushUnstructuredPartyDocument(t *testing.T) {
	svc, count := legalPushTestService(t)
	typeKB := &types.KnowledgeBase{ID: "kb-qd", Name: LegalKBNamePrefix + "Quyết định, Chỉ thị, Kết luận"}
	require.NoError(t, svc.db.Create(typeKB).Error)
	svc.kbs = legalPushKBs{kb: typeKB}
	svc.feed = &LegalFeedService{db: svc.db}
	caller := types.Caller{TenantID: types.PlatformTenantID, UserID: "sa-user"}
	ctx := context.WithValue(types.WithCaller(context.Background(), caller), types.SystemAdminContextKey, true)
	push := func(content string) error {
		_, err := svc.Push(ctx, LegalPushInput{Content: []byte(content), Metadata: legalPartyMeta()}, caller, LegalFeedActor{})
		return err
	}

	err := push(kl114Push)
	var app *apperrors.AppError
	require.True(t, stderrors.As(err, &app), "%v", err)
	require.Equal(t, http.StatusUnprocessableEntity, app.HTTPCode)
	require.Contains(t, app.Message, LegalPartyKBName, "no Party KB yet: refused, not sent to the type KB")

	partyKB := &types.KnowledgeBase{ID: "kb-dang", Name: LegalPartyKBName}
	require.NoError(t, svc.db.Create(partyKB).Error)
	svc.kbs = legalPushKBs{kb: partyKB}
	err = push(kl114Push)
	require.Error(t, err)
	require.Contains(t, err.Error(), "knowledge service is not available", "past the structure check and the routing")

	// unmarked "Điều N." lines are refused: the Điều must be marked
	unmarked := strings.Replace(kl114Push, "<p>1. Thống nhất", "<p><b>Điều&nbsp;1.</b> Thống nhất", 1)
	unmarked = strings.Replace(unmarked, "<p>2. Giao", "<p>Điều 2: Giao", 1)
	err = push(unmarked)
	require.True(t, stderrors.As(err, &app), "%v", err)
	require.Contains(t, app.Message, "prov-article")
	// a near-empty page is no document
	err = push(`<html><body><p>Kết luận số 114-KL/TW</p></body></html>`)
	require.True(t, stderrors.As(err, &app), "%v", err)
	require.Contains(t, app.Message, "không có chữ")
	require.EqualValues(t, 0, count())
}

// Mode catalogue: the row of a document listed without a full text.
func TestLegalPushCatalogueRow(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	res, err := svc.Push(ctx, LegalPushInput{Metadata: legalPartyMeta(), Mode: LegalPushCatalogue}, types.Caller{}, LegalFeedActor{})
	require.NoError(t, err)
	require.True(t, res.CatalogueCreated)
	require.Equal(t, "dangcongsan-tulieu-9001", res.LegalDocumentID)
	var row types.LegalDocument
	require.NoError(t, svc.db.First(&row, "id = ?", res.LegalDocumentID).Error)
	require.Equal(t, "Kết luận", row.DocType)
	require.Equal(t, "Trần Cẩm Tú", row.Signer)
	require.Equal(t, "Xây dựng Đảng", row.Field)
	require.Equal(t, legalFeedStatusUnknown, row.Status)
	require.True(t, row.Unverified())
	require.Contains(t, types.LegalUnverifiedNote(&row), "dangcongsan.vn")

	res, err = svc.Push(ctx, LegalPushInput{Metadata: legalPartyMeta(), Mode: LegalPushCatalogue}, types.Caller{}, LegalFeedActor{})
	require.NoError(t, err)
	require.False(t, res.CatalogueCreated, "pushed again: the row is found")
	require.Equal(t, row.ID, res.LegalDocumentID)

	// without a file the other modes still refuse
	_, err = svc.Push(ctx, LegalPushInput{Metadata: legalPartyMeta(), Mode: LegalPushFull}, types.Caller{}, LegalFeedActor{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "file is empty")
}

// A file without structure only brings a full text: on a document a KB
// already holds (mode links) it is refused, so the source's earlier marks
// are not replaced by none.
func TestLegalPushUnstructuredNeverAsLinks(t *testing.T) {
	svc, count := legalPushTestService(t)
	ctx := context.Background()
	meta := legalPushMetaFor("68/2026/NĐ-CP")
	for _, mode := range []string{"", LegalPushLinks} {
		_, err := svc.Push(ctx, LegalPushInput{Content: []byte(kl114Push), Metadata: meta, Mode: mode}, types.Caller{}, LegalFeedActor{})
		var app *apperrors.AppError
		require.True(t, stderrors.As(err, &app), "%v", err)
		require.Equal(t, http.StatusUnprocessableEntity, app.HTTPCode, "mode %q", mode)
		require.Contains(t, app.Message, "chỉ được nạp toàn văn")
	}
	require.EqualValues(t, 0, count())
}

func TestLegalPushCatalogueRefusesExpired(t *testing.T) {
	svc, _ := legalPushTestService(t)
	m := legalPartyMeta()
	m["eff_status"] = "Hết hiệu lực"
	_, err := svc.Push(context.Background(), LegalPushInput{Metadata: m, Mode: LegalPushCatalogue}, types.Caller{}, LegalFeedActor{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "hết hiệu lực toàn bộ")
	var n int64
	require.NoError(t, svc.db.Model(&types.LegalDocument{}).Where("id = ?", "dangcongsan-tulieu-9001").Count(&n).Error)
	require.Zero(t, n)
}

func TestLegalPushTarget(t *testing.T) {
	kbs := []types.LegalPublicKB{{ID: "kb-tt", Name: LegalKBNamePrefix + "Thông tư"}, {ID: "kb-nq", Name: LegalKBNamePrefix + "Nghị quyết"},
		{ID: "kb-dang", Name: LegalPartyKBName}}
	for _, c := range []struct{ feed, number, docType, want string }{
		{"dangcongsan", "57-NQ/TW", "Nghị quyết", "kb-dang"},
		{"tvpl", "57-NQ/TW", "Nghị quyết", "kb-dang"},
		{"dangcongsan", "1232-CV/BTCTW", "Công văn", "kb-dang"},
		{"tvpl", "46-TC/CĐKT", "Thông tư", "kb-tt"}, // old State numbering
		{"tvpl", "5-NQ/TVQH", "Nghị quyết", "kb-nq"},
	} {
		kb, err := legalPushTarget(kbs, c.feed, c.number, c.docType)
		require.NoError(t, err, c.number)
		require.Equal(t, c.want, kb.ID, c.number)
	}
}

// Party numbers restart every term: the same number, type, issuer and a
// near title on another day is another document, given its own row.
func TestLegalPushPartyNumberReusedOnAnotherDay(t *testing.T) {
	svc, _ := legalPushTestService(t)
	ctx := context.Background()
	m := legalPartyMeta()
	m["doc_number"], m["doc_type"], m["issuer"], m["issue_date"] = "11-CT/TW", "Chỉ thị", "Ban Bí thư", "2016-12-20"
	m["title"] = "Chỉ thị số 11-CT/TW của Ban Bí thư về việc tổ chức Tết năm 2017"
	first, err := svc.Push(ctx, LegalPushInput{Metadata: m, Mode: LegalPushCatalogue}, types.Caller{}, LegalFeedActor{})
	require.NoError(t, err)
	m["source_doc_id"], m["issue_date"] = "tulieu-8021", "2021-12-08"
	m["title"] = "Chỉ thị số 11-CT/TW của Ban Bí thư về việc tổ chức Tết Nhâm Dần năm 2022"
	second, err := svc.Push(ctx, LegalPushInput{Metadata: m, Mode: LegalPushCatalogue}, types.Caller{}, LegalFeedActor{})
	require.NoError(t, err)
	require.True(t, second.CatalogueCreated)
	require.NotEqual(t, first.LegalDocumentID, second.LegalDocumentID)
}

func TestLegalUnmarkedArticlesSkipAppendix(t *testing.T) {
	body := `<html><body><p>1. Nội dung.</p><div class="doc-region-appendix"><p>Điều 1. Tặng</p><p>Điều 2. Cấp</p></div></body></html>`
	require.Zero(t, legalUnmarkedArticles([]byte(body)))
	require.Equal(t, 2, legalUnmarkedArticles([]byte(strings.ReplaceAll(body, `<div class="doc-region-appendix">`, `<div>`))))
}
