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

// Revoke handles DELETE /tenants/:id/access-grants/:grant_id — withdraw an
// approved grant on a KB the caller's tenant owns.
func (h *KBAccessGrantHandler) Revoke(c *gin.Context) {
	ctx := c.Request.Context()
	grantID := c.Param("grant_id")
	if grantID == "" {
		c.Error(apperrors.NewBadRequestError("grant id is required"))
		return
	}
	caller := middleware.KBAccessRequest(c).Caller
	grant, err := h.grantService.Revoke(ctx, caller, grantID)
	if err != nil {
		c.Error(grantHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": grant})
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
	case stderrors.Is(err, service.ErrGrantNotFound):
		return apperrors.NewNotFoundError("kb access grant not found")
	case stderrors.Is(err, service.ErrGrantExists):
		return apperrors.NewConflictError("a live access grant already exists for this knowledge base")
	case stderrors.Is(err, service.ErrGrantNotPending):
		return apperrors.NewConflictError("access grant is no longer pending")
	case stderrors.Is(err, service.ErrGrantSelfTarget):
		return apperrors.NewBadRequestError("cannot request access to a knowledge base your tenant owns")
	default:
		logger.ErrorWithFields(context.Background(), err, nil)
		return apperrors.NewInternalServerError("internal error")
	}
}
