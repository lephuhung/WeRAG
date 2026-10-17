package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Vietnamese document-assistant eval for the extraction prompt.
//
// The Chinese golden set scores the general shape of distillation. This set
// scores what the deployment actually receives: commands about an open
// document, legal questions, and the occasional self-introduction, all in
// Vietnamese. It grades the two channels separately — memories (should a
// command produce one? is a guess marked inferred?) and topics (is the label
// an action, which the code filter would drop, or a field?) — so a prompt
// change can be measured against the write gate instead of around it.
//
//	WEKNORA_MEMORY_EVAL_MODEL=Qwen/Qwen3.6-35B-A3B-FP8 \
//	WEKNORA_MEMORY_EVAL_BASE_URL=http://10.10.0.240:8000/v1 \
//	[WEKNORA_MEMORY_EVAL_PROMPT_FILE=/path/variant.txt] \
//	[WEKNORA_MEMORY_EVAL_TEMPERATURE=0] [WEKNORA_MEMORY_EVAL_REPEAT=1] \
//	[WEKNORA_MEMORY_EVAL_OUT=/path/results.jsonl] \
//	go test ./internal/application/service/memory/ -run TestVietnamesePromptEval -v
//
// Results on 2026-10-17 (Qwen3.6-35B-A3B-FP8 at 10.10.0.240, 40 cases):
//
//	prompt before the rewrite   t=0  19/40   t=0.7 x2  43/80   action-shaped topics 16/45
//	rewrite (now in extract.go) t=0  40/40   t=0.7 x3 120/120  action-shaped topics  0/24
//
// The rewrite stopped naming the action as a topic, stopped recording
// document commands as tasks/preferences, and stopped guessing an identity
// from a single question (six inferred profiles out of eight legal
// questions before; none after). The behaviour-level golden set
// (evalset.json, TestPromptEval) was translated to Vietnamese the same day
// and scores 8–10/10 run to run: vLLM is not deterministic even at
// temperature 0, so compare prompts on repeated runs, never on one.

type viEvalCase struct {
	Name     string             `json:"name"`
	Group    string             `json:"group"`
	Context  []string           `json:"context"`
	Lines    []string           `json:"lines"`
	Existing []evalExistingNote `json:"existing"`

	MemMin           *int     `json:"mem_min"`
	MemMax           *int     `json:"mem_max"`
	Kinds            []string `json:"kinds"`
	NotInferredKinds []string `json:"not_inferred_kinds"`
	// InferredOnly: any profile/preference produced must be marked inferred.
	InferredOnly     bool     `json:"inferred_only"`
	Actions          []string `json:"actions"`
	Targets          []int    `json:"targets"`
	ExpectSubstrings []string `json:"expect_substrings"`
	ExpectExpiry     bool     `json:"expect_expiry"`

	TopicsEmpty    bool     `json:"topics_empty"`
	TopicsNoAction bool     `json:"topics_no_action"`
	TopicAny       []string `json:"topic_any"`
}

type viEvalSet struct {
	Cases []viEvalCase `json:"cases"`
}

func loadViEvalSet(t *testing.T) viEvalSet {
	t.Helper()
	raw, err := os.ReadFile("evalset_vi.json")
	require.NoError(t, err)
	var set viEvalSet
	require.NoError(t, json.Unmarshal(raw, &set))
	require.NotEmpty(t, set.Cases)
	return set
}

func TestViEvalSetIsWellFormed(t *testing.T) {
	set := loadViEvalSet(t)
	seen := map[string]bool{}
	for _, c := range set.Cases {
		require.NotEmpty(t, c.Name)
		require.False(t, seen[c.Name], "duplicate case %q", c.Name)
		seen[c.Name] = true
		require.NotEmpty(t, c.Lines, c.Name)
		require.NotEmpty(t, c.Group, c.Name)
	}
}

type viGrade struct {
	name, group string
	failures    []string
	// counted for the summary
	memories     int
	actionTopics int
	topics       int
	gateDropped  int
}

