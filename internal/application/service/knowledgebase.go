package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/datasource"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/storageallowlist"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// ErrInvalidTenantID represents an error for invalid tenant ID
var ErrInvalidTenantID = errors.New("invalid tenant ID")

const kbTaskCleanupTimeout = 5 * time.Second

// knowledgeBaseService implements the knowledge base service interface
type knowledgeBaseService struct {
	repo                 interfaces.KnowledgeBaseRepository
	kgRepo               interfaces.KnowledgeRepository
	chunkRepo            interfaces.ChunkRepository
	kbAccessGrantService interfaces.KBAccessGrantService
	kbInviteService      interfaces.KBInvitationService
	modelService         interfaces.ModelService
	retrieveEngine       interfaces.RetrieveEngineRegistry
	ownership            retriever.TenantStoreOwnership
	tenantRepo           interfaces.TenantRepository
	fileSvc              interfaces.FileService
	storageResolver      interfaces.StorageBackendResolver
	graphEngine          interfaces.RetrieveGraphRepository
	asynqClient          interfaces.TaskEnqueuer
	taskInspector        interfaces.TaskInspector
	taskPendingRepo      interfaces.TaskPendingOpsRepository
	dsRepo               interfaces.DataSourceRepository
	syncLogRepo          interfaces.SyncLogRepository
	dsScheduler          *datasource.Scheduler
	audit                interfaces.AuditLogService
	resourceCatalog      interfaces.ResourceCatalog
	wikiRepo             interfaces.WikiPageRepository
}

// NewKnowledgeBaseService creates a new knowledge base service
func NewKnowledgeBaseService(repo interfaces.KnowledgeBaseRepository,
	kgRepo interfaces.KnowledgeRepository,
	chunkRepo interfaces.ChunkRepository,
	kbAccessGrantService interfaces.KBAccessGrantService,
	kbInviteService interfaces.KBInvitationService,
	modelService interfaces.ModelService,
	retrieveEngine interfaces.RetrieveEngineRegistry,
	ownership retriever.TenantStoreOwnership,
	tenantRepo interfaces.TenantRepository,
	fileSvc interfaces.FileService,
	storageResolver interfaces.StorageBackendResolver,
	graphEngine interfaces.RetrieveGraphRepository,
	asynqClient interfaces.TaskEnqueuer,
	taskInspector interfaces.TaskInspector,
	taskPendingRepo interfaces.TaskPendingOpsRepository,
	dsRepo interfaces.DataSourceRepository,
	syncLogRepo interfaces.SyncLogRepository,
	dsScheduler *datasource.Scheduler,
	audit interfaces.AuditLogService,
	resourceCatalog interfaces.ResourceCatalog,
	wikiRepo interfaces.WikiPageRepository,
) interfaces.KnowledgeBaseService {
	return &knowledgeBaseService{
		repo:                 repo,
		kgRepo:               kgRepo,
		chunkRepo:            chunkRepo,
		kbAccessGrantService: kbAccessGrantService,
		kbInviteService:      kbInviteService,
		modelService:         modelService,
		retrieveEngine:       retrieveEngine,
		ownership:            ownership,
		tenantRepo:           tenantRepo,
		fileSvc:              fileSvc,
		storageResolver:      storageResolver,
		graphEngine:          graphEngine,
		asynqClient:          asynqClient,
		taskInspector:        taskInspector,
		taskPendingRepo:      taskPendingRepo,
		dsRepo:               dsRepo,
		syncLogRepo:          syncLogRepo,
		dsScheduler:          dsScheduler,
		audit:                audit,
		resourceCatalog:      resourceCatalog,
		wikiRepo:             wikiRepo,
	}
}

// GetRepository gets the knowledge base repository
// Parameters:
//   - ctx: Context with authentication and request information
//
// Returns:
//   - interfaces.KnowledgeBaseRepository: Knowledge base repository
func (s *knowledgeBaseService) GetRepository() interfaces.KnowledgeBaseRepository {
	return s.repo
}

// CreateKnowledgeBase creates a new knowledge base.
//
// When VectorStoreID is set, the binding is validated against the caller's
// tenant scope and the engine registry before persisting. A nil or
// empty-string VectorStoreID is normalized to nil ("use the tenant's
// effective engines") to match the retrieve-engine factory's pre-condition.
func (s *knowledgeBaseService) CreateKnowledgeBase(ctx context.Context,
	kb *types.KnowledgeBase,
) (*types.KnowledgeBase, error) {
	// Generate UUID and set creation timestamps
	if kb.ID == "" {
		kb.ID = uuid.New().String()
	}
	kb.CreatedAt = time.Now()
	kb.TenantID = types.MustTenantIDFromContext(ctx)
	// OwnerTenantID is the authorization owner: tenant-created KBs are
	// owned by the active tenant. TenantID above stays the immutable
	// data-scope/execution partition and is never rewritten by a later
	// public/tenant scope transition (Task 3).
	kb.OwnerTenantID = kb.TenantID
	kb.UpdatedAt = time.Now()
	// Record the creator so RBAC's RequireOwnershipOrRole can let
	// Contributors edit their own KBs without granting them tenant-wide
	// edit rights. The X-API-Key auth path attaches a synthetic
	// `system-<tenantID>` user; we deliberately skip those so the KB
	// stays tenant-owned (CreatorID == ""), which matches the original
	// API-key semantics (any human Admin can manage it) and prevents a
	// later "list KBs by creator" feature from surfacing rows nobody can
	// re-attribute.
	if uid, ok := types.UserIDFromContext(ctx); ok && !types.IsSyntheticUserID(uid) {
		kb.CreatorID = uid
	}
	kb.EnsureDefaults()
	if err := s.validateKBVisibility(ctx, kb); err != nil {
		return nil, err
	}
	// Validate owner/visibility before persistence so no write path can
	// mint a privileged-scope row (e.g. owner 0 + tenant visibility).
	if err := kb.ValidateOwnership(); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	applyTenantDefaultStorageProvider(ctx, kb)
	if err := s.applyAndValidateStorageBackend(ctx, kb); err != nil {
		return nil, err
	}

	// Fold empty-string vector_store_id into nil so this path and the
	// retrieve-engine factory's pre-condition share a single representation.
	wasEmpty := kb.VectorStoreID != nil && *kb.VectorStoreID == ""
	kb.Normalize()
	if wasEmpty {
		logger.Debugf(ctx,
			"[kb.create] empty vector_store_id normalized to nil for tenant=%d",
			kb.TenantID)
	}

	if kb.HasVectorStore() {
		if err := s.validateVectorStoreBinding(ctx, kb.TenantID, *kb.VectorStoreID); err != nil {
			return nil, err
		}
	}

	logger.Infof(ctx, "Creating knowledge base, ID: %s, tenant ID: %d, name: %s", kb.ID, kb.TenantID, kb.Name)

	if err := s.repo.CreateKnowledgeBase(ctx, kb); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": kb.ID,
			"tenant_id":         kb.TenantID,
		})
		// The repository serialization protocol rejects inserts under a
		// deleted/missing tenant instead of orphaning the row; surface
		// that as 404 so callers don't read it as an infra failure.
		if errors.Is(err, repository.ErrTenantNotFound) {
			return nil, apperrors.NewNotFoundError("workspace not found")
		}
		return nil, err
	}
	recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID, types.AuditActionKBCreated,
		"knowledge_base", kb.ID, types.AuditOutcomeSuccess, map[string]any{
			"name": kb.Name, "type": kb.Type,
		})

	logger.Infof(ctx, "Knowledge base created successfully, ID: %s, name: %s", kb.ID, kb.Name)
	return kb, nil
}

func (s *knowledgeBaseService) applyAndValidateStorageBackend(ctx context.Context, kb *types.KnowledgeBase) error {
	if s.storageResolver == nil || kb == nil {
		return nil
	}
	tenant, _ := types.TenantInfoFromContext(ctx)
	if tenant == nil {
		return apperrors.NewBadRequestError("workspace context missing")
	}
	id := ""
	if kb.StorageBackendID != nil {
		id = strings.TrimSpace(*kb.StorageBackendID)
	}
	provider := kb.GetStorageProvider()
	// A newly created KB without an explicit instance follows the concrete
	// tenant default. The legacy provider is only a fallback for workspaces
	// that have not been migrated yet.
	if id == "" && tenant.DefaultStorageBackendID != nil && strings.TrimSpace(*tenant.DefaultStorageBackendID) != "" {
		provider = ""
	}
	backend, err := s.storageResolver.ResolveBackend(ctx, tenant, id, provider)
	if err != nil {
		return apperrors.NewBadRequestError("storage backend is unavailable").WithDetails(err.Error())
	}
	if backend == nil {
		return nil
	}
	kb.StorageBackendID = &backend.ID
	kb.SetStorageProvider(backend.Provider)
	return nil
}

// applyTenantDefaultStorageProvider fills an empty KB storage provider from the
// tenant's global default (Settings → Storage engine). Frontend should send the
// same value; this keeps API clients and legacy UIs consistent.
func applyTenantDefaultStorageProvider(ctx context.Context, kb *types.KnowledgeBase) {
	if kb == nil || strings.TrimSpace(kb.GetStorageProvider()) != "" {
		return
	}
	tenant, _ := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
	provider := ""
	if tenant != nil && tenant.StorageEngineConfig != nil {
		provider = strings.ToLower(strings.TrimSpace(tenant.StorageEngineConfig.DefaultProvider))
	}
	if provider == "" || !storageallowlist.IsAllowed(provider) {
		provider = storageallowlist.FirstAllowed()
	}
	if provider == "" {
		return
	}
	kb.SetStorageProvider(provider)
}

