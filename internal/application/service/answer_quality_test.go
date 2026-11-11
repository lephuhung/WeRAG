package service

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type aqFixture struct {
	db  *gorm.DB
	svc *answerQualityService
	// per organization: a disliked answer and an ungrounded one
	disliked   map[uint64]string
	ungrounded map[uint64]string
}

func newAQFixture(t *testing.T) *aqFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Message{}, &types.Session{}, &types.Tenant{}, &types.User{},
		&types.CustomAgent{}, &types.AnswerQualityReview{}))
	f := &aqFixture{db: db, svc: NewAnswerQualityService(repository.NewAnswerQualityRepository(db)).(*answerQualityService),
		disliked: map[uint64]string{}, ungrounded: map[uint64]string{}}
	now := time.Now().Add(-time.Hour)
	for _, tid := range []uint64{1, 2} {
		require.NoError(t, db.Create(&types.Tenant{ID: tid, Name: "org"}).Error)
		s := &types.Session{TenantID: tid, UserID: "u"}
		require.NoError(t, db.Create(s).Error)
		add := func(m *types.Message) string {
			m.ID, m.SessionID, m.Role = uuid.NewString(), s.ID, "assistant"
			require.NoError(t, db.Create(m).Error)
			return m.ID
		}
		f.disliked[tid] = add(&types.Message{Feedback: types.MessageFeedbackDown, FeedbackAt: &now, Content: "a"})
		f.ungrounded[tid] = add(&types.Message{Content: "b", ExecutionContext: types.MessageExecutionContext{
			Ungrounded: types.NewUngroundedMarker(types.UngroundedNoEvidence, "Thủ tục cấp sổ đỏ?", "")}})
	}
	return f
}

func aqCtx(tenantID uint64, userID string, role types.TenantRole, superAdmin bool) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, superAdmin)
	return types.WithCaller(ctx, types.Caller{TenantID: tenantID, UserID: userID, Role: role})
}

func requireStatus(t *testing.T, err error, status int) {
	t.Helper()
	var appErr *apperrors.AppError
	require.True(t, stderrors.As(err, &appErr), "want an AppError, got %v", err)
	require.Equal(t, status, appErr.HTTPCode)
}

func TestAnswerQualityScope(t *testing.T) {
	f := newAQFixture(t)
	admin1 := aqCtx(1, "admin-1", types.TenantRoleAdmin, false)

	// a Tenant Admin reads their own organization, whatever tenant_id asks
	page, err := f.svc.ListDisliked(admin1, false, types.AnswerQualityFilter{TenantID: 2, AllTenants: true})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, f.disliked[1], page.Items[0].MessageID)
	page, err = f.svc.ListUngrounded(admin1, false, types.AnswerQualityFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, f.ungrounded[1], page.Items[0].MessageID)

	// members, guests (no role), API keys: refused; an admin of another
	// organization than the request's: refused
	for name, ctx := range map[string]context.Context{
		"member":             aqCtx(1, "m", types.TenantRoleMember, false),
		"admin without user": aqCtx(1, "", types.TenantRoleAdmin, false),
		"guest":              aqCtx(1, "g", "", false),
		"api key":            types.WithTenantAPIKeyScope(admin1, types.TenantAPIKeyScope{KeyID: 1, FullAccess: true}),
		"other tenant":       types.WithCaller(admin1, types.Caller{TenantID: 2, UserID: "admin-2", Role: types.TenantRoleAdmin}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.ListDisliked(ctx, false, types.AnswerQualityFilter{})
			requireStatus(t, err, http.StatusForbidden)
			_, err = f.svc.GroupUngrounded(ctx, false, types.AnswerQualityFilter{})
			requireStatus(t, err, http.StatusForbidden)
			_, err = f.svc.Review(ctx, false, f.disliked[1], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindDisliked})
			requireStatus(t, err, http.StatusForbidden)
		})
	}
	// the console is SuperAdmin-only
	_, err = f.svc.ListDisliked(admin1, true, types.AnswerQualityFilter{})
	requireStatus(t, err, http.StatusForbidden)

	// SuperAdmin: every organization, or one
	super := aqCtx(1, "root", "", true)
	page, err = f.svc.ListDisliked(super, true, types.AnswerQualityFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	page, err = f.svc.ListDisliked(super, true, types.AnswerQualityFilter{TenantID: 2})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, f.disliked[2], page.Items[0].MessageID)
	// a SuperAdmin visiting an organization reads that organization only
	page, err = f.svc.ListUngrounded(super, false, types.AnswerQualityFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)

	groups, err := f.svc.GroupUngrounded(super, true, types.AnswerQualityFilter{})
	require.NoError(t, err)
	require.Len(t, groups.Items, 1)
	require.Equal(t, 2, groups.Items[0].Count)
	require.Equal(t, 2, groups.Items[0].Organizations)
	groups, err = f.svc.GroupUngrounded(admin1, false, types.AnswerQualityFilter{})
	require.NoError(t, err)
	require.Equal(t, 1, groups.Items[0].Count, "an organization's grouping counts only its own")
}

