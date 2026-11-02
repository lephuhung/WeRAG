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
var ErrInvalidTenantID = errors.New("invalid organization ID")

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
	audit                interfaces.AuditLogService
	resourceCatalog      interfaces.ResourceCatalog
	wikiRepo             interfaces.WikiPageRepository
	subscriptionRepo     interfaces.KBSubscriptionRepository
	// legalReviews annotates results from reviewed legal documents with
	// their validity; nil in tests leaves results untouched.
	legalReviews interfaces.LegalReviewService
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
	audit interfaces.AuditLogService,
	resourceCatalog interfaces.ResourceCatalog,
	wikiRepo interfaces.WikiPageRepository,
	subscriptionRepo interfaces.KBSubscriptionRepository,
	legalReviews interfaces.LegalReviewService,
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
		audit:                audit,
		resourceCatalog:      resourceCatalog,
		wikiRepo:             wikiRepo,
		subscriptionRepo:     subscriptionRepo,
		legalReviews:         legalReviews,
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
	// Reject bad scope input before EnsureDefaults would silently coerce it.
	if err := validateCreateScopeInput(kb); err != nil {
		return nil, err
	}
	kb.EnsureDefaults()
	s.applyProfileDefaultModels(ctx, kb)
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
			return nil, apperrors.NewNotFoundError("organization not found")
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
		return apperrors.NewBadRequestError("organization context missing")
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

// validateCreateScopeInput rejects scope values the tenant create flow
// does not accept, instead of letting EnsureDefaults coerce them: an
// unknown member_default, 'published' (create as 'tenant', then publish
// through the visibility endpoint) and unknown visibility values are 400.
// Empty values take the defaults ('tenant', 'contribute'); 'public' is
// left to validateKBVisibility.
func validateCreateScopeInput(kb *types.KnowledgeBase) error {
	if kb.MemberDefault != "" && !kb.MemberDefault.IsValid() {
		return apperrors.NewBadRequestError(fmt.Sprintf(
			"invalid member_default %q: use %q or %q", kb.MemberDefault,
			types.KBMemberDefaultView, types.KBMemberDefaultContribute))
	}
	switch kb.Visibility {
	case "", types.KBVisibilityTenant, types.KBVisibilityRestricted, types.KBVisibilityPublic:
		return nil
	case types.KBVisibilityPublished:
		return apperrors.NewBadRequestError(
			"a knowledge base cannot be created as published: create it with visibility 'tenant', then publish it")
	default:
		return apperrors.NewBadRequestError(fmt.Sprintf(
			"invalid visibility %q: use %q or %q", kb.Visibility,
			types.KBVisibilityTenant, types.KBVisibilityRestricted))
	}
}

// validateKBVisibility enforces the scope model on the tenant create
// path. Tenant Admins create tenant-owned KBs only: a public visibility
// payload is rejected for every caller on this flow, including system
// admins — platform-owned public KBs are minted exclusively through
// CreatePublicKnowledgeBase. 'tenant' (the default) and 'restricted'
// (ACL only) are accepted; 'published' and unknown values were already
// rejected by validateCreateScopeInput — publishing is a later decision
// through the visibility endpoint.
func (s *knowledgeBaseService) validateKBVisibility(ctx context.Context, kb *types.KnowledgeBase) error {
	_ = ctx
	switch kb.Visibility {
	case types.KBVisibilityPublic:
		return apperrors.NewForbiddenError("public knowledge bases can only be created by a platform SuperAdmin through the public flow")
	case types.KBVisibilityRestricted:
	default:
		kb.Visibility = types.KBVisibilityTenant
	}
	return nil
}

// CreatePublicKnowledgeBase creates a platform-owned public knowledge base.
// Only an explicit human SuperAdmin (is_system_admin plus a real
// non-synthetic user, never an API-key principal) may invoke it —
// CanAccessAllTenants alone never qualifies. The row is stamped with
// owner 0 and the caller's workspace as data scope, and resolved from platform/global defaults: the
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
	// The KB's documents, files and indexes live in the creating
	// SuperAdmin's workspace: file storage and resource registration need a
	// real tenant, and tenant 0 is rejected there.
	if caller.TenantID == 0 {
		return nil, apperrors.NewBadRequestError("select an organization before creating a public knowledge base")
	}
	if kb.ID == "" {
		kb.ID = uuid.New().String()
	}
	now := time.Now()
	kb.CreatedAt = now
	kb.UpdatedAt = now
	// Platform owned (no owning tenant); TenantID is the immutable
	// execution partition for the KB's documents and indexes.
	kb.OwnerTenantID = 0
	kb.TenantID = caller.TenantID
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
		return nil, apperrors.NewBadRequestError("public knowledge bases cannot bind an organization storage backend")
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
	MemberDefault types.KBMemberDefault
	Known         bool
}