// validateVectorStoreBinding routes through retriever.VerifyBinding so the
// ownership + registry sentinel hierarchy stays the single source of truth.
// The service layer's responsibility is to:
//
//  1. fast-reject malformed UUIDs (cheap pre-flight that also avoids a DB
//     round trip for type-confusion inputs like "' OR 1=1 --"),
//  2. translate retriever sentinels into user-facing AppErrors with
//     generic messages and the typed error codes.
//
// UUID parse failures map to the same "vector store not found" message as
// cross-tenant attempts to avoid an enumeration oracle that distinguishes
// "malformed input" from "non-existent UUID".
func (s *knowledgeBaseService) validateVectorStoreBinding(
	ctx context.Context, tenantID uint64, storeID string,
) error {
	sanitized := secutils.SanitizeForLog(storeID)

	if _, err := uuid.Parse(storeID); err != nil {
		logger.WarnWithFields(ctx, logger.Fields{
			"tenant_id": tenantID,
			"store_id":  sanitized,
			"reason":    "malformed vector_store_id",
		}, "[kb.create] vector store id is not a valid UUID")
		return apperrors.NewVectorStoreBindingInvalidError("vector store not found")
	}

	switch err := retriever.VerifyBinding(
		ctx, s.retrieveEngine, s.ownership, tenantID, storeID,
	); {
	case err == nil:
		return nil
	case errors.Is(err, retriever.ErrVectorStoreForbidden):
		logger.WarnWithFields(ctx, logger.Fields{
			"tenant_id": tenantID,
			"store_id":  sanitized,
			"reason":    "cross-tenant or unknown store",
		}, "[kb.create] vector store not owned by tenant")
		return apperrors.NewVectorStoreBindingInvalidError("vector store not found")
	case errors.Is(err, retriever.ErrVectorStoreNotFound),
		errors.Is(err, retriever.ErrVectorStoreUnavailable):
		logger.WarnWithFields(ctx, logger.Fields{
			"tenant_id": tenantID,
			"store_id":  sanitized,
			"reason":    "store recorded in DB but no engine could be resolved",
		}, "[kb.create] vector store currently unavailable")
		return apperrors.NewVectorStoreUnavailableError(
			"vector store is currently unavailable; check its connection configuration")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The caller went away or ran out of time while the binding was being
		// verified, which can now include rebuilding the store's engine. That
		// is not a server fault, so it must not be logged and answered as one.
		return err
	default:
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
			"store_id":  sanitized,
			"reason":    "binding verification failed",
		})
		return apperrors.NewInternalServerError("failed to verify vector store binding")
	}
}

// validateKBVisibility enforces the scope model on the tenant create
// path. Tenant Admins create tenant KBs only: a public visibility payload
// is rejected for every caller on this flow, including system admins —
// platform-owned public KBs are minted exclusively through
// CreatePublicKnowledgeBase. Tenant is the only accepted scope here.
func (s *knowledgeBaseService) validateKBVisibility(ctx context.Context, kb *types.KnowledgeBase) error {
	_ = ctx
	switch kb.Visibility {
	case types.KBVisibilityPublic:
		return apperrors.NewForbiddenError("public knowledge bases can only be created by a platform SuperAdmin through the public flow")
	case "", types.KBVisibilityTenant:
		kb.Visibility = types.KBVisibilityTenant
	default:
		kb.Visibility = types.KBVisibilityTenant
	}
	return nil
}

// CreatePublicKnowledgeBase creates a platform-owned public knowledge base.
// Only an explicit human SuperAdmin (is_system_admin plus a real
// non-synthetic user, never an API-key principal) may invoke it —
// CanAccessAllTenants alone never qualifies. The row is stamped with
// owner and data scope 0 and resolved from platform/global defaults: the
// selected tenant's storage/model configuration is never inherited and no
// tenant-scoped StorageBackendID binding is accepted.
func (s *knowledgeBaseService) CreatePublicKnowledgeBase(ctx context.Context,
	kb *types.KnowledgeBase,
) (*types.KnowledgeBase, error) {
	if kb == nil {
		return nil, apperrors.NewBadRequestError("knowledge base cannot be empty")
	}
	caller := types.CallerFromContext(ctx)
	if !access.IsExplicitHumanSuperAdmin(ctx, caller) {
		if caller.TenantID == 0 {
			return nil, apperrors.NewUnauthorizedError("Unauthorized")
		}
		return nil, apperrors.NewForbiddenError("only an explicit platform SuperAdmin may create public knowledge bases")
	}
	if kb.ID == "" {
		kb.ID = uuid.New().String()
	}
	now := time.Now()
	kb.CreatedAt = now
	kb.UpdatedAt = now
	// Platform scope: no owning tenant and the reserved platform data
	// scope. TenantID stays the immutable execution partition for the KB's
	// documents and indexes.
	kb.OwnerTenantID = 0
	kb.TenantID = 0
	kb.Visibility = types.KBVisibilityPublic
	if uid, ok := types.UserIDFromContext(ctx); ok && !types.IsSyntheticUserID(uid) {
		kb.CreatorID = uid
	}
	kb.EnsureDefaults()
	kb.Visibility = types.KBVisibilityPublic
	// Platform storage defaults: an explicit provider must clear the
	// global allowlist; otherwise the first globally-allowed provider is
	// used. Tenant-scoped backend bindings and the active tenant's
	// defaults are never consulted here.
	if kb.StorageBackendID != nil && strings.TrimSpace(*kb.StorageBackendID) != "" {
		return nil, apperrors.NewBadRequestError("public knowledge bases cannot bind a tenant storage backend")
	}
	kb.StorageBackendID = nil
	provider := strings.ToLower(strings.TrimSpace(kb.GetStorageProvider()))
	if provider == "" {
		provider = storageallowlist.FirstAllowed()
		if provider == "" {
			return nil, apperrors.NewInternalServerError("no supported storage provider is configured")
		}
		kb.SetStorageProvider(provider)
	} else if !storageallowlist.IsAllowed(provider) {
		return nil, apperrors.NewBadRequestError("Storage provider is not allowed by STORAGE_ALLOW_LIST")
	}
	kb.Normalize()
	if kb.HasVectorStore() {
		// Bound against the platform scope so only a platform-owned
		// store could satisfy it; tenant stores fail closed here.
		if err := s.validateVectorStoreBinding(ctx, 0, *kb.VectorStoreID); err != nil {
			return nil, err
		}
	}
	if err := kb.ValidateOwnership(); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	logger.Infof(ctx, "Creating public knowledge base, ID: %s, name: %s", kb.ID, kb.Name)
	if err := s.repo.CreateKnowledgeBase(ctx, kb); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": kb.ID,
		})
		s.recordKBScopeChange(ctx, kb.ID, oldKBScope{}, newKBScopeOf(kb), 0, types.AuditOutcomeFailed)
		return nil, err
	}
	s.recordKBScopeChange(ctx, kb.ID, oldKBScope{}, newKBScopeOf(kb), 0, types.AuditOutcomeSuccess)
	logger.Infof(ctx, "Public knowledge base created successfully, ID: %s, name: %s", kb.ID, kb.Name)
	return kb, nil
}

// oldKBScope snapshots the pre-transition owner/visibility of a KB for
// auditing. The zero value denotes "no previous scope" (creation).
type oldKBScope struct {
	OwnerTenantID uint64
	Visibility    types.KBVisibility
	Known         bool
}

// newKBScopeOf projects the post-transition owner/visibility of a KB.
func newKBScopeOf(kb *types.KnowledgeBase) oldKBScope {
	if kb == nil {
		return oldKBScope{}
	}
	return oldKBScope{OwnerTenantID: kb.OwnerTenantID, Visibility: kb.Visibility, Known: true}
}

// recordKBScopeChange writes one kb.scope_changed audit event carrying the
// actor, old/new owner+visibility, target tenant when present, KB ID, and
// outcome. Unlike recordKBActivity it permits the platform scope
// (tenant_id 0): public rows have no owning tenant to attribute to, and
// dropping their trail would leave privileged transitions unaudited.
// Best-effort like recordKBActivity: audit outages never roll back the
// business mutation.
func (s *knowledgeBaseService) recordKBScopeChange(
	ctx context.Context,
	kbID string,
	oldScope, newScope oldKBScope,
	targetTenantID uint64,
	outcome types.AuditOutcome,
) {
	if s.audit == nil || kbID == "" {
		return
	}
	if outcome == "" {
		outcome = types.AuditOutcomeSuccess
	}
	tenantID := newScope.OwnerTenantID
	if tenantID == 0 {
		tenantID = oldScope.OwnerTenantID
	}
	actor := types.CallerFromContext(ctx).UserID
	details := map[string]any{
		"kb_id":            kbID,
		"actor_user_id":    actor,
		"new_owner_tenant": newScope.OwnerTenantID,
		"new_visibility":   string(newScope.Visibility),
		"outcome":          string(outcome),
	}
	if oldScope.Known {
		details["old_owner_tenant"] = oldScope.OwnerTenantID
		details["old_visibility"] = string(oldScope.Visibility)
	}
	if targetTenantID != 0 {
		details["target_tenant_id"] = targetTenantID
	}
	var detailJSON types.JSON
	if b, merr := json.Marshal(details); merr == nil {
		detailJSON = types.JSON(b)
	}
	_ = s.audit.Log(ctx, &types.AuditLog{
		TenantID:    tenantID,
		ActorUserID: actor,
		Action:      types.AuditActionKBScopeChanged,
		ScopeType:   auditScopeKnowledgeBase,
		ScopeID:     kbID,
		TargetType:  "knowledge_base",
		TargetID:    kbID,
		Outcome:     outcome,
		Details:     detailJSON,
	})
}

