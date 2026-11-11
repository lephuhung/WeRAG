package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// documentLabelTenants is the part of the tenant repository the label
// handler needs (organization names).
type documentLabelTenants interface {
	GetTenantsByIDs(ctx context.Context, ids []uint64) (map[uint64]*types.Tenant, error)
}

// DocumentLabelHandler shows and exports the labels given on the document
// assistant's spelling underlines and NĐ30 format check reports, as
// training data. A Tenant Admin sees their own organization's labels, the
// SuperAdmin every organization's (tenant_id filters). Labels include the
// user's paragraph text, so API keys are refused (route and handler).
type DocumentLabelHandler struct {
	repo    interfaces.DocumentLabelRepository
	tenants documentLabelTenants
}

// NewDocumentLabelHandler creates the handler.
func NewDocumentLabelHandler(repo interfaces.DocumentLabelRepository, tenants interfaces.TenantRepository) *DocumentLabelHandler {
	return &DocumentLabelHandler{repo: repo, tenants: tenants}
}

// labelScope is the organization the caller may read labels of: the
// requested one (0 = all) for the SuperAdmin, their own for a Tenant Admin.
func labelScope(ctx context.Context, requested string) (uint64, error) {
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return 0, errors.NewForbiddenError("API keys cannot read document labels")
	}
	var want uint64
	if requested = strings.TrimSpace(requested); requested != "" {
		v, err := strconv.ParseUint(requested, 10, 64)
		if err != nil {
			return 0, errors.NewBadRequestError("invalid tenant_id")
		}
		want = v
	}
	if isExplicitHumanSuperAdminCaller(ctx) {
		return want, nil
	}
	caller := types.CallerFromContext(ctx)
	if !caller.Role.IsTenantAdmin() || caller.TenantID == 0 {
		return 0, errors.NewForbiddenError("organization admin required")
	}
	if want != 0 && want != caller.TenantID {
		return 0, errors.NewForbiddenError("an organization admin reads only their own organization's labels")
	}
	return caller.TenantID, nil
}

// labelQuery reads the filters: kind, verdict, source, model, from and to
// (YYYY-MM-DD in Vietnam time, both inclusive) and the organization.
func labelQuery(c *gin.Context) (*types.DocumentLabelQuery, error) {
	tenantID, err := labelScope(c.Request.Context(), c.Query("tenant_id"))
	if err != nil {
		return nil, err
	}
	q := &types.DocumentLabelQuery{
		TenantID: tenantID,
		Kind:     strings.TrimSpace(c.Query("kind")),
		Verdict:  strings.TrimSpace(c.Query("verdict")),
		Source:   strings.TrimSpace(c.Query("source")),
		Model:    strings.TrimSpace(c.Query("model")),
	}
	switch q.Kind {
	case "", types.DocumentLabelSpelling, types.DocumentLabelFormat:
	default:
		return nil, errors.NewBadRequestError("kind must be spelling or format")
	}
	switch q.Source {
	case "", types.SpellingFeedbackSourceUser, types.SpellingFeedbackSourceVerifier:
	default:
		return nil, errors.NewBadRequestError("source must be user or verifier")
	}
	if len(q.Verdict) > 20 || len(q.Model) > 255 {
		return nil, errors.NewBadRequestError("filter too long")
	}
	day := func(name string) (*time.Time, error) {
		v := strings.TrimSpace(c.Query(name))
		if v == "" {
			return nil, nil
		}
		t, err := time.ParseInLocation("2006-01-02", v, types.UsageLocation())
		if err != nil {
			return nil, errors.NewBadRequestError(name + " must be YYYY-MM-DD")
		}
		return &t, nil
	}
	if q.From, err = day("from"); err != nil {
		return nil, err
	}
	if q.To, err = day("to"); err != nil {
		return nil, err
	}
	if q.To != nil {
		end := q.To.AddDate(0, 0, 1)
		q.To = &end
	}
	return q, nil
}

// names fills OrganizationName from a cache, looking up the ids it lacks.
func (h *DocumentLabelHandler) names(ctx context.Context, cache map[uint64]string, labels []types.DocumentLabel) {
	var missing []uint64
	for _, l := range labels {
		if _, ok := cache[l.OrganizationID]; !ok {
			cache[l.OrganizationID] = ""
			missing = append(missing, l.OrganizationID)
		}
	}
	if len(missing) > 0 && h.tenants != nil {
		if found, err := h.tenants.GetTenantsByIDs(ctx, missing); err == nil {
			for id, t := range found {
				if t != nil {
					cache[id] = t.Name
				}
			}
		} else {
			logger.Warnf(ctx, "document labels: organization names unavailable: %v", err)
		}
	}
	for i := range labels {
		labels[i].OrganizationName = cache[labels[i].OrganizationID]
	}
}

