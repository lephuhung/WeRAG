package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Sentinel errors for the KB invitation lifecycle. Handlers map them to
// HTTP statuses; callers compare with errors.Is.
var (
	ErrKBInviteNotFound      = errors.New("kb invitation not found")
	ErrKBInviteExists        = errors.New("a pending invitation already exists for this user on this knowledge base")
	ErrKBInviteNotPending    = errors.New("kb invitation is no longer active")
	ErrKBInviteExpired       = errors.New("kb invitation has expired")
	ErrKBInviteForbidden     = errors.New("only the invited recipient can accept this invitation")
	ErrKBInviteTokenInvalid  = errors.New("kb invitation token is invalid or has been revoked")
	ErrKBInviteSelfTarget    = errors.New("cannot invite a member of the owning tenant via cross-tenant invite")
	ErrKBInviteNotOwnerAdmin = errors.New("only a tenant admin of the owning tenant can manage kb invitations")
	// ErrKBInviteRecipientNotMember is returned at issuance when the
	// named recipient holds no active membership in recipient_tenant_id.
	ErrKBInviteRecipientNotMember = errors.New("recipient must be an active member of the recipient workspace")
)

// kbInvitationService implements interfaces.KBInvitationService.
type kbInvitationService struct {
	invites   interfaces.KBInvitationRepository
	kbs       interfaces.KBInviteKBReader
	members   interfaces.KBInviteMemberReader
	users     interfaces.KBInviteUserReader
	audit     interfaces.KBInviteAuditWriter
	now       func() time.Time
	tokenSize int
}

// NewKBInvitationService wires the service. members/users/audit may be nil
// in tests; business rules degrade fail-closed (no membership check bypass).
func NewKBInvitationService(
	invites interfaces.KBInvitationRepository,
	kbs interfaces.KBInviteKBReader,
	members interfaces.KBInviteMemberReader,
	users interfaces.KBInviteUserReader,
	audit interfaces.KBInviteAuditWriter,
) interfaces.KBInvitationService {
	return &kbInvitationService{
		invites: invites, kbs: kbs, members: members, users: users, audit: audit,
		now: time.Now, tokenSize: 32,
	}
}

