package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service/memory"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// MemoryHandler exposes the caller's own long-term memory.
//
// Every route operates on the memory space derived from the request context,
// so no endpoint takes a subject id. That is deliberate: it removes the entire
// class of "can I read another user's memories by changing an id" bugs instead
// of relying on a per-route ownership check.
type MemoryHandler struct {
	memoryService interfaces.MemoryService
}

func NewMemoryHandler(memoryService interfaces.MemoryService) *MemoryHandler {
	return &MemoryHandler{memoryService: memoryService}
}

// GetSettings godoc
// @Summary      Lấy cài đặt bộ nhớ của tôi
// @Description  Trả về trạng thái bật/tắt bộ nhớ đã gộp (cấp tổ chức + cấp cá nhân) và số mục bộ nhớ
// @Tags         Bộ nhớ
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Cài đặt bộ nhớ"
// @Security     Bearer
// @Router       /memory/settings [get]
func (h *MemoryHandler) GetSettings(c *gin.Context) {
	ctx := c.Request.Context()
	settings, err := h.memoryService.GetSettings(ctx)
	if err != nil {
		h.fail(c, err, "Failed to load memory settings")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": settings})
}

type updateMemorySettingsRequest struct {
	Enabled *bool `json:"enabled"`
}

// UpdateSettings godoc
// @Summary      Cập nhật cài đặt bộ nhớ của tôi
// @Description  Bật hoặc tắt bộ nhớ dài hạn của chính người dùng hiện tại
// @Tags         Bộ nhớ
// @Accept       json
// @Produce      json
// @Param        request  body      object  true  "Settings "
// @Success      200      {object}  map[string]interface{}  "Cài đặt sau khi cập nhật"
// @Security     Bearer
// @Router       /memory/settings [put]
func (h *MemoryHandler) UpdateSettings(c *gin.Context) {
	ctx := c.Request.Context()
	var req updateMemorySettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	if req.Enabled == nil {
		c.Error(apperrors.NewBadRequestError("enabled is required"))
		return
	}
	if err := h.memoryService.SetEnabled(ctx, *req.Enabled); err != nil {
		h.fail(c, err, "Failed to update memory settings")
		return
	}
	settings, err := h.memoryService.GetSettings(ctx)
	if err != nil {
		h.fail(c, err, "Failed to load memory settings")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": settings})
}

// ListItems godoc
// @Summary      Liệt kê bộ nhớ của tôi
// @Description  Trả về theo trang các mục bộ nhớ của người dùng hiện tại, có thể lọc theo trạng thái
// @Tags         Bộ nhớ
// @Produce      json
// @Param        status  query     string  false  "Lọc theo trạng thái"  Enums(active, superseded, archived, pending)
// @Param        limit   query     int     false  "Số mục mỗi trang"  default(50)
// @Param        offset  query     int     false  "Độ lệch"
// @Success      200     {object}  map[string]interface{}  "Danh sách bộ nhớ"
// @Security     Bearer
// @Router       /memory/items [get]
func (h *MemoryHandler) ListItems(c *gin.Context) {
	ctx := c.Request.Context()
	status := c.Query("status")
	switch status {
	case "", types.MemoryStatusActive, types.MemoryStatusSuperseded,
		types.MemoryStatusArchived, types.MemoryStatusPending:
	default:
		c.Error(apperrors.NewBadRequestError("unsupported status"))
		return
	}
	limit, offset := memoryListPaging(c)

	items, total, err := h.memoryService.ListItems(ctx, status, limit, offset)
	if err != nil {
		h.fail(c, err, "Failed to list memories")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    items,
		"total":   total,
	})
}

const (
	// memoryExportPageSize is how many rows one export page reads.
	memoryExportPageSize = 500
	// memoryExportMaxItems bounds a single export so one enormous store cannot
	// turn a download into an unbounded read.
	memoryExportMaxItems = 20000
)

func memoryListPaging(c *gin.Context) (limit, offset int) {
	limit, _ = strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ = strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// ListTopics godoc
// @Summary      Liệt kê các chủ đề đang theo dõi
// @Description  Trả về các chủ đề đã đếm nhưng chưa nâng thành mối quan tâm dài hạn, kèm số lần còn thiếu để đạt ngưỡng
// @Tags         Bộ nhớ
// @Produce      json
// @Param        limit   query     int  false  "Số mục mỗi trang"  default(50)
// @Param        offset  query     int  false  "Độ lệch"
// @Success      200     {object}  map[string]interface{}  "Danh sách chủ đề"
// @Security     Bearer
// @Router       /memory/topics [get]
func (h *MemoryHandler) ListTopics(c *gin.Context) {
	ctx := c.Request.Context()
	limit, offset := memoryListPaging(c)
	topics, total, err := h.memoryService.ListTopics(ctx, limit, offset)
	if err != nil {
		h.fail(c, err, "Failed to list topics")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    topics,
		"total":   total,
	})
}

// PromoteTopic godoc
// @Summary      Ghi ngay thành mối quan tâm dài hạn
// @Description  Không chờ đủ số lần còn lại, nâng chủ đề đang theo dõi thành một mục bộ nhớ quan tâm dài hạn
// @Tags         Bộ nhớ
// @Produce      json
// @Param        id   path      string  true  "ID chủ đề"
// @Success      200  {object}  map[string]interface{}  "Mục bộ nhớ mới"
// @Security     Bearer
// @Router       /memory/topics/{id}/promote [post]
func (h *MemoryHandler) PromoteTopic(c *gin.Context) {
	ctx := c.Request.Context()
	item, err := h.memoryService.PromoteTopic(ctx, c.Param("id"))
	if err != nil {
		h.fail(c, err, "Failed to promote topic")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// DeleteTopic godoc
// @Summary      Ngừng theo dõi một chủ đề
// @Description  Xoá bộ đếm của chủ đề chưa được nâng và ghi nhớ lần từ chối này, sau đó sẽ không tự ghi thành mối quan tâm dài hạn nữa
// @Tags         Bộ nhớ
// @Produce      json
// @Param        id   path      string  true  "ID chủ đề"
// @Success      200  {object}  map[string]interface{}  "Xoá thành công"
// @Security     Bearer
// @Router       /memory/topics/{id} [delete]
func (h *MemoryHandler) DeleteTopic(c *gin.Context) {
	ctx := c.Request.Context()
	if err := h.memoryService.DeleteTopic(ctx, c.Param("id")); err != nil {
		h.fail(c, err, "Failed to delete topic")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListDocuments godoc
// @Summary      Liệt kê tài liệu hay dùng
// @Description  Trả về các tài liệu được trích dẫn lặp lại trong câu trả lời của người dùng hiện tại, chưa đạt ngưỡng thói quen thì không hiển thị
// @Tags         Bộ nhớ
// @Produce      json
// @Param        limit   query     int  false  "Số mục mỗi trang"  default(50)
// @Param        offset  query     int  false  "Độ lệch"
// @Success      200     {object}  map[string]interface{}  "Danh sách tài liệu"
// @Security     Bearer
// @Router       /memory/documents [get]
func (h *MemoryHandler) ListDocuments(c *gin.Context) {
	ctx := c.Request.Context()
	limit, offset := memoryListPaging(c)
	docs, total, err := h.memoryService.ListDocuments(ctx, limit, offset)
	if err != nil {
		h.fail(c, err, "Failed to list documents")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    docs,
		"total":   total,
	})
}

// DeleteDocument godoc
// @Summary      Ngừng dùng một tài liệu để cá nhân hoá tìm kiếm
// @Description  Xoá một bộ đếm mức độ gắn bó với tài liệu, sau đó tìm kiếm không còn ưu tiên tài liệu này nữa
// @Tags         Bộ nhớ
// @Produce      json
// @Param        id   path      string  true  "ID mức độ gắn bó"
// @Success      200  {object}  map[string]interface{}  "Xoá thành công"
// @Security     Bearer
// @Router       /memory/documents/{id} [delete]
func (h *MemoryHandler) DeleteDocument(c *gin.Context) {
	ctx := c.Request.Context()
	if err := h.memoryService.DeleteDocument(ctx, c.Param("id")); err != nil {
		h.fail(c, err, "Failed to delete document affinity")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type createMemoryItemRequest struct {
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Importance int    `json:"importance"`
}

// CreateItem godoc
// @Summary      Thêm một mục bộ nhớ
// @Description  Thêm thủ công một mục bộ nhớ dài hạn
// @Tags         Bộ nhớ
// @Accept       json
// @Produce      json
// @Param        request  body      object  true  "Nội dung bộ nhớ"
// @Success      200      {object}  map[string]interface{}  "Mục bộ nhớ mới"
// @Security     Bearer
// @Router       /memory/items [post]
func (h *MemoryHandler) CreateItem(c *gin.Context) {
	ctx := c.Request.Context()
	var req createMemoryItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	item, err := h.memoryService.CreateItem(ctx, req.Kind, req.Content, req.Importance)
	if err != nil {
		h.fail(c, err, "Failed to create memory")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

type updateMemoryItemRequest struct {
	Content    string `json:"content"`
	Importance int    `json:"importance"`
}

// UpdateItem godoc
// @Summary      Sửa một mục bộ nhớ
// @Description  Sửa nội dung và mức quan trọng của mục bộ nhớ; sau khi sửa, mục này sẽ không bị quá trình trích xuất nền ghi đè
// @Tags         Bộ nhớ
// @Accept       json
// @Produce      json
// @Param        id       path      string  true  "ID bộ nhớ"
// @Param        request  body      object  true  "Nội dung bộ nhớ"
// @Success      200      {object}  map[string]interface{}  "Mục bộ nhớ sau khi cập nhật"
// @Security     Bearer
// @Router       /memory/items/{id} [put]
func (h *MemoryHandler) UpdateItem(c *gin.Context) {
	ctx := c.Request.Context()
	var req updateMemoryItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("Invalid request data").WithDetails(err.Error()))
		return
	}
	item, err := h.memoryService.UpdateItem(ctx, c.Param("id"), req.Content, req.Importance)
	if err != nil {
		h.fail(c, err, "Failed to update memory")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// DeleteItem godoc
// @Summary      Xoá một mục bộ nhớ
// @Description  Xoá vĩnh viễn một mục bộ nhớ
// @Tags         Bộ nhớ
// @Produce      json
// @Param        id  path      string  true  "ID bộ nhớ"
// @Success      200  {object}  map[string]interface{}  "Xoá thành công"
// @Security     Bearer
// @Router       /memory/items/{id} [delete]
func (h *MemoryHandler) DeleteItem(c *gin.Context) {
	ctx := c.Request.Context()
	if err := h.memoryService.DeleteItem(ctx, c.Param("id")); err != nil {
		h.fail(c, err, "Failed to delete memory")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ConfirmItem godoc
// @Summary      Xác nhận một mục bộ nhớ được suy luận
// @Description  Chấp nhận mục bộ nhớ hệ thống suy luận để bắt đầu có hiệu lực
// @Tags         Bộ nhớ
// @Produce      json
// @Param        id   path      string  true  "ID bộ nhớ"
// @Success      200  {object}  map[string]interface{}  "Xác nhận thành công"
// @Security     Bearer
// @Router       /memory/items/{id}/confirm [post]
//
// Inferred memories are the ones worth having and the ones most likely to be
// wrong, so they wait here rather than taking effect silently.
func (h *MemoryHandler) ConfirmItem(c *gin.Context) {
	ctx := c.Request.Context()
	item, err := h.memoryService.ConfirmItem(ctx, c.Param("id"))
	if err != nil {
		h.fail(c, err, "Failed to confirm memory")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// RejectItem godoc
// @Summary      Từ chối một mục bộ nhớ được suy luận
// @Description  Từ chối mục bộ nhớ hệ thống suy luận và ghi nhớ lần từ chối này
// @Tags         Bộ nhớ
// @Produce      json
// @Param        id   path      string  true  "ID bộ nhớ"
// @Success      200  {object}  map[string]interface{}  "Từ chối thành công"
// @Security     Bearer
// @Router       /memory/items/{id}/reject [post]
func (h *MemoryHandler) RejectItem(c *gin.Context) {
	ctx := c.Request.Context()
	if err := h.memoryService.RejectItem(ctx, c.Param("id")); err != nil {
		h.fail(c, err, "Failed to reject memory")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Clear godoc
// @Summary      Xoá toàn bộ bộ nhớ của tôi
// @Description  Xoá vĩnh viễn toàn bộ bộ nhớ của người dùng hiện tại
// @Tags         Bộ nhớ
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Đã xoá toàn bộ"
// @Security     Bearer
// @Router       /memory/items [delete]
func (h *MemoryHandler) Clear(c *gin.Context) {
	ctx := c.Request.Context()
	removed, err := h.memoryService.Clear(ctx)
	if err != nil {
		h.fail(c, err, "Failed to clear memories")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "removed": removed})
}

// Export godoc
// @Summary      Xuất bộ nhớ của tôi
// @Description  Xuất toàn bộ bộ nhớ của người dùng hiện tại dưới dạng JSON
// @Tags         Bộ nhớ
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Bản xuất bộ nhớ"
// @Security     Bearer
// @Router       /memory/export [get]
func (h *MemoryHandler) Export(c *gin.Context) {
	ctx := c.Request.Context()
	// Export is a snapshot, not a page, so it walks every status to the end.
	//
	// A single fixed page used to serve this on the grounds that it matched the
	// largest capacity a workspace can configure. It does not: max_items caps
	// active memories only, while superseded and archived rows accumulate
	// without limit, so a long-lived store holds far more than its capacity and
	// the export quietly returned a prefix of it.
	var items []*types.MemoryItem
	var total int64
	for {
		page, pageTotal, err := h.memoryService.ListItems(ctx, "", memoryExportPageSize, len(items))
		if err != nil {
			h.fail(c, err, "Failed to export memories")
			return
		}
		total = pageTotal
		items = append(items, page...)
		if len(page) < memoryExportPageSize || int64(len(items)) >= total {
			break
		}
		if len(items) >= memoryExportMaxItems {
			break
		}
	}
	c.Header("Content-Disposition", `attachment; filename="weknora-memories.json"`)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"total":   total,
		// Say so rather than letting a partial file look complete. Only the
		// safety ceiling can trigger this, so it stays false in practice.
		"truncated": int64(len(items)) < total,
		"data":      items,
	})
}

// Consolidate godoc
// @Summary      Sắp xếp bộ nhớ của tôi ngay
// @Description  Gộp các mục gần nghĩa, lưu trữ các việc đã đến hạn, không chờ đợt sắp xếp nền hằng ngày
// @Tags         Bộ nhớ
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Kết quả sắp xếp"
// @Security     Bearer
// @Router       /memory/consolidate [post]
func (h *MemoryHandler) Consolidate(c *gin.Context) {
	ctx := c.Request.Context()
	result, err := h.memoryService.ConsolidateNow(ctx)
	if err != nil {
		h.fail(c, err, "Failed to consolidate memories")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// fail maps service errors onto HTTP responses. A missing item and an item
// belonging to someone else produce the same 404 on purpose.
func (h *MemoryHandler) fail(c *gin.Context, err error, message string) {
	switch {
	case errors.Is(err, memory.ErrNoMemoryScope):
		c.Error(apperrors.NewUnauthorizedError("no principal in request"))
	case errors.Is(err, memory.ErrItemNotFound):
		c.Error(apperrors.NewNotFoundError("memory not found"))
	case errors.Is(err, types.ErrMemoryConflict):
		c.Error(apperrors.NewConflictError(err.Error()))
	case errors.Is(err, memory.ErrSensitiveContent):
		c.Error(apperrors.NewBadRequestError(err.Error()))
	case errors.Is(err, memory.ErrMemoryDisabled):
		c.Error(apperrors.NewBadRequestError("memory is disabled"))
	default:
		logger.ErrorWithFields(c.Request.Context(), err, nil)
		c.Error(apperrors.NewInternalServerError(message).WithDetails(err.Error()))
	}
}
