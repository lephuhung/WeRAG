package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubKnowledgeBaseService struct {
	kb      *types.KnowledgeBase
	results []*types.SearchResult
}

func (s *stubKnowledgeBaseService) CreateKnowledgeBase(context.Context, *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *stubKnowledgeBaseService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *stubKnowledgeBaseService) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) FillKnowledgeBaseCounts(context.Context, *types.KnowledgeBase) error {
	return nil
}

func (s *stubKnowledgeBaseService) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) ListKnowledgeBasesByTenantID(context.Context, uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) UpdateKnowledgeBase(
	context.Context,
	string,
	string,
	string,
	*types.KnowledgeBaseConfig,
) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) DeleteKnowledgeBase(context.Context, string) error {
	return nil
}

func (s *stubKnowledgeBaseService) TogglePinKnowledgeBase(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	return s.results, nil
}

func (s *stubKnowledgeBaseService) GetQueryEmbedding(context.Context, string, string) ([]float32, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) ResolveEmbeddingModelKeys(context.Context, []*types.KnowledgeBase) map[string]string {
	return nil
}

func (s *stubKnowledgeBaseService) CopyKnowledgeBase(
	context.Context,
	string,
	string,
) (*types.KnowledgeBase, *types.KnowledgeBase, error) {
	return nil, nil, nil
}

func (s *stubKnowledgeBaseService) DuplicateKnowledgeBase(
	context.Context,
	string,
) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) GetRepository() interfaces.KnowledgeBaseRepository {
	return nil
}

func (s *stubKnowledgeBaseService) ProcessKBDelete(context.Context, *asynq.Task) error {
	return nil
}

func TestQueryKnowledgeGraph_ReportsConfiguredEntityAndRelationTypes(t *testing.T) {
	tool := NewQueryKnowledgeGraphTool(&stubKnowledgeBaseService{
		kb: &types.KnowledgeBase{
			ID: "kb-1",
			ExtractConfig: &types.ExtractConfig{
				Enabled: true,
				Nodes: []*types.GraphNode{
					{Name: "Hợp đồng"},
					{Name: "Phòng Pháp chế"},
					{Name: "Quy trình phê duyệt"},
					{Name: "Hợp đồng"},
					nil,
					{Name: ""},
				},
				Relations: []*types.GraphRelation{
					{Type: "thuộc về"},
					{Type: "quản lý"},
					{Type: "phê duyệt"},
					{Type: "quản lý"},
					{Type: ""},
					nil,
				},
			},
		},
		results: []*types.SearchResult{
			{
				ID:             "chunk-approval-1",
				Content:        "Quy trình phê duyệt hợp đồng do Phòng Pháp chế và Phòng Mua sắm cùng duy trì, Phòng Pháp chế chịu trách nhiệm rà soát tuân thủ.",
				KnowledgeID:    "doc-approval",
				KnowledgeTitle: "Quy chế quản lý phê duyệt hợp đồng",
				Score:          0.97,
				MatchType:      types.MatchTypeEmbedding,
			},
			{
				ID:             "chunk-approval-2",
				Content:        "Sau khi nộp đề nghị mua sắm sẽ vào quy trình phê duyệt hợp đồng, phê duyệt xong thì lưu vào sổ theo dõi hợp đồng.",
				KnowledgeID:    "doc-procurement",
				KnowledgeTitle: "Quy định phối hợp mua sắm và hợp đồng",
				Score:          0.89,
				MatchType:      types.MatchTypeKeywords,
			},
			{
				ID:             "chunk-approval-3",
				Content:        "Phòng Pháp chế quản lý mẫu hợp đồng chuẩn và duy trì danh mục rà soát rủi ro hợp đồng.",
				KnowledgeID:    "doc-legal",
				KnowledgeTitle: "Bản mô tả nhiệm vụ Phòng Pháp chế",
				Score:          0.84,
				MatchType:      types.MatchTypeEmbedding,
			},
		},
	})

	args, err := json.Marshal(QueryKnowledgeGraphInput{
		KnowledgeBaseIDs: []string{"kb-1"},
		Query:            "Phê duyệt hợp đồng và phối hợp pháp chế",
	})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success)
	t.Logf("tool output:\n%s", result.Output)

	assert.Contains(t, result.Output, "Entity Types (3)")
	assert.Contains(t, result.Output, "Relationship Types (3)")
	assert.NotContains(t, result.Output, "No entity types configured")
	assert.NotContains(t, result.Output, "No relationship types configured")
	assert.Contains(t, result.Output, "Hợp đồng")
	assert.Contains(t, result.Output, "Phòng Pháp chế")
	assert.Contains(t, result.Output, "Quy trình phê duyệt")
	assert.Contains(t, result.Output, "quản lý")
	assert.Contains(t, result.Output, "phê duyệt")
	assert.Contains(t, result.Output, "✓ Found 3 relevant results (deduplicated)")
	assert.Contains(t, result.Output, "Result #1:")
	assert.Contains(t, result.Output, "Result #2:")
	assert.Contains(t, result.Output, "Result #3:")
	assert.Contains(t, result.Output, "Quy chế quản lý phê duyệt hợp đồng")

	graphConfig, ok := result.Data["graph_config"].(map[string]interface{})
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"Hợp đồng", "Quy trình phê duyệt", "Phòng Pháp chế"}, graphConfig["nodes"])
	assert.ElementsMatch(t, []string{"thuộc về", "phê duyệt", "quản lý"}, graphConfig["relations"])
}

func (s *stubKnowledgeBaseService) CreatePublicKnowledgeBase(context.Context, *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) ListPublicCatalog(
	context.Context, int, int, string,
) ([]*types.KnowledgeBase, int64, error) {
	return nil, 0, nil
}

func (s *stubKnowledgeBaseService) ListPublishedCatalog(
	context.Context, int, int, string,
) ([]*types.KnowledgeBase, int64, error) {
	return nil, 0, nil
}

func (s *stubKnowledgeBaseService) SubscribeKnowledgeBase(context.Context, string, bool) error {
	return nil
}

func (s *stubKnowledgeBaseService) UnsubscribeKnowledgeBase(context.Context, string, bool) error {
	return nil
}

func (s *stubKnowledgeBaseService) SetKnowledgeBaseVisibility(
	ctx context.Context, id string, visibility types.KBVisibility, targetTenantID uint64,
) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) SetKnowledgeBaseMemberDefault(
	ctx context.Context, id string, memberDefault types.KBMemberDefault,
) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *stubKnowledgeBaseService) TransferKnowledgeBaseOwner(
	ctx context.Context, id string, targetTenantID uint64,
) (*types.KBOwnerTransferResult, error) {
	return nil, nil
}
