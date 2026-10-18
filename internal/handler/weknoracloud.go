package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// WeKnoraCloudHandler xử lý quản lý thông tin xác thực WeKnoraCloud
type WeKnoraCloudHandler struct {
	svc interfaces.WeKnoraCloudService
}

// NewWeKnoraCloudHandler là hàm khởi tạo
func NewWeKnoraCloudHandler(svc interfaces.WeKnoraCloudService) *WeKnoraCloudHandler {
	return &WeKnoraCloudHandler{svc: svc}
}

type weKnoraCloudCredentialsRequest struct {
	AppID     string `json:"app_id"     binding:"required"`
	AppSecret string `json:"app_secret" binding:"required"`
}

// SaveCredentials POST /api/v1/weknoracloud/credentials
// Chỉ lưu APPID/APPSECRET vào cấu hình đơn vị, không tự tạo mô hình
//
// SaveCredentials godoc
// @Summary      Lưu thông tin xác thực WeKnoraCloud
// @Description  Lưu APPID/APPSECRET vào cấu hình tổ chức hiện tại (không tự tạo mô hình)
// @Tags         WeKnoraCloud
// @Accept       json
// @Produce      json
// @Param        request  body      map[string]interface{}  true  "{app_id, app_secret}"
// @Success      200      {object}  map[string]interface{}  "success: true"
// @Failure      400      {object}  map[string]interface{}  "tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /weknoracloud/credentials [post]
func (h *WeKnoraCloudHandler) SaveCredentials(c *gin.Context) {
	var req weKnoraCloudCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.SaveCredentials(c.Request.Context(), req.AppID, req.AppSecret); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "đã lưu thông tin xác thực"})
}

// Status GET /api/v1/models/weknoracloud/status
// Kiểm tra thông tin xác thực WeKnoraCloud của đơn vị hiện tại còn nguyên vẹn không; nếu cần khởi tạo lại thì trả về needs_reinit=true
//
// Status godoc
// @Summary      Kiểm tra trạng thái thông tin xác thực WeKnoraCloud
// @Description  Kiểm tra thông tin xác thực WeKnoraCloud của tổ chức hiện tại còn nguyên vẹn không; needs_reinit=true nghĩa là cần lưu lại
// @Tags         WeKnoraCloud
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Trạng thái thông tin xác thực"
// @Failure      500  {object}  map[string]interface{}  "lỗi hệ thống"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models/weknoracloud/status [get]
func (h *WeKnoraCloudHandler) Status(c *gin.Context) {
	result, err := h.svc.CheckStatus(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
