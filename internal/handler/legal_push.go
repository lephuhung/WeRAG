package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// LegalPushHandler takes legal documents pushed by sources other than
// vbpl.vn (docs/legal-document-feed-spec.md §9) and serves the SuperAdmin
// review of the provision links they carry.
type LegalPushHandler struct {
	svc    *service.LegalPushService
	drafts *service.LegalDraftService
}

// NewLegalPushHandler creates the handler.
func NewLegalPushHandler(svc *service.LegalPushService, drafts *service.LegalDraftService) *LegalPushHandler {
	return &LegalPushHandler{svc: svc, drafts: drafts}
}

// legalPushAllowed: an API key of the platform tenant (the route's API-key
// policy already required full access or the ingest capability), an explicit
// human SuperAdmin, or a human account flagged users.can_upload_legal_documents
// — the narrow permission the external-source feed's operator account holds
// (push only; the server still files every link in the SuperAdmin review queue).
func legalPushAllowed(ctx context.Context) error {
	caller := types.CallerFromContext(ctx)
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		if caller.TenantID != types.PlatformTenantID {
			return errors.NewForbiddenError("only an API key of the platform organization (SuperAdmin) may push legal documents")
		}
		return nil
	}
	if access.IsExplicitHumanSuperAdmin(ctx, caller) {
		return nil
	}
	if access.IsAuthenticatedHuman(ctx, caller) && types.CanUploadLegalDocumentsFromContext(ctx) {
		return nil
	}
	return errors.NewForbiddenError("Forbidden: system administrator required")
}

// PushLegalDocument godoc
// @Summary      Push a legal document from another source
// @Description  Multipart: file (HTML in the "WeRAG legal HTML v1" format:
// @Description  prov-* classes, an id per Điều/khoản/điểm, data-amend marks),
// @Description  metadata (JSON string → string: source, source_url,
// @Description  source_doc_id, format, legal_document_id or doc_number +
// @Description  issue_date/issuer, doc_type; optional signer, field), mode
// @Description  (auto | links | full | catalogue). A file without Điều (no
// @Description  prov-* markup, no marks) is taken as full text only.
// @Description  catalogue adds the catalogue row only and needs no file.
// @Description  Resolves the document in the legal catalogue, adds the full
// @Description  text when no public KB holds it (full), and stores every
// @Description  data-amend mark as a provision link candidate for SuperAdmin
// @Description  review. API key of the platform organization (ingest), a
// @Description  human SuperAdmin, or a human account allowed to upload legal
// @Description  documents.
// @Tags         Rà soát pháp lý
// @Accept       multipart/form-data
// @Produce      json
// @Param        file     formData file   false "HTML file (not needed in mode catalogue)"
// @Param        metadata formData string true  "JSON metadata"
// @Param        mode     formData string false "auto (default), links, full or catalogue"
// @Success      200 {object} map[string]interface{} "Push result"
// @Router       /system/legal-documents/push [post]
func (h *LegalPushHandler) PushLegalDocument(c *gin.Context) {
	ctx := c.Request.Context()
	if err := legalPushAllowed(ctx); err != nil {
		c.Error(err)
		return
	}
	limitUploadBody(c, service.LegalPushMaxBytes)
	mode := c.PostForm("mode")
	var content []byte
	fileName := ""
	fh, err := c.FormFile("file")
	switch {
	case err != nil && strings.EqualFold(strings.TrimSpace(mode), service.LegalPushCatalogue):
		// a catalogue row needs no file
	case err != nil:
		c.Error(errors.NewBadRequestError("file is required"))
		return
	case fh.Size > service.LegalPushMaxBytes:
		c.Error(errors.NewBadRequestError("file is too large"))
		return
	default:
		f, err := fh.Open()
		if err != nil {
			c.Error(errors.NewBadRequestError("cannot read file"))
			return
		}
		content, err = io.ReadAll(io.LimitReader(f, service.LegalPushMaxBytes+1))
		f.Close()
		if err != nil {
			c.Error(errors.NewBadRequestError("cannot read file"))
			return
		}
		fileName = fh.Filename
	}
	meta := map[string]string{}
	if raw := c.PostForm("metadata"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &meta); err != nil {
			c.Error(errors.NewBadRequestError("metadata must be a JSON object of strings").WithDetails(err.Error()))
			return
		}
	}
	caller := types.CallerFromContext(ctx)
	actor := service.LegalFeedActor{TenantID: caller.TenantID, UserID: caller.UserID, Channel: types.ChannelAPI}
	if scope, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		actor.EndpointID, actor.UserID = "apikey:"+strconv.FormatUint(scope.KeyID, 10), ""
	}
	res, err := h.svc.Push(ctx, service.LegalPushInput{
		Content: content, FileName: fileName, Metadata: meta, Mode: mode,
	}, caller, actor)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// PushLegalScan godoc
