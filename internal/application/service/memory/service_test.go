package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newMemoryHarness builds a service over a real SQLite database. The write
// path is mostly about what ends up in the database after a conflict, so a
// mocked repository would assert the wrong thing.
func newMemoryHarness(t *testing.T) (*Service, *gorm.DB, *stubTenantRepo) {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{Logger: logger.Discard},
	)
	require.NoError(t, err)
	// Recall records usage on a background goroutine. A shared-cache SQLite
	// file rejects a concurrent writer, so pin the pool to one connection and
	// let the driver serialize instead of failing the next read.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&types.MemorySubject{}, &types.MemoryItem{}, &types.MemoryTombstone{},
		&types.MemoryTopicStat{}, &types.MemoryDocAffinity{},
		&types.MemoryItemEmbedding{}, &types.MemoryExtractionSession{}))

	tenantRepo := &stubTenantRepo{
		configs: map[uint64]*types.MemoryConfig{},
	}
	svc := &Service{
		repo:       repository.NewMemoryRepository(db),
		tenantRepo: tenantRepo,
	}
	return svc, db, tenantRepo
}

// enabledCtx returns a request context for one principal in one workspace,
// with memory switched on for that workspace.
func enabledCtx(t *testing.T, tenantRepo *stubTenantRepo, tenantID uint64, userID string) context.Context {
	t.Helper()
	tenantRepo.set(tenantID, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, EmbeddingModelID: "embed-1",
	})
	ctx := context.WithValue(t.Context(), types.TenantIDContextKey, tenantID)
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: userID})
}

func TestRememberStoresAndRecalls(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindPreference, Content: "Trả lời xin đi thẳng vào kết luận, không dẫn dắt dài dòng", Importance: 4,
	})
	require.NoError(t, err)

	recall := svc.Recall(ctx, "giúp tôi xem lỗi này")
	require.Contains(t, recall.Prompt, "Trả lời xin đi thẳng vào kết luận")
	require.Len(t, recall.Items, 1)
}

// TestExplicitMemoryIsAlwaysAvailable pins the rule that a user who said
// "remember this" gets it back regardless of how they phrase the next
// question. Leaving it to lexical matching means the one memory the user
// deliberately asked for is the one most likely to go missing.
func TestExplicitMemoryIsAlwaysAvailable(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind:    types.MemoryKindFact,
		Content: "my editor is Neovim and my terminal is WezTerm",
		Origin:  types.MemoryOriginExplicit,
	})
	require.NoError(t, err)

	// Not one word in common with the memory.
	recall := svc.Recall(ctx, "what tools do I use daily")
	require.Contains(t, recall.Prompt, "Neovim")
	require.Len(t, recall.Items, 1)
}

// TestExtractedFactStillNeedsAQueryMatch is the counterpart: memory the user
// never asked for must stay out of context unless it is relevant, or the block
// grows without bound.
func TestExtractedFactStillNeedsAQueryMatch(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind:    types.MemoryKindFact,
		Topic:   "Trình soạn thảo",
		Content: "trình soạn thảo đang dùng là Neovim",
		Origin:  types.MemoryOriginExtracted,
	})
	require.NoError(t, err)

	require.Empty(t, svc.Recall(ctx, "giúp tôi tính sổ sách tháng này").Prompt)
	require.Contains(t, svc.Recall(ctx, "cấu hình trình soạn thảo thế nào").Prompt, "Neovim")
}

// TestRecalledItemIsNotListedTwice guards the seam between the resident block
// and query matching: an explicit fact is in both candidate sets.
func TestRecalledItemIsNotListedTwice(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind:    types.MemoryKindFact,
		Content: "CSDL chính thức là PostgreSQL 17",
		Origin:  types.MemoryOriginExplicit,
	})
	require.NoError(t, err)

	recall := svc.Recall(ctx, "kết nối CSDL chính thức cấu hình bao nhiêu")
	require.Len(t, recall.Items, 1, "the same memory must not be reported twice")
	require.Equal(t, 1, strings.Count(recall.Prompt, "CSDL chính thức là PostgreSQL 17"))
}