// generateInviteToken returns a fresh random plaintext token (base64url).
func (s *kbInvitationService) generateInviteToken() (string, error) {
	buf := make([]byte, s.tokenSize)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// requireOwnerAdmin verifies the caller may manage invites on the KB-owning
// tenant. Route guards enforce this too; the service re-checks so direct
// callers cannot bypass it. Ownership is the authorization owner
// (OwnerTenantID): platform-owned rows (owner 0) admit only an explicit
// human SuperAdmin (real non-synthetic user, no API-key identity),
// regardless of active TenantID; tenant-owned rows require strict
// same-tenant TenantAdmin membership, and platform-admin status never
// overrides another tenant's ownership.
func (s *kbInvitationService) requireOwnerAdmin(ctx context.Context, caller types.Caller, ownerTenantID uint64) error {
	caller = caller.Normalize()
	if ownerTenantID == 0 {
		if access.IsExplicitHumanSuperAdmin(ctx, caller) {
			return nil
		}
		return ErrKBInviteNotOwnerAdmin
	}
	if caller.TenantID != ownerTenantID {
		return ErrKBInviteNotOwnerAdmin
	}
	if caller.Role.IsTenantAdmin() {
		return nil
	}
	return ErrKBInviteNotOwnerAdmin
}

func inviteToResponse(inv *types.KBInvitation, kbName, token string) *types.KBInviteResponse {
	return &types.KBInviteResponse{
		ID: inv.ID, KBID: inv.KBID, KBName: kbName,
		OwnerTenantID:   inv.OwnerTenantID,
		RecipientUserID: inv.RecipientUserID, RecipientTenantID: inv.RecipientTenantID,
		Status: inv.Status, Message: inv.Message,
		ExpiresAt: inv.ExpiresAt, AcceptedAt: inv.AcceptedAt,
		CreatedAt: inv.CreatedAt, Token: token,
	}
}

// Issue creates a pending invite on a KB owned by the caller's tenant.
// The recipient must be an ACTIVE MEMBER of recipient_tenant_id (not
// merely an existing user); inviting a member of the owning tenant is
// rejected (they already have access). Emits a kb.invite_issued audit row.
func (s *kbInvitationService) Issue(ctx context.Context, caller types.Caller, kbID string, req *types.CreateKBInviteRequest) (*types.KBInviteResponse, error) {
	caller = caller.Normalize()
	if kbID == "" || req == nil || req.RecipientTenantID == 0 {
		return nil, ErrKBInviteNotFound
	}
	recipientUserID := req.RecipientUserID
	if req.RecipientEmail != "" {
		if s.users == nil {
			return nil, ErrKBInviteNotFound
		}
		u, err := s.users.GetUserByEmail(ctx, req.RecipientEmail)
		if err != nil || u == nil {
			return nil, ErrKBInviteNotFound
		}
		if recipientUserID != "" && recipientUserID != u.ID {
			return nil, ErrKBInviteNotFound
		}
		recipientUserID = u.ID
	}
	if recipientUserID == "" {
		return nil, ErrKBInviteNotFound
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil {
		return nil, ErrKBInviteNotFound
	}
	if err := s.requireOwnerAdmin(ctx, caller, kb.OwnerTenantID); err != nil {
		return nil, err
	}
	if req.RecipientTenantID == kb.OwnerTenantID {
		return nil, ErrKBInviteSelfTarget
	}
	// Recipient must be a real user when the user repo is available.
	if s.users != nil {
		u, err := s.users.GetUserByID(ctx, recipientUserID)
		if err != nil || u == nil {
			return nil, ErrKBInviteNotFound
		}
	}
	// The recipient must hold an active membership in the intended
	// tenant at issuance time (re-validated at accept time). When the
	// member reader is unavailable we fail closed.
	if s.members == nil {
		return nil, ErrKBInviteRecipientNotMember
	}
	m, err := s.members.GetMembership(ctx, recipientUserID, req.RecipientTenantID)
	if err != nil || m == nil || m.Status != types.TenantMemberStatusActive {
		return nil, ErrKBInviteRecipientNotMember
	}
	if existing, err := s.invites.GetPendingByPair(ctx, kbID, recipientUserID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrKBInviteExists
	}
	token, err := s.generateInviteToken()
	if err != nil {
		return nil, err
	}
	inv := &types.KBInvitation{
		ID: tokenUUID(), KBID: kb.ID, OwnerTenantID: kb.OwnerTenantID,
		RecipientUserID: recipientUserID, RecipientTenantID: req.RecipientTenantID,
		InviterUserID: caller.UserID, TokenHash: types.HashKBInviteToken(token),
		Status: types.KBInvitationStatusPending, Message: req.Message,
		ExpiresAt: req.ExpiresAt,
	}
	if err := s.invites.Create(ctx, inv); err != nil {
		return nil, err
	}
	s.emitInviteAudit(ctx, kb.TenantID, types.AuditActionKBInviteIssued, caller, inv)
	return inviteToResponse(inv, kb.Name, token), nil
}

// ListByKB lists invites on a KB owned by the caller's tenant.
func (s *kbInvitationService) ListByKB(ctx context.Context, caller types.Caller, kbID string) ([]*types.KBInviteResponse, error) {
	caller = caller.Normalize()
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil {
		return nil, ErrKBInviteNotFound
	}
	if err := s.requireOwnerAdmin(ctx, caller, kb.OwnerTenantID); err != nil {
		return nil, err
	}
	rows, err := s.invites.ListByKB(ctx, kbID, nil)
	if err != nil {
		return nil, err
	}
	out := make([]*types.KBInviteResponse, 0, len(rows))
	for _, inv := range rows {
		out = append(out, inviteToResponse(inv, kb.Name, ""))
	}
	return out, nil
}

// Revoke withdraws a pending OR accepted invite on a KB owned by the
// caller's tenant. The transition is atomic (single UPDATE with a
// status-in predicate): revoking an accepted invite stops all future
// reads immediately because HasAcceptedInvite only counts accepted rows.
// Emits a kb.invite_revoked audit row.
func (s *kbInvitationService) Revoke(ctx context.Context, caller types.Caller, inviteID string) error {
	caller = caller.Normalize()
	inv, err := s.invites.GetByID(ctx, inviteID)
	if err != nil || inv == nil {
		return ErrKBInviteNotFound
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, inv.KBID)
	if err != nil || kb == nil {
		return ErrKBInviteNotFound
	}
	if kb.OwnerTenantID != inv.OwnerTenantID {
		return ErrKBInviteNotFound
	}
	if err := s.requireOwnerAdmin(ctx, caller, kb.OwnerTenantID); err != nil {
		return err
	}
	if inv.Status != types.KBInvitationStatusPending && inv.Status != types.KBInvitationStatusAccepted {
		return ErrKBInviteNotPending
	}
	if err := s.invites.MarkRevoked(ctx, inviteID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrKBInviteNotPending
		}
		return err
	}
	s.emitInviteAudit(ctx, kb.TenantID, types.AuditActionKBInviteRevoked, caller, inv)
	return nil
}

// Accept redeems a pending invite as the authenticated recipient. It
// verifies, in order: token hash resolves to a pending row; row not
// expired; caller IS the bound recipient (non-enumerating error
// otherwise); caller holds an active membership in the intended tenant;
// then atomically consumes the invite. The plaintext token is single-use.
func (s *kbInvitationService) Accept(ctx context.Context, caller types.Caller, token string) (*types.KBInviteResponse, error) {
	caller = caller.Normalize()
	if token == "" {
		return nil, ErrKBInviteTokenInvalid
	}
	if caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) {
		return nil, ErrKBInviteTokenInvalid
	}
	inv, err := s.invites.GetByTokenHash(ctx, types.HashKBInviteToken(token))
	if err != nil || inv == nil {
		return nil, ErrKBInviteTokenInvalid
	}
	if inv.Status != types.KBInvitationStatusPending {
		return nil, ErrKBInviteTokenInvalid
	}
	if inv.ExpiresAt != nil && !inv.ExpiresAt.After(s.now()) {
		_ = s.invites.MarkStatusIfPending(ctx, inv.ID, types.KBInvitationStatusExpired)
		return nil, ErrKBInviteTokenInvalid
	}
	// Recipient binding: only the named user may redeem. Same
	// non-enumerating error so an attacker cannot probe which user a
	// token belongs to.
	if inv.RecipientUserID != caller.UserID {
		return nil, ErrKBInviteTokenInvalid
	}
	// The recipient must hold an active membership in the intended
	// tenant at accept time. When the member service is unavailable we
	// fail closed.
	if s.members == nil {
		return nil, ErrKBInviteForbidden
	}
	m, err := s.members.GetMembership(ctx, caller.UserID, inv.RecipientTenantID)
	if err != nil || m == nil || m.Status != types.TenantMemberStatusActive {
		return nil, ErrKBInviteForbidden
	}
	accepted, err := s.invites.MarkAccepted(ctx, inv.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKBInviteTokenInvalid
		}
		return nil, err
	}
	var kbName string
	if s.kbs != nil {
		if kb, err := s.kbs.GetKnowledgeBaseByID(ctx, inv.KBID); err == nil && kb != nil {
			kbName = kb.Name
		}
	}
	s.emitInviteAudit(ctx, inv.OwnerTenantID, types.AuditActionKBInviteAccepted, caller, accepted)
	return inviteToResponse(accepted, kbName, ""), nil
}