// @Summary      Push a scanned legal document from another source for OCR review
// @Description  Multipart: file (PDF: the document and its appendices in one
// @Description  file, ≤ 60 MB), metadata (JSON string → string, the push keys:
// @Description  source, source_url, source_doc_id, doc_number, doc_type, issuer,
// @Description  issue_date, title; optional eff_from, eff_status, signer, field).
// @Description  Resolves (or creates as a feed row) the catalogue document like
// @Description  a push in mode full and OCRs the PDF into a draft the SuperAdmin
// @Description  reviews at /platform/admin/legal-drafts/{id}; a document a KB
// @Description  holds is refused (409), an existing draft is returned as is.
// @Tags         Rà soát pháp lý
// @Accept       multipart/form-data
// @Produce      json
// @Param        file     formData file   true "PDF file"
// @Param        metadata formData string true "JSON metadata"
// @Success      200 {object} map[string]interface{} "Draft, catalogue id"
// @Router       /system/legal-documents/push-scan [post]
func (h *LegalPushHandler) PushLegalScan(c *gin.Context) {
	ctx := c.Request.Context()
	if err := legalPushAllowed(ctx); err != nil {
		c.Error(err)
		return
	}
	if h.drafts == nil {
		c.Error(errors.NewServiceUnavailableError("legal drafts are not available"))
		return
	}
	limitUploadBody(c, service.LegalFeedDraftMaxBytes)
	fh, err := c.FormFile("file")
	if err != nil {
		c.Error(errors.NewBadRequestError("file is required"))
		return
	}
	if fh.Size > service.LegalFeedDraftMaxBytes {
		c.Error(errors.NewBadRequestError("file is too large"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.Error(errors.NewBadRequestError("cannot read file"))
		return
	}
	content, err := io.ReadAll(io.LimitReader(f, service.LegalFeedDraftMaxBytes+1))
	f.Close()
	if err != nil {
		c.Error(errors.NewBadRequestError("cannot read file"))
		return
	}
	meta := map[string]string{}
	if raw := c.PostForm("metadata"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &meta); err != nil {
			c.Error(errors.NewBadRequestError("metadata must be a JSON object of strings").WithDetails(err.Error()))
			return
		}
	}
	caller := types.CallerFromContext(ctx)
	actor := service.LegalFeedActor{TenantID: caller.TenantID, UserID: caller.UserID, Channel: types.ChannelAPI}
	if scope, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		actor.EndpointID, actor.UserID = "apikey:"+strconv.FormatUint(scope.KeyID, 10), ""
	}
	res, err := h.drafts.PushFeedDraft(ctx, service.LegalFeedDraftInput{PDF: content, FileName: fh.Filename, Metadata: meta}, actor)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// ListLegalLinkCandidates godoc
// @Summary      List pushed provision link candidates
// @Description  review_status pending (default), approved, rejected, auto or
// @Description  all; match_status, legal_document_id and feed narrow the list.
// @Description  counts holds the total per review status.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        review_status     query string false "pending, approved, rejected, auto or all"
// @Param        match_status      query string false "matched, doc_only, target_missing, unresolved, duplicate"
// @Param        legal_document_id query string false "Catalogue id of the amended document"
// @Param        feed              query string false "Feed name (tvpl)"
// @Param        page              query int    false "Page"
// @Param        page_size         query int    false "Page size"
// @Success      200 {object} map[string]interface{} "Items, total and counts"
// @Router       /system/legal-link-candidates [get]
func (h *LegalPushHandler) ListLegalLinkCandidates(c *gin.Context) {
	ctx := c.Request.Context()
	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		c.Error(errors.NewBadRequestError("invalid pagination").WithDetails(err.Error()))
		return
	}
	size := page.PageSize
	if size <= 0 {
		size = 100
	}
	items, total, counts, err := h.svc.ListCandidates(ctx, service.LegalLinkCandidateFilter{
		ReviewStatus: c.Query("review_status"), MatchStatus: c.Query("match_status"),
		TargetDocID: c.Query("legal_document_id"), Feed: c.Query("feed"),
	}, (page.GetPage()-1)*size, size)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	if items == nil {
		items = []*service.LegalLinkCandidateItem{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total, "counts": counts}})
}

