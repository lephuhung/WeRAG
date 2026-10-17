package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// This file is the behavioural regression set. Each case is a short sequence
// of turns across separate sessions, ending in an assertion about what the
// next turn's prompt contains. They are written against the service the chat
// path actually calls, so a change that keeps the unit tests green but breaks
// the user-visible behaviour still fails here.
//
// LoCoMo and LongMemEval are deliberately not used: their published scores are
// vendor-run and disagree by tens of points, and neither has a Chinese split
// that matches how this feature is used.

type memoryScenario struct {
	name string
	// userTurns are what the user says, in order, as if across sessions.
	userTurns []string
	// extracted is the distillation the model returns for those turns.
	extracted []map[string]any
	// laterQuery is what the user asks in a new session afterwards.
	laterQuery string
	// wantInPrompt must appear in the memory injected into that later turn.
	wantInPrompt []string
	// wantAbsent must not appear.
	wantAbsent []string
}

func TestCrossSessionMemoryScenarios(t *testing.T) {
	scenarios := []memoryScenario{
		{
			name:      "ghi nhớ hồ sơ cá nhân và mang sang cuộc trò chuyện mới",
			userTurns: []string{"Tôi là chuyên viên văn thư lưu trữ, chủ yếu soạn thảo công văn"},
			extracted: []map[string]any{
				{
					"action": "add", "kind": "profile", "topic": "Nghề nghiệp",
					"content": "chuyên viên văn thư lưu trữ, chủ yếu soạn thảo công văn",
				},
			},
			laterQuery:   "Giúp tôi soạn một tờ trình",
			wantInPrompt: []string{"văn thư lưu trữ", "chuyên viên văn thư"},
		},
		{
			name:      "sở thích luôn thường trực, không liên quan câu hỏi vẫn mang theo",
			userTurns: []string{"Từ nay trả lời đi thẳng vào kết luận, đừng dẫn dắt dài dòng"},
			extracted: []map[string]any{
				{"action": "add", "kind": "preference", "topic": "Phong cách trả lời", "content": "Trả lời đi thẳng vào kết luận, không dẫn dắt"},
			},
			laterQuery:   "Hôm nay thời tiết có hợp để chạy bộ không",
			wantInPrompt: []string{"đi thẳng vào kết luận"},
		},
		{
			name: "sự việc được gợi lại theo độ liên quan, không liên quan thì không vào ngữ cảnh",
			userTurns: []string{
				"CSDL chính thức của đơn vị tôi là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh",
				"Giao diện của đơn vị tôi dùng Vue 3 và Vite",
			},
			extracted: []map[string]any{
				{
					"action": "add", "kind": "fact", "topic": "CSDL chính thức",
					"content": "CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh",
				},
				{"action": "add", "kind": "fact", "topic": "Công nghệ giao diện", "content": "Giao diện của đơn vị tôi dùng Vue 3 và Vite"},
			},
			laterQuery:   "kết nối CSDL chính thức nên cấu hình bao nhiêu",
			wantInPrompt: []string{"PostgreSQL 17"},
			wantAbsent:   []string{"Vue 3"},
		},
		{
			name: "sau khi đính chính mâu thuẫn chỉ giữ thông tin mới nhất",
			userTurns: []string{
				"Đơn vị tôi dùng MySQL",
				"Đính chính: đơn vị tôi đã chuyển sang PostgreSQL từ tháng trước",
			},
			extracted: []map[string]any{
				{"action": "add", "kind": "fact", "topic": "CSDL đang dùng", "content": "đang dùng MySQL"},
				{"action": "update", "kind": "fact", "topic": "CSDL đang dùng", "content": "đã chuyển sang PostgreSQL"},
			},
			laterQuery:   "Viết mã mẫu kết nối CSDL đang dùng",
			wantInPrompt: []string{"PostgreSQL"},
			wantAbsent:   []string{"MySQL"},
		},
		{
			name:      "việc đang làm tiếp nối được qua các cuộc trò chuyện",
			userTurns: []string{"Tuần này tôi đang rà soát quy trình tiếp nhận hồ sơ một cửa, chưa xong"},
			extracted: []map[string]any{
				{
					"action": "add", "kind": "task", "topic": "Việc rà soát đang làm",
					"content": "đang rà soát quy trình tiếp nhận hồ sơ một cửa, chưa hoàn thành",
				},
			},
			laterQuery:   "việc rà soát quy trình một cửa tiếp theo làm thế nào",
			wantInPrompt: []string{"quy trình tiếp nhận hồ sơ"},
		},
		{
			name: "việc đã xong không được gợi lại nữa",
			userTurns: []string{
				"đang rà soát quy trình tiếp nhận hồ sơ một cửa",
				"Việc rà soát quy trình một cửa đã hoàn thành rồi",
			},
			extracted: []map[string]any{
				{"action": "add", "kind": "task", "topic": "Việc rà soát đang làm", "content": "đang rà soát quy trình tiếp nhận hồ sơ một cửa"},
				{"action": "delete", "kind": "task", "topic": "Việc rà soát đang làm", "content": "rà soát quy trình một cửa đã hoàn thành"},
			},
			laterQuery: "bộ phận một cửa hiện còn việc gì đang làm",
			wantAbsent: []string{"đang rà soát quy trình tiếp nhận hồ sơ một cửa"},
		},
		{
			name:      "câu hỏi một lần không được ghi thành sự việc lâu dài",
			userTurns: []string{"Map trong Go có an toàn khi dùng đồng thời không"},
			// A well-behaved extraction returns nothing here, which is the
			// normal outcome; the assertion is that we store nothing either.
			extracted:  nil,
			laterQuery: "Slice trong Go mở rộng dung lượng thế nào",
			wantAbsent: []string{"map", "đồng thời"},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			svc, tenantRepo, messages, models, _ := newExtractionHarness(t)
			tenantRepo.set(1, &types.MemoryConfig{Enabled: true, WriteMode: types.MemoryWriteAuto})

			// Replay the turns one distillation at a time, so an update or a
			// delete sees the state its predecessor left behind.
			for i, turn := range scenario.userTurns {
				messages.messages = []*types.Message{{Role: "user", Content: turn}}
				var decisions []map[string]any
				if i < len(scenario.extracted) {
					decisions = scenario.extracted[i : i+1]
				}
				body, err := json.Marshal(map[string]any{"memories": decisions})
				require.NoError(t, err)
				models.response = string(body)

				require.NoError(t, svc.Handle(context.Background(), extractTask(t, types.MemoryExtractPayload{
					TenantID:    1,
					SubjectID:   "web_user:alice",
					SessionID:   "session-" + string(rune('a'+i)),
					MessageID:   "message-" + string(rune('a'+i)),
					ChatModelID: "conversation-model",
				})))
			}

			// A brand new session: nothing but long-term memory carries over.
			laterCtx := enabledCtx(t, tenantRepo, 1, "alice")
			prompt := svc.Recall(laterCtx, scenario.laterQuery).Prompt

			for _, want := range scenario.wantInPrompt {
				require.Contains(t, prompt, want,
					"expected the later turn to carry %q\nprompt was:\n%s", want, prompt)
			}
			for _, absent := range scenario.wantAbsent {
				require.NotContains(t, prompt, absent,
					"did not expect the later turn to carry %q\nprompt was:\n%s", absent, prompt)
			}
		})
	}
}

