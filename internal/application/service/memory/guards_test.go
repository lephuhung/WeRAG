package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// Rows found on the dev store (2026-11-02) and their kin: none describes the
// user, and each was being recalled into unrelated later questions.
func TestGateDropsStatementsThatAreNotAboutTheUser(t *testing.T) {
	letter := "Kính gửi: Sở Nội vụ. " + strings.Repeat("Chúng tôi đề nghị Sở xem xét hồ sơ của ông Nguyễn Văn A. ", 30)
	cases := []struct {
		source string
		want   gateVerdict
	}{
		{"chỗ HN nói là tất cả chỉ là mua usdt chứ HN k tra thuê bao internet cho anh, chỉ 1 lần thôi", gateDropReported},
		{"Anh Hùng bảo là hồ sơ của bà Mai bị trả lại vì thiếu giấy xác nhận cư trú", gateDropReported},
		{"Chị Lan: mai họp giao ban lúc 8h nhé\nAnh Tú: ok, nhớ mang hồ sơ đền bù\nChị Lan: còn thiếu biên bản", gateDropPasted},
		{"Em: em đang làm hồ sơ\nSếp: ừ làm nhanh nhé", gateDropPasted},
		{letter, gateDropPasted},
		{"giá hornor magic 9 pro", gateDropLookup},
		{"xử lý vphc trong lĩnh vực giao thông", gateDropLookup},
		{"thủ tục cấp lại thẻ căn cước bị mất", gateDropLookup},
	}
	for _, c := range cases {
		for _, kind := range []string{types.MemoryKindFact, types.MemoryKindTask, types.MemoryKindProfile} {
			_, verdict := gateDecision(extractionDecision{Kind: kind, Content: "x"}, c.source)
			require.Equal(t, c.want, verdict, "%s / %.60s", kind, c.source)
			require.True(t, verdict.dropped())
		}
	}
}

func TestGateKeepsShortStatementsAndFormAnswers(t *testing.T) {
	for _, c := range []struct{ kind, source string }{
		{types.MemoryKindTask, "Mai phải nộp báo cáo quý cho Sở Nội vụ"},
		{types.MemoryKindTask, "Tuần này phải xong báo cáo tổng kết"},
		{types.MemoryKindFact, "Bộ phận: Đội 4, Ký trực tiếp"},
		{types.MemoryKindProfile, "Tôi là chuyên viên, sếp tôi bảo là phải soát kỹ thể thức"},
		// A note asked to be kept survives inside a pasted log.
		{types.MemoryKindProfile, "Hãy nhớ: tôi làm văn thư\nChị Lan: ok\nAnh Tú: ok"},
	} {
		_, verdict := gateDecision(extractionDecision{Kind: c.kind, Content: "x", ExpiresAt: "2099-01-01"}, c.source)
		require.False(t, verdict.dropped(), "%s -> %s", c.source, verdict)
	}
}

func TestDeleteNeedsTheUserToSaySomethingChanged(t *testing.T) {
	for _, source := range []string{
		"Tôi không còn phụ trách văn thư nữa",
		"Báo cáo tổng kết xong rồi, nộp hôm qua",
		"Đã chuyển sang Phòng Tổ chức cán bộ",
		"Thôi bỏ ghi chú đó đi",
	} {
		require.True(t, deleteHasCue(source), source)
	}
	for _, source := range []string{
		"Cho tôi hỏi thủ tục đăng ký khai sinh cho trẻ cần giấy tờ gì?",
		"Tóm tắt Nghị định 30/2020 giúp tôi",
		"Bên tôi cũng dùng thêm phần mềm iGate",
	} {
		require.False(t, deleteHasCue(source), source)
	}
}

func TestApplyDecisionsGatesDeletes(t *testing.T) {
	s, _, tr := newMemoryHarness(t)
	ctx := enabledCtx(t, tr, 1, "alice")
	scope, cfg, ok := s.enabledScope(ctx)
	require.True(t, ok)
	unit, err := s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindProfile, Topic: "đơn vị công tác", Content: "Công tác tại Sở Tư pháp"})
	require.NoError(t, err)
	task, err := s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindTask, Topic: "hộ tịch", Content: "Đang rà soát hồ sơ hộ tịch tồn đọng"})
	require.NoError(t, err)
	existing := []*types.MemoryItem{unit, task}
	segment := func(line string) transcriptSegment {
		return transcriptSegment{sessionID: "s", lines: []transcriptLine{{sessionID: "s", messageID: "m1", content: line}}}
	}
	zero, one := 0, 1

	// A question gives no ground to retire anything.
	require.NoError(t, s.applyDecisions(ctx, scope, cfg, segment("Thủ tục đăng ký khai sinh cần giấy tờ gì?"), existing,
		[]extractionDecision{{Action: "delete", Target: &zero}, {Action: "delete", Target: &one}}))
	_, total, err := s.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)

	// With a cue, still only one delete per run.
	require.NoError(t, s.applyDecisions(ctx, scope, cfg, segment("Xong hết rồi, tôi cũng không còn ở Sở Tư pháp"), existing,
		[]extractionDecision{{Action: "delete", Target: &zero}, {Action: "delete", Target: &one}}))
	_, total, err = s.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
}

