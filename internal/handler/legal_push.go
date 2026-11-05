package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

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
	svc *service.LegalPushService
}

// NewLegalPushHandler creates the handler.
func NewLegalPushHandler(svc *service.LegalPushService) *LegalPushHandler {
	return &LegalPushHandler{svc: svc}
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
// @Description  issue_date/issuer, doc_type), mode (auto | links | full).
// @Description  Resolves the document in the legal catalogue, adds the full
// @Description  text when no public KB holds it (full), and stores every
// @Description  data-amend mark as a provision link candidate for SuperAdmin
// @Description  review. API key of the platform organization (ingest) or a
// @Description  human SuperAdmin.
// @Tags         Rà soát pháp lý
// @Accept       multipart/form-data
// @Produce      json
// @Param        file     formData file   true  "HTML file"
// @Param        metadata formData string true  "JSON metadata"
// @Param        mode     formData string false "auto (default), links or full"
// @Success      200 {object} map[string]interface{} "Push result"
// @Router       /system/legal-documents/push [post]
func (h *LegalPushHandler) PushLegalDocument(c *gin.Context) {
	ctx := c.Request.Context()
	if err := legalPushAllowed(ctx); err != nil {
		c.Error(err)
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.Error(errors.NewBadRequestError("file is required"))
		return
	}
	if fh.Size > service.LegalPushMaxBytes {
		c.Error(errors.NewBadRequestError("file is too large"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.Error(errors.NewBadRequestError("cannot read file"))
		return
	}
	content, err := io.ReadAll(io.LimitReader(f, service.LegalPushMaxBytes+1))
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
	res, err := h.svc.Push(ctx, service.LegalPushInput{
		Content: content, FileName: fh.Filename, Metadata: meta, Mode: c.PostForm("mode"),
	}, caller, actor)
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