// logKBAuditAllowZero writes a minimal KB audit event that survives the
// platform scope (tenant_id 0). Used for platform-owned lifecycle events
// that recordKBActivity would silently drop.
func (s *knowledgeBaseService) logKBAuditAllowZero(
	ctx context.Context, tenantID uint64, kbID string,
	action types.AuditAction, outcome types.AuditOutcome,
) {
	if s.audit == nil || kbID == "" {
		return
	}
	_ = s.audit.Log(ctx, &types.AuditLog{
		TenantID:    tenantID,
		ActorUserID: types.CallerFromContext(ctx).UserID,
		Action:      action,
		ScopeType:   auditScopeKnowledgeBase,
		ScopeID:     kbID,
		TargetType:  "knowledge_base",
		TargetID:    kbID,
		Outcome:     outcome,
	})
}

// requireKBLifecycleAccess enforces the owner-aware mutation policy for KB
// lifecycle operations (update/delete). Platform-owned public rows admit
// only an explicit human SuperAdmin. Tenant-owned rows require the caller
// to act inside the owning tenant under its existing role policy: platform
// SuperAdmin authority alone never overrides tenant ownership, so a
// SuperAdmin without (or outside) the owning tenant is denied. API-key
// principals never qualify on public rows and stay bounded by the
// allowlist checks elsewhere on tenant rows.
func requireKBLifecycleAccess(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil || kb.ID == "" {
		return apperrors.NewNotFoundError("knowledge base not found")
	}
	caller := types.CallerFromContext(ctx)
	if access.IsPlatformPublicKB(kb) {
		if access.IsExplicitHumanSuperAdmin(ctx, caller) {
			return nil
		}
		if caller.TenantID == 0 {
			return apperrors.NewUnauthorizedError("Unauthorized")
		}
		return apperrors.NewForbiddenError("only an explicit platform SuperAdmin may manage this public knowledge base")
	}
	owner := kb.OwnerTenantID
	if owner == 0 {
		// Legacy rows predating the owner backfill: the data scope is
		// the only ownership signal available.
		owner = kb.TenantID
	}
	if owner == 0 {
		// Platform data-scope row that is not public: fail closed to
		// explicit human SuperAdmins only.
		if access.IsExplicitHumanSuperAdmin(ctx, caller) {
			return nil
		}
		if caller.TenantID == 0 {
			return apperrors.NewUnauthorizedError("Unauthorized")
		}
		return apperrors.NewForbiddenError("only an explicit platform SuperAdmin may manage this knowledge base")
	}
	if caller.TenantID == 0 || caller.TenantID != owner {
		if caller.TenantID == 0 {
			return apperrors.NewUnauthorizedError("Unauthorized")
		}
		return apperrors.NewForbiddenError("không thể sửa knowledge base thuộc workspace khác")
	}
	return nil
}

// SetKnowledgeBaseVisibility changes a KB's owner/scope. Promotion
// (tenant→public) and scope transfer (public→tenant) alike require an
// explicit human SuperAdmin — tenant Admins, CanAccessAllTenants-only
// operators, and API-key principals are all denied. A public→tenant
// transition requires the nonzero destination tenant and verifies it
// exists before touching the row; rejected transitions leave the stored
// owner/visibility intact. Only owner/visibility metadata changes: the KB
// ID and data-scope tenant_id are preserved and no child rows, vector
// indexes, or background jobs are touched.
func (s *knowledgeBaseService) SetKnowledgeBaseVisibility(
	ctx context.Context, id string, visibility types.KBVisibility, targetTenantID uint64,
) (*types.KnowledgeBase, error) {
	if id == "" {
		return nil, apperrors.NewBadRequestError("knowledge base ID cannot be empty")
	}
	if !visibility.IsValid() {
		return nil, apperrors.NewBadRequestError("invalid visibility")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, apperrors.NewNotFoundError("knowledge base not found")
	}
	caller := types.CallerFromContext(ctx)
	if !access.IsExplicitHumanSuperAdmin(ctx, caller) {
		if caller.TenantID == 0 {
			return nil, apperrors.NewUnauthorizedError("Unauthorized")
		}
		return nil, apperrors.NewForbiddenError("only an explicit platform SuperAdmin may change knowledge base scope")
	}
	oldScope := oldKBScope{OwnerTenantID: kb.OwnerTenantID, Visibility: kb.Visibility, Known: true}
	switch {
	case kb.Visibility == types.KBVisibilityPublic && visibility == types.KBVisibilityPublic:
		if targetTenantID != 0 {
			return nil, apperrors.NewBadRequestError("target tenant must be omitted when keeping public scope")
		}
		return kb, nil
	case kb.Visibility == types.KBVisibilityTenant && visibility == types.KBVisibilityTenant:
		owner := kb.OwnerTenantID
		if owner == 0 {
			owner = kb.TenantID
		}
		if targetTenantID == 0 || targetTenantID == owner {
			return kb, nil
		}
		return nil, apperrors.NewBadRequestError("tenant-to-tenant scope transfer is not supported")
	case kb.Visibility == types.KBVisibilityTenant && visibility == types.KBVisibilityPublic:
		if targetTenantID != 0 {
			return nil, apperrors.NewBadRequestError("target tenant must be omitted when publishing to public scope")
		}
		kb.OwnerTenantID = 0
		kb.Visibility = types.KBVisibilityPublic
	case kb.Visibility == types.KBVisibilityPublic && visibility == types.KBVisibilityTenant:
		if targetTenantID == 0 {
			return nil, apperrors.NewBadRequestError("target tenant is required when moving a public knowledge base to tenant scope")
		}
		// The destination must exist before the row is touched so a
		// typo leaves the previous owner/visibility intact.
		if s.tenantRepo == nil {
			return nil, apperrors.NewInternalServerError("tenant service unavailable")
		}
		target, terr := s.tenantRepo.GetTenantByID(ctx, targetTenantID)
		if terr != nil {
			if errors.Is(terr, repository.ErrTenantNotFound) {
				return nil, apperrors.NewNotFoundError("target tenant not found")
			}
			return nil, terr
		}
		if target == nil {
			return nil, apperrors.NewNotFoundError("target tenant not found")
		}
		kb.OwnerTenantID = targetTenantID
		kb.Visibility = types.KBVisibilityTenant
	default:
		return nil, apperrors.NewBadRequestError("invalid visibility")
	}
	kb.EnsureDefaults()
	// Validate owner/visibility before persistence.
	if err := kb.ValidateOwnership(); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	kb.UpdatedAt = time.Now()
	if err := s.repo.UpdateKnowledgeBase(ctx, kb); err != nil {
		s.recordKBScopeChange(ctx, kb.ID, oldScope, newKBScopeOf(kb), targetTenantID, types.AuditOutcomeFailed)
		return nil, err
	}
	s.recordKBScopeChange(ctx, kb.ID, oldScope, newKBScopeOf(kb), targetTenantID, types.AuditOutcomeSuccess)
	return kb, nil
}

// GetKnowledgeBaseByID retrieves a knowledge base by its ID
func (s *knowledgeBaseService) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	kb.EnsureDefaults()
	return kb, nil
}

// GetKnowledgeBaseByIDOnly retrieves knowledge base by ID without tenant filter
// Used for cross-tenant shared KB access where permission is checked elsewhere
func (s *knowledgeBaseService) GetKnowledgeBaseByIDOnly(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	kb.EnsureDefaults()
	return kb, nil
}

// GetKnowledgeBasesByIDsOnly retrieves knowledge bases by IDs without tenant filter (batch).
func (s *knowledgeBaseService) GetKnowledgeBasesByIDsOnly(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	kbs, err := s.repo.GetKnowledgeBaseByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, kb := range kbs {
		if kb != nil {
			kb.EnsureDefaults()
		}
	}
	return kbs, nil
}

