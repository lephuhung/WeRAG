package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

// answerQualityFixture: two organizations (1 and 2), one user and one
// session each, answers voted down / gated, and a review.
type answerQualityFixture struct {
	db   *gorm.DB
	repo *answerQualityRepository
	base time.Time
}

func newAnswerQualityFixture(t *testing.T) *answerQualityFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	return answerQualityFixtureOn(t, db)
}

// answerQualityFixtureOn migrates the tables the lists read on db.
func answerQualityFixtureOn(t *testing.T, db *gorm.DB) *answerQualityFixture {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.Session{}, &types.Message{},
		&types.CustomAgent{}, &types.AnswerQualityReview{}))
	return &answerQualityFixture{db: db, repo: &answerQualityRepository{db: db}, base: time.Now().Add(-48 * time.Hour).Truncate(time.Second)}
}

func (f *answerQualityFixture) org(t *testing.T, tenantID uint64, name, userID, username string) string {
	t.Helper()
	require.NoError(t, f.db.Create(&types.Tenant{ID: tenantID, Name: name}).Error)
	require.NoError(t, f.db.Create(&types.User{ID: userID, Username: username, Email: username + "@x.vn", PasswordHash: "x", TenantID: tenantID}).Error)
	require.NoError(t, f.db.Create(&types.CustomAgent{ID: "agent-" + name, TenantID: tenantID, Name: "Trợ lý " + name}).Error)
	return f.session(t, tenantID, userID)
}

// session creates a conversation (Session.BeforeCreate sets its id).
func (f *answerQualityFixture) session(t *testing.T, tenantID uint64, userID string) string {
	t.Helper()
	s := &types.Session{TenantID: tenantID, UserID: userID}
	require.NoError(t, f.db.Create(s).Error)
	return s.ID
}

// turn adds a question and its answer minutes after base; mutate shapes the answer.
func (f *answerQualityFixture) turn(t *testing.T, sessionID string, minutes int, question string, mutate func(*types.Message)) *types.Message {
	t.Helper()
	at := f.base.Add(time.Duration(minutes) * time.Minute)
	require.NoError(t, f.db.Create(&types.Message{ID: uuid.NewString(), SessionID: sessionID, Role: "user", Content: question,
		CreatedAt: at, UpdatedAt: at}).Error)
	a := &types.Message{ID: uuid.NewString(), SessionID: sessionID, Role: "assistant", Content: "Trả lời: " + question,
		CreatedAt: at.Add(time.Second), UpdatedAt: at.Add(time.Second)}
	if mutate != nil {
		mutate(a)
	}
	require.NoError(t, f.db.Create(a).Error)
	return a
}

func disliked(comment string, at time.Time, agent string, tenantID uint64) func(*types.Message) {
	return func(m *types.Message) {
		m.Feedback, m.FeedbackComment, m.FeedbackAt = types.MessageFeedbackDown, comment, &at
		m.AgentID, m.AgentTenantID = agent, tenantID
		m.KnowledgeReferences = types.References{{ID: "c1"}, {ID: "c2"}}
	}
}

func ungrounded(reason, query, legal string) func(*types.Message) {
	return func(m *types.Message) {
		m.ExecutionContext.Ungrounded = types.NewUngroundedMarker(reason, query, legal)
	}
}

func TestAnswerQualityDislikedTenantIsolationAndFilters(t *testing.T) {
	checkAnswerQualityDisliked(t, newAnswerQualityFixture(t))
}

