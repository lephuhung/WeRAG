package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// FAQHandler handles FAQ knowledge base operations.
//
// All KB-access checks (own / org-shared / via shared agent) are now
// performed by the route-level g.KBAccessRead / g.KBAccessWrite
// guards in router.go — the guard rewrites c.Request.Context() to
// carry the effective tenant ID for the duration of the handler, so
// the handler reads tenant from context the way it always did.
type FAQHandler struct {
	knowledgeService interfaces.KnowledgeService
	kbService        interfaces.KnowledgeBaseService
}

// NewFAQHandler creates a new FAQ handler.
func NewFAQHandler(
	knowledgeService interfaces.KnowledgeService,
	kbService interfaces.KnowledgeBaseService,
) *FAQHandler {
	return &FAQHandler{
		knowledgeService: knowledgeService,
		kbService:        kbService,
	}
}

// faqDeleteRequest is a request for deleting FAQ entries in batch
type faqDeleteRequest struct {
	IDs []int64 `json:"ids" binding:"required,min=1"`
}

// faqEntryTagBatchRequest is a request for updating tags for FAQ entries in batch
// key: entry seq_id, value: tag seq_id (nil to remove tag)
type faqEntryTagBatchRequest struct {
	Updates map[int64]*int64 `json:"updates" binding:"required,min=1"`
}

// addSimilarQuestionsRequest is a request for adding similar questions to a FAQ entry
type addSimilarQuestionsRequest struct {
	SimilarQuestions []string `json:"similar_questions" binding:"required,min=1"`
}

// updateLastFAQImportResultDisplayStatusRequest is the request payload for UpdateLastImportResultDisplayStatus
type updateLastFAQImportResultDisplayStatusRequest struct {
	DisplayStatus string `json:"display_status" binding:"required,oneof=open close"`
}

// ListEntries godoc
// @Summary      Lấy danh sách mục FAQ
// @Description  Lấy danh sách mục FAQ trong kho tri thức, hỗ trợ phân trang và lọc
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id           path      string  true   "Knowledge BaseID"
// @Param        page         query     int     false  "Số trang"
// @Param        page_size    query     int     false  "Số mục mỗi trang"
// @Param        tag_id       query     int     false  "Lọc theo ID thẻ (seq_id), tương thích bản cũ một thẻ"
// @Param        tag_ids      query     string  false  "Lọc theo UUID thẻ, phân cách bằng dấu phẩy (ngữ nghĩa OR)"
// @Param        keyword      query     string  false  "Tìm theo từ khóa"
// @Param        search_field query     string  false  "Trường tìm kiếm: standard_question (câu hỏi chuẩn), similar_questions (câu hỏi tương tự), answers (câu trả lời); mặc định tìm tất cả"
// @Param        sort_order   query     string  false  "Thứ tự sắp xếp: asc (theo thời gian cập nhật tăng dần); mặc định giảm dần theo thời gian cập nhật"
// @Param        is_enabled   query     bool    false  "Lọc theo trạng thái bật; không truyền thì trả về tất cả"
// @Success      200        {object}  map[string]interface{}  "FAQList "
// @Failure      400        {object}  errors.AppError         "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries [get]
func (h *FAQHandler) ListEntries(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		logger.Error(ctx, "Failed to bind pagination query", err)
		c.Error(errors.NewBadRequestError("tham số phân trang không hợp lệ").WithDetails(err.Error()))
		return
	}

	tagUUIDs := parseCommaSeparatedTagIDs(c.Query("tag_ids"))
	var legacyTagSeqID int64
	tagIDStr := c.Query("tag_id")
	if tagIDStr != "" {
		var err error
		legacyTagSeqID, err = strconv.ParseInt(tagIDStr, 10, 64)
		if err != nil {
			c.Error(errors.NewBadRequestError("tag_id phải là số nguyên"))
			return
		}
	}
	keyword := secutils.SanitizeForLog(c.Query("keyword"))
	searchField := secutils.SanitizeForLog(c.Query("search_field"))
	sortOrder := secutils.SanitizeForLog(c.Query("sort_order"))
	isEnabled, err := parseOptionalFAQEnabled(c)
	if err != nil {
		c.Error(err)
		return
	}

	result, err := h.knowledgeService.ListFAQEntries(ctx, kbID, &page, tagUUIDs, legacyTagSeqID, keyword, searchField, sortOrder, isEnabled)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// parseOptionalFAQEnabled preserves the distinction between an omitted filter