// ListKnowledgeBases returns the user-facing catalog for the caller's
// active tenant: owner-tenant KBs, a bounded first page of the platform
// public catalog (authenticated human callers only — never API-key or
// machine principals), and accepted recipient-bound invites. Rows are
// deduplicated by KB ID; temporary/deleted rows are excluded at the
// repository layer. Foreign tenant-owned rows never enter through public
// visibility or cross-tenant access.
//
// Count/status enrichment runs under each KB's stored data-scope
// tenant_id (never the requesting tenant), so converted and invited rows
// report their own partition's numbers.
func (s *knowledgeBaseService) ListKnowledgeBases(ctx context.Context) ([]*types.KnowledgeBase, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	caller := types.CallerFromContext(ctx)
	// Human-only public leg: the helper rejects every machine identity
	// (any API-key scope, API principal types), so public rows stay
	// human-only even for allowlisted platform keys.
	human := access.IsAuthenticatedHuman(ctx, caller)

	// Owner-tenant rows (plus legacy pre-backfill rows in the same data
	// scope). Data-scope/agent listings keep their own repository
	// methods and are untouched by this composition.
	kbs, err := s.repo.ListOwnedKnowledgeBases(ctx, tenantID)
	if err == nil && human {
		// Bounded public page for human discovery. A catalog failure
		// degrades to owned+invites (warned) rather than hiding the
		// caller's own KBs — same best-effort convention as invites
		// and processing counts below.
		if pub, _, perr := s.repo.ListPlatformPublicCatalog(
			ctx, "", types.PublicCatalogDefaultPageSize, 0,
		); perr == nil {
			seen := make(map[string]struct{}, len(kbs)+len(pub))
			for _, kb := range kbs {
				if kb != nil {
					seen[kb.ID] = struct{}{}
				}
			}
			for _, kb := range pub {
				if kb == nil || kb.ID == "" {
					continue
				}
				if _, dup := seen[kb.ID]; dup {
					continue
				}
				seen[kb.ID] = struct{}{}
				kbs = append(kbs, kb)
			}
		} else {
			logger.Warnf(ctx, "Failed to list public catalog for tenant=%d: %v", tenantID, perr)
		}
	}
	if err == nil {
		// Recipient-bound invites: KBs this user was individually
		// invited to read (accepted, unexpired). Lookup errors fail
		// closed (no invites); appendInvitedKBs dedupes against rows
		// already listed.
		if s.kbInviteService != nil {
			if userID, ok := types.UserIDFromContext(ctx); ok && userID != "" && !types.IsSyntheticUserID(userID) {
				if invited, ierr := s.kbInviteService.InvitedKBIDs(ctx, userID); ierr == nil && len(invited) > 0 {
					seen := make(map[string]struct{}, len(kbs))
					for _, kb := range kbs {
						if kb != nil {
							seen[kb.ID] = struct{}{}
						}
					}
					var fresh []string
					for _, id := range invited {
						if _, dup := seen[id]; !dup {
							fresh = append(fresh, id)
						}
					}
					if len(fresh) > 0 {
						if invitedKBs, kerr := s.repo.GetKnowledgeBaseByIDs(ctx, fresh); kerr == nil {
							kbs = appendInvitedKBs(kbs, invitedKBs)
						}
					}
				}
			}
		}
	}
	if err != nil {
		for _, kb := range kbs {
			kb.EnsureDefaults()
		}

		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		return nil, err
	}

	// Query knowledge count and chunk count for each knowledge base,
	// always under the KB's own stored data scope — never the requesting
	// tenant — so converted and invited rows report their own partition.
	for _, kb := range kbs {
		kb.EnsureDefaults()
		dataTenant := kb.TenantID

		// Get knowledge count
		switch kb.Type {
		case types.KnowledgeBaseTypeDocument:
			knowledgeCount, err := s.kgRepo.CountKnowledgeByKnowledgeBaseID(ctx, dataTenant, kb.ID)
			if err != nil {
				logger.Warnf(ctx, "Failed to get knowledge count for knowledge base %s: %v", kb.ID, err)
			} else {
				kb.KnowledgeCount = knowledgeCount
			}
		case types.KnowledgeBaseTypeFAQ:
			// Get chunk count
			chunkCount, err := s.chunkRepo.CountChunksByKnowledgeBaseID(ctx, dataTenant, kb.ID)
			if err != nil {
				logger.Warnf(ctx, "Failed to get chunk count for knowledge base %s: %v", kb.ID, err)
			} else {
				kb.ChunkCount = chunkCount
			}
		}

		// Check if there is a processing import task
		processingCount, err := s.kgRepo.CountKnowledgeByStatus(
			ctx,
			dataTenant,
			kb.ID,
			[]string{"pending", "processing"},
		)
		if err != nil {
			logger.Warnf(ctx, "Failed to check processing status for knowledge base %s: %v", kb.ID, err)
		} else {
			kb.IsProcessing = processingCount > 0
			kb.ProcessingCount = processingCount
		}
	}

	// Per-user pin stamping + ordering. The "main" list view is the
	// only path that needs to honour the caller's personal pin set;
	// agent/share/IM callers go through ListKnowledgeBasesByTenantID
	// which also enriches but keys off the user in their own context.
	if userID, ok := types.UserIDFromContext(ctx); ok && userID != "" {
		s.applyUserKBPins(ctx, tenantID, userID, kbs)
	}
	return kbs, nil
}

// ListPublicCatalog returns one bounded page of the platform-owned public
// catalog with the catalog total. Human callers only: anonymous contexts
// are unauthorized and API-key principals are forbidden, so public
// visibility never becomes an implicit key grant. No tenant context is
// required, so tenantless explicit human SuperAdmins can discover the
// catalog. Counts are enriched under each KB's own data scope.
func (s *knowledgeBaseService) ListPublicCatalog(
	ctx context.Context, page, pageSize int, keyword string,
) ([]*types.KnowledgeBase, int64, error) {
	caller := types.CallerFromContext(ctx)
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return nil, 0, apperrors.NewForbiddenError("API keys cannot access the public catalog")
	}
	if !access.IsAuthenticatedHuman(ctx, caller) {
		return nil, 0, apperrors.NewUnauthorizedError("Unauthorized")
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = types.PublicCatalogDefaultPageSize
	}
	if pageSize > types.PublicCatalogMaxPageSize {
		pageSize = types.PublicCatalogMaxPageSize
	}
	items, total, err := s.repo.ListPlatformPublicCatalog(ctx, keyword, pageSize, saturateCatalogOffset(page, pageSize))
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"page": page, "page_size": pageSize,
		})
		return nil, 0, err
	}
	for _, kb := range items {
		if kb == nil {
			continue
		}
		kb.EnsureDefaults()
		if cerr := s.FillKnowledgeBaseCounts(ctx, kb); cerr != nil {
			logger.Warnf(ctx, "Failed to fill KB counts for %s: %v", kb.ID, cerr)
		}
	}
	return items, total, nil
}

// saturateCatalogOffset computes (page-1)*pageSize without wrapping: on
// extreme pages the offset saturates at maxInt (a valid empty page) instead
// of overflowing into a negative offset that the repository would clamp
// back to the first page. Callers normalize page >= 1 and pageSize >= 1
// before reaching here.
func saturateCatalogOffset(page, pageSize int) int {
	maxOffset := int64(int(^uint(0) >> 1))
	if p := int64(page - 1); p > maxOffset/int64(pageSize) {
		return int(maxOffset)
	}
	return (page - 1) * pageSize
}

// appendInvitedKBs merges recipient-invited KB rows into a listing,
// skipping nil rows and IDs already present so an invite overlapping
// own-tenant access never duplicates a row. Temporary KBs are skipped:
// the owned and public legs exclude them at SQL, and an accepted invite
// to an ephemeral row must not resurface it in the catalog.
func appendInvitedKBs(existing, invited []*types.KnowledgeBase) []*types.KnowledgeBase {
	seen := make(map[string]struct{}, len(existing))
	for _, kb := range existing {
		if kb != nil {
			seen[kb.ID] = struct{}{}
		}
	}
	for _, kb := range invited {
		if kb == nil || kb.ID == "" || kb.IsTemporary {
			continue
		}
		if _, dup := seen[kb.ID]; dup {
			continue
		}
		seen[kb.ID] = struct{}{}
		existing = append(existing, kb)
	}
	return existing
}

// ListKnowledgeBasesByTenantID returns all knowledge bases for the given tenant (e.g. for shared agent context).
func (s *knowledgeBaseService) ListKnowledgeBasesByTenantID(ctx context.Context, tenantID uint64) ([]*types.KnowledgeBase, error) {
	// This method also lists a shared agent's source-tenant KBs for a
	// foreign caller — org-scoped KBs must never cross that boundary.
	caller := types.CallerFromContext(ctx)
	var kbs []*types.KnowledgeBase
	var err error
	if caller.TenantID != 0 && caller.TenantID == tenantID {
		kbs, err = s.repo.ListKnowledgeBasesByTenantID(ctx, tenantID)
	} else {
		kbs, err = s.repo.ListForeignKnowledgeBasesByTenantID(ctx, tenantID)
	}
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		return nil, err
	}
	for _, kb := range kbs {
		kb.EnsureDefaults()
		switch kb.Type {
		case types.KnowledgeBaseTypeDocument:
			if cnt, err := s.kgRepo.CountKnowledgeByKnowledgeBaseID(ctx, tenantID, kb.ID); err == nil {
				kb.KnowledgeCount = cnt
			}
		case types.KnowledgeBaseTypeFAQ:
			if cnt, err := s.chunkRepo.CountChunksByKnowledgeBaseID(ctx, tenantID, kb.ID); err == nil {
				kb.ChunkCount = cnt
			}
		}
		if processingCount, err := s.kgRepo.CountKnowledgeByStatus(ctx, tenantID, kb.ID, []string{"pending", "processing"}); err == nil {
			kb.IsProcessing = processingCount > 0
			kb.ProcessingCount = processingCount
		}
	}

	// Stamp pin state from the caller's perspective. The tenantID
	// argument may not match the caller's own tenant (this method is
	// also used to list a shared-agent's source-tenant KBs); we still
	// scope user_kb_pins by `tenantID` since a pin tied to one tenant
	// shouldn't surface when browsing another tenant's KBs.
	if userID, ok := types.UserIDFromContext(ctx); ok && userID != "" {
		s.applyUserKBPins(ctx, tenantID, userID, kbs)
	}
	return kbs, nil
}

// FillKnowledgeBaseCounts fills KnowledgeCount, ChunkCount, IsProcessing, ProcessingCount for the given KB using kb.TenantID.
func (s *knowledgeBaseService) FillKnowledgeBaseCounts(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil {
		return nil
	}
	tenantID := kb.TenantID
	kb.EnsureDefaults()
	switch kb.Type {
	case types.KnowledgeBaseTypeDocument:
		if cnt, err := s.kgRepo.CountKnowledgeByKnowledgeBaseID(ctx, tenantID, kb.ID); err == nil {
			kb.KnowledgeCount = cnt
		}
	case types.KnowledgeBaseTypeFAQ:
		if cnt, err := s.chunkRepo.CountChunksByKnowledgeBaseID(ctx, tenantID, kb.ID); err == nil {
			kb.ChunkCount = cnt
		}
	}
	if processingCount, err := s.kgRepo.CountKnowledgeByStatus(ctx, tenantID, kb.ID, []string{"pending", "processing"}); err == nil {
		kb.IsProcessing = processingCount > 0
		kb.ProcessingCount = processingCount
	}
	return nil
}