func checkAnswerQualityDisliked(t *testing.T, f *answerQualityFixture) {
	ctx := context.Background()
	sA := f.org(t, 1, "A", "u-a", "nguyenvana")
	sB := f.org(t, 2, "B", "u-b", "tranthib")
	var aIDs []string
	for i := 0; i < 5; i++ {
		comment := ""
		if i%2 == 0 {
			comment = fmt.Sprintf("sai căn cứ %d", i)
		}
		m := f.turn(t, sA, i*10, fmt.Sprintf("Câu hỏi A%d", i), disliked(comment, f.base.Add(time.Duration(i)*time.Hour), "agent-A", 1))
		aIDs = append(aIDs, m.ID)
	}
	f.turn(t, sA, 100, "Câu được thích", func(m *types.Message) { m.Feedback = types.MessageFeedbackUp })
	f.turn(t, sA, 110, "Câu không phản hồi", nil)
	bMsg := f.turn(t, sB, 5, "Câu hỏi B", disliked("tệ", f.base, types.BuiltinSmartReasoningID, 2))
	// a deleted conversation leaves the list
	sGone := f.session(t, 1, "u-a")
	f.turn(t, sGone, 1, "đã xoá", disliked("x", f.base, "", 1))
	require.NoError(t, f.db.Where("id = ?", sGone).Delete(&types.Session{}).Error)

	// organization A sees its 5, never B's
	rows, total, err := f.repo.ListDisliked(ctx, types.AnswerQualityFilter{TenantID: 1, Limit: 2})
	require.NoError(t, err)
	require.EqualValues(t, 5, total)
	require.Len(t, rows, 2)
	require.Equal(t, aIDs[4], rows[0].MessageID, "newest vote first")
	r := rows[0]
	require.Equal(t, "Câu hỏi A4", r.Question)
	require.Equal(t, "Trả lời: Câu hỏi A4", r.Answer)
	require.Equal(t, "A", r.TenantName)
	require.Equal(t, "nguyenvana", r.UserName)
	require.Equal(t, "Trợ lý A", r.AgentName)
	require.Equal(t, 2, r.References)
	require.Equal(t, "sai căn cứ 4", r.Comment)
	require.Nil(t, r.Review)

	// pagination
	page2, _, err := f.repo.ListDisliked(ctx, types.AnswerQualityFilter{TenantID: 1, Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Equal(t, []string{aIDs[2], aIDs[1]}, []string{page2[0].MessageID, page2[1].MessageID})
	page3, _, err := f.repo.ListDisliked(ctx, types.AnswerQualityFilter{TenantID: 1, Limit: 2, Offset: 4})
	require.NoError(t, err)
	require.Len(t, page3, 1)
	for _, p := range [][]*types.AnswerQualityRow{rows, page2, page3} {
		for _, row := range p {
			require.NotEqual(t, bMsg.ID, row.MessageID)
			require.EqualValues(t, 1, row.TenantID)
		}
	}

	// has comment; date range (votes at base+0h..4h)
	_, total, err = f.repo.ListDisliked(ctx, types.AnswerQualityFilter{TenantID: 1, HasComment: true, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	from, to := f.base.Add(time.Hour), f.base.Add(3*time.Hour)
	_, total, err = f.repo.ListDisliked(ctx, types.AnswerQualityFilter{TenantID: 1, From: &from, To: &to, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total, "1h and 2h; 3h excluded")

	// a tenant-scoped filter with no tenant matches nothing
	_, total, err = f.repo.ListDisliked(ctx, types.AnswerQualityFilter{Limit: 10})
	require.NoError(t, err)
	require.Zero(t, total)

	// SuperAdmin: all organizations, or one
	all, total, err := f.repo.ListDisliked(ctx, types.AnswerQualityFilter{AllTenants: true, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 6, total)
	require.Len(t, all, 6)
	onlyB, total, err := f.repo.ListDisliked(ctx, types.AnswerQualityFilter{AllTenants: false, TenantID: 2, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, bMsg.ID, onlyB[0].MessageID)
	if a := types.GetBuiltinAgent(types.BuiltinSmartReasoningID, 2); a != nil {
		require.Equal(t, a.Name, onlyB[0].AgentName, "a built-in agent without a row gets its default name")
	}

	// agent filter + agent choices
	_, total, err = f.repo.ListDisliked(ctx, types.AnswerQualityFilter{AllTenants: true, AgentID: "agent-A", Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 5, total)
	agents, err := f.repo.Agents(ctx, types.AnswerQualityKindDisliked, types.AnswerQualityFilter{TenantID: 1, AgentID: "ignored"})
	require.NoError(t, err)
	require.Equal(t, []*types.AnswerQualityAgent{{ID: "agent-A", Name: "Trợ lý A"}}, agents)

	// reviews: joined, and the reviewed filter
	now := time.Now()
	require.NoError(t, f.repo.SaveReview(ctx, &types.AnswerQualityReview{MessageID: aIDs[4], Kind: types.AnswerQualityKindDisliked,
		TenantID: 1, Cause: types.AnswerQualityCauseWrongBasis, Reviewed: true, ReviewedBy: "u-a", ReviewedAt: &now, UpdatedAt: now}))
	// a review of the other list does not count here
	require.NoError(t, f.repo.SaveReview(ctx, &types.AnswerQualityReview{MessageID: aIDs[3], Kind: types.AnswerQualityKindUngrounded,
		TenantID: 1, Reviewed: true, UpdatedAt: now}))
	yes, no := true, false
	rev, total, err := f.repo.ListDisliked(ctx, types.AnswerQualityFilter{TenantID: 1, Reviewed: &yes, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, types.AnswerQualityCauseWrongBasis, rev[0].Review.Cause)
	require.Equal(t, "nguyenvana", rev[0].Review.ReviewerName)
	_, total, err = f.repo.ListDisliked(ctx, types.AnswerQualityFilter{TenantID: 1, Reviewed: &no, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 4, total)

	// upsert replaces
	require.NoError(t, f.repo.SaveReview(ctx, &types.AnswerQualityReview{MessageID: aIDs[4], Kind: types.AnswerQualityKindDisliked,
		TenantID: 1, Cause: types.AnswerQualityCauseOther, UpdatedAt: now}))
	got, err := f.repo.GetReview(ctx, aIDs[4], types.AnswerQualityKindDisliked)
	require.NoError(t, err)
	require.Equal(t, types.AnswerQualityCauseOther, got.Cause)
	require.False(t, got.Reviewed)
	none, err := f.repo.GetReview(ctx, aIDs[0], types.AnswerQualityKindDisliked)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestAnswerQualityUngroundedListExcludesFailedTurns(t *testing.T) {
	checkAnswerQualityUngrounded(t, newAnswerQualityFixture(t))
}

func checkAnswerQualityUngrounded(t *testing.T, f *answerQualityFixture) {
	ctx := context.Background()
	sA := f.org(t, 1, "A", "u-a", "nguyenvana")
	sB := f.org(t, 2, "B", "u-b", "tranthib")
	m1 := f.turn(t, sA, 1, "Giá iPhone 18?", ungrounded(types.UngroundedNoEvidence, "Giá iPhone 18?", ""))
	m2 := f.turn(t, sA, 2, "Nghị định 99/2099/NĐ-CP?", ungrounded(types.UngroundedLegalDocumentMissing, "Nghị định 99/2099/NĐ-CP?", "99/2099/NĐ-CP"))
	f.turn(t, sA, 3, "lỗi mô hình", func(m *types.Message) {
		ungrounded(types.UngroundedNoEvidence, "lỗi mô hình", "")(m)
		m.ExecutionContext.TurnFailure = types.TurnFailureModelUnavailable
	})
	f.turn(t, sA, 4, "bình thường", nil)
	mB := f.turn(t, sB, 5, "Câu của B", ungrounded(types.UngroundedNoEvidence, "Câu của B", ""))

	rows, total, err := f.repo.ListUngrounded(ctx, types.AnswerQualityFilter{TenantID: 1, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total, "the failed turn and the normal answer are out")
	require.Equal(t, m2.ID, rows[0].MessageID)
	require.Equal(t, types.UngroundedLegalDocumentMissing, rows[0].Reason)
	require.Equal(t, "99/2099/NĐ-CP", rows[0].LegalRequest)
	require.Equal(t, m1.ID, rows[1].MessageID)

	_, total, err = f.repo.ListUngrounded(ctx, types.AnswerQualityFilter{TenantID: 1, Reason: types.UngroundedNoEvidence, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)

	all, total, err := f.repo.ListUngrounded(ctx, types.AnswerQualityFilter{AllTenants: true, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Equal(t, mB.ID, all[0].MessageID)

	markers, err := f.repo.UngroundedMarkers(ctx, types.AnswerQualityFilter{TenantID: 1}, 10)
	require.NoError(t, err)
	require.Len(t, markers, 2)
	require.Equal(t, "Nghị định 99/2099/NĐ-CP?", markers[0].Question)

	target, err := f.repo.Target(ctx, m1.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, target.TenantID)
	require.True(t, target.Ungrounded)
	require.Equal(t, "assistant", target.Role)
	missing, err := f.repo.Target(ctx, "nope")
	require.NoError(t, err)
	require.Nil(t, missing)
}

// The question is the user message of the answer's request; a message the
// user sent while the answer ran (steer) does not take its place. A vote
// without feedback_at is ordered by the answer's time.
func TestAnswerQualityQuestionPairingAndOrder(t *testing.T) {
	checkAnswerQualityPairing(t, newAnswerQualityFixture(t))
}

func checkAnswerQualityPairing(t *testing.T, f *answerQualityFixture) {
	ctx := context.Background()
	s := f.org(t, 1, "A", "u-a", "nguyenvana")
	at := f.base
	add := func(m *types.Message) {
		t.Helper()
		m.ID, m.SessionID = uuid.NewString(), s
		require.NoError(t, f.db.Create(m).Error)
	}
	add(&types.Message{Role: "user", Content: "Câu hỏi gốc", RequestID: "r1", CreatedAt: at})
	add(&types.Message{Role: "user", Content: "Câu chen vào", RequestID: "r2", CreatedAt: at.Add(time.Second)})
	voted := at.Add(time.Hour)
	add(&types.Message{Role: "assistant", Content: "Trả lời", RequestID: "r1", CreatedAt: at.Add(2 * time.Second),
		Feedback: types.MessageFeedbackDown, FeedbackAt: &voted})
	// an older-style vote with no feedback_at, answer later than the vote above
	add(&types.Message{Role: "user", Content: "Câu cũ", CreatedAt: at.Add(2 * time.Hour)})
	add(&types.Message{Role: "assistant", Content: "Trả lời cũ", CreatedAt: at.Add(2*time.Hour + time.Second),
		Feedback: types.MessageFeedbackDown})

	rows, total, err := f.repo.ListDisliked(ctx, types.AnswerQualityFilter{TenantID: 1, Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Equal(t, "Câu cũ", rows[0].Question, "no feedback_at: ordered by the answer's time, paired by time")
	require.Equal(t, "Câu hỏi gốc", rows[1].Question, "paired by request_id, not the later steer message")
}
