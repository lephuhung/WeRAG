package handler

import (
	"context"
	stderrors "errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// KBAccessGrantHandler exposes the tenant-to-tenant KB access grant
// lifecycle: grantee-side requests and owner-side review/revoke.
type KBAccessGrantHandler struct {
	grantService interfaces.KBAccessGrantService
}

// NewKBAccessGrantHandler creates the handler.
func NewKBAccessGrantHandler(grantService interfaces.KBAccessGrantService) *KBAccessGrantHandler {
	return &KBAccessGrantHandler{grantService: grantService}
}

// RequestAccess handles POST /knowledge-bases/:id/access-requests — the
// caller's tenant asks the KB's owning tenant for read access.
func (h *KBAccessGrantHandler) RequestAccess(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	if kbID == "" {
		c.Error(apperrors.NewBadRequestError("knowledge base id is required"))
		return
	}
	var req types.RequestKBAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid request body: " + err.Error()))
		return
	}
	caller := middleware.KBAccessRequest(c).Caller
	grant, err := h.grantService.RequestAccess(ctx, caller, kbID, &req)
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": grant})
}

// ListIncoming handles GET /tenants/:id/access-grants — pending and
// terminal grants on KBs the caller's tenant owns. Optional ?status=
// filter (comma separated).
func (h *KBAccessGrantHandler) ListIncoming(c *gin.Context) {
	ctx := c.Request.Context()
	caller := middleware.KBAccessRequest(c).Caller
	rows, err := h.grantService.ListIncoming(ctx, caller, parseGrantStatuses(c.Query("status")))
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// ListOutgoing handles GET /access-grants — grants the caller's tenant
// has requested on foreign KBs. Optional ?status= filter.
func (h *KBAccessGrantHandler) ListOutgoing(c *gin.Context) {
	ctx := c.Request.Context()
	caller := middleware.KBAccessRequest(c).Caller
	rows, err := h.grantService.ListOutgoing(ctx, caller, parseGrantStatuses(c.Query("status")))
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// Review handles PUT /tenants/:id/access-grants/:grant_id — approve or
// reject a pending request on a KB the caller's tenant owns.
func (h *KBAccessGrantHandler) Review(c *gin.Context) {
	ctx := c.Request.Context()
	grantID := c.Param("grant_id")
	if grantID == "" {
		c.Error(apperrors.NewBadRequestError("grant id is required"))
		return
	}
	var req types.ReviewKBAccessGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid request body: " + err.Error()))
		return
	}
	caller := middleware.KBAccessRequest(c).Caller
	grant, err := h.grantService.Review(ctx, caller, grantID, &req)
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": grant})
}

// GrantTenantAccess handles POST /knowledge-bases/:id/grants — the owning
// tenant's admin (or a SuperAdmin) shares the KB read-only with every member
// of another tenant.
func (h *KBAccessGrantHandler) GrantTenantAccess(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	var req types.GrantKBAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid request body: " + err.Error()))
		return
	}
	grant, err := h.grantService.GrantTenantAccess(ctx, middleware.KBAccessRequest(c).Caller, kbID, &req)
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": grant})
}

// ListKBGrants handles GET /knowledge-bases/:id/grants.
func (h *KBAccessGrantHandler) ListKBGrants(c *gin.Context) {
	rows, err := h.grantService.ListByKB(c.Request.Context(), middleware.KBAccessRequest(c).Caller, c.Param("id"))
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// RevokeGrant handles DELETE /knowledge-bases/:id/grants/:grant_id. The
// grant must belong to :id; the service re-checks that the caller manages
// the KB before revoking.
func (h *KBAccessGrantHandler) RevokeGrant(c *gin.Context) {
	ctx := c.Request.Context()
	caller := middleware.KBAccessRequest(c).Caller
	grantID := c.Param("grant_id")
	rows, err := h.grantService.ListByKB(ctx, caller, c.Param("id"))
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	found := false
	for _, row := range rows {
		if row.ID == grantID {
			found = true
			break
		}
	}
	if !found {
		c.Error(apperrors.NewNotFoundError("kb access grant not found"))
		return
	}
	h.revoke(c, caller, grantID)
}

// RevokeIncomingGrant handles DELETE /tenants/:id/access-grants/:grant_id
// for the owning tenant's admin.
func (h *KBAccessGrantHandler) RevokeIncomingGrant(c *gin.Context) {
	h.revoke(c, middleware.KBAccessRequest(c).Caller, c.Param("grant_id"))
}

func (h *KBAccessGrantHandler) revoke(c *gin.Context, caller types.Caller, grantID string) {
	grant, err := h.grantService.Revoke(c.Request.Context(), caller, grantID)
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": grant})
}

// Retired request/approve mutations. Each returns 410 Gone: a tenant can no
// longer ask for access; the owning tenant grants it directly
// (POST /knowledge-bases/:id/grants). The service layer independently
// rejects these operations with ErrGrantDisabled.
func (h *KBAccessGrantHandler) RequestAccessDisabled(c *gin.Context) {
	c.Error(&apperrors.AppError{
		Code:     apperrors.ErrNotFound,
		Message:  "kb access requests are retired; ask the owning workspace admin to grant access",
		HTTPCode: http.StatusGone,
	})
}

// ReviewDisabled handles PUT /tenants/:id/access-grants/:grant_id.
func (h *KBAccessGrantHandler) ReviewDisabled(c *gin.Context) {
	c.Error(&apperrors.AppError{
		Code:     apperrors.ErrNotFound,
		Message:  "kb access request review is retired; the owning workspace grants access directly",
		HTTPCode: http.StatusGone,
	})
}

// parseGrantStatuses parses a comma-separated ?status= query value into
// the typed status list; unknown values are dropped.
func parseGrantStatuses(raw string) []types.GrantStatus {
	if raw == "" {
		return nil
	}
	var out []types.GrantStatus
	for _, part := range splitComma(raw) {
		switch types.GrantStatus(part) {
		case types.GrantStatusPending, types.GrantStatusApproved,
			types.GrantStatusRejected, types.GrantStatusRevoked,
			types.GrantStatusExpired:
			out = append(out, types.GrantStatus(part))
		}
	}
	return out
}

func splitComma(raw string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == ',' {
			if part := raw[start:i]; part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}

func grantHTTPError(err error) error {
	switch {
	case stderrors.Is(err, service.ErrGrantDisabled):
		return &apperrors.AppError{
			Code:     apperrors.ErrNotFound,
			Message:  "kb access requests are retired; ask the owning workspace admin to grant access",
			HTTPCode: http.StatusGone,
		}
	case stderrors.Is(err, service.ErrGrantNotFound):
		return apperrors.NewNotFoundError("kb access grant not found")
	case stderrors.Is(err, service.ErrGrantExists):
		return apperrors.NewConflictError("a live access grant already exists for this knowledge base")
	case stderrors.Is(err, service.ErrGrantNotPending):
		return apperrors.NewConflictError("access grant is no longer pending")
	case stderrors.Is(err, service.ErrGrantSelfTarget):
		return apperrors.NewBadRequestError("cannot grant a knowledge base to the tenant that owns it")
	case stderrors.Is(err, service.ErrGrantNotManager):
		return apperrors.NewForbiddenError(err.Error())
	case stderrors.Is(err, service.ErrGrantPlatformKB):
		return apperrors.NewBadRequestError(err.Error())
	case stderrors.Is(err, service.ErrGrantTenantNotFound):
		return apperrors.NewNotFoundError(err.Error())
	default:
		logger.ErrorWithFields(context.Background(), err, nil)
		return apperrors.NewInternalServerError("internal error")
	}
}
