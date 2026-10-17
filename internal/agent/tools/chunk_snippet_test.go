package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func faqChunkForSnippetTest(t *testing.T) *types.Chunk {
	t.Helper()
	chunk := &types.Chunk{
		ChunkType: types.ChunkTypeFAQ,
		Content: `Q: Lý do nghỉ việc có thể tự định nghĩa không
Similar Questions:
- Tùy chỉnh lý do nghỉ việc
- Thiết lập lý do nghỉ việc`,
	}
	meta := &types.FAQChunkMetadata{
		StandardQuestion: "Lý do nghỉ việc có thể tự định nghĩa không",
		SimilarQuestions: []string{"Tùy chỉnh lý do nghỉ việc", "Thiết lập lý do nghỉ việc"},
		Answers:          []string{"Có thể, tự định nghĩa lý do nghỉ việc trong phần cài đặt hệ thống."},
	}
	if err := chunk.SetFAQMetadata(meta); err != nil {
		t.Fatalf("SetFAQMetadata: %v", err)
	}
	return chunk
}

func TestFaqMatchSnippet_StandardQuestionAndMetadataAnswer(t *testing.T) {
	chunk := faqChunkForSnippetTest(t)
	re := regexp.MustCompile(`(?i)có thể tự định nghĩa không`)
	snippet := faqMatchSnippet(chunk, []*regexp.Regexp{re})

	if !strings.Contains(snippet, "Q: Lý do nghỉ việc có thể tự định nghĩa không") {
		t.Fatalf("want standard question in snippet, got: %s", snippet)
	}
	if !strings.Contains(snippet, "A: Có thể, tự định nghĩa lý do") {
		t.Fatalf("want metadata answer in snippet, got: %s", snippet)
	}
	for _, unwanted := range []string{"Similar Questions", "Tùy chỉnh lý do nghỉ việc", "Thiết lập lý do nghỉ việc"} {
		if strings.Contains(snippet, unwanted) {
			t.Errorf("snippet should not include similar-question noise %q: %s", unwanted, snippet)
		}
	}
}

func TestFaqMatchSnippetFromQueries_StandardQuestionAndMetadataAnswer(t *testing.T) {
	meta := &types.FAQChunkMetadata{
		StandardQuestion: "Lý do nghỉ việc có thể tự định nghĩa không",
		SimilarQuestions: []string{"Tùy chỉnh lý do nghỉ việc", "Thiết lập lý do nghỉ việc"},
		Answers:          []string{"Có thể, tự định nghĩa lý do nghỉ việc trong phần cài đặt hệ thống."},
	}
	snippet := faqMatchSnippetFromQueries(meta, []string{"có thể tự định nghĩa không"})

	if !strings.Contains(snippet, "Q: Lý do nghỉ việc có thể tự định nghĩa không") {
		t.Fatalf("want standard question, got: %s", snippet)
	}
	if !strings.Contains(snippet, "A: Có thể, tự định nghĩa lý do") {
		t.Fatalf("want metadata answer, got: %s", snippet)
	}
	if strings.Contains(snippet, "Similar Questions") {
		t.Fatalf("should not include similar-question list: %s", snippet)
	}
}

func TestFaqMatchSnippetFromQueries_SimilarQuestionHitShowsMatchedVariant(t *testing.T) {
	meta := &types.FAQChunkMetadata{
		StandardQuestion: "Lý do nghỉ việc có thể tự định nghĩa không",
		SimilarQuestions: []string{"Tùy chỉnh lý do nghỉ việc", "Thiết lập lý do nghỉ việc"},
		Answers:          []string{"Có thể tự định nghĩa."},
	}
	snippet := faqMatchSnippetFromQueries(meta, []string{"Tùy chỉnh lý do nghỉ việc"})

	if !strings.Contains(snippet, "Q: Tùy chỉnh lý do nghỉ việc") {
		t.Fatalf("want matched similar question, got: %s", snippet)
	}
}

func TestFaqMatchSnippet_SimilarQuestionHitShowsMatchedVariant(t *testing.T) {
	chunk := faqChunkForSnippetTest(t)
	re := regexp.MustCompile(`(?i)Tùy chỉnh lý do nghỉ việc`)
	snippet := faqMatchSnippet(chunk, []*regexp.Regexp{re})

	if !strings.Contains(snippet, "Q: Tùy chỉnh lý do nghỉ việc") {
		t.Fatalf("want matched similar question, got: %s", snippet)
	}
	if strings.Contains(snippet, "có thể tự định nghĩa không") {
		t.Fatalf("should not show standard question when similar question matched: %s", snippet)
	}
}

func TestExtractChunkMatchSnippet_NonFAQUsesBodyContext(t *testing.T) {
	content := strings.Repeat("Phần trước. ", 50) + "TARGET" + strings.Repeat("Phần sau. ", 50)
	chunk := &types.Chunk{ChunkType: types.ChunkTypeText, Content: content}
	re := regexp.MustCompile(`TARGET`)
	snippet := extractChunkMatchSnippet(chunk, []*regexp.Regexp{re})

	if !strings.Contains(snippet, "TARGET") {
		t.Fatalf("missing match: %s", snippet)
	}
	if !strings.Contains(snippet, "Phần trước") || !strings.Contains(snippet, "Phần sau") {
		t.Fatalf("expected expanded body context: %s", snippet)
	}
}