// ListDocumentLabels godoc
// @Summary      List spelling and format labels
// @Description  Labels given on the document assistant's spelling underlines
// @Description  ("not a mistake" from users or the verifier model) and NĐ30
// @Description  format check reports (users' "báo sai" and other verdicts),
// @Description  newest first, with the paragraph and the flagged span (rune
// @Description  offsets). A Tenant Admin sees their own organization, the
// @Description  SuperAdmin all of them or tenant_id. API keys are refused.
// @Tags         Nhãn tài liệu
// @Produce      json
// @Param        kind      query string false "spelling or format"
// @Param        verdict   query string false "not_error, false_alarm, not_nd30, wrong_type, other"
// @Param        source    query string false "user or verifier"
// @Param        model     query string false "Model name"
// @Param        tenant_id query int    false "Organization (SuperAdmin)"
// @Param        from      query string false "YYYY-MM-DD (Vietnam time)"
// @Param        to        query string false "YYYY-MM-DD, inclusive"
// @Param        page      query int    false "Page"
// @Param        page_size query int    false "Page size (max 200)"
// @Success      200 {object} map[string]interface{} "Items and total"
// @Router       /document-labels [get]
func (h *DocumentLabelHandler) ListDocumentLabels(c *gin.Context) {
	ctx := c.Request.Context()
	q, err := labelQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		c.Error(errors.NewBadRequestError("invalid pagination"))
		return
	}
	size := page.PageSize
	if size <= 0 || size > 200 {
		size = 50
	}
	q.Offset, q.Limit = (page.GetPage()-1)*size, size
	items, total, err := h.repo.List(ctx, q)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("could not list labels"))
		return
	}
	h.names(ctx, map[uint64]string{}, items)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items": items, "total": total, "page": page.GetPage(), "page_size": size,
	}})
}

// DocumentLabelFacets godoc
// @Summary      Filter values for the label table
// @Description  The model names stored on the labels in the caller's scope.
// @Tags         Nhãn tài liệu
// @Produce      json
// @Param        tenant_id query int false "Organization (SuperAdmin)"
// @Success      200 {object} map[string]interface{} "models"
// @Router       /document-labels/facets [get]
func (h *DocumentLabelHandler) DocumentLabelFacets(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, err := labelScope(ctx, c.Query("tenant_id"))
	if err != nil {
		c.Error(err)
		return
	}
	models, err := h.repo.Models(ctx, tenantID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("could not list models"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"models": models}})
}

// ExportDocumentLabels godoc
// @Summary      Export spelling and format labels as JSONL
// @Description  Streams every label matching the filters, one JSON object per
// @Description  line: id, kind, organization_id, created_at, paragraph,
// @Description  span_start, span_end (rune offsets, end exclusive), flagged_text,
// @Description  suggestion, verdict, source, check_id, model and the format
// @Description  finding's details. Same scope and filters as the list.
// @Tags         Nhãn tài liệu
// @Produce      application/x-ndjson
// @Router       /document-labels/export [get]
func (h *DocumentLabelHandler) ExportDocumentLabels(c *gin.Context) {
	ctx := c.Request.Context()
	q, err := labelQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	name := fmt.Sprintf("nhan-chinh-ta-the-thuc-%s.jsonl", time.Now().In(types.UsageLocation()).Format("20060102-1504"))
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	cache := map[uint64]string{}
	n := 0
	err = h.repo.Each(ctx, q, func(l *types.DocumentLabel) error {
		one := []types.DocumentLabel{*l}
		h.names(ctx, cache, one)
		if err := enc.Encode(&one[0]); err != nil {
			return err
		}
		n++
		if n%200 == 0 {
			c.Writer.Flush()
		}
		return ctx.Err()
	})
	c.Writer.Flush()
	uid, _ := types.UserIDFromContext(ctx)
	if err != nil {
		// the status line is gone: the file just ends early
		logger.Errorf(ctx, "document labels export by %s (organization %d) stopped after %d rows: %v", uid, q.TenantID, n, err)
		return
	}
	logger.Infof(ctx, "document labels exported by %s: organization %d, %d rows", uid, q.TenantID, n)
}
