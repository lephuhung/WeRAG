package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// StatsService aggregates platform-wide statistics for the SystemAdmin
// dashboard (account counters, documents by parse status, message
// activity heatmap).
type StatsService interface {
	// GetSystemStats returns platform-wide counters. days bounds the
	// message-activity window (trailing N days).
	GetSystemStats(ctx context.Context, days int) (*types.SystemStats, error)
}
