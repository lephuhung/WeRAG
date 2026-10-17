package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// TemplateStyleHandler exposes the font sizes of the NĐ30 templates drafted
// from chat: the platform default (system setting, SuperAdmin) and the
// current workspace's override (its admin) — the router attaches the guards.
type TemplateStyleHandler struct {
	repo     interfaces.TenantTemplateStyleRepository
	settings interfaces.SystemSettingService
}

// NewTemplateStyleHandler creates the handler.
func NewTemplateStyleHandler(repo interfaces.TenantTemplateStyleRepository, settings interfaces.SystemSettingService) *TemplateStyleHandler {
	return &TemplateStyleHandler{repo: repo, settings: settings}
}

// TemplateStyleResponse is everything the editor shows: the components with
// their NĐ30 ranges, each level's saved sizes and the sizes in force.
type TemplateStyleResponse struct {
	Components []types.TemplateFontSizeSpec `json:"components"`
	Platform   types.TemplateFontSizes      `json:"platform"`
	Tenant     types.TemplateFontSizes      `json:"tenant"`
	Effective  types.TemplateFontSizes      `json:"effective"`
}

// TemplateStyleRequest is a save of the workspace's override; a component
// left out follows the platform default.
type TemplateStyleRequest struct {
	FontSizes types.TemplateFontSizes `json:"font_sizes"`
}

func (h *TemplateStyleHandler) tenantSizes(c *gin.Context) (types.TemplateFontSizes, error) {
	s, err := h.repo.Get(c.Request.Context(), c.GetUint64(types.TenantIDContextKey.String()))
	if err != nil || s == nil {
		return types.TemplateFontSizes{}, err
	}
	f, err := types.ParseTemplateFontSizes(s.FontSizes)
	if err != nil {
		// a size outside a later-narrowed range: the workspace follows the
		// platform until its admin saves again
		return types.TemplateFontSizes{}, nil
	}
	return f, nil
}

// GetTemplateStyle returns the template font sizes of the workspace.
// GET /tenant/document-template-style
func (h *TemplateStyleHandler) GetTemplateStyle(c *gin.Context) {
	ctx := c.Request.Context()
	tenant, err := h.tenantSizes(c)
	if err != nil {
		logger.Errorf(ctx, "get template style: %v", err)
		c.Error(errors.NewInternalServerError("Failed to load the template font sizes"))
		return
	}
	platform := types.PlatformTemplateFontSizes(ctx, h.settings)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": TemplateStyleResponse{
		Components: types.TemplateFontSizeSpecs, Platform: platform, Tenant: tenant,
		Effective: types.ResolveTemplateFontSizes(platform, tenant),
	}})
}

// UpdateTemplateStyle saves the workspace's override.
// PUT /tenant/document-template-style
func (h *TemplateStyleHandler) UpdateTemplateStyle(c *gin.Context) {
	ctx := c.Request.Context()
	var req TemplateStyleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid template font sizes"))
		return
	}
	if req.FontSizes == nil {
		req.FontSizes = types.TemplateFontSizes{}
	}
	if err := req.FontSizes.Validate(); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	raw, _ := json.Marshal(req.FontSizes)
	userID, _ := types.UserIDFromContext(ctx)
	if err := h.repo.Save(ctx, &types.TenantTemplateStyle{
		TenantID: c.GetUint64(types.TenantIDContextKey.String()), FontSizes: types.JSON(raw), UpdatedBy: userID,
	}); err != nil {
		logger.Errorf(ctx, "save template style: %v", err)
		c.Error(errors.NewInternalServerError("Failed to save the template font sizes"))
		return
	}
	h.GetTemplateStyle(c)
}
