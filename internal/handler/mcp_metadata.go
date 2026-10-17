package handler

import (
	"context"
	stderrors "errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// GetMCPMetadata godoc
// @Summary      Đọc danh mục công cụ MCP đã lưu
// @Description  Chỉ đọc cơ sở dữ liệu, không kết nối upstream. Chưa đồng bộ thì data là null; sau khi cấu hình kết nối thay đổi thì stale là true. Danh mục OAuth được tách theo chủ thể uỷ quyền hiện tại.
// @Tags         Dịch vụ MCP
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "ID dịch vụ MCP"
// @Success      200  {object}  map[string]interface{}  "Ảnh chụp danh mục"
// @Failure      400  {object}  errors.AppError         "tham số yêu cầu không hợp lệ"
// @Failure      401  {object}  errors.AppError         "danh mục OAuth thiếu chủ thể uỷ quyền"
// @Failure      404  {object}  errors.AppError         "dịch vụ không tồn tại"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-services/{id}/metadata [get]
func (h *MCPServiceHandler) GetMCPMetadata(c *gin.Context) { h.mcpMetadata(c, false) }

// RefreshMCPMetadata godoc
// @Summary      Đồng bộ danh mục công cụ MCP
// @Description  Kết nối upstream rõ ràng và thay thế nguyên tử toàn bộ danh mục. Dịch vụ OAuth ghi ảnh chụp của người dùng hiện tại, Viewer trở lên gọi được; xác thực tĩnh ghi ảnh chụp dùng chung của đơn vị, cần Admin.
// @Tags         Dịch vụ MCP
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "ID dịch vụ MCP"
// @Success      200  {object}  map[string]interface{}  "Ảnh chụp danh mục sau khi đồng bộ"
// @Failure      400  {object}  errors.AppError         "danh mục không đầy đủ hoặc kiểm tra thất bại"
// @Failure      401  {object}  errors.AppError         "danh mục OAuth thiếu chủ thể uỷ quyền"
// @Failure      403  {object}  errors.AppError         "danh mục xác thực tĩnh cần quản trị viên làm mới"
// @Failure      404  {object}  errors.AppError         "dịch vụ không tồn tại"
// @Failure      409  {object}  errors.AppError         "cấu hình kết nối đã thay đổi trong lúc làm mới"
// @Failure      503  {object}  errors.AppError         "kho metadata không khả dụng"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-services/{id}/metadata/refresh [post]
func (h *MCPServiceHandler) RefreshMCPMetadata(c *gin.Context) { h.mcpMetadata(c, true) }

func (h *MCPServiceHandler) mcpMetadata(c *gin.Context, refresh bool) {
	ctx := c.Request.Context()
	tenant := c.GetUint64(types.TenantIDContextKey.String())
	if tenant == 0 {
		_ = c.Error(errors.NewBadRequestError("Workspace ID cannot be empty"))
		return
	}
	svc, ok := h.mcpServiceService.(interfaces.MCPMetadataService)
	if !ok {
		_ = c.Error(errors.NewServiceUnavailableError("MCP metadata storage is unavailable"))
		return
	}
	id := c.Param("id")
	var snapshot *types.MCPMetadata
	var err error
	if refresh {
		service, getErr := h.mcpServiceService.GetMCPServiceByID(ctx, tenant, id)
		if getErr != nil || service == nil {
			logger.ErrorWithFields(ctx, getErr, map[string]interface{}{
				"service_id": secutils.SanitizeForLog(id),
				"refresh":    true,
			})
			_ = c.Error(mcpMetadataAppError(types.ErrMCPServiceNotFound, true))
			return
		}
		if !service.AuthConfig.IsOAuth() && !mayWriteSharedMCPMetadata(ctx) {
			_ = c.Error(errors.NewForbiddenError("Refreshing a shared MCP directory requires an administrator"))
			return
		}
		snapshot, err = svc.RefreshMCPMetadata(ctx, tenant, id)
	} else {
		snapshot, err = svc.GetMCPMetadata(ctx, tenant, id)
	}
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"service_id": secutils.SanitizeForLog(id),
			"refresh":    refresh,
		})
		_ = c.Error(mcpMetadataAppError(err, refresh))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": snapshot})
}

// mayWriteSharedMCPMetadata is the extra gate for static-auth catalogs. The
// route stays Viewer+ so OAuth users can persist their own snapshot after
// authorizing in chat. API keys already passed manage-MCP; JWT callers need Admin.
func mayWriteSharedMCPMetadata(ctx context.Context) bool {
	if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		return true
	}
	if types.IsSystemAdminFromContext(ctx) {
		return true
	}
	return types.CallerFromContext(ctx).Role.IsTenantAdmin()
}

func mcpMetadataAppError(err error, refresh bool) *errors.AppError {
	switch {
	case stderrors.Is(err, types.ErrMCPServiceNotFound):
		return errors.NewNotFoundError("MCP service not found")
	case stderrors.Is(err, types.ErrMCPOAuthPrincipalRequired):
		return errors.NewUnauthorizedError("OAuth metadata requires an authenticated user")
	case stderrors.Is(err, types.ErrMCPMetadataStorage):
		return errors.NewServiceUnavailableError("MCP metadata storage is unavailable")
	case stderrors.Is(err, types.ErrMCPMetadataConnectionChanged):
		return errors.NewConflictError("MCP connection changed during refresh; save the configuration and sync again")
	case stderrors.Is(err, types.ErrMCPMetadataTooLarge), stderrors.Is(err, types.ErrMCPMetadataInvalidTools):
		return errors.NewBadRequestError("MCP directory is invalid or too large")
	default:
		if refresh {
			return errors.NewBadRequestError("Failed to refresh MCP tools. Check the connection and try again.")
		}
		return errors.NewInternalServerError("Failed to read MCP metadata")
	}
}
