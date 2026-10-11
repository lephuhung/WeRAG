package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// LegalReviewHandler exposes the SuperAdmin review of the legal identity
// (số hiệu, dates, validity) of documents in platform public KBs. The
// router gates every route on an explicit human SuperAdmin.
type LegalReviewHandler struct {
	svc interfaces.LegalReviewService
}

// NewLegalReviewHandler creates the handler.
func NewLegalReviewHandler(svc interfaces.LegalReviewService) *LegalReviewHandler {
	return &LegalReviewHandler{svc: svc}
}

// ListLegalReviews godoc
// @Summary      List legal identity reviews
// @Description  Pages the review queue of public-KB documents. status is
// @Description  pending, approved, rejected, alerts (approved documents a
// @Description  newer one supersedes) or empty for all.
// @Tags         Legal review
// @Produce      json
// @Param        status    query  string  false  "Review status or alerts"
// @Param        page      query  int     false  "Page"
// @Param        page_size query  int     false  "Page size"
// @Success      200       {object} map[string]interface{} "Items + total"
// @Router       /system/legal-reviews [get]
func (h *LegalReviewHandler) ListLegalReviews(c *gin.Context) {
	ctx := c.Request.Context()
	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		c.Error(errors.NewBadRequestError("invalid pagination").WithDetails(err.Error()))
		return
	}
	size := page.PageSize
	if size <= 0 {
		size = 50
	}
	items, total, err := h.svc.List(ctx, c.Query("status"), (page.GetPage()-1)*size, size)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items, "total": total})
}

// ScanLegalReviews godoc
// @Summary      Suggest legal identities for new public-KB documents
// @Description  Reads the header of every parsed public-KB document without
// @Description  a review yet and stores a pending suggestion matched against
// @Description  the legal catalogue.
// @Tags         Legal review
// @Produce      json
// @Success      200 {object} map[string]interface{} "created count"
// @Router       /system/legal-reviews/scan [post]
func (h *LegalReviewHandler) ScanLegalReviews(c *gin.Context) {
	ctx := c.Request.Context()
	created, err := h.svc.Scan(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"created": created}})
}

// DecideLegalReview godoc
// @Summary      Approve or reject a document's legal identity
// @Tags         Legal review
// @Accept       json
// @Produce      json
// @Param        knowledge_id path string                     true "Knowledge ID"
// @Param        request      body types.LegalReviewDecision  true "Decision"
// @Success      200 {object} map[string]interface{} "Updated item"
// @Router       /system/legal-reviews/{knowledge_id} [put]
func (h *LegalReviewHandler) DecideLegalReview(c *gin.Context) {
	ctx := c.Request.Context()
	var req types.LegalReviewDecision
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid request").WithDetails(err.Error()))
		return
	}
	item, err := h.svc.Decide(ctx, c.Param("knowledge_id"), &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// SearchLegalCatalog godoc
// @Summary      Search the legal catalogue
// @Description  Finds catalogue documents by số hiệu or title words so a
// @Description  reviewer can link a document by hand.
// @Tags         Legal review
// @Produce      json
// @Param        q query string true "Number or title words"
// @Success      200 {object} map[string]interface{} "Documents"
// @Router       /system/legal-catalog/search [get]
func (h *LegalReviewHandler) SearchLegalCatalog(c *gin.Context) {
	ctx := c.Request.Context()
	docs, err := h.svc.SearchCatalog(ctx, c.Query("q"), 10)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": docs})
}

// SpellcheckLegalReview godoc
// @Summary      Proofread chunks of a reviewed document
// @Description  Checks Vietnamese spelling of the given chunk texts (as the
// @Description  reviewer has them, saved or not) with the KB's summary
// @Description  model. Nothing is saved; at most 40 chunks per call.
// @Tags         Legal review
// @Accept       json
// @Produce      json
// @Param        knowledge_id path string true "Knowledge ID"
// @Param        request      body object true "{items: [{chunk_id, content}]}"
// @Success      200 {object} map[string]interface{} "Findings"
// @Router       /system/legal-reviews/{knowledge_id}/spellcheck [post]
func (h *LegalReviewHandler) SpellcheckLegalReview(c *gin.Context) {
	ctx := c.Request.Context()
	var req struct {
		Items []types.LegalSpellcheckItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid request").WithDetails(err.Error()))
		return
	}
	findings, err := h.svc.Spellcheck(ctx, c.Param("knowledge_id"), req.Items)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": findings})
}
