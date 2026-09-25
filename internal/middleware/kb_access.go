package middleware

import (
	"context"
	stderrors "errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// KBAccess aliases the shared policy result. This adapter resolves route
// parameters, applies the RBAC rollout switch, and scopes downstream services
// to the resource tenant. Gin's tenant key retains the authenticated caller.
type KBAccess = access.KBAccess

// KBAccessContextKey is the gin.Context key under which a successful
// KB access resolution is stored.
const KBAccessContextKey = "rbac.kb_access"

// KBAccessFromContext returns the KBAccess stashed by the guard, if
// any. Handlers that don't care can just rely on the rewritten
// c.Request.Context() for tenant scoping.
func KBAccessFromContext(c *gin.Context) (*KBAccess, bool) {
	v, ok := c.Get(KBAccessContextKey)
	if !ok {
		return nil, false
	}
	a, ok := v.(*KBAccess)
	return a, ok && a != nil
}

// KBLookup is the minimum surface the guard needs from the
// knowledge-base service: a single method that turns an ID into a
// KnowledgeBase pointer (or repo.ErrKnowledgeBaseNotFound). Defining
// it as a tiny dedicated interface keeps the guard testable without
// forcing test stubs to satisfy the full KnowledgeBaseService surface.
type KBLookup interface {
	GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error)
}

// KnowledgeLookup mirrors KBLookup but for resolving a knowledge id
// (document id) back to its parent KB. Used by the chunk routes whose
// URL param is a knowledge_id, not a kb_id.
type KnowledgeLookup interface {
	GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error)
}

// ChunkLookup mirrors KBLookup for resolving a chunk id back to its
// owning knowledge document, which then resolves to the parent KB.
// Used by the /chunks/by-id/:id routes that address chunks directly.
type ChunkLookup interface {
	GetChunkByIDOnly(ctx context.Context, id string) (*types.Chunk, error)
}

// KBIDResolver tells the guard how to find the kb_id for a given
// request. Built-in resolvers below cover the param shapes we use:
// :id, :kb_id, :kbId, :knowledge_id (-> parent KB).
//
// On error, resolvers MUST return either a 4xx apperror (bad request /
// not found) or a generic Go error for transient/internal failures;
// the guard maps the latter to 503.
type KBIDResolver func(c *gin.Context) (string, error)

// KBIDFromParam returns a resolver that reads a fixed gin param.
func KBIDFromParam(param string) KBIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		return v, nil
	}
}

// KBIDFromKnowledgeIDParam reads `:knowledge_id` from the URL, looks
// up the knowledge document, and returns its KB id. Used by the chunk
// routes that address a chunk via /chunks/:knowledge_id.
//
// A genuine "not found" maps to 404; transient errors (DB hiccup,
// service unavailable) are surfaced as a plain Go error so the guard
// can return 503 instead of pretending the resource doesn't exist
// (a 404 here would also short-circuit any retry / monitoring).
func KBIDFromKnowledgeIDParam(param string, kgService KnowledgeLookup) KBIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		k, err := kgService.GetKnowledgeByIDOnly(c.Request.Context(), v)
		if err != nil {
			if isResourceNotFound(err) {
				return "", apperrors.NewNotFoundError("Knowledge not found")
			}
			return "", err
		}
		if k == nil {
			return "", apperrors.NewNotFoundError("Knowledge not found")
		}
		return k.KnowledgeBaseID, nil
	}
}

