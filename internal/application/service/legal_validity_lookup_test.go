package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	lookupTenantA uint64 = 7
	lookupTenantB uint64 = 8
)

type legalLookupFixture struct {
	svc     *legalValidityLookupService
	db      *gorm.DB
	catalog interfaces.LegalCatalogRepository
	misses  interfaces.LegalLookupMissRepository
}

func lookupDay(s string) *time.Time { d, _ := time.Parse("2006-01-02", s); return &d }

func newLegalLookupFixture(t *testing.T) *legalLookupFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.TenantMember{},
		&types.KBInvitation{}, &types.KBAccessGrant{},
		&types.LegalDocument{}, &types.LegalDocumentRelation{}, &types.LegalDocumentLink{}, &types.LegalLookupMiss{}))
	ctx := context.Background()
	catalog := repository.NewLegalCatalogRepository(db)

	snap := lookupDay("2026-11-10")
	doc := func(id, num, typ, title, status, issued, effective string) *types.LegalDocument {
		d := &types.LegalDocument{ID: id, Source: types.LegalDocumentSourceVBPL, DocumentNumber: num,
			NumberKey: vietnamese_legal.CatalogNumberKey(num), DocType: typ, Title: title,
			Issuer: "Quốc hội", Status: status, IssuedOn: lookupDay(issued), SourceUpdatedAt: snap}
		if effective != "" {
			d.EffectiveOn = lookupDay(effective)
		}
		return d
	}
	feed := doc("feed", "99/2026/NĐ-CP", "Nghị định", "Nghị định thử nghiệm", "Còn hiệu lực", "2026-10-01", "")
	feed.Source = "tvpl"
	feed.SourceUpdatedAt = lookupDay("2026-11-11") // a feed push is not the vbpl snapshot
	stale := doc("ubnd-old", "53/QĐ-UBND", "Quyết định", "Quyết định về chợ", "Còn hiệu lực", "2019-03-01", "")
	stale.SourceUpdatedAt = lookupDay("2026-11-01")
	require.NoError(t, catalog.UpsertDocuments(ctx, []*types.LegalDocument{
		doc("law-old", "45/2013/QH13", "Luật", "Luật Đất đai", "Còn hiệu lực", "2013-11-29", "2014-07-01"),
		doc("law", "31/2024/QH15", "Luật", "Luật Đất đai", "Còn hiệu lực", "2024-01-18", "2024-08-01"),
		doc("amend", "43/2024/QH15", "Luật", "Luật sửa đổi, bổ sung một số điều của Luật Đất đai",
			"Còn hiệu lực", "2024-06-29", "2024-08-01"),
		doc("amend2", "1/2025/QH15", "Luật", "Luật thay thế luật sửa đổi", "Còn hiệu lực", "2025-01-10", "2025-07-01"),
		doc("blhs", "100/2015/QH13", "Bộ luật", "Bộ luật Hình sự", "Còn hiệu lực", "2015-11-27", "2018-01-01"),
		doc("nd100", "100/2019/NĐ-CP", "Nghị định",
			"Quy định xử phạt vi phạm hành chính trong lĩnh vực giao thông đường bộ và đường sắt",
			"Hết hiệu lực một phần", "2019-12-30", "2020-01-01"),
		doc("nd100b", "100/2020/NĐ-CP", "Nghị định", "Về kinh doanh dịch vụ bảo vệ", "Còn hiệu lực", "2020-08-28", ""),
		doc("nd53", "53/2022/NĐ-CP", "Nghị định", "Quy định chi tiết Luật An ninh mạng", "Còn hiệu lực", "2022-08-15", "2022-10-01"),
		doc("tt53", "53/2022/TT-BTC", "Thông tư", "Thông tư về phí", "Hết hiệu lực một phần", "2022-08-20", ""),
		doc("qd135", "551/QĐ-TTg", "Quyết định", "Phê duyệt Chương trình 135 giai đoạn III", "Còn hiệu lực", "2013-04-04", ""),
		doc("ubnd2024", "2024/QĐ-UBND", "Quyết định", "Quyết định về đất đai ở xã", "Còn hiệu lực", "2020-05-05", ""),
		doc("mention", "7/2099/TT-BNV", "Thông tư", "Thông tư hướng dẫn Nghị định 30/2099/NĐ-CP", "Còn hiệu lực", "2026-01-01", ""),
		// số hiệu is not unique: two provinces' decisions share one
		doc("qd-a", "12/2025/QĐ-UBND", "Quyết định", "Quyết định tỉnh A", "Còn hiệu lực", "2025-02-01", ""),
		doc("qd-b", "12/2025/QĐ-UBND", "Quyết định", "Quyết định tỉnh B", "Hết hiệu lực toàn bộ", "2025-01-10", ""),
		stale, feed,
	}))
	require.NoError(t, catalog.ReplaceRelations(ctx, []*types.LegalDocumentRelation{
		{FromID: "law", ToID: "law-old", Relation: types.LegalRelationReplaces},
		{FromID: "amend", ToID: "law", Relation: types.LegalRelationAmends},
		{FromID: "amend2", ToID: "amend", Relation: types.LegalRelationReplaces},
		{FromID: "nd53", ToID: "law", Relation: types.LegalRelationGuides}, // not a validity edge
	}))

	kb := func(id string, owner uint64, vis types.KBVisibility) {
		require.NoError(t, db.Create(&types.KnowledgeBase{ID: id, Name: "KB " + id, TenantID: owner, OwnerTenantID: owner,
			Visibility: vis, Type: types.KnowledgeBaseTypeDocument}).Error)
	}
	kb("kb-pub", 0, types.KBVisibilityPublic)
	kb("kb-a", lookupTenantA, types.KBVisibilityTenant)
	kb("kb-a-restricted", lookupTenantA, types.KBVisibilityRestricted)
	kb("kb-a-gone", lookupTenantA, types.KBVisibilityTenant)
	kb("kb-b", lookupTenantB, types.KBVisibilityTenant)
	kb("kb-b-published", lookupTenantB, types.KBVisibilityPublished)
	require.NoError(t, db.Delete(&types.KnowledgeBase{}, "id = ?", "kb-a-gone").Error)

	knowledge := func(id, kbID, docID string) {
		meta := types.JSON(`{"external_id":"` + docID + `"}`)
		require.NoError(t, db.Create(&types.Knowledge{ID: id, KnowledgeBaseID: kbID, Title: id + ".html",
			Metadata: meta, CustomMetadata: types.JSON(`{}`), ParseStatus: types.ParseStatusCompleted}).Error)
	}
	link := func(knowledgeID, kbID, docID string, status types.LegalReviewStatus) {
		require.NoError(t, db.Create(&types.LegalDocumentLink{KnowledgeID: knowledgeID, KnowledgeBaseID: kbID,
			LegalDocumentID: docID, ReviewStatus: status}).Error)
	}
	// copies of the 2024 law
	knowledge("k-pub", "kb-pub", "law")
	link("k-pub", "kb-pub", "law", types.LegalReviewApproved)
	knowledge("k-pub-pending", "kb-pub", "law")
	link("k-pub-pending", "kb-pub", "law", types.LegalReviewPending)
	knowledge("k-a", "kb-a", "law")
	knowledge("k-a-rejected", "kb-a", "law")
	link("k-a-rejected", "kb-a", "law", types.LegalReviewRejected)
	knowledge("k-a-deleted", "kb-a", "law")
	require.NoError(t, db.Delete(&types.Knowledge{}, "id = ?", "k-a-deleted").Error)
	knowledge("k-a-restricted", "kb-a-restricted", "law")
	knowledge("k-a-gone", "kb-a-gone", "law")
	knowledge("k-b", "kb-b", "law")
	knowledge("k-b-published", "kb-b-published", "law")
	// the old law: only in tenant A
	knowledge("k-a-old", "kb-a", "law-old")

	for _, m := range []struct {
		user   string
		tenant uint64
	}{{"member-a", lookupTenantA}, {"member-b", lookupTenantB}} {
		require.NoError(t, db.Create(&types.TenantMember{UserID: m.user, TenantID: m.tenant,
			Role: types.TenantRoleMember, Status: types.TenantMemberStatusActive, JoinedAt: time.Now()}).Error)
	}

	misses := repository.NewLegalLookupMissRepository(db)
	svc := NewLegalValidityLookupService(catalog, repository.NewLegalReviewRepository(db),
		repository.NewKnowledgeBaseRepository(db), misses).(*legalValidityLookupService)
	svc.now = func() time.Time { return *lookupDay("2026-11-11") }
	svc.recordMiss = func(m *types.LegalLookupMiss) { svc.writeMiss(m) }
	return &legalLookupFixture{svc: svc, db: db, catalog: catalog, misses: misses}
}