// UpdateKnowledgeBase updates a knowledge base's mutable properties.
//
// IMPORTANT — vector_store_id immutability contract:
// The vector_store_id binding is deliberately not accepted by this method.
// Two layers enforce immutability:
//
//  1. ORM layer: the GORM tag `<-:create` on KnowledgeBase.VectorStoreID
//     makes every UPDATE path (Save / Updates / Select-Updates) a no-op for
//     that column. Verified by repository/knowledgebase_sqlite_test.go.
//  2. Service layer: this method intentionally omits VectorStoreID from its
//     parameter list, and the matching handler DTO UpdateKnowledgeBaseRequest
//     omits the field as well. A reflection-based regression test
//     (handler/knowledgebase_request_test.go) fails if either DTO field
//     is added back, alerting future maintainers.
//
// Any future cross-store rebind workflow must use raw SQL through a
// dedicated repository method — the only sanctioned write path post-creation.
func (s *knowledgeBaseService) UpdateKnowledgeBase(ctx context.Context,
	id string,
	name string,
	description string,
	config *types.KnowledgeBaseConfig,
) (*types.KnowledgeBase, error) {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	logger.Infof(ctx, "Updating knowledge base, ID: %s, name: %s", id, name)

	// Get existing knowledge base
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}
	// Owner-aware lifecycle guard: platform-owned rows admit only an
	// explicit human SuperAdmin; tenant-owned rows require the caller to
	// act inside the owning tenant. Route role floors stay authoritative
	// for the owning tenant's own policy; this check keeps platform
	// authority from overriding tenant ownership for direct callers.
	if err := requireKBLifecycleAccess(ctx, kb); err != nil {
		return nil, err
	}

	changedFields := make([]string, 0, 3)
	profileWasEnabled := kb.ProfileConfig.IsEnabled()
	if kb.Name != name {
		changedFields = append(changedFields, "name")
	}
	if kb.Description != description {
		changedFields = append(changedFields, "description")
	}
	if config != nil {
		changedFields = append(changedFields, "config")
	}

	// Update the knowledge base properties
	kb.Name = name
	kb.Description = description
	if config != nil {
		kb.ChunkingConfig = config.ChunkingConfig
		kb.ImageProcessingConfig = config.ImageProcessingConfig
		if config.FAQConfig != nil {
			kb.FAQConfig = config.FAQConfig
		}
		if config.WikiConfig != nil {
			kb.WikiConfig = config.WikiConfig
		}
		if config.AutoTagConfig != nil {
			config.AutoTagConfig.Normalize()
			kb.AutoTagConfig = config.AutoTagConfig
		}
		if config.ProfileConfig != nil {
			profileWasEnabled = kb.ProfileConfig.IsEnabled()
			kb.ProfileConfig = config.ProfileConfig
		}
		// Update indexing strategy — syncs to ExtractConfig for backward compat
		if config.IndexingStrategy != nil {
			if !config.IndexingStrategy.HasAnyIndexing() {
				return nil, errors.New("at least one indexing strategy must be enabled")
			}
			kb.IndexingStrategy = *config.IndexingStrategy
			// Ensure WikiConfig exists when wiki indexing is enabled so that
			// wiki-specific tunables (synthesis model, granularity, …) have a home.
			if kb.WikiConfig == nil && config.IndexingStrategy.WikiEnabled {
				kb.WikiConfig = &types.WikiConfig{}
			}
			// Sync GraphEnabled → ExtractConfig
			if kb.ExtractConfig != nil {
				kb.ExtractConfig.Enabled = config.IndexingStrategy.GraphEnabled
			} else if config.IndexingStrategy.GraphEnabled {
				kb.ExtractConfig = &types.ExtractConfig{Enabled: true}
			}
		}
	}
	kb.UpdatedAt = time.Now()
	kb.EnsureDefaults()

	logger.Info(ctx, "Saving knowledge base update")
	if err := s.repo.UpdateKnowledgeBase(ctx, kb); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}
	recordKBActivity(ctx, s.audit, kb.TenantID, kb.ID, types.AuditActionKBUpdated,
		"knowledge_base", kb.ID, types.AuditOutcomeSuccess, map[string]any{
			"name": kb.Name, "changed_fields": changedFields,
		})
	// Turning automatic description generation on should produce a
	// description now, not after the next upload.
	if !profileWasEnabled && kb.ProfileConfig.IsEnabled() {
		_ = requestKnowledgeBaseProfileRefresh(ctx, s.asynqClient, kb, false)
	}

	logger.Infof(ctx, "Knowledge base updated successfully, ID: %s, name: %s", kb.ID, kb.Name)
	return kb, nil
}

// TogglePinKnowledgeBase toggles whether the calling user has pinned
// this knowledge base. Pin state is per-(user, kb) as of migration
// 000050; previously this method flipped a tenant-wide column on the
// KB row which broke down under RBAC (only Admin/creator could pin,
// and the pin reordered the list for everyone in the tenant). The
// public signature is unchanged so the HTTP handler / CLI / SDK don't
// move.
//
// The KB still has to belong to the caller's tenant — the route is
// already gated behind KBAccessRead, but we re-check via
// GetKnowledgeBaseByIDAndTenant so a stale param survives a tenant
// switch cleanly.
func (s *knowledgeBaseService) TogglePinKnowledgeBase(
	ctx context.Context, id string,
) (*types.KnowledgeBase, error) {
	if id == "" {
		return nil, errors.New("knowledge base ID cannot be empty")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	userID, ok := types.UserIDFromContext(ctx)
	if !ok || userID == "" {
		// API-key callers without a user identity can't have a personal
		// pin set. We surface this rather than silently flipping a
		// shared-tenant flag like the old behaviour.
		return nil, errors.New("pin requires an authenticated user")
	}

	// Look the KB up without a tenant filter: the route's KBAccessRead
	// guard already validated that this caller can see this KB (own,
	// org-shared, or agent-shared). Filtering by the caller's tenant
	// here would 404 every legitimate pin against a shared KB whose
	// owning tenant differs from the caller's active tenant.
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
			"tenant_id":         tenantID,
		})
		return nil, err
	}

	// Read current pin state to decide direction. ListUserKBPinIDs is
	// already optimised for the "many KBs at once" path; for a single-id
	// check the round-trip is acceptable and avoids leaking a second
	// repository method just for this.
	pins, err := s.repo.ListUserKBPinIDs(ctx, tenantID, userID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
			"tenant_id":         tenantID,
			"user_id":           userID,
		})
		return nil, err
	}
	_, currentlyPinned := pins[id]

	pinnedAt, err := s.repo.SetUserKBPin(ctx, tenantID, userID, id, !currentlyPinned)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
			"tenant_id":         tenantID,
			"user_id":           userID,
			"target_pinned":     !currentlyPinned,
		})
		return nil, err
	}

	kb.EnsureDefaults()
	kb.IsPinned = !currentlyPinned
	kb.PinnedAt = pinnedAt
	logger.Infof(ctx, "Knowledge base pin toggled, ID: %s, user: %s, is_pinned: %v",
		id, userID, kb.IsPinned)
	return kb, nil
}

