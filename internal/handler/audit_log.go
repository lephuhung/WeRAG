package handler

import (
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// AuditLogHandler exposes the per-tenant audit-log feed (PR 6, #1303).
// The route group lives under /tenants/:id/audit-log, gated by
// PathTenantMatch (URL :id == active tenant) plus an Admin role
// requirement — leaks of denied-action histories should not surface
// to ordinary members.
type AuditLogHandler struct {
	auditService interfaces.AuditLogService
}

// NewAuditLogHandler constructs the handler.
func NewAuditLogHandler(auditService interfaces.AuditLogService) *AuditLogHandler {
	return &AuditLogHandler{auditService: auditService}
}

// auditLogListResponse is the response envelope for ListTenantAuditLog. The
// cursor is the integer id of the last entry, or 0 if no more rows remain.
type auditLogListResponse struct {
	Success    bool              `json:"success"`
	Data       []*types.AuditLog `json:"data"`
	NextCursor uint64            `json:"next_cursor"`
}

// ListTenantAuditLog godoc
// @Summary      Lấy nhật ký kiểm toán của đơn vị
// @Description  Trả về các sự kiện kiểm toán gần đây của đơn vị, sắp xếp id giảm dần. Phân trang con trỏ: dùng next_cursor của phản hồi trước làm after_id cho yêu cầu tiếp theo.
// @Tags         Nhật ký kiểm toán
// @Produce      json
// @Param        id        path   string  true   "Tenant workspaceID"
// @Param        after_id  query  int     false  "Con trỏ: trả về bản ghi có id nhỏ hơn giá trị này (mặc định bắt đầu từ mới nhất)"
// @Param        limit     query  int     false  "Kích thước trang, 1-100, mặc định 50"
// @Param        action    query  string  false  "Lọc chính xác theo action (ví dụ rbac.member_added / rbac.access_denied)"
// @Param        outcome   query  string  false  "Lọc chính xác theo outcome (success / denied)"
// @Param        actor     query  string  false  "Lọc chính xác theo actor_user_id"
// @Success      200  {object}  auditLogListResponse
// @Failure      400  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /tenants/{id}/audit-log [get]
func (h *AuditLogHandler) ListTenantAuditLog(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		// parseTenantIDFromPath has already attached an error to gin.
		return
	}

	// after_id cursor — invalid values are tolerated (treated as "from
	// the top") so a misconfigured client doesn't see a hard 400 on
	// the empty / first request. Tighter validation belongs at the
	// frontend.
	var afterID uint64
	if raw := c.Query("after_id"); raw != "" {
		if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
			afterID = v
		}
	}
	limit := 0 // 0 lets the repository pick its default (50)
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}

	q := &interfaces.AuditLogQuery{
		AfterID:      afterID,
		Limit:        limit,
		Action:       types.AuditAction(c.Query("action")),
		Outcome:      types.AuditOutcome(c.Query("outcome")),
		ActorUserID:  c.Query("actor"),
		UnscopedOnly: true,
	}

	entries, err := h.auditService.List(ctx, tenantID, q)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"tenant_id": tenantID})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	// next_cursor is the smallest id in the page (since rows are sorted
	// id DESC). Empty page ⇒ 0, telling the client there's nothing
	// older to fetch.
	var nextCursor uint64
	if n := len(entries); n > 0 {
		nextCursor = entries[n-1].ID
	}

	c.JSON(http.StatusOK, auditLogListResponse{
		Success:    true,
		Data:       entries,
		NextCursor: nextCursor,
	})
}