func lookupCtx(user string, tenant uint64, role types.TenantRole) context.Context {
	ctx := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: user})
	return types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: user, Role: role})
}

func guestCtx() context.Context { return lookupCtx("guest", 0, "") }

func lookupIDs(got *types.LegalValidityLookup) []string {
	ids := []string{}
	for _, m := range got.Matches {
		ids = append(ids, m.ID)
	}
	return ids
}

func knowledgeIDs(m types.LegalValidityMatch) []string {
	ids := []string{}
	for _, k := range m.Knowledge {
		ids = append(ids, k.KnowledgeID)
	}
	return ids
}

func TestLegalValidityLookupByNumber(t *testing.T) {
	f := newLegalLookupFixture(t)
	ctx := guestCtx()

	got, err := f.svc.LookupValidity(ctx, "Luật số 45/2013/QH13,", 0)
	require.NoError(t, err)
	require.Equal(t, "number", got.By)
	require.Empty(t, got.NumberMissing)
	require.False(t, got.More)
	require.Equal(t, "2026-11-10", got.CatalogSnapshotDate, "vbpl snapshot only, not the feed row")
	require.Len(t, got.Matches, 1)
	m := got.Matches[0]
	require.Equal(t, "45/2013/QH13", m.DocumentNumber)
	require.Equal(t, "Còn hiệu lực", m.StatusText)
	// the portal still says in force, but the replacing law is in force
	require.Equal(t, types.LegalStatusKindExpired, m.Status)
	require.NotNil(t, m.Derived)
	require.Equal(t, types.LegalStatusExpired, m.Derived.StatusText)
	require.Equal(t, "2024-08-01", m.Derived.ExpiredOn)
	require.Equal(t, "law", m.Derived.By.ID)
	require.Len(t, m.ReplacedBy, 1)
	require.Equal(t, 1, m.ReplacedByTotal)
	require.Equal(t, "31/2024/QH15", m.ReplacedBy[0].DocumentNumber)
	require.Equal(t, types.LegalRelationReplaces, m.ReplacedBy[0].Relation)
	require.Equal(t, "2013-11-29", m.IssuedOn)
	require.Empty(t, m.Knowledge, "a guest does not see tenant A's copy")

	got, err = f.svc.LookupValidity(ctx, "31/2024/qh15", 0)
	require.NoError(t, err)
	require.Len(t, got.Matches, 1)
	m = got.Matches[0]
	require.Equal(t, types.LegalStatusKindInForce, m.Status)
	require.Nil(t, m.Derived)
	require.Len(t, m.AmendedBy, 1)
	require.Equal(t, "43/2024/QH15", m.AmendedBy[0].DocumentNumber)
	// the amending law was itself replaced by one in force (L4)
	require.Equal(t, types.LegalStatusKindExpired, m.AmendedBy[0].Status)
	require.Empty(t, m.ReplacedBy)
	require.Empty(t, m.EndedBy)

	// a shared số hiệu returns every row
	got, err = f.svc.LookupValidity(ctx, "12/2025/QĐ-UBND", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"qd-a", "qd-b"}, lookupIDs(got), "newest first")
	require.Equal(t, types.LegalStatusKindExpired, got.Matches[1].Status)

	// a date before the số hiệu is not the number (L6)
	got, err = f.svc.LookupValidity(ctx, "ngày 18/1/2024 Luật 31/2024/QH15", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"law"}, lookupIDs(got))
}