func gradeViCase(c viEvalCase, resp extractionResponse) viGrade {
	g := viGrade{name: c.Name, group: c.Group}
	fail := func(format string, args ...any) { g.failures = append(g.failures, fmt.Sprintf(format, args...)) }

	var kept []extractionDecision
	for _, d := range resp.Memories {
		action := strings.ToLower(strings.TrimSpace(d.Action))
		if action == "" || action == "none" || action == "noop" {
			continue
		}
		kept = append(kept, d)
		if action == "add" || action == "update" {
			segment := segmentForViCase(c)
			if _, verdict := gateDecision(d, d.resolveSource(segment).content); verdict.dropped() {
				g.gateDropped++
			}
		}
	}
	g.memories = len(kept)
	if c.MemMin != nil && len(kept) < *c.MemMin {
		fail("expected at least %d memories, got %d", *c.MemMin, len(kept))
	}
	if c.MemMax != nil && len(kept) > *c.MemMax {
		fail("expected at most %d memories, got %d: %s", *c.MemMax, len(kept), decisionsBlob(kept))
	}
	blob := strings.ToLower(decisionsBlob(kept))
	for _, want := range c.ExpectSubstrings {
		if !strings.Contains(blob, strings.ToLower(want)) {
			fail("missing %q", want)
		}
	}
	for _, kind := range c.Kinds {
		if !hasField(kept, func(d extractionDecision) bool { return strings.EqualFold(d.Kind, kind) }) {
			fail("no memory of kind %q", kind)
		}
	}
	for _, kind := range c.NotInferredKinds {
		if hasField(kept, func(d extractionDecision) bool { return strings.EqualFold(d.Kind, kind) && d.Inferred }) {
			fail("%s was marked inferred although the user stated it", kind)
		}
	}
	if c.InferredOnly {
		for _, d := range kept {
			k := strings.ToLower(d.Kind)
			if (k == types.MemoryKindProfile || k == types.MemoryKindPreference) && !d.Inferred {
				fail("%s %q asserted from a question without inferred=true", k, d.Content)
			}
		}
	}
	for _, action := range c.Actions {
		if !hasField(kept, func(d extractionDecision) bool { return strings.EqualFold(d.Action, action) }) {
			fail("no %q action", action)
		}
	}
	for _, target := range c.Targets {
		if !hasField(kept, func(d extractionDecision) bool { return d.Target != nil && *d.Target == target }) {
			fail("no decision targeting note %d", target)
		}
	}
	if c.ExpectExpiry && !hasField(kept, func(d extractionDecision) bool { return parseExpiry(d.ExpiresAt) != nil }) {
		fail("no usable expires_at")
	}

	var topics []string
	for _, topic := range resp.Topics {
		if topic = types.SanitizeMemoryTopic(topic); topic != "" {
			topics = append(topics, topic)
		}
	}
	g.topics = len(topics)
	for _, topic := range topics {
		if topicIsAnAction(topic) {
			g.actionTopics++
		}
	}
	if c.TopicsEmpty && len(topics) > 0 {
		fail("expected no topics, got %v", topics)
	}
	if (c.TopicsNoAction || c.TopicsEmpty) && g.actionTopics > 0 {
		fail("action-shaped topics: %v", topics)
	}
	if len(c.TopicAny) > 0 {
		found := false
		for _, topic := range topics {
			for _, want := range c.TopicAny {
				if strings.Contains(strings.ToLower(topic), strings.ToLower(want)) {
					found = true
				}
			}
		}
		if !found {
			fail("no topic naming %v, got %v", c.TopicAny, topics)
		}
	}
	return g
}

func segmentForViCase(c viEvalCase) transcriptSegment {
	now := time.Now()
	segment := transcriptSegment{sessionID: "eval", context: c.Context}
	for i, line := range c.Lines {
		segment.lines = append(segment.lines, transcriptLine{
			sessionID: "eval", messageID: fmt.Sprintf("m%d", i), at: now.Add(time.Duration(i) * time.Minute), content: line,
		})
	}
	return segment
}

func existingForViCase(c viEvalCase) []*types.MemoryItem {
	items := make([]*types.MemoryItem, 0, len(c.Existing))
	for _, note := range c.Existing {
		items = append(items, &types.MemoryItem{Kind: note.Kind, Topic: note.Topic, Content: note.Content})
	}
	return items
}

func newViEvalChatModel(modelID string) (chat.Chat, error) {
	baseURL := strings.TrimSpace(firstNonEmpty(os.Getenv("WEKNORA_MEMORY_EVAL_BASE_URL"), os.Getenv("OPENAI_BASE_URL")))
	if baseURL == "" {
		return nil, fmt.Errorf("set WEKNORA_MEMORY_EVAL_BASE_URL")
	}
	return chat.NewChat(&chat.ChatConfig{
		Source: types.ModelSourceRemote, ModelName: modelID, BaseURL: baseURL,
		APIKey:   strings.TrimSpace(firstNonEmpty(os.Getenv("WEKNORA_MEMORY_EVAL_API_KEY"), os.Getenv("OPENAI_API_KEY"), "none")),
		Provider: string(provider.ProviderGeneric), // vLLM: thinking off via chat_template_kwargs, as in production
	}, nil)
}

