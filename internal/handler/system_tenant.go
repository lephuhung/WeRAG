package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// tenantMemberTarget names the user a SystemAdmin assigns to a workspace:
// an existing account by user_id or email, or a new local account. Exactly
// one of the three must be given.
type tenantMemberTarget struct {
	UserID  string                        `json:"user_id"`
	Email   string                        `json:"email"`
	NewUser *types.AdminCreateUserRequest `json:"new_user"`
}

// ProvisionTenantRequest is the body of POST /system/admin/tenants.
type ProvisionTenantRequest struct {
	Name        string             `json:"name"        binding:"required"`
	Description string             `json:"description"`
	Admin       tenantMemberTarget `json:"admin"`
}

// ProvisionTenantResponse returns the new workspace, its Tenant Admin and,
// when the admin account was created here without a password, the
// generated password (shown exactly once).
type ProvisionTenantResponse struct {
	Tenant            *types.Tenant   `json:"tenant"`
	Admin             *types.UserInfo `json:"admin"`
	AdminCreated      bool            `json:"admin_created"`
	GeneratedPassword string          `json:"generated_password,omitempty"`
}

// AddSystemTenantMemberRequest is the body of
// POST /system/admin/tenants/:tenant_id/members.
type AddSystemTenantMemberRequest struct {
	tenantMemberTarget
	Role types.TenantRole `json:"role" binding:"required"`
}

// errTenantMemberTarget marks a malformed member target (400).
var errTenantMemberTarget = errors.New("give exactly one of user_id, email or new_user")

// resolveTenantMemberTarget finds or creates the user named by target. A new
// account is always tenantless: the membership written by the caller is its
// only workspace. created reports whether the account was created here.
func (h *SystemHandler) resolveTenantMemberTarget(
	ctx context.Context, target tenantMemberTarget,
) (user *types.User, created bool, generatedPassword string, err error) {
	userID := strings.TrimSpace(target.UserID)
	email := strings.TrimSpace(target.Email)
	given := 0
	for _, set := range []bool{userID != "", email != "", target.NewUser != nil} {
		if set {
			given++
		}
	}
	if given != 1 {
		return nil, false, "", errTenantMemberTarget
	}
	switch {
	case userID != "":
		user, err = h.userSvc.GetUserByID(ctx, userID)
	case email != "":
		user, err = h.userSvc.GetUserByEmail(ctx, email)
	default:
		req := *target.NewUser
		req.Username = strings.TrimSpace(req.Username)
		req.Email = strings.TrimSpace(req.Email)
		if req.Username == "" || req.Email == "" {
			return nil, false, "", apperrors.NewValidationError("new_user needs username and email")
		}
		if n := utf8.RuneCountInString(req.Username); n < 2 || n > 50 {
			return nil, false, "", apperrors.NewValidationError("Username must be 2-50 characters")
		}
		user, generatedPassword, err = h.userSvc.AdminCreateUser(ctx, &req, types.TenantProvisioningTenantless)
		switch {
		case err == nil:
			return user, true, generatedPassword, nil
		case (errors.Is(err, service.ErrUserEmailExists) || errors.Is(err, service.ErrUserUsernameExists)) && user != nil:
			// The identity already exists: assign that account.
			return user, false, "", nil
		case errors.Is(err, service.ErrPasswordPolicy):
			return nil, false, "", apperrors.NewValidationError(err.Error())
		case errors.Is(err, service.ErrUserIdentityConflict):
			return nil, false, "", apperrors.NewConflictError(err.Error())
		default:
			return nil, false, "", err
		}
	}
	if err != nil || user == nil {
		return nil, false, "", apperrors.NewNotFoundError("user not found")
	}
	return user, false, "", nil
}

// writeTenantMemberTargetError maps resolveTenantMemberTarget failures.
func writeTenantMemberTargetError(c *gin.Context, err error) {
	var appErr *apperrors.AppError
	switch {
	case errors.Is(err, errTenantMemberTarget):
		c.Error(apperrors.NewValidationError(err.Error()))
	case errors.As(err, &appErr):
		c.Error(appErr)
	default:
		logger.Errorf(c.Request.Context(), "resolve tenant member target: %v", err)
		c.Error(apperrors.NewInternalServerError("failed to resolve user"))
	}
}

