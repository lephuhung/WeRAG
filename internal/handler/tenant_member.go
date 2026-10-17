package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// TenantMemberHandler exposes /tenants/:id/members CRUD. The route layer
// enforces RBAC (Member for list, strict TenantAdmin for mutations) —
// see router.RegisterTenantRoutes. The owner role is retired: only
// admin/member may be assigned.
//
// Tenant scoping: the auth middleware resolves the caller's role against
// the *active* tenant (JWT / X-Tenant-ID switch / API-key). The URL :id
// is independent and MUST be cross-checked: a user who is Owner of
// tenant A could otherwise POST /tenants/B/members and have the role
// gate happily accept their tenant-A role for an operation that targets
// tenant B. That cross-check now lives in
// middleware.RequirePathTenantMatch (mounted at the /tenants/:id route
// group); by the time a request reaches one of the methods below, :id
// is guaranteed to either match the active tenant or carry a
// cross-tenant superuser bypass.
type TenantMemberHandler struct {
	memberService interfaces.TenantMemberService
	userService   interfaces.UserService
}

// NewTenantMemberHandler wires the dependencies. PR 1 already provides
// both services through the dig container; we just consume them. The
// previous *config.Config argument was removed once
// middleware.RequirePathTenantMatch took over the cross-tenant
// superuser carve-out.
func NewTenantMemberHandler(
	memberService interfaces.TenantMemberService,
	userService interfaces.UserService,
) *TenantMemberHandler {
	return &TenantMemberHandler{
		memberService: memberService,
		userService:   userService,
	}
}

// addMemberRequest is the JSON body for POST /tenants/:id/members.
// Email is the user-facing invite identifier; the handler resolves it to a
// User via UserService.GetUserByEmail. PR 3 does not implement
// email-based invitations for users that don't exist yet — the invitee
// must already have an account. Sending an email invite is tracked as a
// PR 4 candidate.
type addMemberRequest struct {
	Email string           `json:"email" binding:"required,email"`
	Role  types.TenantRole `json:"role" binding:"required"`
}

// updateMemberRoleRequest is the JSON body for PUT /tenants/:id/members/:user_id.
type updateMemberRoleRequest struct {
	Role types.TenantRole `json:"role" binding:"required"`
}

