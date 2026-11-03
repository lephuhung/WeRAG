package handler

import (
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"

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
	graph     interfaces.RetrieveGraphRepository
	kbService interfaces.KnowledgeBaseService
	legal     interfaces.LegalCatalogService
	focus     interfaces.LegalGraphFocusService
}

// NewKnowledgeGraphViewHandler creates the handler.
func NewKnowledgeGraphViewHandler(
	graph interfaces.RetrieveGraphRepository,
	kbService interfaces.KnowledgeBaseService,
	legal interfaces.LegalCatalogService,
	focus interfaces.LegalGraphFocusService,
) *KnowledgeGraphViewHandler {
	return &KnowledgeGraphViewHandler{graph: graph, kbService: kbService, legal: legal, focus: focus}
}

// GetEntityGraph godoc
// @Summary      Entity graph of a knowledge base
// @Description  The best-connected entities extracted from the KB's documents
// @Description  and the relations among them. q filters by name (adding the
// @Description  matches' neighbours), types by entity type (comma-separated:
// @Description  Document, Article, Organization, Person, Task, Location),
// @Description  knowledge_id by document.
// @Tags         Đồ thị tri thức
// @Produce      json
// @Param        id           path   string true  "Knowledge base ID"
// @Param        q            query  string false "Name words"
// @Param        types        query  string false "Entity types"
// @Param        knowledge_id query  string false "Only this document"
// @Param        limit        query  int    false "Node budget (default 150, max 500)"
// @Success      200 {object} map[string]interface{} "GraphView + graph_enabled + graph_store"
// @Router       /knowledge-bases/{id}/entity-graph [get]
func (h *KnowledgeGraphViewHandler) GetEntityGraph(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	kb, err := h.kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		c.Error(err)
		return
	}
	storeOn := h.graph != nil
	resp := gin.H{"success": true, "graph_enabled": kb.IsGraphEnabled(), "graph_store": storeOn}
	if !storeOn {
		resp["data"] = &types.GraphView{Nodes: []*types.GraphViewNode{}, Edges: []*types.GraphViewEdge{}}
		c.JSON(http.StatusOK, resp)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	var entityTypes []string
	for _, t := range strings.Split(c.Query("types"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			entityTypes = append(entityTypes, t)
		}
	}
	view, err := h.graph.ViewGraph(ctx, types.NameSpace{KnowledgeBase: kbID}, types.GraphViewFilter{
		Query: strings.TrimSpace(c.Query("q")), Types: entityTypes,
		KnowledgeID: strings.TrimSpace(c.Query("knowledge_id")), Limit: limit,
	})
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"kb_id": kbID})
		c.Error(errors.NewInternalServerError("failed to read the knowledge graph"))
		return
	}
	resp["data"] = view
	c.JSON(http.StatusOK, resp)
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
// @Description  best-connected held document is focused. q filters the
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
