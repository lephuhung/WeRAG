package repository

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// answerQualityRepository reads the admins' answer-quality lists over
// messages (joined to their session for the organization) and stores the
// reviews (answer_quality_reviews, migration 000197).
type answerQualityRepository struct {
	db *gorm.DB
}

// NewAnswerQualityRepository creates the repository.
func NewAnswerQualityRepository(db *gorm.DB) interfaces.AnswerQualityRepository {
	return &answerQualityRepository{db: db}
}

func (r *answerQualityRepository) postgres() bool {
	return r.db.Dialector != nil && r.db.Dialector.Name() == "postgres"
}

// ungroundedSQL is the condition of the ungrounded list: a marker and no
// failure. The Postgres form matches the partial index
// idx_messages_ungrounded.
func (r *answerQualityRepository) ungroundedSQL() string {
	if r.postgres() {
		return "(m.execution_context -> 'ungrounded') IS NOT NULL" +
			" AND COALESCE(m.execution_context ->> 'turn_failure', '') = ''"
	}
	return "json_extract(CAST(m.execution_context AS TEXT), '$.ungrounded') IS NOT NULL" +
		" AND COALESCE(json_extract(CAST(m.execution_context AS TEXT), '$.turn_failure'), '') = ''"
}

func (r *answerQualityRepository) reasonSQL() string {
	if r.postgres() {
		return "(m.execution_context -> 'ungrounded' ->> 'reason') = ?"
	}
	return "json_extract(CAST(m.execution_context AS TEXT), '$.ungrounded.reason') = ?"
}

func (r *answerQualityRepository) referencesSQL() string {
	if r.postgres() {
		return "CASE WHEN jsonb_typeof(m.knowledge_references) = 'array'" +
			" THEN jsonb_array_length(m.knowledge_references) ELSE 0 END"
	}
	return "COALESCE(json_array_length(CAST(m.knowledge_references AS TEXT)), 0)"
}

// where builds the WHERE clause of a list. timeCol is the column the date
// range applies to.
func (r *answerQualityRepository) where(kind string, f types.AnswerQualityFilter, withAgent bool) (string, []interface{}) {
	conds := []string{"m.deleted_at IS NULL", "m.role = 'assistant'"}
	var args []interface{}
	timeCol := "m.created_at"
	switch kind {
	case types.AnswerQualityKindDisliked:
		timeCol = "m.feedback_at"
		conds = append(conds, "m.feedback = ?")
		args = append(args, types.MessageFeedbackDown)
		if f.HasComment {
			conds = append(conds, "TRIM(m.feedback_comment) <> ''")
		}
	case types.AnswerQualityKindUngrounded:
		conds = append(conds, r.ungroundedSQL())
		if f.Reason != "" {
			conds = append(conds, r.reasonSQL())
			args = append(args, f.Reason)
		}
	}
	if !f.AllTenants || f.TenantID != 0 {
		// a tenant-scoped read with no tenant matches nothing
		conds = append(conds, "s.tenant_id = ?")
		args = append(args, f.TenantID)
	}
	if f.From != nil {
		conds = append(conds, timeCol+" >= ?")
		args = append(args, *f.From)
	}
	if f.To != nil {
		conds = append(conds, timeCol+" < ?")
		args = append(args, *f.To)
	}
	if withAgent && f.AgentID != "" {
		conds = append(conds, "m.agent_id = ?")
		args = append(args, f.AgentID)
	}
	if f.Reviewed != nil {
		if *f.Reviewed {
			conds = append(conds, "rv.reviewed = ?")
			args = append(args, true)
		} else {
			conds = append(conds, "(rv.reviewed IS NULL OR rv.reviewed = ?)")
			args = append(args, false)
		}
	}
	return strings.Join(conds, " AND "), args
}

// from is the join every list reads: the session (organization, owner;
// a deleted conversation is gone from the lists too) and the review of the
// list's kind (bound first).
const answerQualityFrom = ` FROM messages m
JOIN sessions s ON s.id = m.session_id AND s.deleted_at IS NULL
LEFT JOIN answer_quality_reviews rv ON rv.message_id = m.id AND rv.kind = ?`

type answerQualityScan struct {
	MessageID        string
	SessionID        string
	CreatedAt        time.Time
	TenantID         uint64
	TenantName       *string
	UserID           *string
	UserName         *string
	UserEmail        *string
	AgentID          string
	AgentTenantID    uint64
	AgentName        *string
	Answer           string
	Refs             int
	Feedback         int
	Comment          string
	FeedbackAt       *time.Time
	ExecutionContext types.MessageExecutionContext
	Question         *string
	RvCause          *string
	RvNote           *string
	RvReviewed       *bool
	RvReviewedBy     *string
	RvReviewedAt     *time.Time
	ReviewerName     *string
}