// parseTenantIDFromPath reads :id from the gin route and validates it as
// a tenant ID. Returning (0, false) means we already wrote the error to
// the gin context and the caller should `return` immediately.
func parseTenantIDFromPath(c *gin.Context) (uint64, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	if raw == "" {
		c.Error(apperrors.NewValidationError("workspace id is required"))
		return 0, false
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v == 0 {
		c.Error(apperrors.NewValidationError("workspace id must be a positive integer"))
		return 0, false
	}
	return v, true
}

// ListMembers godoc
// @Summary      Liệt kê thành viên đơn vị
// @Description  Trả về theo trang các thành viên active của đơn vị hiện tại (kèm vai trò, email, ảnh đại diện); hỗ trợ q để lọc theo email/tên người dùng
// @Tags         Thành viên đơn vị
// @Produce      json
// @Param        id         path   string  true   "Tenant workspace ID"
// @Param        q          query  string  false  "Lọc gần đúng theo email/tên người dùng"
// @Param        page       query  int     false  "Số trang (bắt đầu từ 1)"  default(1)
// @Param        page_size  query  int     false  "Số mục mỗi trang (tối đa 100)"  default(20)
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /tenants/{id}/members [get]
func (h *TenantMemberHandler) ListMembers(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}

	q := strings.TrimSpace(c.Query("q"))
	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}

	members, total, err := h.memberService.ListMembersPage(ctx, tenantID, q, page, pageSize)
	if err != nil {
		logger.Errorf(ctx, "ListMembersPage failed: tenant=%d err=%v", tenantID, err)
		c.Error(apperrors.NewInternalServerError("failed to list members").WithDetails(err.Error()))
		return
	}

	// Hydrate user-facing fields in one batched query. Before this we
	// did N+1 GetUserByID calls; tenants with hundreds of members
	// pressed the user repo hard for no good reason. Failure is
	// best-effort — a transient batch error degrades to "no email /
	// username on this page" rather than dropping rows, so dangling
	// memberships can still be cleaned up by the Owner.
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	usersByID := map[string]*types.User{}
	if u, err := h.userService.GetUsersByIDs(ctx, ids); err == nil {
		usersByID = u
	} else {
		logger.Warnf(ctx, "ListMembers batch user lookup failed: tenant=%d err=%v", tenantID, err)
	}

	resp := make([]types.TenantMemberResponse, 0, len(members))
	for _, m := range members {
		row := types.TenantMemberResponse{
			UserID:    m.UserID,
			Role:      m.Role,
			Status:    m.Status,
			InvitedBy: m.InvitedBy,
			JoinedAt:  m.JoinedAt,
		}
		if u, ok := usersByID[m.UserID]; ok && u != nil {
			row.Email = u.Email
			row.Username = u.Username
			row.Avatar = u.Avatar
		}
		resp = append(resp, row)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"members":   resp,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// AddMember godoc
// @Summary      Thêm trực tiếp thành viên đơn vị (đường thêm thẳng)
// @Description
//
//	Owner thêm thẳng người dùng theo email làm thành viên active của đơn vị hiện tại.
//
//	Đây là [đường thêm thẳng]: người được thêm xuất hiện trong đơn vị mà không có cơ hội xác nhận —
//	giữ lại nó cho ba trường hợp không cần qua xác nhận lời mời:
//	  1. script tự động / vận hành nền tảng / di chuyển dữ liệu;
//	  2. siêu quản trị liên đơn vị (CanAccessAllTenants) điều phối hàng loạt;
//	  3. đồng bộ thành viên một chiều từ nguồn định danh khi tích hợp IdP bên ngoài.
//
//	Mọi tương tác "mời đồng nghiệp tham gia" từ UI nên chuyển sang
//	POST /tenants/:id/invitations; đường đó tạo hàng pending trước, để người được mời
//	chủ động chấp nhận tại /me/invitations rồi mới ghi hàng tenant_members (tiếp theo PR #1303).
//	Đường này và đường invitations cùng tồn tại, không thay thế nhau.
//
// @Tags         Thành viên đơn vị
// @Accept       json
// @Produce      json
// @Param        id        path  string                 true  "Tenant workspace ID"
// @Param        request   body  addMemberRequest       true  "Yêu cầu mời"
// @Success      201  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /tenants/{id}/members [post]
func (h *TenantMemberHandler) AddMember(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}

	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request body").WithDetails(err.Error()))
		return
	}
	// Defence in depth — service also re-validates, but rejecting early
	// gives the client a better error message than the generic service
	// sentinel-mapped 400.
	if req.Role == types.TenantRoleOwner {
		c.Error(apperrors.NewValidationError("the owner role is retired; assign admin instead"))
		return
	}
	if !req.Role.IsValid() {
		c.Error(apperrors.NewValidationError("role must be one of admin/member"))
		return
	}

	user, err := h.userService.GetUserByEmail(ctx, strings.TrimSpace(req.Email))
	if err != nil {
		// ErrUserNotFound is the deliberate "not registered yet" signal;
		// mapping it to 404 lets the UI render "ask them to sign up first"
		// instead of a generic failure.
		if errors.Is(err, apprepo.ErrUserNotFound) {
			c.Error(apperrors.NewNotFoundError(
				"user with this email is not registered; ask them to sign up first"))
			return
		}
		logger.Errorf(ctx, "GetUserByEmail failed: email=%s err=%v",
			secutils.SanitizeForLog(req.Email), err)
		c.Error(apperrors.NewInternalServerError("failed to look up user").WithDetails(err.Error()))
		return
	}

	// Attribute the invite to a human caller only. The X-API-Key auth
	// path attaches a synthetic "system-<tenantID>" user (see
	// types.IsSyntheticUserID); recording that as invited_by would
	// permanently break join-with-users views and any future "who
	// invited whom" UX. Leaving invited_by NULL is the correct fallback
	// — matches the same treatment KB.CreatorID gets in PR 2.
	caller, _ := types.UserIDFromContext(ctx)
	var invitedBy *string
	if caller != "" && !types.IsSyntheticUserID(caller) {
		invitedBy = &caller
	}

	// Add the member and write the 201 / mapped-error response through the
	// shared helper (also used by the invitation auto-accept path).
	addMemberAndRespond(c, ctx, h.memberService, user, tenantID, req.Role, invitedBy)
}

