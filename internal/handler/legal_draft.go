package handler

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// LegalDraftHandler serves the SuperAdmin review of OCR drafts: a document
// vbpl.vn publishes only as a scanned PDF is fetched and OCRed into a draft,
// edited in the embedded editor, checked and approved before it is
// ingested as vbpl-style HTML. The router gates every route on an explicit
// human SuperAdmin.
type LegalDraftHandler struct {
	svc             interfaces.LegalDraftService
	frontendBaseURL string
}

// NewLegalDraftHandler creates the handler.
func NewLegalDraftHandler(svc interfaces.LegalDraftService, cfg *config.Config) *LegalDraftHandler {
	h := &LegalDraftHandler{svc: svc}
	if cfg != nil {
		h.frontendBaseURL = cfg.FrontendBaseURL
	}
	return h
}

func (h *LegalDraftHandler) fail(c *gin.Context, err error) {
	logger.ErrorWithFields(c.Request.Context(), err, nil)
	if _, ok := errors.IsAppError(err); ok {
		c.Error(err)
		return
	}
	c.Error(errors.NewInternalServerError("legal draft request failed").WithDetails(err.Error()))
}

// FetchLegalDraft godoc
// @Summary      Fetch a request's scanned PDF into an OCR draft
// @Description  For a document vbpl.vn has only as a scanned PDF: downloads
// @Description  the PDF and OCRs it into a draft (up to 20 pages while the
// @Description  request waits, longer scans in the background: status "ocr").
// @Description  An existing draft is returned as it is; a failed one is
// @Description  fetched again. 400 when vbpl.vn has the HTML full text (ingest
// @Description  it directly) or no full text at all.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        id path string true "Request ID"
// @Success      200 {object} map[string]interface{} "The draft"
// @Router       /system/legal-requests/{id}/fetch [post]
func (h *LegalDraftHandler) FetchLegalDraft(c *gin.Context) {
	draft, err := h.svc.FetchForRequest(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": draft})
}

// ListLegalDrafts godoc
// @Summary      List OCR drafts
// @Description  status is fetching, ocr, ready, reviewing, approved, failed
// @Description  or all (default); newest change first, without OCR text.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        status    query  string  false  "Draft status or all"
// @Param        page      query  int     false  "Page"
// @Param        page_size query  int     false  "Page size"
// @Success      200 {object} map[string]interface{} "Items and total"
// @Router       /system/legal-drafts [get]
func (h *LegalDraftHandler) ListLegalDrafts(c *gin.Context) {
	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		c.Error(errors.NewBadRequestError("invalid pagination").WithDetails(err.Error()))
		return
	}
	size := page.PageSize
	if size <= 0 {
		size = 50
	}
	items, total, err := h.svc.List(c.Request.Context(), c.DefaultQuery("status", "all"), (page.GetPage()-1)*size, size)
	if err != nil {
		h.fail(c, err)
		return
	}
	if items == nil {
		items = []*types.LegalDraftItem{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total}})
}

// GetLegalDraft godoc
// @Summary      One OCR draft with its raw OCR text and request
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        id path string true "Draft ID"
// @Success      200 {object} map[string]interface{} "The draft"
// @Router       /system/legal-drafts/{id} [get]
func (h *LegalDraftHandler) GetLegalDraft(c *gin.Context) {
	item, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// OpenLegalDraft godoc
// @Summary      Open an OCR draft in the editor
// @Description  The first call builds the draft's .docx and opens it in a
// @Description  hidden document-assistant session of the platform
// @Description  organization owned by the caller; later calls return the same
// @Description  ids. The session routes (/sessions/{session_id}/documents/
// @Description  {document_id}/...) are called with X-Tenant-ID = tenant_id.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        id path string true "Draft ID"
// @Success      200 {object} map[string]interface{} "draft, editor, session_id, document_id, tenant_id, pdf_url"
// @Router       /system/legal-drafts/{id}/open [post]
func (h *LegalDraftHandler) OpenLegalDraft(c *gin.Context) {
	ctx := c.Request.Context()
	viewer := types.LegalDraftViewer{Lang: editorLanguage(c.GetHeader("Accept-Language")), Origin: h.editorOrigin(c)}
	viewer.UserID, _ = types.UserIDFromContext(ctx)
	if user, ok := ctx.Value(types.UserContextKey).(*types.User); ok && user != nil {
		if viewer.UserID == "" {
			viewer.UserID = user.ID
		}
		viewer.UserName = user.Username
	}
	res, err := h.svc.Open(ctx, c.Param("id"), viewer)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// LegalDraftStructure godoc
// @Summary      Check the legal structure of a draft
// @Description  Saves the open editor, reads the draft's current text and
// @Description  reports its Phần/Chương/Mục/Điều tree, articles, khoản/điểm
// @Description  counts and warnings; ready_to_approve is false while a
// @Description  blocking warning remains.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        id path string true "Draft ID"
// @Success      200 {object} map[string]interface{} "Structure report"
// @Router       /system/legal-drafts/{id}/structure [get]
func (h *LegalDraftHandler) LegalDraftStructure(c *gin.Context) {
	report, err := h.svc.Structure(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": report})
}

// ApproveLegalDraftRequest names the public KB a reviewed draft goes to.
type ApproveLegalDraftRequest struct {
	KnowledgeBaseID string `json:"knowledge_base_id"`
}

// ApproveLegalDraft godoc
// @Summary      Approve a reviewed draft and ingest it
// @Description  Converts the draft's current text to vbpl-style HTML (prov-*
// @Description  markup), checks it with the vbpl converter and ingests it into
// @Description  the public knowledge base like a portal page
// @Description  (source_format pdf_reviewed). 400 when the structure check
// @Description  still blocks or the converter reads another article count.
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Param        id      path string                   true "Draft ID"
// @Param        request body ApproveLegalDraftRequest true "Target knowledge base"
// @Success      200 {object} map[string]interface{} "draft, request, result, knowledge_id, articles"
// @Router       /system/legal-drafts/{id}/approve [post]
func (h *LegalDraftHandler) ApproveLegalDraft(c *gin.Context) {
	var req ApproveLegalDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid request").WithDetails(err.Error()))
		return
	}
	res, err := h.svc.Approve(c.Request.Context(), c.Param("id"), req.KnowledgeBaseID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// DiscardLegalDraft godoc
// @Summary      Discard an OCR draft
// @Description  Deletes a draft that was not approved (the PDF stays
// @Description  stored); its requests go back to "requested".
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        id path string true "Draft ID"
// @Success      200 {object} map[string]interface{} "success"
// @Router       /system/legal-drafts/{id} [delete]
func (h *LegalDraftHandler) DiscardLegalDraft(c *gin.Context) {
	if err := h.svc.Discard(c.Request.Context(), c.Param("id")); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// editorOrigin is the origin of the page that embeds the editor (the
// assistant plugin's hostOrigin), as the session document routes pick it.
func (h *LegalDraftHandler) editorOrigin(c *gin.Context) string {
	for _, candidate := range []string{c.GetHeader("Origin"), c.GetHeader("Referer"), h.frontendBaseURL, os.Getenv("FRONTEND_BASE_URL")} {
		u, err := url.Parse(strings.TrimSpace(candidate))
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return ""
}

func editorLanguage(acceptLanguage string) string {
	first := strings.TrimSpace(strings.SplitN(acceptLanguage, ",", 2)[0])
	if strings.HasPrefix(strings.ToLower(first), "en") {
		return "en"
	}
	return "vi"
}