// and an explicit false value so the management endpoint remains backward compatible.
func parseOptionalFAQEnabled(c *gin.Context) (*bool, error) {
	raw, exists := c.GetQuery("is_enabled")
	if !exists {
		return nil, nil
	}

	var value bool
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		value = true
	case "false":
		value = false
	default:
		return nil, errors.NewBadRequestError("is_enabled must be true or false")
	}
	return &value, nil
}

// UpsertEntries godoc
// @Summary      Cập nhật/chèn hàng loạt mục FAQ
// @Description  Cập nhật hoặc chèn hàng loạt mục FAQ bất đồng bộ. Hỗ trợ chế độ dry_run (đặt dry_run=true): chỉ kiểm tra bất đồng bộ, không nhập thật.
// @Description  Chế độ dry_run là thao tác bất đồng bộ, trả về task_id; xem tiến độ và kết quả qua /faq/import/progress/{task_id}.
// @Description  Nội dung kiểm tra gồm: 1) định dạng cơ bản của mục 2) câu hỏi trùng (trong lô và đã có trong kho tri thức) 3) kiểm tra an toàn nội dung.
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id       path      string                    true  "Knowledge BaseID"
// @Param        request  body      types.FAQBatchUpsertPayload  true  "Yêu cầu thao tác hàng loạt"
// @Success      200      {object}  map[string]interface{}    "ID tác vụ"
// @Failure      400      {object}  errors.AppError           "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries [post]
func (h *FAQHandler) UpsertEntries(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req types.FAQBatchUpsertPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind FAQ upsert payload", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}

	taskID, err := h.knowledgeService.UpsertFAQEntries(ctx, kbID, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"task_id": taskID,
		},
	})
}

// CreateEntry godoc
// @Summary      Tạo một mục FAQ
// @Description  Tạo đồng bộ một mục FAQ
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id       path      string                true  "Knowledge BaseID"
// @Param        request  body      types.FAQEntryPayload true  "Mục FAQ"
// @Success      200      {object}  map[string]interface{}  "Mục FAQ đã tạo"
// @Failure      400      {object}  errors.AppError         "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entry [post]
func (h *FAQHandler) CreateEntry(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req types.FAQEntryPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind FAQ entry payload", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}

	entry, err := h.knowledgeService.CreateFAQEntry(ctx, kbID, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entry,
	})
}

// UpdateEntry godoc
// @Summary      Cập nhật mục FAQ
// @Description  Cập nhật mục FAQ chỉ định
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id        path      string                true  "Knowledge BaseID"
// @Param        entry_id  path      int                   true  "ID mục FAQ (seq_id)"
// @Param        request   body      types.FAQEntryPayload true  "Mục FAQ"
// @Success      200       {object}  map[string]interface{}  "Cập nhật thành công"
// @Failure      400       {object}  errors.AppError         "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries/{entry_id} [put]
func (h *FAQHandler) UpdateEntry(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req types.FAQEntryPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind FAQ entry payload", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}

	entrySeqID, err := strconv.ParseInt(c.Param("entry_id"), 10, 64)
	if err != nil {
		c.Error(errors.NewBadRequestError("entry_id phải là số nguyên"))
		return
	}

	entry, err := h.knowledgeService.UpdateFAQEntry(ctx, kbID, entrySeqID, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entry,
	})
}