func writeAddMemberError(
	c *gin.Context,
	ctx context.Context,
	user *types.User,
	tenantID uint64,
	err error,
) {
	switch {
	case errors.Is(err, service.ErrInvalidTenantRole):
		c.Error(apperrors.NewValidationError(err.Error()))
	case errors.Is(err, service.ErrAPIKeyCannotAssignAdmin):
		c.Error(apperrors.NewForbiddenError(err.Error()))
	case errors.Is(err, service.ErrMembershipAlreadyExists):
		// 409 reads better than 400 here: the request was syntactically
		// fine, the conflict is semantic ("already a member").
		c.Error(apperrors.NewConflictError(err.Error()))
	default:
		logger.Errorf(ctx, "AddMember failed: user=%s tenant=%d err=%v",
			user.ID, tenantID, err)
		c.Error(apperrors.NewInternalServerError("failed to add member").WithDetails(err.Error()))
	}
}

func writeAddMemberSuccess(c *gin.Context, user *types.User, member *types.TenantMember) {
	// Project the freshly added row through the same response shape the
	// list endpoint uses, so the UI can swap "Add Member" UX into the
	// table without an extra round-trip.
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": types.TenantMemberResponse{
			UserID:    member.UserID,
			Email:     user.Email,
			Username:  user.Username,
			Avatar:    user.Avatar,
			Role:      member.Role,
			Status:    member.Status,
			InvitedBy: member.InvitedBy,
			JoinedAt:  member.JoinedAt,
		},
	})
}

// addMemberAndRespond calls TenantMemberService.AddMember and writes the
// HTTP response: 201 with a TenantMemberResponse on success, or the service
// sentinel mapped to its HTTP status (400 / 403 / 409 / 500) on error. It
// always writes exactly one response, so the caller MUST return right after.
// Shared by TenantMemberHandler.AddMember and the auto-accept branch of
// TenantInvitationHandler.CreateInvitation so the mapping never drifts.
func addMemberAndRespond(
	c *gin.Context,
	ctx context.Context,
	memberService interfaces.TenantMemberService,
	user *types.User,
	tenantID uint64,
	role types.TenantRole,
	invitedBy *string,
) {
	member, err := memberService.AddMember(ctx, user.ID, tenantID, role, invitedBy)
	if err != nil {
		writeAddMemberError(c, ctx, user, tenantID, err)
		return
	}
	writeAddMemberSuccess(c, user, member)
}