func TestRecallMatchesSituationalItemsByQuery(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Content: "CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh",
	})
	require.NoError(t, err)
	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Content: "Giao diện dùng Vue 3 và Vite",
	})
	require.NoError(t, err)

	recall := svc.Recall(ctx, "kết nối CSDL chính thức bị quá hạn, rà soát thế nào")
	require.Contains(t, recall.Prompt, "PostgreSQL 17")
	// An unrelated fact must stay out: injecting it would spend context and
	// invite the model to use it.
	require.NotContains(t, recall.Prompt, "Vue 3")
}

func TestContradictionSupersedesRatherThanDeletes(t *testing.T) {
	svc, db, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	first, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL đang dùng", Content: "Đơn vị tôi dùng MySQL",
	})
	require.NoError(t, err)
	second, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL đang dùng", Content: "Đơn vị tôi đã chuyển sang PostgreSQL",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)

	var old types.MemoryItem
	require.NoError(t, db.First(&old, "id = ?", first.ID).Error)
	require.Equal(t, types.MemoryStatusSuperseded, old.Status,
		"the outdated statement must be superseded, not left active")
	require.Equal(t, second.ID, old.SupersededBy)
	require.NotNil(t, old.InvalidAt, "a superseded item must record when it stopped being true")
	require.Equal(t, "Đơn vị tôi dùng MySQL", old.Content,
		"history must stay readable, so the old content is preserved")

	recall := svc.Recall(ctx, "CSDL của đơn vị tôi là gì")
	require.Contains(t, recall.Prompt, "PostgreSQL")
	require.NotContains(t, recall.Prompt, "MySQL")
}

func TestRepeatedIdenticalStatementDoesNotChurn(t *testing.T) {
	svc, db, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	first, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindProfile, Topic: "Chức vụ", Content: "chuyên viên văn thư",
	})
	require.NoError(t, err)
	second, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindProfile, Topic: "Chức vụ", Content: "chuyên viên văn thư",
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "an unchanged statement must not create a new row")

	var count int64
	require.NoError(t, db.Model(&types.MemoryItem{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

// TestRestatedFactDoesNotDuplicate covers the shape seen in real use: the user
// says "remember X" and the background distillation later produces the same
// fact with slightly different wording. They carry different topic keys, so
// without a containment check the user's memory list shows the same thing
// twice.
func TestRestatedFactDoesNotDuplicate(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	explicit, err := svc.Remember(ctx, types.MemoryItem{
		Kind:    types.MemoryKindFact,
		Content: "Hiện nay CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh",
		Origin:  types.MemoryOriginExplicit,
	})
	require.NoError(t, err)

	// The distillation restates it more tersely and under its own topic.
	extracted, err := svc.Remember(ctx, types.MemoryItem{
		Kind:    types.MemoryKindFact,
		Topic:   "CSDL chính thức",
		Content: "CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh",
		Origin:  types.MemoryOriginExtracted,
	})
	require.NoError(t, err)
	require.Equal(t, explicit.ID, extracted.ID,
		"a restatement contained in an existing memory must not create a second row")

	_, total, err := svc.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
}

// TestMoreSpecificRestatementSupersedes is the other direction: when the new
// statement contains the old one it carries strictly more information, so it
// replaces it instead of sitting beside it.
func TestMoreSpecificRestatementSupersedes(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	short, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL chính thức", Content: "CSDL chính thức là PostgreSQL 17",
	})
	require.NoError(t, err)
	long, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Content: "CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh",
	})
	require.NoError(t, err)
	require.NotEqual(t, short.ID, long.ID)

	items, total, err := svc.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "CSDL chính thức là PostgreSQL 17, đặt tại Trung tâm dữ liệu tỉnh", items[0].Content)
}