// UpdateEntryTagBatch godoc
// @Summary      Cập nhật thẻ FAQ hàng loạt
// @Description  Cập nhật hàng loạt thẻ của các mục FAQ
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id       path      string  true  "Knowledge BaseID"
// @Param        request  body      object  true  "Yêu cầu cập nhật thẻ"
// @Success      200      {object}  map[string]interface{}  "Cập nhật thành công"
// @Failure      400      {object}  errors.AppError         "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries/tags [put]
func (h *FAQHandler) UpdateEntryTagBatch(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req faqEntryTagBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind FAQ entry tag batch payload", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}
	if err := h.knowledgeService.UpdateFAQEntryTagBatch(ctx, kbID, req.Updates); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// UpdateEntryFieldsBatch godoc
// @Summary      Cập nhật trường FAQ hàng loạt
// @Description  Cập nhật hàng loạt nhiều trường của mục FAQ (is_enabled, is_recommended, tag_id)
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id       path      string                        true  "Knowledge BaseID"
// @Param        request  body      types.FAQEntryFieldsBatchUpdate  true  "Yêu cầu cập nhật trường"
// @Success      200      {object}  map[string]interface{}        "Cập nhật thành công"
// @Failure      400      {object}  errors.AppError               "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries/fields [put]
func (h *FAQHandler) UpdateEntryFieldsBatch(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req types.FAQEntryFieldsBatchUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind FAQ entry fields batch payload", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}
	if err := h.knowledgeService.UpdateFAQEntryFieldsBatch(ctx, kbID, &req); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// DeleteEntries godoc
// @Summary      Xóa hàng loạt mục FAQ
// @Description  Xóa hàng loạt các mục FAQ chỉ định
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id       path      string  true  "Knowledge BaseID"
// @Param        request  body      object{ids=[]int}  true  "Danh sách ID FAQ cần xóa (seq_id)"
// @Success      200      {object}  map[string]interface{}  "Xóa thành công"
// @Failure      400      {object}  errors.AppError         "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries [delete]
func (h *FAQHandler) DeleteEntries(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req faqDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf(ctx, "Failed to bind FAQ delete payload: %s", secutils.SanitizeForLog(err.Error()))
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}

	if err := h.knowledgeService.DeleteFAQEntries(ctx, kbID, req.IDs); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// SearchFAQ godoc
// @Summary      Tìm kiếm FAQ
// @Description  Tìm trong FAQ bằng tìm kiếm kết hợp, hỗ trợ truy hồi theo thẻ hai mức ưu tiên: first_priority_tag_ids cao nhất, second_priority_tag_ids kế tiếp
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id       path      string                true  "Knowledge BaseID"
// @Param        request  body      types.FAQSearchRequest  true  "Yêu cầu tìm kiếm"
// @Success      200      {object}  map[string]interface{}  "Kết quả tìm kiếm"
// @Failure      400      {object}  errors.AppError         "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/search [post]
func (h *FAQHandler) SearchFAQ(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req types.FAQSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind FAQ search payload", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}
	req.QueryText = secutils.SanitizeForLog(req.QueryText)
	if req.MatchCount <= 0 {
		req.MatchCount = 10
	}
	if req.MatchCount > 200 {
		req.MatchCount = 200
	}
	entries, err := h.knowledgeService.SearchFAQEntries(ctx, kbID, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entries,
	})
}

// ExportEntries godoc
// @Summary      Xuất mục FAQ
// @Description  Xuất toàn bộ mục FAQ ra CSV (mặc định) hoặc JSON. ?format=json trả về mảng tương thích cấu trúc FAQEntryPayload.
// @Tags         FAQ
// @Accept       json
// @Produce      text/csv
// @Produce      application/json
// @Param        id      path      string  true   "Knowledge BaseID"
// @Param        format  query     string  false  "Định dạng xuất: csv (mặc định) hoặc json"
// @Success      200     {file}    file    "Tệp xuất"
// @Failure      400     {object}  errors.AppError  "Tham số yêu cầu không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries/export [get]
func (h *FAQHandler) ExportEntries(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))
	format := strings.ToLower(strings.TrimSpace(c.Query("format")))

	if format == "json" {
		jsonData, err := h.knowledgeService.ExportFAQEntriesJSON(ctx, kbID)
		if err != nil {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(err)
			return
		}
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.Header("Content-Disposition", "attachment; filename=faq_export.json")
		c.Data(http.StatusOK, "application/json; charset=utf-8", jsonData)
		return
	}

	csvData, err := h.knowledgeService.ExportFAQEntries(ctx, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	// Set response headers for CSV download
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=faq_export.csv")
	// Add BOM for Excel compatibility with UTF-8
	bom := []byte{0xEF, 0xBB, 0xBF}
	c.Data(http.StatusOK, "text/csv; charset=utf-8", append(bom, csvData...))
}

