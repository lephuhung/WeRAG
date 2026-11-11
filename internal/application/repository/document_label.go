package repository

import (
	"context"
	"sort"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

const (
	documentLabelMaxPage   = 200
	documentLabelMaxDepth  = 10000 // offset+limit a list page may reach
	documentLabelBatchSize = 500
)

type documentLabelRepository struct {
	db *gorm.DB
}

// NewDocumentLabelRepository reads the spelling and format feedback tables
// back as labels (review table and JSONL export).
func NewDocumentLabelRepository(db *gorm.DB) interfaces.DocumentLabelRepository {
	return &documentLabelRepository{db: db}
}

func labelKinds(q *types.DocumentLabelQuery) (spelling, format bool) {
	spelling = q.Kind == "" || q.Kind == types.DocumentLabelSpelling
	// format verdicts are all the user's
	format = (q.Kind == "" || q.Kind == types.DocumentLabelFormat) &&
		(q.Source == "" || q.Source == types.SpellingFeedbackSourceUser)
	return spelling, format
}

func (r *documentLabelRepository) filtered(ctx context.Context, model any, q *types.DocumentLabelQuery, spelling bool) *gorm.DB {
	db := r.db.WithContext(ctx).Model(model)
	if q.TenantID != 0 {
		db = db.Where("tenant_id = ?", q.TenantID)
	}
	if q.Verdict != "" {
		db = db.Where("verdict = ?", q.Verdict)
	}
	if q.Model != "" {
		db = db.Where("model = ?", q.Model)
	}
	if q.From != nil {
		db = db.Where("created_at >= ?", *q.From)
	}
	if q.To != nil {
		db = db.Where("created_at < ?", *q.To)
	}
	if spelling && q.Source != "" {
		db = db.Where("source = ?", q.Source)
	}
	return db
}

func (r *documentLabelRepository) List(ctx context.Context, q *types.DocumentLabelQuery) ([]types.DocumentLabel, int64, error) {
	limit := q.Limit
	if limit <= 0 || limit > documentLabelMaxPage {
		limit = 50
	}
	offset := max(q.Offset, 0)
	if offset+limit > documentLabelMaxDepth {
		return []types.DocumentLabel{}, 0, nil
	}
	// Both tables are read up to offset+limit rows and merged: the review
	// table pages a few hundred rows deep at most.
	want := offset + limit
	spelling, format := labelKinds(q)
	var out []types.DocumentLabel
	var total int64
	if spelling {
		var n int64
		if err := r.filtered(ctx, &types.DocumentSpellingFeedback{}, q, true).Count(&n).Error; err != nil {
			return nil, 0, err
		}
		total += n
		var rows []types.DocumentSpellingFeedback
		if err := r.filtered(ctx, &types.DocumentSpellingFeedback{}, q, true).
			Order("created_at DESC, id DESC").Limit(want).Find(&rows).Error; err != nil {
			return nil, 0, err
		}
		for i := range rows {
			out = append(out, types.SpellingLabel(&rows[i]))
		}
	}
	if format {
		var n int64
		if err := r.filtered(ctx, &types.DocumentFormatFeedback{}, q, false).Count(&n).Error; err != nil {
			return nil, 0, err
		}
		total += n
		var rows []types.DocumentFormatFeedback
		if err := r.filtered(ctx, &types.DocumentFormatFeedback{}, q, false).
			Order("created_at DESC, id DESC").Limit(want).Find(&rows).Error; err != nil {
			return nil, 0, err
		}
		for i := range rows {
			out = append(out, types.FormatLabel(&rows[i]))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	if offset >= len(out) {
		return []types.DocumentLabel{}, total, nil
	}
	return out[offset:min(len(out), offset+limit)], total, nil
}

func (r *documentLabelRepository) Each(ctx context.Context, q *types.DocumentLabelQuery, fn func(*types.DocumentLabel) error) error {
	spelling, format := labelKinds(q)
	if spelling {
		var last uint64
		for {
			var rows []types.DocumentSpellingFeedback
			if err := r.filtered(ctx, &types.DocumentSpellingFeedback{}, q, true).Where("id > ?", last).
				Order("id ASC").Limit(documentLabelBatchSize).Find(&rows).Error; err != nil {
				return err
			}
			for i := range rows {
				l := types.SpellingLabel(&rows[i])
				if err := fn(&l); err != nil {
					return err
				}
			}
			if len(rows) < documentLabelBatchSize {
				break
			}
			last = rows[len(rows)-1].ID
		}
	}
	if format {
		var last uint64
		for {
			var rows []types.DocumentFormatFeedback
			if err := r.filtered(ctx, &types.DocumentFormatFeedback{}, q, false).Where("id > ?", last).
				Order("id ASC").Limit(documentLabelBatchSize).Find(&rows).Error; err != nil {
				return err
			}
			for i := range rows {
				l := types.FormatLabel(&rows[i])
				if err := fn(&l); err != nil {
					return err
				}
			}
			if len(rows) < documentLabelBatchSize {
				break
			}
			last = rows[len(rows)-1].ID
		}
	}
	return nil
}

func (r *documentLabelRepository) Models(ctx context.Context, tenantID uint64) ([]string, error) {
	seen := map[string]bool{}
	for _, model := range []any{&types.DocumentSpellingFeedback{}, &types.DocumentFormatFeedback{}} {
		db := r.db.WithContext(ctx).Model(model).Where("model <> ''")
		if tenantID != 0 {
			db = db.Where("tenant_id = ?", tenantID)
		}
		var names []string
		if err := db.Distinct("model").Limit(200).Pluck("model", &names).Error; err != nil {
			return nil, err
		}
		for _, n := range names {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}
