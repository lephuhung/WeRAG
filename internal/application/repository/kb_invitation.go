package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// kbInvitationRepository implements interfaces.KBInvitationRepository.
type kbInvitationRepository struct {
	db *gorm.DB
}

// NewKBInvitationRepository constructs the repo against db.
func NewKBInvitationRepository(db *gorm.DB) interfaces.KBInvitationRepository {
	return &kbInvitationRepository{db: db}
}

func (r *kbInvitationRepository) Create(ctx context.Context, inv *types.KBInvitation) error {
	if inv.Status == "" {
		inv.Status = types.KBInvitationStatusPending
	}
	return r.db.WithContext(ctx).Create(inv).Error
}

func (r *kbInvitationRepository) GetByID(ctx context.Context, id string) (*types.KBInvitation, error) {
	var inv types.KBInvitation
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&inv).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &inv, nil
}

func (r *kbInvitationRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*types.KBInvitation, error) {
	if tokenHash == "" {
		return nil, nil
	}
	var inv types.KBInvitation
	err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&inv).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &inv, nil
}

func (r *kbInvitationRepository) GetPendingByPair(ctx context.Context, kbID, recipientUserID string) (*types.KBInvitation, error) {
	var inv types.KBInvitation
	err := r.db.WithContext(ctx).
		Where("kb_id = ? AND recipient_user_id = ? AND status = ?", kbID, recipientUserID, types.KBInvitationStatusPending).
		First(&inv).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &inv, nil
}

func (r *kbInvitationRepository) ListByKB(ctx context.Context, kbID string, statuses []types.KBInvitationStatus) ([]*types.KBInvitation, error) {
	q := r.db.WithContext(ctx).Where("kb_id = ?", kbID).Order("created_at DESC")
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var rows []*types.KBInvitation
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *kbInvitationRepository) ListByRecipient(ctx context.Context, recipientUserID string, statuses []types.KBInvitationStatus) ([]*types.KBInvitation, error) {
	q := r.db.WithContext(ctx).Where("recipient_user_id = ?", recipientUserID).Order("created_at DESC")
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var rows []*types.KBInvitation
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// MarkAccepted atomically transitions a pending row to accepted and stamps
// accepted_at. The status=pending predicate makes replay/double-accept
// safe: only the first writer wins; others see ErrRecordNotFound.
func (r *kbInvitationRepository) MarkAccepted(ctx context.Context, id string) (*types.KBInvitation, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&types.KBInvitation{}).
		Where("id = ? AND status = ?", id, types.KBInvitationStatusPending).
		Updates(map[string]any{"status": types.KBInvitationStatusAccepted, "accepted_at": now, "updated_at": now})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return r.GetByID(ctx, id)
}

func (r *kbInvitationRepository) MarkStatusIfPending(ctx context.Context, id string, status types.KBInvitationStatus) error {
	res := r.db.WithContext(ctx).Model(&types.KBInvitation{}).
		Where("id = ? AND status = ?", id, types.KBInvitationStatusPending).
		Updates(map[string]any{"status": status, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *kbInvitationRepository) MarkRevoked(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Model(&types.KBInvitation{}).
		Where("id = ? AND status IN ?", id, []types.KBInvitationStatus{
			types.KBInvitationStatusPending, types.KBInvitationStatusAccepted,
		}).
		Updates(map[string]any{"status": types.KBInvitationStatusRevoked, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *kbInvitationRepository) ListLiveInvitedKBIDsByUser(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	var ids []string
	// One query: an invite counts only while the KB still belongs to the
	// tenant that issued it (same rule as HasAcceptedInviteForOwner).
	err := r.db.WithContext(ctx).Model(&types.KBInvitation{}).
		Joins("JOIN knowledge_bases ON knowledge_bases.id = kb_invitations.kb_id"+
			" AND knowledge_bases.owner_tenant_id = kb_invitations.owner_tenant_id"+
			" AND knowledge_bases.deleted_at IS NULL").
		Where("kb_invitations.recipient_user_id = ? AND kb_invitations.status = ?",
			userID, types.KBInvitationStatusAccepted).
		Where("(kb_invitations.expires_at IS NULL OR kb_invitations.expires_at > ?)", time.Now()).
		Distinct().
		Pluck("kb_invitations.kb_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *kbInvitationRepository) HasAcceptedInviteForOwner(
	ctx context.Context, kbID, userID string, ownerTenantID uint64,
) (bool, error) {
	if kbID == "" || userID == "" || ownerTenantID == 0 {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&types.KBInvitation{}).
		Where("kb_id = ? AND recipient_user_id = ? AND owner_tenant_id = ? AND status = ?",
			kbID, userID, ownerTenantID, types.KBInvitationStatusAccepted).
		Where("(expires_at IS NULL OR expires_at > ?)", time.Now()).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasLiveInviteForOwner reports whether recipientUserID holds a live
// invite on kbID issued by ownerTenantID whose recipient still holds an
// active membership in the invite's recipient_tenant_id.
func (r *kbInvitationRepository) HasLiveInviteForOwner(
	ctx context.Context, kbID, userID string, ownerTenantID uint64,
) (bool, error) {
	if kbID == "" || userID == "" || ownerTenantID == 0 {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&types.KBInvitation{}).
		Joins("JOIN tenant_members ON tenant_members.user_id = kb_invitations.recipient_user_id"+
			" AND tenant_members.tenant_id = kb_invitations.recipient_tenant_id"+
			" AND tenant_members.status = ? AND tenant_members.deleted_at IS NULL",
			types.TenantMemberStatusActive).
		Where("kb_invitations.kb_id = ? AND kb_invitations.recipient_user_id = ?"+
			" AND kb_invitations.owner_tenant_id = ? AND kb_invitations.status = ?",
			kbID, userID, ownerTenantID, types.KBInvitationStatusAccepted).
		Where("(kb_invitations.expires_at IS NULL OR kb_invitations.expires_at > ?)", time.Now()).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *kbInvitationRepository) DeleteByKBID(ctx context.Context, kbID string) error {
	return r.db.WithContext(ctx).Where("kb_id = ?", kbID).Delete(&types.KBInvitation{}).Error
}
