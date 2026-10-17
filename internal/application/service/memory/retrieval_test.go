package memory

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// The point of phase two is that memory changes what gets retrieved, not only
// what the answer prompt says. These tests pin the behaviours that make that
// true, and the ones that keep it from becoming a way to assert wrong things
// about a person or to read somebody else's data.

func TestRetrievalContextCarriesWhoIsAsking(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindProfile, Content: "đang làm số hóa hồ sơ lưu trữ", Importance: 4,
	})
	require.NoError(t, err)

	memCtx := svc.RetrievalContextFor(ctx)
	require.False(t, memCtx.Empty())
	require.Contains(t, memCtx.Background, "số hóa hồ sơ")
	require.NotEmpty(t, memCtx.Items, "the UI has to be able to show what shaped the search")
}

func TestRetrievalContextIsEmptyWhenConditioningIsOff(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindProfile, Content: "đang làm số hóa hồ sơ lưu trữ", Importance: 4,
	})
	require.NoError(t, err)

	off := false
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled:               true,
		WriteMode:             types.MemoryWriteAuto,
		RetrievalConditioning: &off,
	})

	require.True(t, svc.RetrievalContextFor(ctx).Empty())
	// The answer prompt is a separate switch, and turning off conditioning
	// must not quietly turn off memory itself.
	require.NotEmpty(t, svc.Recall(ctx, "số hóa hồ sơ").Items)
}

// Users no longer review memory, so a guess that stands on its own is used
// at once (and announced on the next turn, see announce_test.go). A guess that
// would replace what the user said stays a pending proposal and never reaches
// a prompt.
func TestPendingMemoriesNeverReachAPrompt(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind:    types.MemoryKindProfile,
		Topic:   "Nghề nghiệp",
		Content: "Tôi là chuyên viên văn thư",
		Origin:  types.MemoryOriginManual,
	})
	require.NoError(t, err)
	proposal, err := svc.Remember(ctx, types.MemoryItem{
		Kind:       types.MemoryKindProfile,
		Topic:      "Nghề nghiệp",
		Content:    "có thể đang phụ trách văn bản đi đến của xã",
		Importance: 3,
		Origin:     types.MemoryOriginExtracted,
		Inferred:   true,
	})
	require.NoError(t, err)
	require.Equal(t, types.MemoryStatusPending, proposal.Status)

	for _, item := range svc.Recall(ctx, "văn bản đi đến").Items {
		require.NotEqual(t, proposal.ID, item.ID,
			"a guess contradicting the user must not be asserted")
	}
}

func TestRejectingAGuessStopsItComingBack(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	stored, err := svc.Remember(ctx, types.MemoryItem{
		Kind:       types.MemoryKindProfile,
		Content:    "có thể đang phụ trách văn bản đi đến của xã",
		Importance: 3,
		Origin:     types.MemoryOriginExtracted,
		Inferred:   true,
	})
	require.NoError(t, err)
	require.NoError(t, svc.RejectItem(ctx, stored.ID))

	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind:       types.MemoryKindProfile,
		Content:    "có thể đang phụ trách văn bản đi đến của xã",
		Importance: 3,
		Origin:     types.MemoryOriginExtracted,
		Inferred:   true,
	})
	require.ErrorIs(t, err, ErrPreviouslyForgotten,
		"declining a guess has to be remembered, or the same guess returns next week")
}

func TestATopicBecomesAnInterestOnlyWhenItRecurs(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, InterestThreshold: 3,
	})

	require.Empty(t, sighting(svc, ctx, 1, []string{"quản lý văn bản đi đến"}),
		"one question is a passing curiosity, not a fact about the person")
	require.Empty(t, sighting(svc, ctx, 2, []string{"quản lý văn bản đi đến"}))

	promoted := sighting(svc, ctx, 3, []string{"quản lý văn bản đi đến"})
	require.Equal(t, []string{"quản lý văn bản đi đến"}, promoted,
		"the same subject across conversations is a signal worth keeping")

	memCtx := svc.RetrievalContextFor(ctx)
	require.Contains(t, memCtx.Interests, "quản lý văn bản đi đến")

	require.Empty(t, sighting(svc, ctx, 4, []string{"quản lý văn bản đi đến"}),
		"and it is promoted once, not on every question thereafter")
}

func TestInterestsDoNotCrossBetweenPeople(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	alice := enabledCtx(t, tenantRepo, 1, "alice")
	bob := enabledCtx(t, tenantRepo, 1, "bob")
	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, InterestThreshold: 2,
	})

	svc.ObserveQuestionTopics(alice, []string{"số hóa hồ sơ lưu trữ"})
	require.Empty(t, svc.ObserveQuestionTopics(bob, []string{"số hóa hồ sơ lưu trữ"}),
		"bob asking once must not inherit alice's count")
	require.Empty(t, svc.RetrievalContextFor(bob).Interests)
}

