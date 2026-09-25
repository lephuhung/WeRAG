package types

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"time"

	"gorm.io/gorm"
)

// KBInvitationStatus enumerates the lifecycle of a single recipient-bound
// KB read invitation. Legal transitions:
//
//	pending -> accepted | revoked | expired
//
// Accepted is terminal and single-use: the token cannot be replayed after
// acceptance. Revoked immediately denies future reads. Expired is set by
// the lazy sweep when now passes ExpiresAt.
type KBInvitationStatus string

const (
	KBInvitationStatusPending  KBInvitationStatus = "pending"
	KBInvitationStatusAccepted KBInvitationStatus = "accepted"
	KBInvitationStatusRevoked  KBInvitationStatus = "revoked"
	KBInvitationStatusExpired  KBInvitationStatus = "expired"
)

// IsTerminal reports whether the status can no longer grant access.
func (s KBInvitationStatus) IsTerminal() bool {
	switch s {
	case KBInvitationStatusAccepted, KBInvitationStatusRevoked, KBInvitationStatusExpired:
		return true
	default:
		return false
	}
}

// KBInvitation is a read-only grant of one knowledge base to one specific
// user in another tenant. It is created by a Tenant Admin of the KB-owning
// tenant, bound to (kb_id, recipient user, recipient tenant), and redeemed
// only by authenticating as that recipient. It never grants access to other
// members of the recipient's tenant, never grants write, and cannot be
// re-shared by the recipient.
//
// The bearer token is never persisted: only TokenHash (SHA-256 hex of the
// random token) is stored. The plaintext is returned once at creation for
// out-of-band delivery and must be compared in constant time on accept.
type KBInvitation struct {
	ID string `json:"id" gorm:"type:varchar(36);primaryKey"`
	// KBID references knowledge_bases.id.
	KBID string `json:"kb_id" gorm:"column:kb_id;type:varchar(36);not null;index"`
	// OwnerTenantID is denormalized from knowledge_bases.tenant_id.
	OwnerTenantID uint64 `json:"owner_tenant_id" gorm:"not null;index"`
	// RecipientUserID is the specific user who may redeem this invite.
	RecipientUserID string `json:"recipient_user_id" gorm:"type:varchar(36);not null;index"`
	// RecipientTenantID is the tenant the recipient must hold an active
	// membership in at accept time (validation only; grants no tenant-wide access).
	RecipientTenantID uint64 `json:"recipient_tenant_id" gorm:"not null;index"`
	// InviterUserID is the owning-tenant admin who issued the invite.
	InviterUserID string `json:"inviter_user_id" gorm:"type:varchar(36);not null"`
	// TokenHash is the SHA-256 hex digest of the bearer token. Never store raw tokens.
	TokenHash string             `json:"-" gorm:"type:varchar(64);not null;uniqueIndex:idx_kb_invites_token_hash"`
	Status    KBInvitationStatus `json:"status" gorm:"type:varchar(16);not null;default:'pending';index"`
	Message   string             `json:"message,omitempty" gorm:"type:varchar(500)"`

	ExpiresAt  *time.Time     `json:"expires_at,omitempty" gorm:"type:timestamp with time zone"`
	AcceptedAt *time.Time     `json:"accepted_at,omitempty" gorm:"type:timestamp with time zone"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `json:"deleted_at" gorm:"index"`
}

// TableName binds KBInvitation to the kb_invitations table.
func (KBInvitation) TableName() string { return "kb_invitations" }

// IsLive reports whether the invite currently confers read access:
// accepted and not past expiry. Pending rows grant nothing.
func (inv *KBInvitation) IsLive(now time.Time) bool {
	if inv == nil || inv.Status != KBInvitationStatusAccepted {
		return false
	}
	if inv.ExpiresAt != nil && !inv.ExpiresAt.After(now) {
		return false
	}
	return true
}

// HashKBInviteToken returns the SHA-256 hex digest of a plaintext invite token.
func HashKBInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// VerifyKBInviteToken compares a plaintext candidate against a stored hash
// in constant time. Empty inputs never verify.
func VerifyKBInviteToken(candidate, storedHash string) bool {
	if candidate == "" || storedHash == "" {
		return false
	}
	candidateHash := HashKBInviteToken(candidate)
	if len(candidateHash) != len(storedHash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidateHash), []byte(strings.ToLower(storedHash))) == 1 ||
		subtle.ConstantTimeCompare([]byte(candidateHash), []byte(storedHash)) == 1
}

// CreateKBInviteRequest is the body of POST /knowledge-bases/:id/invites.
// Only a Tenant Admin of the KB-owning tenant may call it. The recipient
// is identified by user ID, or by email (resolved server-side via the
// user repository); when both are given they must agree.
type CreateKBInviteRequest struct {
	RecipientUserID   string     `json:"recipient_user_id"`
	RecipientEmail    string     `json:"recipient_email"`
	RecipientTenantID uint64     `json:"recipient_tenant_id" binding:"required"`
	Message           string     `json:"message" binding:"omitempty,max=500"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
}

// AcceptKBInviteRequest is the body of POST /kb-invites/accept.
type AcceptKBInviteRequest struct {
	Token string `json:"token" binding:"required"`
}

// KBInviteResponse is the API projection. The token hash is never exposed;
// the plaintext token is returned once alongside creation (Token field)
// and never again.
type KBInviteResponse struct {
	ID                string             `json:"id"`
	KBID              string             `json:"kb_id"`
	KBName            string             `json:"kb_name,omitempty"`
	OwnerTenantID     uint64             `json:"owner_tenant_id"`
	RecipientUserID   string             `json:"recipient_user_id"`
	RecipientTenantID uint64             `json:"recipient_tenant_id"`
	Status            KBInvitationStatus `json:"status"`
	Message           string             `json:"message,omitempty"`
	ExpiresAt         *time.Time         `json:"expires_at,omitempty"`
	AcceptedAt        *time.Time         `json:"accepted_at,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
	// Token carries the plaintext exactly once at creation.
	Token string `json:"token,omitempty"`
}