func TestAnswerQualityReview(t *testing.T) {
	f := newAQFixture(t)
	admin1 := aqCtx(1, "admin-1", types.TenantRoleAdmin, false)
	cause := types.AnswerQualityCauseWrongBasis
	yes, no := true, false

	// another organization's message is not found
	_, err := f.svc.Review(admin1, false, f.disliked[2], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindDisliked, Cause: &cause})
	requireStatus(t, err, http.StatusNotFound)
	// unknown cause / kind / message not in the list
	bad := "made_up"
	_, err = f.svc.Review(admin1, false, f.disliked[1], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindDisliked, Cause: &bad})
	requireStatus(t, err, http.StatusBadRequest)
	kb := types.AnswerQualityCauseKBMissing
	_, err = f.svc.Review(admin1, false, f.disliked[1], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindDisliked, Cause: &kb})
	requireStatus(t, err, http.StatusBadRequest)
	_, err = f.svc.Review(admin1, false, f.disliked[1], types.AnswerQualityReviewInput{Kind: "x"})
	requireStatus(t, err, http.StatusBadRequest)
	_, err = f.svc.Review(admin1, false, f.ungrounded[1], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindDisliked, Reviewed: &yes})
	requireStatus(t, err, http.StatusBadRequest)

	rv, err := f.svc.Review(admin1, false, f.disliked[1], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindDisliked, Cause: &cause})
	require.NoError(t, err)
	require.Equal(t, cause, rv.Cause)
	require.False(t, rv.Reviewed)
	require.EqualValues(t, 1, rv.TenantID)
	rv, err = f.svc.Review(admin1, false, f.disliked[1], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindDisliked, Reviewed: &yes})
	require.NoError(t, err)
	require.Equal(t, cause, rv.Cause, "a partial update keeps the cause")
	require.True(t, rv.Reviewed)
	require.Equal(t, "admin-1", rv.ReviewedBy)
	require.NotNil(t, rv.ReviewedAt)

	page, err := f.svc.ListDisliked(admin1, false, types.AnswerQualityFilter{Reviewed: &yes})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, cause, page.Items[0].Review.Cause)

	rv, err = f.svc.Review(admin1, false, f.disliked[1], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindDisliked, Reviewed: &no})
	require.NoError(t, err)
	require.False(t, rv.Reviewed)
	require.Empty(t, rv.ReviewedBy)
	require.Nil(t, rv.ReviewedAt)

	// the user's message is untouched
	var m types.Message
	require.NoError(t, f.db.Where("id = ?", f.disliked[1]).First(&m).Error)
	require.Equal(t, types.MessageFeedbackDown, m.Feedback)
	require.Equal(t, "a", m.Content)

	// SuperAdmin reviews any organization; the ungrounded list has its own tags
	super := aqCtx(1, "root", "", true)
	_, err = f.svc.Review(super, true, f.ungrounded[2], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindUngrounded, Cause: &kb, Reviewed: &yes})
	require.NoError(t, err)
	page, err = f.svc.ListUngrounded(super, true, types.AnswerQualityFilter{TenantID: 2})
	require.NoError(t, err)
	require.Equal(t, kb, page.Items[0].Review.Cause)
	require.True(t, page.Items[0].Review.Reviewed)
}

func TestNormalizeQuestionAndGrouping(t *testing.T) {
	require.Equal(t, "thủ tục cấp sổ đỏ", NormalizeQuestion("  Thủ tục cấp SỔ ĐỎ??? "))
	require.Equal(t, NormalizeQuestion("Thủ tục cấp sổ đỏ"), NormalizeQuestion("thủ tục, cấp sổ đỏ."))
	require.Equal(t, "nghị định 53/2022/nđ-cp quy định gì", NormalizeQuestion("Nghị định 53/2022/NĐ-CP quy định gì?"))
	require.NotEqual(t, NormalizeQuestion("bán"), NormalizeQuestion("ban"), "diacritics are kept")
	// NFD input groups with NFC
	require.Equal(t, NormalizeQuestion("sổ đỏ"), NormalizeQuestion("sổ đỏ"))

	base := time.Now()
	rows := []*types.AnswerQualityRow{
		{MessageID: "3", CreatedAt: base, TenantID: 1, Question: "Giá iPhone 18?", Reason: types.UngroundedNoEvidence},
		{MessageID: "2", CreatedAt: base.Add(-time.Minute), TenantID: 2, Question: "Nghị định 99/2099/NĐ-CP?",
			Reason: types.UngroundedLegalDocumentMissing, LegalRequest: "99/2099/NĐ-CP"},
		{MessageID: "1", CreatedAt: base.Add(-2 * time.Minute), TenantID: 1, Question: "nghị định 99/2099/nđ-cp", Reason: types.UngroundedNoEvidence},
		{MessageID: "0", CreatedAt: base.Add(-3 * time.Minute), TenantID: 1, Question: "   "},
	}
	groups := GroupUngroundedQuestions(rows)
	require.Len(t, groups, 2)
	require.Equal(t, 2, groups[0].Count)
	require.Equal(t, "2", groups[0].MessageID, "latest row of the group")
	require.Equal(t, "Nghị định 99/2099/NĐ-CP?", groups[0].Sample)
	require.Equal(t, "99/2099/NĐ-CP", groups[0].LegalRequest)
	require.Equal(t, 2, groups[0].Organizations)
	require.Equal(t, map[string]int{types.UngroundedLegalDocumentMissing: 1, types.UngroundedNoEvidence: 1}, groups[0].Reasons)
	require.Equal(t, 1, groups[1].Count)
}