// applyUserKBPins stamps IsPinned / PinnedAt onto each KB in the slice
// from the caller's perspective and sorts the slice so pinned rows
// float to the top (newest pin first, ties broken by created_at desc).
// Safe to call with an empty userID (no-op stamp; default sort by
// created_at preserved).
func (s *knowledgeBaseService) applyUserKBPins(
	ctx context.Context, tenantID uint64, userID string, kbs []*types.KnowledgeBase,
) {
	if len(kbs) == 0 || userID == "" {
		return
	}
	pins, err := s.repo.ListUserKBPinIDs(ctx, tenantID, userID)
	if err != nil {
		// Pin enrichment is best-effort: a transient DB blip here
		// should not break listing KBs. Log and bail without altering
		// the slice — caller still gets a valid list, just unsorted by
		// pin.
		logger.Warnf(ctx, "applyUserKBPins: failed to load pins for tenant=%d user=%s: %v",
			tenantID, userID, err)
		return
	}
	if len(pins) == 0 {
		return
	}
	for _, kb := range kbs {
		if ts, ok := pins[kb.ID]; ok {
			kb.IsPinned = true
			t := ts
			kb.PinnedAt = &t
		}
	}
	sort.SliceStable(kbs, func(i, j int) bool {
		a, b := kbs[i], kbs[j]
		if a.IsPinned != b.IsPinned {
			return a.IsPinned
		}
		if a.IsPinned && b.IsPinned {
			at, bt := a.PinnedAt, b.PinnedAt
			if at != nil && bt != nil && !at.Equal(*bt) {
				return at.After(*bt)
			}
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
}

// DeleteKnowledgeBase deletes a knowledge base by its ID
// This method marks the knowledge base as deleted and enqueues an async task
// to handle the heavy cleanup operations (embeddings, chunks, files, graph data)
func (s *knowledgeBaseService) DeleteKnowledgeBase(ctx context.Context, id string) error {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return errors.New("knowledge base ID cannot be empty")
	}

	logger.Infof(ctx, "Deleting knowledge base, ID: %s", id)

	// Execution scope only: tenant 0 is legitimate for platform-owned
	// rows (explicit human SuperAdmin without an active tenant). The
	// owner-aware check below decides authority; never assume the
	// caller's tenant owns the row.
	tenantID, _ := types.TenantIDFromContext(ctx)
	tenantInfo, _ := types.TenantInfoFromContext(ctx)

	// Load the KB before soft-delete so we can snapshot its VectorStoreID
	// into the async cleanup payload. GORM's soft-delete filter hides the
	// row from subsequent reads, so this read must happen first.
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return err
	}
	var vectorStoreIDSnapshot *string
	if kb != nil {
		vectorStoreIDSnapshot = kb.VectorStoreID
	}
	// Owner-aware lifecycle guard (see UpdateKnowledgeBase): a
	// tenantless/out-of-tenant SuperAdmin must not delete tenant-owned
	// KBs; platform-owned rows require explicit human SuperAdmin.
	if err := requireKBLifecycleAccess(ctx, kb); err != nil {
		return err
	}

	// Step 1: Delete the knowledge base record first (mark as deleted)
	logger.Infof(ctx, "Deleting knowledge base from database")
	err = s.repo.DeleteKnowledgeBase(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return err
	}
	deletedName := ""
	if kb != nil {
		deletedName = kb.Name
	}
	// Platform-owned rows have no owning tenant, and recordKBActivity
	// drops tenant-0 events: log those deletes directly so the trail
	// survives. Tenant-owned rows keep the existing activity path.
	if kb != nil && kb.OwnerTenantID == 0 && kb.TenantID == 0 {
		s.logKBAuditAllowZero(ctx, 0, id, types.AuditActionKBDeleted, types.AuditOutcomeSuccess)
	} else {
		recordKBActivity(ctx, s.audit, tenantID, id, types.AuditActionKBDeleted,
			"knowledge_base", id, types.AuditOutcomeSuccess, map[string]any{"name": deletedName})
	}

	// Stop both ephemeral queue work and durable wiki operations that target
	// the now-deleted KB. ProcessKBDelete repeats this with document IDs and
	// performs one final scrub after heavy cleanup to close enqueue races.
	//
	// Run detached with a bounded timeout so a disconnecting API client cannot
	// truncate this best-effort scrub mid-scan, matching ProcessKBDelete's
	// cleanup semantics. The KB row is already soft-deleted, so the async
	// delete task remains the durable backstop even if this pass is cut short.
	kbCleanupCtx, cancelKBCleanup := context.WithTimeout(
		context.WithoutCancel(ctx), kbTaskCleanupTimeout,
	)
	s.cleanupTasksForKnowledgeBase(kbCleanupCtx, id, nil, nil)
	cancelKBCleanup()

	// Step 1b: Revoke all access grants for this KB so grantee tenants lose access.
	if s.kbAccessGrantService != nil {
		if delErr := s.kbAccessGrantService.DeleteAllForKB(ctx, id); delErr != nil {
			logger.Warnf(ctx, "Failed to delete KB access grants for knowledge base %s: %v", id, delErr)
		}
	}

	// Step 1c: Stop and soft-delete all data sources bound to this KB so cron
	// schedules and in-flight sync logs do not keep running against a deleted KB.
	dataSourceIDs := s.deleteDataSourcesForKnowledgeBase(ctx, id)
	if len(dataSourceIDs) > 0 {
		dsCancelCtx, cancelDSCancel := context.WithTimeout(
			context.WithoutCancel(ctx), kbTaskCleanupTimeout,
		)
		s.cancelTasksForKnowledgeBase(dsCancelCtx, id, nil, dataSourceIDs)
		cancelDSCancel()
	}

	// Step 2: Enqueue async task for heavy cleanup operations.
	// tenantInfo is absent for tenantless platform deletes; fall back to
	// the system-default engines so the payload stays well-formed.
	var effectiveEngines []types.RetrieverEngineParams
	if tenantInfo != nil {
		effectiveEngines = tenantInfo.GetEffectiveEngines()
	} else {
		effectiveEngines = types.GetDefaultRetrieverEngines()
	}
	payload := types.KBDeletePayload{
		TenantID:         tenantID,
		KnowledgeBaseID:  id,
		DataSourceIDs:    dataSourceIDs,
		EffectiveEngines: effectiveEngines,
		VectorStoreID:    vectorStoreIDSnapshot, // snapshot taken before soft-delete
	}
	langfuse.InjectTracing(ctx, &payload)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Warnf(ctx, "Failed to marshal KB delete payload: %v", err)
		// Don't fail the request, the KB record is already deleted
		return nil
	}

	task := asynq.NewTask(types.TypeKBDelete, payloadBytes,
		asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(3), asynq.Timeout(2*time.Hour))
	info, err := s.asynqClient.Enqueue(task)
	if err != nil {
		logger.Warnf(ctx, "Failed to enqueue KB delete task: %v", err)
		// Don't fail the request, the KB record is already deleted
		return nil
	}

	logger.Infof(ctx, "KB delete task enqueued: %s, knowledge base ID: %s", info.ID, id)
	logger.Infof(ctx, "Knowledge base deleted successfully, ID: %s", id)
	return nil
}

// ProcessKBDelete handles async knowledge base deletion task
// This method performs heavy cleanup operations: deleting embeddings, chunks, files, and graph data
func (s *knowledgeBaseService) ProcessKBDelete(ctx context.Context, t *asynq.Task) (err error) {
	var payload types.KBDeletePayload
	if unmarshalErr := json.Unmarshal(t.Payload(), &payload); unmarshalErr != nil {
		logger.Errorf(ctx, "Failed to unmarshal KB delete payload: %v", unmarshalErr)
		return unmarshalErr
	}

	tenantID := payload.TenantID
	kbID := payload.KnowledgeBaseID
	var knowledgeIDs []string

	// Set tenant context for downstream services
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	defer func() {
		// Wiki rows are independent of the vector engine. Run this on every
		// return path — including SkipRetry — with its own timeout so a slow
		// Redis queue scan cannot starve the SQL deletes.
		wikiCtx, cancelWiki := context.WithTimeout(context.WithoutCancel(ctx), kbTaskCleanupTimeout)
		wikiErr := s.cleanupWikiForKnowledgeBase(wikiCtx, tenantID, kbID)
		cancelWiki()
		if wikiErr != nil {
			logger.Warnf(ctx, "Failed to clean wiki data for KB %s: %v", kbID, wikiErr)
			// SkipRetry would otherwise drop the task while wiki orphans
			// remain. Promote the wiki error so asynq retries; a later
			// attempt that succeeds at wiki cleanup can still SkipRetry.
			if err == nil || errors.Is(err, asynq.SkipRetry) {
				err = wikiErr
			}
		} else if err == nil {
			logger.Infof(ctx, "KB delete task completed successfully, knowledge base ID: %s", kbID)
		}

		// Workers may enqueue downstream work while the delete task performs
		// heavy storage cleanup. A detached, bounded final scrub runs on every
		// return path, including retryable failures and cancellation.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kbTaskCleanupTimeout)
		defer cancel()
		s.cleanupTasksForKnowledgeBase(cleanupCtx, kbID, knowledgeIDs, payload.DataSourceIDs)
	}()

	logger.Infof(ctx, "Processing KB delete task for knowledge base: %s", kbID)

	// Step 1: Get all knowledge entries in this knowledge base
	logger.Infof(ctx, "Fetching all knowledge entries in knowledge base, ID: %s", kbID)
	knowledgeList, err := s.kgRepo.ListKnowledgeByKnowledgeBaseID(ctx, tenantID, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": kbID,
		})
		return err
	}
	logger.Infof(ctx, "Found %d knowledge entries to delete", len(knowledgeList))
	knowledgeIDs = make([]string, 0, len(knowledgeList))
	for _, knowledge := range knowledgeList {
		knowledgeIDs = append(knowledgeIDs, knowledge.ID)
	}

	// Repeat the best-effort queue scrub with document IDs. Some batch tasks
	// only carry knowledge_id(s), and active work from the first pass may have
	// enqueued another downstream task before cancellation reached it.
	s.cleanupTasksForKnowledgeBase(ctx, kbID, knowledgeIDs, payload.DataSourceIDs)

	// Step 2: Delete all knowledge entries and their resources
	if len(knowledgeList) > 0 {
		logger.Infof(ctx, "Deleting all knowledge entries and their resources")

		// Delete embeddings from vector store.
		// Resolve the engine via the factory, using the VectorStoreID captured
		// at enqueue time (may be nil → falls back to payload.EffectiveEngines).
		// If the payload references a store no longer owned/registered
		// (e.g. tampered queue entry or a store that was deleted while the
		// task sat in the queue), the factory returns a sentinel and we
		// SkipRetry to avoid burning retries on an unrecoverable situation.
		logger.Infof(ctx, "Deleting embeddings from vector store")
		retrieveEngine, err := retriever.CreateRetrieveEngineFromPayload(
			ctx,
			s.retrieveEngine,
			s.ownership,
			payload.TenantID,
			payload.EffectiveEngines,
			payload.VectorStoreID,
		)
		if errors.Is(err, retriever.ErrVectorStoreForbidden) ||
			errors.Is(err, retriever.ErrVectorStoreNotFound) {
			logger.Errorf(ctx, "KB delete task aborted: %v (tenant=%d, kb=%s)", err, payload.TenantID, payload.KnowledgeBaseID)
			return asynq.SkipRetry
		}
		if err != nil {
			// Transient failures — store temporarily unavailable, request
			// cancellation during resolution, or other retryable errors —
			// must not fall through and report success while embeddings remain.
			logger.Errorf(ctx, "KB delete task deferred: %v (tenant=%d, kb=%s)", err, payload.TenantID, payload.KnowledgeBaseID)
			return err
		} else {
			// Group knowledge by embedding model and type
			type groupKey struct {
				EmbeddingModelID string
				Type             string
			}
			embeddingGroups := make(map[groupKey][]string)
			for _, knowledge := range knowledgeList {
				key := groupKey{EmbeddingModelID: knowledge.EmbeddingModelID, Type: knowledge.Type}
				embeddingGroups[key] = append(embeddingGroups[key], knowledge.ID)
			}

			for key, knowledgeGroup := range embeddingGroups {
				embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, key.EmbeddingModelID)
				if err != nil {
					logger.Warnf(ctx, "Failed to get embedding model %s: %v", key.EmbeddingModelID, err)
					continue
				}
				if err := retrieveEngine.DeleteByKnowledgeIDList(ctx, knowledgeGroup, embeddingModel.GetDimensions(), key.Type); err != nil {
					logger.Warnf(ctx, "Failed to delete embeddings for model %s: %v", key.EmbeddingModelID, err)
				}
			}
		}

		// Collect image URLs before chunks are deleted
		chunkImageInfos, imgErr := s.chunkRepo.ListImageInfoByKnowledgeIDs(ctx, tenantID, knowledgeIDs)
		if imgErr != nil {
			logger.Warnf(ctx, "Failed to collect image URLs for KB delete: %v", imgErr)
		}
		var imageInfoStrs []string
		for _, ci := range chunkImageInfos {
			imageInfoStrs = append(imageInfoStrs, ci.ImageInfo)
		}
		imageURLs := collectImageURLs(ctx, imageInfoStrs)

		// Delete all chunks
		logger.Infof(ctx, "Deleting all chunks in knowledge base")
		for _, knowledgeID := range knowledgeIDs {
			if err := s.chunkRepo.DeleteChunksByKnowledgeID(ctx, tenantID, knowledgeID); err != nil {
				logger.Warnf(ctx, "Failed to delete chunks for knowledge %s: %v", knowledgeID, err)
			}
		}

		// Delete physical files, extracted images, and adjust storage
		logger.Infof(ctx, "Deleting physical files and extracted images")
		storageAdjust := int64(0)
		for _, knowledge := range knowledgeList {
			if knowledge.FilePath != "" {
				if err := s.fileSvc.DeleteFile(ctx, knowledge.FilePath); err != nil {
					logger.Warnf(ctx, "Failed to delete file %s: %v", knowledge.FilePath, err)
				}
			}
			storageAdjust -= knowledge.StorageSize
		}
		deleteExtractedImages(ctx, s.fileSvc, knowledgeResourceOwners(s.resourceCatalog, knowledgeIDs...), imageURLs)
		if storageAdjust != 0 {
			if err := s.tenantRepo.AdjustStorageUsed(ctx, tenantID, storageAdjust); err != nil {
				logger.Warnf(ctx, "Failed to adjust tenant storage: %v", err)
			}
		}

		// Delete knowledge graph data
		logger.Infof(ctx, "Deleting knowledge graph data")
		namespaces := make([]types.NameSpace, 0, len(knowledgeList))
		for _, knowledge := range knowledgeList {
			namespaces = append(namespaces, types.NameSpace{
				KnowledgeBase: knowledge.KnowledgeBaseID,
				Knowledge:     knowledge.ID,
			})
		}
		if s.graphEngine != nil && len(namespaces) > 0 {
			if err := s.graphEngine.DelGraph(ctx, namespaces); err != nil {
				logger.Warnf(ctx, "Failed to delete knowledge graph: %v", err)
			}
		}

		// Delete all knowledge entries from database
		logger.Infof(ctx, "Deleting knowledge entries from database")
		if err := s.kgRepo.DeleteKnowledgeList(ctx, tenantID, knowledgeIDs); err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"knowledge_base_id": kbID,
			})
			return err
		}
	}

	logger.Infof(ctx, "KB resource cleanup finished, knowledge base ID: %s", kbID)
	return nil
}

