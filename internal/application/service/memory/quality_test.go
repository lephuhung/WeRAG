package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// This file covers the extraction-quality work: what the model is shown, how
// its answer is interpreted, and what is refused. Each test names the concrete
// failure it exists to prevent, because most of them were found by probing the
// running system rather than by reading the code.

// ---------------------------------------------------------------------------
// A memory the user deleted must not come back
// ---------------------------------------------------------------------------

// TestDeletedMemoryIsNotReExtracted is the one users would hit first. An
// explicit "remember X" is stored immediately, and the debounced distillation
// for that same turn reads the same message minutes later — so deleting the
// memory in between used to be undone by the system itself.
func TestDeletedMemoryIsNotReExtracted(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})

	stored, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL chính thức",
		Content: "CSDL chính thức là PostgreSQL 17", Origin: types.MemoryOriginExplicit,
	})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteItem(ctx, stored.ID))

	messages.set("session-1", []*types.Message{
		{
			ID: "m1", SessionID: "session-1", Role: "user",
			Content: "Hãy nhớ: CSDL chính thức là PostgreSQL 17", CreatedAt: time.Now().Add(-time.Minute),
		},
	})
	models.response = `{"memories":[{"action":"add","target":null,"kind":"fact",` +
		`"topic":"CSDL chính thức","content":"CSDL chính thức là PostgreSQL 17","source":1}]}`
	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)

	_, total, err := svc.ListItems(ctx, "", 10, 0)
	require.NoError(t, err)
	require.Zero(t, total, "a memory the user deleted must not be re-extracted")
}

// TestRewordedMemoryDoesNotComeBack is the case the fingerprint alone misses:
// distillation re-reads the same message and words the statement slightly
// differently, so it hashes differently and used to slip straight through.
func TestRewordedMemoryDoesNotComeBack(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})

	stored, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Content: "Hiện nay CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh",
		Origin: types.MemoryOriginExplicit, SourceMessageID: "m1", SourceSessionID: "session-1",
	})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteItem(ctx, stored.ID))

	messages.set("session-1", []*types.Message{
		{
			ID: "m1", SessionID: "session-1", Role: "user",
			Content:   "Hãy nhớ: Hiện nay CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh",
			CreatedAt: time.Now().Add(-time.Minute),
		},
	})
	// Same fact, different wording: a different fingerprint.
	models.response = `{"memories":[{"action":"add","target":null,"kind":"fact",` +
		`"topic":"CSDL chính thức","content":"CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh","source":1}]}`
	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)

	_, total, err := svc.ListItems(ctx, "", 10, 0)
	require.NoError(t, err)
	require.Zero(t, total,
		"a re-worded restatement from the same rejected message must not come back")
}

// TestSayingItAgainLaterStillWorks keeps the suppression from turning into a
// permanent ban: the user asking again is not the system re-deriving.
func TestSayingItAgainLaterStillWorks(t *testing.T) {
	svc, tenantRepo, _, _, _ := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	stored, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Content: "CSDL chính thức chạy PostgreSQL 17",
		Origin: types.MemoryOriginExplicit, SourceMessageID: "m1",
	})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteItem(ctx, stored.ID))

	// A later turn, a later message: this is the user asking again.
	again, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Content: "CSDL chính thức đúng là chạy PostgreSQL 17",
		Origin: types.MemoryOriginExplicit, SourceMessageID: "m9",
	})
	require.NoError(t, err)
	require.NotEmpty(t, again.ID)
}

func TestForgottenTopicsAreShownToTheModel(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})

	stored, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "Địa chỉ nhà", Content: "ở phường Thuận Hòa, Huế",
	})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteItem(ctx, stored.ID))

	messages.set("session-1", []*types.Message{
		{ID: "m1", SessionID: "session-1", Role: "user", Content: "Chuyện phiếm buổi sáng", CreatedAt: time.Now()},
	})
	models.response = `{"memories":[]}`
	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)

	// The fingerprint check only catches an identical restatement, so the model
	// is also told which topics were rejected.
	require.Contains(t, models.lastPrompt, "Địa chỉ nhà")
	require.NotContains(t, models.lastPrompt, "ở phường Thuận Hòa, Huế",
		"a tombstone must not retain the statement the user asked to forget")
}