// GetEntry godoc
// @Summary      Lấy chi tiết mục FAQ
// @Description  Lấy chi tiết một mục FAQ theo ID
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id        path      string  true  "Knowledge BaseID"
// @Param        entry_id  path      int     true  "ID mục FAQ (seq_id)"
// @Success      200       {object}  map[string]interface{}  "Chi tiết mục FAQ"
// @Failure      400       {object}  errors.AppError         "Tham số yêu cầu không hợp lệ"
// @Failure      404       {object}  errors.AppError         "Không tìm thấy mục"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries/{entry_id} [get]
func (h *FAQHandler) GetEntry(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	entrySeqID, err := strconv.ParseInt(c.Param("entry_id"), 10, 64)
	if err != nil {
		c.Error(errors.NewBadRequestError("entry_id phải là số nguyên"))
		return
	}

	entry, err := h.knowledgeService.GetFAQEntry(ctx, kbID, entrySeqID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entry,
	})
}

// GetImportProgress godoc
// @Summary      Lấy tiến độ nhập FAQ
// @Description  Lấy tiến độ của tác vụ nhập FAQ
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        task_id  path      string  true  "ID tác vụ"
// @Success      200      {object}  map[string]interface{}  "Tiến độ nhập"
// @Failure      404      {object}  errors.AppError         "Không tìm thấy tác vụ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /faq/import/progress/{task_id} [get]
func (h *FAQHandler) GetImportProgress(c *gin.Context) {
	ctx := c.Request.Context()
	taskID := secutils.SanitizeForLog(c.Param("task_id"))
	if err := requireTaskProgressTenant(ctx, taskID); err != nil {
		c.Error(err)
		return
	}

	progress, err := h.knowledgeService.GetFAQImportProgress(ctx, taskID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    progress,
	})
}

// UpdateLastImportResultDisplayStatus godoc
// @Summary      Cập nhật trạng thái hiển thị kết quả nhập FAQ gần nhất
// @Description  Cập nhật trạng thái hiện/ẩn thẻ thống kê kết quả nhập của kho tri thức FAQ
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id      path      string                                         true  "Knowledge BaseID"
// @Param        request body      updateLastFAQImportResultDisplayStatusRequest  true  "Yêu cầu cập nhật trạng thái"
// @Success      200     {object}  map[string]interface{}                         "Cập nhật thành công"
// @Failure      400     {object}  errors.AppError                                "Tham số yêu cầu không hợp lệ"
// @Failure      404     {object}  errors.AppError                                "Không tìm thấy kho tri thức hoặc chưa có lần nhập nào"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/import/last-result/display [put]
func (h *FAQHandler) UpdateLastImportResultDisplayStatus(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req updateLastFAQImportResultDisplayStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind display status update payload", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}

	if err := h.knowledgeService.UpdateLastFAQImportResultDisplayStatus(ctx, kbID, req.DisplayStatus); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// AddSimilarQuestions godoc
// @Summary      Thêm câu hỏi tương tự
// @Description  Thêm câu hỏi tương tự vào mục FAQ chỉ định
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Param        id        path      string                      true  "Knowledge BaseID"
// @Param        entry_id  path      int                         true  "ID mục FAQ (seq_id)"
// @Param        request   body      addSimilarQuestionsRequest  true  "Danh sách câu hỏi tương tự"
// @Success      200       {object}  map[string]interface{}      "Mục FAQ sau cập nhật"
// @Failure      400       {object}  errors.AppError             "Tham số yêu cầu không hợp lệ"
// @Failure      404       {object}  errors.AppError             "Không tìm thấy mục"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/faq/entries/{entry_id}/similar-questions [post]
func (h *FAQHandler) AddSimilarQuestions(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	entrySeqID, err := strconv.ParseInt(c.Param("entry_id"), 10, 64)
	if err != nil {
		c.Error(errors.NewBadRequestError("entry_id phải là số nguyên"))
		return
	}

	var req addSimilarQuestionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind add similar questions payload", err)
		c.Error(errors.NewBadRequestError("tham số yêu cầu không hợp lệ").WithDetails(err.Error()))
		return
	}

	entry, err := h.knowledgeService.AddSimilarQuestions(ctx, kbID, entrySeqID, req.SimilarQuestions)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entry,
	})
}
