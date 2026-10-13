package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// WikiCatalogueHandler exposes the code-generated ("catalogue" mode) wiki:
// a rebuild trigger and its progress. See WikiCatalogueOverviewService.
type WikiCatalogueHandler struct {
	svc       *service.WikiCatalogueOverviewService
	kbService interfaces.KnowledgeBaseService
}

func NewWikiCatalogueHandler(svc *service.WikiCatalogueOverviewService, kbService interfaces.KnowledgeBaseService) *WikiCatalogueHandler {
	return &WikiCatalogueHandler{svc: svc, kbService: kbService}
}

// Rebuild godoc
// @Summary      Rebuild the catalogue-generated wiki
// @Description  Regenerates every overview page of a catalogue-mode wiki from the legal catalogue, in the background. No LLM call is made.
// @Tags         Wiki
// @Produce      json
// @Param        kb_id  path  string  true  "Knowledge base ID"
// @Success      202  {object}  service.WikiCatalogueBuildStatus
// @Security     Bearer
// @Router       /knowledgebase/{kb_id}/wiki/catalogue/rebuild [post]
func (h *WikiCatalogueHandler) Rebuild(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("kb_id")
	st, err := h.svc.StartRebuild(ctx, kbID)
	switch {
	case errors.Is(err, service.ErrWikiCatalogueBuildRunning):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "data": h.svc.Status(kbID)})
		return
	case errors.Is(err, service.ErrWikiCatalogueModeRequired):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case err != nil:
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"knowledge_base_id": kbID})
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": st})
}

// Status godoc
// @Summary      Catalogue wiki rebuild status
// @Tags         Wiki
// @Produce      json
// @Param        kb_id  path  string  true  "Knowledge base ID"
// @Success      200  {object}  service.WikiCatalogueBuildStatus
// @Security     Bearer
// @Router       /knowledgebase/{kb_id}/wiki/catalogue/status [get]
func (h *WikiCatalogueHandler) Status(c *gin.Context) {
	kbID := c.Param("kb_id")
	st := h.svc.Status(kbID)
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": st})
}
