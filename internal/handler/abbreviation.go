package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// AbbreviationHandler exposes the global Vietnamese abbreviation dictionary
// (AIRAG port). Reads and suggestions are Viewer+; activation, edits and
// deletes are Admin+ — the router attaches the guards.
type AbbreviationHandler struct {
	svc interfaces.AbbreviationService
}

// NewAbbreviationHandler creates the handler.
func NewAbbreviationHandler(svc interfaces.AbbreviationService) *AbbreviationHandler {
	return &AbbreviationHandler{svc: svc}
}

// ListAbbreviations godoc
// @Summary      List abbreviations
// @Description  Lists dictionary entries, optionally filtered by a substring
// @Description  match on short_form/full_form and by active state.
// @Tags         Abbreviations
// @Produce      json
// @Param        search    query  string  false  "Substring filter"
// @Param        is_active query  bool    false  "Filter by active state"
// @Param        page      query  int     false  "Page"
// @Param        page_size query  int     false  "Page size"
// @Success      200       {object} map[string]interface{} "Rows + total"
// @Router       /abbreviations [get]
func (h *AbbreviationHandler) ListAbbreviations(c *gin.Context) {
	ctx := c.Request.Context()
	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		c.Error(errors.NewBadRequestError("分页参数不合法").WithDetails(err.Error()))
		return
	}
	search := c.Query("search")
	var isActive *bool
	if v := c.Query("is_active"); v != "" {
		b := v == "true" || v == "1"
		isActive = &b
	}
	rows, total, err := h.svc.List(ctx, search, isActive, page.GetPage(), page.PageSize)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    rows,
		"total":   total,
		"page":    page.GetPage(),
	})
}

// GetAbbreviation godoc
// @Summary      Get one abbreviation
// @Router       /abbreviations/{id} [get]
func (h *AbbreviationHandler) GetAbbreviation(c *gin.Context) {
	ctx := c.Request.Context()
	id := secutils.SanitizeForLog(c.Param("id"))
	row, err := h.svc.Get(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	if row == nil {
		c.Error(errors.NewNotFoundError("Abbreviation not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

// CreateAbbreviation godoc
// @Summary      Suggest an abbreviation
// @Description  Creates an inactive suggestion; an admin activates it later.
// @Router       /abbreviations [post]
func (h *AbbreviationHandler) CreateAbbreviation(c *gin.Context) {
	ctx := c.Request.Context()
	var req types.AbbreviationCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("请求参数不合法").WithDetails(err.Error()))
		return
	}
	row, err := h.svc.Suggest(ctx, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

// UpdateAbbreviation godoc
// @Summary      Update an abbreviation (admin)
// @Description  Partial update; is_active flips are admin-only by route guard.
// @Router       /abbreviations/{id} [patch]
func (h *AbbreviationHandler) UpdateAbbreviation(c *gin.Context) {
	ctx := c.Request.Context()
	id := secutils.SanitizeForLog(c.Param("id"))
	var req types.AbbreviationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("请求参数不合法").WithDetails(err.Error()))
		return
	}
	row, err := h.svc.Update(ctx, id, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	if row == nil {
		c.Error(errors.NewNotFoundError("Abbreviation not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

// DeleteAbbreviation godoc
// @Summary      Delete an abbreviation (admin)
// @Router       /abbreviations/{id} [delete]
func (h *AbbreviationHandler) DeleteAbbreviation(c *gin.Context) {
	ctx := c.Request.Context()
	id := secutils.SanitizeForLog(c.Param("id"))
	row, err := h.svc.Get(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	if row == nil {
		c.Error(errors.NewNotFoundError("Abbreviation not found"))
		return
	}
	if err := h.svc.Delete(ctx, id); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
