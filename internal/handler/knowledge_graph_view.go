package handler

import (
	"net/http"
	"os"
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
}

// NewKnowledgeGraphViewHandler creates the handler.
func NewKnowledgeGraphViewHandler(
	graph interfaces.RetrieveGraphRepository,
	kbService interfaces.KnowledgeBaseService,
	legal interfaces.LegalCatalogService,
) *KnowledgeGraphViewHandler {
	return &KnowledgeGraphViewHandler{graph: graph, kbService: kbService, legal: legal}
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
	storeOn := strings.EqualFold(os.Getenv("NEO4J_ENABLE"), "true") && h.graph != nil
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