func TestClearLeavesTombstones(t *testing.T) {
	svc, tenantRepo, _, _, _ := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL", Content: "CSDL chính thức là PostgreSQL",
	})
	require.NoError(t, err)
	_, err = svc.Clear(ctx)
	require.NoError(t, err)

	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL", Content: "CSDL chính thức là PostgreSQL",
	})
	require.ErrorIs(t, err, ErrPreviouslyForgotten,
		"clearing is a rejection of everything, not just a bulk delete")
}

func TestForgettingDoesNotBlockGenuinelyNewInformation(t *testing.T) {
	svc, tenantRepo, _, _, _ := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	stored, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL đang dùng", Content: "CSDL chính thức là MySQL",
	})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteItem(ctx, stored.ID))

	// Same topic, different statement: the user moved on, and suppressing this
	// would make deleting one memory quietly ban a subject forever.
	updated, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL đang dùng", Content: "CSDL chính thức đã chuyển sang PostgreSQL",
	})
	require.NoError(t, err)
	require.Equal(t, "CSDL chính thức đã chuyển sang PostgreSQL", updated.Content)
}

// ---------------------------------------------------------------------------
// Provenance
// ---------------------------------------------------------------------------

// TestProvenancePointsAtTheRightMessage covers the regression that arrived with
// multi-session runs: every memory used to be attributed to the turn that
// triggered the run, which could be a different conversation entirely.
func TestProvenancePointsAtTheRightMessage(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})

	base := time.Now().Add(-time.Hour)
	messages.set("session-a", []*types.Message{
		{
			ID: "msg-a1", SessionID: "session-a", Role: "user",
			Content: "Tôi đang làm số hóa hồ sơ lưu trữ", CreatedAt: base,
		},
	})
	messages.set("session-b", []*types.Message{
		{
			ID: "msg-b1", SessionID: "session-b", Role: "user",
			Content: "Tiện thể hỏi thời tiết", CreatedAt: base.Add(time.Second),
		},
	})
	models.response = `{"memories":[{"action":"add","target":null,"kind":"profile",` +
		`"topic":"Nghề nghiệp","content":"đang làm số hóa hồ sơ lưu trữ","source":1}]}`

	svc.ScheduleExtraction(ctx, "session-a", "trigger-msg", "model-1")
	svc.ScheduleExtraction(ctx, "session-b", "trigger-msg", "model-1")
	drainExtractions(t, svc, enqueuer)

	items, _, err := svc.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.NotEmpty(t, items)
	for _, item := range items {
		if item.Content != "đang làm số hóa hồ sơ lưu trữ" {
			continue
		}
		require.Equal(t, "session-a", item.SourceSessionID)
		require.Equal(t, "msg-a1", item.SourceMessageID)
		return
	}
	t.Fatal("the extracted memory was not found")
}

func TestOutOfRangeSourceFallsBackInsideTheSegment(t *testing.T) {
	segment := transcriptSegment{lines: []transcriptLine{
		{sessionID: "s1", messageID: "m1", content: "Câu thứ nhất"},
		{sessionID: "s1", messageID: "m2", content: "Câu thứ hai"},
	}}
	bogus := 99
	resolved := extractionDecision{Source: &bogus}.resolveSource(segment)
	require.Equal(t, "m1", resolved.messageID,
		"a hallucinated line number must still land inside the right conversation")
}

// ---------------------------------------------------------------------------
// Prior context
// ---------------------------------------------------------------------------

// TestPriorContextResolvesAReferringStatement: a run sees only what is new, so
// without a lead-in a turn like "Thì dùng cái trước đó đi" has nothing to resolve against.
func TestPriorContextResolvesAReferringStatement(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-time.Hour)
	messages.set("session-1", []*types.Message{
		{
			ID: "m1", SessionID: "session-1", Role: "user",
			Content: "Tôi đang đánh giá PostgreSQL và MySQL", CreatedAt: base,
		},
	})
	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)

	messages.set("session-1", []*types.Message{
		{
			ID: "m1", SessionID: "session-1", Role: "user",
			Content: "Tôi đang đánh giá PostgreSQL và MySQL", CreatedAt: base,
		},
		{
			ID: "m2", SessionID: "session-1", Role: "user",
			Content: "Thì dùng cái trước đó đi", CreatedAt: base.Add(time.Minute),
		},
	})
	svc.ScheduleExtraction(ctx, "session-1", "m2", "model-1")
	drainExtractions(t, svc, enqueuer)

	require.Contains(t, models.lastPrompt, "Tôi đang đánh giá",
		"a referring statement needs the turn it refers to")
	require.NotContains(t, transcriptBlock(models.lastPrompt), "Tôi đang đánh giá",
		"context must not be extracted from a second time")
}