func TestLegalValidityLookupCopies(t *testing.T) {
	f := newLegalLookupFixture(t)

	// a guest: the approved public copy and the published KB's copy only
	got, err := f.svc.LookupValidity(guestCtx(), "31/2024/QH15", 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"k-pub", "k-b-published"}, knowledgeIDs(got.Matches[0]))

	// a member of A also sees A's own copy — not the restricted KB's
	// (no grant), the rejected or deleted one, the deleted KB's, nor B's
	got, err = f.svc.LookupValidity(lookupCtx("member-a", lookupTenantA, types.TenantRoleMember), "31/2024/QH15", 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"k-pub", "k-b-published", "k-a"}, knowledgeIDs(got.Matches[0]))
	require.Equal(t, "KB kb-a", func() string {
		for _, k := range got.Matches[0].Knowledge {
			if k.KnowledgeID == "k-a" {
				return k.KnowledgeBaseName
			}
		}
		return ""
	}())

	// a member of B sees B's copy, not A's
	got, err = f.svc.LookupValidity(lookupCtx("member-b", lookupTenantB, types.TenantRoleMember), "31/2024/QH15", 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"k-pub", "k-b-published", "k-b"}, knowledgeIDs(got.Matches[0]))

	// an admin of A reads the restricted KB of A too (the KB list's rule)
	got, err = f.svc.LookupValidity(lookupCtx("admin-a", lookupTenantA, types.TenantRoleAdmin), "31/2024/QH15", 0)
	require.NoError(t, err)
	require.Contains(t, knowledgeIDs(got.Matches[0]), "k-a-restricted")

	got, err = f.svc.LookupValidity(lookupCtx("member-a", lookupTenantA, types.TenantRoleMember), "45/2013/QH13", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"k-a-old"}, knowledgeIDs(got.Matches[0]))
}