// KBIDFromChunkIDParam walks chunk_id -> knowledge_id -> kb_id.
// Used by /chunks/by-id/:id routes that address a chunk directly. The
// chunk's KnowledgeBaseID is denormalised on the row, so a single
// lookup is enough — no need to chain through GetKnowledgeByIDOnly.
//
// Not-found / transient split mirrors KBIDFromKnowledgeIDParam.
func KBIDFromChunkIDParam(param string, chunkService ChunkLookup) KBIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		ch, err := chunkService.GetChunkByIDOnly(c.Request.Context(), v)
		if err != nil {
			if isResourceNotFound(err) {
				return "", apperrors.NewNotFoundError("Chunk not found")
			}
			return "", err
		}
		if ch == nil {
			return "", apperrors.NewNotFoundError("Chunk not found")
		}
		if ch.KnowledgeBaseID == "" {
			// Should-never-happen on a fresh schema; on legacy rows the
			// chunk effectively isn't resolvable to a KB so the client
			// gets the same 404 they'd get for a missing chunk rather
			// than a 500 that pollutes alerting.
			logger.Warnf(c.Request.Context(),
				"[kb_access] chunk %s has empty knowledge_base_id; treating as not-found", v)
			return "", apperrors.NewNotFoundError("Chunk not found")
		}
		return ch.KnowledgeBaseID, nil
	}
}

// isResourceNotFound recognises the various "not found" sentinels we
// might see from the underlying services. Keeps the resolvers above
// from forcing every service to standardise on a single error type
// before this refactor is useful.
func isResourceNotFound(err error) bool {
	// ErrChunkNotFound is defined in the repository layer and aliased by the
	// service; match the canonical repo sentinel so this predicate depends
	// only on the repository package (KB / Knowledge / Chunk are all here).
	return stderrors.Is(err, apprepo.ErrKnowledgeBaseNotFound) ||
		stderrors.Is(err, apprepo.ErrKnowledgeNotFound) ||
		stderrors.Is(err, apprepo.ErrChunkNotFound) ||
		stderrors.Is(err, ErrResourceNotFound)
}

// RequireKBAccess returns a gin.HandlerFunc that resolves KB access
// (own tenant / granted to the caller's tenant / public), enforces the
// minimum required permission, and on success stores the result under
// KBAccessContextKey AND rewrites c.Request.Context() to carry the
// effective tenant ID. Handlers downstream just read tenant from
// context as before.
//
// On failure the guard aborts with the appropriate HTTP status (400 /
// 401 / 404 / 403 / 503). Behaviour matches what each handler's
// effectiveCtxForKB helper used to do; the guard is what consolidates
// the repetition so a fix in the resolution order propagates to every
// gated route at once.
//
// Required permission semantics:
//   - KBPermissionViewer -> read-only routes (grants may satisfy these)
//   - KBPermissionEditor -> mutating routes (own KB only; grants are
//     read-only today)
//   - KBPermissionAdmin  -> grant-management routes (only the owning
//     tenant should pass)
//
// The EnableRBAC rollout switch does not apply here. It relaxes the role
// checks inside a workspace, while ResolveKB always grants a KB's own
// workspace, so a denial from it means another workspace's KB without a
// grant. Handlers behind this guard load KBs, chunks and wiki pages by ID
// without a tenant filter, so letting a denial through would lift
// workspace isolation for the whole rollout window.
func RequireKBAccess(
	resolveKBID KBIDResolver,
	requiredPermission types.KBPermission,
	kbService KBLookup,
	kbGrantService interfaces.KBAccessGrantService,
	cfg *config.Config,
) gin.HandlerFunc {
	return RequireKBAccessWithInvite(resolveKBID, requiredPermission, kbService, kbGrantService, nil, cfg)
}

// RequireKBAccessWithInvite extends RequireKBAccess with recipient-bound
// invitation access: when the caller's tenant has no grant, a live invite
// accepted by THIS user confers read-only access (Viewer only). A nil
// invites lookup disables the fallback (fail-closed); write guards pass
// nil because invites never satisfy Editor/Admin.
func RequireKBAccessWithInvite(
	resolveKBID KBIDResolver,
	requiredPermission types.KBPermission,
	kbService KBLookup,
	kbGrantService interfaces.KBAccessGrantService,
	invites access.KBInviteLookup,
	cfg *config.Config,
) gin.HandlerFunc {
	warnOnNilConfig(cfg)
	return runKBAccessGuard(resolveKBID, cfg, func(ctx context.Context, c *gin.Context, kbID string) (*KBAccess, error) {
		return resolveKBAccess(ctx, c, kbID, requiredPermission, kbService, kbGrantService, invites)
	})
}

