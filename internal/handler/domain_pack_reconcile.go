package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// DomainPackReconcileHandler exposes the SuperAdmin check of a domain
// pack's catalogue against the legal catalogue. The router gates it on an
// explicit human SuperAdmin.
type DomainPackReconcileHandler struct {
	reconciler *service.DomainPackReconciler
}

// NewDomainPackReconcileHandler creates the handler.
func NewDomainPackReconcileHandler(reconciler *service.DomainPackReconciler) *DomainPackReconcileHandler {
	return &DomainPackReconcileHandler{reconciler: reconciler}
}

// ReconcileDomainPack godoc
// @Summary      Đối chiếu danh mục của bộ kỹ năng chuyên môn
// @Description  Tra từng số hiệu trong catalogue.yaml của gói theo
// @Description  legal_documents.number_key: found (một văn bản, kèm
// @Description  status_note khi danh mục ghi hết hiệu lực hoặc bị thay thế),
// @Description  missing (không có; ghi một yêu cầu bổ sung văn bản nguồn
// @Description  detail.origin = pack:<tên gói>, không trùng yêu cầu đã có) hoặc ambiguous
// @Description  (nhiều văn bản cùng số hiệu). Không sửa legal_documents.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        name path string true "Tên bộ kỹ năng (domain pack)"
// @Success      200 {object} types.DomainPackReconcileResult "Kết quả đối chiếu"
// @Security     Bearer
// @Router       /system/domain-packs/{name}/reconcile [post]
func (h *DomainPackReconcileHandler) ReconcileDomainPack(c *gin.Context) {
	ctx := c.Request.Context()
	result, err := h.reconciler.Reconcile(ctx, c.Param("name"))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// ListSystemDomainPacks godoc
// @Summary      Danh sách bộ kỹ năng chuyên môn (SuperAdmin)
// @Description  Các bộ kỹ năng chuyên môn có trong bản build: phiên bản, mốc dữ
// @Description  liệu, hạn rà soát, số văn bản trích dẫn, số công thức, số
// @Description  agent đang dùng (mọi Tổ chức).
// @Tags         Rà soát pháp lý
// @Produce      json
// @Success      200 {array} types.DomainPackSummary "Danh sách gói"
// @Security     Bearer
// @Router       /system/domain-packs [get]
func (h *DomainPackReconcileHandler) ListSystemDomainPacks(c *gin.Context) {
	ctx := c.Request.Context()
	list, err := h.reconciler.List(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	if list == nil {
		list = []types.DomainPackSummary{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// GetSystemDomainPack godoc
// @Summary      Chi tiết bộ kỹ năng chuyên môn (SuperAdmin)
// @Description  Văn bản trích dẫn (tra danh mục chỉ đọc: trạng thái, có
// @Description  trong KB, ghi chú hiệu lực, tệp trích, công thức dùng),
// @Description  công thức (căn cứ ok/warning), agent đang dùng. Không ghi
// @Description  yêu cầu nào; POST …/reconcile mới ghi.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        name path string true "Tên bộ kỹ năng (domain pack)"
// @Success      200 {object} types.DomainPackDetail "Chi tiết gói"
// @Security     Bearer
// @Router       /system/domain-packs/{name} [get]
func (h *DomainPackReconcileHandler) GetSystemDomainPack(c *gin.Context) {
	ctx := c.Request.Context()
	detail, err := h.reconciler.Detail(ctx, c.Param("name"))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
}

// UpdateDomainPackFormula godoc
// @Summary      Sửa công thức của bộ kỹ năng chuyên môn (SuperAdmin)
// @Description  Thay lời của một công thức trong formulas.yaml: tên, biểu
// @Description  thức, mô tả/đơn vị/nguồn của biến, căn cứ (số hiệu phải có
// @Description  trong catalogue.yaml), ví dụ. Không đổi được id, tool và tên
// @Description  biến (code Go gắn với chúng); phép tính vẫn là code Go. Mọi
// @Description  ví dụ phải được code Go tính ra đúng (tiền ±1 đ, tỷ lệ ±1e-9);
// @Description  biểu thức không được ghi tỷ lệ là hằng số. Lỗi kiểm tra trả
// @Description  400 kèm danh sách lỗi trong details. Bản sửa áp dụng ngay cho
// @Description  lượt chat sau; DELETE cùng đường dẫn khôi phục bản gốc.
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Param        name    path string                      true "Tên bộ kỹ năng (domain pack)"
// @Param        id      path string                      true "id công thức"
// @Param        formula body types.DomainPackFormulaEdit true "Công thức đã sửa"
// @Success      200 {object} types.DomainPackDetail "Chi tiết gói sau khi sửa"
// @Security     Bearer
// @Router       /system/domain-packs/{name}/formulas/{id} [put]
func (h *DomainPackReconcileHandler) UpdateDomainPackFormula(c *gin.Context) {
	ctx := c.Request.Context()
	var edit types.DomainPackFormulaEdit
	if err := c.ShouldBindJSON(&edit); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid formula body").WithDetails(err.Error()))
		return
	}
	detail, err := h.reconciler.UpdateFormula(ctx, c.Param("name"), c.Param("id"), edit)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
}

// ResetDomainPackFormula godoc
// @Summary      Khôi phục công thức gốc của bộ kỹ năng chuyên môn (SuperAdmin)
// @Description  Bỏ bản sửa của công thức: formulas.yaml áp dụng lại.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        name path string true "Tên bộ kỹ năng (domain pack)"
// @Param        id   path string true "id công thức"
// @Success      200 {object} types.DomainPackDetail "Chi tiết gói"
// @Security     Bearer
// @Router       /system/domain-packs/{name}/formulas/{id} [delete]
func (h *DomainPackReconcileHandler) ResetDomainPackFormula(c *gin.Context) {
	ctx := c.Request.Context()
	detail, err := h.reconciler.ResetFormula(ctx, c.Param("name"), c.Param("id"))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
}