// TestDifferentFactsSharingWordsAreKept guards the containment rule from being
// too eager: two genuinely different statements must both survive.
func TestDifferentFactsSharingWordsAreKept(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL chính thức", Content: "CSDL chính thức là PostgreSQL 17",
	})
	require.NoError(t, err)
	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL kiểm thử", Content: "CSDL kiểm thử là PostgreSQL 15",
	})
	require.NoError(t, err)

	_, total, err := svc.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
}

func TestMemoriesAreIsolatedAcrossSubjectsAndWorkspaces(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)

	aliceInOne := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(aliceInOne, types.MemoryItem{
		Kind: types.MemoryKindProfile, Content: "Lan đang làm dự án số hóa hồ sơ lưu trữ",
	})
	require.NoError(t, err)

	// Same workspace, different person.
	bobInOne := enabledCtx(t, tenantRepo, 1, "bob")
	require.Empty(t, svc.Recall(bobInOne, "tôi đang làm dự án gì").Prompt,
		"another user in the same workspace must not see the memory")

	// Same person, different workspace: the agreed scope is (workspace,
	// principal), so work memories do not follow someone across workspaces.
	aliceInTwo := enabledCtx(t, tenantRepo, 2, "alice")
	require.Empty(t, svc.Recall(aliceInTwo, "tôi đang làm dự án gì").Prompt,
		"the same user in another workspace must not see the memory")

	require.Contains(t, svc.Recall(aliceInOne, "tôi đang làm dự án gì").Prompt, "số hóa hồ sơ")
}

func TestListItemsIsScopedToTheCaller(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	aliceCtx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(aliceCtx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "bí mật của Lan"})
	require.NoError(t, err)

	bobCtx := enabledCtx(t, tenantRepo, 1, "bob")
	items, total, err := svc.ListItems(bobCtx, "", 50, 0)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, items)
}

func TestDeleteAnotherUsersMemoryIsNotFound(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	aliceCtx := enabledCtx(t, tenantRepo, 1, "alice")
	item, err := svc.Remember(aliceCtx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "bí mật của Lan"})
	require.NoError(t, err)

	bobCtx := enabledCtx(t, tenantRepo, 1, "bob")
	require.ErrorIs(t, svc.DeleteItem(bobCtx, item.ID), ErrItemNotFound)

	// And the item survives the attempt.
	require.Contains(t, svc.Recall(aliceCtx, "bí mật của Lan").Prompt, "bí mật của Lan")
}

func TestWorkspaceSwitchOffDisablesReadAndWrite(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "điều đã ghi nhớ"})
	require.NoError(t, err)

	tenantRepo.set(1, &types.MemoryConfig{Enabled: false})
	require.Empty(t, svc.Recall(ctx, "điều đã ghi nhớ").Prompt)
	_, err = svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "điều mới"})
	require.ErrorIs(t, err, ErrMemoryDisabled)
}

// A Guest in the public portal neither saves nor recalls memory: the memory
// routes that would let them see or delete it are Member-only.
func TestPublicPortalHasNoMemory(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "điều đã ghi nhớ"})
	require.NoError(t, err)

	portalCtx := context.WithValue(ctx, types.TenantInfoContextKey,
		&types.Tenant{ID: 1, Kind: types.TenantKindPublicPortal})
	require.Empty(t, svc.Recall(portalCtx, "điều đã ghi nhớ").Prompt)
	_, err = svc.Remember(portalCtx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "điều mới"})
	require.ErrorIs(t, err, ErrMemoryDisabled)
}

// Memory is a platform setting: a per-user opt-out stored before that change
// must not keep switching memory off for the user, who has no way to undo it.
func TestStoredUserSwitchIsIgnored(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "điều đã ghi nhớ"})
	require.NoError(t, err)

	require.NoError(t, svc.SetEnabled(ctx, false))
	require.Contains(t, svc.Recall(ctx, "điều đã ghi nhớ").Prompt, "điều đã ghi nhớ")
	_, err = svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "điều mới"})
	require.NoError(t, err)
}