// ---------------------------------------------------------------------------
// Segmentation
// ---------------------------------------------------------------------------

// TestLongSilenceStartsANewSegment keeps one call from having to make sense of
// two unrelated situations at once.
func TestLongSilenceStartsANewSegment(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-24 * time.Hour)
	messages.set("session-1", []*types.Message{
		{ID: "m1", SessionID: "session-1", Role: "user", Content: "Chuyện bàn buổi sáng", CreatedAt: base},
		{
			ID: "m2", SessionID: "session-1", Role: "user", Content: "Chuyện bàn buổi tối",
			CreatedAt: base.Add(6 * time.Hour),
		},
	})
	svc.ScheduleExtraction(ctx, "session-1", "m2", "model-1")
	drainExtractions(t, svc, enqueuer)

	require.Equal(t, 2, models.calls, "a six-hour gap must split the run into two calls")
	seen := models.seenTranscripts()
	require.Contains(t, seen, "Chuyện bàn buổi sáng")
	require.Contains(t, seen, "Chuyện bàn buổi tối")
}

func TestSeparateSessionsAreSeparateSegments(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-time.Hour)
	messages.set("session-a", []*types.Message{
		{ID: "a1", SessionID: "session-a", Role: "user", Content: "lời ở cuộc trò chuyện A", CreatedAt: base},
	})
	messages.set("session-b", []*types.Message{
		{
			ID: "b1", SessionID: "session-b", Role: "user", Content: "lời ở cuộc trò chuyện B",
			CreatedAt: base.Add(time.Second),
		},
	})
	svc.ScheduleExtraction(ctx, "session-a", "a1", "model-1")
	svc.ScheduleExtraction(ctx, "session-b", "b1", "model-1")
	drainExtractions(t, svc, enqueuer)

	require.Equal(t, 2, models.calls)
	for _, prompt := range models.prompts {
		block := transcriptBlock(prompt)
		require.False(t, strings.Contains(block, "lời ở cuộc trò chuyện A") && strings.Contains(block, "lời ở cuộc trò chuyện B"),
			"two conversations must not be merged into one call")
	}
}

// TestSegmentCapStillCoversEverything: capping the calls one run makes must
// delay work, never lose it.
func TestSegmentCapStillCoversEverything(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-100 * time.Hour)
	var transcript []*types.Message
	for i := 0; i < extractMaxSegmentsPerRun*2+1; i++ {
		transcript = append(transcript, &types.Message{
			ID: fmt.Sprintf("m%d", i), SessionID: "session-1", Role: "user",
			Content:   fmt.Sprintf("Lời ở đoạn thứ %d trong phiên", i),
			CreatedAt: base.Add(time.Duration(i) * 3 * time.Hour),
		})
	}
	messages.set("session-1", transcript)
	svc.ScheduleExtraction(ctx, "session-1", "m0", "model-1")
	drainExtractions(t, svc, enqueuer)

	seen := models.seenTranscripts()
	for i := 0; i < extractMaxSegmentsPerRun*2+1; i++ {
		require.Contains(t, seen, fmt.Sprintf("Lời ở đoạn thứ %d trong phiên", i),
			"segment %d was never read", i)
	}
}

// ---------------------------------------------------------------------------
// Decision handling
// ---------------------------------------------------------------------------

// TestEmptyActionIsIgnored: a truncated response used to be treated as add,
// which turned a broken model reply into a silent write.
func TestEmptyActionIsIgnored(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	messages.set("session-1", []*types.Message{
		{ID: "m1", SessionID: "session-1", Role: "user", Content: "Chuyện phiếm buổi sáng", CreatedAt: time.Now()},
	})
	models.response = `{"memories":[{"kind":"fact","topic":"t","content":"một sự việc"},` +
		`{"action":"none","kind":"fact","topic":"t2","content":"một sự việc khác"}]}`

	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)

	_, total, err := svc.ListItems(ctx, "", 10, 0)
	require.NoError(t, err)
	require.Zero(t, total, "only an explicit add/update/delete may write")
}

