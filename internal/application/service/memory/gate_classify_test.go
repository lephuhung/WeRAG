package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// labelModel answers the classifier with a fixed label per message text.
type labelModel struct {
	labels []labelRule
	fail   bool
	calls  int
	opts   *chat.ChatOptions
}

func (m *labelModel) Chat(_ context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	m.calls++
	m.opts = opts
	if m.fail {
		return nil, errors.New("model down")
	}
	text := messages[len(messages)-1].Content
	for _, r := range m.labels {
		if strings.Contains(text, r.marker) {
			return &types.ChatResponse{Content: r.reply}, nil
		}
	}
	return &types.ChatResponse{Content: `{"label":"self"}`}, nil
}

type labelRule struct{ marker, reply string }

func (m *labelModel) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("unused")
}
func (m *labelModel) GetModelName() string { return "label" }
func (m *labelModel) GetModelID() string   { return "label" }

func TestClassifySentenceReadsTheLabel(t *testing.T) {
	m := &labelModel{labels: []labelRule{
		{"json", `{"label": "reported"}`},
		{"bare", "lookup"},
		{"fence", "```json\n{\"label\":\"document\"}\n```"},
		{"wrong", `{"label": "statement"}`},
		{"broken", `{label: self}`},
	}}
	for text, want := range map[string]sentenceClass{"json": classReported, "bare": classLookup, "fence": classDocument} {
		got, err := classifySentence(context.Background(), m, text)
		require.NoError(t, err, text)
		require.Equal(t, want, got, text)
	}
	_, err := classifySentence(context.Background(), m, "wrong")
	require.Error(t, err, "a label outside the set is not trusted")
	_, err = classifySentence(context.Background(), m, "broken")
	require.Error(t, err, "unparsable JSON is not read as a bare label")
	require.NotNil(t, m.opts.Thinking)
	require.False(t, *m.opts.Thinking)
	require.NotEmpty(t, m.opts.Format)
}

func TestGateClassifierCachesAndFallsBackToRules(t *testing.T) {
	m := &labelModel{labels: []labelRule{{"Mai phải nộp", `{"label":"self"}`}}}
	g := &gateClassifier{model: m, modelID: "label", cache: map[string]sentenceClass{}}
	ctx := context.Background()
	c, byModel := g.classOf(ctx, "Mai phải nộp báo cáo quý cho Sở Nội vụ")
	require.True(t, byModel)
	require.Equal(t, classSelf, c)
	g.classOf(ctx, "Mai phải nộp báo cáo quý cho Sở Nội vụ")
	require.Equal(t, 1, m.calls, "one call per sentence per run")

	m.fail = true
	c, byModel = g.classOf(ctx, "giá hornor magic 9 pro")
	require.False(t, byModel)
	require.Equal(t, classLookup, c, "the rules answer when the model fails")
	g.classOf(ctx, "Tôi là chuyên viên")
	require.Equal(t, 2, m.calls, "after a failure the rest of the run skips the model")

	var none *gateClassifier
	c, byModel = none.classOf(ctx, "chỗ HN nói là họ mua usdt")
	require.False(t, byModel)
	require.Equal(t, classReported, c)
}

// The code checks hold whatever the model says.
func TestGateForClassKeepsTheCodeChecks(t *testing.T) {
	fact := extractionDecision{Kind: types.MemoryKindFact, Content: "x"}
	_, v := gateForClass(fact, "Hãy nhớ: tôi phụ trách Đội 4", classOther)
	require.Equal(t, gateKept, v, "an explicit remember is kept even if the model calls it other")

	long := strings.Repeat("Căn cứ Luật Tổ chức chính quyền địa phương. ", 40)
	_, v = gateForClass(fact, long, classSelf)
	require.Equal(t, gateDropPasted, v, "a long paste is never the user speaking")

	_, v = gateForClass(fact, "là PA05 nhé", classOther)
	require.Equal(t, gateDropOther, v)
	got, v := gateForClass(fact, "Tôi công tác ở đâu?", classQuestion)
	require.Equal(t, gateDemotedToTask, v)
	require.Equal(t, types.MemoryKindTask, got.Kind)
	_, v = gateForClass(extractionDecision{Kind: types.MemoryKindProfile, Content: "x"}, "Soạn công văn", classCommand)
	require.Equal(t, gateDropProfile, v)
}

