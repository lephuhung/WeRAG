package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
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
// @Summary      Đối chiếu danh mục của gói chuyên môn
// @Description  Tra từng số hiệu trong catalogue.yaml của gói theo
// @Description  legal_documents.number_key: found (một văn bản, kèm
// @Description  status_note khi danh mục ghi hết hiệu lực hoặc bị thay thế),
// @Description  missing (không có; ghi một yêu cầu bổ sung văn bản nguồn
// @Description  detail.origin = pack:<tên gói>, không trùng yêu cầu đã có) hoặc ambiguous
// @Description  (nhiều văn bản cùng số hiệu). Không sửa legal_documents.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        name path string true "Tên gói chuyên môn"
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
// @Summary      Danh sách gói chuyên môn (SuperAdmin)
// @Description  Các gói chuyên môn có trong bản build: phiên bản, mốc dữ
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
// @Summary      Chi tiết gói chuyên môn (SuperAdmin)
// @Description  Văn bản trích dẫn (tra danh mục chỉ đọc: trạng thái, có
// @Description  trong KB, ghi chú hiệu lực, tệp trích, công thức dùng),
// @Description  công thức (căn cứ ok/warning), agent đang dùng. Không ghi
// @Description  yêu cầu nào; POST …/reconcile mới ghi.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        name path string true "Tên gói chuyên môn"
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