// cleanupWikiForKnowledgeBase removes wiki pages, folders, revisions, and
// issues for a deleted knowledge base. Pages, folders, and issues are
// soft-deleted (they carry DeletedAt); revisions are hard-deleted (no
// deleted_at column — they are immutable snapshots).
//
// nil-safe for tests that construct knowledgeBaseService without wikiRepo.
// Failures are returned so the KB delete task can retry; the caller logs.
func (s *knowledgeBaseService) cleanupWikiForKnowledgeBase(
	ctx context.Context, tenantID uint64, kbID string,
) error {
	if s.wikiRepo == nil || kbID == "" {
		return nil
	}
	logger.Infof(ctx, "Cleaning up wiki data for knowledge base")
	var errs error
	if err := s.wikiRepo.DeleteByKnowledgeBaseID(ctx, tenantID, kbID); err != nil {
		logger.Warnf(ctx, "Failed to delete wiki pages for KB %s: %v", kbID, err)
		errs = errors.Join(errs, err)
	}
	if err := s.wikiRepo.DeleteFoldersByKnowledgeBaseID(ctx, tenantID, kbID); err != nil {
		logger.Warnf(ctx, "Failed to delete wiki folders for KB %s: %v", kbID, err)
		errs = errors.Join(errs, err)
	}
	if err := s.wikiRepo.DeleteRevisionsByKnowledgeBaseID(ctx, tenantID, kbID); err != nil {
		logger.Warnf(ctx, "Failed to delete wiki revisions for KB %s: %v", kbID, err)
		errs = errors.Join(errs, err)
	}
	if err := s.wikiRepo.DeleteIssuesByKnowledgeBaseID(ctx, tenantID, kbID); err != nil {
		logger.Warnf(ctx, "Failed to delete wiki issues for KB %s: %v", kbID, err)
		errs = errors.Join(errs, err)
	}
	return errs
}

// cancelTasksForKnowledgeBase removes queue work for a deleted KB when the
// configured task backend supports knowledge-base-wide inspection. Queue
// cleanup is an optimization: the soft-deleted database row remains the
// durable source of truth, so backend failures must not fail KB deletion.
func (s *knowledgeBaseService) cancelTasksForKnowledgeBase(
	ctx context.Context,
	kbID string,
	knowledgeIDs []string,
	dataSourceIDs []string,
) {
	canceller, ok := s.taskInspector.(interfaces.KnowledgeBaseTaskCanceller)
	if !ok || kbID == "" {
		return
	}
	if _, _, err := canceller.CancelTasksForKnowledgeBase(ctx, kbID, knowledgeIDs, dataSourceIDs); err != nil {
		logger.Warnf(ctx, "Failed to cancel queued tasks for deleted KB %s: %v", kbID, err)
	}
}

// cleanupTasksForKnowledgeBase removes both asynq records and durable wiki
// operations. The latter must be cleared as well or startup recovery can
// recreate Redis triggers for a KB that no longer exists.
func (s *knowledgeBaseService) cleanupTasksForKnowledgeBase(
	ctx context.Context,
	kbID string,
	knowledgeIDs []string,
	dataSourceIDs []string,
) {
	if kbID == "" {
		return
	}
	cleaner, ok := s.taskPendingRepo.(interfaces.TaskPendingOpsScopeCleaner)
	if ok {
		// Clear durable work before scanning Redis. A large or degraded queue
		// must not consume the caller's entire deadline and starve the database
		// fence that prevents startup recovery from reviving this KB.
		if err := cleaner.DeleteByScope(ctx, types.TaskScopeKnowledgeBase, kbID); err != nil {
			logger.Warnf(ctx, "Failed to clear durable tasks for deleted KB %s: %v", kbID, err)
		}
	}
	s.cancelTasksForKnowledgeBase(ctx, kbID, knowledgeIDs, dataSourceIDs)
}

// deleteDataSourcesForKnowledgeBase mirrors DataSourceService.DeleteDataSource for
// every data source attached to the KB. Errors on individual sources are logged
// but do not fail KB deletion — the KB record is already soft-deleted.
func (s *knowledgeBaseService) deleteDataSourcesForKnowledgeBase(ctx context.Context, kbID string) []string {
	if s.dsRepo == nil {
		return nil
	}

	dataSources, err := s.dsRepo.FindByKnowledgeBase(ctx, kbID)
	if err != nil {
		logger.Warnf(ctx, "Failed to list data sources for deleted KB %s: %v", kbID, err)
		return nil
	}
	dataSourceIDs := make([]string, 0, len(dataSources))
	for _, ds := range dataSources {
		if ds == nil || ds.ID == "" {
			continue
		}
		dataSourceIDs = append(dataSourceIDs, ds.ID)
		if err := s.dsRepo.Delete(ctx, ds.ID); err != nil {
			logger.Warnf(ctx, "Failed to delete data source %s for KB %s: %v", ds.ID, kbID, err)
			continue
		}
		if s.dsScheduler != nil {
			s.dsScheduler.Remove(ds.ID)
		}
		if s.syncLogRepo != nil {
			if err := s.syncLogRepo.CancelPendingByDataSource(ctx, ds.ID); err != nil {
				logger.Warnf(ctx, "Failed to cancel pending sync logs for ds=%s (kb=%s): %v", ds.ID, kbID, err)
			}
		}
		logger.Infof(ctx, "Data source deleted with knowledge base: ds=%s kb=%s", ds.ID, kbID)
	}
	return dataSourceIDs
}

// SetEmbeddingModel sets the embedding model for a knowledge base
func (s *knowledgeBaseService) SetEmbeddingModel(ctx context.Context, id string, modelID string) error {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return errors.New("knowledge base ID cannot be empty")
	}

	if modelID == "" {
		logger.Error(ctx, "Model ID is empty")
		return errors.New("model ID cannot be empty")
	}

	logger.Infof(ctx, "Setting embedding model for knowledge base, knowledge base ID: %s, model ID: %s", id, modelID)

	// Get the knowledge base
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return err
	}

	// Update the knowledge base's embedding model
	kb.EmbeddingModelID = modelID
	kb.UpdatedAt = time.Now()

	logger.Info(ctx, "Saving knowledge base embedding model update")
	err = s.repo.UpdateKnowledgeBase(ctx, kb)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id":  id,
			"embedding_model_id": modelID,
		})
		return err
	}

	logger.Infof(
		ctx,
		"Knowledge base embedding model set successfully, knowledge base ID: %s, model ID: %s",
		id,
		modelID,
	)
	return nil
}

