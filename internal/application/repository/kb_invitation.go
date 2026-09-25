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

func (r *kbInvitationRepository) ListAcceptedKBIDsByUser(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).Model(&types.KBInvitation{}).
		Where("recipient_user_id = ? AND status = ?", userID, types.KBInvitationStatusAccepted).
		Where("(expires_at IS NULL OR expires_at > ?)", time.Now()).
		Pluck("kb_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *kbInvitationRepository) HasAcceptedInvite(ctx context.Context, kbID, userID string) (bool, error) {
	if kbID == "" || userID == "" {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&types.KBInvitation{}).
		Where("kb_id = ? AND recipient_user_id = ? AND status = ?", kbID, userID, types.KBInvitationStatusAccepted).
		Where("(expires_at IS NULL OR expires_at > ?)", time.Now()).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *kbInvitationRepository) DeleteByKBID(ctx context.Context, kbID string) error {
	return r.db.WithContext(ctx).Where("kb_id = ?", kbID).Delete(&types.KBInvitation{}).Error
}