// ProvisionTenant godoc
// @Summary      Create a workspace with its Tenant Admin
// @Description  SystemAdmin only. Creates the workspace and makes the named
// @Description  (or newly created) user its Tenant Admin. The calling
// @Description  SystemAdmin does not become a member.
// @Tags         System Admin
// @Accept       json
// @Produce      json
// @Param        request body ProvisionTenantRequest true "Workspace and admin"
// @Success      201  {object}  ProvisionTenantResponse
// @Router       /system/admin/tenants [post]
func (h *SystemHandler) ProvisionTenant(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())

	var req ProvisionTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.Error(apperrors.NewValidationError("name is required"))
		return
	}
	if h.memberSvc == nil {
		c.Error(apperrors.NewInternalServerError("membership service unavailable"))
		return
	}

	admin, created, generatedPassword, err := h.resolveTenantMemberTarget(ctx, req.Admin)
	if err != nil {
		writeTenantMemberTargetError(c, err)
		return
	}

	tenant := &types.Tenant{Name: name, Description: strings.TrimSpace(req.Description)}
	gb := int64(10)
	if h.systemSettingSvc != nil {
		gb = h.systemSettingSvc.GetInt(ctx,
			"tenant.default_storage_quota_gb", "WEKNORA_TENANT_DEFAULT_STORAGE_QUOTA_GB", 10)
	}
	if gb <= 0 {
		gb = 10
	}
	tenant.StorageQuota = gb * 1024 * 1024 * 1024

	createdTenant, err := h.tenantSvc.CreateTenant(ctx, tenant)
	if err != nil {
		if appErr, ok := apperrors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.Errorf(ctx, "ProvisionTenant: create tenant: %v", err)
		c.Error(apperrors.NewInternalServerError("failed to create workspace").WithDetails(err.Error()))
		return
	}

	// Without its admin membership the workspace is unreachable, so roll
	// the tenant back when the assignment fails.
	if _, err := h.memberSvc.AddMember(ctx, admin.ID, createdTenant.ID, types.TenantRoleAdmin, nil); err != nil {
		logger.Errorf(ctx, "ProvisionTenant: assign admin %s to tenant %d: %v — rolling back",
			admin.ID, createdTenant.ID, err)
		if delErr := h.tenantSvc.DeleteTenant(ctx, createdTenant.ID); delErr != nil {
			logger.Errorf(ctx, "ProvisionTenant: rollback tenant %d: %v", createdTenant.ID, delErr)
		}
		c.Error(apperrors.NewInternalServerError("failed to assign workspace admin").WithDetails(err.Error()))
		return
	}

	logger.Infof(ctx, "System admin provisioned tenant %d (%s) with admin %s",
		createdTenant.ID, secutils.SanitizeForLog(name), admin.ID)
	h.emitAdminAudit(ctx, types.AuditActionSystemTenantProvisioned, admin, map[string]any{
		"tenant_id":     createdTenant.ID,
		"tenant_name":   createdTenant.Name,
		"admin_created": created,
	})
	c.JSON(http.StatusCreated, ProvisionTenantResponse{
		Tenant:            createdTenant,
		Admin:             admin.ToUserInfo(),
		AdminCreated:      created,
		GeneratedPassword: generatedPassword,
	})
}

func parseSystemTenantID(c *gin.Context) (uint64, bool) {
	tenantID, err := strconv.ParseUint(strings.TrimSpace(c.Param("tenant_id")), 10, 64)
	if err != nil || tenantID == 0 {
		c.Error(apperrors.NewValidationError("invalid tenant_id"))
		return 0, false
	}
	return tenantID, true
}