// CopyKnowledgeBase copies a knowledge base to a new knowledge base (shallow copy).
// Source and target must belong to the tenant in context; cross-tenant access is rejected.
//
// The transfer grant fixes both resource IDs and whether a new destination
// may be created. Existing targets must be compatible with the source. A
// worker retry reloads the reserved target instead of creating another KB.
func (s *knowledgeBaseService) CopyKnowledgeBase(ctx context.Context,
	srcKB string, dstKB string,
) (*types.KnowledgeBase, *types.KnowledgeBase, error) {
	destinationID, create, creatorID, err := access.CloneDestination(ctx, srcKB)
	if err != nil {
		return nil, nil, err
	}
	if dstKB != "" && dstKB != destinationID {
		return nil, nil, access.ErrForbidden
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	sourceKB, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, srcKB, tenantID)
	if err != nil {
		return nil, nil, err
	}
	if sourceKB == nil || sourceKB.ID != srcKB || sourceKB.TenantID != tenantID {
		return nil, nil, access.ErrForbidden
	}
	sourceCopy := *sourceKB
	sourceKB = &sourceCopy
	sourceKB.EnsureDefaults()
	targetKB, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, destinationID, tenantID)
	if err != nil && (!create || !errors.Is(err, repository.ErrKnowledgeBaseNotFound)) {
		return nil, nil, err
	}
	if targetKB != nil {
		targetCopy := *targetKB
		targetKB = &targetCopy
		targetKB.EnsureDefaults()
		if err := access.RequireKBTransfer(ctx, sourceKB, targetKB, access.KBTransferClone); err != nil {
			return nil, nil, err
		}
		tenant, _ := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
		if err := access.ValidateKBTransferCompatibility(sourceKB,
			targetKB,
			access.KBTransferClone,
			"",
			tenant); err != nil {
			return nil, nil, apperrors.NewBadRequestError(err.Error())
		}
	} else {
		reserved := &types.KnowledgeBase{ID: destinationID, TenantID: tenantID}
		if !create {
			return nil, nil, access.ErrNotFound
		}
		if err := access.RequireKBTransfer(ctx, sourceKB, reserved, access.KBTransferClone); err != nil {
			return nil, nil, err
		}
		var faqConfig *types.FAQConfig
		if sourceKB.FAQConfig != nil {
			cfg := *sourceKB.FAQConfig
			faqConfig = &cfg
		}
		var profileConfig *types.KnowledgeBaseProfileConfig
		if sourceKB.ProfileConfig != nil {
			cfg := *sourceKB.ProfileConfig
			profileConfig = &cfg
		}
		// Preserve VectorStoreID so the cloned KB lands on the same
		// physical index. GORM `<-:create` permits the value at INSERT.
		targetKB = &types.KnowledgeBase{
			ID:          destinationID,
			CreatorID:   creatorID,
			Name:        sourceKB.Name,
			Type:        sourceKB.Type,
			Description: sourceKB.Description,
			TenantID:    tenantID,
			// The clone is a fresh tenant-owned row: ownership is
			// stamped from the execution scope, never inherited from
			// the source's owner/visibility.
			OwnerTenantID:         tenantID,
			Visibility:            types.KBVisibilityTenant,
			ChunkingConfig:        sourceKB.ChunkingConfig,
			ImageProcessingConfig: sourceKB.ImageProcessingConfig,
			EmbeddingModelID:      sourceKB.EmbeddingModelID,
			SummaryModelID:        sourceKB.SummaryModelID,
			VLMConfig:             sourceKB.VLMConfig,
			StorageProviderConfig: sourceKB.StorageProviderConfig,
			StorageBackendID:      sourceKB.StorageBackendID,
			StorageConfig:         sourceKB.StorageConfig,
			FAQConfig:             faqConfig,
			ProfileConfig:         profileConfig,
			VectorStoreID:         sourceKB.VectorStoreID,
		}
		// The clone is owned by the caller, not the original creator —
		// otherwise a Contributor copying someone else's KB would still
		// not be able to edit the result. Skip synthetic API-key users
		// (see CreateKnowledgeBase for the same reasoning).
		if uid, ok := types.UserIDFromContext(ctx); ok && !types.IsSyntheticUserID(uid) {
			targetKB.CreatorID = uid
		}
		targetKB.EnsureDefaults()
		// Validate owner/visibility before persistence.
		if err := targetKB.ValidateOwnership(); err != nil {
			return nil, nil, apperrors.NewBadRequestError(err.Error())
		}
		if err := s.repo.CreateKnowledgeBase(ctx, targetKB); err != nil {
			return nil, nil, err
		}
	}
	return sourceKB, targetKB, nil
}

// DuplicateKnowledgeBase creates a new KB from the source KB's settings only.
// Runtime/content state is deliberately reset so this path never copies
// knowledge entries, chunks, FAQ content, wiki pages, indexes, shares or pins.
func (s *knowledgeBaseService) DuplicateKnowledgeBase(
	ctx context.Context,
	srcKB string,
) (*types.KnowledgeBase, error) {
	srcKB = strings.TrimSpace(srcKB)
	if srcKB == "" {
		return nil, apperrors.NewBadRequestError("source knowledge base ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)
	sourceKB, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, srcKB, tenantID)
	if err != nil {
		logger.Errorf(ctx, "Get source knowledge base failed: %v", err)
		return nil, err
	}
	sourceKB.EnsureDefaults()

	targetKB, err := cloneKnowledgeBaseConfiguration(sourceKB)
	if err != nil {
		return nil, err
	}
	// A duplicate copies settings, not content. The generated description is
	// derived from the source's documents, so carrying it over would describe
	// documents the copy does not have; ProfileConfig is kept so the copy
	// produces its own once documents arrive.
	targetKB.GeneratedProfile = nil
	targetKB.ID = uuid.New().String()
	targetKB.TenantID = tenantID
	// Scope is never inherited: the copy is a plain tenant-owned KB even
	// when the source carried platform ownership. Owner and visibility are
	// stamped together so the row always satisfies the owner/visibility
	// invariant before persistence.
	targetKB.OwnerTenantID = tenantID
	targetKB.Name = s.buildDuplicateKnowledgeBaseName(ctx, tenantID, sourceKB.Name)
	targetKB.CreatorID = ""
	if uid, ok := types.UserIDFromContext(ctx); ok && !types.IsSyntheticUserID(uid) {
		targetKB.CreatorID = uid
	}
	now := time.Now()
	targetKB.CreatedAt = now
	targetKB.UpdatedAt = now
	targetKB.DeletedAt.Valid = false
	targetKB.DeletedAt.Time = time.Time{}
	targetKB.IsTemporary = false
	// Scope is never inherited: duplicating a public KB must not
	// mint a new privileged-scope KB for the caller. The copy is a plain
	// tenant KB (owner and visibility stamped together, above and below,
	// so the row satisfies the owner/visibility invariant); a privileged
	// caller can re-scope it afterwards through SetKnowledgeBaseVisibility.
	targetKB.Visibility = types.KBVisibilityTenant
	targetKB.IsPinned = false
	targetKB.PinnedAt = nil
	targetKB.KnowledgeCount = 0
	targetKB.ChunkCount = 0
	targetKB.IsProcessing = false
	targetKB.ProcessingCount = 0
	targetKB.ShareCount = 0
	targetKB.CreatorName = ""
	targetKB.EnsureDefaults()
	targetKB.Normalize()
	// Validate owner/visibility before persistence.
	if err := targetKB.ValidateOwnership(); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}

	if targetKB.HasVectorStore() {
		if err := s.validateVectorStoreBinding(ctx, tenantID, *targetKB.VectorStoreID); err != nil {
			return nil, err
		}
	}

	if err := s.repo.CreateKnowledgeBase(ctx, targetKB); err != nil {
		return nil, err
	}
	recordKBActivity(ctx, s.audit, tenantID, targetKB.ID, types.AuditActionKBDuplicated,
		"knowledge_base", targetKB.ID, types.AuditOutcomeSuccess, map[string]any{
			"source_kb_id": sourceKB.ID, "name": targetKB.Name,
		})
	return targetKB, nil
}

func duplicateKBCopySuffix(locale string) string {
	locale = strings.ToLower(locale)
	switch {
	case strings.HasPrefix(locale, "zh"):
		return " 副本"
	case strings.HasPrefix(locale, "ko"):
		return " 사본"
	case strings.HasPrefix(locale, "ru"):
		return " копия"
	default:
		return " Copy"
	}
}

func duplicateKBDefaultName(locale string) string {
	locale = strings.ToLower(locale)
	switch {
	case strings.HasPrefix(locale, "zh"):
		return "知识库"
	case strings.HasPrefix(locale, "ko"):
		return "지식베이스"
	case strings.HasPrefix(locale, "ru"):
		return "База знаний"
	default:
		return "Knowledge Base"
	}
}

func (s *knowledgeBaseService) buildDuplicateKnowledgeBaseName(
	ctx context.Context,
	tenantID uint64,
	sourceName string,
) string {
	locale := types.LanguageFromContextOrDefault(ctx)
	suffix := duplicateKBCopySuffix(locale)

	baseName := strings.TrimSpace(sourceName)
	if baseName == "" {
		baseName = duplicateKBDefaultName(locale)
	}

	kbs, err := s.repo.ListKnowledgeBasesByTenantID(ctx, tenantID)
	if err != nil {
		logger.Warnf(ctx, "List tenant knowledge bases failed while building duplicate name: %v", err)
		return baseName + suffix
	}

	existing := make(map[string]struct{}, len(kbs))
	for _, kb := range kbs {
		if kb == nil {
			continue
		}
		existing[kb.Name] = struct{}{}
	}

	candidate := baseName + suffix
	if _, ok := existing[candidate]; !ok {
		return candidate
	}
	for i := 2; ; i++ {
		candidate = fmt.Sprintf("%s%s %d", baseName, suffix, i)
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
	}
}

func cloneKnowledgeBaseConfiguration(sourceKB *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	if sourceKB == nil {
		return nil, apperrors.NewBadRequestError("source knowledge base cannot be empty")
	}
	data, err := json.Marshal(sourceKB)
	if err != nil {
		return nil, err
	}
	var targetKB types.KnowledgeBase
	if err := json.Unmarshal(data, &targetKB); err != nil {
		return nil, err
	}
	return &targetKB, nil
}
