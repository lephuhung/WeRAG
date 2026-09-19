package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// TenantOrgHandler exposes tenant-org CRUD, member management and
// org-bound invite links. Tenant orgs are user groups inside one
// tenant; they scope 'org'-visibility knowledge bases.
type TenantOrgHandler struct {
	service    interfaces.TenantOrgService
	configInfo *config.Config
}

// NewTenantOrgHandler creates the handler. configInfo supplies
// FrontendBaseURL for invite-link URL composition.
func NewTenantOrgHandler(service interfaces.TenantOrgService, configInfo *config.Config) *TenantOrgHandler {
	return &TenantOrgHandler{service: service, configInfo: configInfo}
}

func (h *TenantOrgHandler) respondError(c *gin.Context, err error) {
	if appErr, ok := apperrors.IsAppError(err); ok {
		c.Error(appErr)
		return
	}
	c.Error(apperrors.NewInternalServerError(err.Error()))
}

func (h *TenantOrgHandler) parseOrgID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.Error(apperrors.NewBadRequestError("invalid org id"))
		return 0, false
	}
	return id, true
}

// CreateOrg godoc
// @Summary      创建组织
// @Description  在当前空间内创建一个组织（org）
// @Tags         组织管理
// @Accept       json
// @Produce      json
// @Param        request  body  types.CreateTenantOrgRequest  true  "组织信息"
// @Success      201  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs [post]
func (h *TenantOrgHandler) CreateOrg(c *gin.Context) {
	var req types.CreateTenantOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request body").WithDetails(err.Error()))
		return
	}
	org, err := h.service.CreateOrg(c.Request.Context(), &req)
	if err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": org})
}

// ListOrgs godoc
// @Summary      组织列表
// @Description  列出当前空间的全部组织
// @Tags         组织管理
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs [get]
func (h *TenantOrgHandler) ListOrgs(c *gin.Context) {
	orgs, err := h.service.ListOrgs(c.Request.Context())
	if err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": orgs})
}

// GetOrg godoc
// @Summary      组织详情
// @Tags         组织管理
// @Produce      json
// @Param        id  path  int  true  "组织 ID"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs/{id} [get]
func (h *TenantOrgHandler) GetOrg(c *gin.Context) {
	id, ok := h.parseOrgID(c)
	if !ok {
		return
	}
	org, err := h.service.GetOrg(c.Request.Context(), id)
	if err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": org})
}

// UpdateOrg godoc
// @Summary      更新组织
// @Tags         组织管理
// @Accept       json
// @Produce      json
// @Param        id       path  int                            true  "组织 ID"
// @Param        request  body  types.UpdateTenantOrgRequest  true  "更新请求"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs/{id} [put]
func (h *TenantOrgHandler) UpdateOrg(c *gin.Context) {
	id, ok := h.parseOrgID(c)
	if !ok {
		return
	}
	var req types.UpdateTenantOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request body").WithDetails(err.Error()))
		return
	}
	org, err := h.service.UpdateOrg(c.Request.Context(), id, &req)
	if err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": org})
}

// DeleteOrg godoc
// @Summary      删除组织
// @Description  软删除组织及其成员关系；org-scoped KB 的 org_id 随之下沉校验
// @Tags         组织管理
// @Produce      json
// @Param        id  path  int  true  "组织 ID"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs/{id} [delete]
func (h *TenantOrgHandler) DeleteOrg(c *gin.Context) {
	id, ok := h.parseOrgID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteOrg(c.Request.Context(), id); err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListMembers godoc
// @Summary      组织成员列表
// @Tags         组织管理
// @Produce      json
// @Param        id  path  int  true  "组织 ID"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs/{id}/members [get]
func (h *TenantOrgHandler) ListMembers(c *gin.Context) {
	id, ok := h.parseOrgID(c)
	if !ok {
		return
	}
	members, err := h.service.ListMembers(c.Request.Context(), id)
	if err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": members})
}

// AddMember godoc
// @Summary      添加组织成员
// @Description  将已是空间成员的用户加入组织
// @Tags         组织管理
// @Accept       json
// @Produce      json
// @Param        id       path  int                                 true  "组织 ID"
// @Param        request  body  types.AddTenantOrgMemberRequest    true  "成员请求"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs/{id}/members [post]
func (h *TenantOrgHandler) AddMember(c *gin.Context) {
	id, ok := h.parseOrgID(c)
	if !ok {
		return
	}
	var req types.AddTenantOrgMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request body").WithDetails(err.Error()))
		return
	}
	if err := h.service.AddMember(c.Request.Context(), id, &req); err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// UpdateMemberRole godoc
// @Summary      调整组织成员角色
// @Tags         组织管理
// @Accept       json
// @Produce      json
// @Param        id       path  int                                     true  "组织 ID"
// @Param        user_id  path  string                                  true  "用户 ID"
// @Param        request  body  types.UpdateTenantOrgMemberRequest     true  "角色请求"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs/{id}/members/{user_id} [put]
func (h *TenantOrgHandler) UpdateMemberRole(c *gin.Context) {
	id, ok := h.parseOrgID(c)
	if !ok {
		return
	}
	userID := c.Param("user_id")
	var req types.UpdateTenantOrgMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request body").WithDetails(err.Error()))
		return
	}
	if err := h.service.UpdateMemberRole(c.Request.Context(), id, userID, &req); err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RemoveMember godoc
// @Summary      移除组织成员
// @Tags         组织管理
// @Produce      json
// @Param        id       path  int     true  "组织 ID"
// @Param        user_id  path  string  true  "用户 ID"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs/{id}/members/{user_id} [delete]
func (h *TenantOrgHandler) RemoveMember(c *gin.Context) {
	id, ok := h.parseOrgID(c)
	if !ok {
		return
	}
	if err := h.service.RemoveMember(c.Request.Context(), id, c.Param("user_id")); err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// CreateInviteLink godoc
// @Summary      组织邀请链接
// @Description  生成多次使用的注册链接；接受链接的用户注册账号、加入空间并自动成为该组织成员
// @Tags         组织管理
// @Accept       json
// @Produce      json
// @Param        id       path  int                                  true  "组织 ID"
// @Param        request  body  types.CreateTenantOrgInviteRequest  true  "邀请配置"
// @Success      201  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /orgs/{id}/invite-links [post]
func (h *TenantOrgHandler) CreateInviteLink(c *gin.Context) {
	id, ok := h.parseOrgID(c)
	if !ok {
		return
	}
	var req types.CreateTenantOrgInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("invalid request body").WithDetails(err.Error()))
		return
	}
	inv, token, err := h.service.CreateInviteLink(c.Request.Context(), id, &req)
	if err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": gin.H{
			"invitation": inv,
			"url":        buildInviteRegisterURL(h.configInfo, token),
		},
	})
}