func TestLegalValidityLookupBareNumber(t *testing.T) {
	f := newLegalLookupFixture(t)
	ctx := guestCtx()

	// a bare number: the exact local number and every 53/…
	got, err := f.svc.LookupValidity(ctx, "53", 0)
	require.NoError(t, err)
	require.Equal(t, "number", got.By)
	require.Equal(t, []string{"tt53", "nd53", "ubnd-old"}, lookupIDs(got))
	require.Equal(t, types.LegalStatusKindPartlyExpired, got.Matches[0].Status)

	// a leading doc type narrows it, in SQL (L1)
	got, err = f.svc.LookupValidity(ctx, "Nghị định 53", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"nd53"}, lookupIDs(got))
	got, err = f.svc.LookupValidity(ctx, "nghị định số 53", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"nd53"}, lookupIDs(got))

	// M2: type + number + content words (stop words ignored)
	got, err = f.svc.LookupValidity(ctx, "Nghị định 100 về xử phạt giao thông", 0)
	require.NoError(t, err)
	require.Equal(t, "number", got.By)
	require.Equal(t, []string{"nd100"}, lookupIDs(got))
	// a bare "Bộ luật 100" is the code, not the decrees
	got, err = f.svc.LookupValidity(ctx, "Bộ luật 100", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"blhs"}, lookupIDs(got))

	// limit and the "more" signal
	got, err = f.svc.LookupValidity(ctx, "53", 2)
	require.NoError(t, err)
	require.Len(t, got.Matches, 2)
	require.True(t, got.More)
	got, err = f.svc.LookupValidity(ctx, "53", 3)
	require.NoError(t, err)
	require.False(t, got.More)
}

// M1: digits inside a title are not a bare number.
func TestLegalValidityLookupTitleWithDigits(t *testing.T) {
	f := newLegalLookupFixture(t)
	ctx := guestCtx()

	got, err := f.svc.LookupValidity(ctx, "Luật Đất đai 2024", 0)
	require.NoError(t, err)
	require.Equal(t, "title", got.By)
	// the 2024 law first, then the 2024 law amending it; never decision 2024/QĐ-UBND
	require.Equal(t, []string{"law", "amend"}, lookupIDs(got))

	got, err = f.svc.LookupValidity(ctx, "Bộ luật Hình sự 2015", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"blhs"}, lookupIDs(got))

	got, err = f.svc.LookupValidity(ctx, "chương trình 135", 0)
	require.NoError(t, err)
	require.Equal(t, "title", got.By)
	require.Equal(t, []string{"qd135"}, lookupIDs(got))

	// (SQLite's lower() folds ASCII only, so the test keeps the title's
	// case for "Đ"; Postgres matches with ILIKE regardless)
	got, err = f.svc.LookupValidity(ctx, "luật Đất đai", 0)
	require.NoError(t, err)
	require.Equal(t, "title", got.By)
	require.Equal(t, []string{"law", "law-old", "amend"}, lookupIDs(got), "titles naming it first, newest first")
}