func (r *answerQualityRepository) list(
	ctx context.Context, kind string, f types.AnswerQualityFilter,
) ([]*types.AnswerQualityRow, int64, error) {
	where, args := r.where(kind, f, true)
	countArgs := append([]interface{}{kind}, args...)
	var total int64
	if err := r.db.WithContext(ctx).Raw("SELECT COUNT(*)"+answerQualityFrom+" WHERE "+where, countArgs...).
		Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*types.AnswerQualityRow{}, 0, nil
	}
	order := "m.created_at DESC, m.id"
	if kind == types.AnswerQualityKindDisliked {
		order = "m.feedback_at DESC, m.id"
	}
	limit, offset := f.Limit, f.Offset
	if limit <= 0 || limit > types.AnswerQualityListMax {
		limit = types.AnswerQualityListMax
	}
	if offset < 0 {
		offset = 0
	}
	query := `SELECT m.id AS message_id, m.session_id, m.created_at, s.tenant_id,
  t.name AS tenant_name, s.user_id, u.username AS user_name, u.email AS user_email,
  m.agent_id, m.agent_tenant_id, ca.name AS agent_name,
  m.content AS answer, ` + r.referencesSQL() + ` AS refs,
  m.feedback, m.feedback_comment AS comment, m.feedback_at, m.execution_context,
  (SELECT q.content FROM messages q
    WHERE q.session_id = m.session_id AND q.role = 'user' AND q.deleted_at IS NULL
      AND q.created_at <= m.created_at
    ORDER BY q.created_at DESC LIMIT 1) AS question,
  rv.cause AS rv_cause, rv.note AS rv_note, rv.reviewed AS rv_reviewed,
  rv.reviewed_by AS rv_reviewed_by, rv.reviewed_at AS rv_reviewed_at, ru.username AS reviewer_name` +
		answerQualityFrom + `
LEFT JOIN tenants t ON t.id = s.tenant_id
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN custom_agents ca ON ca.id = m.agent_id AND ca.tenant_id = m.agent_tenant_id
LEFT JOIN users ru ON ru.id = rv.reviewed_by
WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{kind}, args...), limit, offset)
	var scans []answerQualityScan
	if err := r.db.WithContext(ctx).Raw(query, pageArgs...).Scan(&scans).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]*types.AnswerQualityRow, 0, len(scans))
	for i := range scans {
		rows = append(rows, scans[i].row())
	}
	return rows, total, nil
}

func (s *answerQualityScan) row() *types.AnswerQualityRow {
	row := &types.AnswerQualityRow{
		MessageID:  s.MessageID,
		SessionID:  s.SessionID,
		CreatedAt:  s.CreatedAt,
		TenantID:   s.TenantID,
		TenantName: deref(s.TenantName),
		UserID:     deref(s.UserID),
		UserName:   deref(s.UserName),
		AgentID:    s.AgentID,
		AgentName:  agentDisplayName(s.AgentID, s.AgentTenantID, deref(s.AgentName)),
		Question:   deref(s.Question),
		References: s.Refs,
		Feedback:   s.Feedback,
		Comment:    s.Comment,
		FeedbackAt: s.FeedbackAt,
	}
	if row.UserName == "" {
		row.UserName = deref(s.UserEmail)
	}
	row.Answer = s.Answer
	if utf8.RuneCountInString(row.Answer) > types.AnswerQualityAnswerMaxRunes {
		row.Answer = string([]rune(row.Answer)[:types.AnswerQualityAnswerMaxRunes])
		row.AnswerCut = true
	}
	if mk := s.ExecutionContext.Ungrounded; mk != nil {
		row.Reason = mk.Reason
		row.LegalRequest = mk.LegalRequest
		if row.Question == "" {
			row.Question = mk.Query
		}
	}
	if s.RvReviewed != nil {
		row.Review = &types.AnswerQualityReviewView{
			Cause:        deref(s.RvCause),
			Note:         deref(s.RvNote),
			Reviewed:     *s.RvReviewed,
			ReviewedBy:   deref(s.RvReviewedBy),
			ReviewerName: deref(s.ReviewerName),
			ReviewedAt:   s.RvReviewedAt,
		}
	}
	return row
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// agentDisplayName is the stored agent's name, or a built-in's default
// name when the tenant keeps no row for it.
func agentDisplayName(id string, tenantID uint64, name string) string {
	if name != "" || id == "" {
		return name
	}
	if a := types.GetBuiltinAgent(id, tenantID); a != nil {
		return a.Name
	}
	return ""
}

func (r *answerQualityRepository) ListDisliked(
	ctx context.Context, f types.AnswerQualityFilter,
) ([]*types.AnswerQualityRow, int64, error) {
	return r.list(ctx, types.AnswerQualityKindDisliked, f)
}

func (r *answerQualityRepository) ListUngrounded(
	ctx context.Context, f types.AnswerQualityFilter,
) ([]*types.AnswerQualityRow, int64, error) {
	return r.list(ctx, types.AnswerQualityKindUngrounded, f)
}

func (r *answerQualityRepository) Agents(
	ctx context.Context, kind string, f types.AnswerQualityFilter,
) ([]*types.AnswerQualityAgent, error) {
	where, args := r.where(kind, f, false)
	var scans []struct {
		AgentID       string
		AgentTenantID uint64
		AgentName     *string
	}
	err := r.db.WithContext(ctx).Raw(`SELECT DISTINCT m.agent_id, m.agent_tenant_id, ca.name AS agent_name`+
		answerQualityFrom+`
