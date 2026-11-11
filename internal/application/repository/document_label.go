package repository

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

const (
	documentLabelMaxPage   = 200
	documentLabelMaxDepth  = 2000 // offset+limit a list page may reach (the export has no cap)
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

// labelKey is one row's place in the merged, newest-first order.
type labelKey struct {
	kind      string
	id        uint64
	createdAt time.Time
}

func (r *documentLabelRepository) keys(ctx context.Context, model any, kind string, q *types.DocumentLabelQuery, want int) ([]labelKey, int64, error) {
	var n int64
	if err := r.filtered(ctx, model, q, kind == types.DocumentLabelSpelling).Count(&n).Error; err != nil {
		return nil, 0, err
	}
	var rows []struct {
		ID        uint64
		CreatedAt time.Time
	}
	if err := r.filtered(ctx, model, q, kind == types.DocumentLabelSpelling).Select("id", "created_at").
		Order("created_at DESC, id DESC").Limit(want).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]labelKey, len(rows))
	for i, row := range rows {
		out[i] = labelKey{kind: kind, id: row.ID, createdAt: row.CreatedAt}
	}
	return out, n, nil
}

func (r *documentLabelRepository) List(ctx context.Context, q *types.DocumentLabelQuery) ([]types.DocumentLabel, int64, error) {
	limit := q.Limit
	if limit <= 0 || limit > documentLabelMaxPage {
		limit = 50
	}
	offset := max(q.Offset, 0)
	if offset+limit > documentLabelMaxDepth {
		return nil, 0, interfaces.ErrDocumentLabelPageTooDeep
	}
	// Both tables' (id, created_at) are read up to offset+limit rows and
	// merged; only the page's rows (with their reports) are then loaded.
	want := offset + limit
	spelling, format := labelKinds(q)
	var keys []labelKey
	var total int64
	if spelling {
		k, n, err := r.keys(ctx, &types.DocumentSpellingFeedback{}, types.DocumentLabelSpelling, q, want)
		if err != nil {
			return nil, 0, err
		}
		keys, total = append(keys, k...), total+n
	}
	if format {
		k, n, err := r.keys(ctx, &types.DocumentFormatFeedback{}, types.DocumentLabelFormat, q, want)
		if err != nil {
			return nil, 0, err
		}
		keys, total = append(keys, k...), total+n
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if !a.createdAt.Equal(b.createdAt) {
			return a.createdAt.After(b.createdAt)
		}
		if a.kind != b.kind {
			return a.kind > b.kind // spelling before format
		}
		return a.id > b.id
	})
	if offset >= len(keys) {
		return []types.DocumentLabel{}, total, nil
	}
	keys = keys[offset:min(len(keys), offset+limit)]

	var spellingIDs, formatIDs []uint64
	for _, k := range keys {
		if k.kind == types.DocumentLabelSpelling {
			spellingIDs = append(spellingIDs, k.id)
		} else {
			formatIDs = append(formatIDs, k.id)
		}
	}
	labels := make(map[string]types.DocumentLabel, len(keys))
	if len(spellingIDs) > 0 {
		var rows []types.DocumentSpellingFeedback
		if err := r.db.WithContext(ctx).Where("id IN ?", spellingIDs).Find(&rows).Error; err != nil {
			return nil, 0, err
		}
		for i := range rows {
			l := types.SpellingLabel(&rows[i])
			labels[l.ID] = l
		}
	}
	if len(formatIDs) > 0 {
		var rows []types.DocumentFormatFeedback
		if err := r.db.WithContext(ctx).Where("id IN ?", formatIDs).Find(&rows).Error; err != nil {
			return nil, 0, err
		}
		for i := range rows {
			l := types.FormatLabel(&rows[i])
			labels[l.ID] = l
		}
	}
	out := make([]types.DocumentLabel, 0, len(keys))
	for _, k := range keys {
		if l, ok := labels[k.kind+":"+strconv.FormatUint(k.id, 10)]; ok {
			out = append(out, l)
		}
	}
	return out, total, nil
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