// UpdateMemberRole godoc
// @Summary      Đổi vai trò thành viên đơn vị
// @Description  Owner đổi vai trò của một thành viên trong đơn vị hiện tại; không được hạ cấp Owner cuối cùng
// @Tags         Thành viên đơn vị
// @Accept       json
// @Produce      json
// @Param        id       path  string                  true  "Tenant workspace ID"
// @Param        user_id  path  string                  true  "ID người dùng"
// @Param        request  body  updateMemberRoleRequest true  "Vai trò đích"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /tenants/{id}/members/{user_id} [put]
func (h *TenantMemberHandler) UpdateMemberRole(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}
	userID := strings.TrimSpace(c.Param("user_id"))
	if userID == "" {
		c.Error(apperrors.NewValidationError("user_id is required"))
		return
	}

	var req updateMemberRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request body").WithDetails(err.Error()))
		return
	}
	if req.Role == types.TenantRoleOwner {
		c.Error(apperrors.NewValidationError("the owner role is retired; assign admin instead"))
		return
	}
	if !req.Role.IsValid() {
		c.Error(apperrors.NewValidationError("role must be one of admin/member"))
		return
	}

	if err := h.memberService.UpdateRole(ctx, userID, tenantID, req.Role); err != nil {
		switch {
		case errors.Is(err, service.ErrMembershipNotFound):
			c.Error(apperrors.NewNotFoundError("membership not found"))
		case errors.Is(err, service.ErrLastAdmin):
			c.Error(apperrors.NewConflictError(err.Error()))
		case errors.Is(err, service.ErrOwnerRoleRetired):
			c.Error(apperrors.NewValidationError(err.Error()))
		case errors.Is(err, service.ErrInvalidTenantRole):
			c.Error(apperrors.NewValidationError(err.Error()))
		case errors.Is(err, service.ErrAPIKeyCannotAssignAdmin):
			c.Error(apperrors.NewForbiddenError(err.Error()))
		default:
			logger.Errorf(ctx, "UpdateRole failed: user=%s tenant=%d err=%v",
				userID, tenantID, err)
			c.Error(apperrors.NewInternalServerError("failed to update member role").WithDetails(err.Error()))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RemoveMember godoc
// @Summary      Gỡ thành viên đơn vị
// @Description  Owner gỡ một thành viên khỏi đơn vị hiện tại (xoá mềm hàng tenant_members); không được gỡ Owner cuối cùng
// @Tags         Thành viên đơn vị
// @Produce      json
// @Param        id       path  string  true  "Tenant workspace ID"
// @Param        user_id  path  string  true  "ID người dùng"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /tenants/{id}/members/{user_id} [delete]
func (h *TenantMemberHandler) RemoveMember(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}
	userID := strings.TrimSpace(c.Param("user_id"))
	if userID == "" {
		c.Error(apperrors.NewValidationError("user_id is required"))
		return
	}

	if err := h.memberService.RemoveMember(ctx, userID, tenantID); err != nil {
		switch {
		case errors.Is(err, service.ErrMembershipNotFound):
			c.Error(apperrors.NewNotFoundError("membership not found"))
		case errors.Is(err, service.ErrLastAdmin):
			c.Error(apperrors.NewConflictError(err.Error()))
		default:
			logger.Errorf(ctx, "RemoveMember failed: user=%s tenant=%d err=%v",
				userID, tenantID, err)
			c.Error(apperrors.NewInternalServerError("failed to remove member").WithDetails(err.Error()))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// LeaveTenant godoc
// @Summary      Rời đơn vị hiện tại
// @Description  Người gọi chủ động rời đơn vị hiện tại. Tương đương gọi RemoveMember với user_id của chính mình,
//
//	nhưng không cần quyền Admin — người không phải Admin cũng tự rời được. Admin cuối cùng vẫn không được rời
//	(phải nâng thành viên khác lên Admin trước), do tầng service chặn bằng ErrLastAdmin.
//
// @Tags         Thành viên đơn vị
// @Produce      json
// @Param        id  path  string  true  "Tenant workspace ID"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /tenants/{id}/leave [post]
func (h *TenantMemberHandler) LeaveTenant(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}
	caller, ok := types.UserIDFromContext(ctx)
	if !ok || caller == "" {
		c.Error(apperrors.NewUnauthorizedError("caller user id missing from context"))
		return
	}

	if err := h.memberService.RemoveMember(ctx, caller, tenantID); err != nil {
		switch {
		case errors.Is(err, service.ErrMembershipNotFound):
			c.Error(apperrors.NewNotFoundError("you are not a member of this workspace"))
		case errors.Is(err, service.ErrLastAdmin):
			c.Error(apperrors.NewConflictError(err.Error()))
		default:
			logger.Errorf(ctx, "LeaveTenant failed: user=%s tenant=%d err=%v",
				caller, tenantID, err)
			c.Error(apperrors.NewInternalServerError("failed to leave workspace").WithDetails(err.Error()))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