// DecideLegalLinkCandidate godoc
// @Summary      Approve or reject a pushed provision link
// @Description  approve copies the candidate into legal_provision_links
// @Description  (source = its feed); amender_doc_id, source_path and
// @Description  target_path correct it first. reject keeps it as rejected so
// @Description  the feed's next push does not ask again.
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Param        id      path int                        true "Candidate ID"
// @Param        request body service.LegalLinkDecision  true "Decision"
// @Success      200 {object} map[string]interface{} "Updated item"
// @Router       /system/legal-link-candidates/{id} [put]
func (h *LegalPushHandler) DecideLegalLinkCandidate(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.Error(errors.NewBadRequestError("invalid id"))
		return
	}
	var req service.LegalLinkDecision
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid request").WithDetails(err.Error()))
		return
	}
	item, err := h.svc.DecideCandidate(ctx, id, req, types.CallerFromContext(ctx))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// ListLegalLinkCandidateDocuments godoc
// @Summary      Documents with pushed provision link candidates
// @Description  One row per amended document A: its catalogue row, whether a
// @Description  public KB holds its full text, and the candidates per state
// @Description  (pending_approvable: amending document known; pending_blocked:
// @Description  amending document not in the catalogue yet). totals sums the
// @Description  pending counts.
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        feed query string false "Feed name (tvpl)"
// @Success      200 {object} map[string]interface{} "Items and totals"
// @Router       /system/legal-link-candidates/documents [get]
func (h *LegalPushHandler) ListLegalLinkCandidateDocuments(c *gin.Context) {
	ctx := c.Request.Context()
	items, totals, err := h.svc.ListCandidateDocuments(ctx, c.Query("feed"))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "totals": totals}})
}

// GetLegalProvisionText godoc
// @Summary      Wording of one provision of a catalogue document
// @Description  Read from the chunks of the document's public-KB copy. held
// @Description  false: no public KB holds the full text; reason tells why no
// @Description  text is given (unknown_doc, not_held, no_path, appendix,
// @Description  not_found). Text is capped at 4,000 characters (truncated).
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        doc_id query string true  "Catalogue id"
// @Param        path   query string false "Provision path (dieu:5/khoan:2/diem:a)"
// @Success      200 {object} map[string]interface{} "Provision text"
// @Router       /system/legal-link-candidates/provision-text [get]
func (h *LegalPushHandler) GetLegalProvisionText(c *gin.Context) {
	ctx := c.Request.Context()
	res, err := h.svc.ProvisionText(ctx, c.Query("doc_id"), c.Query("path"))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// ListLegalHeldProvisions godoc
// @Summary      Provisions of a catalogue document's held copy
// @Description  The Điều/khoản/điểm paths the chunks of the document's
// @Description  public-KB copy record, sorted; held false when no public KB
// @Description  holds it (the structure is unknown).
// @Tags         Rà soát pháp lý
// @Produce      json
// @Param        doc_id query string true "Catalogue id"
// @Success      200 {object} map[string]interface{} "Provisions"
// @Router       /system/legal-link-candidates/provisions [get]
func (h *LegalPushHandler) ListLegalHeldProvisions(c *gin.Context) {
	ctx := c.Request.Context()
	res, err := h.svc.HeldProvisionList(ctx, c.Query("doc_id"))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// RecheckLegalLinkCandidates godoc
// @Summary      Resolve again the amending documents of pending links
// @Description  Pending candidates whose amending document was not in the
// @Description  catalogue are resolved again by số hiệu (several documents
// @Description  with the number stay unresolved); a fully matched one is
// @Description  approved without a person under the push's own setting.
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Param        request body object false "{target_doc_id}"
// @Success      200 {object} map[string]interface{} "Counts"
// @Router       /system/legal-link-candidates/recheck [post]
func (h *LegalPushHandler) RecheckLegalLinkCandidates(c *gin.Context) {
	ctx := c.Request.Context()
	var req struct {
		TargetDocID string `json:"target_doc_id"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
			c.Error(errors.NewBadRequestError("invalid request").WithDetails(err.Error()))
			return
		}
	}
	res, err := h.svc.RecheckCandidates(ctx, req.TargetDocID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// DecideLegalLinkCandidatesBatch godoc
// @Summary      Approve or reject several pushed provision links
// @Description  Each id is decided like PUT /{id} without corrections; a
// @Description  failing id is reported in failed and does not stop the
// @Description  others. At most 200 ids.
// @Tags         Rà soát pháp lý
// @Accept       json
// @Produce      json
// @Param        request body service.LegalLinkBatchDecision true "Decision"
// @Success      200 {object} map[string]interface{} "Done and failed ids"
// @Router       /system/legal-link-candidates/decide-batch [post]
func (h *LegalPushHandler) DecideLegalLinkCandidatesBatch(c *gin.Context) {
	ctx := c.Request.Context()
	var req service.LegalLinkBatchDecision
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid request").WithDetails(err.Error()))
		return
	}
	res, err := h.svc.DecideCandidates(ctx, req, types.CallerFromContext(ctx))
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}
