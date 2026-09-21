package service

import (
	"context"
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
	db *gorm.DB
}

// NewStatsService creates the stats service.
func NewStatsService(db *gorm.DB) interfaces.StatsService {
	return &statsService{db: db}
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
	if stats.Accounts.TotalTenants, err = count(db.Model(&types.Tenant{})); err != nil {
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
	dayExpr := "date(created_at)"
	switch db.Dialector.Name() {
	case "postgres":
		dayExpr = "to_char(created_at, 'YYYY-MM-DD')"
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
