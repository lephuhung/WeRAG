package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// KBAccessOverviewHandler serves GET /knowledge-bases/:id/access: who owns a
// KB, its scope, what the caller may do there and why, and — for the KB's
// managers — everyone it is shared with.
type KBAccessOverviewHandler struct {
	grants  interfaces.KBAccessGrantService
	invites interfaces.KBInvitationService
	users   interfaces.UserService
	tenants interfaces.TenantService
}

// NewKBAccessOverviewHandler creates the handler. Every dependency may be
// nil in tests; names then fall back to IDs and grantee lists stay empty.
func NewKBAccessOverviewHandler(
	grants interfaces.KBAccessGrantService,
	invites interfaces.KBInvitationService,
	users interfaces.UserService,
	tenants interfaces.TenantService,
) *KBAccessOverviewHandler {
	return &KBAccessOverviewHandler{grants: grants, invites: invites, users: users, tenants: tenants}
}

// KBEffectivePermissions is what the caller may do on the KB.
type KBEffectivePermissions struct {
	View             bool `json:"view"`
	Contribute       bool `json:"contribute"`
	Manage           bool `json:"manage"`
	DownloadOriginal bool `json:"download_original"`
}

// KBGrantee is one ACL entry: a per-user invite or a per-tenant grant.
// ID is the grantee (user ID or tenant ID); ShareID is the invitation or
// grant row ID that DELETE /knowledge-bases/:id/invites|grants/:share_id
// revokes.
type KBGrantee struct {
	Kind          string     `json:"kind"` // "user" | "tenant"
	ID            string     `json:"id"`
	ShareID       string     `json:"share_id"`
	Name          string     `json:"name"`
	Email         string     `json:"email,omitempty"`
	TenantID      uint64     `json:"tenant_id,omitempty"`
	TenantName    string     `json:"tenant_name,omitempty"`
	Permission    string     `json:"permission"`
	Status        string     `json:"status"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RevokedReason string     `json:"revoked_reason,omitempty"`
}

// KBAccessOverview is the GET /knowledge-bases/:id/access payload.
// Grantees is present only for the KB's managers.
type KBAccessOverview struct {
	KBID                 string                 `json:"kb_id"`
	OwnerTenantID        uint64                 `json:"owner_tenant_id"`
	OwnerTenantName      string                 `json:"owner_tenant_name,omitempty"`
	Visibility           types.KBVisibility     `json:"visibility"`
	MemberDefault        types.KBMemberDefault  `json:"member_default"`
	EffectivePermissions KBEffectivePermissions `json:"effective_permissions"`
	Sources              []access.AccessSource  `json:"sources"`
	Grantees             []KBGrantee            `json:"grantees,omitempty"`
}

// GetAccess godoc
// @Summary      KB access overview
// @Description  Owner, scope, member_default, the caller's effective permissions and their sources; grantees for the KB's managers
// @Tags         Knowledge Base
// @Produce      json
// @Param        id   path      string  true  "Knowledge Base ID"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /knowledge-bases/{id}/access [get]
func (h *KBAccessOverviewHandler) GetAccess(c *gin.Context) {
	grant, ok := middleware.KBAccessFromContext(c)
	if !ok || grant.KnowledgeBase == nil || grant.KnowledgeBase.ID != c.Param("id") {
		_ = c.Error(apperrors.NewForbiddenError("Permission denied to access this knowledge base"))
		return
	}
	ctx := c.Request.Context()
	kb := grant.KnowledgeBase
	caller := grant.Caller
	permission := grant.Permission
	out := KBAccessOverview{
		KBID:          kb.ID,
		OwnerTenantID: kb.OwnerTenantID,
		Visibility:    kb.Visibility,
		MemberDefault: kb.MemberDefault,
		EffectivePermissions: KBEffectivePermissions{
			View:             permission.HasPermission(types.KBPermissionViewer),
			Contribute:       permission.HasPermission(types.KBPermissionEditor),
			Manage:           permission.HasPermission(types.KBPermissionAdmin),
			DownloadOriginal: grant.CanDownloadOriginal,
		},
		Sources: h.sources(ctx, caller, kb, grant.Source),
	}
	if !out.MemberDefault.IsValid() {
		out.MemberDefault = types.KBMemberDefaultContribute
	}
	names := newGranteeNames(h.users, h.tenants)
	if kb.OwnerTenantID != 0 {
		out.OwnerTenantName = names.tenant(ctx, kb.OwnerTenantID)
	}
	if access.CanManageKBSharing(ctx, caller, kb.OwnerTenantID) {
		out.Grantees = h.grantees(ctx, caller, kb, names)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// sources lists every rule that currently gives the caller access, the
// deciding one first, so a UI can warn that revoking one ACL entry leaves
// access through another (e.g. the KB is also published).
func (h *KBAccessOverviewHandler) sources(ctx context.Context, caller types.Caller, kb *types.KnowledgeBase,
	primary access.AccessSource,
) []access.AccessSource {
	out := []access.AccessSource{}
	if primary != access.SourceNone {
		out = append(out, primary)
	}
	if kb.OwnerTenantID == 0 || !access.IsAuthenticatedHuman(ctx, caller) {
		return out
	}
	add := func(s access.AccessSource) {
		for _, have := range out {
			if have == s {
				return
			}
		}
		out = append(out, s)
	}
	if h.invites != nil && caller.UserID != "" {
		if ok, err := h.invites.HasLiveInvite(ctx, kb.ID, caller.UserID); err == nil && ok {
			add(access.SourceACLUser)
		}
	}
	if caller.HasTenantRole() && h.grants != nil &&
		access.HasTenantGrant(ctx, caller, kb.ID, kb.OwnerTenantID, h.grants) {
		add(access.SourceACLTenant)
	}
	return out
}

func (h *KBAccessOverviewHandler) grantees(ctx context.Context, caller types.Caller, kb *types.KnowledgeBase,
	names *granteeNames,
) []KBGrantee {
	out := []KBGrantee{}
	if h.invites != nil {
		rows, err := h.invites.ListByKB(ctx, caller, kb.ID)
		if err != nil {
			logger.Warnf(ctx, "[kb_access] list invites for %s: %v", kb.ID, err)
		}
		for _, inv := range rows {
			name, email := names.user(ctx, inv.RecipientUserID)
			out = append(out, KBGrantee{
				Kind: "user", ID: inv.RecipientUserID, ShareID: inv.ID, Name: name, Email: email,
				TenantID: inv.RecipientTenantID, TenantName: names.tenant(ctx, inv.RecipientTenantID),
				Permission: string(types.KBPermissionViewer), Status: string(inv.Status),
				ExpiresAt: inv.ExpiresAt, RevokedReason: inv.RevokedReason,
			})
		}
	}
	if h.grants != nil && kb.OwnerTenantID != 0 {
		rows, err := h.grants.ListByKB(ctx, caller, kb.ID)
		if err != nil {
			logger.Warnf(ctx, "[kb_access] list tenant grants for %s: %v", kb.ID, err)
		}
		for _, g := range rows {
			name := g.GranteeName
			if name == "" {
				name = names.tenant(ctx, g.GranteeTenantID)
			}
			out = append(out, KBGrantee{
				Kind: "tenant", ID: strconv.FormatUint(g.GranteeTenantID, 10), ShareID: g.ID, Name: name,
				TenantID: g.GranteeTenantID, TenantName: name,
				Permission: string(g.Permission), Status: string(g.Status), ExpiresAt: g.ExpiresAt,
			})
		}
	}
	return out
}

// granteeNames resolves display names once per request, falling back to
// the ID when a lookup fails.
type granteeNames struct {
	users   interfaces.UserService
	tenants interfaces.TenantService
	u       map[string][2]string
	t       map[uint64]string
}

func newGranteeNames(users interfaces.UserService, tenants interfaces.TenantService) *granteeNames {
	return &granteeNames{users: users, tenants: tenants, u: map[string][2]string{}, t: map[uint64]string{}}
}

func (n *granteeNames) user(ctx context.Context, id string) (string, string) {
	if v, ok := n.u[id]; ok {
		return v[0], v[1]
	}
	name, email := id, ""
	if n.users != nil && id != "" {
		if u, err := n.users.GetUserByID(ctx, id); err == nil && u != nil {
			email = u.Email
			if u.Username != "" {
				name = u.Username
			} else if u.Email != "" {
				name = u.Email
			}
		}
	}
	n.u[id] = [2]string{name, email}
	return name, email
}

func (n *granteeNames) tenant(ctx context.Context, id uint64) string {
	if id == 0 {
		return ""
	}
	if v, ok := n.t[id]; ok {
		return v
	}
	name := strconv.FormatUint(id, 10)
	if n.tenants != nil {
		if t, err := n.tenants.GetTenantByID(ctx, id); err == nil && t != nil && t.Name != "" {
			name = t.Name
		}
	}
	n.t[id] = name
	return name
}
