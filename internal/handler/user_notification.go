package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// UserNotificationHandler serves the signed-in person's in-app
// notifications (/notifications). The person comes from the context only:
// every call reads or changes the caller's own rows, whichever organization
// (or the public portal, for a Guest) the request runs in. API keys are
// refused (the routes are not declared to the API-key gate, and the
// handler checks again).
type UserNotificationHandler struct {
	repo interfaces.UserNotificationRepository
	now  func() time.Time
}

// NewUserNotificationHandler creates the handler.
func NewUserNotificationHandler(repo interfaces.UserNotificationRepository) *UserNotificationHandler {
	return &UserNotificationHandler{repo: repo, now: time.Now}
}

// notificationSeenRequest is the body of POST /notifications/seen.
type notificationSeenRequest struct {
	IDs []string `json:"ids"`
}

// maxSeenIDs caps the ids of one seen call.
const maxSeenIDs = types.UserNotificationListMax

// caller returns the signed-in person, or writes the error.
func (h *UserNotificationHandler) caller(c *gin.Context) (string, bool) {
	ctx := c.Request.Context()
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		c.Error(apperrors.NewForbiddenError("API keys cannot read notifications"))
		return "", false
	}
	userID, _ := types.UserIDFromContext(ctx)
	if strings.TrimSpace(userID) == "" {
		c.Error(apperrors.NewUnauthorizedError("sign in to read notifications"))
		return "", false
	}
	return userID, true
}

// List godoc
// @Summary      My notifications
// @Description  The caller's in-app notifications, newest first. unread=true
// @Description  keeps the unread ones; before (RFC 3339) pages back:
// @Description  pass the previous page's next_before.
// @Tags         Thông báo
// @Produce      json
// @Param        unread  query  bool    false  "only unread"
// @Param        limit   query  int     false  "page size (1-100, default 20)"
// @Param        before  query  string  false  "created before (RFC 3339)"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /notifications [get]
func (h *UserNotificationHandler) List(c *gin.Context) {
	userID, ok := h.caller(c)
	if !ok {
		return
	}
	limit := 20
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > types.UserNotificationListMax {
			c.Error(apperrors.NewBadRequestError("limit must be 1-100"))
			return
		}
		limit = n
	}
	var before *time.Time
	if v := strings.TrimSpace(c.Query("before")); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			c.Error(apperrors.NewBadRequestError("before must be an RFC 3339 time"))
			return
		}
		before = &t
	}
	unread := c.Query("unread") == "true" || c.Query("unread") == "1"
	rows, err := h.repo.List(c.Request.Context(), userID, unread, before, limit)
	if err != nil {
		c.Error(apperrors.NewInternalServerError("failed to list notifications").WithDetails(err.Error()))
		return
	}
	if rows == nil {
		rows = []*types.UserNotification{}
	}
	data := gin.H{"items": rows, "next_before": nil}
	if len(rows) == limit {
		data["next_before"] = rows[len(rows)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// UnreadCount godoc
// @Summary      My unread notification count
// @Description  unread: not read yet; unseen: of those, no toast shown yet.
// @Tags         Thông báo
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /notifications/unread-count [get]
func (h *UserNotificationHandler) UnreadCount(c *gin.Context) {
	userID, ok := h.caller(c)
	if !ok {
		return
	}
	unread, unseen, err := h.repo.CountUnread(c.Request.Context(), userID)
	if err != nil {
		c.Error(apperrors.NewInternalServerError("failed to count notifications").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"unread": unread, "unseen": unseen}})
}

// Seen godoc
// @Summary      Toasts shown
// @Description  Marks the caller's notifications among ids as seen (their
// @Description  toast was shown); other people's ids are ignored.
// @Tags         Thông báo
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /notifications/seen [post]
func (h *UserNotificationHandler) Seen(c *gin.Context) {
	userID, ok := h.caller(c)
	if !ok {
		return
	}
	var req notificationSeenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewBadRequestError("body must be {\"ids\": [...]}"))
		return
	}
	if len(req.IDs) > maxSeenIDs {
		c.Error(apperrors.NewBadRequestError("at most 100 ids"))
		return
	}
	n, err := h.repo.MarkSeen(c.Request.Context(), userID, req.IDs, h.now())
	if err != nil {
		c.Error(apperrors.NewInternalServerError("failed to mark notifications seen").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"updated": n}})
}

// Read godoc
// @Summary      Read one notification
// @Tags         Thông báo
// @Produce      json
// @Param        id  path  string  true  "notification id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /notifications/{id}/read [post]
func (h *UserNotificationHandler) Read(c *gin.Context) {
	userID, ok := h.caller(c)
	if !ok {
		return
	}
	found, err := h.repo.MarkRead(c.Request.Context(), userID, c.Param("id"), h.now())
	if err != nil {
		c.Error(apperrors.NewInternalServerError("failed to mark the notification read").WithDetails(err.Error()))
		return
	}
	if !found {
		c.Error(apperrors.NewNotFoundError("notification not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ReadAll godoc
// @Summary      Read all my notifications
// @Tags         Thông báo
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /notifications/read-all [post]
func (h *UserNotificationHandler) ReadAll(c *gin.Context) {
	userID, ok := h.caller(c)
	if !ok {
		return
	}
	n, err := h.repo.MarkAllRead(c.Request.Context(), userID, h.now())
	if err != nil {
		c.Error(apperrors.NewInternalServerError("failed to mark notifications read").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"updated": n}})
}