// newKBScopeOf projects the post-transition owner/visibility of a KB.
func newKBScopeOf(kb *types.KnowledgeBase) oldKBScope {
	if kb == nil {
		return oldKBScope{}
	}
	return oldKBScope{
		OwnerTenantID: kb.OwnerTenantID, Visibility: kb.Visibility, MemberDefault: kb.MemberDefault, Known: true,
	}
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
	s.recordKBScopeAudit(ctx, types.AuditActionKBScopeChanged, kbID, oldScope, newScope, targetTenantID, outcome, nil)
}

// recordKBScopeAudit is recordKBScopeChange with an explicit action and
// extra detail keys (e.g. revoked share counts on an owner transfer).
func (s *knowledgeBaseService) recordKBScopeAudit(
	ctx context.Context,
	action types.AuditAction,
	kbID string,
	oldScope, newScope oldKBScope,
	targetTenantID uint64,
	outcome types.AuditOutcome,
	extra map[string]any,
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
	if newScope.MemberDefault != "" {
		details["new_member_default"] = string(newScope.MemberDefault)
	}
	if oldScope.Known {
		details["old_owner_tenant"] = oldScope.OwnerTenantID
		details["old_visibility"] = string(oldScope.Visibility)
		if oldScope.MemberDefault != "" {
			details["old_member_default"] = string(oldScope.MemberDefault)
		}
	}
	if targetTenantID != 0 {
		details["target_tenant_id"] = targetTenantID
	}
	for k, v := range extra {
		details[k] = v
	}
	var detailJSON types.JSON
	if b, merr := json.Marshal(details); merr == nil {
		detailJSON = types.JSON(b)
	}
	_ = s.audit.Log(ctx, &types.AuditLog{
		TenantID:    tenantID,
		ActorUserID: actor,
		Action:      action,
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

// requireKBLifecycleAccess enforces KB lifecycle operations
// (update/delete) through access.DecideKB with ActionManage: an explicit
// human SuperAdmin on every KB, the owner tenant's Tenant Admin, and the
// owner tenant's machine principals (API keys keep their allow-list and
// capability checks elsewhere). Temporary KBs (chat/web-search scratch
// KBs cleaned up from their creator's session) need only Contribute, so
// a member's session cleanup keeps working.
func requireKBLifecycleAccess(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil || kb.ID == "" {
		return apperrors.NewNotFoundError("knowledge base not found")
	}
	action := access.ActionManage
	if kb.IsTemporary {
		action = access.ActionContribute
	}
	subject := kb
	if kb.OwnerTenantID == 0 && !access.IsPlatformPublicKB(kb) && kb.TenantID != 0 {
		// Legacy rows predating the owner backfill (000113): the data
		// scope is the only ownership signal available.
		legacy := *kb
		legacy.OwnerTenantID = kb.TenantID
		if !legacy.Visibility.IsValid() {
			legacy.Visibility = types.KBVisibilityTenant
		}
		subject = &legacy
	}
	_, err := access.DecideKB(ctx, types.CallerFromContext(ctx), subject, action, access.KBLookups{})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, access.ErrUnauthorized):
		return apperrors.NewUnauthorizedError("Unauthorized")
	case errors.Is(err, access.ErrNotFound):
		return apperrors.NewNotFoundError("knowledge base not found")
	default:
		return apperrors.NewForbiddenError("không có quyền quản lý knowledge base này")
	}
}

// SetKnowledgeBaseVisibility changes a KB's scope without changing its
// owner. Moving between the tenant-owned scopes (tenant, restricted,
// published) keeps the owner and is decided by the owning tenant's admin or
// a SuperAdmin. Promotion to platform public (tenant→public) requires an
// explicit human SuperAdmin — tenant Admins, CanAccessAllTenants-only
// operators, and API-key principals are all denied. public→tenant and any
// target tenant other than the current owner are owner changes: they are
// rejected with 400 and go through TransferKnowledgeBaseOwner instead.
// Rejected transitions leave the stored owner/visibility intact. Only
// owner/visibility metadata changes: the KB ID and data-scope tenant_id are
// preserved and no child rows, vector indexes, or background jobs are
// touched.
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
	oldScope := newKBScopeOf(kb)
	// Moving between tenant, restricted and published keeps the owner: its
	// Tenant Admin decides, as does a SuperAdmin. A different target tenant
	// is an owner change, which this call does not do.
	if kb.OwnerTenantID != 0 && isTenantOwnedVisibility(kb.Visibility) && isTenantOwnedVisibility(visibility) &&
		kb.Visibility != visibility {
		if targetTenantID != 0 && targetTenantID != kb.OwnerTenantID {
			return nil, errOwnerChangeUseTransfer()
		}
		if !access.CanManageKBSharing(ctx, caller, kb.OwnerTenantID) {
			if caller.TenantID == 0 {
				return nil, apperrors.NewUnauthorizedError("Unauthorized")
			}
			return nil, apperrors.NewForbiddenError("only the owning organization's admin may change this knowledge base's scope")
		}
		if kb.Visibility == visibility {
			return kb, nil
		}
		kb.Visibility = visibility
		return s.persistKBScopeChange(ctx, kb, oldScope, 0)
	}
	if !access.IsExplicitHumanSuperAdmin(ctx, caller) {
		if caller.TenantID == 0 {
			return nil, apperrors.NewUnauthorizedError("Unauthorized")
		}
		return nil, apperrors.NewForbiddenError("only an explicit platform SuperAdmin may change knowledge base scope")
	}
	if kb.Visibility == visibility && kb.OwnerTenantID != 0 &&
		(kb.Visibility == types.KBVisibilityPublished || kb.Visibility == types.KBVisibilityRestricted) {
		if targetTenantID == 0 || targetTenantID == kb.OwnerTenantID {
			return kb, nil
		}
		return nil, errOwnerChangeUseTransfer()
	}
	if kb.Visibility == types.KBVisibilityPublished || visibility == types.KBVisibilityPublished ||
		kb.Visibility == types.KBVisibilityRestricted || visibility == types.KBVisibilityRestricted {
		return nil, apperrors.NewBadRequestError("move the knowledge base to organization scope first")
	}
	switch {
	case kb.Visibility == types.KBVisibilityPublic && visibility == types.KBVisibilityPublic:
		if targetTenantID != 0 {
			return nil, apperrors.NewBadRequestError("target organization must be omitted when keeping public scope")
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
		return nil, errOwnerChangeUseTransfer()
	case kb.Visibility == types.KBVisibilityTenant && visibility == types.KBVisibilityPublic:
		if targetTenantID != 0 {
			return nil, apperrors.NewBadRequestError("target organization must be omitted when publishing to public scope")
		}
		kb.OwnerTenantID = 0
		kb.Visibility = types.KBVisibilityPublic
	case kb.Visibility == types.KBVisibilityPublic && visibility == types.KBVisibilityTenant:
		// An owner change: the transfer endpoint revokes shares, audits
		// kb.owner_transferred and validates the destination.
		return nil, errOwnerChangeUseTransfer()
	default:
		return nil, apperrors.NewBadRequestError("invalid visibility")
	}
	return s.persistKBScopeChange(ctx, kb, oldScope, targetTenantID)
}

// SetKnowledgeBaseMemberDefault sets what active members of the owner
// tenant may do on a 'tenant' KB (view | contribute). Only the owner
// tenant's Tenant Admin or an explicit human SuperAdmin decides; API keys
// never do. The change is audited as a scope change and never touches
// invitations or grants.
func (s *knowledgeBaseService) SetKnowledgeBaseMemberDefault(
	ctx context.Context, id string, memberDefault types.KBMemberDefault,
) (*types.KnowledgeBase, error) {
	if id == "" {
		return nil, apperrors.NewBadRequestError("knowledge base ID cannot be empty")
	}
	if !memberDefault.IsValid() {
		return nil, apperrors.NewBadRequestError("invalid member_default")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return nil, apperrors.NewNotFoundError("knowledge base not found")
	}
	if kb.OwnerTenantID == 0 {
		return nil, apperrors.NewBadRequestError("member_default applies to organization-owned knowledge bases only")
	}
	caller := types.CallerFromContext(ctx)
	if !access.CanManageKBSharing(ctx, caller, kb.OwnerTenantID) {
		if caller.TenantID == 0 {
			return nil, apperrors.NewUnauthorizedError("Unauthorized")
		}
		return nil, apperrors.NewForbiddenError("only the owning organization's admin may change member access")
	}
	if kb.MemberDefault == memberDefault {
		return kb, nil
	}
	oldScope := newKBScopeOf(kb)
	kb.MemberDefault = memberDefault
	return s.persistKBScopeChange(ctx, kb, oldScope, 0)
}

// isTenantOwnedVisibility reports whether v keeps a nonzero owner tenant.
func isTenantOwnedVisibility(v types.KBVisibility) bool {
	return v == types.KBVisibilityTenant || v == types.KBVisibilityPublished || v == types.KBVisibilityRestricted
}

// persistKBScopeChange validates and stores an owner/visibility change and
// records the scope-change audit row. When the owner tenant changes, the
// previous owner's invitations and tenant grants are revoked in the same
// transaction (revoked_reason=owner_changed): sharing is the owner's
// decision and the new owner re-shares if it wants to.
func (s *knowledgeBaseService) persistKBScopeChange(
	ctx context.Context, kb *types.KnowledgeBase, oldScope oldKBScope, targetTenantID uint64,
) (*types.KnowledgeBase, error) {
	kb, _, err := s.persistKBScope(ctx, types.AuditActionKBScopeChanged, kb, oldScope, targetTenantID)
	return kb, err
}

// persistKBScope is persistKBScopeChange with an explicit audit action; it
// also returns the shares revoked by an owner change.
func (s *knowledgeBaseService) persistKBScope(
	ctx context.Context, action types.AuditAction, kb *types.KnowledgeBase, oldScope oldKBScope,
	targetTenantID uint64,
) (*types.KnowledgeBase, types.KBShareRevocation, error) {
	kb.EnsureDefaults()
	// Validate owner/visibility before persistence.
	if err := kb.ValidateOwnership(); err != nil {
		return nil, types.KBShareRevocation{}, apperrors.NewBadRequestError(err.Error())
	}
	kb.UpdatedAt = time.Now()
	revokeReason := ""
	if oldScope.Known && oldScope.OwnerTenantID != kb.OwnerTenantID {
		revokeReason = types.KBInviteRevokedReasonOwnerChanged
	}
	revoked, err := s.repo.UpdateKnowledgeBaseScope(ctx, kb, revokeReason)
	if err != nil {
		s.recordKBScopeAudit(ctx, action, kb.ID, oldScope, newKBScopeOf(kb), targetTenantID,
			types.AuditOutcomeFailed, nil)
		return nil, types.KBShareRevocation{}, err
	}
	var extra map[string]any
	if revokeReason != "" {
		extra = map[string]any{
			"revoked_invitations": revoked.Invitations,
			"revoked_grants":      revoked.Grants,
		}
	}
	s.recordKBScopeAudit(ctx, action, kb.ID, oldScope, newKBScopeOf(kb), targetTenantID,
		types.AuditOutcomeSuccess, extra)
	return kb, revoked, nil
}

// errOwnerChangeUseTransfer rejects owner changes requested through the
// visibility endpoint.
func errOwnerChangeUseTransfer() error {
	return apperrors.NewBadRequestError(
		"changing the owner organization is not a visibility change; use POST /knowledge-bases/:id/transfer-owner")
}

// TransferKnowledgeBaseOwner moves a KB to targetTenantID. Explicit human
// SuperAdmin only (API keys and Tenant Admins are denied). The target must
// exist, be active and be a real workspace (not a configuration profile)
// and differ from the current owner. The scope never widens: 'tenant'
// stays 'tenant' and 'restricted' stays 'restricted' (every ACL is revoked,
// so only the new owner's Admins and SuperAdmins read it until the new
// owner shares it); 'published' is withdrawn to 'tenant'; platform 'public'
// becomes 'tenant' of the target. The data-scope
// tenant_id and member_default are unchanged. persistKBScope revokes every
// invitation (revoked_reason=owner_changed) and tenant grant in the same
// transaction and audits kb.owner_transferred.
func (s *knowledgeBaseService) TransferKnowledgeBaseOwner(
	ctx context.Context, id string, targetTenantID uint64,
) (*types.KBOwnerTransferResult, error) {
	caller := types.CallerFromContext(ctx)
	if !access.IsExplicitHumanSuperAdmin(ctx, caller) {
		if caller.TenantID == 0 {
			return nil, apperrors.NewUnauthorizedError("Unauthorized")
		}
		return nil, apperrors.NewForbiddenError("only an explicit platform SuperAdmin may transfer knowledge base ownership")
	}
	if id == "" {
		return nil, apperrors.NewBadRequestError("knowledge base ID cannot be empty")
	}
	if targetTenantID == 0 {
		return nil, apperrors.NewBadRequestError("target_tenant_id is required")
	}
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			return nil, apperrors.NewNotFoundError("knowledge base not found")
		}
		return nil, err
	}
	if kb == nil {
		return nil, apperrors.NewNotFoundError("knowledge base not found")
	}
	if kb.IsTemporary {
		return nil, apperrors.NewBadRequestError("temporary knowledge bases cannot change owner")
	}
	switch {
	case kb.Visibility == types.KBVisibilityPublic && kb.OwnerTenantID == 0:
	case kb.OwnerTenantID != 0 && isTenantOwnedVisibility(kb.Visibility):
	default:
		return nil, apperrors.NewBadRequestError("knowledge base scope does not allow an owner transfer")
	}
	if targetTenantID == kb.OwnerTenantID {
		return nil, apperrors.NewBadRequestError("the knowledge base already belongs to this organization")
	}
	// The destination must exist before the row is touched so a typo
	// leaves the previous owner/visibility intact.
	if s.tenantRepo == nil {
		return nil, apperrors.NewInternalServerError("organization service unavailable")
	}
	target, terr := s.tenantRepo.GetTenantByID(ctx, targetTenantID)
	if terr != nil {
		if errors.Is(terr, repository.ErrTenantNotFound) {
			return nil, apperrors.NewNotFoundError("target organization not found")
		}
		return nil, terr
	}
	if target == nil {
		return nil, apperrors.NewNotFoundError("target organization not found")
	}
	if target.IsHiddenTenant() {
		return nil, apperrors.NewBadRequestError("the target is not an organization")
	}
	if !isActiveTenantStatus(target.Status) {
		return nil, apperrors.NewBadRequestError("target organization is not active")
	}
	oldScope := newKBScopeOf(kb)
	result := &types.KBOwnerTransferResult{
		PreviousOwnerTenantID:  kb.OwnerTenantID,
		PreviousVisibility:     kb.Visibility,
		WithdrawnFromPublished: kb.Visibility == types.KBVisibilityPublished,
	}
	kb.OwnerTenantID = targetTenantID
	kb.Visibility = transferredVisibility(kb.Visibility)
	updated, revoked, err := s.persistKBScope(ctx, types.AuditActionKBOwnerTransferred, kb, oldScope, targetTenantID)
	if err != nil {
		return nil, err
	}
	result.KnowledgeBase = updated
	result.RevokedInvitations = revoked.Invitations
	result.RevokedGrants = revoked.Grants
	return result, nil
}

