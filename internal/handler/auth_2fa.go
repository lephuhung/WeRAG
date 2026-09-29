package handler

import (
	stderrors "errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
)

// Two-factor authentication (TOTP, 6-digit authenticator codes) endpoints.
// All four routes are authenticated; identity comes from the token, never
// from the request. Machine-readable failure reasons ride in the error
// `details` field so the frontend can branch without string matching.

const (
	DetailTwoFactorInvalidCode    = "invalid_two_factor_code"
	DetailTwoFactorNotSetup       = "two_factor_not_setup"
	DetailTwoFactorAlreadyEnabled = "two_factor_already_enabled"
)

// mapTwoFactorError converts service-layer 2FA sentinels to HTTP app errors.
func mapTwoFactorError(err error) *errors.AppError {
	switch {
	case stderrors.Is(err, service.ErrTwoFactorInvalidCode):
		return errors.NewBadRequestError("Invalid or expired code").WithDetails(DetailTwoFactorInvalidCode)
	case stderrors.Is(err, service.ErrTwoFactorNotSetup):
		return errors.NewBadRequestError("Two-factor setup has not started").WithDetails(DetailTwoFactorNotSetup)
	case stderrors.Is(err, service.ErrTwoFactorAlreadyEnabled):
		return errors.NewBadRequestError("Two-factor authentication is already enabled").WithDetails(DetailTwoFactorAlreadyEnabled)
	default:
		return errors.NewBadRequestError("Two-factor operation failed").WithDetails(err.Error())
	}
}

// TwoFactorStatus godoc
// @Summary      2FA 状态
// @Description  返回当前账户是否已启用 TOTP 两步验证
// @Tags         认证
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "success / enabled"
// @Security     Bearer
// @Router       /auth/2fa/status [get]
func (h *AuthHandler) TwoFactorStatus(c *gin.Context) {
	ctx := c.Request.Context()
	user, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("Failed to get user information").WithDetails(err.Error()))
		return
	}
	enabled, err := h.userService.TwoFactorStatus(ctx, user.ID)
	if err != nil {
		logger.Errorf(ctx, "Failed to read 2FA status: %v", err)
		c.Error(mapTwoFactorError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "enabled": enabled})
}

// TwoFactorSetup godoc
// @Summary      开始 2FA 绑定
// @Description  生成待确认的 TOTP 密钥，返回 Base32 密钥与 otpauth:// URI（前端渲染二维码）
// @Tags         认证
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "success / secret / otpauth_url"
// @Security     Bearer
// @Router       /auth/2fa/setup [post]
func (h *AuthHandler) TwoFactorSetup(c *gin.Context) {
	ctx := c.Request.Context()
	user, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("Failed to get user information").WithDetails(err.Error()))
		return
	}
	secret, otpauth, err := h.userService.BeginTwoFactorSetup(ctx, user.ID)
	if err != nil {
		logger.Errorf(ctx, "Failed to begin 2FA setup: %v", err)
		c.Error(mapTwoFactorError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"secret":      secret,
		"otpauth_url": otpauth,
	})
}

// TwoFactorEnable godoc
// @Summary      启用 2FA
// @Description  用验证器当前 6 位验证码确认绑定；成功后生成一次性恢复代码（仅本次返回）
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request  body      object{code=string}    true  "6 位验证码"
// @Success      200      {object}  map[string]interface{} "success / recovery_codes"
// @Failure      400      {object}  errors.AppError        "验证码无效"
// @Security     Bearer
// @Router       /auth/2fa/enable [post]
func (h *AuthHandler) TwoFactorEnable(c *gin.Context) {
	ctx := c.Request.Context()
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("Invalid 2FA enable request").WithDetails(err.Error()))
		return
	}
	user, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("Failed to get user information").WithDetails(err.Error()))
		return
	}
	codes, err := h.userService.EnableTwoFactor(ctx, user.ID, req.Code)
	if err != nil {
		logger.Errorf(ctx, "Failed to enable 2FA: %v", err)
		c.Error(mapTwoFactorError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"recovery_codes": codes,
	})
}

// TwoFactorDisable godoc
// @Summary      关闭 2FA
// @Description  用当前 TOTP 验证码（或未使用的恢复代码）关闭两步验证并清除绑定
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request  body      object{code=string}    true  "6 位验证码或恢复代码"
// @Success      200      {object}  map[string]interface{} "success"
// @Failure      400      {object}  errors.AppError        "验证码无效"
// @Security     Bearer
// @Router       /auth/2fa/disable [post]
func (h *AuthHandler) TwoFactorDisable(c *gin.Context) {
	ctx := c.Request.Context()
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewValidationError("Invalid 2FA disable request").WithDetails(err.Error()))
		return
	}
	user, err := h.userService.GetCurrentUser(ctx)
	if err != nil {
		c.Error(errors.NewUnauthorizedError("Failed to get user information").WithDetails(err.Error()))
		return
	}
	if err := h.userService.DisableTwoFactor(ctx, user.ID, req.Code); err != nil {
		logger.Errorf(ctx, "Failed to disable 2FA: %v", err)
		c.Error(mapTwoFactorError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Two-factor authentication disabled"})
}
