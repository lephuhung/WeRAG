package handler

import (
	stderrors "errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// resolvedKBAccess reuses only a grant for this caller and this resource. A
// Viewer grant cannot satisfy an Editor check after the tenant context switches.
func resolvedKBAccess(c *gin.Context, kbID string, required types.KBPermission) (*access.KBAccess, bool) {
	grant, ok := middleware.KBAccessFromContext(c)
	return grant, ok && grant.KnowledgeBase != nil && grant.KnowledgeBase.ID == kbID &&
		grant.Caller == middleware.KBAccessRequest(c).Caller &&
		access.HasKBGrant(grant.WithGrant(c.Request.Context()), kbID, grant.EffectiveTenantID, required)
}

// resolveHandlerKBAccess is also used by body/query-based endpoints that cannot
// resolve a single KB in route middleware. It always enforces access, preserving
// the handler checks even when the middleware's RBAC rollout flag is disabled.
func resolveHandlerKBAccess(c *gin.Context, kbID string, kbService middleware.KBLookup,
	grants interfaces.KBAccessGrantService,
) (*access.KBAccess, error) {
	return resolveHandlerKBAccessFor(c, kbID, kbService, grants, types.KBPermissionViewer)
}

func resolveHandlerKBAccessFor(c *gin.Context, kbID string, kbService middleware.KBLookup,
	grants interfaces.KBAccessGrantService, required types.KBPermission,
) (*access.KBAccess, error) {
	ctx := c.Request.Context()
	request := middleware.KBAccessRequest(c)
	request.Caller = request.Caller.Normalize()
	// Tenantless callers proceed only as explicit human SuperAdmins or
	// real human users; per-KB guards then admit them solely on
	// platform-public rows (tenant-owned, invited, and malformed rows
	// stay denied downstream). Anonymous and machine principals stay
	// unauthorized here.
	if request.Caller.TenantID == 0 && !access.IsExplicitHumanSuperAdmin(ctx, request.Caller) &&
		!access.IsAuthenticatedHuman(ctx, request.Caller) {
		return nil, apperrors.NewUnauthorizedError("Unauthorized")
	}
	if kbID == "" {
		return nil, apperrors.NewBadRequestError("Knowledge base ID cannot be empty")
	}
	if err := requireTenantAPIKeyKnowledgeBase(ctx, kbID); err != nil {
		return nil, err
	}
	if grant, ok := resolvedKBAccess(c, kbID, required); ok {
		return grant, nil
	}
	kb, err := kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			return nil, apperrors.NewNotFoundError("knowledge base not found")
		}
		logger.ErrorWithFields(ctx, err, nil)
		return nil, apperrors.NewInternalServerError(err.Error())
	}
	grant, err := access.ResolveKB(ctx, request, kb, required, grants)
	if err == nil {
		// Preserve authorization for body-based routes without changing the
		// execution tenant until the handler performs its resource operation.
		c.Request = c.Request.WithContext(grant.WithGrant(ctx))
	}
	return grant, kbAccessHTTPError(err)
}

// resolveHandlerKBDownloadAccessFor resolves the dedicated original-download
// grant for body/query-based endpoints. Invitation reads are never consulted:
// invite Viewers stay denied here by construction.
func resolveHandlerKBDownloadAccessFor(c *gin.Context, kbID string, kbService middleware.KBLookup) (*access.KBAccess, error) {
	ctx := c.Request.Context()
	request := middleware.KBAccessRequest(c)
	request.Caller = request.Caller.Normalize()
	if request.Caller.TenantID == 0 && !access.IsExplicitHumanSuperAdmin(ctx, request.Caller) &&
		!access.IsAuthenticatedHuman(ctx, request.Caller) {
		return nil, apperrors.NewUnauthorizedError("Unauthorized")
	}
	if kbID == "" {
		return nil, apperrors.NewBadRequestError("Knowledge base ID cannot be empty")
	}
	if err := requireTenantAPIKeyKnowledgeBase(ctx, kbID); err != nil {
		return nil, err
	}
	// No stashed-grant shortcut: a Viewer stash may come from a read-only
	// invitation, which must never confer original download. The guard on the
	// download routes already applied this same decision.
	kb, err := kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			return nil, apperrors.NewNotFoundError("knowledge base not found")
		}
		logger.ErrorWithFields(ctx, err, nil)
		return nil, apperrors.NewInternalServerError(err.Error())
	}
	grant, err := access.ResolveKBForDownload(ctx, request, kb)
	if err == nil {
		c.Request = c.Request.WithContext(grant.WithGrant(ctx))
	}
	return grant, kbAccessHTTPError(err)
}

// resolveHandlerKBManageAccessFor resolves the owner-aware mutation grant
// for body/query-based endpoints: owning tenant via the existing policy,
// platform-owned rows via explicit human SuperAdmin only.
func resolveHandlerKBManageAccessFor(c *gin.Context, kbID string, kbService middleware.KBLookup,
	_ interfaces.KBAccessGrantService,
) (*access.KBAccess, error) {
	ctx := c.Request.Context()
	request := middleware.KBAccessRequest(c)
	request.Caller = request.Caller.Normalize()
	// Tenantless mutation is reserved for explicit human SuperAdmins;
	// deny before lookup to avoid exposing KB existence to other callers.
	if request.Caller.TenantID == 0 && !access.IsExplicitHumanSuperAdmin(ctx, request.Caller) {
		return nil, apperrors.NewUnauthorizedError("Unauthorized")
	}
	if kbID == "" {
		return nil, apperrors.NewBadRequestError("Knowledge base ID cannot be empty")
	}
	if err := requireTenantAPIKeyKnowledgeBase(ctx, kbID); err != nil {
		return nil, err
	}
	if grant, ok := resolvedKBAccess(c, kbID, types.KBPermissionEditor); ok {
		return grant, nil
	}
	kb, err := kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			return nil, apperrors.NewNotFoundError("knowledge base not found")
		}
		logger.ErrorWithFields(ctx, err, nil)
		return nil, apperrors.NewInternalServerError(err.Error())
	}
	grant, err := access.ResolveKBForManage(ctx, request, kb)
	if err == nil {
		c.Request = c.Request.WithContext(grant.WithGrant(ctx))
	}
	return grant, kbAccessHTTPError(err)
}

func kbAccessHTTPError(err error) error {
	switch {
	case stderrors.Is(err, access.ErrUnauthorized):
		return apperrors.NewUnauthorizedError("Unauthorized")
	case stderrors.Is(err, access.ErrNotFound):
		return apperrors.NewNotFoundError("knowledge base not found")
	case stderrors.Is(err, access.ErrForbidden):
		return apperrors.NewForbiddenError("Permission denied to access this knowledge base")
	default:
		return err
	}
}
