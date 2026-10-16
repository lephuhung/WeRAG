package handler

import (
	stderrors "errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// AgencyProfileHandler exposes the current workspace's issuing agency
// (types.TenantAgencyProfile): every member reads it, the workspace's
// admin or a SuperAdmin edits it — the router attaches the guards.
type AgencyProfileHandler struct {
	repo interfaces.TenantAgencyProfileRepository
}

// NewAgencyProfileHandler creates the handler.
func NewAgencyProfileHandler(repo interfaces.TenantAgencyProfileRepository) *AgencyProfileHandler {
	return &AgencyProfileHandler{repo: repo}
}

// AgencyProfileRequest is an edit of the profile. Version is the version
// the editor read (0: none saved yet); a newer save answers 409.
type AgencyProfileRequest struct {
	ParentAgency      string   `json:"parent_agency"`
	AgencyName        string   `json:"agency_name"`
	AgencyAbbr        string   `json:"agency_abbr"`
	Locality          string   `json:"locality"`
	HeadTitle         string   `json:"head_title"`
	CollectiveName    string   `json:"collective_name"`
	DefaultRecipients []string `json:"default_recipients"`
	Version           int      `json:"version"`
}

// GetAgencyProfile returns the workspace's profile; data is null when none
// was saved.
// GET /tenant/agency-profile
func (h *AgencyProfileHandler) GetAgencyProfile(c *gin.Context) {
	ctx := c.Request.Context()
	p, err := h.repo.Get(ctx, c.GetUint64(types.TenantIDContextKey.String()))
	if err != nil {
		logger.Errorf(ctx, "get agency profile: %v", err)
		c.Error(errors.NewInternalServerError("Failed to load the agency profile"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": p})
}

// UpdateAgencyProfile saves the workspace's profile.
// PUT /tenant/agency-profile
func (h *AgencyProfileHandler) UpdateAgencyProfile(c *gin.Context) {
	ctx := c.Request.Context()
	var req AgencyProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid agency profile"))
		return
	}
	userID, _ := types.UserIDFromContext(ctx)
	p := &types.TenantAgencyProfile{
		TenantID:     c.GetUint64(types.TenantIDContextKey.String()),
		ParentAgency: req.ParentAgency, AgencyName: req.AgencyName, AgencyAbbr: req.AgencyAbbr,
		Locality: req.Locality, HeadTitle: req.HeadTitle, CollectiveName: req.CollectiveName,
		DefaultRecipients: req.DefaultRecipients, UpdatedBy: userID,
	}
	p.Normalize()
	if err := p.Validate(); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	saved, err := h.repo.Save(ctx, p, req.Version)
	switch {
	case stderrors.Is(err, interfaces.ErrAgencyProfileVersion):
		c.Error(errors.NewConflictError("Hồ sơ đơn vị vừa được người khác cập nhật; hãy tải lại rồi sửa."))
		return
	case err != nil:
		logger.Errorf(ctx, "save agency profile: %v", err)
		c.Error(errors.NewInternalServerError("Failed to save the agency profile"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": saved})
}
