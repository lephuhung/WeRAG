package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type retentionSettings struct{ days int64 }

func (s retentionSettings) GetInt(context.Context, string, string, int64) int64 { return s.days }

type retentionFixture struct {
	db      *gorm.DB
	w       *LegalExpiredRetention
	now     time.Time
	removed []string
}

func newRetentionFixture(t *testing.T, days int64) *retentionFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, owner_tenant_id INTEGER, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, metadata TEXT, parse_status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE legal_documents (id TEXT PRIMARY KEY, source TEXT, status TEXT, document_number TEXT, number_key TEXT, doc_type TEXT, issuer TEXT, issued_on DATE)`,
		`CREATE TABLE legal_feed_log (id INTEGER PRIMARY KEY, action TEXT, vbpl_id TEXT, knowledge_id TEXT, knowledge_base_id TEXT,
			document_number TEXT, reason TEXT, actor_endpoint_id TEXT, actor_tenant_id INTEGER, detail TEXT, created_at DATETIME)`,
		`INSERT INTO knowledge_bases VALUES ('pub', 0, NULL), ('priv', 7, NULL)`,
		`INSERT INTO legal_documents VALUES ('exp', 'vbpl', 'Hết hiệu lực toàn bộ', '1/2020/ND-CP', 'k1', 'Nghị định', 'Chính phủ', '2020-01-05'),
			('live', 'vbpl', 'Còn hiệu lực', '2/2020/ND-CP', 'k2', 'Nghị định', 'Chính phủ', '2020-01-05'),
			('private', 'vbpl', 'Hết hiệu lực toàn bộ', '3/2020/ND-CP', 'k3', 'Nghị định', 'Chính phủ', '2020-01-05'),
			('feed', 'tvpl', 'Hết hiệu lực toàn bộ', '4/2020/ND-CP', 'k4', 'Nghị định', 'Chính phủ', '2020-01-05')`,
		`INSERT INTO knowledges VALUES
			('k-exp', 'pub', '{"external_id":"exp"}', 'completed', NULL),
			('k-live', 'pub', '{"external_id":"live"}', 'completed', NULL),
			('k-private', 'priv', '{"external_id":"private"}', 'completed', NULL),
			('k-feed', 'pub', '{"external_id":"feed"}', 'completed', NULL)`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
	f := &retentionFixture{db: db, now: time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)}
	f.w = &LegalExpiredRetention{db: db, kbs: autoKBs{}, tenants: autoTenants{}, settings: retentionSettings{days},
		now: func() time.Time { return f.now }, remove: func(ctx context.Context, id string) error {
			if ctx.Value(types.KBGrantsContextKey) == nil || ctx.Value(types.TenantInfoContextKey) == nil {
				t.Errorf("%s removed without a grant and tenant info", id)
			}
			f.removed = append(f.removed, id)
			return db.Exec(`UPDATE knowledges SET deleted_at = '2026-01-01' WHERE id = ?`, id).Error
		}}
	return f
}

func (f *retentionFixture) stamp(t *testing.T, id string) string {
	t.Helper()
	var s *string
	require.NoError(t, f.db.Raw(`SELECT metadata->>'eff_expired_at' FROM knowledges WHERE id = ?`, id).Row().Scan(&s))
	if s == nil {
		return ""
	}
	return *s
}

func TestLegalExpiredRetentionKeepsTheTextForTheWholePeriod(t *testing.T) {
	f := newRetentionFixture(t, 15)
	ctx := context.Background()

	require.Equal(t, 0, f.w.Tick(ctx), "the first pass only starts the clock")
	require.NotEmpty(t, f.stamp(t, "k-exp"))
	require.Empty(t, f.stamp(t, "k-live"))
	require.Empty(t, f.stamp(t, "k-private"), "only public KBs")
	require.Empty(t, f.stamp(t, "k-feed"), "only documents of the portal")

	f.now = f.now.AddDate(0, 0, 14)
	require.Equal(t, 0, f.w.Tick(ctx))
	first := f.stamp(t, "k-exp")
	require.Equal(t, "2026-11-09T10:00:00Z", first, "the clock does not restart")

	f.now = f.now.AddDate(0, 0, 1)
	require.Equal(t, 1, f.w.Tick(ctx))
	require.Equal(t, []string{"k-exp"}, f.removed)

	var status, number string
	require.NoError(t, f.db.Raw(`SELECT status, document_number FROM legal_documents WHERE id = 'exp'`).Row().Scan(&status, &number))
	require.Equal(t, "1/2020/ND-CP", number, "the catalogue row stays")
	var logged int64
	f.db.Raw(`SELECT count(*) FROM legal_feed_log WHERE action = ? AND vbpl_id = 'exp'`, legalRetentionRemovedAs).Scan(&logged)
	require.EqualValues(t, 1, logged)
}

func TestLegalExpiredRetentionClearsTheClockWhenBackInForce(t *testing.T) {
	f := newRetentionFixture(t, 15)
	ctx := context.Background()
	f.w.Tick(ctx)
	require.NotEmpty(t, f.stamp(t, "k-exp"))
	require.NoError(t, f.db.Exec(`UPDATE legal_documents SET status = 'Hết hiệu lực một phần' WHERE id = 'exp'`).Error)
	f.now = f.now.AddDate(0, 0, 30)
	require.Equal(t, 0, f.w.Tick(ctx))
	require.Empty(t, f.stamp(t, "k-exp"))
	require.Empty(t, f.removed)
}

func TestLegalExpiredRetentionKeepsTextWhileACopyIsInForce(t *testing.T) {
	f := newRetentionFixture(t, 15)
	ctx := context.Background()
	require.NoError(t, f.db.Exec(`INSERT INTO legal_documents VALUES
		('twin', 'vbpl', 'Còn hiệu lực', '1/2020/ND-CP', 'k1', 'Nghị định', 'Chính phủ', '2020-01-05')`).Error)
	f.w.Tick(ctx)
	f.now = f.now.AddDate(0, 0, 20)
	require.Equal(t, 0, f.w.Tick(ctx))
	require.Empty(t, f.removed)

	// The same number on another document (other day) does not protect it.
	require.NoError(t, f.db.Exec(`UPDATE legal_documents SET issued_on = '2021-06-01' WHERE id = 'twin'`).Error)
	require.Equal(t, 1, f.w.Tick(ctx))
}

func TestLegalExpiredRetentionUsesTheSettingAndNeverZero(t *testing.T) {
	f := newRetentionFixture(t, 3)
	ctx := context.Background()
	f.w.Tick(ctx)
	f.now = f.now.AddDate(0, 0, 3)
	require.Equal(t, 1, f.w.Tick(ctx), "a shorter period from the setting")

	g := newRetentionFixture(t, 0)
	g.w.Tick(ctx)
	g.now = g.now.AddDate(0, 0, 14)
	require.Equal(t, 0, g.w.Tick(ctx), "0 falls back to the default")
}