// stubPlatformSettings serves the platform memory settings; every other
// SystemSettingService method is unused by the memory service.
type stubPlatformSettings struct {
	interfaces.SystemSettingService
	memoryConfig []byte
}

func (s *stubPlatformSettings) GetBool(context.Context, string, string, bool) bool { return true }

func (s *stubPlatformSettings) GetJSON(_ context.Context, key string) (types.JSON, bool) {
	if key != types.MemoryConfigSettingKey || s.memoryConfig == nil {
		return nil, false
	}
	return types.JSON(s.memoryConfig), true
}

// The SuperAdmin's platform configuration wins over whatever a workspace
// stored before memory became a platform setting.
func TestPlatformConfigOverridesWorkspaceConfig(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	settings := &stubPlatformSettings{}
	svc.systemSettings = settings

	settings.memoryConfig = []byte(`{"enabled": false}`)
	_, err := svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "điều đã ghi nhớ"})
	require.ErrorIs(t, err, ErrMemoryDisabled, "the platform switched memory off")

	tenantRepo.set(1, &types.MemoryConfig{Enabled: false})
	settings.memoryConfig = []byte(`{"enabled": true}`)
	_, err = svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "điều đã ghi nhớ"})
	require.NoError(t, err, "the platform switched memory on for every workspace")
}

func TestAgentOptOutDisablesRecall(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindPreference, Content: "chỉ trả lời bằng tiếng Việt"})
	require.NoError(t, err)

	disabled := false
	agentCtx := types.ApplyAgentMemoryPreference(ctx, &disabled)
	require.Empty(t, svc.Recall(agentCtx, "giúp tôi viết một hàm").Prompt)
	require.Contains(t, svc.Recall(ctx, "giúp tôi viết một hàm").Prompt, "chỉ trả lời bằng tiếng Việt")
}

func TestRecallWithoutPrincipalIsEmpty(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	tenantRepo.set(1, &types.MemoryConfig{Enabled: true})
	ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(1))
	require.Empty(t, svc.Recall(ctx, "câu hỏi bất kỳ").Prompt,
		"a request with no principal has no memory space to read")
}

func TestCapacityCapArchivesLowestRanked(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	tenantRepo.set(1, &types.MemoryConfig{Enabled: true, WriteMode: types.MemoryWriteAuto, MaxItems: 3})

	// The important one is written first so recency alone would evict it.
	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "Sự việc then chốt", Content: "sự việc quan trọng nhất", Importance: 5,
	})
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		_, err := svc.Remember(ctx, types.MemoryItem{
			Kind:       types.MemoryKindFact,
			Topic:      fmt.Sprintf("Sự việc phụ-%d", i),
			Content:    fmt.Sprintf("sự việc phụ %d", i),
			Importance: 1,
		})
		require.NoError(t, err)
	}

	active, total, err := svc.ListItems(ctx, types.MemoryStatusActive, 50, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), total, "the cap must be enforced")

	var kept []string
	for _, item := range active {
		kept = append(kept, item.Content)
	}
	require.Contains(t, kept, "sự việc quan trọng nhất", "importance must outrank recency")

	// Overflow is archived, not deleted, so it stays visible in the manager.
	_, archivedTotal, err := svc.ListItems(ctx, types.MemoryStatusArchived, 50, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), archivedTotal)
}

func TestClearForgetsEverything(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	for i := 0; i < 3; i++ {
		_, err := svc.Remember(ctx, types.MemoryItem{
			Kind: types.MemoryKindFact, Topic: fmt.Sprintf("k%d", i), Content: fmt.Sprintf("sự việc %d", i),
		})
		require.NoError(t, err)
	}
	removed, err := svc.Clear(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), removed)

	require.Empty(t, svc.Recall(ctx, "sự việc").Prompt)
	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Zero(t, settings.ItemCount)

	tenantRepo.set(1, &types.MemoryConfig{
		Enabled: true, WriteMode: types.MemoryWriteAuto, InterestThreshold: 3,
	})
	svc.ObserveQuestionTopics(ctx, []string{"quản lý văn bản đi đến"})
	_, err = svc.Clear(ctx)
	require.NoError(t, err)
	topics, total, err := svc.ListTopics(ctx, 10, 0)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, topics)
}