// TestReadPathMakesNoModelCall pins the cost promise: recall must not add a
// model call to a turn, no matter how many memories the user has.
func TestReadPathMakesNoModelCall(t *testing.T) {
	svc, tenantRepo, _, models, _ := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	for i := 0; i < 30; i++ {
		_, err := svc.Remember(ctx, types.MemoryItem{
			Kind:    types.MemoryKindFact,
			Topic:   "sự việc " + string(rune('a'+i)),
			Content: "sự việc liên quan CSDL " + string(rune('a'+i)),
		})
		require.NoError(t, err)
	}

	for i := 0; i < 5; i++ {
		require.NotEmpty(t, svc.Recall(ctx, "tinh chỉnh CSDL thế nào").Prompt)
	}
	require.Zero(t, models.calls, "the read path must not call a model")
}

// TestInjectedMemoryStaysInsideItsBudget keeps a user with a large memory
// space from quietly eating the context window.
func TestInjectedMemoryStaysInsideItsBudget(t *testing.T) {
	svc, tenantRepo, _, _, _ := newExtractionHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	for i := 0; i < 40; i++ {
		_, err := svc.Remember(ctx, types.MemoryItem{
			Kind:    types.MemoryKindPreference,
			Topic:   "sở thích " + string(rune('a'+i)),
			Content: strings.Repeat("mô tả sở thích dài ", 8) + string(rune('a'+i)),
		})
		require.NoError(t, err)
		_, err = svc.Remember(ctx, types.MemoryItem{
			Kind:    types.MemoryKindFact,
			Topic:   "sự việc " + string(rune('a'+i)),
			Content: "CSDL " + strings.Repeat("mô tả sự việc dài ", 8) + string(rune('a'+i)),
		})
		require.NoError(t, err)
	}

	prompt := svc.Recall(ctx, "tinh chỉnh CSDL thế nào").Prompt
	require.NotEmpty(t, prompt)
	// Envelope wording aside, the memory content itself must fit in the two
	// declared budgets.
	require.LessOrEqual(t, len([]rune(prompt)),
		types.MemoryBlockRuneBudget+types.MemoryRecallRuneBudget+600,
		"injected memory must stay within its budget")
}