// AcceptByID redeems a pending invite by ID as the authenticated
// recipient — the in-app counterpart to token Accept (which covers
// out-of-band delivery). Same binding, membership and single-use checks;
// emits kb.invite_accepted.
func (s *kbInvitationService) AcceptByID(ctx context.Context, caller types.Caller, inviteID string) (*types.KBInviteResponse, error) {
	caller = caller.Normalize()
	if inviteID == "" {
		return nil, ErrKBInviteNotFound
	}
	if caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) {
		return nil, ErrKBInviteForbidden
	}
	inv, err := s.invites.GetByID(ctx, inviteID)
	if err != nil || inv == nil {
		return nil, ErrKBInviteNotFound
	}
	if inv.Status != types.KBInvitationStatusPending {
		return nil, ErrKBInviteNotPending
	}
	if inv.ExpiresAt != nil && !inv.ExpiresAt.After(s.now()) {
		_ = s.invites.MarkStatusIfPending(ctx, inv.ID, types.KBInvitationStatusExpired)
		return nil, ErrKBInviteExpired
	}
	if inv.RecipientUserID != caller.UserID {
		return nil, ErrKBInviteForbidden
	}
	if s.members == nil {
		return nil, ErrKBInviteForbidden
	}
	m, err := s.members.GetMembership(ctx, caller.UserID, inv.RecipientTenantID)
	if err != nil || m == nil || m.Status != types.TenantMemberStatusActive {
		return nil, ErrKBInviteForbidden
	}
	accepted, err := s.invites.MarkAccepted(ctx, inv.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKBInviteNotPending
		}
		return nil, err
	}
	var kbName string
	if s.kbs != nil {
		if kb, err := s.kbs.GetKnowledgeBaseByID(ctx, inv.KBID); err == nil && kb != nil {
			kbName = kb.Name
		}
	}
	s.emitInviteAudit(ctx, inv.OwnerTenantID, types.AuditActionKBInviteAccepted, caller, accepted)
	return inviteToResponse(accepted, kbName, ""), nil
}