// TestClearTombstonesLiveMemoriesFirst pins which memories get the bounded
// rejection budget. max_items caps active memories only, so superseded and
// archived rows accumulate without limit and a long-lived store easily holds
// more rows than MaxMemoryTombstones. Spending the budget on whatever happened
// to be newest left the memory the user was actually being served with no
// tombstone, free to be written straight back.
func TestClearTombstonesLiveMemoriesFirst(t *testing.T) {
	svc, db, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	// The one memory still in use, deliberately the oldest row in the store.
	require.NoError(t, db.Create(&types.MemoryItem{
		ID: "live-1", TenantID: 1, SubjectID: "web_user:alice",
		Kind: types.MemoryKindFact, Topic: "CSDL", Content: "CSDL chính thức là PostgreSQL",
		NormalizedKey: "csdl", Status: types.MemoryStatusActive,
		ValidFrom: time.Now().Add(-24 * time.Hour),
	}).Error)

	// Enough newer dead rows to exhaust the budget on their own.
	for i := 0; i < types.MaxMemoryTombstones+10; i++ {
		require.NoError(t, db.Create(&types.MemoryItem{
			ID: fmt.Sprintf("dead-%d", i), TenantID: 1, SubjectID: "web_user:alice",
			Kind: types.MemoryKindFact, Content: fmt.Sprintf("cách nói cũ %d", i),
			NormalizedKey: fmt.Sprintf("cũ|%d", i), Status: types.MemoryStatusSuperseded,
			ValidFrom: time.Now(),
		}).Error)
	}

	_, err := svc.Clear(ctx)
	require.NoError(t, err)

	_, err = svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindFact, Topic: "CSDL", Content: "CSDL chính thức là PostgreSQL",
	})
	require.ErrorIs(t, err, ErrPreviouslyForgotten,
		"the memory that was in use must keep its tombstone, not be crowded out by dead rows")
}

func TestGetSettingsReportsMergedState(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindFact, Content: "một ghi nhớ"})
	require.NoError(t, err)

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.WorkspaceEnabled)
	require.True(t, settings.UserEnabled)
	require.True(t, settings.Effective)
	require.Equal(t, 1, settings.ItemCount)

	require.NoError(t, svc.SetEnabled(ctx, false))
	settings, err = svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.WorkspaceEnabled)
	require.False(t, settings.UserEnabled)
	require.False(t, settings.Effective, "either switch being off must make the effective state off")
}

func TestUpdateItemMarksMemoryAsManual(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	item, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindPreference, Content: "thích giải thích thật dài",
	})
	require.NoError(t, err)

	updated, err := svc.UpdateItem(ctx, item.ID, "thích giải thích ngắn gọn", 5)
	require.NoError(t, err)
	require.Equal(t, "thích giải thích ngắn gọn", updated.Content)
	require.Equal(t, 5, updated.Importance)
	require.Equal(t, types.MemoryOriginManual, updated.Origin,
		"a corrected memory must be marked manual so extraction does not undo it")

	require.Contains(t, svc.Recall(ctx, "hỏi bâng quơ một câu").Prompt, "thích giải thích ngắn gọn")
}

func TestResidentBlockSurvivesCacheLoss(t *testing.T) {
	svc, db, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.Remember(ctx, types.MemoryItem{
		Kind: types.MemoryKindProfile, Content: "đang làm số hóa hồ sơ lưu trữ",
	})
	require.NoError(t, err)

	// Simulate a row written before a block-rebuild failure.
	require.NoError(t, db.Model(&types.MemorySubject{}).
		Where("1 = 1").Update("block_text", "").Error)

	require.Contains(t, svc.Recall(ctx, "hỏi bâng quơ").Prompt, "đang làm số hóa hồ sơ lưu trữ",
		"an empty block cache must not silently drop the user's memories")
}