// RequireKBDownload gates original-file download routes (single-file
// download and batch ZIP). It is the only guard that lets a
// platform-public Viewer fetch original bytes: same-owner-tenant callers
// keep the tenant download policy, invitation viewers stay denied, and
// preview/read routes stay behind Viewer access.
func RequireKBDownload(
	resolveKBID KBIDResolver,
	kbService KBLookup,
	_ interfaces.KBAccessGrantService,
	cfg *config.Config,
) gin.HandlerFunc {
	warnOnNilConfig(cfg)
	return runKBAccessGuard(resolveKBID, cfg, func(ctx context.Context, c *gin.Context, kbID string) (*KBAccess, error) {
		return resolveKBDownloadAccess(ctx, c, kbID, kbService)
	})
}

// RequireKBManage gates content-mutation routes by KB owner: the owning
// tenant keeps its existing role policy (floors stay on the route), while
// platform-owned rows admit only an explicit human SuperAdmin. It never
// treats a public Viewer as an editor.
func RequireKBManage(
	resolveKBID KBIDResolver,
	kbService KBLookup,
	_ interfaces.KBAccessGrantService,
	cfg *config.Config,
) gin.HandlerFunc {
	warnOnNilConfig(cfg)
	return runKBAccessGuard(resolveKBID, cfg, func(ctx context.Context, c *gin.Context, kbID string) (*KBAccess, error) {
		return resolveKBManageAccess(ctx, c, kbID, kbService)
	})
}

// runKBAccessGuard resolves one KB, maps resolution errors to HTTP status,
// and on success stashes the grant plus rewrites the request to carry the
// effective (data-scope) tenant ID.
func runKBAccessGuard(
	resolveKBID KBIDResolver,
	cfg *config.Config,
	resolve func(ctx context.Context, c *gin.Context, kbID string) (*KBAccess, error),
) gin.HandlerFunc {
	warnOnNilConfig(cfg)
	return func(c *gin.Context) {
		kbID, err := resolveKBID(c)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}

		ctx := c.Request.Context()
		if err := types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbID); err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}

		grant, err := resolve(ctx, c, kbID)
		switch {
		case stderrors.Is(err, access.ErrUnauthorized):
			_ = c.Error(apperrors.NewUnauthorizedError("Unauthorized"))
			c.Abort()
			return
		case stderrors.Is(err, access.ErrNotFound):
			_ = c.Error(apperrors.NewNotFoundError("knowledge base not found"))
			c.Abort()
			return
		case stderrors.Is(err, access.ErrForbidden):
			_ = c.Error(apperrors.NewForbiddenError("Permission denied to access this knowledge base"))
			c.Abort()
			return
		case err != nil:
			logger.ErrorWithFields(ctx, err, nil)
			// Transient/internal -> 503 so monitoring catches the
			// underlying failure rather than a misleading 500.
			_ = c.Error(apperrors.NewServiceUnavailableError("cannot verify KB access right now"))
			c.Abort()
			return
		}

		// Stash the resolution and rewrite the request to carry the
		// effective tenant id. Handlers reading tenant from context now
		// see the source-tenant for shared KBs (so retrieval queries
		// hit the right embedding store) without having to know.
		c.Set(KBAccessContextKey, grant)
		newCtx := grant.Context(ctx)
		c.Request = c.Request.WithContext(newCtx)
		c.Next()
	}
}

