package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
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
	require.Equal(t, false, spec.Default, "off until the re-review passes")
	require.False(t, legalResolverOn(ctx, nil), "no settings service: the default")
	require.False(t, legalResolverOn(ctx, resolverSwitch{}), "unset: the default")
	require.True(t, legalResolverOn(ctx, resolverSwitch{set: true, on: true}))
	require.False(t, legalResolverOn(ctx, resolverSwitch{set: true}))
	// the agents read the switch from the injected settings service
	a := &agentService{}
	a.SetLegalWordingSettings(resolverSwitch{set: true, on: true})
	require.True(t, legalResolverOn(ctx, a.legalSettings))

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
	calls = map[string]int{}

	// a panic in the resolver is a failure: the old output
	panicking := func() (map[string]string, error) { panic("index out of range") }
	require.Equal(t, oldOut, legalWordingChoice(ctx, true, panicking, old))
	require.Equal(t, "old", legalHistoryChoice(ctx, true, func() (string, error) { panic("boom") }, func() string { return "old" }))

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

// Review B1: a second copy of NĐ 141 in a KB outside the scope must not
// win over the public copy (the change would be withheld, "01 tỷ" lost).
func TestLegalAmendTenantCopyDoesNotHidePublicCopy(t *testing.T) {
	db := legalAmendTestDB(t)
	meta, _ := json.Marshal(map[string]string{"doc_number": "141/2026/NĐ-CP", "external_id": "nd141", "doc_type": "Nghị định"})
	require.NoError(t, db.Create(&types.Knowledge{ID: "k0141", KnowledgeBaseID: "kb-ten", Title: "141", ParseStatus: types.ParseStatusCompleted, Metadata: meta}).Error)
	legal := vietnamese_legal.LegalMetadata{Source: "vbpl", ArticleNos: []string{"1"}, SourceDocID: "nd141", Quoted: true,
		Amends: []vietnamese_legal.AmendRef{
			{Document: "Nghị định số 68/2026/NĐ-CP", Article: "3", Phrase: true, From: "500 triệu đồng", To: "01 tỷ đồng"},
			{Document: "Nghị định số 68/2026/NĐ-CP", Article: "8", Khoan: "5"},
		}}
	cm, _ := json.Marshal(types.DocumentChunkMetadata{Legal: &legal})
	// id "b0" sorts before the public copy "b1"
	require.NoError(t, db.Create(&types.Chunk{ID: "b0", KnowledgeID: "k0141", KnowledgeBaseID: "kb-ten", ChunkIndex: 1,
		ChunkType: types.ChunkTypeText, IsEnabled: true, Content: strings.Replace(nd141Dieu1, "01 tỷ đồng thì", "1 tỷ đồng thì", 1), Metadata: cm}).Error)
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	out, err := LegalCurrentWordingResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3", "a8"}, now)
	require.NoError(t, err)
	require.Contains(t, out["a3"], "01 tỷ đồng [thay cho “500 triệu đồng”", "the public copy wins")
	require.NotContains(t, out["a8"], "xung đột", "two copies of B are one source, not a conflict")
	require.Contains(t, out["a8"], "trên 01 tỷ đồng thì phải áp dụng", "the quote of the public copy")
}

// Review B2: history must not print B's words the wording withholds.
func TestLegalAmendHistoryWithheldPhrase(t *testing.T) {
	db := legalAmendTestDB(t)
	require.NoError(t, db.Exec(`UPDATE chunks SET knowledge_base_id='kb-ten' WHERE id='b1'`).Error)
	require.NoError(t, db.Exec(`UPDATE knowledges SET knowledge_base_id='kb-ten' WHERE id='k141'`).Error)
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	answer := "Theo khoản 1 Điều 3 Nghị định 68/2026/NĐ-CP, hộ có doanh thu từ 500 triệu đồng trở xuống không chịu thuế."
	w, err := LegalCurrentWordingResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3"}, now)
	require.NoError(t, err)
	require.NotContains(t, w["a3"], "01 tỷ")
	require.Contains(t, w["a3"], "không thuộc phạm vi được dùng")
	h, err := LegalAmendmentHistoryResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3"}, answer, now)
	require.NoError(t, err)
	require.NotContains(t, h, "01 tỷ", "§10.2: B's words from a KB outside the scope")
	require.Contains(t, h, "cụm từ “500 triệu đồng” đã được sửa đổi bởi Nghị định 141/2026/NĐ-CP, có hiệu lực từ 01/01/2026 (lời văn mới không thuộc phạm vi được dùng)")
}

// Review S4: a document's catalogue note once per result set, after the text.
func TestLegalCurrentWordingNoteOncePerDocument(t *testing.T) {
	db := legalAmendTestDB(t)
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	out, err := LegalCurrentWordingResolved(context.Background(), db, []string{"kb-pub"}, []string{"a3", "a8"}, now)
	require.NoError(t, err)
	note := "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: danh mục ghi văn bản này bị sửa đổi bởi Nghị định 200/2026/NĐ-CP, chưa rõ ngày hiệu lực]"
	require.True(t, strings.HasSuffix(out["a3"], note), out["a3"])
	require.NotContains(t, out["a8"], note, "shown on the first chunk of the document only")
}

// Review 2, S2: a panic in a loader goroutine comes back as an error.
func TestLegalGoRecovers(t *testing.T) {
	var g errgroup.Group
	legalGo(&g, func() error { var m map[string]int; m["x"] = 1; return nil })
	err := g.Wait()
	require.Error(t, err)
	require.Contains(t, err.Error(), "panic")
}