// transferredVisibility is the scope a KB keeps after an owner transfer:
// restricted stays restricted, everything else (tenant, published, platform
// public) lands in the new owner's tenant scope.
func transferredVisibility(from types.KBVisibility) types.KBVisibility {
	if from == types.KBVisibilityRestricted {
		return types.KBVisibilityRestricted
	}
	return types.KBVisibilityTenant
}

// isActiveTenantStatus treats an empty status (the column default before
// it was backfilled) and "active" as active.
func isActiveTenantStatus(status string) bool {
	return status == "" || strings.EqualFold(status, "active")
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

// ListKnowledgeBases returns the user-facing catalog: every KB the caller
// may VIEW under access.DecideKB, loaded in one repository statement
// (ListReadableKnowledgeBases) with an access_source per row. SuperAdmins
// see every KB; owner-tenant Admins every KB of their tenant; members the
// tenant/published KBs of their tenant plus restricted ones they hold a
// live invite or tenant grant on; humans the platform public catalog;
// machine principals only their own tenant's KBs. Published KBs of other
// tenants stay out of the default list unless the caller or the caller's
// tenant subscribed — a subscription filters the readable set, it never
// grants. Temporary/deleted rows are excluded in SQL.
//
// Count/status enrichment runs under each KB's stored data-scope
// tenant_id (never the requesting tenant), so converted and invited rows
// report their own partition's numbers.
func (s *knowledgeBaseService) ListKnowledgeBases(ctx context.Context) ([]*types.KnowledgeBase, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	caller := types.CallerFromContext(ctx)
	reader := access.KBReaderFor(ctx, caller)

	kbs, err := s.repo.ListReadableKnowledgeBases(ctx, reader)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		return nil, err
	}
	if !reader.SuperAdmin {
		kbs = s.keepSubscribedForeignPublished(ctx, caller, kbs)
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

// keepSubscribedForeignPublished drops published KBs of other tenants that
// neither the caller nor the caller's tenant subscribed to, and stamps the
// subscription flags on the ones kept. Every other row passes unchanged.
func (s *knowledgeBaseService) keepSubscribedForeignPublished(
	ctx context.Context, caller types.Caller, kbs []*types.KnowledgeBase,
) []*types.KnowledgeBase {
	var mine, tenant map[string]bool
	out := kbs[:0]
	for _, kb := range kbs {
		if kb == nil {
			continue
		}
		if kb.AccessSource == string(access.SourcePublished) {
			if mine == nil {
				mine, tenant = s.readerSubscriptions(ctx)
			}
			if !mine[kb.ID] && !tenant[kb.ID] {
				continue
			}
			kb.SubscribedByMe, kb.SubscribedByTenant = mine[kb.ID], tenant[kb.ID]
		}
		out = append(out, kb)
	}
	return out
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
	s.cleanupTasksForKnowledgeBase(kbCleanupCtx, id, nil)
	cancelKBCleanup()

	// Step 1b: Revoke all access grants for this KB so grantee tenants lose access.
	if s.kbAccessGrantService != nil {
		if delErr := s.kbAccessGrantService.DeleteAllForKB(ctx, id); delErr != nil {
			logger.Warnf(ctx, "Failed to delete KB access grants for knowledge base %s: %v", id, delErr)
		}
	}
	if s.subscriptionRepo != nil {
		if delErr := s.subscriptionRepo.DeleteByKnowledgeBaseID(ctx, id); delErr != nil {
			logger.Warnf(ctx, "Failed to delete KB subscriptions for knowledge base %s: %v", id, delErr)
		}
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
		s.cleanupTasksForKnowledgeBase(cleanupCtx, kbID, knowledgeIDs)
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
	s.cleanupTasksForKnowledgeBase(ctx, kbID, knowledgeIDs)

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
) {
	canceller, ok := s.taskInspector.(interfaces.KnowledgeBaseTaskCanceller)
	if !ok || kbID == "" {
		return
	}
	if _, _, err := canceller.CancelTasksForKnowledgeBase(ctx, kbID, knowledgeIDs); err != nil {
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
	s.cancelTasksForKnowledgeBase(ctx, kbID, knowledgeIDs)
}

// applyProfileDefaultModels fills a new knowledge base's embedding and
// summary models from the configuration profile its tenant inherits from.
// A knowledge base created from the UI carries no models, and ingestion
// cannot embed without one; the SuperAdmin picks them once for everyone.
// Lookup failures leave the fields empty, as before.
func (s *knowledgeBaseService) applyProfileDefaultModels(ctx context.Context, kb *types.KnowledgeBase) {
	if s.tenantRepo == nil || (kb.EmbeddingModelID != "" && kb.SummaryModelID != "") {
		return
	}
	tenant, err := s.tenantRepo.GetTenantByID(ctx, kb.TenantID)
	if err != nil || tenant == nil || tenant.ConfigProfileID == nil {
		return
	}
	profile, err := s.tenantRepo.GetTenantByID(ctx, *tenant.ConfigProfileID)
	if err != nil || profile == nil {
		logger.Warnf(ctx, "[kb.create] cannot load configuration profile %d: %v", *tenant.ConfigProfileID, err)
		return
	}
	if kb.EmbeddingModelID == "" {
		kb.EmbeddingModelID = profile.DefaultEmbeddingModelID
	}
	if kb.SummaryModelID == "" {
		kb.SummaryModelID = profile.DefaultSummaryModelID
	}
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
	case strings.HasPrefix(locale, "vi"):
		return " (bản sao)"
	default:
		return " Copy"
	}
}

func duplicateKBDefaultName(locale string) string {
	locale = strings.ToLower(locale)
	switch {
	case strings.HasPrefix(locale, "vi"):
		return "Kho tri thức"
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
