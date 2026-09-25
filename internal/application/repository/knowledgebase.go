package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var ErrKnowledgeBaseNotFound = errors.New("knowledge base not found")

// knowledgeBaseRepository implements the KnowledgeBaseRepository interface
type knowledgeBaseRepository struct {
	db *gorm.DB
}

// NewKnowledgeBaseRepository creates a new knowledge base repository
func NewKnowledgeBaseRepository(db *gorm.DB) interfaces.KnowledgeBaseRepository {
	return &knowledgeBaseRepository{db: db}
}

// CreateKnowledgeBase creates a new knowledge base.
//
// Tenant-scoped rows (tenant_id != 0) serialize against tenant deletion on
// the live tenant row: the insert runs inside a transaction that locks and
// verifies the tenant first, so a KB can never be orphaned under a tenant
// that a concurrent delete just removed (creation fails with
// ErrTenantNotFound instead). Platform-owned rows (tenant_id == 0) have no
// tenant owner and stay independent of any tenant row.
func (r *knowledgeBaseRepository) CreateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil {
		return errors.New("knowledge base cannot be empty")
	}
	if kb.TenantID == 0 {
		return r.db.WithContext(ctx).Create(kb).Error
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockLiveTenantRow(tx, ctx, kb.TenantID); err != nil {
			return err
		}
		return tx.Create(kb).Error
	})
}

// GetKnowledgeBaseByID gets a knowledge base by id (no tenant scope; caller must enforce isolation where needed)
func (r *knowledgeBaseRepository) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKnowledgeBaseNotFound
		}
		return nil, err
	}
	return &kb, nil
}

// GetKnowledgeBaseByIDAndTenant gets a knowledge base by id only if it belongs to the given tenant (enforces tenant isolation)
func (r *knowledgeBaseRepository) GetKnowledgeBaseByIDAndTenant(ctx context.Context, id string, tenantID uint64) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKnowledgeBaseNotFound
		}
		return nil, err
	}
	return &kb, nil
}

// GetKnowledgeBaseByIDs gets knowledge bases by multiple ids
func (r *knowledgeBaseRepository) GetKnowledgeBaseByIDs(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	if len(ids) == 0 {
		return []*types.KnowledgeBase{}, nil
	}
	var kbs []*types.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&kbs).Error; err != nil {
		return nil, err
	}
	return kbs, nil
}

// ListKnowledgeBases lists all knowledge bases
func (r *knowledgeBaseRepository) ListKnowledgeBases(ctx context.Context) ([]*types.KnowledgeBase, error) {
	var kbs []*types.KnowledgeBase
	if err := r.db.WithContext(ctx).Find(&kbs).Error; err != nil {
		return nil, err
	}
	return kbs, nil
}

// ListKnowledgeBasesByTenantID lists all knowledge bases by tenant id.
//
// Ordering used to also include `is_pinned DESC, pinned_at DESC` so the
// repository would return tenant-wide pinned rows first. That column is
// no longer the source of truth (see migration 000050) — pin state is
// now per (user, kb) and applied by the service layer after enrichment.
// We keep `created_at DESC` here so callers that don't enrich (chat
// pipeline, agent editor, IM commands) still get a stable ordering.
func (r *knowledgeBaseRepository) ListKnowledgeBasesByTenantID(
	ctx context.Context, tenantID uint64,
) ([]*types.KnowledgeBase, error) {
	var kbs []*types.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND is_temporary = ?", tenantID, false).
		Order("created_at DESC").Find(&kbs).Error; err != nil {
		return nil, err
	}
	return kbs, nil
}

// GetKBScopeByID returns the access-scope projection of one KB without
// loading the full row: the data-scope tenant, the authorization owner
// (0 = platform-owned), and the visibility.
func (r *knowledgeBaseRepository) GetKBScopeByID(ctx context.Context, id string) (*types.KBScope, error) {
	var scope types.KBScope
	err := r.db.WithContext(ctx).Model(&types.KnowledgeBase{}).
		Select("tenant_id", "owner_tenant_id", "visibility").
		Where("id = ?", id).
		Take(&scope).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if !scope.Visibility.IsValid() {
		scope.Visibility = types.KBVisibilityTenant
	}
	return &scope, nil
}

// ListVisibleKnowledgeBases returns the non-temporary KBs of tenantID:
// tenant- and public-scoped rows. Cross-tenant visibility comes from
// kb_access_grants, resolved by the service layer.
func (r *knowledgeBaseRepository) ListVisibleKnowledgeBases(
	ctx context.Context, tenantID uint64,
) ([]*types.KnowledgeBase, error) {
	var kbs []*types.KnowledgeBase
	q := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_temporary = ?", tenantID, false).
		Where("visibility IN ?", []string{
			string(types.KBVisibilityTenant), string(types.KBVisibilityPublic)})
	if err := q.Order("created_at DESC").Find(&kbs).Error; err != nil {
		return nil, err
	}
	return kbs, nil
}