// End to end: with a classifier model configured, applyDecisions drops what
// the model calls someone else's words even where the word lists see nothing.
func TestApplyDecisionsUsesTheClassifierModel(t *testing.T) {
	s, _, tr := newMemoryHarness(t)
	ctx := enabledCtx(t, tr, 1, "alice")
	scope, cfg, ok := s.enabledScope(ctx)
	require.True(t, ok)
	m := &labelModel{labels: []labelRule{{"bên kia báo", `{"label":"reported"}`}}}
	s.modelService = &fixedModelService{stubModelService: &stubModelService{}, model: m}
	s.systemSettings = &auxSettings{stubPlatformSettings: &stubPlatformSettings{}, aux: "gemma"}

	segment := transcriptSegment{sessionID: "s", lines: []transcriptLine{
		{sessionID: "s", messageID: "m1", content: "Tiến độ bên kia báo chậm 2 tuần"},
		{sessionID: "s", messageID: "m2", content: "Tôi phụ trách Đội 4"},
	}}
	one, two := 1, 2
	require.NoError(t, s.applyDecisions(ctx, scope, cfg, segment, nil, []extractionDecision{
		{Action: "add", Source: &one, Kind: types.MemoryKindFact, Topic: "tiến độ", Content: "Dự án chậm 2 tuần"},
		{Action: "add", Source: &two, Kind: types.MemoryKindProfile, Topic: "đội", Content: "Phụ trách Đội 4"},
	}))
	items, total, err := s.ListItems(ctx, types.MemoryStatusActive, 10, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total, "%v", items)
	require.Equal(t, "Phụ trách Đội 4", items[0].Content)
	require.Equal(t, types.MemoryGateModel, items[0].GateSource, "the row records that the model judged it")
	require.Equal(t, 2, m.calls)

	// The daily rules-only re-check leaves the model's call alone: the rules
	// would drop this sentence ("theo lời" reads as someone else's words).
	stub := &stubMessageRepo{}
	s.messageRepo = stub
	stub.messages = []*types.Message{{ID: "m9", SessionID: "s", Role: "user", Content: "Theo lời sếp thì tuần sau em phải nộp báo cáo quý"}}
	modelKept, err := s.Remember(ctx, types.MemoryItem{Kind: types.MemoryKindTask, Topic: "báo cáo quý", Content: "Nộp báo cáo quý tuần sau",
		Origin: types.MemoryOriginExtracted, SourceSessionID: "s", SourceMessageID: "m9", GateSource: types.MemoryGateModel})
	require.NoError(t, err)
	kept, archived := s.regateStored(ctx, scope, []*types.MemoryItem{modelKept})
	require.Zero(t, archived)
	require.Len(t, kept, 1)
	require.Equal(t, gateDropReported, func() gateVerdict {
		_, v := gateDecision(extractionDecision{Kind: "task", Content: "x"}, stub.messages[0].Content)
		return v
	}(),
		"the rules alone would have archived it")
}

// The classifier model comes from the memory configuration, then the
// platform auxiliary model; "off" (setting or env) keeps the rules.
func TestGateModelIDFollowsTheConfiguration(t *testing.T) {
	s, _, _ := newMemoryHarness(t)
	ctx := context.Background()
	s.systemSettings = &auxSettings{stubPlatformSettings: &stubPlatformSettings{}, aux: "builtin-llm-gemma4-e4b"}
	require.Equal(t, "builtin-llm-gemma4-e4b", s.gateModelID(ctx, &types.MemoryConfig{}))
	require.Equal(t, "qwen-small", s.gateModelID(ctx, &types.MemoryConfig{GateModelID: "qwen-small"}))
	require.Equal(t, "", s.gateModelID(ctx, &types.MemoryConfig{GateModelID: "off"}))
	t.Setenv(gateClassifierEnv, "off")
	require.Equal(t, "", s.gateModelID(ctx, &types.MemoryConfig{GateModelID: "qwen-small"}))
}

type fixedModelService struct {
	*stubModelService
	model chat.Chat
}

func (f *fixedModelService) GetChatModel(context.Context, string) (chat.Chat, error) {
	return f.model, nil
}

type auxSettings struct {
	*stubPlatformSettings
	aux string
}

func (a *auxSettings) GetString(_ context.Context, key, _, def string) string {
	if key == types.AuxiliaryModelSettingKey {
		return a.aux
	}
	return def
}
