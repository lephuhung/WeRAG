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
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{},
		&types.LegalDocumentRequest{}, &types.LegalDocumentLink{}, &types.CustomAgent{}))
	require.NoError(t, db.Exec(`CREATE TABLE tenants (id INTEGER PRIMARY KEY, name TEXT)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE knowledges (id TEXT, knowledge_base_id TEXT, title TEXT,
		file_name TEXT, deleted_at DATETIME, metadata TEXT)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_bases (id TEXT, name TEXT, owner_tenant_id INTEGER,
		deleted_at DATETIME)`).Error)
	packs, err := domainpacks.Load(domainpackassets.FS)
	require.NoError(t, err)
	r := NewDomainPackReconciler(packs, repository.NewLegalCatalogRepository(db),
		repository.NewLegalRequestRepository(db), repository.NewLegalReviewRepository(db), repository.NewDomainPackAgentRepository(db))
	r.now = func() time.Time { return time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC) }
	return r, db
}

func reconcileCallerCtx() context.Context {
	return types.WithCaller(context.Background(), types.Caller{TenantID: 10000, UserID: "super-1"})
}

func TestDomainPackReconcileSortsCatalogueNumbers(t *testing.T) {
	r, db := newReconcileFixture(t)
	ctx := reconcileCallerCtx()
	catalog := repository.NewLegalCatalogRepository(db)
	requests := repository.NewLegalRequestRepository(db)

	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "ldn", DocumentNumber: "59/2020/QH14", NumberKey: "59/2020/QH14", Title: "Luật Doanh nghiệp",
			DocType: "Luật", Status: "Còn hiệu lực", EffectiveOn: testDay(t, "2021-01-01")},
		{ID: "ldn-sua", DocumentNumber: "03/2022/QH15", NumberKey: "03/2022/QH15", EffectiveOn: testDay(t, "2022-03-01")},
		{ID: "tt132", DocumentNumber: "132/2018/TT-BTC", NumberKey: "132/2018/TT-BTC", Title: "Chế độ kế toán DN siêu nhỏ",
			DocType: "Thông tư", Status: "Còn hiệu lực", EffectiveOn: testDay(t, "2019-01-01")},
		{ID: "tt58", DocumentNumber: "58/2026/TT-BTC", NumberKey: "58/2026/TT-BTC", Title: "Chế độ kế toán DN siêu nhỏ",
			Status: "Còn hiệu lực", EffectiveOn: testDay(t, "2026-07-01")},
		{ID: "tt133", DocumentNumber: "133/2016/TT-BTC", NumberKey: "133/2016/TT-BTC",
			Status: "Hết hiệu lực toàn bộ", ExpiredOn: testDay(t, "2026-01-01")},
		{ID: "luat67", DocumentNumber: "67/2025/QH15", NumberKey: "67/2025/QH15", EffectiveOn: testDay(t, "2027-01-01")},
		{ID: "luat48", DocumentNumber: "48/2024/QH15", NumberKey: "48/2024/QH15", Status: "Ngưng hiệu lực"},
		// số hiệu is not unique: one live row, one fully expired
		{ID: "nd01a", DocumentNumber: "168/2025/NĐ-CP", NumberKey: "168/2025/ND-CP", Title: "Đăng ký doanh nghiệp", Status: "Còn hiệu lực"},
		{ID: "nd01b", DocumentNumber: "168/2025/NĐ-CP", NumberKey: "168/2025/ND-CP", Title: "Văn bản khác cùng số", Status: "Hết hiệu lực toàn bộ"},
		// a prefix match must not count as the document
		{ID: "luat22x", DocumentNumber: "22/2023/QH15/X", NumberKey: "22/2023/QH15/X"},
	}))
	require.NoError(t, catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "tt58", ToID: "tt132", Relation: types.LegalRelationReplaces},
		{FromID: "ldn-sua", ToID: "ldn", Relation: types.LegalRelationAmends},
	}))
	// 59/2020/QH14 is held through an approved link, 58/2026/TT-BTC through
	// an ingested public copy not linked yet
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases (id, name, owner_tenant_id) VALUES ('kb-pub', 'VBPL – Luật', 0)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id, knowledge_base_id, title, metadata) VALUES
		('k-ldn', 'kb-pub', 'Luật Doanh nghiệp', '{}'),
		('k-tt58', 'kb-pub', 'Thông tư 58', '{"external_id":"tt58"}')`).Error)
	require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: "k-ldn", KnowledgeBaseID: "kb-pub",
		LegalDocumentID: "ldn", ReviewStatus: types.LegalReviewApproved}).Error)

	// a user already asked for 88/2015/QH13 (open) and for 133/2016/TT-BTC
	// (its catalogue row); 174/2016/NĐ-CP was rejected
	open, err := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "88/2015/QH13", DocumentNumber: "88/2015/QH13",
		LastSessionID: "s1", LastQuery: "Luật Kế toán 88/2015/QH13"})
	require.NoError(t, err)
	open133, err := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "133/2016/TT-BTC", LegalDocumentID: "tt133",
		Source: types.LegalRequestSourceCatalogue, LastSessionID: "s2"})
	require.NoError(t, err)
	rejected, err := requests.Record(ctx, &types.LegalDocumentRequest{NumberKey: "174/2016/ND-CP", DocumentNumber: "174/2016/NĐ-CP"})
	require.NoError(t, err)
	_, err = requests.Decide(ctx, rejected.ID, types.LegalRequestRejected, "admin", "không có", "", "")
	require.NoError(t, err)

	var before []types.LegalDocument
	require.NoError(t, db.Order("id").Find(&before).Error)

	res, err := r.Reconcile(ctx, "opc-vn-business-law")
	require.NoError(t, err)
	assert.Empty(t, res.Errors)
	assert.Equal(t, 17, len(res.Found)+len(res.Missing)+len(res.Ambiguous))

	found := map[string]types.DomainPackReconcileDocument{}
	for _, f := range res.Found {
		found[f.Number] = f.Document
	}
	ldn := found["59/2020/QH14"]
	assert.Equal(t, "ldn", ldn.ID)
	assert.True(t, ldn.HeldInKB)
	assert.Nil(t, ldn.Request)
	assert.Contains(t, ldn.StatusNote, "đã được sửa đổi bởi 03/2022/QH15")
	assert.True(t, found["58/2026/TT-BTC"].HeldInKB, "an ingested copy counts as held")
	assert.Nil(t, found["58/2026/TT-BTC"].Request)

	tt132 := found["132/2018/TT-BTC"]
	assert.False(t, tt132.HeldInKB)
	assert.Contains(t, tt132.StatusNote, "bị thay thế bởi 58/2026/TT-BTC (hiệu lực 01/07/2026)")
	require.NotNil(t, tt132.Request)
	assert.True(t, tt132.Request.Created)
	row, err := requests.Get(ctx, tt132.Request.ID)
	require.NoError(t, err)
	assert.Equal(t, "tt132", row.LegalDocumentID, "the ingest button needs the catalogue id")
	assert.Equal(t, types.LegalRequestSourceCatalogue, row.Source)
	assert.Equal(t, "Thông tư", row.DocType)
	assert.Equal(t, uint64(10000), row.LastTenantID)
	assert.Equal(t, "super-1", row.LastUserID)
	var detail map[string]any
	require.NoError(t, json.Unmarshal(row.Detail, &detail))
	assert.Equal(t, "pack:opc-vn-business-law", detail["origin"])

	tt133 := found["133/2016/TT-BTC"]
	assert.Contains(t, tt133.StatusNote, "Hết hiệu lực toàn bộ")
	assert.Contains(t, tt133.StatusNote, "01/01/2026")
	assert.Nil(t, tt133.Request, "every row fully expired: no request, the status note is the signal")
	still133, err := requests.Get(ctx, open133.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, still133.RequestCount)
	assert.Contains(t, found["67/2025/QH15"].StatusNote, "chưa có hiệu lực (hiệu lực từ 01/01/2027)")
	assert.Contains(t, found["48/2024/QH15"].StatusNote, "Ngưng hiệu lực")

	require.Len(t, res.Ambiguous, 1)
	amb := res.Ambiguous[0]
	assert.Equal(t, "168/2025/NĐ-CP", amb.Number)
	require.Len(t, amb.Documents, 2)
	for _, d := range amb.Documents {
		if d.ID == "nd01a" {
			require.NotNil(t, d.Request, "the live row is requested")
		} else {
			assert.Nil(t, d.Request, "the expired row beside a live one is not")
		}
	}

	missing := map[string]types.DomainPackReconcileMissing{}
	for _, m := range res.Missing {
		missing[m.Number] = m
	}
	assert.Len(t, missing, 17-6-1)
	assert.Contains(t, missing, "22/2023/QH15", "a prefix match is not the document")
	assert.Equal(t, open.ID, missing["88/2015/QH13"].Request.ID)
	assert.False(t, missing["88/2015/QH13"].Request.Created)
	assert.Equal(t, types.LegalRequestRejected, missing["174/2016/NĐ-CP"].Request.Status)
	created := missing["41/2024/QH15"].Request
	require.True(t, created.Created)
	row, err = requests.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Empty(t, row.Source, "unknown to the catalogue, as the gate records it")
	assert.Empty(t, row.LegalDocumentID)
	assert.Equal(t, uint64(10000), row.LastTenantID)

	// existing requests were left as they were
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
	again, err := r.Reconcile(ctx, "opc-vn-business-law")
	require.NoError(t, err)
	for _, m := range again.Missing {
		assert.False(t, m.Request.Created, m.Number)
	}
	for _, f := range again.Found {
		if f.Document.Request != nil {
			assert.False(t, f.Document.Request.Created, f.Number)
		}
	}
	var n2 int64
	require.NoError(t, db.Model(&types.LegalDocumentRequest{}).Count(&n2).Error)
	assert.Equal(t, n1, n2)
}

func TestDomainPackReconcileReportsPerNumberErrors(t *testing.T) {
	r, db := newReconcileFixture(t)
	// the request table is gone: every missing number fails, the run goes on
	require.NoError(t, db.Migrator().DropTable(&types.LegalDocumentRequest{}))
	res, err := r.Reconcile(reconcileCallerCtx(), "opc-vn-business-law")
	require.NoError(t, err)
	assert.Len(t, res.Errors, 17)
	assert.Equal(t, "59/2020/QH14", res.Errors[0].Number)
}

func TestDomainPackReconcileUnknownPack(t *testing.T) {
	r, _ := newReconcileFixture(t)
	_, err := r.Reconcile(context.Background(), "no-such-pack")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-pack")
}

func TestDomainPackDetailIsReadOnly(t *testing.T) {
	r, db := newReconcileFixture(t)
	ctx := reconcileCallerCtx()
	catalog := repository.NewLegalCatalogRepository(db)
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		{ID: "luat67", DocumentNumber: "67/2025/QH15", NumberKey: "67/2025/QH15", Title: "Luật Thuế TNDN",
			Status: "Hết hiệu lực một phần", EffectiveOn: testDay(t, "2025-10-01")},
		{ID: "ldn", DocumentNumber: "59/2020/QH14", NumberKey: "59/2020/QH14", Status: "Còn hiệu lực"},
	}))
	require.NoError(t, db.Exec(`INSERT INTO tenants (id, name) VALUES (10003, 'Công an tỉnh'), (10004, 'Sở Tư pháp')`).Error)
	for _, a := range []*types.CustomAgent{
		{ID: "a1", Name: "Tư vấn DN", TenantID: 10003, AppliesToTenantIDs: types.AgentTenantScope{10004, 10077},
			Config: types.CustomAgentConfig{DomainPacks: []string{"opc-vn-business-law"}}},
		{ID: "a2", Name: "Khác", TenantID: 10003, Config: types.CustomAgentConfig{DomainPacks: []string{"other-pack"}}},
		{ID: "a3", Name: "Hỗ trợ", TenantID: 10004, Config: types.CustomAgentConfig{DomainPacks: []string{"x", "opc-vn-business-law"}}},
	} {
		require.NoError(t, db.Create(a).Error)
	}

	detail, err := r.Detail(ctx, "opc-vn-business-law")
	require.NoError(t, err)
	var n int64
	require.NoError(t, db.Model(&types.LegalDocumentRequest{}).Count(&n).Error)
	assert.Zero(t, n, "the detail view never records a request")

	assert.Equal(t, 17, detail.DocumentCount)
	assert.Equal(t, 9, detail.FormulaCount)
	require.Len(t, detail.Agents, 2)
	assert.Equal(t, "a1", detail.Agents[0].ID)
	assert.Equal(t, "Công an tỉnh", detail.Agents[0].TenantName)
	assert.Equal(t, "Sở Tư pháp", detail.Agents[1].TenantName)
	// "Tổ chức áp dụng": named where the tenant exists, [] = every Tổ chức
	assert.Equal(t, []types.DomainPackAgentScope{{TenantID: 10004, TenantName: "Sở Tư pháp"}, {TenantID: 10077}},
		detail.Agents[0].AppliesTo)
	assert.Empty(t, detail.Agents[1].AppliesTo)
	assert.NotNil(t, detail.Agents[1].AppliesTo, "an array in the API, not null")

	docs := map[string]types.DomainPackDocumentView{}
	for _, d := range detail.Documents {
		docs[d.Number] = d
	}
	tndn := docs["67/2025/QH15"]
	assert.Equal(t, types.DomainPackLookupFound, tndn.Lookup)
	assert.True(t, tndn.Warning)
	assert.Contains(t, tndn.Formulas, "so_sanh_thue_tndn")
	assert.NotEmpty(t, tndn.CitedIn)
	assert.Equal(t, types.DomainPackLookupMissing, docs["88/2015/QH13"].Lookup)
	assert.True(t, docs["88/2015/QH13"].Warning)
	assert.Contains(t, docs["59/2020/QH14"].Articles, "Điều 85")
	assert.True(t, docs["59/2020/QH14"].Warning, "no public KB holds it")

	require.Len(t, detail.Formulas, 9)
	for _, f := range detail.Formulas {
		assert.True(t, f.Warning, f.ID)
		require.NotEmpty(t, f.Basis)
		assert.Equal(t, types.DomainPackBasisWarning, f.Basis[0].Status)
		assert.NotEmpty(t, f.Examples)
		if f.Tool != "compare_tax_methods" {
			// the household formulas cite documents this catalogue lacks
			assert.Contains(t, f.Basis[0].Note, "không có trong danh mục văn bản", f.ID)
			continue
		}
		assert.Contains(t, f.Basis[0].Note, "Hết hiệu lực một phần")
	}

	list, err := r.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, 2, list[0].AgentCount)
	assert.Equal(t, 17, list[0].DocumentCount)

	_, err = r.Detail(ctx, "nope")
	assert.Error(t, err)
}