// ListPublicKnowledgeBasesExcept lists public KBs owned by other
// tenants — the cross-tenant readable catalog.
func (r *knowledgeBaseRepository) ListPublicKnowledgeBasesExcept(
	ctx context.Context, tenantID uint64,
) ([]*types.KnowledgeBase, error) {
	var kbs []*types.KnowledgeBase
	if err := r.db.WithContext(ctx).
		Where("tenant_id <> ? AND is_temporary = ? AND visibility = ?",
			tenantID, false, string(types.KBVisibilityPublic)).
		Order("created_at DESC").Find(&kbs).Error; err != nil {
		return nil, err
	}
	return kbs, nil
}

// ListForeignKnowledgeBasesByTenantID lists tenantID's KBs that callers
// outside the tenant may see: tenant- and public-scoped rows.
func (r *knowledgeBaseRepository) ListForeignKnowledgeBasesByTenantID(
	ctx context.Context, tenantID uint64,
) ([]*types.KnowledgeBase, error) {
	var kbs []*types.KnowledgeBase
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND is_temporary = ?", tenantID, false).
		Order("created_at DESC").Find(&kbs).Error; err != nil {
		return nil, err
	}
	return kbs, nil
}

// catalogOrder keeps every catalog query on one stable ordering so paged
// reads never skip or duplicate rows: newest first, id ASC as tiebreak
// (created_at has second precision on SQLite and ties are common).
func catalogOrder(q *gorm.DB) *gorm.DB {
	return q.Order("created_at DESC").Order("id ASC")
}

// ListOwnedKnowledgeBases lists the non-temporary KBs authorized under
// ownerTenantID: owner-owned rows plus legacy pre-backfill rows (owner 0
// with tenant visibility inside the same data scope). Platform-owned
// public rows never match; foreign tenant-owned rows never match.
func (r *knowledgeBaseRepository) ListOwnedKnowledgeBases(
	ctx context.Context, ownerTenantID uint64,
) ([]*types.KnowledgeBase, error) {
	var kbs []*types.KnowledgeBase
	q := r.db.WithContext(ctx).
		Where("is_temporary = ?", false).
		Where(
			"owner_tenant_id = ? OR (owner_tenant_id = 0 AND visibility = ? AND tenant_id = ?)",
			ownerTenantID, string(types.KBVisibilityTenant), ownerTenantID,
		)
	if err := catalogOrder(q).Find(&kbs).Error; err != nil {
		return nil, err
	}
	return kbs, nil
}

// ListPlatformPublicCatalog lists one page of the platform-owned public
// catalog: owner_tenant_id = 0 AND visibility = public. The data-scope
// tenant_id is never consulted, so converted rows that retained a tenant
// data scope are included. Temporary and soft-deleted rows (via GORM's
// auto-scope) are excluded. total counts the filtered catalog ignoring
// limit/offset.
func (r *knowledgeBaseRepository) ListPlatformPublicCatalog(
	ctx context.Context, keyword string, limit, offset int,
) ([]*types.KnowledgeBase, int64, error) {
	if limit <= 0 {
		limit = types.PublicCatalogDefaultPageSize
	}
	if offset < 0 {
		offset = 0
	}
	base := r.db.WithContext(ctx).Model(&types.KnowledgeBase{}).
		Where("is_temporary = ?", false).
		Where("owner_tenant_id = 0 AND visibility = ?", string(types.KBVisibilityPublic))
	if q := strings.TrimSpace(keyword); q != "" {
		// Same LIKE convention as SearchTenants: backslash-escaped
		// wildcards with the default LIKE escape.
		like := "%" + escapeLikeKeyword(q) + "%"
		base = base.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var kbs []*types.KnowledgeBase
	if err := catalogOrder(base).Limit(limit).Offset(offset).Find(&kbs).Error; err != nil {
		return nil, 0, err
	}
	return kbs, total, nil
}

// userKBPinRow mirrors the user_kb_pins table. Kept local to the
// repository because it never escapes the package; callers see the
// higher-level map[kb_id]pinned_at returned by ListUserKBPinIDs.
type userKBPinRow struct {
	TenantID uint64    `gorm:"column:tenant_id"`
	UserID   string    `gorm:"column:user_id"`
	KBID     string    `gorm:"column:kb_id"`
	PinnedAt time.Time `gorm:"column:pinned_at"`
}

func (userKBPinRow) TableName() string { return "user_kb_pins" }

// SetUserKBPin upserts (pinned=true) or deletes (pinned=false) the row
// for the given (tenant, user, kb) triple. The returned pinned_at is
// nil when pinned=false; otherwise it carries the timestamp written
// to the row (either the existing one if the row already existed, or
// the current time on insert) so the caller can stamp the response
// without a follow-up SELECT.
func (r *knowledgeBaseRepository) SetUserKBPin(
	ctx context.Context, tenantID uint64, userID string, kbID string, pinned bool,
) (*time.Time, error) {
	if userID == "" {
		return nil, errors.New("user_kb_pins: empty user_id")
	}
	if !pinned {
		err := r.db.WithContext(ctx).
			Where("tenant_id = ? AND user_id = ? AND kb_id = ?", tenantID, userID, kbID).
			Delete(&userKBPinRow{}).Error
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	// Upsert with idempotent INSERT … ON CONFLICT DO NOTHING. We then
	// SELECT to learn whether an existing row's pinned_at survived (so
	// repeated calls return a stable timestamp instead of bumping it).
	row := userKBPinRow{
		TenantID: tenantID,
		UserID:   userID,
		KBID:     kbID,
		PinnedAt: time.Now(),
	}
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND kb_id = ?", tenantID, userID, kbID).
		Attrs(userKBPinRow{PinnedAt: row.PinnedAt}).
		FirstOrCreate(&row).Error; err != nil {
		return nil, err
	}
	pa := row.PinnedAt
	return &pa, nil
}

// ListUserKBPinIDs returns every KB id this user has personally pinned
// in this tenant, mapped to its pinned_at. Returns an empty map (not
// nil) when there are no pins, so callers can do `len(m) == 0` checks
// without a nil guard.
func (r *knowledgeBaseRepository) ListUserKBPinIDs(
	ctx context.Context, tenantID uint64, userID string,
) (map[string]time.Time, error) {
	out := make(map[string]time.Time)
	if userID == "" {
		return out, nil
	}
	var rows []userKBPinRow
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.KBID] = row.PinnedAt
	}
	return out, nil
}

