package session

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// ListArtifactLibrary godoc
// @Summary      Liệt kê sản phẩm của tôi
// @Description  Liệt kê các tệp do kỹ năng tạo ra trong mọi cuộc trò chuyện người dùng hiện tại nhìn thấy, mỗi tệp chỉ trả về phiên bản mới nhất (không kèm URL lưu trữ)
// @Tags         Cuộc trò chuyện
// @Produce      json
// @Param        keyword     query  string  false  "Lọc theo tên tệp"
// @Param        file_types  query  string  false  "Các phần mở rộng cách nhau bằng dấu phẩy, ví dụ .pdf,.pptx"
// @Param        page        query  int     false  "Số trang"
// @Param        page_size   query  int     false  "Số mục mỗi trang (tối đa 100)"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /artifacts [get]
//
// Items carry session_id, message_id and index so the client downloads
// through the per-session endpoint, which re-runs the ownership check.
func (h *Handler) ListArtifactLibrary(c *gin.Context) {
	ctx := c.Request.Context()

	var pagination types.Pagination
	if err := c.ShouldBindQuery(&pagination); err != nil {
		_ = c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	var fileTypes []string
	if raw := c.Query("file_types"); raw != "" {
		fileTypes = strings.Split(raw, ",")
	}

	result, err := h.messageService.ListArtifactLibrary(ctx, &types.ArtifactLibraryQuery{
		Keyword:   c.Query("keyword"),
		FileTypes: fileTypes,
		Page:      pagination.Page,
		PageSize:  pagination.PageSize,
	})
	if err != nil {
		logger.Errorf(ctx, "list artifact library failed: %v", err)
		_ = c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"data":      result.Data,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}
