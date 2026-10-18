package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	domainpackassets "github.com/Tencent/WeKnora/domain-packs"
	"github.com/Tencent/WeKnora/internal/agent/domainpacks"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

func testDay(t *testing.T, s string) *time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return &d
}

func newReconcileFixture(t *testing.T) (*DomainPackReconciler, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentRequest{}))
	packs, err := domainpacks.Load(domainpackassets.FS)
	require.NoError(t, err)
	r := NewDomainPackReconciler(packs, repository.NewLegalCatalogRepository(db), repository.NewLegalRequestRepository(db))
	r.now = func() time.Time { return time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC) }
	return r, db
}

func TestDomainPackReconcileSortsCatalogueNumbers(t *testing.T) {
	r, db := newReconcileFixture(t)
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)
	requests := repository.NewLegalRequestRepository(db)

	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "ldn", DocumentNumber: "59/2020/QH14", NumberKey: "59/2020/QH14", Title: "Luật Doanh nghiệp",
			DocType: "Luật", Status: "Còn hiệu lực", EffectiveOn: testDay(t, "2021-01-01")},
		{ID: "tt132", DocumentNumber: "132/2018/TT-BTC", NumberKey: "132/2018/TT-BTC", Title: "Chế độ kế toán DN siêu nhỏ",
			Status: "Còn hiệu lực", EffectiveOn: testDay(t, "2019-01-01")},
		{ID: "tt58", DocumentNumber: "58/2026/TT-BTC", NumberKey: "58/2026/TT-BTC", Title: "Chế độ kế toán DN siêu nhỏ",
			Status: "Còn hiệu lực", EffectiveOn: testDay(t, "2026-07-01")},
		{ID: "tt133", DocumentNumber: "133/2016/TT-BTC", NumberKey: "133/2016/TT-BTC",
			Status: "Hết hiệu lực toàn bộ", ExpiredOn: testDay(t, "2026-01-01")},
		// số hiệu is not unique
		{ID: "nd01a", DocumentNumber: "01/2021/NĐ-CP", NumberKey: "01/2021/ND-CP", Title: "Đăng ký doanh nghiệp"},
		{ID: "nd01b", DocumentNumber: "01/2021/NĐ-CP", NumberKey: "01/2021/ND-CP", Title: "Văn bản khác cùng số"},
		// a prefix match must not count as the document
		{ID: "luat22x", DocumentNumber: "22/2023/QH15/X", NumberKey: "22/2023/QH15/X"},
	}))
	require.NoError(t, catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "tt58", ToID: "tt132", Relation: types.LegalRelationReplaces},
	}))
	// a user already asked for 88/2015/QH13 (open), and 174/2016/NĐ-CP was rejected
	open, err := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "88/2015/QH13", DocumentNumber: "88/2015/QH13",
		LastSessionID: "s1", LastQuery: "Luật Kế toán 88/2015/QH13"})
	require.NoError(t, err)
	rejected, err := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "174/2016/ND-CP", DocumentNumber: "174/2016/NĐ-CP"})
	require.NoError(t, err)
	_, err = requests.Decide(ctx, rejected.ID, types.LegalRequestRejected, "admin", "không có", "", "")
	require.NoError(t, err)

	var before []types.LegalDocument
	require.NoError(t, db.Order("id").Find(&before).Error)

	res, err := r.Reconcile(ctx, "opc-vn-business-law", "super-1")
	require.NoError(t, err)
	assert.Equal(t, "opc-vn-business-law", res.Pack)
	assert.Equal(t, 13, len(res.Found)+len(res.Missing)+len(res.Ambiguous))

	found := map[string]types.DomainPackReconcileFound{}
	for _, f := range res.Found {
		found[f.Number] = f
	}
	require.Contains(t, found, "59/2020/QH14")
	assert.Equal(t, "ldn", found["59/2020/QH14"].Document.ID)
	assert.Equal(t, "2021-01-01", found["59/2020/QH14"].Document.EffectiveOn)
	assert.Empty(t, found["59/2020/QH14"].Document.StatusNote)
	assert.Contains(t, found["132/2018/TT-BTC"].Document.StatusNote, "bị thay thế bởi 58/2026/TT-BTC (hiệu lực 01/07/2026)")
	assert.Empty(t, found["58/2026/TT-BTC"].Document.StatusNote)
	assert.Contains(t, found["133/2016/TT-BTC"].Document.StatusNote, "Hết hiệu lực toàn bộ")
	assert.Contains(t, found["133/2016/TT-BTC"].Document.StatusNote, "01/01/2026")
	assert.Equal(t, "2026-01-01", found["133/2016/TT-BTC"].Document.ExpiredOn)

	require.Len(t, res.Ambiguous, 1)
	assert.Equal(t, "01/2021/NĐ-CP", res.Ambiguous[0].Number)
	assert.Len(t, res.Ambiguous[0].Documents, 2)

	missing := map[string]types.DomainPackReconcileMissing{}
	for _, m := range res.Missing {
		missing[m.Number] = m
	}
	assert.Len(t, missing, 13-4-1)
	assert.Contains(t, missing, "22/2023/QH15", "a prefix match is not the document")
	assert.Equal(t, open.ID, missing["88/2015/QH13"].RequestID)
	assert.False(t, missing["88/2015/QH13"].RequestCreated)
	assert.Equal(t, types.LegalRequestRejected, missing["174/2016/NĐ-CP"].RequestStatus)
	assert.False(t, missing["174/2016/NĐ-CP"].RequestCreated)
	created := missing["67/2025/QH15"]
	require.True(t, created.RequestCreated)
	assert.Equal(t, types.LegalRequestRequested, created.RequestStatus)

	row, err := requests.Get(ctx, created.RequestID)
	require.NoError(t, err)
	assert.Equal(t, "pack:opc-vn-business-law", row.Source)
	assert.Equal(t, "67/2025/QH15", row.NumberKey)
	assert.Equal(t, "super-1", row.LastUserID)
	assert.Empty(t, row.LegalDocumentID)
	var detail map[string]any
	require.NoError(t, json.Unmarshal(row.Detail, &detail))
	assert.Equal(t, "pack:opc-vn-business-law", detail["origin"])
	assert.Equal(t, "1.0.0", detail["pack_version"])

	// the existing requests were left as they were
	kept, err := requests.Get(ctx, open.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, kept.RequestCount)
	assert.Equal(t, "s1", kept.LastSessionID)
	still, err := requests.Get(ctx, rejected.ID)
	require.NoError(t, err)
	assert.Equal(t, types.LegalRequestRejected, still.Status)

	// legal_documents is never changed
	var after []types.LegalDocument
	require.NoError(t, db.Order("id").Find(&after).Error)
	require.Equal(t, len(before), len(after))
	for i := range before {
		assert.Equal(t, before[i].Status, after[i].Status)
		assert.True(t, before[i].UpdatedAt.Equal(after[i].UpdatedAt), before[i].ID)
	}

	// a second run records nothing new and bumps nothing
	var n1 int64
	require.NoError(t, db.Model(&types.LegalDocumentRequest{}).Count(&n1).Error)
	again, err := r.Reconcile(ctx, "opc-vn-business-law", "super-1")
	require.NoError(t, err)
	for _, m := range again.Missing {
		assert.False(t, m.RequestCreated, m.Number)
	}
	var n2 int64
	require.NoError(t, db.Model(&types.LegalDocumentRequest{}).Count(&n2).Error)
	assert.Equal(t, n1, n2)
	row, err = requests.Get(ctx, created.RequestID)
	require.NoError(t, err)
	assert.Equal(t, 1, row.RequestCount)
}

func TestDomainPackReconcileUnknownPack(t *testing.T) {
	r, _ := newReconcileFixture(t)
	_, err := r.Reconcile(context.Background(), "no-such-pack", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-pack")
}
