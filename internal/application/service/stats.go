package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// statsService answers the SystemAdmin dashboard with plain aggregate
// queries over the primary database. It intentionally bypasses the
// per-tenant repositories: the caller is already gated to SystemAdmin and
// the numbers are deliberately platform-wide.
type statsService struct {
	db    *gorm.DB
	usage interfaces.UsageMeter
}

// NewStatsService creates the stats service.
func NewStatsService(db *gorm.DB, usage interfaces.UsageMeter) interfaces.StatsService {
	return &statsService{db: db, usage: usage}
}

func (s *statsService) GetSystemStats(ctx context.Context, days int) (*types.SystemStats, error) {
	if days <= 0 {
		days = 90
	}
	stats := &types.SystemStats{}
	if err := s.loadAccountStats(ctx, stats); err != nil {
		return nil, err
	}
	if err := s.loadDocumentStats(ctx, stats); err != nil {
		return nil, err
	}
	if err := s.loadMessageStats(ctx, stats, days); err != nil {
		return nil, err
	}
	if err := s.loadChatWindowStats(ctx, stats, 24); err != nil {
		return nil, err
	}
	if err := s.loadFeedbackStats(ctx, stats); err != nil {
		return nil, err
	}
	if err := s.loadPublicKBStats(ctx, stats); err != nil {
		return nil, err
	}
	if s.usage != nil {
		totals, err := s.usage.PlatformTotals(ctx)
		if err != nil {
			return nil, fmt.Errorf("usage totals: %w", err)
		}
		stats.Usage = *totals
	}
	return stats, nil
}

func (s *statsService) loadAccountStats(ctx context.Context, stats *types.SystemStats) error {
	db := s.db.WithContext(ctx)
	count := func(query *gorm.DB) (int64, error) {
		var n int64
		err := query.Count(&n).Error
		return n, err
	}
	var err error
	if stats.Accounts.TotalUsers, err = count(db.Model(&types.User{})); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if stats.Accounts.ActiveUsers, err = count(
		db.Model(&types.User{}).Where("is_active = ?", true)); err != nil {
		return fmt.Errorf("count active users: %w", err)
	}
	if stats.Accounts.SystemAdmins, err = count(
		db.Model(&types.User{}).Where("is_system_admin = ?", true)); err != nil {
		return fmt.Errorf("count system admins: %w", err)
	}
	if stats.Accounts.TotalTenants, err = count(db.Model(&types.Tenant{}).Where("kind = ?", types.TenantKindWorkspace)); err != nil {
		return fmt.Errorf("count tenants: %w", err)
	}
	if stats.Accounts.NewUsersLast30d, err = count(
		db.Model(&types.User{}).Where("created_at >= ?", time.Now().AddDate(0, 0, -30))); err != nil {
		return fmt.Errorf("count new users: %w", err)
	}
	stats.Accounts.InactiveUsers = stats.Accounts.TotalUsers - stats.Accounts.ActiveUsers
	return nil
}

func (s *statsService) loadDocumentStats(ctx context.Context, stats *types.SystemStats) error {
	db := s.db.WithContext(ctx)
	rows := make([]types.DocumentStatusCount, 0)
	err := db.Model(&types.Knowledge{}).
		Select("parse_status AS status, COUNT(*) AS count").
		Group("parse_status").
		Order("count DESC").
		Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("group documents by status: %w", err)
	}
	stats.Documents.ByStatus = rows
	for _, r := range rows {
		stats.Documents.Total += r.Count
	}
	return nil
}

func (s *statsService) loadMessageStats(ctx context.Context, stats *types.SystemStats, days int) error {
	db := s.db.WithContext(ctx)
	var err error
	if err = db.Model(&types.Message{}).Count(&stats.Messages.Total).Error; err != nil {
		return fmt.Errorf("count messages: %w", err)
	}
	if err = db.Model(&types.Session{}).Count(&stats.Messages.TotalSessions).Error; err != nil {
		return fmt.Errorf("count sessions: %w", err)
	}

	// Calendar-day bucket expression, per dialect. Everything returns a
	// 'YYYY-MM-DD' string so the result scans into MessageDayActivity.Date.
	// Postgres buckets by the Vietnam day: the session runs in UTC.
	dayExpr := "date(created_at)"
	switch db.Dialector.Name() {
	case "postgres":
		dayExpr = "to_char(created_at AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYYY-MM-DD')"
	case "mysql":
		dayExpr = "DATE(created_at)"
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	rows := make([]types.MessageDayActivity, 0)
	err = db.Model(&types.Message{}).
		Select(
			dayExpr+" AS date",
			"COUNT(*) AS count",
			"SUM(CASE WHEN role = 'user' THEN 1 ELSE 0 END) AS user_count",
			"SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END) AS assistant_count",
		).
		Where("created_at >= ?", cutoff).
		Group(dayExpr).
		Order("date").
		Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("group messages by day: %w", err)
	}
	stats.Messages.Days = days
	stats.Messages.ByDay = rows
	return nil
}