// M4: a số hiệu the catalogue lacks is said to be missing, and counted.
func TestLegalValidityLookupMissingNumber(t *testing.T) {
	f := newLegalLookupFixture(t)
	ctx := lookupCtx("member-a", lookupTenantA, types.TenantRoleMember)

	got, err := f.svc.LookupValidity(ctx, "Nghị định 30/2099/NĐ-CP", 0)
	require.NoError(t, err)
	require.Equal(t, "30/2099/NĐ-CP", got.NumberMissing)
	require.Equal(t, "title", got.By)
	require.Equal(t, []string{"mention"}, lookupIDs(got))

	got, err = f.svc.LookupValidity(ctx, "777/2099/NĐ-CP", 0)
	require.NoError(t, err)
	require.Equal(t, "777/2099/NĐ-CP", got.NumberMissing)
	require.Empty(t, got.Matches)
	require.NotNil(t, got.Matches)

	_, err = f.svc.LookupValidity(ctx, "  777/2099/nđ-cp ", 0) // same normalized query
	require.NoError(t, err)
	_, err = f.svc.LookupValidity(guestCtx(), "không có văn bản này", 0)
	require.NoError(t, err)
	_, err = f.svc.LookupValidity(ctx, "31/2024/QH15", 0) // a hit is no miss
	require.NoError(t, err)

	rows, total, err := f.misses.List(context.Background(), true, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	byQuery := map[string]*types.LegalLookupMiss{}
	for _, r := range rows {
		byQuery[r.Query] = r
	}
	require.Equal(t, "777/2099/nđ-cp", rows[0].Query, "most frequent first")
	require.EqualValues(t, 2, rows[0].Count)
	require.Equal(t, types.LegalLookupMissNumber, rows[0].Reason)
	require.Equal(t, "777/2099/nđ-cp", rows[0].Number)
	require.Equal(t, lookupTenantA, rows[0].LastTenantID)
	require.Equal(t, "member-a", rows[0].LastUserID)
	require.Equal(t, types.LegalLookupMissNumber, byQuery["nghị định 30/2099/nđ-cp"].Reason)
	require.Equal(t, types.LegalLookupMissNoMatch, byQuery["không có văn bản này"].Reason)
	require.Equal(t, "", byQuery["không có văn bản này"].Number)
}

func TestLegalValidityLookupOddInputs(t *testing.T) {
	f := newLegalLookupFixture(t)
	ctx := guestCtx()
	for _, q := range []string{"%", "_", "%%%", "1/", "50%", "%/2024/%", "Luật %", "_ 53", "'; DROP TABLE legal_documents; --", "   "} {
		got, err := f.svc.LookupValidity(ctx, q, 0)
		require.NoError(t, err, q)
		require.NotNil(t, got.Matches, q)
		for _, m := range got.Matches {
			require.NotEqual(t, "", m.ID, q)
		}
		if q == "%" || q == "_" || q == "%%%" {
			require.Empty(t, got.Matches, "wildcards are literal: %q", q)
		}
	}
	// the feed row is flagged
	got, err := f.svc.LookupValidity(ctx, "99/2026/NĐ-CP", 0)
	require.NoError(t, err)
	require.True(t, got.Matches[0].Unverified)
	require.NotEmpty(t, got.Matches[0].UnverifiedNote)
	require.Equal(t, "thuvienphapluat.vn", got.Matches[0].SourceLabel)

	// punctuation around digits is trimmed: "%/2024/%" is the number 2024
	got, err = f.svc.LookupValidity(ctx, "%/2024/%", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"ubnd2024"}, lookupIDs(got))

	var n int64
	require.NoError(t, f.db.Model(&types.LegalDocument{}).Count(&n).Error)
	require.EqualValues(t, 16, n)
}

