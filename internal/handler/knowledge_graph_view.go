package handler

import (
	stderrors "errors"
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// KnowledgeGraphViewHandler serves the two graphs drawn on a KB page: the
// entity graph extracted from its documents (Neo4j) and the relation graph
// of its catalogued legal documents (legal catalogue, Postgres).
type KnowledgeGraphViewHandler struct {
	legal interfaces.LegalCatalogService
	focus interfaces.LegalGraphFocusService
}

// NewKnowledgeGraphViewHandler creates the handler.
func NewKnowledgeGraphViewHandler(
	legal interfaces.LegalCatalogService,
	focus interfaces.LegalGraphFocusService,
) *KnowledgeGraphViewHandler {
	return &KnowledgeGraphViewHandler{legal: legal, focus: focus}
}

// GetLegalGraph godoc
// @Summary      Legal relation graph of a knowledge base
// @Description  The KB's documents linked to the legal catalogue and their
// @Description  direct neighbours: replacement, repeal, amendment, guidance,
// @Description  consolidation, and "căn cứ" between two KB documents.
// @Tags         Đồ thị tri thức
// @Produce      json
// @Param        id    path  string true  "Knowledge base ID"
// @Param        limit query int    false "Node budget (default 150, max 400)"
// @Success      200 {object} map[string]interface{} "GraphView"
// @Router       /knowledge-bases/{id}/legal-graph [get]
func (h *KnowledgeGraphViewHandler) GetLegalGraph(c *gin.Context) {
	ctx := c.Request.Context()
	limit, _ := strconv.Atoi(c.Query("limit"))
	view, err := h.legal.KBGraph(ctx, c.Param("id"), limit)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"kb_id": c.Param("id")})
		c.Error(errors.NewInternalServerError("failed to read the legal relation graph"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// GetLegalGraphFocus godoc
// @Summary      Focused legal relation graph of a knowledge base
// @Description  One catalogue document in focus: the documents it acts on
// @Description  (direction "out": amends, replaces, repeals, guides, or is
// @Description  based on them) and those acting on it ("in"), at most 60 per
// @Description  direction and relation (neighbour_totals has the counts),
// @Description  plus its provision-level links in both directions (at most
// @Description  600 rows each; *_total and provisions_truncated report the
// @Description  rest). doc must be held by the KB (approved legal link) or a
// @Description  direct neighbour of a held document, else 404; empty, the
// @Description  best-connected held document is focused — or, with q, only
// @Description  the candidates are returned (focus null). q filters the
// @Description  held-document candidates of the picker (number or title,
// @Description  diacritics optional).
// @Tags         Đồ thị tri thức
// @Produce      json
// @Param        id   path  string true  "Knowledge base ID"
// @Param        doc  query string false "Legal document ID (catalogue)"
// @Param        q    query string false "Picker search over the KB's documents"
// @Success      200 {object} map[string]interface{} "LegalGraphFocus"
// @Failure      404 {object} map[string]interface{} "Document not reachable from this KB"
// @Router       /knowledge-bases/{id}/legal-graph/focus [get]
func (h *KnowledgeGraphViewHandler) GetLegalGraphFocus(c *gin.Context) {
	ctx := c.Request.Context()
	if h.focus == nil {
		c.Error(errors.NewNotFoundError("legal graph unavailable"))
		return
	}
	view, err := h.focus.KBFocus(ctx, c.Param("id"), c.Query("doc"), c.Query("q"))
	if stderrors.Is(err, types.ErrLegalGraphFocusNotFound) {
		c.Error(errors.NewNotFoundError("legal document not found in this knowledge base"))
		return
	}
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"kb_id": c.Param("id"), "doc": c.Query("doc")})
		c.Error(errors.NewInternalServerError("failed to read the legal relation graph"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}