func TestDocumentAffinityGrowsWithUseAndStaysPerPerson(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	alice := enabledCtx(t, tenantRepo, 1, "alice")
	bob := enabledCtx(t, tenantRepo, 1, "bob")

	refs := []types.MemoryDocAffinity{{KnowledgeID: "doc-1", Title: "Sổ tay số hóa hồ sơ lưu trữ"}}
	svc.RecordAnswerSources(alice, refs)
	svc.RecordAnswerSources(alice, refs)

	require.Equal(t, map[string]int{"doc-1": 2}, svc.DocumentAffinity(alice, []string{"doc-1"}))
	require.Empty(t, svc.DocumentAffinity(bob, []string{"doc-1"}),
		"what alice reads must not reorder bob's results")

	// Two sightings is a habit worth telling the rewriter about; one is not.
	require.Contains(t, svc.RetrievalContextFor(alice).Documents, "Sổ tay số hóa hồ sơ lưu trữ")
}

func TestMemoryIsNotSharedAcrossWorkspaces(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	first := enabledCtx(t, tenantRepo, 1, "alice")
	second := enabledCtx(t, tenantRepo, 2, "alice")

	svc.RecordAnswerSources(first, []types.MemoryDocAffinity{
		{KnowledgeID: "doc-1", Title: "Hướng dẫn định mức nội bộ"},
	})
	require.Empty(t, svc.DocumentAffinity(second, []string{"doc-1"}))
	require.Empty(t, svc.RetrievalContextFor(second).Documents)
}

func TestStaleTasksAreDemotedRatherThanDeleted(t *testing.T) {
	svc, db, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	stored, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindTask, Content: "Rà soát quy trình một cửa, dự kiến xong trong tuần", Importance: 4,
	})
	require.NoError(t, err)

	old := stored.ValidFrom.Add(-staleTaskAge - staleTaskAge)
	require.NoError(t, db.Model(&types.MemoryItem{}).
		Where("id = ?", stored.ID).
		Updates(map[string]interface{}{"valid_from": old, "last_used_at": nil}).Error)

	scope := scopeFor(t, ctx)
	items, _, err := svc.repo.ListItems(ctx, scope, types.MemoryStatusActive, 50, 0)
	require.NoError(t, err)
	require.Equal(t, 1, svc.demoteStaleTasks(ctx, scope, items))

	after, err := svc.repo.GetItem(ctx, scope, stored.ID)
	require.NoError(t, err)
	require.Equal(t, 1, after.Importance)
	require.Equal(t, types.MemoryStatusActive, after.Status,
		"the user never said they finished it, so it is demoted rather than deleted")
}

func TestOnlySimilarMemoriesAreEverMerged(t *testing.T) {
	// Merging two memories that merely looked alike destroys something the
	// user actually told us. A missed merge only leaves the store redundant,
	// so the threshold is deliberately lopsided.
	items := []*types.MemoryItem{
		{ID: "a", Kind: types.MemoryKindPreference, Topic: "Phong cách trả lời", Content: "Trả lời đi thẳng vào kết luận không dẫn dắt"},
		{ID: "b", Kind: types.MemoryKindPreference, Topic: "Phong cách trả lời", Content: "Trả lời đi thẳng vào kết luận khỏi dẫn dắt"},
		{ID: "c", Kind: types.MemoryKindPreference, Topic: "Ngôn ngữ trả lời", Content: "Luôn trả lời tôi bằng tiếng Việt"},
	}
	clusters := clusterSimilar(items)
	require.Len(t, clusters, 1)
	require.Len(t, clusters[0], 2)
	require.ElementsMatch(t, []string{"a", "b"},
		[]string{clusters[0][0].ID, clusters[0][1].ID})
}

func TestMemoriesOfDifferentKindsAreNeverMerged(t *testing.T) {
	items := []*types.MemoryItem{
		{ID: "a", Kind: types.MemoryKindTask, Topic: "Rà soát một cửa", Content: "Tuần này phải rà soát quy trình một cửa"},
		{ID: "b", Kind: types.MemoryKindFact, Topic: "Rà soát một cửa", Content: "Tuần này phải rà soát quy trình một cửa"},
	}
	require.Empty(t, clusterSimilar(items),
		"what someone is doing and what is true of their system are different claims")
}

func scopeFor(t *testing.T, ctx context.Context) interfaces.MemoryScope {
	t.Helper()
	scope, err := ResolveScope(ctx)
	require.NoError(t, err)
	return scope
}

// The same guess arriving twice must not stack up two copies in the review
// list. Deduplication originally looked only at active memories, so every
// re-derivation of an inference added another row the user had to decline
// separately.
func TestARepeatedGuessDoesNotStackUpInTheInbox(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	guess := types.MemoryItem{
		Kind:       types.MemoryKindProfile,
		Topic:      "Vai trò có thể",
		Content:    "có thể đang phụ trách văn bản đi đến của xã",
		Importance: 2,
		Origin:     types.MemoryOriginExtracted,
		Inferred:   true,
	}
	first, err := svc.Remember(ctx, guess)
	require.NoError(t, err)
	second, err := svc.Remember(ctx, guess)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	_, total, err := svc.ListItems(ctx, types.MemoryStatusPending, 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total, "a guess waits for confirmation, once")
}
