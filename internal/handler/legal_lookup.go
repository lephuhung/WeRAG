package handler

import (
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// legalLookupMaxQuery caps the query length (a số hiệu or a title).
const legalLookupMaxQuery = 200

// LegalLookupHandler serves the validity lookup page: members and Guests
// look a legal document up without the chat. It reads the legal catalogue
// only — no LLM, nothing counted against question or token quotas.
// It also serves the SuperAdmin list of lookups that found nothing.
type LegalLookupHandler struct {
	svc     interfaces.LegalValidityLookupService
	misses  interfaces.LegalLookupMissService
	explore interfaces.LegalGraphExploreService
}

// NewLegalLookupHandler creates the handler.
func NewLegalLookupHandler(
	svc interfaces.LegalValidityLookupService, misses interfaces.LegalLookupMissService,
	explore interfaces.LegalGraphExploreService,
) *LegalLookupHandler {
	return &LegalLookupHandler{svc: svc, misses: misses, explore: explore}
}

// ExploreLegalGraph godoc
// @Summary      Catalogue map around one legal document
// @Description  The document and the documents around it to depth steps
// @Description  (1–3, default 2) over the whole legal catalogue: on one side
// @Description  what acts on it and what acts on those (the decrees guiding
// @Description  a law, the circulars guiding the decrees), on the other what
// @Description  it acts on. Every relation between two reached documents is
// @Description  returned. Open to members and guests like /legal-lookup.
// @Tags         Tra cứu hiệu lực
// @Produce      json
// @Param        doc   query string true  "Catalogue document id"
// @Param        depth query int    false "Steps per side (1–3, default 2)"
// @Success      200 {object} map[string]interface{} "LegalGraphExplore"
// @Failure      404 {object} map[string]interface{} "No such document"
// @Router       /legal-graph/explore [get]
func (h *LegalLookupHandler) ExploreLegalGraph(c *gin.Context) {
	ctx := c.Request.Context()
	doc := strings.TrimSpace(c.Query("doc"))
	if doc == "" || len(doc) > 64 {
		c.Error(errors.NewBadRequestError("doc is required"))
		return
	}
	if h.explore == nil {
		c.Error(errors.NewServiceUnavailableError("legal catalogue map is not available"))
		return
	}
	depth, _ := strconv.Atoi(c.Query("depth"))
	res, err := h.explore.Explore(ctx, doc, depth)
	if err != nil {
		if stderrors.Is(err, types.ErrLegalGraphFocusNotFound) {
			c.Error(errors.NewNotFoundError("legal document not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"doc": doc})
		c.Error(errors.NewInternalServerError("legal catalogue map failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// LookupLegalValidity godoc
// @Summary      Look up the validity of a legal document
// @Description  Finds catalogue documents by số hiệu (every row of a shared
// @Description  number), bare number or title words, at most 10, each with
// @Description  its status, dates and the documents replacing, ending or
// @Description  amending it, plus the catalogue's snapshot date. Open to
// @Description  every signed-in person; no usage quota is consumed.
// @Tags         Tra cứu hiệu lực
// @Produce      json
// @Param        q query string true "Số hiệu or title words (max 200 characters)"
// @Success      200 {object} map[string]interface{} "types.LegalValidityLookup"
// @Failure      400 {object} errors.AppError
// @Failure      429 {object} errors.AppError
// @Router       /legal-lookup [get]
func (h *LegalLookupHandler) LookupLegalValidity(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.Error(errors.NewBadRequestError("q is required"))
		return
	}
	if utf8.RuneCountInString(q) > legalLookupMaxQuery {
		c.Error(errors.NewBadRequestError("q is too long"))
		return
	}
	res, err := h.svc.LookupValidity(ctx, q, 0)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("legal catalogue lookup failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// SuggestLegalDocuments godoc
// @Summary      Documents a search box's text may mean
// @Description  Type-ahead for the validity lookup: at most 8 catalogue
// @Description  documents matched like /legal-lookup (số hiệu, bare number
// @Description  with a type word, title words), without validity details.
// @Description  Nothing is recorded as a miss.
// @Tags         Tra cứu hiệu lực
// @Produce      json
// @Param        q query string true "Text typed so far"
// @Success      200 {object} map[string]interface{} "LegalValidityRef list"
// @Router       /legal-lookup/suggest [get]
func (h *LegalLookupHandler) SuggestLegalDocuments(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	if q == "" || utf8.RuneCountInString(q) > legalLookupMaxQuery {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []types.LegalValidityRef{}})
		return
	}
	res, err := h.svc.Suggest(ctx, q, 0)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("legal catalogue lookup failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// ListLegalLookupMisses godoc
// @Summary      List validity lookups that found nothing
// @Description  Catalogue gaps: normalized queries of /legal-lookup that
// @Description  matched nothing, or only documents mentioning a số hiệu the
// @Description  catalogue lacks, with counts. sort=count (default) or recent.
// @Tags         Tra cứu hiệu lực
// @Produce      json
// @Param        sort      query string false "count or recent"
// @Param        page      query int    false "Page"
// @Param        page_size query int    false "Page size (max 100)"
// @Success      200 {object} map[string]interface{} "Items + total"
// @Router       /system/legal-lookup-misses [get]
func (h *LegalLookupHandler) ListLegalLookupMisses(c *gin.Context) {
	ctx := c.Request.Context()
	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		c.Error(errors.NewBadRequestError("invalid pagination").WithDetails(err.Error()))
		return
	}
	size := page.PageSize
	if size <= 0 || size > 100 {
		size = 20
	}
	items, total, err := h.misses.ListMisses(ctx, c.Query("sort") != "recent", (page.GetPage()-1)*size, size)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("listing lookup misses failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items, "total": total})
}

// DeleteLegalLookupMiss godoc
// @Summary      Dismiss a validity lookup miss
// @Tags         Tra cứu hiệu lực
// @Param        id path int true "Miss ID"
// @Success      200 {object} map[string]interface{} "Deleted"
// @Router       /system/legal-lookup-misses/{id} [delete]
func (h *LegalLookupHandler) DeleteLegalLookupMiss(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.Error(errors.NewBadRequestError("invalid id"))
		return
	}
	if err := h.misses.DeleteMiss(ctx, id); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("deleting the lookup miss failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