// AddSystemTenantMember godoc
// @Summary      Add a user to any workspace
// @Description  SystemAdmin only. Adds an existing or new user to the
// @Description  workspace with the given role (admin or member).
// @Tags         System Admin
// @Accept       json
// @Produce      json
// @Param        tenant_id path string true "Workspace ID"
// @Param        request body AddSystemTenantMemberRequest true "User and role"
// @Success      201  {object}  map[string]interface{}
// @Router       /system/admin/tenants/{tenant_id}/members [post]
func (h *SystemHandler) AddSystemTenantMember(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	tenantID, ok := parseSystemTenantID(c)
	if !ok {
		return
	}
	var req AddSystemTenantMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request").WithDetails(err.Error()))
		return
	}
	if !req.Role.IsHumanTenantRole() {
		c.Error(apperrors.NewValidationError("role must be one of admin/member"))
		return
	}
	if h.memberSvc == nil {
		c.Error(apperrors.NewInternalServerError("membership service unavailable"))
		return
	}
	if tenant, err := h.tenantSvc.GetTenantByID(ctx, tenantID); err != nil || tenant == nil {
		c.Error(apperrors.NewNotFoundError("workspace not found"))
		return
	}

	user, created, generatedPassword, err := h.resolveTenantMemberTarget(ctx, req.tenantMemberTarget)
	if err != nil {
		writeTenantMemberTargetError(c, err)
		return
	}
	member, err := h.memberSvc.AddMember(ctx, user.ID, tenantID, req.Role, nil)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrMembershipAlreadyExists):
			c.Error(apperrors.NewConflictError("user is already a member of this workspace"))
		case errors.Is(err, service.ErrInvalidTenantRole), errors.Is(err, service.ErrOwnerRoleRetired):
			c.Error(apperrors.NewValidationError(err.Error()))
		default:
			logger.Errorf(ctx, "AddSystemTenantMember: user=%s tenant=%d: %v", user.ID, tenantID, err)
			c.Error(apperrors.NewInternalServerError("failed to add member").WithDetails(err.Error()))
		}
		return
	}

	h.emitAdminAudit(ctx, types.AuditActionSystemTenantMemberAdded, user, map[string]any{
		"tenant_id":    tenantID,
		"role":         member.Role,
		"user_created": created,
	})
	c.JSON(http.StatusCreated, gin.H{
		"member":             member,
		"user":               user.ToUserInfo(),
		"user_created":       created,
		"generated_password": generatedPassword,
	})
}

// RemoveSystemTenantMember godoc
// @Summary      Remove a user from any workspace
// @Tags         System Admin
// @Param        tenant_id path string true "Workspace ID"
// @Param        user_id   path string true "User ID"
// @Success      200  {object}  map[string]interface{}
// @Router       /system/admin/tenants/{tenant_id}/members/{user_id} [delete]
func (h *SystemHandler) RemoveSystemTenantMember(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	tenantID, ok := parseSystemTenantID(c)
	if !ok {
		return
	}
	userID := strings.TrimSpace(c.Param("user_id"))
	if userID == "" {
		c.Error(apperrors.NewValidationError("invalid user_id"))
		return
	}
	if h.memberSvc == nil {
		c.Error(apperrors.NewInternalServerError("membership service unavailable"))
		return
	}
	if err := h.memberSvc.RemoveMember(ctx, userID, tenantID); err != nil {
		switch {
		case errors.Is(err, service.ErrMembershipNotFound):
			c.Error(apperrors.NewNotFoundError("membership not found"))
		case errors.Is(err, service.ErrLastAdmin):
			c.Error(apperrors.NewConflictError(err.Error()))
		default:
			logger.Errorf(ctx, "RemoveSystemTenantMember: user=%s tenant=%d: %v", userID, tenantID, err)
			c.Error(apperrors.NewInternalServerError("failed to remove member").WithDetails(err.Error()))
		}
		return
	}
	h.emitAdminAudit(ctx, types.AuditActionSystemTenantMemberRemoved, &types.User{ID: userID}, map[string]any{
		"tenant_id": tenantID,
	})
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListSystemTenantMembers godoc
// @Summary      List the members of any workspace
// @Tags         System Admin
// @Param        tenant_id path string true "Workspace ID"
// @Success      200  {object}  map[string]interface{}
// @Router       /system/admin/tenants/{tenant_id}/members [get]
func (h *SystemHandler) ListSystemTenantMembers(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := parseSystemTenantID(c)
	if !ok {
		return
	}
	if h.memberSvc == nil {
		c.Error(apperrors.NewInternalServerError("membership service unavailable"))
		return
	}
	members, err := h.memberSvc.ListByTenant(ctx, tenantID)
	if err != nil {
		logger.Errorf(ctx, "ListSystemTenantMembers: tenant=%d: %v", tenantID, err)
		c.Error(apperrors.NewInternalServerError("failed to list members"))
		return
	}
	out := make([]types.TenantMemberResponse, 0, len(members))
	for _, m := range members {
		if m == nil {
			continue
		}
		row := types.TenantMemberResponse{
			UserID: m.UserID, Role: types.NormalizeTenantRole(m.Role), Status: m.Status,
			InvitedBy: m.InvitedBy, JoinedAt: m.JoinedAt,
		}
		if u, uerr := h.userSvc.GetUserByID(ctx, m.UserID); uerr == nil && u != nil {
			row.Email, row.Username, row.Avatar = u.Email, u.Username, u.Avatar
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}
