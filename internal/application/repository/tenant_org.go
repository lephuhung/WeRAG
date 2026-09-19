package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var (
	// ErrTenantOrgNotFound is returned when the org row does not exist.
	ErrTenantOrgNotFound = errors.New("tenant org not found")
	// ErrTenantOrgMemberNotFound is returned for a missing membership row.
	ErrTenantOrgMemberNotFound = errors.New("tenant org member not found")
	// ErrTenantOrgMemberExists is returned on duplicate (org, user) inserts.
	ErrTenantOrgMemberExists = errors.New("user is already a member of this org")
)

// tenantOrgRepository implements TenantOrgRepository over tenant_orgs
// and tenant_org_members.
type tenantOrgRepository struct {
	db *gorm.DB
}

// NewTenantOrgRepository creates a new tenant org repository.
func NewTenantOrgRepository(db *gorm.DB) interfaces.TenantOrgRepository {
	return &tenantOrgRepository{db: db}
}

func (r *tenantOrgRepository) CreateOrg(ctx context.Context, org *types.TenantOrg) error {
	return r.db.WithContext(ctx).Create(org).Error
}

func (r *tenantOrgRepository) GetOrgByID(ctx context.Context, id uint64) (*types.TenantOrg, error) {
	var org types.TenantOrg
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&org).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantOrgNotFound
		}
		return nil, err
	}
	return &org, nil
}

func (r *tenantOrgRepository) ListOrgsByTenant(ctx context.Context, tenantID uint64) ([]*types.TenantOrg, error) {
	var orgs []*types.TenantOrg
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at ASC").Find(&orgs).Error; err != nil {
		return nil, err
	}
	return orgs, nil
}

func (r *tenantOrgRepository) UpdateOrg(ctx context.Context, org *types.TenantOrg) error {
	return r.db.WithContext(ctx).Model(&types.TenantOrg{}).
		Where("id = ?", org.ID).
		Updates(map[string]interface{}{
			"name":        org.Name,
			"description": org.Description,
		}).Error
}

func (r *tenantOrgRepository) DeleteOrg(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ?", id).Delete(&types.TenantOrgMember{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&types.TenantOrg{}).Error
	})
}

func (r *tenantOrgRepository) AddMember(ctx context.Context, member *types.TenantOrgMember) error {
	var existing types.TenantOrgMember
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND user_id = ?", member.OrgID, member.UserID).
		First(&existing).Error
	if err == nil {
		return ErrTenantOrgMemberExists
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.db.WithContext(ctx).Create(member).Error
}

func (r *tenantOrgRepository) RemoveMember(ctx context.Context, orgID uint64, userID string) error {
	res := r.db.WithContext(ctx).
		Where("org_id = ? AND user_id = ?", orgID, userID).
		Delete(&types.TenantOrgMember{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTenantOrgMemberNotFound
	}
	return nil
}

func (r *tenantOrgRepository) UpdateMemberRole(ctx context.Context, orgID uint64, userID string, role types.TenantOrgRole) error {
	res := r.db.WithContext(ctx).Model(&types.TenantOrgMember{}).
		Where("org_id = ? AND user_id = ?", orgID, userID).
		Update("role", role)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTenantOrgMemberNotFound
	}
	return nil
}

func (r *tenantOrgRepository) GetMember(ctx context.Context, orgID uint64, userID string) (*types.TenantOrgMember, error) {
	var m types.TenantOrgMember
	if err := r.db.WithContext(ctx).
		Where("org_id = ? AND user_id = ?", orgID, userID).
		First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *tenantOrgRepository) ListMembers(ctx context.Context, orgID uint64) ([]*types.TenantOrgMember, error) {
	var members []*types.TenantOrgMember
	if err := r.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("created_at ASC").Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

func (r *tenantOrgRepository) ListOrgIDsForUser(ctx context.Context, tenantID uint64, userID string) ([]uint64, error) {
	var ids []uint64
	if userID == "" {
		return ids, nil
	}
	err := r.db.WithContext(ctx).Model(&types.TenantOrgMember{}).
		Joins("JOIN tenant_orgs ON tenant_orgs.id = tenant_org_members.org_id").
		Where("tenant_orgs.tenant_id = ? AND tenant_orgs.deleted_at IS NULL AND tenant_org_members.user_id = ?", tenantID, userID).
		Pluck("tenant_org_members.org_id", &ids).Error
	return ids, err
}

func (r *tenantOrgRepository) CountMembers(ctx context.Context, orgID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&types.TenantOrgMember{}).
		Where("org_id = ?", orgID).Count(&count).Error
	return count, err
}