// KBAccessRequest captures caller identity before a guard scopes the request
// context to a shared resource. The stored grant also supports nested guards.
func KBAccessRequest(c *gin.Context) access.KBRequest {
	ctx := c.Request.Context()
	caller := types.CallerFromContext(ctx)
	if _, captured := ctx.Value(types.CallerContextKey).(types.Caller); !captured {
		// Compatibility for direct handler invocations that only seed Gin.
		if tenantID, ok := c.Get(types.TenantIDContextKey.String()); ok {
			caller.TenantID, _ = tenantID.(uint64)
		} else if grant, found := KBAccessFromContext(c); found {
			caller = grant.Caller
		}
		if userID, ok := c.Get(types.UserIDContextKey.String()); ok {
			caller.UserID, _ = userID.(string)
		}
	}
	return access.KBRequest{Caller: caller}
}

func resolveKBAccess(
	ctx context.Context,
	c *gin.Context,
	kbID string,
	requiredPermission types.KBPermission,
	kbService KBLookup,
	kbGrantService interfaces.KBAccessGrantService,
	invites access.KBInviteLookup,
) (*KBAccess, error) {
	request := KBAccessRequest(c)
	request.Caller = request.Caller.Normalize()
	if request.Caller.TenantID == 0 && !access.IsExplicitHumanSuperAdmin(ctx, request.Caller) &&
		!access.IsAuthenticatedHuman(ctx, request.Caller) {
		return nil, access.ErrUnauthorized
	}
	kb, err := loadKBForGuard(ctx, kbID, kbService)
	if err != nil {
		return nil, err
	}
	if invites == nil {
		return access.ResolveKB(ctx, request, kb, requiredPermission, kbGrantService)
	}
	return access.ResolveKBWithInvite(ctx, request, kb, requiredPermission, kbGrantService, invites)
}

// resolveKBDownloadAccess resolves the dedicated original-download grant.
// Invitation lookups are never consulted: invite readers keep read-only
// access and stay denied here.
func resolveKBDownloadAccess(
	ctx context.Context,
	c *gin.Context,
	kbID string,
	kbService KBLookup,
) (*KBAccess, error) {
	request := KBAccessRequest(c)
	request.Caller = request.Caller.Normalize()
	if request.Caller.TenantID == 0 && !access.IsExplicitHumanSuperAdmin(ctx, request.Caller) &&
		!access.IsAuthenticatedHuman(ctx, request.Caller) {
		return nil, access.ErrUnauthorized
	}
	kb, err := loadKBForGuard(ctx, kbID, kbService)
	if err != nil {
		return nil, err
	}
	return access.ResolveKBForDownload(ctx, request, kb)
}

// resolveKBManageAccess resolves the owner-aware mutation grant: owning
// tenant via the existing policy, platform-owned rows via explicit human
// SuperAdmin only.
func resolveKBManageAccess(
	ctx context.Context,
	c *gin.Context,
	kbID string,
	kbService KBLookup,
) (*KBAccess, error) {
	request := KBAccessRequest(c)
	request.Caller = request.Caller.Normalize()
	// Unlike public reads, tenantless mutations are reserved for an
	// explicit human SuperAdmin. Reject before lookup to avoid a KB
	// existence oracle for ordinary tenantless users.
	if request.Caller.TenantID == 0 && !access.IsExplicitHumanSuperAdmin(ctx, request.Caller) {
		return nil, access.ErrUnauthorized
	}
	kb, err := loadKBForGuard(ctx, kbID, kbService)
	if err != nil {
		return nil, err
	}
	return access.ResolveKBForManage(ctx, request, kb)
}

// loadKBForGuard loads one KB for guard resolution. A genuine "not found"
// maps to the access sentinel; transient errors propagate for a 503.
func loadKBForGuard(ctx context.Context, kbID string, kbService KBLookup) (*types.KnowledgeBase, error) {
	kb, err := kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		if stderrors.Is(err, apprepo.ErrKnowledgeBaseNotFound) {
			return nil, access.ErrNotFound
		}
		return nil, err
	}
	return kb, nil
}
