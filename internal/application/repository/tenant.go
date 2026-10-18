package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTenantNotFound         = errors.New("tenant not found")
	ErrTenantHasKnowledgeBase = errors.New("organization has associated knowledge bases")
)

// TenantKBDependencyError rejects a tenant deletion while an active
// knowledge base still uses the tenant as its data scope. It wraps
// ErrTenantHasKnowledgeBase so errors.Is keeps matching the domain
// sentinel while errors.As exposes the blocking KB ID for the response.
type TenantKBDependencyError struct {
	TenantID        uint64
	KnowledgeBaseID string
	OwnerTenantID   uint64
	Visibility      string
}

func (e *TenantKBDependencyError) Error() string {
	return fmt.Sprintf("organization %d cannot be deleted: knowledge base %q still uses it as data scope",
		e.TenantID, e.KnowledgeBaseID)
}

func (e *TenantKBDependencyError) Unwrap() error { return ErrTenantHasKnowledgeBase }

// lockLiveTenantRow is the shared serialization point for the KB-create /
// tenant-delete protocol: both paths lock the same live tenant row inside
// their own transaction before touching knowledge_bases rows scoped to it.
// Whichever transaction acquires the row lock first wins — a deleter that
// locks first makes a racing create fail here on the soft-deleted row, and
// a creator that locks first makes a racing deleter observe its committed
// KB in the dependency probe. A missing or soft-deleted tenant reports
// ErrTenantNotFound (GORM's soft-delete scope hides deleted rows).
//
// Dialect note: clause.Locking emits SELECT ... FOR UPDATE on Postgres and
// is ignored by SQLite (verified: no locking clause generated), where
// writers serialize at the database level instead.
func lockLiveTenantRow(tx *gorm.DB, ctx context.Context, id uint64) error {
	var tenant types.Tenant
	err := tx.WithContext(ctx).Model(&types.Tenant{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("id = ?", id).
		Take(&tenant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrTenantNotFound
	}
	return err
}

// tenantRepository implements tenant repository interface
type tenantRepository struct {
	db *gorm.DB
}

// NewTenantRepository creates a new tenant repository
func NewTenantRepository(db *gorm.DB) interfaces.TenantRepository {
	return &tenantRepository{db: db}
}

// CreateTenant creates tenant
func (r *tenantRepository) CreateTenant(ctx context.Context, tenant *types.Tenant) error {
	return r.db.WithContext(ctx).Create(tenant).Error
}

// GetTenantByID gets tenant by ID
func (r *tenantRepository) GetTenantByID(ctx context.Context, id uint64) (*types.Tenant, error) {
	var tenant types.Tenant
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantNotFound
		}
		return nil, err
	}
	return &tenant, nil
}

// GetTenantsByIDs batches GetTenantByID with a single IN-list query.
// Returns a map keyed by tenant ID; missing rows are simply absent from
// the map (no error). An empty input slice short-circuits to an empty map
// without hitting the database.
func (r *tenantRepository) GetTenantsByIDs(ctx context.Context, ids []uint64) (map[uint64]*types.Tenant, error) {
	if len(ids) == 0 {
		return map[uint64]*types.Tenant{}, nil
	}
	var tenants []*types.Tenant
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&tenants).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]*types.Tenant, len(tenants))
	for _, t := range tenants {
		if t != nil {
			out[t.ID] = t
		}
	}
	return out, nil
}

// ListTenants lists all workspaces. Configuration profiles are hidden.
func (r *tenantRepository) ListTenants(ctx context.Context) ([]*types.Tenant, error) {
	var tenants []*types.Tenant
	if err := r.db.WithContext(ctx).Where("kind = ?", types.TenantKindWorkspace).
		Order("created_at DESC").Find(&tenants).Error; err != nil {
		return nil, err
	}
	return tenants, nil
}

// profileInheritedColumns are the tenant-row settings a workspace takes from
// its configuration profile. Identity, quota, storage bindings, chat history
// (which points at a per-workspace knowledge base) and API principals stay
// per workspace.
var profileInheritedColumns = []string{
	"retriever_engines",
	"web_search_config",
	"parser_engine_config",
	"retrieval_config",
	"credentials",
}

// SyncProfileSettings copies the profile's inherited columns onto the
// workspaces on it in one statement, so they never disagree half-way.
// Workspaces keep reading their own row; this is what makes a SuperAdmin's
// save reach them, including background jobs that reload the tenant.
func (r *tenantRepository) SyncProfileSettings(ctx context.Context, profileID, workspaceID uint64) (int64, error) {
	sets := make([]string, 0, len(profileInheritedColumns))
	args := make([]any, 0, len(profileInheritedColumns)+2)
	for _, col := range profileInheritedColumns {
		sets = append(sets, col+" = (SELECT p."+col+" FROM tenants p WHERE p.id = ?)")
		args = append(args, profileID)
	}
	sql := "UPDATE tenants SET " + strings.Join(sets, ", ") +
		" WHERE config_profile_id = ? AND kind = ? AND deleted_at IS NULL"
	args = append(args, profileID, types.TenantKindWorkspace)
	if workspaceID != 0 {
		sql += " AND id = ?"
		args = append(args, workspaceID)
	}
	res := r.db.WithContext(ctx).Exec(sql, args...)
	return res.RowsAffected, res.Error
}

