package handler

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// legalLookupMaxQuery caps the query length (a số hiệu or a title).
const legalLookupMaxQuery = 200

// LegalLookupHandler serves the validity lookup page: members and Guests
// look a legal document up without the chat. It reads the legal catalogue
// only — no LLM, nothing counted against question or token quotas.
type LegalLookupHandler struct {
	svc interfaces.LegalValidityLookupService
}

// NewLegalLookupHandler creates the handler.
func NewLegalLookupHandler(svc interfaces.LegalValidityLookupService) *LegalLookupHandler {
	return &LegalLookupHandler{svc: svc}
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
