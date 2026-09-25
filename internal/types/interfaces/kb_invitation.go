package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KBInviteKBReader is the narrow KB dependency the invitation service
// needs. interfaces.KnowledgeBaseRepository satisfies it.
type KBInviteKBReader interface {
	GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error)
}

// KBInviteUserReader is the narrow user dependency the invitation service
// needs (recipient existence/email resolution). interfaces.UserRepository
// satisfies it.
type KBInviteUserReader interface {
	GetUserByID(ctx context.Context, id string) (*types.User, error)
	GetUserByEmail(ctx context.Context, email string) (*types.User, error)
}

// KBInviteAuditWriter is the narrow audit dependency the invitation service
// needs. interfaces.AuditLogService satisfies it.
type KBInviteAuditWriter interface {
	Log(ctx context.Context, entry *types.AuditLog) error
}

// KBInviteMemberReader is the narrow membership dependency the invitation
// service needs (active-membership validation at accept time).
type KBInviteMemberReader interface {
	GetMembership(ctx context.Context, userID string, tenantID uint64) (*types.TenantMember, error)
}

// KBInvitationRepository persists recipient-bound KB read invitations.
// Pending-state uniqueness (at most one pending row per (kb_id,
// recipient_user_id)) is enforced by the partial unique index
// uniq_kb_invite_pending; token-hash uniqueness by idx_kb_invites_token_hash.
type KBInvitationRepository interface {
	// Create inserts an invitation row.
	Create(ctx context.Context, inv *types.KBInvitation) error
	// GetByID returns the row or (nil, nil) when missing.
	GetByID(ctx context.Context, id string) (*types.KBInvitation, error)
	// GetByTokenHash resolves a pending row by its token hash, or (nil, nil).
	GetByTokenHash(ctx context.Context, tokenHash string) (*types.KBInvitation, error)
	// GetPendingByPair returns the pending invite for (kbID, recipientUserID), or (nil, nil).
	GetPendingByPair(ctx context.Context, kbID, recipientUserID string) (*types.KBInvitation, error)
	// ListByKB lists invites on a KB, newest first; statuses filters when non-empty.
	ListByKB(ctx context.Context, kbID string, statuses []types.KBInvitationStatus) ([]*types.KBInvitation, error)
	// ListByRecipient lists invites addressed to a user, newest first.
	ListByRecipient(ctx context.Context, recipientUserID string, statuses []types.KBInvitationStatus) ([]*types.KBInvitation, error)
	// MarkAccepted atomically transitions a pending row to accepted;
	// returns gorm.ErrRecordNotFound when the row already left pending.
	MarkAccepted(ctx context.Context, id string) (*types.KBInvitation, error)
	// MarkStatusIfPending atomically transitions a pending row to status.
	MarkStatusIfPending(ctx context.Context, id string, status types.KBInvitationStatus) error
	// MarkRevoked atomically transitions a pending OR accepted row to
	// revoked. Revoking an accepted row stops future reads immediately.
	// Returns gorm.ErrRecordNotFound when the row is in another state.
	MarkRevoked(ctx context.Context, id string) error
	// HasAcceptedInvite reports whether recipientUserID holds a live
	// (accepted, unexpired) invite on kbID.
	HasAcceptedInvite(ctx context.Context, kbID, recipientUserID string) (bool, error)
	// ListAcceptedKBIDsByUser returns the KB IDs on which userID holds a
	// live (accepted, unexpired) invite — recipient-side discovery.
	ListAcceptedKBIDsByUser(ctx context.Context, userID string) ([]string, error)
	// DeleteByKBID soft-deletes all invite rows for a KB.
	DeleteByKBID(ctx context.Context, kbID string) error
}

// KBInvitationService manages the recipient-bound invite lifecycle:
// owning-tenant Admin issues/revokes, the named recipient accepts.
type KBInvitationService interface {
	// Issue creates a pending invite on a KB owned by caller's tenant.
	// Returns the response with the plaintext token exactly once.
	Issue(ctx context.Context, caller types.Caller, kbID string, req *types.CreateKBInviteRequest) (*types.KBInviteResponse, error)
	// ListByKB lists invites on a KB owned by caller's tenant.
	ListByKB(ctx context.Context, caller types.Caller, kbID string) ([]*types.KBInviteResponse, error)
	// Revoke withdraws a pending invite on a KB owned by caller's tenant.
	Revoke(ctx context.Context, caller types.Caller, inviteID string) error
	// Accept redeems a pending invite as the authenticated recipient.
	Accept(ctx context.Context, caller types.Caller, token string) (*types.KBInviteResponse, error)
	// AcceptByID redeems a pending invite by ID as the authenticated
	// recipient (in-app flow; the server verifies recipient binding).
	AcceptByID(ctx context.Context, caller types.Caller, inviteID string) (*types.KBInviteResponse, error)
	// ListMine lists invites addressed to the caller.
	ListMine(ctx context.Context, caller types.Caller) ([]*types.KBInviteResponse, error)
	// HasAcceptedInvite reports whether userID holds a live invite on kbID
	// (implements access.KBInviteLookup).
	HasAcceptedInvite(ctx context.Context, kbID, userID string) (bool, error)
	// InvitedKBIDs returns the KB IDs on which userID holds a live
	// (accepted, unexpired) invite — recipient-side discovery.
	InvitedKBIDs(ctx context.Context, userID string) ([]string, error)
}
