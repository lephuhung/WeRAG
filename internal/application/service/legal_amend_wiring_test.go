package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// resolverSwitch is a settings service holding only the switch.
type resolverSwitch struct {
	interfaces.SystemSettingService
	on, set bool
}

func (s resolverSwitch) GetBool(_ context.Context, key, _ string, def bool) bool {
	if key != LegalWordingResolverSetting || !s.set {
		return def
	}
	return s.on
}

func TestLegalResolverSwitch(t *testing.T) {
	ctx := context.Background()
	spec, ok := registry[LegalWordingResolverSetting]
	require.True(t, ok, "the switch is a registered system setting")
	require.Equal(t, "bool", spec.Type)
	require.Equal(t, true, spec.Default)
	require.True(t, legalResolverOn(ctx, nil), "default on")
	require.True(t, legalResolverOn(ctx, resolverSwitch{}), "default on when unset")
	require.False(t, legalResolverOn(ctx, resolverSwitch{set: true}))

	oldOut := map[string]string{"c1": "old wording"}
	calls := map[string]int{}
	old := func() map[string]string { calls["old"]++; return oldOut }
	newOK := func() (map[string]string, error) { calls["new"]++; return map[string]string{"c1": "new"}, nil }
	newErr := func() (map[string]string, error) { calls["new"]++; return nil, context.DeadlineExceeded }

	// off: exactly the old function's output, the resolver never runs
	require.Equal(t, oldOut, legalWordingChoice(ctx, false, newOK, old))
	require.Equal(t, map[string]int{"old": 1}, calls)
	// on: the resolver
	require.Equal(t, map[string]string{"c1": "new"}, legalWordingChoice(ctx, true, newOK, old))
	// on, resolver fails or times out: the old output, never empty
	require.Equal(t, oldOut, legalWordingChoice(ctx, true, newErr, old))
	require.Equal(t, map[string]int{"old": 2, "new": 2}, calls)

	h := legalHistoryChoice(ctx, false, func() (string, error) { t.Fatal("resolver ran while off"); return "", nil }, func() string { return "old" })
	require.Equal(t, "old", h)
	h = legalHistoryChoice(ctx, true, func() (string, error) { return "", errors.New("boom") }, func() string { return "old" })
	require.Equal(t, "old", h)
	h = legalHistoryChoice(ctx, true, func() (string, error) { return "", nil }, func() string { return "old" })
	require.Equal(t, "", h, "an empty resolver answer is an answer")
}

func TestLegalCurrentWordingResolvedSQLite(t *testing.T) {
	db := legalAmendTestDB(t)
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	out, err := LegalCurrentWordingResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3", "a8", "missing"}, now)
	require.NoError(t, err)
	require.Contains(t, out["a3"], "01 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026]")
	require.Contains(t, out["a8"], "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: khoản 5 Điều 8 có thể đã được sửa đổi")
	require.NotContains(t, out["a3"], "Bí mật", "NĐ 200 is in a tenant KB outside the scope")

	// a timeout is an error: the caller falls back
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = LegalCurrentWordingResolved(ctx, db, []string{"kb-pub"}, []string{"a3"}, now)
	require.Error(t, err)
}

func TestLegalAmendmentHistoryResolvedSQLite(t *testing.T) {
	db := legalAmendTestDB(t)
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	answer := "Theo khoản 1 Điều 3 Nghị định 68/2026/NĐ-CP, hộ có doanh thu từ 500 triệu đồng trở xuống không chịu thuế; khoản 5 Điều 8 quy định hóa đơn điện tử."
	h, err := LegalAmendmentHistoryResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3", "a8"}, answer, now)
	require.NoError(t, err)
	require.Contains(t, h, "**Lịch sử sửa đổi**")
	require.Contains(t, h, "khoản 5 Điều 8 Nghị định 68/2026/NĐ-CP: đã được sửa đổi bởi Nghị định 141/2026/NĐ-CP, có hiệu lực từ 01/01/2026. (Chưa xác minh.) Quy định mới: “5. Sử dụng hóa đơn điện tử")
	require.Contains(t, h, "Nghị định 68/2026/NĐ-CP: cụm từ “500 triệu đồng” được thay bằng “01 tỷ đồng” bởi Nghị định 141/2026/NĐ-CP, có hiệu lực từ 01/01/2026.")
	require.NotContains(t, h, "Bí mật")

	none, err := LegalAmendmentHistoryResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3"}, "Câu trả lời không nêu văn bản nào.", now)
	require.NoError(t, err)
	require.Empty(t, none)
}