// ListKnowledgeBaseActivity returns the durable activity projection for one
// knowledge base. The route has already resolved KB access; this handler adds
// an owner-tenant check so organization-shared consumers cannot inspect source
// workspace actors or configuration history.
// @Summary      Lấy lịch sử hoạt động của kho tri thức
// @Description  Trả về các thay đổi quan trọng và tác vụ nền của kho tri thức. Chỉ người tạo kho tri thức hoặc quản trị viên đơn vị sở hữu được đọc; đơn vị được chia sẻ không đọc được.
// @Tags         Kho tri thức
// @Produce      json
// @Param        id        path   string  true   "Knowledge BaseID"
// @Param        after_id  query  int     false  "Con trỏ: trả về bản ghi có id nhỏ hơn giá trị này"
// @Param        limit     query  int     false  "Kích thước trang, 1-100, mặc định 50"
// @Param        action    query  string  false  "Lọc chính xác theo action"
// @Param        outcome   query  string  false  "Lọc chính xác theo outcome"
// @Param        actor     query  string  false  "Lọc chính xác theo actor_user_id"
// @Success      200  {object}  auditLogListResponse
// @Failure      403  {object}  errors.AppError
// @Security     Bearer
// @Router       /knowledge-bases/{id}/activity [get]
func (h *AuditLogHandler) ListKnowledgeBaseActivity(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	access, ok := middleware.KBAccessFromContext(c)
	if !ok || access == nil || access.KnowledgeBase == nil || access.KnowledgeBase.ID != kbID {
		c.Error(errors.NewNotFoundError("knowledge base not found"))
		return
	}
	callerTenantID := c.GetUint64(types.TenantIDContextKey.String())
	if callerTenantID == 0 || access.KnowledgeBase.TenantID != callerTenantID {
		c.Error(errors.NewForbiddenError("knowledge base activity is only available in the owner workspace"))
		return
	}
	actorID, _ := types.UserIDFromContext(ctx)
	role := types.TenantRoleFromContext(ctx)
	if access.KnowledgeBase.CreatorID != actorID && !role.IsTenantAdmin() {
		c.Error(errors.NewForbiddenError("knowledge base activity requires creator or admin access"))
		return
	}

	afterID, limit := parseAuditCursor(c)
	q := &interfaces.AuditLogQuery{
		AfterID:     afterID,
		Limit:       limit,
		Action:      types.AuditAction(c.Query("action")),
		Outcome:     types.AuditOutcome(c.Query("outcome")),
		ActorUserID: c.Query("actor"),
		ScopeType:   "knowledge_base",
		ScopeID:     kbID,
	}
	entries, err := h.auditService.List(ctx, access.KnowledgeBase.TenantID, q)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"knowledge_base_id": kbID})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	var nextCursor uint64
	if n := len(entries); n > 0 {
		nextCursor = entries[n-1].ID
	}
	c.JSON(http.StatusOK, auditLogListResponse{
		Success: true, Data: entries, NextCursor: nextCursor,
	})
}

func parseAuditCursor(c *gin.Context) (uint64, int) {
	var afterID uint64
	if raw := c.Query("after_id"); raw != "" {
		if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
			afterID = v
		}
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	return afterID, limit
}

// ListSystemAuditLog godoc
// @Summary      Lấy nhật ký kiểm toán nền tảng
// @Description  Trả về các sự kiện kiểm toán phạm vi hệ thống (tenant_id=0), gồm các thao tác SystemAdmin như system.setting_changed / system.admin_promoted / system.admin_revoked. Phân trang con trỏ theo id giảm dần.
// @Tags         Nhật ký kiểm toán
// @Produce      json
// @Param        after_id  query  int     false  "Con trỏ: trả về bản ghi có id nhỏ hơn giá trị này (mặc định bắt đầu từ mới nhất)"
// @Param        limit     query  int     false  "Kích thước trang, 1-100, mặc định 50"
// @Param        action    query  string  false  "Lọc chính xác theo action (ví dụ system.setting_changed)"
// @Param        outcome   query  string  false  "Lọc chính xác theo outcome (success / denied)"
// @Param        actor     query  string  false  "Lọc chính xác theo actor_user_id"
// @Success      200  {object}  auditLogListResponse
// @Failure      500  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /system/admin/audit-log [get]
//
// Mounted on /api/v1/system/admin/audit-log under the SystemAdmin()
// guard. Unlike ListTenantAuditLog this route is *not* tenant-scoped:
// the system_settings table, admin promote/revoke, and the
// apply-default-storage-quota bulk write all emit audit rows with
// tenant_id=0 (see audit_log.go for the action constants). Those rows
// would never surface in any tenant's audit feed, so this endpoint
// fixes the "wrote audit, can't see it" gap.
func (h *AuditLogHandler) ListSystemAuditLog(c *gin.Context) {
	ctx := c.Request.Context()

	// Cursor / page-size parsing mirrors ListTenantAuditLog so the
	// frontend can share the same call shape; tolerant of garbage
	// because the empty / first request shouldn't bounce.
	var afterID uint64
	if raw := c.Query("after_id"); raw != "" {
		if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
			afterID = v
		}
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}

	q := &interfaces.AuditLogQuery{
		AfterID:     afterID,
		Limit:       limit,
		Action:      types.AuditAction(c.Query("action")),
		Outcome:     types.AuditOutcome(c.Query("outcome")),
		ActorUserID: c.Query("actor"),
	}

	// tenant_id=0 is the system-scope convention; see
	// AuditActionSystemSettingChanged comment in types/audit_log.go.
	entries, err := h.auditService.List(ctx, 0, q)
	if err != nil {
		logger.Error(ctx, err)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	var nextCursor uint64
	if n := len(entries); n > 0 {
		nextCursor = entries[n-1].ID
	}

	c.JSON(http.StatusOK, auditLogListResponse{
		Success:    true,
		Data:       entries,
		NextCursor: nextCursor,
	})
}