// UpdateKnowledgeBase updates a knowledge base
func (r *knowledgeBaseRepository) UpdateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error {
	return r.db.WithContext(ctx).Save(kb).Error
}

// UpdateKnowledgeBaseGeneratedProfile writes the generated_profile column
// only. The rest of the row is left untouched so a settings save that lands
// while a profile is being generated is not overwritten by the worker.
func (r *knowledgeBaseRepository) UpdateKnowledgeBaseGeneratedProfile(
	ctx context.Context, id string, profile *types.KnowledgeBaseProfile,
) error {
	if id == "" {
		return errors.New("knowledge base ID cannot be empty")
	}
	var value interface{}
	if profile != nil {
		value = *profile
	}
	return r.db.WithContext(ctx).Model(&types.KnowledgeBase{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"generated_profile": value,
			"updated_at":        time.Now(),
		}).Error
}

// DeleteKnowledgeBase deletes a knowledge base
func (r *knowledgeBaseRepository) DeleteKnowledgeBase(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.KnowledgeBase{}).Error
}

// CountByVectorStoreID counts active knowledge bases that are bound to the
// given vector store within a tenant scope.
//
// Soft-delete filter is applied automatically by GORM because KnowledgeBase
// has a gorm.DeletedAt column — we deliberately do not add an explicit
// `deleted_at IS NULL` predicate to keep the single source of truth on the
// auto-scope.
//
// Pass db == nil to use the repository's default db handle; pass a *gorm.DB
// bound to a transaction (e.g., from db.Transaction) to share the same
// write-lock context as the caller. Query column order matches the
// composite index idx_knowledge_bases_tenant_vector_store(tenant_id,
// vector_store_id).
func (r *knowledgeBaseRepository) CountByVectorStoreID(
	ctx context.Context, db *gorm.DB, tenantID uint64, storeID string,
) (int64, error) {
	if db == nil {
		db = r.db
	}
	var count int64
	err := db.WithContext(ctx).
		Model(&types.KnowledgeBase{}).
		Where("tenant_id = ? AND vector_store_id = ?", tenantID, storeID).
		Count(&count).Error
	return count, err
}

// CountByModelID counts active knowledge bases that reference modelID in any
// model-binding column (scalar fields or JSON config blobs).
func (r *knowledgeBaseRepository) CountByModelID(
	ctx context.Context, tenantID uint64, modelID string,
) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).
		Model(&types.KnowledgeBase{}).
		Where("tenant_id = ?", tenantID)
	query = scopeKnowledgeBasesByModelID(query, modelID)
	err := query.Count(&count).Error
	return count, err
}

// ListModelUsages returns active knowledge bases that reference modelID. It
// selects only the columns needed to identify the resource and its bindings;
// GORM's default scope excludes soft-deleted rows.
func (r *knowledgeBaseRepository) ListModelUsages(
	ctx context.Context, tenantID uint64, modelID string,
) ([]types.ModelUsageResource, error) {
	rows := make([]*types.KnowledgeBase, 0)
	query := r.db.WithContext(ctx).
		Model(&types.KnowledgeBase{}).
		Select(
			"id", "name", "embedding_model_id", "summary_model_id",
			"image_processing_config", "vlm_config", "asr_config", "wiki_config", "auto_tag_config",
		).
		Where("tenant_id = ?", tenantID)
	query = scopeKnowledgeBasesByModelID(query, modelID)
	if err := query.Order("name ASC, id ASC").Limit(types.ModelUsageListLimit).Find(&rows).Error; err != nil {
		return nil, err
	}

	usages := make([]types.ModelUsageResource, 0, len(rows))
	for _, row := range rows {
		bindings := knowledgeBaseModelUsageBindings(row, modelID)
		if len(bindings) == 0 {
			continue
		}
		usages = append(usages, types.ModelUsageResource{
			ID:       row.ID,
			Name:     row.Name,
			Bindings: bindings,
		})
	}
	return usages, nil
}
