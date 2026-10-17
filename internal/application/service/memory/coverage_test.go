package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

// This file is about one property: while memory is switched on, every user
// message is eventually read by distillation.
//
// It exists because the first version of the scheduler compared the current
// time against the last run and returned early inside the interval, which
// silently discarded every turn in that window — the feature looked enabled and
// quietly learned nothing. Timers may delay a message; they may not lose it.

func userMessage(sessionID, content string, at time.Time) *types.Message {
	return &types.Message{
		ID:        content,
		SessionID: sessionID,
		Role:      "user",
		Content:   content,
		CreatedAt: at,
	}
}

// drainExtractions runs every task the service queued, plus any follow-ups
// those runs queue, until the queue is empty. Bounded so a scheduling bug
// shows up as a failure rather than a hang.
func drainExtractions(t *testing.T, svc *Service, enqueuer *stubEnqueuer) int {
	t.Helper()
	runs := 0
	for i := 0; i < 50; i++ {
		task := enqueuer.pop()
		if task == nil {
			return runs
		}
		require.NoError(t, svc.Handle(context.Background(), task))
		runs++
	}
	t.Fatal("extraction did not settle: follow-up tasks kept queueing")
	return runs
}

// TestEveryTurnIsEventuallyRead is the headline guarantee. Turns arrive faster
// than the debounce window, so most of them are recorded while a run is already
// in flight; all of them must still reach the model.
func TestEveryTurnIsEventuallyRead(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto,
		ExtractDelaySeconds: 5, ExtractMinIntervalSeconds: 1,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-time.Hour)
	var transcript []*types.Message
	for i := 0; i < 12; i++ {
		content := fmt.Sprintf("Câu thứ %d trong phiên", i)
		transcript = append(transcript, userMessage("session-1", content, base.Add(time.Duration(i)*time.Second)))
		messages.set("session-1", transcript)
		svc.ScheduleExtraction(ctx, "session-1", fmt.Sprintf("message-%d", i), "model-1")
	}

	drainExtractions(t, svc, enqueuer)

	seen := models.seenTranscripts()
	for i := 0; i < 12; i++ {
		require.Contains(t, seen, fmt.Sprintf("Câu thứ %d trong phiên", i),
			"turn %d was never read by distillation", i)
	}
}

// TestTurnsDuringARunAreNotLost covers the narrow window that the queue exists
// for: a message that arrives after a run has already taken its work.
func TestTurnsDuringARunAreNotLost(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-time.Hour)
	messages.set("session-1", []*types.Message{userMessage("session-1", "Câu thứ nhất", base)})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")

	first := enqueuer.pop()
	require.NotNil(t, first)

	// The second turn lands while the first run is still queued.
	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Câu thứ nhất", base),
		userMessage("session-1", "Câu thứ hai", base.Add(time.Second)),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-2", "model-1")

	require.NoError(t, svc.Handle(context.Background(), first))
	drainExtractions(t, svc, enqueuer)

	seen := models.seenTranscripts()
	require.Contains(t, seen, "Câu thứ nhất")
	require.Contains(t, seen, "Câu thứ hai")
}

// TestMessagesBeyondOneRunsCapAreFollowedUp covers a subject who said more in
// one window than a single run is allowed to read.
func TestMessagesBeyondOneRunsCapAreFollowedUp(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-time.Hour)
	total := extractMaxMessagesPerRun*2 + 5
	var transcript []*types.Message
	for i := 0; i < total; i++ {
		transcript = append(transcript,
			userMessage("session-1", fmt.Sprintf("Tin nhắn thứ %d trong phiên", i), base.Add(time.Duration(i)*time.Second)))
	}
	messages.set("session-1", transcript)
	svc.ScheduleExtraction(ctx, "session-1", "message-last", "model-1")

	runs := drainExtractions(t, svc, enqueuer)
	require.Greater(t, runs, 1, "a backlog larger than one run must produce follow-up runs")

	seen := models.seenTranscripts()
	require.Contains(t, seen, "Tin nhắn thứ 0 trong phiên", "the oldest unread message must not be skipped")
	require.Contains(t, seen, fmt.Sprintf("Tin nhắn thứ %d trong phiên", total-1))
}