func (s *statsService) loadChatWindowStats(ctx context.Context, stats *types.SystemStats, hours int) error {
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	var row struct {
		Questions      int64
		Answers        int64
		ActiveSessions int64
		ActiveUsers    int64
	}
	err := s.db.WithContext(ctx).Raw(`
SELECT
  COALESCE(SUM(CASE WHEN m.role = 'user' THEN 1 ELSE 0 END), 0) AS questions,
  COALESCE(SUM(CASE WHEN m.role = 'assistant' THEN 1 ELSE 0 END), 0) AS answers,
  COUNT(DISTINCT m.session_id) AS active_sessions,
  COUNT(DISTINCT NULLIF(s.user_id, '')) AS active_users
FROM messages m
LEFT JOIN sessions s ON s.id = m.session_id
WHERE m.deleted_at IS NULL AND m.created_at >= ?`, cutoff).Scan(&row).Error
	if err != nil {
		return fmt.Errorf("count chat activity: %w", err)
	}
	stats.Chat24h = types.ChatWindowStats{
		Hours:          hours,
		Questions:      row.Questions,
		Answers:        row.Answers,
		ActiveSessions: row.ActiveSessions,
		ActiveUsers:    row.ActiveUsers,
	}
	return nil
}

func (s *statsService) loadFeedbackStats(ctx context.Context, stats *types.SystemStats) error {
	var row struct {
		Likes       int64
		Dislikes    int64
		Likes24h    int64
		Dislikes24h int64
	}
	err := s.db.WithContext(ctx).Raw(`
SELECT
  COALESCE(SUM(CASE WHEN feedback = @up THEN 1 ELSE 0 END), 0) AS likes,
  COALESCE(SUM(CASE WHEN feedback = @down THEN 1 ELSE 0 END), 0) AS dislikes,
  COALESCE(SUM(CASE WHEN feedback = @up AND feedback_at >= @cutoff THEN 1 ELSE 0 END), 0) AS likes24h,
  COALESCE(SUM(CASE WHEN feedback = @down AND feedback_at >= @cutoff THEN 1 ELSE 0 END), 0) AS dislikes24h
FROM messages
WHERE deleted_at IS NULL AND feedback <> 0`,
		sql.Named("up", types.MessageFeedbackUp),
		sql.Named("down", types.MessageFeedbackDown),
		sql.Named("cutoff", time.Now().Add(-24*time.Hour)),
	).Scan(&row).Error
	if err != nil {
		return fmt.Errorf("count feedback: %w", err)
	}
	stats.Feedback = types.FeedbackStats{
		Likes:       row.Likes,
		Dislikes:    row.Dislikes,
		Likes24h:    row.Likes24h,
		Dislikes24h: row.Dislikes24h,
	}
	return nil
}

// loadPublicKBStats breaks documents down per platform-public knowledge
// base (owner 0, visibility public) — the KBs every user searches.
func (s *statsService) loadPublicKBStats(ctx context.Context, stats *types.SystemStats) error {
	rows := make([]types.PublicKBDocumentCount, 0)
	err := s.db.WithContext(ctx).Raw(`
SELECT
  kb.id, kb.name, kb.type,
  COUNT(k.id) AS total,
  COALESCE(SUM(CASE WHEN k.parse_status = @completed THEN 1 ELSE 0 END), 0) AS completed,
  COALESCE(SUM(CASE WHEN k.parse_status IN @active THEN 1 ELSE 0 END), 0) AS processing,
  COALESCE(SUM(CASE WHEN k.parse_status = @failed THEN 1 ELSE 0 END), 0) AS failed
FROM knowledge_bases kb
LEFT JOIN knowledges k ON k.knowledge_base_id = kb.id AND k.deleted_at IS NULL
WHERE kb.deleted_at IS NULL AND kb.owner_tenant_id = 0 AND kb.visibility = @public
GROUP BY kb.id, kb.name, kb.type
ORDER BY total DESC, kb.name`,
		sql.Named("completed", types.ParseStatusCompleted),
		sql.Named("active", activeParseStatuses),
		sql.Named("failed", types.ParseStatusFailed),
		sql.Named("public", string(types.KBVisibilityPublic)),
	).Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("count public kb documents: %w", err)
	}
	stats.PublicKBs = rows
	return nil
}

// activeParseStatuses are the parse states of a document still in the
// ingest pipeline.
var activeParseStatuses = []string{
	types.ParseStatusPending,
	types.ParseStatusProcessing,
	types.ParseStatusFinalizing,
}

func (s *statsService) ListProcessingDocuments(ctx context.Context, limit int) (*types.ProcessingDocuments, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	db := s.db.WithContext(ctx)
	out := &types.ProcessingDocuments{Items: make([]types.ProcessingDocument, 0)}
	if err := db.Model(&types.Knowledge{}).
		Where("parse_status IN ?", activeParseStatuses).
		Count(&out.Total).Error; err != nil {
		return nil, fmt.Errorf("count processing documents: %w", err)
	}
	if err := db.Model(&types.Knowledge{}).
		Where("parse_status = ? AND updated_at >= ?", types.ParseStatusFailed, time.Now().Add(-24*time.Hour)).
		Count(&out.FailedLast24h).Error; err != nil {
		return nil, fmt.Errorf("count failed documents: %w", err)
	}
	if out.Total == 0 {
		return out, nil
	}
	err := db.Raw(`
SELECT
  k.id, k.title, k.file_name, k.file_type, k.parse_status, k.pending_subtasks_count,
  k.knowledge_base_id, COALESCE(kb.name, '') AS knowledge_base_name,
  k.tenant_id, COALESCE(t.name, '') AS tenant_name,
  k.created_at, k.updated_at
FROM knowledges k
LEFT JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id
LEFT JOIN tenants t ON t.id = k.tenant_id
WHERE k.deleted_at IS NULL AND k.parse_status IN ?
ORDER BY k.created_at
LIMIT ?`, activeParseStatuses, limit).Scan(&out.Items).Error
	if err != nil {
		return nil, fmt.Errorf("list processing documents: %w", err)
	}
	return out, nil
}