func runViExtraction(ctx context.Context, chatModel chat.Chat, systemPrompt, userPrompt string, temperature float64) (extractionResponse, string, error) {
	thinking := false
	response, err := chatModel.Chat(ctx, []chat.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, &chat.ChatOptions{
		Temperature: temperature, MaxCompletionTokens: extractBudgetTokens, Thinking: &thinking, Format: extractionSchema,
	})
	if err != nil {
		return extractionResponse{}, "", err
	}
	if response == nil {
		return extractionResponse{}, "", fmt.Errorf("empty response")
	}
	parsed, err := parseExtractionResponse(response.Content)
	return parsed, response.Content, err
}

// Skipped unless WEKNORA_MEMORY_EVAL_MODEL is set.
func TestVietnamesePromptEval(t *testing.T) {
	modelID := strings.TrimSpace(os.Getenv("WEKNORA_MEMORY_EVAL_MODEL"))
	if modelID == "" {
		t.Skip("set WEKNORA_MEMORY_EVAL_MODEL (plus base URL) to score the prompt on Vietnamese traffic")
	}
	chatModel, err := newViEvalChatModel(modelID)
	require.NoError(t, err)

	systemPrompt := extractionSystemPrompt
	promptLabel := "current"
	if path := strings.TrimSpace(os.Getenv("WEKNORA_MEMORY_EVAL_PROMPT_FILE")); path != "" {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		systemPrompt = string(raw)
		promptLabel = path
	}
	temperature := 0.0
	if v := os.Getenv("WEKNORA_MEMORY_EVAL_TEMPERATURE"); v != "" {
		temperature, err = strconv.ParseFloat(v, 64)
		require.NoError(t, err)
	}
	repeat := 1
	if v := os.Getenv("WEKNORA_MEMORY_EVAL_REPEAT"); v != "" {
		repeat, err = strconv.Atoi(v)
		require.NoError(t, err)
	}
	var out *os.File
	if path := strings.TrimSpace(os.Getenv("WEKNORA_MEMORY_EVAL_OUT")); path != "" {
		out, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		require.NoError(t, err)
		defer out.Close()
	}

	set := loadViEvalSet(t)
	type tally struct{ runs, passed, memories, gateDropped, topics, actionTopics int }
	byGroup := map[string]*tally{}
	total := &tally{}
	for _, c := range set.Cases {
		userPrompt := buildExtractionPrompt(segmentForViCase(c), existingForViCase(c), nil, nil, "")
		for r := 0; r < repeat; r++ {
			resp, raw, err := runViExtraction(context.Background(), chatModel, systemPrompt, userPrompt, temperature)
			if err != nil {
				t.Errorf("case %q: %v", c.Name, err)
				continue
			}
			g := gradeViCase(c, resp)
			for _, tl := range []*tally{total, groupTally(byGroup, c.Group)} {
				tl.runs++
				tl.memories += g.memories
				tl.gateDropped += g.gateDropped
				tl.topics += g.topics
				tl.actionTopics += g.actionTopics
				if len(g.failures) == 0 {
					tl.passed++
				}
			}
			if len(g.failures) == 0 {
				t.Logf("PASS  %-28s topics=%v", c.Name, resp.Topics)
			} else {
				t.Logf("FAIL  %-28s %s", c.Name, strings.Join(g.failures, " | "))
			}
			if out != nil {
				line, _ := json.Marshal(map[string]any{
					"prompt": promptLabel, "temperature": temperature, "case": c.Name, "group": c.Group,
					"failures": g.failures, "memories": resp.Memories, "topics": resp.Topics, "raw": raw,
				})
				fmt.Fprintln(out, string(line))
			}
		}
	}
	groups := make([]string, 0, len(byGroup))
	for g := range byGroup {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	t.Logf("=== prompt=%s model=%s temperature=%.1f repeat=%d", promptLabel, modelID, temperature, repeat)
	for _, g := range groups {
		tl := byGroup[g]
		t.Logf("%-22s pass %2d/%2d  memories=%2d (gate would drop %2d)  topics=%2d action-shaped=%2d",
			g, tl.passed, tl.runs, tl.memories, tl.gateDropped, tl.topics, tl.actionTopics)
	}
	t.Logf("%-22s pass %2d/%2d  memories=%2d (gate would drop %2d)  topics=%2d action-shaped=%2d",
		"TOTAL", total.passed, total.runs, total.memories, total.gateDropped, total.topics, total.actionTopics)
}

func groupTally[T any](m map[string]*T, key string) *T {
	if m[key] == nil {
		m[key] = new(T)
	}
	return m[key]
}
