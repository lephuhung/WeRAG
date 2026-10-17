package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// TenantTemplateStyleRepository stores each workspace's template font sizes.
type TenantTemplateStyleRepository interface {
	// Get returns the workspace's override, nil when none was saved.
	Get(ctx context.Context, tenantID uint64) (*types.TenantTemplateStyle, error)
	// Save writes the workspace's override (the last save wins).
	Save(ctx context.Context, s *types.TenantTemplateStyle) error
}