LEFT JOIN custom_agents ca ON ca.id = m.agent_id AND ca.tenant_id = m.agent_tenant_id
WHERE `+where+` AND m.agent_id <> '' LIMIT 500`, append([]interface{}{kind}, args...)...).Scan(&scans).Error
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]*types.AnswerQualityAgent, 0, len(scans))
	for _, s := range scans {
		if seen[s.AgentID] {
			continue
		}
		seen[s.AgentID] = true
		out = append(out, &types.AnswerQualityAgent{
			ID: s.AgentID, Name: agentDisplayName(s.AgentID, s.AgentTenantID, deref(s.AgentName)),
		})
	}
	return out, nil
}

func (r *answerQualityRepository) UngroundedMarkers(
	ctx context.Context, f types.AnswerQualityFilter, limit int,
) ([]*types.AnswerQualityRow, error) {
	where, args := r.where(types.AnswerQualityKindUngrounded, f, true)
	if limit <= 0 {
		limit = types.UngroundedGroupScanMax
	}
	var scans []struct {
		MessageID        string
		CreatedAt        time.Time
		TenantID         uint64
		ExecutionContext types.MessageExecutionContext
	}
	err := r.db.WithContext(ctx).Raw(`SELECT m.id AS message_id, m.created_at, s.tenant_id, m.execution_context`+
		answerQualityFrom+` WHERE `+where+` ORDER BY m.created_at DESC, m.id LIMIT ?`,
		append(append([]interface{}{types.AnswerQualityKindUngrounded}, args...), limit)...).Scan(&scans).Error
	if err != nil {
		return nil, err
	}
	out := make([]*types.AnswerQualityRow, 0, len(scans))
	for _, s := range scans {
		row := &types.AnswerQualityRow{MessageID: s.MessageID, CreatedAt: s.CreatedAt, TenantID: s.TenantID}
		if mk := s.ExecutionContext.Ungrounded; mk != nil {
			row.Question, row.Reason, row.LegalRequest = mk.Query, mk.Reason, mk.LegalRequest
		}
		out = append(out, row)
	}
	return out, nil
}

func (r *answerQualityRepository) Target(ctx context.Context, messageID string) (*interfaces.AnswerQualityTarget, error) {
	var scans []struct {
		MessageID        string
		TenantID         uint64
		Role             string
		Feedback         int
		ExecutionContext types.MessageExecutionContext
	}
	err := r.db.WithContext(ctx).Raw(`SELECT m.id AS message_id, s.tenant_id, m.role, m.feedback, m.execution_context
FROM messages m JOIN sessions s ON s.id = m.session_id AND s.deleted_at IS NULL
WHERE m.id = ? AND m.deleted_at IS NULL LIMIT 1`, messageID).Scan(&scans).Error
	if err != nil || len(scans) == 0 {
		return nil, err
	}
	s := scans[0]
	return &interfaces.AnswerQualityTarget{
		MessageID: s.MessageID, TenantID: s.TenantID, Role: s.Role, Feedback: s.Feedback,
		Ungrounded: s.ExecutionContext.Ungrounded != nil && s.ExecutionContext.TurnFailure == "",
	}, nil
}

func (r *answerQualityRepository) GetReview(ctx context.Context, messageID, kind string) (*types.AnswerQualityReview, error) {
	var rows []types.AnswerQualityReview
	if err := r.db.WithContext(ctx).Where("message_id = ? AND kind = ?", messageID, kind).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *answerQualityRepository) SaveReview(ctx context.Context, rv *types.AnswerQualityReview) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "message_id"}, {Name: "kind"}},
		DoUpdates: clause.AssignmentColumns([]string{"tenant_id", "cause", "note", "reviewed", "reviewed_by", "reviewed_at", "updated_by", "updated_at"}),
	}).Create(rv).Error
}
