package memory

import (
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// The reason search exists at all: recall is ranked once, against the question
// the user opened with. An agent that works its way from that question to a
// different sub-problem is holding memories chosen for a query it has left
// behind, and nothing in the turn's budget can fix that.
func TestSearchFindsWhatTheOpeningQuestionDidNotMatch(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	for _, item := range []types.MemoryItem{
		{Kind: types.MemoryKindFact, Topic: "CSDL", Content: "CSDL chính thức đang dùng PostgreSQL"},
		{Kind: types.MemoryKindFact, Topic: "Triển khai", Content: "Triển khai theo hình thức cuốn chiếu từng đơn vị"},
	} {
		_, err := svc.Remember(ctx, item)
		require.NoError(t, err)
	}

	// The turn opened with a database question, so that is what recall ranked
	// against and the deployment memory is nowhere in the prompt.
	recall := svc.Recall(ctx, "giúp tôi xem kết nối CSDL")
	require.Contains(t, recall.Prompt, "PostgreSQL")
	require.NotContains(t, recall.Prompt, "cuốn chiếu")

	// Several iterations later the agent is looking at deployment instead.
	result := svc.SearchMemory(ctx, "cách triển khai", 10)
	require.True(t, result.Available)
	require.Len(t, result.Items, 1)
	require.Equal(t, "Triển khai theo hình thức cuốn chiếu từng đơn vị", result.Items[0].Content)
}

// The other half of the gap: recall admits five situational items no matter
// how many matched, because it is paid for on every turn. A search is paid for
// only when the model asked for it, so it can afford to answer properly.
func TestSearchReachesPastTheFiveItemTurnBudget(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	for i := 0; i < 8; i++ {
		_, err := svc.Remember(ctx, types.MemoryItem{
			Kind:    types.MemoryKindFact,
			Topic:   fmt.Sprintf("Mục cấu hình-%d", i),
			Content: fmt.Sprintf("Mục cấu hình cổng dịch vụ số %d đã chỉnh", i),
		})
		require.NoError(t, err)
	}

	recall := svc.Recall(ctx, "cấu hình cổng dịch vụ")
	require.Len(t, recall.Items, types.MemoryRecallMaxItems)

	result := svc.SearchMemory(ctx, "cấu hình cổng dịch vụ", 8)
	require.True(t, result.Available)
	require.Len(t, result.Items, 8)
}

// Resident kinds are in the block rather than in situational recall, and the
// block has its own rune budget. Search covers them too, or the tool would be
// unable to answer "what do you know about my preferences" for anyone whose
// block is full.
func TestSearchCoversTheResidentKindsToo(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindPreference, Topic: "Phong cách soạn thảo", Content: "Chú thích trong văn bản thống nhất dùng tiếng Việt",
	})
	require.NoError(t, err)

	result := svc.SearchMemory(ctx, "chú thích văn bản", 10)
	require.True(t, result.Available)
	require.Len(t, result.Items, 1)
	require.Equal(t, types.MemoryKindPreference, result.Items[0].Kind)
}

// A statement a later one replaced is exactly what the supersede machinery
// exists to keep out of an answer. Reaching it through search would undo that
// and hand the model a fact the user has already corrected.
func TestSearchDoesNotResurrectReplacedMemories(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL chính thức", Content: "CSDL chính thức đang dùng MySQL",
	})
	require.NoError(t, err)
	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL chính thức", Content: "CSDL chính thức đã chuyển sang PostgreSQL",
	})
	require.NoError(t, err)

	result := svc.SearchMemory(ctx, "CSDL chính thức", 10)
	require.True(t, result.Available)
	require.Len(t, result.Items, 1)
	require.Contains(t, result.Items[0].Content, "PostgreSQL")
}

// "Switched off" and "nothing stored" have to stay distinguishable all the way
// out to the caller. Collapsing them would have the agent tell someone who
// disabled memory that it remembers nothing about them.
func TestSearchTellsDisabledApartFromEmpty(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL", Content: "CSDL chính thức đang dùng PostgreSQL",
	})
	require.NoError(t, err)

	empty := svc.SearchMemory(ctx, "chủ đề hoàn toàn không liên quan", 10)
	require.True(t, empty.Available, "memory is on, this user simply has no match")
	require.Empty(t, empty.Items)

	disabled := false
	off := svc.SearchMemory(types.ApplyAgentMemoryPreference(ctx, &disabled), "CSDL", 10)
	require.False(t, off.Available, "an agent opting out must not be able to search either")
	require.Empty(t, off.Items)
}

// MemoryAvailable is what lets a caller decide not to offer a memory feature
// at all. It has to track every switch the read path honours, or the agent
// would keep being handed a tool that can only report that memory is off.
func TestMemoryAvailableTracksEverySwitch(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	require.True(t, svc.MemoryAvailable(ctx))

	// The agent handling this request.
	disabled := false
	require.False(t, svc.MemoryAvailable(types.ApplyAgentMemoryPreference(ctx, &disabled)))

	// The workspace setting.
	tenantRepo.set(1, &types.MemoryConfig{Enabled: false})
	require.False(t, svc.MemoryAvailable(ctx), "the workspace switched memory off")
}

// The predicate and the search must never disagree: anything that reports
// available has to be searchable, and anything unavailable has to say so
// rather than come back looking like an empty store.
func TestMemoryAvailableAgreesWithSearch(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL", Content: "CSDL chính thức đang dùng PostgreSQL",
	})
	require.NoError(t, err)

	require.Equal(t, svc.MemoryAvailable(ctx), svc.SearchMemory(ctx, "CSDL", 10).Available)

	tenantRepo.set(1, &types.MemoryConfig{Enabled: false})
	require.Equal(t, svc.MemoryAvailable(ctx), svc.SearchMemory(ctx, "CSDL", 10).Available)
	require.False(t, svc.SearchMemory(ctx, "CSDL", 10).Available)
}

func TestSearchWithoutPrincipalIsUnavailable(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	tenantRepo.set(1, &types.MemoryConfig{Enabled: true})
	ctx := t.Context()

	result := svc.SearchMemory(ctx, "CSDL", 10)
	require.False(t, result.Available,
		"a request with no principal has no memory space to search")
}

func TestSearchClampsAnAbsurdLimit(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	for i := 0; i < types.MemorySearchMaxItems+10; i++ {
		_, err := svc.Remember(ctx, types.MemoryItem{
			Kind:    types.MemoryKindFact,
			Topic:   fmt.Sprintf("Cổng-%d", i),
			Content: fmt.Sprintf("Mục cấu hình cổng dịch vụ số %d đã chỉnh", i),
		})
		require.NoError(t, err)
	}

	result := svc.SearchMemory(ctx, "cấu hình cổng dịch vụ", 10_000)
	require.True(t, result.Available)
	require.LessOrEqual(t, len(result.Items), types.MemorySearchMaxItems)
}
