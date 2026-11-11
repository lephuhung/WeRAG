package handler

import (
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AnswerQualityHandler serves the admins' answer-quality inbox: answers
// users disliked and questions answered without evidence. The same
// handlers serve two route sets: /quality (a Tenant Admin, own
// organization) and /system/quality (SuperAdmin console, every
// organization, ?tenant_id= narrows). The service decides the scope from
// the caller; API keys are refused (the routes are declared to no API-key
// policy, and the service checks again).
type AnswerQualityHandler struct {
	svc interfaces.AnswerQualityService
}

// NewAnswerQualityHandler creates the handler.
func NewAnswerQualityHandler(svc interfaces.AnswerQualityService) *AnswerQualityHandler {
	return &AnswerQualityHandler{svc: svc}
}

// answerQualityDateLayout is the date of the from/to filters (Vietnam
// time, the process's time.Local).
const answerQualityDateLayout = "2006-01-02"

// parseAnswerQualityTime reads a from/to value: a date (the day's start;
// for `to` the next day's start, so the day is included) or RFC 3339.
func parseAnswerQualityTime(v string, endOfDay bool) (*time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	if t, err := time.ParseInLocation(answerQualityDateLayout, v, time.Local); err == nil {
		if endOfDay {
			t = t.AddDate(0, 0, 1)
		}
		return &t, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// filter reads the list filters of the query string. tenant_id is read
// only on the console (platform); the service ignores it otherwise.
func (h *AnswerQualityHandler) filter(c *gin.Context, platform bool) (types.AnswerQualityFilter, bool) {
	var f types.AnswerQualityFilter
	var err error
	if f.From, err = parseAnswerQualityTime(c.Query("from"), false); err != nil {
		c.Error(errors.NewBadRequestError("from must be YYYY-MM-DD or RFC 3339"))
		return f, false
	}
	if f.To, err = parseAnswerQualityTime(c.Query("to"), true); err != nil {
		c.Error(errors.NewBadRequestError("to must be YYYY-MM-DD or RFC 3339"))
		return f, false
	}
	if platform {
		if v := strings.TrimSpace(c.Query("tenant_id")); v != "" {
			id, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				c.Error(errors.NewBadRequestError("invalid tenant_id"))
				return f, false
			}
			f.TenantID = id
		}
	}
	f.AgentID = strings.TrimSpace(c.Query("agent_id"))
	if len(f.AgentID) > 36 {
		c.Error(errors.NewBadRequestError("invalid agent_id"))
		return f, false
	}
	f.HasComment = c.Query("has_comment") == "true"
	switch reason := c.Query("reason"); reason {
	case "", types.UngroundedNoEvidence, types.UngroundedLegalDocumentMissing:
		f.Reason = reason
	default:
		c.Error(errors.NewBadRequestError("unknown reason"))
		return f, false
	}
	switch c.Query("reviewed") {
	case "true":
		v := true
		f.Reviewed = &v
	case "false":
		v := false
		f.Reviewed = &v
	}
	page, size := 1, 20
	if v := c.Query("page"); v != "" {
		if page, err = strconv.Atoi(v); err != nil || page < 1 {
			c.Error(errors.NewBadRequestError("invalid page"))
			return f, false
		}
	}
	if v := c.Query("page_size"); v != "" {
		if size, err = strconv.Atoi(v); err != nil || size < 1 || size > types.AnswerQualityListMax {
			c.Error(errors.NewBadRequestError("page_size must be 1-100"))
			return f, false
		}
	}
	f.Offset, f.Limit = (page-1)*size, size
	return f, true
}

// fail writes err: an AppError (forbidden, not found, bad request) as is,
// anything else as a 500.
func (h *AnswerQualityHandler) fail(c *gin.Context, err error, what string) {
	var appErr *errors.AppError
	if stderrors.As(err, &appErr) {
		c.Error(appErr)
		return
	}
	logger.ErrorWithFields(c.Request.Context(), err, nil)
	c.Error(errors.NewInternalServerError(what + " failed"))
}

func (h *AnswerQualityHandler) listDisliked(c *gin.Context, platform bool) {
	f, ok := h.filter(c, platform)
	if !ok {
		return
	}
	page, err := h.svc.ListDisliked(c.Request.Context(), platform, f)
	if err != nil {
		h.fail(c, err, "listing disliked answers")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}

func (h *AnswerQualityHandler) listUngrounded(c *gin.Context, platform bool) {
	f, ok := h.filter(c, platform)
	if !ok {
		return
	}
	page, err := h.svc.ListUngrounded(c.Request.Context(), platform, f)
	if err != nil {
		h.fail(c, err, "listing ungrounded questions")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}

func (h *AnswerQualityHandler) groupUngrounded(c *gin.Context, platform bool) {
	f, ok := h.filter(c, platform)
	if !ok {
		return
	}
	page, err := h.svc.GroupUngrounded(c.Request.Context(), platform, f)
	if err != nil {
		h.fail(c, err, "grouping ungrounded questions")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}

func (h *AnswerQualityHandler) review(c *gin.Context, platform bool) {
	var in types.AnswerQualityReviewInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.Error(errors.NewBadRequestError("invalid body"))
		return
	}
	rv, err := h.svc.Review(c.Request.Context(), platform, c.Param("message_id"), in)
	if err != nil {
		h.fail(c, err, "saving the review")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rv})
}

// ListDisliked godoc
// @Summary      Disliked answers of my organization
// @Description  Tenant Admin. Answers voted down, newest vote first, with
// @Description  question, comment, agent and the admin's review.
// @Tags         Chất lượng trả lời
// @Produce      json
// @Param        from        query string false "YYYY-MM-DD (Vietnam time) or RFC 3339"
// @Param        to          query string false "YYYY-MM-DD (inclusive) or RFC 3339"
// @Param        agent_id    query string false "agent"
// @Param        has_comment query bool   false "only answers with a comment"
// @Param        reviewed    query bool   false "reviewed or not"
// @Param        page        query int    false "page"
// @Param        page_size   query int    false "1-100"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /quality/disliked [get]
func (h *AnswerQualityHandler) ListDisliked(c *gin.Context) { h.listDisliked(c, false) }

// ListUngrounded godoc
// @Summary      Ungrounded questions of my organization
// @Description  Tenant Admin. Turns a gate answered without evidence
// @Description  (no_evidence, legal_document_missing), newest first.
// @Tags         Chất lượng trả lời
// @Produce      json
// @Param        from      query string false "YYYY-MM-DD or RFC 3339"
// @Param        to        query string false "YYYY-MM-DD (inclusive) or RFC 3339"
// @Param        agent_id  query string false "agent"
// @Param        reason    query string false "no_evidence or legal_document_missing"
// @Param        reviewed  query bool   false "reviewed or not"
// @Param        page      query int    false "page"
// @Param        page_size query int    false "1-100"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /quality/ungrounded [get]
func (h *AnswerQualityHandler) ListUngrounded(c *gin.Context) { h.listUngrounded(c, false) }

// GroupUngrounded godoc
// @Summary      Most frequent ungrounded questions of my organization
// @Description  Tenant Admin. Ungrounded questions of the period grouped by
// @Description  normalized text, most frequent first.
// @Tags         Chất lượng trả lời
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /quality/ungrounded/groups [get]
func (h *AnswerQualityHandler) GroupUngrounded(c *gin.Context) { h.groupUngrounded(c, false) }

// Review godoc
// @Summary      Tag / mark reviewed a row of my organization
// @Description  Tenant Admin. Body: kind (disliked|ungrounded), cause,
// @Description  note, reviewed. The user's message is not changed.
// @Tags         Chất lượng trả lời
// @Accept       json
// @Produce      json
// @Param        message_id path string true "assistant message id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /quality/reviews/{message_id} [put]
func (h *AnswerQualityHandler) Review(c *gin.Context) { h.review(c, false) }

// PlatformListDisliked godoc
// @Summary      Disliked answers (all organizations)
// @Description  SuperAdmin. As /quality/disliked; tenant_id narrows to one organization.
// @Tags         Chất lượng trả lời
// @Produce      json
// @Param        tenant_id query int false "organization"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/quality/disliked [get]
func (h *AnswerQualityHandler) PlatformListDisliked(c *gin.Context) { h.listDisliked(c, true) }

// PlatformListUngrounded godoc
// @Summary      Ungrounded questions (all organizations)
// @Description  SuperAdmin. As /quality/ungrounded; tenant_id narrows to one organization.
// @Tags         Chất lượng trả lời
// @Produce      json
// @Param        tenant_id query int false "organization"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/quality/ungrounded [get]
func (h *AnswerQualityHandler) PlatformListUngrounded(c *gin.Context) { h.listUngrounded(c, true) }

// PlatformGroupUngrounded godoc
// @Summary      Most frequent ungrounded questions (all organizations)
// @Tags         Chất lượng trả lời
// @Produce      json
// @Param        tenant_id query int false "organization"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/quality/ungrounded/groups [get]
func (h *AnswerQualityHandler) PlatformGroupUngrounded(c *gin.Context) { h.groupUngrounded(c, true) }

// PlatformReview godoc
// @Summary      Tag / mark reviewed a row (any organization)
// @Tags         Chất lượng trả lời
// @Accept       json
// @Produce      json
// @Param        message_id path string true "assistant message id"
// @Success      200 {object} map[string]interface{}
// @Security     Bearer
// @Router       /system/quality/reviews/{message_id} [put]
func (h *AnswerQualityHandler) PlatformReview(c *gin.Context) { h.review(c, true) }
