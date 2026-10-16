package interfaces

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

// ErrAgencyProfileVersion: the profile was saved by someone else since the
// version the caller edited.
var ErrAgencyProfileVersion = errors.New("agency profile changed since it was read")

// TenantAgencyProfileRepository stores each workspace's issuing agency.
type TenantAgencyProfileRepository interface {
	// Get returns the workspace's profile, nil when none was saved.
	Get(ctx context.Context, tenantID uint64) (*types.TenantAgencyProfile, error)
	// Save writes p when the stored version is still expectedVersion (0:
	// none stored yet) and returns it with its new version; otherwise
	// ErrAgencyProfileVersion.
	Save(ctx context.Context, p *types.TenantAgencyProfile, expectedVersion int) (*types.TenantAgencyProfile, error)
}