// TestParallelSessionsAreAllRead: a person talking in two conversations must
// not have one of them ignored because the other triggered the run.
func TestParallelSessionsAreAllRead(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-time.Hour)
	messages.set("session-a", []*types.Message{userMessage("session-a", "Lời nói ở cuộc trò chuyện A", base)})
	messages.set("session-b", []*types.Message{userMessage("session-b", "Lời nói ở cuộc trò chuyện B", base.Add(time.Second))})

	svc.ScheduleExtraction(ctx, "session-a", "message-a", "model-1")
	svc.ScheduleExtraction(ctx, "session-b", "message-b", "model-1")
	drainExtractions(t, svc, enqueuer)

	seen := models.seenTranscripts()
	require.Contains(t, seen, "Lời nói ở cuộc trò chuyện A")
	require.Contains(t, seen, "Lời nói ở cuộc trò chuyện B")
}

// TestAlreadyReadMessagesAreNotReread keeps the guarantee from degenerating
// into "read everything every time", which would make cost grow with history.
func TestAlreadyReadMessagesAreNotReread(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-time.Hour)
	messages.set("session-1", []*types.Message{userMessage("session-1", "Câu cũ", base)})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")
	drainExtractions(t, svc, enqueuer)
	require.Equal(t, 1, models.calls)

	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Câu cũ", base),
		userMessage("session-1", "Câu mới", base.Add(time.Minute)),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-2", "model-1")
	drainExtractions(t, svc, enqueuer)

	require.Equal(t, 2, models.calls)
	// The earlier message may appear as read-only context, but it must not be
	// inside the block the model extracts from, or it would be re-derived into
	// a memory on every run.
	transcript := transcriptBlock(models.lastPrompt)
	require.Contains(t, transcript, "Câu mới")
	require.NotContains(t, transcript, "Câu cũ",
		"a message already behind the watermark must not be extracted from twice")
	require.Contains(t, models.lastPrompt, "context only",
		"the earlier turn should still be visible as context")
}

// TestFailedRunLeavesMessagesUnread: a model error must not consume the
// messages it failed on, or a transient outage would silently erase them.
func TestFailedRunLeavesMessagesUnread(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 5,
	})

	base := time.Now().Add(-time.Hour)
	messages.set("session-1", []*types.Message{userMessage("session-1", "Câu quan trọng", base)})
	models.failNext = true
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")

	task := enqueuer.pop()
	require.NotNil(t, task)
	require.Error(t, svc.Handle(context.Background(), task))

	// The next turn schedules a fresh run, which must see the message again.
	models.response = `{"memories":[]}`
	svc.ScheduleExtraction(ctx, "session-1", "message-2", "model-1")
	drainExtractions(t, svc, enqueuer)
	require.Contains(t, models.seenTranscripts(), "Câu quan trọng")
}

// TestScheduleUsesTheConfiguredDelay pins that the timers are configuration,
// not constants.
func TestScheduleUsesTheConfiguredDelay(t *testing.T) {
	svc, tenantRepo, _, _, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 7,
	})

	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")
	require.Len(t, enqueuer.options, 1)
	require.Equal(t, 7*time.Second, enqueuer.options[0].processIn)
}

// TestMinIntervalDefersInsteadOfDropping is the exact behaviour change: a turn
// arriving soon after a run is queued further out, not discarded.
func TestMinIntervalDefersInsteadOfDropping(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto,
		ExtractDelaySeconds: 5, ExtractMinIntervalSeconds: 600,
	})
	models.response = `{"memories":[]}`

	base := time.Now().Add(-time.Hour)
	messages.set("session-1", []*types.Message{userMessage("session-1", "Câu thứ nhất", base)})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")
	first := enqueuer.pop()
	require.NotNil(t, first)
	require.NoError(t, svc.Handle(context.Background(), first))

	// Immediately after a run: well inside the ten-minute floor.
	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Câu thứ nhất", base),
		userMessage("session-1", "Câu thứ hai", base.Add(time.Second)),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-2", "model-1")

	require.Len(t, enqueuer.tasks, 1, "the turn must still be scheduled, not dropped")
	last := enqueuer.options[len(enqueuer.options)-1]
	require.Greater(t, last.processIn, 5*time.Second,
		"the minimum interval must push the run out rather than discard the turn")

	require.NoError(t, svc.Handle(context.Background(), enqueuer.pop()))
	require.Contains(t, models.seenTranscripts(), "Câu thứ hai")
}