// snapshotFailingCatalog fails LatestSnapshot and counts the calls.
type snapshotFailingCatalog struct {
	interfaces.LegalCatalogRepository
	calls int
	fail  bool
}

func (c *snapshotFailingCatalog) LatestSnapshot(ctx context.Context) (*time.Time, error) {
	c.calls++
	if c.fail {
		return nil, errors.New("db down")
	}
	return c.LegalCatalogRepository.LatestSnapshot(ctx)
}

func TestLegalValidityLookupSnapshotCache(t *testing.T) {
	f := newLegalLookupFixture(t)
	cat := &snapshotFailingCatalog{LegalCatalogRepository: f.catalog, fail: true}
	f.svc.repo = cat
	now := *lookupDay("2026-11-11")
	f.svc.now = func() time.Time { return now }
	ctx := context.Background()

	require.Equal(t, "", f.svc.snapshotDate(ctx), "a failure gives no date")
	require.Equal(t, "", f.svc.snapshotDate(ctx))
	require.Equal(t, 1, cat.calls, "a failure is cached briefly")
	now = now.Add(legalLookupSnapshotErrorTTL + time.Second)
	cat.fail = false
	require.Equal(t, "2026-11-10", f.svc.snapshotDate(ctx))
	require.Equal(t, 2, cat.calls)
	now = now.Add(legalLookupSnapshotTTL - time.Minute)
	cat.fail = true
	require.Equal(t, "2026-11-10", f.svc.snapshotDate(ctx), "cached")
	require.Equal(t, 2, cat.calls)
	now = now.Add(2 * time.Minute)
	require.Equal(t, "2026-11-10", f.svc.snapshotDate(ctx), "a later failure keeps the last date")
	require.Equal(t, 3, cat.calls)
}

func TestParseLegalLookupQuery(t *testing.T) {
	cases := []struct {
		in      string
		number  string
		slash   bool
		bare    bool
		types   []string
		content []string
		year    int
	}{
		{"30/2020/NĐ-CP", "30/2020/NĐ-CP", true, false, nil, nil, 0},
		{"Nghị định số 30/2020/NĐ-CP", "30/2020/NĐ-CP", true, false, []string{"nghị định"}, nil, 0},
		{"(30/2020/NĐ-CP),", "30/2020/NĐ-CP", true, false, nil, nil, 0},
		{"30/2020", "30/2020", true, false, nil, nil, 0},
		{"ngày 5/3/2020 Nghị định 30/2020/NĐ-CP", "30/2020/NĐ-CP", true, false, nil, []string{"5/3/2020", "Nghị", "định"}, 0},
		{"Nghị định 53", "53", false, true, []string{"nghị định"}, nil, 0},
		{"Bộ luật 100", "100", false, true, []string{"luật", "bộ luật"}, nil, 0},
		{"Nghị định 100 về xử phạt giao thông", "100", false, true, []string{"nghị định"}, []string{"xử", "phạt", "giao", "thông"}, 0},
		{"Luật Đất đai 2024", "2024", false, true, []string{"luật", "bộ luật"}, []string{"Đất", "đai"}, 2024},
		{"chương trình 135", "", false, false, nil, []string{"chương", "trình", "135"}, 0},
		{"Luật Đất đai", "", false, false, []string{"luật", "bộ luật"}, []string{"Đất", "đai"}, 0},
		{"quy định về phí năm 2025", "", false, false, nil, []string{"phí"}, 2025},
	}
	for _, c := range cases {
		pq := parseLegalLookupQuery(c.in)
		require.Equal(t, c.number, pq.number, c.in)
		require.Equal(t, c.slash, pq.slashNumber, c.in)
		require.Equal(t, c.bare, pq.bare, c.in)
		require.Equal(t, c.types, pq.docTypes, c.in)
		require.Equal(t, c.content, pq.content, c.in)
		require.Equal(t, c.year, pq.year, c.in)
	}
}
