package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// DomainPackInfo is one shipped domain pack as the agent editor shows it.
type DomainPackInfo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Version       string   `json:"version"`
	AsOf          string   `json:"as_of"`     // YYYY-MM-DD
	ReviewBy      string   `json:"review_by"` // YYYY-MM-DD
	Overdue       bool     `json:"overdue"`
	RequiredTools []string `json:"required_tools"`
	Domain        string   `json:"domain"`
	Attribution   string   `json:"attribution"`
}

// ListDomainPacks godoc
// @Summary      Lấy danh sách gói chuyên môn
// @Description  Trả về các gói chuyên môn có trong phiên bản này để chọn cho trợ lý suy luận: phiên bản, mốc dữ liệu, hạn rà soát (overdue = đã quá hạn) và công cụ bắt buộc
// @Tags         Trợ lý
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "danh sách gói chuyên môn"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /agents/domain-packs [get]
func (h *CustomAgentHandler) ListDomainPacks(c *gin.Context) {
	now := types.PromptNow()
	list := make([]DomainPackInfo, 0, len(h.domainPacks.Names()))
	for _, name := range h.domainPacks.Names() {
		pack, _ := h.domainPacks.Get(name)
		m := pack.Manifest.Metadata
		required := append([]string{}, m.RequiredTools...)
		list = append(list, DomainPackInfo{
			Name:          name,
			Description:   pack.Manifest.Description,
			Version:       m.Version,
			AsOf:          m.AsOf.Format("2006-01-02"),
			ReviewBy:      m.ReviewBy.Format("2006-01-02"),
			Overdue:       pack.Overdue(now),
			RequiredTools: required,
			Domain:        m.Domain,
			Attribution:   m.Attribution,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// validateDomainPacks refuses a domain_packs selection the agent cannot use:
// a name this build does not ship, a non-smart-reasoning agent, or tools
// the packs require that the agent does not have.
func (h *CustomAgentHandler) validateDomainPacks(cfg types.CustomAgentConfig) error {
	lookup := func(name string) ([]string, bool) {
		pack, ok := h.domainPacks.Get(name)
		if !ok {
			return nil, false
		}
		return pack.Manifest.Metadata.RequiredTools, true
	}
	// the run time reads retired tool names as their successors
	if len(cfg.AllowedTools) > 0 {
		cfg.AllowedTools = tools.NormalizeAllowedTools(cfg.AllowedTools)
	}
	if err := cfg.ValidateDomainPacks(lookup, tools.DefaultAllowedTools(),
		tools.KnowledgeScopedTools(cfg.WebSearchEnabled)); err != nil {
		return errors.NewBadRequestError(err.Error())
	}
	return nil
}