// TestUpdateAddressesTheNoteByIndex is the anti-hallucination measure: a model
// that mis-types a topic would otherwise silently create a duplicate.
func TestUpdateAddressesTheNoteByIndex(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL đang dùng", Content: "CSDL chính thức là MySQL",
	})
	require.NoError(t, err)

	messages.set("session-1", []*types.Message{
		{
			ID: "m1", SessionID: "session-1", Role: "user",
			Content: "Đơn vị tôi đã chuyển sang PostgreSQL", CreatedAt: time.Now(),
		},
	})
	// The topic is deliberately misspelled; the index is what must win.
	models.response = `{"memories":[{"action":"delete","target":0,"kind":"fact",` +
		`"topic":"CSDL đang dung","content":"Không còn dùng MySQL","source":1}]}`

	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)

	_, active, err := svc.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.Zero(t, active, "delete must find the note by index despite the wrong topic")
}

func TestDuplicateTopicsInOneResponseDoNotChurn(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	messages.set("session-1", []*types.Message{
		{ID: "m1", SessionID: "session-1", Role: "user", Content: "Đơn vị tôi dùng PostgreSQL 17, nhắc lại hai lần", CreatedAt: time.Now()},
	})
	models.response = `{"memories":[
		{"action":"add","target":null,"kind":"fact","topic":"CSDL","content":"dùng PostgreSQL","source":1},
		{"action":"add","target":null,"kind":"fact","topic":"CSDL","content":"dùng PostgreSQL 17","source":1}]}`

	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)

	_, superseded, err := svc.ListItems(ctx, types.MemoryStatusSuperseded, 10, 0)
	require.NoError(t, err)
	require.Zero(t, superseded, "one run must not supersede its own output")
}

// ---------------------------------------------------------------------------
// Expiry
// ---------------------------------------------------------------------------

func TestExpiredTaskLeavesTheContext(t *testing.T) {
	svc, tenantRepo, _, _, _ := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(48 * time.Hour)
	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindTask, Topic: "Việc đã quá hạn", Content: "báo cáo tuần phải nộp tuần trước",
		ExpiresAt: &past,
	})
	require.NoError(t, err)
	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindTask, Topic: "Việc đang làm", Content: "báo cáo tuần phải nộp tuần này",
		ExpiresAt: &future,
	})
	require.NoError(t, err)

	prompt := svc.Recall(ctx, "báo cáo tuần đến đâu rồi").Prompt
	require.Contains(t, prompt, "báo cáo tuần phải nộp tuần này")
	require.NotContains(t, prompt, "báo cáo tuần phải nộp tuần trước",
		"an expired task must stop being recalled")
}

func TestParseExpiryRejectsUnusableDates(t *testing.T) {
	require.Nil(t, parseExpiry(""))
	require.Nil(t, parseExpiry("null"))
	require.Nil(t, parseExpiry("thứ sáu tuần sau"))
	require.Nil(t, parseExpiry("2020-01-01"), "a date already past would be stored and archived at once")
	require.NotNil(t, parseExpiry(time.Now().Add(72*time.Hour).Format("2006-01-02")))
}

// ---------------------------------------------------------------------------
// Workspace instructions
// ---------------------------------------------------------------------------

func TestWorkspaceInstructionsReachThePrompt(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
		ExtractInstructions: "Không bao giờ ghi lại tên của công dân",
	})
	models.response = `{"memories":[]}`
	messages.set("session-1", []*types.Message{
		{ID: "m1", SessionID: "session-1", Role: "user", Content: "Chuyện phiếm buổi sáng", CreatedAt: time.Now()},
	})

	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)
	require.Contains(t, models.lastPrompt, "Không bao giờ ghi lại tên của công dân")
}

// ---------------------------------------------------------------------------
// Structured output
// ---------------------------------------------------------------------------

func TestExtractionRequestsStructuredOutput(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`
	messages.set("session-1", []*types.Message{
		{ID: "m1", SessionID: "session-1", Role: "user", Content: "Chuyện phiếm buổi sáng", CreatedAt: time.Now()},
	})

	svc.ScheduleExtraction(ctx, "session-1", "m1", "model-1")
	drainExtractions(t, svc, enqueuer)

	require.NotEmpty(t, models.lastFormat,
		"the response schema must be sent, not just described in prose")
	require.Contains(t, string(models.lastFormat), "memories")
}

var _ = context.Background
