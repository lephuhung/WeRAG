package memory

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Users have no review queue, so a memory the system inferred is used at once
// and announced on the next turn instead of waiting as pending.
func TestInferredMemoryIsActiveAndAnnouncedOnce(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	inferred, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindProfile, Content: "Làm ở phòng Kế hoạch",
		Origin: types.MemoryOriginExtracted, Inferred: true,
	})
	require.NoError(t, err)
	require.Equal(t, types.MemoryStatusActive, inferred.Status)

	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Content: "Báo cáo tuần nộp thứ Sáu", Origin: types.MemoryOriginExplicit,
	})
	require.NoError(t, err)

	saved := svc.TakeNewlySaved(ctx)
	require.Len(t, saved, 1, "only what the system distilled on its own is announced")
	require.Equal(t, inferred.ID, saved[0].ID)
	require.Empty(t, svc.TakeNewlySaved(ctx), "an announcement goes out once")
}

// "Forget ..." deletes the memory it names, and leaves alone a memory that
// only shares a word or two with the request.
func TestForgetMatchingDeletesOnlyACloseMatch(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	office, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Content: "Tôi làm ở phòng Kế hoạch", Origin: types.MemoryOriginExplicit,
	})
	require.NoError(t, err)
	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindPreference, Content: "Thích câu trả lời ngắn", Origin: types.MemoryOriginExplicit,
	})
	require.NoError(t, err)

	removed, err := svc.ForgetMatching(ctx, "phòng ban của tôi ở đâu")
	require.NoError(t, err)
	require.Nil(t, removed, "a loose match must not delete anything")

	removed, err = svc.ForgetMatching(ctx, "tôi làm ở phòng Kế hoạch")
	require.NoError(t, err)
	require.NotNil(t, removed)
	require.Equal(t, office.ID, removed.ID)
	require.NotContains(t, svc.Recall(ctx, "phòng Kế hoạch").Prompt, "phòng Kế hoạch")
}
