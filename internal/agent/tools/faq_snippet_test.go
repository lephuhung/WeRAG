package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestWriteFAQMetadataXML_IncludesAnswers(t *testing.T) {
	var b strings.Builder
	meta := &types.FAQChunkMetadata{
		StandardQuestion: "Làm thế nào để tạo kho tri thức?",
		SimilarQuestions: []string{"Tạo kho tri thức bằng cách nào?"},
		Answers:          []string{"Trong bảng điều khiển, bấm Tạo kho tri thức mới."},
	}
	writeFAQMetadataXML(&b, meta)
	out := b.String()

	for _, want := range []string{
		"<faq>", "<question>Làm thế nào để tạo kho tri thức?</question>",
		"<similar_question>Tạo kho tri thức bằng cách nào?</similar_question>",
		"<answer>Trong bảng điều khiển, bấm Tạo kho tri thức mới.</answer>", "</faq>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteSimilarQuestionsXML_TruncatesWhenTooMany(t *testing.T) {
	var b strings.Builder
	questions := make([]string, 8)
	for i := range questions {
		questions[i] = fmt.Sprintf("Câu hỏi tương tự %d", i+1)
	}
	writeSimilarQuestionsXML(&b, questions)
	out := b.String()

	if strings.Count(out, "<similar_question>") != faqMaxSimilarQuestionsDisplay {
		t.Fatalf("want %d similar_question tags, got:\n%s", faqMaxSimilarQuestionsDisplay, out)
	}
	if !strings.Contains(out, `<similar_questions_omitted count="3"`) {
		t.Fatalf("want omitted marker, got:\n%s", out)
	}
}

func TestWriteFAQEntryXML_UsesFaqNotChunk(t *testing.T) {
	chunk := &types.Chunk{
		ID:          "faq-chunk-1",
		ChunkType:   types.ChunkTypeFAQ,
		ChunkIndex:  0,
		KnowledgeID: "kb-doc-1",
		Content:     "Q: test\nSimilar Questions:\n- alt",
	}
	meta := &types.FAQChunkMetadata{
		StandardQuestion: "Làm thế nào để tạo kho tri thức?",
		Answers:          []string{"Chỉ cần bấm Tạo mới."},
	}
	if err := chunk.SetFAQMetadata(meta); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	writeFAQEntryXML(&b, chunk)
	out := b.String()

	if strings.Contains(out, "<chunk") {
		t.Fatalf("FAQ list output must not use <chunk>, got:\n%s", out)
	}
	for _, want := range []string{
		`<faq faq_id="faq-chunk-1"`,
		"<question>Làm thế nào để tạo kho tri thức?</question>",
		"<answer>Chỉ cần bấm Tạo mới.</answer>",
		"</faq>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
