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

// KBInvitationHandler exposes the recipient-bound KB read-invitation
// lifecycle: the owning tenant's Admin issues/revokes invites for a
// specific user in another tenant; the named recipient accepts.
// Tenant join invitations remain a separate flow (tenant_invitation.go).
type KBInvitationHandler struct {
	inviteService interfaces.KBInvitationService
}

// NewKBInvitationHandler creates the handler.
func NewKBInvitationHandler(inviteService interfaces.KBInvitationService) *KBInvitationHandler {
	return &KBInvitationHandler{inviteService: inviteService}
}

// Issue handles POST /knowledge-bases/:id/invites — owning-tenant Admin
// invites a specific user in another tenant to read this KB.
func (h *KBInvitationHandler) Issue(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	if kbID == "" {
		c.Error(apperrors.NewBadRequestError("knowledge base id is required"))
		return
	}
	var req types.CreateKBInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid request body: " + err.Error()))
		return
	}
	caller := middleware.KBAccessRequest(c).Caller
	invite, err := h.inviteService.Issue(ctx, caller, kbID, &req)
	if err != nil {
		c.Error(kbInviteHTTPError(err))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": invite})
}

// ListByKB handles GET /knowledge-bases/:id/invites — owning-tenant Admin
// lists invites on the KB.
func (h *KBInvitationHandler) ListByKB(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	if kbID == "" {
		c.Error(apperrors.NewBadRequestError("knowledge base id is required"))
		return
	}
	caller := middleware.KBAccessRequest(c).Caller
	rows, err := h.inviteService.ListByKB(ctx, caller, kbID)
	if err != nil {
		c.Error(kbInviteHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// Revoke handles DELETE /knowledge-bases/:id/invites/:invite_id —
// owning-tenant Admin withdraws a pending or accepted invite. Revocation takes
// effect immediately for future reads.
func (h *KBInvitationHandler) Revoke(c *gin.Context) {
	ctx := c.Request.Context()
	inviteID := c.Param("invite_id")
	if inviteID == "" {
		c.Error(apperrors.NewBadRequestError("invite id is required"))
		return
	}
	caller := middleware.KBAccessRequest(c).Caller
	if err := h.inviteService.Revoke(ctx, caller, inviteID); err != nil {
		c.Error(kbInviteHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Accept handles POST /kb-invites/accept — the authenticated recipient
// redeems their invite. The token is single-use; wrong-user, expired,
// revoked and replayed tokens all fail closed.
func (h *KBInvitationHandler) Accept(c *gin.Context) {
	ctx := c.Request.Context()
	var req types.AcceptKBInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Token == "" {
		c.Error(apperrors.NewBadRequestError("invitation token is required"))
		return
	}
	caller := middleware.KBAccessRequest(c).Caller
	invite, err := h.inviteService.Accept(ctx, caller, req.Token)
	if err != nil {
		c.Error(kbInviteHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": invite})
}

// AcceptByID handles POST /kb-invites/:id/accept — the in-app accept
// flow. The server verifies the caller is the bound recipient and holds
// an active membership in the intended tenant; no token needed.
func (h *KBInvitationHandler) AcceptByID(c *gin.Context) {
	ctx := c.Request.Context()
	inviteID := c.Param("id")
	if inviteID == "" {
		c.Error(apperrors.NewBadRequestError("invite id is required"))
		return
	}
	caller := middleware.KBAccessRequest(c).Caller
	invite, err := h.inviteService.AcceptByID(ctx, caller, inviteID)
	if err != nil {
		c.Error(kbInviteHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": invite})
}

// ListMine handles GET /kb-invites — invites addressed to the caller.
func (h *KBInvitationHandler) ListMine(c *gin.Context) {
	ctx := c.Request.Context()
	caller := middleware.KBAccessRequest(c).Caller
	rows, err := h.inviteService.ListMine(ctx, caller)
	if err != nil {
		c.Error(kbInviteHTTPError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func kbInviteHTTPError(err error) error {
	switch {
	case stderrors.Is(err, service.ErrKBInviteNotFound):
		return apperrors.NewNotFoundError("kb invitation not found")
	case stderrors.Is(err, service.ErrKBInviteExists):
		return apperrors.NewConflictError("a pending invitation already exists for this user on this knowledge base")
	case stderrors.Is(err, service.ErrKBInviteNotPending):
		return apperrors.NewConflictError("kb invitation is no longer pending")
	case stderrors.Is(err, service.ErrKBInviteExpired):
		return &apperrors.AppError{
			Code:     apperrors.ErrNotFound,
			Message:  "kb invitation has expired",
			HTTPCode: http.StatusGone,
		}
	case stderrors.Is(err, service.ErrKBInviteForbidden):
		return apperrors.NewForbiddenError("only the invited recipient can accept this invitation")
	case stderrors.Is(err, service.ErrKBInviteTokenInvalid):
		// Non-enumerating: unknown/expired/revoked/replayed/wrong-user
		// all collapse here so tokens cannot be probed.
		return &apperrors.AppError{
			Code:     apperrors.ErrNotFound,
			Message:  "kb invitation token is invalid or has been revoked",
			HTTPCode: http.StatusGone,
		}
	case stderrors.Is(err, service.ErrKBInviteSelfTarget):
		return apperrors.NewBadRequestError("cannot invite a member of the owning tenant via cross-tenant invite")
	case stderrors.Is(err, service.ErrKBInviteNotOwnerAdmin):
		return apperrors.NewForbiddenError("only a tenant admin of the owning tenant can manage kb invitations")
	case stderrors.Is(err, service.ErrKBInviteRecipientNotMember):
		return apperrors.NewBadRequestError("recipient must be an active member of the recipient workspace")
	default:
		logger.ErrorWithFields(context.Background(), err, nil)
		return apperrors.NewInternalServerError("internal error")
	}
}