// An interest is a standing property of the person, so it has to be present
// whatever they ask. The case that forced this: "what am I focused on" shares
// no words with "tích hợp dịch vụ công trực tuyến", so query matching can never reach it, and
// the assistant claimed to know nothing about a memory the user could see
// listed in the memory manager.
func TestInterestIsPresentRegardlessOfTheQuestion(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.CreateItem(ctx, types.MemoryKindInterest, "tích hợp dịch vụ công trực tuyến", 3)
	require.NoError(t, err)

	require.Contains(t, svc.Recall(ctx, "Tôi đang quan tâm những việc gì?").Prompt, "tích hợp dịch vụ công trực tuyến")
	require.Contains(t, svc.Recall(ctx, "Tôi đang quan tâm những việc gì?").Prompt, "Long-term focus")
	require.Contains(t, svc.Recall(ctx, "hôm nay thời tiết thế nào").Prompt, "tích hợp dịch vụ công trực tuyến")
}

// Being injected and being reported are different things. An interest that is
// present only because the cap left room is standing background; listing it as
// a memory this answer recalled would put something unrelated to the question
// on the chat timeline every single turn.
func TestUnrelatedInterestIsInjectedButNotReported(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	_, err := svc.CreateItem(ctx, types.MemoryKindInterest, "tích hợp dịch vụ công trực tuyến", 3)
	require.NoError(t, err)

	unrelated := svc.Recall(ctx, "hôm nay thời tiết thế nào")
	require.Contains(t, unrelated.Prompt, "tích hợp dịch vụ công trực tuyến")
	require.Empty(t, unrelated.Items)

	related := svc.Recall(ctx, "tích hợp dịch vụ công trực tuyến thế nào")
	require.Contains(t, related.Prompt, "tích hợp dịch vụ công trực tuyến")
	require.Len(t, related.Items, 1, "an interest the question matches is a real recall")
}

// Relevance does not decide whether interests appear, it decides which ones
// survive the cap.
func TestInterestsBeyondTheCapAreChosenByRelevance(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")
	topics := []string{
		"số hóa hồ sơ lưu trữ", "cải cách hành chính", "thẩm định dự thảo nghị quyết", "quản lý văn bản đi đến",
		"chế độ báo cáo định kỳ", "tích hợp dịch vụ công trực tuyến",
	}
	for _, topic := range topics {
		_, err := svc.CreateItem(ctx, types.MemoryKindInterest, topic, 3)
		require.NoError(t, err)
	}

	recall := svc.Recall(ctx, "tích hợp dịch vụ công trực tuyến thế nào")
	require.Contains(t, recall.Prompt, "tích hợp dịch vụ công trực tuyến",
		"the interest the question is about must not be the one dropped by the cap")

	var injected int
	for _, topic := range topics {
		if strings.Contains(recall.Prompt, topic) {
			injected++
		}
	}
	require.Equal(t, types.MemoryResidentInterestMaxItems, injected,
		"the block carries at most the cap, not every interest ever promoted")
}

func TestCreateItemFromManagerGoesThroughTheWritePath(t *testing.T) {
	svc, _, tenantRepo := newMemoryHarness(t)
	ctx := enabledCtx(t, tenantRepo, 1, "alice")

	_, err := svc.CreateItem(ctx, types.MemoryKindPreference, "  Trả lời bằng tiếng Việt  \n", 0)
	require.NoError(t, err)

	items, _, err := svc.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "Trả lời bằng tiếng Việt", items[0].Content, "manual input must be sanitized like any other")
	require.Equal(t, types.MemoryOriginManual, items[0].Origin)
	require.False(t, strings.Contains(items[0].Content, "\n"))
}