func TestUnrelatedFactsUnderOneTopicBothStay(t *testing.T) {
	s, _, tr := newMemoryHarness(t)
	ctx := enabledCtx(t, tr, 1, "alice")
	first, err := s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Topic: "phần mềm", Content: "Bên tôi dùng eOffice để quản lý văn bản"})
	require.NoError(t, err)
	second, err := s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Topic: "phần mềm", Content: "Đơn vị sử dụng thêm phần mềm iGate cho dịch vụ công"})
	require.NoError(t, err)
	require.NotEqual(t, first.NormalizedKey, second.NormalizedKey)
	items, total, err := s.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.EqualValues(t, 2, total, "%v", items)

	// Restating the sibling finds the sibling, not a third row.
	again, err := s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Topic: "phần mềm", Content: "Đơn vị sử dụng thêm phần mềm iGate cho dịch vụ công"})
	require.NoError(t, err)
	require.Equal(t, second.ID, again.ID)

	// A fact never retires a profile note that shares its topic.
	profile, err := s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindProfile, Topic: "nhiệm vụ", Content: "Phụ trách văn thư"})
	require.NoError(t, err)
	_, err = s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Topic: "nhiệm vụ", Content: "Phòng vừa nhận thêm việc lưu trữ"})
	require.NoError(t, err)
	reloaded, err := s.repo.GetItem(ctx, mustScope(t, s, ctx), profile.ID)
	require.NoError(t, err)
	require.Equal(t, types.MemoryStatusActive, reloaded.Status)

	// A profile note is single-valued: the newer statement replaces it.
	_, err = s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindProfile, Topic: "nhiệm vụ", Content: "Phụ trách thi đua khen thưởng"})
	require.NoError(t, err)
	reloaded, err = s.repo.GetItem(ctx, mustScope(t, s, ctx), profile.ID)
	require.NoError(t, err)
	require.Equal(t, types.MemoryStatusSuperseded, reloaded.Status)
}

func TestSiblingKeyFitsTheColumn(t *testing.T) {
	key := strings.Repeat("ữ", 120) // 360 bytes
	got := siblingMemoryKey(key, "nội dung")
	require.LessOrEqual(t, len(got), 255)
	require.True(t, strings.HasSuffix(got, "#"+types.MemoryFingerprint("nội dung")[:12]))
	require.True(t, strings.ToValidUTF8(got, "?") == got)
}

func mustScope(t *testing.T, s *Service, ctx context.Context) interfaces.MemoryScope {
	t.Helper()
	scope, _, ok := s.enabledScope(ctx)
	require.True(t, ok)
	return scope
}

// Rows distilled before a gate rule existed are held to it by the daily
// review: the four junk rows found on the dev store go, the form answer stays,
// and a question saved as a fact lives only the week a task would.
func TestRegateArchivesWhatTheGateNowRejects(t *testing.T) {
	s, db, tr := newMemoryHarness(t)
	ctx := enabledCtx(t, tr, 1, "alice")
	scope := mustScope(t, s, ctx)
	stub := &stubMessageRepo{}
	s.messageRepo = stub
	at := time.Now().UTC()
	rows := []struct{ id, kind, source, content string }{
		{"m1", types.MemoryKindFact, "giá hornor magic 9 pro", "Quan tâm giá Honor Magic 9 Pro"},
		{"m2", types.MemoryKindTask, "Văn bản đang mở có mấy đoạn, đoạn đầu là gì? Trả lời ngắn.", "Tóm tắt đoạn đầu"},
		{"m3", types.MemoryKindFact, "chỗ HN nói là tất cả chỉ là mua usdt chứ HN k tra thuê bao internet cho anh", "Đối tác HN chỉ hỗ trợ mua USDT"},
		{"m4", types.MemoryKindTask, "xử lý vphc trong lĩnh vực giao thông", "Đang xử lý vi phạm hành chính giao thông"},
		{"m5", types.MemoryKindFact, "Bộ phận: Đội 4, Ký trực tiếp", "Phụ trách Đội 4, ký trực tiếp"},
		{"m6", types.MemoryKindFact, "công thức phân bổ căn hộ cho đối tượng ưu tiên như thế nào", "Đang tìm hiểu công thức phân bổ căn hộ"},
		{"m7", types.MemoryKindFact, "công thức tính thuế hộ kinh doanh như thế nào", "Đang tìm hiểu thuế hộ kinh doanh"},
	}
	var items []*types.MemoryItem
	for i, r := range rows {
		stub.messages = append(stub.messages, &types.Message{ID: r.id, SessionID: "s", Role: "user", Content: r.source, CreatedAt: at})
		item, err := s.Remember(ctx, types.MemoryItem{Kind: r.kind, Topic: r.id, Content: r.content,
			Origin: types.MemoryOriginExtracted, SourceSessionID: "s", SourceMessageID: r.id})
		require.NoError(t, err)
		if i < 6 {
			// Written three weeks ago; m7 is fresh.
			require.NoError(t, db.Model(&types.MemoryItem{}).Where("id = ?", item.ID).
				Update("valid_from", at.Add(-21*24*time.Hour)).Error)
			item.ValidFrom = at.Add(-21 * 24 * time.Hour)
		}
		items = append(items, item)
	}
	// An item the user asked to keep is never re-judged.
	explicit, err := s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "giá hornor magic 9 pro",
		Origin: types.MemoryOriginExplicit, SourceSessionID: "s", SourceMessageID: "m1"})
	require.NoError(t, err)
	items = append(items, explicit)

	kept, archived := s.regateStored(ctx, scope, items)
	require.Equal(t, 5, archived)
	var keptIDs []string
	for _, item := range kept {
		keptIDs = append(keptIDs, item.ID)
	}
	require.ElementsMatch(t, []string{items[4].ID, items[6].ID, explicit.ID}, keptIDs)
	reloaded, err := s.repo.GetItem(ctx, scope, items[0].ID)
	require.NoError(t, err)
	require.Equal(t, types.MemoryStatusArchived, reloaded.Status)
	require.NotNil(t, reloaded.InvalidAt)
}
