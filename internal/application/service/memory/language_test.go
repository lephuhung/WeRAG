package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/types"
)

// The memory prompts used to teach by Chinese examples only, and the model
// copied the language of the examples: Vietnamese conversations were stored
// as Chinese notes and subjects.
func TestMemoryPromptsCarryNoChineseExamples(t *testing.T) {
	for name, prompt := range map[string]string{
		"extraction":    extractionSystemPrompt,
		"consolidation": consolidationSystemPrompt,
		"adjudication":  topicAdjudicationPrompt,
	} {
		require.False(t, types.ContainsHan(prompt), "%s prompt still contains Chinese", name)
	}
	prompt := buildExtractionPrompt(
		transcriptSegment{lines: []transcriptLine{{content: "Hỏi về thủ tục"}}}, nil, nil,
		[]*types.MemoryTopicStat{{Topic: "cấp phép xây dựng"}}, "")
	require.False(t, types.ContainsHan(prompt))
}

func TestDropForeignScriptRemovesChineseForVietnameseTranscript(t *testing.T) {
	segment := transcriptSegment{lines: []transcriptLine{{content: "Tôi là cán bộ UBND xã"}}}
	parsed := extractionResponse{
		Memories: []extractionDecision{
			{Action: "add", Topic: "岗位背景", Content: "用户在乡镇政府担任干部"},
			{Action: "add", Topic: "công việc", Content: "Cán bộ UBND xã"},
			// An update may echo the stored (Chinese) topic; only its content counts.
			{Action: "update", Topic: "岗位背景", Content: "Cán bộ UBND xã"},
		},
		Topics: []string{"乡镇社保法规", "bảo hiểm xã hội cán bộ xã"},
	}
	got := dropForeignScript(context.Background(), segment, parsed)
	require.Len(t, got.Memories, 2)
	require.Equal(t, "công việc", got.Memories[0].Topic)
	require.Equal(t, "update", got.Memories[1].Action)
	require.Equal(t, []string{"bảo hiểm xã hội cán bộ xã"}, got.Topics)
}

func TestDropForeignScriptKeepsChineseForChineseTranscript(t *testing.T) {
	segment := transcriptSegment{lines: []transcriptLine{{content: "我在做后端"}}}
	parsed := extractionResponse{
		Memories: []extractionDecision{{Action: "add", Topic: "职业", Content: "做后端"}},
		Topics:   []string{"后端开发"},
	}
	got := dropForeignScript(context.Background(), segment, parsed)
	require.Len(t, got.Memories, 1)
	require.Len(t, got.Topics, 1)
}
