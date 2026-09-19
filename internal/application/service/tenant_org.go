package service

import (
	"context"
	"strconv"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// tenantOrgService implements interfaces.TenantOrgService. Tenant orgs
// are user groups inside one tenant — the scope unit for
// visibility='org' knowledge bases. Management is split:
//
//   - org CRUD: tenant Admin/Owner (route layer) — the service re-checks
//     same-tenant binding only;
//   - member roster + invite links: tenant Admin/Owner or an org manager;
//   - invitee must hold tenant membership before joining an org.
type tenantOrgService struct {
	repo          interfaces.TenantOrgRepository
	memberSvc     interfaces.TenantMemberService
	invitationSvc interfaces.TenantInvitationService
	userRepo      interfaces.UserRepository
	audit         interfaces.AuditLogService
}

// NewTenantOrgService wires the org service. audit may be nil.
func NewTenantOrgService(
	repo interfaces.TenantOrgRepository,
	memberSvc interfaces.TenantMemberService,
	invitationSvc interfaces.TenantInvitationService,
	userRepo interfaces.UserRepository,
	audit interfaces.AuditLogService,
) interfaces.TenantOrgService {
	return &tenantOrgService{
		repo:          repo,
		memberSvc:     memberSvc,
		invitationSvc: invitationSvc,
		userRepo:      userRepo,
		audit:         audit,
	}
}

func (s *tenantOrgService) emitAudit(ctx context.Context, entry *types.AuditLog) {
	if s.audit != nil {
		_ = s.audit.Log(ctx, entry)
	}
}

// requireOrg loads the org and verifies it belongs to the caller's
// tenant. Cross-tenant org ids surface as "not found" to avoid an
// enumeration oracle.
func (s *tenantOrgService) requireOrg(ctx context.Context, id uint64) (*types.TenantOrg, error) {
	org, err := s.repo.GetOrgByID(ctx, id)
	if err != nil || org == nil {
		return nil, apperrors.NewNotFoundError("org not found")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	if org.TenantID != tenantID {
		return nil, apperrors.NewNotFoundError("org not found")
	}
	return org, nil
}

// requireOrgManager allows tenant Admin/Owner, system admins and
// managers of this specific org to run member-management operations.
func (s *tenantOrgService) requireOrgManager(ctx context.Context, orgID uint64) error {
	caller := types.CallerFromContext(ctx)
	if types.IsSystemAdminFromContext(ctx) || caller.Role.HasPermission(types.TenantRoleAdmin) {
		return nil
	}
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey && caller.UserID == "" {
		return nil
	}
	member, err := s.repo.GetMember(ctx, orgID, caller.UserID)
	if err != nil {
		return err
	}
	if member == nil || member.Role != types.TenantOrgRoleManager {
		return apperrors.NewForbiddenError("chỉ quản trị viên tổ chức hoặc Admin của workspace mới có quyền này")
	}
	return nil
}

// requireTenantMember ensures the target user belongs to the org's
// tenant — org membership never exists outside tenant membership.
func (s *tenantOrgService) requireTenantMember(ctx context.Context, org *types.TenantOrg, userID string) error {
	m, err := s.memberSvc.GetMembership(ctx, userID, org.TenantID)
	if err != nil {
		return err
	}
	if m == nil || m.Status != types.TenantMemberStatusActive {
		return apperrors.NewBadRequestError("user is not an active member of this workspace")
	}
	return nil
}

func (s *tenantOrgService) CreateOrg(ctx context.Context, req *types.CreateTenantOrgRequest) (*types.TenantOrg, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	userID, _ := types.UserIDFromContext(ctx)
	org := &types.TenantOrg{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		CreatedBy:   userID,
	}
	if err := s.repo.CreateOrg(ctx, org); err != nil {
		return nil, err
	}
	s.emitAudit(ctx, &types.AuditLog{
		TenantID:    tenantID,
		ActorUserID: auditActor(ctx),
		ActorRole:   auditActorRole(ctx),
		Action:      types.AuditActionOrgCreated,
		TargetType:  "tenant_org",
		TargetID:    strconv.FormatUint(org.ID, 10),
		Outcome:     types.AuditOutcomeSuccess,
	})
	return org, nil
}

func (s *tenantOrgService) GetOrg(ctx context.Context, id uint64) (*types.TenantOrg, error) {
	return s.requireOrg(ctx, id)
}

func (s *tenantOrgService) ListOrgs(ctx context.Context) ([]*types.TenantOrgResponse, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	orgs, err := s.repo.ListOrgsByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*types.TenantOrgResponse, 0, len(orgs))
	for _, o := range orgs {
		count, _ := s.repo.CountMembers(ctx, o.ID)
		out = append(out, &types.TenantOrgResponse{
			ID:          o.ID,
			TenantID:    o.TenantID,
			Name:        o.Name,
			Description: o.Description,
			CreatedBy:   o.CreatedBy,
			MemberCount: int(count),
			CreatedAt:   o.CreatedAt,
		})
	}
	return out, nil
}

func (s *tenantOrgService) UpdateOrg(ctx context.Context, id uint64, req *types.UpdateTenantOrgRequest) (*types.TenantOrg, error) {
	org, err := s.requireOrg(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireOrgManager(ctx, org.ID); err != nil {
		return nil, err
	}
	if req.Name != nil {
		org.Name = *req.Name
	}
	if req.Description != nil {
		org.Description = *req.Description
	}
	if err := s.repo.UpdateOrg(ctx, org); err != nil {
		return nil, err
	}
	return org, nil
}

func (s *tenantOrgService) DeleteOrg(ctx context.Context, id uint64) error {
	org, err := s.requireOrg(ctx, id)
	if err != nil {
		return err
	}
	// Deleting an org detaches its KB scope — keep it Admin+ only so an
	// org manager cannot silently widen their org's KBs to tenant scope.
	caller := types.CallerFromContext(ctx)
	if !caller.Role.HasPermission(types.TenantRoleAdmin) && !types.IsSystemAdminFromContext(ctx) {
		return apperrors.NewForbiddenError("chỉ Admin của workspace mới được xoá tổ chức")
	}
	if err := s.repo.DeleteOrg(ctx, org.ID); err != nil {
		return err
	}
	s.emitAudit(ctx, &types.AuditLog{
		TenantID:    org.TenantID,
		ActorUserID: auditActor(ctx),
		ActorRole:   auditActorRole(ctx),
		Action:      types.AuditActionOrgDeleted,
		TargetType:  "tenant_org",
		TargetID:    strconv.FormatUint(org.ID, 10),
		Outcome:     types.AuditOutcomeSuccess,
	})
	return nil
}

func (s *tenantOrgService) AddMember(ctx context.Context, orgID uint64, req *types.AddTenantOrgMemberRequest) error {
	org, err := s.requireOrg(ctx, orgID)
	if err != nil {
		return err
	}
	if !req.Role.IsValid() {
		return apperrors.NewBadRequestError("invalid org role")
	}
	if err := s.requireOrgManager(ctx, org.ID); err != nil {
		return err
	}
	if err := s.requireTenantMember(ctx, org, req.UserID); err != nil {
		return err
	}
	return s.repo.AddMember(ctx, &types.TenantOrgMember{
		OrgID:  org.ID,
		UserID: req.UserID,
		Role:   req.Role,
	})
}

func (s *tenantOrgService) RemoveMember(ctx context.Context, orgID uint64, userID string) error {
	org, err := s.requireOrg(ctx, orgID)
	if err != nil {
		return err
	}
	if err := s.requireOrgManager(ctx, org.ID); err != nil {
		return err
	}
	return s.repo.RemoveMember(ctx, org.ID, userID)
}

func (s *tenantOrgService) UpdateMemberRole(ctx context.Context, orgID uint64, userID string, req *types.UpdateTenantOrgMemberRequest) error {
	org, err := s.requireOrg(ctx, orgID)
	if err != nil {
		return err
	}
	if !req.Role.IsValid() {
		return apperrors.NewBadRequestError("invalid org role")
	}
	if err := s.requireOrgManager(ctx, org.ID); err != nil {
		return err
	}
	return s.repo.UpdateMemberRole(ctx, org.ID, userID, req.Role)
}

func (s *tenantOrgService) ListMembers(ctx context.Context, orgID uint64) ([]*types.TenantOrgMemberResponse, error) {
	org, err := s.requireOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembers(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	out := make([]*types.TenantOrgMemberResponse, 0, len(members))
	for _, m := range members {
		resp := &types.TenantOrgMemberResponse{
			OrgID:     m.OrgID,
			UserID:    m.UserID,
			Role:      m.Role,
			CreatedAt: m.CreatedAt,
		}
		if s.userRepo != nil {
			if u, err := s.userRepo.GetUserByID(ctx, m.UserID); err == nil && u != nil {
				resp.Username = u.Username
				resp.Email = u.Email
			}
		}
		out = append(out, resp)
	}
	return out, nil
}

// CreateInviteLink mints a multi-use share-link bound to this org.
// Accepting it registers the account, joins the tenant with the given
// role, and enrols the user into the org as a member — all through the
// existing tenant-invitation pipeline.
func (s *tenantOrgService) CreateInviteLink(
	ctx context.Context,
	orgID uint64,
	req *types.CreateTenantOrgInviteRequest,
) (*types.TenantInvitation, string, error) {
	org, err := s.requireOrg(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	if err := s.requireOrgManager(ctx, org.ID); err != nil {
		return nil, "", err
	}
	if s.invitationSvc == nil {
		return nil, "", apperrors.NewServiceUnavailableError("invitation service unavailable")
	}
	// Org invites grant ordinary tenant roles only — admin/owner stays
	// with tenant-level invitation APIs.
	if !req.Role.IsValid() || req.Role.HasPermission(types.TenantRoleAdmin) {
		return nil, "", apperrors.NewBadRequestError("org invite links may only grant viewer or contributor roles")
	}
	invitedBy, _ := types.UserIDFromContext(ctx)
	return s.invitationSvc.CreateShareLink(ctx, org.TenantID, org.ID, req.Role, &invitedBy, req.Message)
}

func (s *tenantOrgService) OrgIDsForUser(ctx context.Context, tenantID uint64, userID string) ([]uint64, error) {
	return s.repo.ListOrgIDsForUser(ctx, tenantID, userID)
}

func (s *tenantOrgService) MemberRole(ctx context.Context, orgID uint64, userID string) (types.TenantOrgRole, error) {
	m, err := s.repo.GetMember(ctx, orgID, userID)
	if err != nil || m == nil {
		return "", err
	}
	return m.Role, nil
}
