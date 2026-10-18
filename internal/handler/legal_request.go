package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// LegalRequestHandler exposes the SuperAdmin queue of requests to add legal
// documents the system lacks (a chat question named a số hiệu no knowledge
// base holds). The router gates every route on an explicit human SuperAdmin.
type LegalRequestHandler struct {
	svc interfaces.LegalRequestService
}

// NewLegalRequestHandler creates the handler.
func NewLegalRequestHandler(svc interfaces.LegalRequestService) *LegalRequestHandler {
	return &LegalRequestHandler{svc: svc}
}

// ListLegalRequests godoc
// @Summary      List legal document requests
// @Description  Pages the requests to add legal documents, most recently
// @Description  requested first. status is requested (default), added,
// @Description  rejected or all; counts holds the total per status.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        status    query  string  false  "requested, added, rejected or all"
// @Param        page      query  int     false  "Page"
// @Param        page_size query  int     false  "Page size"
// @Success      200       {object} map[string]interface{} "Items, total and counts"
// @Router       /system/legal-requests [get]
func (h *LegalRequestHandler) ListLegalRequests(c *gin.Context) {
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
	status := c.DefaultQuery("status", types.LegalRequestRequested)
	items, total, counts, err := h.svc.List(ctx, status, (page.GetPage()-1)*size, size)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	if items == nil {
		items = []*types.LegalRequestItem{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total, "counts": counts}})
}

// DecideLegalRequest godoc
// @Summary      Ingest, reject or close a legal document request
// @Description  action "ingest" fetches the document from vbpl.vn into the
// @Description  public knowledge base knowledge_base_id (same code as the MCP
// @Description  tool legal_ingest_document); allow_pdf lets it use the
// @Description  portal's original PDF scan (OCR) when there is no HTML full
// @Description  text, otherwise such a document answers 400 and its portal
// @Description  availability is stored as detail.portal; "reject" keeps note as the
// @Description  reason users see; "added" records a document added by hand.
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Param        id      path string                      true "Request ID"
// @Param        request body types.LegalRequestDecision  true "Decision"
// @Success      200 {object} map[string]interface{} "Updated item"
// @Router       /system/legal-requests/{id} [put]
func (h *LegalRequestHandler) DecideLegalRequest(c *gin.Context) {
	ctx := c.Request.Context()
	var req types.LegalRequestDecision
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid request").WithDetails(err.Error()))
		return
	}
	item, err := h.svc.Decide(ctx, c.Param("id"), &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// CheckLegalRequestPortal godoc
// @Summary      Check what vbpl.vn offers for a requested document
// @Description  Reads the document's portal page (no download) and stores
// @Description  detail.portal = {checked_at, status, html, pdf_file, exists}
// @Description  on the request: HTML full text, only an original PDF scan,
// @Description  or nothing.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        id path string true "Request ID"
// @Success      200 {object} map[string]interface{} "Updated item"
// @Router       /system/legal-requests/{id}/portal-check [post]
func (h *LegalRequestHandler) CheckLegalRequestPortal(c *gin.Context) {
	ctx := c.Request.Context()
	item, err := h.svc.CheckPortal(ctx, c.Param("id"))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// ListLegalRequestKnowledgeBases godoc
// @Summary      Public knowledge bases a request can be ingested into
// @Tags         Rà soát pháp lý
// @Produce      json
// @Success      200 {object} map[string]interface{} "[{id, name}]"
// @Router       /system/legal-requests/public-knowledge-bases [get]
func (h *LegalRequestHandler) ListLegalRequestKnowledgeBases(c *gin.Context) {
	ctx := c.Request.Context()
	kbs, err := h.svc.PublicKnowledgeBases(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	if kbs == nil {
		kbs = []types.LegalPublicKB{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": kbs})
}
