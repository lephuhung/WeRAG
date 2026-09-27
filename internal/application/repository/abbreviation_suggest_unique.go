package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

func (r *abbreviationRepository) SuggestUnique(ctx context.Context, row *types.Abbreviation) (*types.Abbreviation, error) {
	if row == nil || strings.TrimSpace(row.ShortForm) == "" || strings.TrimSpace(row.FullForm) == "" {
		return nil, types.ErrAbbreviationBadSelection
	}
	short := strings.ToLower(strings.TrimSpace(row.ShortForm))
	full := strings.ToLower(strings.TrimSpace(row.FullForm))
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(short+"\x00"+full)))
	var result *types.Abbreviation
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result = nil
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(`INSERT INTO abbreviation_suggestion_locks ("key") VALUES (?) ON CONFLICT DO NOTHING`, key).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE abbreviation_suggestion_locks SET "key" = "key" WHERE "key" = ?`, key).Error; err != nil {
				return err
			}
			var rows []*types.Abbreviation
			if err := tx.Find(&rows).Error; err != nil {
				return err
			}
			for _, existing := range rows {
				if strings.ToLower(strings.TrimSpace(existing.ShortForm)) == short &&
					strings.ToLower(strings.TrimSpace(existing.FullForm)) == full &&
					(result == nil || existing.IsActive && !result.IsActive) {
					result = existing
				}
			}
			if result != nil {
				return nil
			}
			candidate := *row
			candidate.ID = ""
			candidate.DeletedAt = gorm.DeletedAt{}
			candidate.ShortForm = strings.TrimSpace(row.ShortForm)
			candidate.FullForm = strings.TrimSpace(row.FullForm)
			candidate.IsActive = false
			if err := tx.Create(&candidate).Error; err != nil {
				return err
			}
			result = &candidate
			return nil
		})
		if err == nil {
			return result, nil
		}
		var sqliteErr sqlite3.Error
		if r.db.Dialector.Name() != "sqlite" || !errors.As(err, &sqliteErr) ||
			(sqliteErr.Code != sqlite3.ErrBusy && sqliteErr.Code != sqlite3.ErrLocked) || attempt == 4 {
			return nil, err
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return nil, types.ErrAbbreviationConflict
}