// GetDefaultConfigProfile returns the oldest live configuration profile.
func (r *tenantRepository) GetDefaultConfigProfile(ctx context.Context) (*types.Tenant, error) {
	var profile types.Tenant
	err := r.db.WithContext(ctx).Where("kind = ?", types.TenantKindConfigProfile).
		Order("id ASC").First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// SearchTenants searches tenants with pagination and filters
func (r *tenantRepository) SearchTenants(ctx context.Context, keyword string, tenantID uint64, page, pageSize int) ([]*types.Tenant, int64, error) {
	var tenants []*types.Tenant
	var total int64

	// Configuration profiles are not units: never searchable.
	query := r.db.WithContext(ctx).Model(&types.Tenant{}).Where("kind = ?", types.TenantKindWorkspace)

	// Build search conditions
	if tenantID > 0 && keyword != "" {
		escaped := escapeLikeKeyword(keyword)
		query = query.Where("id = ? OR name LIKE ? OR description LIKE ?", tenantID, "%"+escaped+"%", "%"+escaped+"%")
	} else if tenantID > 0 {
		query = query.Where("id = ?", tenantID)
	} else if keyword != "" {
		escaped := escapeLikeKeyword(keyword)
		query = query.Where("name LIKE ? OR description LIKE ?", "%"+escaped+"%", "%"+escaped+"%")
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply pagination
	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		query = query.Offset(offset).Limit(pageSize)
	}

	// Order by created_at DESC
	query = query.Order("created_at DESC")

	// Execute query
	if err := query.Find(&tenants).Error; err != nil {
		return nil, 0, err
	}

	return tenants, total, nil
}

// UpdateTenant updates tenant.
func (r *tenantRepository) UpdateTenant(ctx context.Context, tenant *types.Tenant) error {
	return r.db.WithContext(ctx).Model(&types.Tenant{}).Where("id = ?", tenant.ID).Updates(tenant).Error
}

// DeleteTenant soft-deletes the tenant and every active membership row
// for that tenant in one transaction. Without the membership purge,
// /auth/me still lists the defunct tenant (name lookup fails → UI shows
// "#<id>").
//
// The same transaction first rejects the delete while any active knowledge
// base still uses the tenant as its data scope (knowledge_bases.tenant_id),
// including platform-owned public KBs that retained this tenant's storage
// scope after a scope transition. The rejection carries the blocking KB ID
// via *TenantKBDependencyError; KB rows of other tenants never block.
func (r *tenantRepository) DeleteTenant(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the live tenant row first: this serializes against
		// concurrent KB creates, which lock the same row before
		// inserting. A missing/already-deleted tenant fails here.
		if err := lockLiveTenantRow(tx, ctx, id); err != nil {
			return err
		}
		var blocker struct {
			ID            string `gorm:"column:id"`
			OwnerTenantID uint64 `gorm:"column:owner_tenant_id"`
			Visibility    string `gorm:"column:visibility"`
		}
		// GORM's soft-delete scope excludes already-deleted KB rows, so
		// only active data-scope dependents block. The filter is on the
		// data-scope tenant_id alone — never owner_tenant_id — so a
		// platform-owned KB converted from this tenant still blocks.
		err := tx.Model(&types.KnowledgeBase{}).
			Select("id", "owner_tenant_id", "visibility").
			Where("tenant_id = ?", id).
			Order("id ASC").
			Take(&blocker).Error
		switch {
		case err == nil:
			return &TenantKBDependencyError{
				TenantID: id, KnowledgeBaseID: blocker.ID,
				OwnerTenantID: blocker.OwnerTenantID, Visibility: blocker.Visibility,
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			// No dependent KB: proceed with the delete below.
		default:
			return err
		}
		if err := tx.Where("tenant_id = ?", id).Delete(&types.TenantMember{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&types.Tenant{}).Error
	})
}

func (r *tenantRepository) AdjustStorageUsed(ctx context.Context, tenantID uint64, delta int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tenant types.Tenant
		// Dùng khóa bi quan để an toàn khi đồng thời
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&tenant, tenantID).Error; err != nil {
			return err
		}

		tenant.StorageUsed += delta
		// Lưu cập nhật và kiểm tra quy tắc nghiệp vụ
		if tenant.StorageUsed < 0 {
			logger.Errorf(ctx, "tenant storage used is negative %d: %d", tenant.ID, tenant.StorageUsed)
			tenant.StorageUsed = 0
		}

		return tx.Save(&tenant).Error
	})
}

// BulkSetStorageQuota writes quotaBytes to storage_quota for every
// tenant in one statement. We don't WHERE-filter (the action is
// "apply globally"), so the affected count equals the row count of
// the tenants table.
//
// No transaction here: the operation is a single statement and we
// don't want to hold a long lock just to update a single column. If
// a concurrent CreateTenant lands in the middle, the new row gets
// the new default via the system-setting resolver in the handler —
// no risk of the new tenant being skipped.
func (r *tenantRepository) BulkSetStorageQuota(ctx context.Context, quotaBytes int64) (int64, error) {
	res := r.db.WithContext(ctx).
		Model(&types.Tenant{}).
		Where("1 = 1"). // GORM refuses unconditional UPDATEs without an explicit WHERE
		Update("storage_quota", quotaBytes)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
