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
	var result *types.DomainPackReconcileResult
	result, err := h.reconciler.Reconcile(ctx, c.Param("name"))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