// ListMine lists invites addressed to the caller.
func (s *kbInvitationService) ListMine(ctx context.Context, caller types.Caller) ([]*types.KBInviteResponse, error) {
	caller = caller.Normalize()
	if caller.UserID == "" {
		return nil, ErrKBInviteForbidden
	}
	rows, err := s.invites.ListByRecipient(ctx, caller.UserID, nil)
	if err != nil {
		return nil, err
	}
	out := make([]*types.KBInviteResponse, 0, len(rows))
	for _, inv := range rows {
		out = append(out, inviteToResponse(inv, "", ""))
	}
	return out, nil
}

// HasAcceptedInvite implements access.KBInviteLookup.
func (s *kbInvitationService) HasAcceptedInvite(ctx context.Context, kbID, userID string) (bool, error) {
	return s.invites.HasAcceptedInvite(ctx, kbID, userID)
}

// InvitedKBIDs returns the KB IDs on which userID holds a live (accepted,
// unexpired) invite — used for recipient-side discovery (listing).
func (s *kbInvitationService) InvitedKBIDs(ctx context.Context, userID string) ([]string, error) {
	return s.invites.ListAcceptedKBIDsByUser(ctx, userID)
}

// emitInviteAudit writes a best-effort audit row for the invite
// lifecycle; a nil audit service disables it without failing business ops.
func (s *kbInvitationService) emitInviteAudit(ctx context.Context, tenantID uint64, action types.AuditAction, caller types.Caller, inv *types.KBInvitation) {
	if s.audit == nil || inv == nil {
		return
	}
	details, _ := json.Marshal(map[string]any{
		"invite_id":           inv.ID,
		"owner_tenant_id":     inv.OwnerTenantID,
		"recipient_user_id":   inv.RecipientUserID,
		"recipient_tenant_id": inv.RecipientTenantID,
	})
	_ = s.audit.Log(ctx, &types.AuditLog{
		TenantID:    tenantID,
		ActorUserID: caller.UserID,
		ActorRole:   string(caller.Role),
		Action:      action,
		ScopeType:   "knowledge_base",
		ScopeID:     inv.KBID,
		TargetType:  "kb_invitation",
		TargetID:    inv.ID,
		Outcome:     types.AuditOutcomeSuccess,
		Details:     types.JSON(details),
	})
}

func tokenUUID() string { return uuid.NewString() }