// TestNothingIsScheduledWhileMemoryIsOff is the other half of the promise: the
// guarantee applies while the switch is on, and costs nothing while it is off.
func TestNothingIsScheduledWhileMemoryIsOff(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	tenantRepo.set(1, &types.MemoryConfig{Enabled: false})
	ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(1))
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "alice"})

	messages.set("session-1", []*types.Message{userMessage("session-1", "Một câu", time.Now())})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")

	require.Empty(t, enqueuer.tasks)
	require.Zero(t, models.calls)
}

// transcriptBlock returns just the part of the prompt the model is asked to
// extract from, so a test can distinguish "shown as context" from "extracted".
func transcriptBlock(prompt string) string {
	start := strings.Index(prompt, "<transcript>")
	end := strings.Index(prompt, "</transcript>")
	if start < 0 || end <= start {
		return ""
	}
	return prompt[start:end]
}

var (
	_ = json.Marshal
	_ asynq.Task
)

// Distillation runs on a worker whose context carries no principal — its scope
// travels in the task payload. Anything the distiller calls therefore has to be
// handed that scope explicitly. When topic counting re-derived the scope from
// the context instead, it silently counted nothing: extraction looked healthy,
// memories were written, and interests never appeared.
func TestTopicsAreCountedOnTheBackgroundWorker(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto,
		ExtractDelaySeconds: 1, InterestThreshold: 2,
	})
	models.response = `{"memories":[],"topics":["số hóa hồ sơ lưu trữ"]}`

	base := time.Now().Add(-time.Hour)
	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Số hóa hồ sơ lưu trữ theo quy trình ra sao", base),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")
	drainExtractions(t, svc, enqueuer)

	scope, err := ResolveScope(ctx)
	require.NoError(t, err)
	stats, err := svc.repo.TopTopics(context.Background(), scope, 10)
	require.NoError(t, err)
	require.Len(t, stats, 1, "the worker must be able to count topics without a request context")
	require.Equal(t, "số hóa hồ sơ lưu trữ", stats[0].Topic)
	require.Equal(t, 1, stats[0].Hits)
}

// A model that returns nothing must not be mistaken for a conversation with
// nothing in it.
//
// This is the failure the token ceiling actually produces in the field: a
// reasoning model spends the whole completion budget on its own deliberation
// and returns an empty string with finish_reason=length. Treating that as
// "nothing worth recording" advanced the watermark over messages no model had
// ever read, so the run reported success, the coverage guarantee held on paper,
// and the feature learned nothing — silently, forever.
func TestATruncatedRunDoesNotSwallowTheMessages(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 1,
	})
	// Truncate every attempt, including the retry with more room.
	models.truncateUntilCall = 99

	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Tôi đang làm số hóa hồ sơ lưu trữ", time.Now().Add(-time.Hour)),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")

	task := enqueuer.pop()
	require.NotNil(t, task)
	require.NoError(t, svc.Handle(context.Background(), task))
	require.NotEmpty(t, enqueuer.tasks, "invalid output must schedule another attempt")

	scope, err := ResolveScope(ctx)
	require.NoError(t, err)
	subject, err := svc.repo.GetSubject(context.Background(), scope)
	require.NoError(t, err)
	require.True(t, subject.ExtractCursor == nil || subject.ExtractCursor.IsZero(),
		"the watermark must not advance over messages the model never read")

	// The same message is still there to be read once the model can answer.
	models.truncateUntilCall = 0
	models.response = `{"memories":[{"action":"add","kind":"profile","topic":"Nghề nghiệp",` +
		`"content":"đang làm số hóa hồ sơ lưu trữ","importance":4,"source":1}]}`
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")
	drainExtractions(t, svc, enqueuer)

	_, total, err := svc.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total, "the message must still be distilled after the model recovers")
}

// A model that only needs more room gets it, without the caller ever seeing a
// failure.
func TestTruncationIsRetriedWithMoreRoom(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 1,
	})
	models.truncateUntilCall = 1
	models.response = `{"memories":[{"action":"add","kind":"profile","topic":"Nghề nghiệp",` +
		`"content":"đang làm số hóa hồ sơ lưu trữ","importance":4,"source":1}]}`

	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Tôi đang làm số hóa hồ sơ lưu trữ", time.Now().Add(-time.Hour)),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")
	drainExtractions(t, svc, enqueuer)

	require.Greater(t, models.lastBudgetAsked(), extractBudgetTokens,
		"the retry has to offer more room than the attempt that ran out of it")

	_, total, err := svc.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
}