func TestGroupUngroundedPaging(t *testing.T) {
	f := newAQFixture(t)
	super := aqCtx(1, "root", "", true)
	page, err := f.svc.GroupUngrounded(super, true, types.AnswerQualityFilter{Limit: 1, Offset: 5})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Equal(t, 1, page.Total)
	require.Equal(t, 2, page.Rows)
}

// A fork copies the conversation, not the admins' ungrounded mark.
func TestForkedCopiesDoNotInheritUngroundedMarker(t *testing.T) {
	src := &types.Message{ID: "m1", Role: "assistant", ExecutionContext: types.MessageExecutionContext{
		Locale:     "vi-VN",
		Ungrounded: types.NewUngroundedMarker(types.UngroundedNoEvidence, "q", ""),
	}}
	copies := copyMessagesInto("fork", []*types.Message{src})
	require.Nil(t, copies[0].ExecutionContext.Ungrounded)
	require.Equal(t, "vi-VN", copies[0].ExecutionContext.Locale)
	require.NotNil(t, src.ExecutionContext.Ungrounded, "the parent keeps its mark")
}

type countingAQRepo struct {
	interfaces.AnswerQualityRepository
	markerCalls int
}

func (r *countingAQRepo) UngroundedMarkers(ctx context.Context, f types.AnswerQualityFilter, limit int) ([]*types.AnswerQualityRow, error) {
	r.markerCalls++
	return r.AnswerQualityRepository.UngroundedMarkers(ctx, f, limit)
}

// The grouping is cached per organization and filter for a minute; paging
// reuses it, another organization or filter does not, a review clears it.
func TestGroupUngroundedIsCached(t *testing.T) {
	f := newAQFixture(t)
	repo := &countingAQRepo{AnswerQualityRepository: repository.NewAnswerQualityRepository(f.db)}
	now := time.Now()
	svc := NewAnswerQualityService(repo).(*answerQualityService)
	svc.now = func() time.Time { return now }
	admin1 := aqCtx(1, "admin-1", types.TenantRoleAdmin, false)
	admin2 := aqCtx(2, "admin-2", types.TenantRoleAdmin, false)

	_, err := svc.GroupUngrounded(admin1, false, types.AnswerQualityFilter{Limit: 10})
	require.NoError(t, err)
	_, err = svc.GroupUngrounded(admin1, false, types.AnswerQualityFilter{Limit: 10, Offset: 10})
	require.NoError(t, err)
	require.Equal(t, 1, repo.markerCalls, "paging reuses the grouping")

	page, err := svc.GroupUngrounded(admin2, false, types.AnswerQualityFilter{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 2, repo.markerCalls, "another organization is grouped on its own")
	require.Equal(t, 1, page.Items[0].Count)
	_, err = svc.GroupUngrounded(admin1, false, types.AnswerQualityFilter{Limit: 10, Reason: types.UngroundedLegalDocumentMissing})
	require.NoError(t, err)
	require.Equal(t, 3, repo.markerCalls, "another filter is grouped on its own")

	now = now.Add(61 * time.Second)
	_, err = svc.GroupUngrounded(admin1, false, types.AnswerQualityFilter{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 4, repo.markerCalls, "expired after a minute")

	yes := true
	_, err = svc.Review(admin1, false, f.ungrounded[1], types.AnswerQualityReviewInput{Kind: types.AnswerQualityKindUngrounded, Reviewed: &yes})
	require.NoError(t, err)
	_, err = svc.GroupUngrounded(admin1, false, types.AnswerQualityFilter{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 5, repo.markerCalls, "a review clears the cache")
}
