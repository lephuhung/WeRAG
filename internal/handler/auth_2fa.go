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
// @Summary      Trạng thái 2FA
// @Description  Trả về tài khoản hiện tại đã bật xác thực hai bước TOTP hay chưa
// @Tags         Xác thực
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
// @Summary      Bắt đầu liên kết 2FA
// @Description  Tạo khoá TOTP chờ xác nhận, trả về khoá Base32 và URI otpauth:// (frontend dựng mã QR)
// @Tags         Xác thực
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
// @Summary      Bật 2FA
// @Description  Xác nhận liên kết bằng mã 6 số hiện tại của ứng dụng xác thực; thành công sẽ tạo mã khôi phục dùng một lần (chỉ trả về lần này)
// @Tags         Xác thực
// @Accept       json
// @Produce      json
// @Param        request  body      object{code=string}    true  "mã xác thực 6 số"
// @Success      200      {object}  map[string]interface{} "success / recovery_codes"
// @Failure      400      {object}  errors.AppError        "mã xác thực không hợp lệ"
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
// @Summary      Tắt 2FA
// @Description  Dùng mã TOTP hiện tại (hoặc mã khôi phục chưa dùng) để tắt xác thực hai bước và xoá liên kết
// @Tags         Xác thực
// @Accept       json
// @Produce      json
// @Param        request  body      object{code=string}    true  "mã xác thực 6 số hoặc mã khôi phục"
// @Success      200      {object}  map[string]interface{} "success"
// @Failure      400      {object}  errors.AppError        "mã xác thực không hợp lệ"
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