// Every other structured-output call in this codebase disables thinking. The
// memory calls are a classification job with a fixed schema, so reasoning buys
// nothing and on a model that reasons by default it eats the whole budget.
func TestExtractionDoesNotAskTheModelToThink(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, ExtractDelaySeconds: 1,
	})
	models.response = `{"memories":[]}`

	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Chuyện phiếm buổi sáng", time.Now().Add(-time.Hour)),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "model-1")
	drainExtractions(t, svc, enqueuer)

	thinking := models.lastThinkingAsked()
	require.NotNil(t, thinking, "leaving it unset defers to the model, which is how this broke")
	require.False(t, *thinking)
}

// "Blank extraction model" is the default and the settings UI promises it means
// "use the model the conversation used". When nothing can be resolved, the run
// used to log and return success, which advanced the watermark over messages no
// model had read. A workspace on defaults therefore had memory enabled, tasks
// succeeding, and nothing whatsoever learned.
func TestNoAvailableModelDoesNotConsumeTheMessages(t *testing.T) {
	svc, tenantRepo, messages, _, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto,
		ExtractModelID: "", ExtractDelaySeconds: 1,
	})

	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Tôi đang làm số hóa hồ sơ lưu trữ", time.Now().Add(-time.Hour)),
	})
	// Schedule with no conversation model either, which is what the QA path
	// actually passes: the effective model is resolved inside the pipeline and
	// never written back onto the message.
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "")

	task := enqueuer.pop()
	require.NotNil(t, task)
	require.Error(t, svc.Handle(context.Background(), task))

	scope, err := ResolveScope(ctx)
	require.NoError(t, err)
	subject, err := svc.repo.GetSubject(context.Background(), scope)
	require.NoError(t, err)
	require.True(t, subject.ExtractCursor == nil || subject.ExtractCursor.IsZero(),
		"messages no model ever read must not be marked as read")
}

// The model tier of topic resolution has to use the same fallback the
// extraction call does. While it read the configured model directly, a default
// workspace lost the tier entirely — and losing it looks exactly like the
// symptom that led here: several wordings of one subject, each in its own row,
// each stuck at one hit, none ever reaching the threshold.
func TestTopicResolutionUsesTheSameModelFallbackAsExtraction(t *testing.T) {
	svc, tenantRepo, messages, models, enqueuer := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto,
		ExtractModelID: "", ExtractDelaySeconds: 1, InterestThreshold: 3,
	})
	scope, err := ResolveScope(ctx)
	require.NoError(t, err)

	models.responseFor = map[string]string{
		"You maintain the list of subjects one person keeps asking about": `{"resolutions":[{"index":0,"same_as":0}]}`,
	}
	models.response = `{"memories":[],"topics":["hạn mức tiếp nhận hồ sơ"]}`
	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Danh sách thí sinh dự thi tra ở đâu", time.Now().Add(-2*time.Hour)),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-1", "conversation-model")
	drainExtractions(t, svc, enqueuer)

	// A second, lexically distant wording of the same subject. Only the model
	// tier can resolve it, and it only runs if the fallback is applied.
	models.response = `{"memories":[],"topics":["ngưỡng số hồ sơ một cửa mỗi ngày"]}`
	messages.set("session-1", []*types.Message{
		userMessage("session-1", "Danh sách thí sinh dự thi tra ở đâu", time.Now().Add(-2*time.Hour)),
		userMessage("session-1", "Số thí sinh vào vòng chung kết là bao nhiêu", time.Now().Add(-time.Hour)),
	})
	svc.ScheduleExtraction(ctx, "session-1", "message-2", "conversation-model")
	drainExtractions(t, svc, enqueuer)

	stats, err := svc.repo.TopTopics(context.Background(), scope, 10)
	require.NoError(t, err)
	require.Len(t, stats, 1, "both wordings are one subject, so there is one row")
	// The exact count depends on how the run happened to segment the
	// transcript, which is not what this test is about. What matters is that a
	// second wording advanced the count instead of starting its own row.
	require.GreaterOrEqual(t, stats[0].Hits, 2,
		"the count has to move, or nothing is ever promoted")
}
