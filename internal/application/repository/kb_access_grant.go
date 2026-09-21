package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// ErrKBAccessGrantExists is returned by Create when a pending or approved
// grant already exists for (kb_id, grantee_tenant_id). The service layer
// maps it to a conflict response.
var ErrKBAccessGrantExists = errors.New("repository: kb access grant already exists")

// kbAccessGrantRepository implements interfaces.KBAccessGrantRepository.
type kbAccessGrantRepository struct {
	db *gorm.DB
}

// NewKBAccessGrantRepository constructs the repo against db. Wired up via
// the dig container alongside other repositories.
func NewKBAccessGrantRepository(db *gorm.DB) interfaces.KBAccessGrantRepository {
	return &kbAccessGrantRepository{db: db}
}

// Create inserts a grant row after a best-effort duplicate pre-check inside
// a transaction. The partial unique index uniq_kb_grant_pending is the
// authoritative guard; the pre-check only converts the typical
// "click twice" case into a clean sentinel.
func (r *kbAccessGrantRepository) Create(ctx context.Context, grant *types.KBAccessGrant) error {
	if grant.Status == "" {
		grant.Status = types.GrantStatusPending
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var probe types.KBAccessGrant
		err := tx.
			Where("kb_id = ? AND grantee_tenant_id = ? AND status IN ?",
				grant.KBID, grant.GranteeTenantID,
				[]types.GrantStatus{types.GrantStatusPending, types.GrantStatusApproved}).
			First(&probe).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			// fallthrough — clean to insert.
		case err != nil:
			return err
		default:
			return ErrKBAccessGrantExists
		}
		return tx.Create(grant).Error
	})
}

// GetByID returns the grant row or (nil, nil) when missing.
func (r *kbAccessGrantRepository) GetByID(ctx context.Context, id string) (*types.KBAccessGrant, error) {
	var grant types.KBAccessGrant
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&grant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &grant, nil
}

// GetLiveByPair returns the pending or approved grant for the pair, or
// (nil, nil). Expired approved rows (expires_at in the past) are treated
// as dead so a fresh request can be filed.
func (r *kbAccessGrantRepository) GetLiveByPair(
	ctx context.Context, kbID string, granteeTenantID uint64,
) (*types.KBAccessGrant, error) {
	var grant types.KBAccessGrant
	err := r.db.WithContext(ctx).
		Where("kb_id = ? AND grantee_tenant_id = ?", kbID, granteeTenantID).
		Where("status = ? OR (status = ? AND (expires_at IS NULL OR expires_at > ?))",
			types.GrantStatusPending, types.GrantStatusApproved, time.Now()).
		First(&grant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &grant, nil
}

// ListByOwnerTenant lists grants on KBs owned by the tenant, newest first.
func (r *kbAccessGrantRepository) ListByOwnerTenant(
	ctx context.Context, tenantID uint64, statuses []types.GrantStatus,
) ([]*types.KBAccessGrant, error) {
	q := r.db.WithContext(ctx).
		Where("owner_tenant_id = ?", tenantID).
		Order("created_at DESC")
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var rows []*types.KBAccessGrant
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListByGranteeTenant lists grants requested by the tenant, newest first.
func (r *kbAccessGrantRepository) ListByGranteeTenant(
	ctx context.Context, tenantID uint64, statuses []types.GrantStatus,
) ([]*types.KBAccessGrant, error) {
	q := r.db.WithContext(ctx).
		Where("grantee_tenant_id = ?", tenantID).
		Order("created_at DESC")
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var rows []*types.KBAccessGrant
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListApprovedKBIDs returns the distinct KB IDs with a live approved grant
// to tenantID.
func (r *kbAccessGrantRepository) ListApprovedKBIDs(ctx context.Context, tenantID uint64) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).
		Model(&types.KBAccessGrant{}).
		Where("grantee_tenant_id = ? AND status = ?", tenantID, types.GrantStatusApproved).
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Distinct().
		Pluck("kb_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// CountByKBIDs returns the number of live (pending or approved) grants per
// KB id — used to render the "shared" badge on KB list rows.
func (r *kbAccessGrantRepository) CountByKBIDs(ctx context.Context, kbIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(kbIDs))
	if len(kbIDs) == 0 {
		return counts, nil
	}
	type row struct {
		KBID  string
		Count int
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Model(&types.KBAccessGrant{}).
		Select("kb_id, COUNT(*) AS count").
		Where("kb_id IN ? AND status IN ?", kbIDs,
			[]types.GrantStatus{types.GrantStatusPending, types.GrantStatusApproved}).
		Group("kb_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.KBID] = r.Count
	}
	return counts, nil
}

// UpdateStatus persists the full row (status + reviewer fields).
func (r *kbAccessGrantRepository) UpdateStatus(ctx context.Context, grant *types.KBAccessGrant) error {
	return r.db.WithContext(ctx).Save(grant).Error
}

// DeleteByKnowledgeBaseID soft-deletes every grant row for a KB — used when
// the KB itself is deleted so grantee tenants lose access immediately.
func (r *kbAccessGrantRepository) DeleteByKnowledgeBaseID(ctx context.Context, kbID string) error {
	return r.db.WithContext(ctx).
		Where("kb_id = ?", kbID).
		Delete(&types.KBAccessGrant{}).Error
}

// MarkStatusIfPending atomically transitions a pending row to a terminal
// status. The WHERE guard makes concurrent approve/reject/revoke safe —
// only the first writer wins.
func (r *kbAccessGrantRepository) MarkStatusIfPending(
	ctx context.Context, id string, status types.GrantStatus, approvedBy string, message string,
) error {
	now := time.Now()
	res := r.db.WithContext(ctx).
		Model(&types.KBAccessGrant{}).
		Where("id = ? AND status = ?", id, types.GrantStatusPending).
		Updates(map[string]any{
			"status":       status,
			"approved_by":  approvedBy,
			"message":      message,
			"responded_at": now,
			"updated_at":   now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
