package chatpipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestGetEnrichedPassageKeepsQuestionsFromEarlierRevision(t *testing.T) {
	metadata, err := json.Marshal(types.DocumentChunkMetadata{
		GeneratedQuestionsRevision: 1,
		GeneratedQuestions: []types.GeneratedQuestion{{
			ID: "old", Question: "question generated before the edit",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	passage := getEnrichedPassage(context.Background(), &types.SearchResult{
		Content:       "edited chunk body",
		ChunkMetadata: types.JSON(metadata),
	})
	if !strings.Contains(passage, "question generated before the edit") {
		t.Fatalf("earlier generated question was excluded from rerank passage: %q", passage)
	}
}

func TestGetEnrichedPassageKeepsCodeAndMathCandidates(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "code only",
			content: "```go\nfunc answer() int { return 42 }\n```",
			want:    "func answer() int { return 42 }",
		},
		{
			name:    "math only",
			content: "$$\nE = mc^2\n$$",
			want:    "E = mc^2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passage := getEnrichedPassage(context.Background(), &types.SearchResult{Content: tt.content})
			if strings.TrimSpace(passage) == "" {
				t.Fatal("semantic-only candidate was removed from the rerank passage")
			}
			if !strings.Contains(passage, tt.want) {
				t.Fatalf("rerank passage %q does not preserve %q", passage, tt.want)
			}
		})
	}
}

func TestCleanPassageForRerank(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "plain text unchanged",
			input:  "Đây là một đoạn văn bản thông thường",
			expect: "Đây là một đoạn văn bản thông thường",
		},
		{
			name:   "remove markdown images",
			input:  "Phần trước ![mô tả ảnh](https://example.com/img.png) phần sau",
			expect: "Phần trước  phần sau",
		},
		{
			name:   "convert markdown links to text",
			input:  "Xem [tài liệu chính thức](https://docs.example.com) để biết chi tiết",
			expect: "Xem tài liệu chính thức để biết chi tiết",
		},
		{
			name:   "remove standalone URLs",
			input:  "Truy cập https://example.com/path?q=1&b=2 để biết thêm thông tin",
			expect: "Truy cập  để biết thêm thông tin",
		},
		{
			name:   "unwrap code blocks",
			input:  "Mã ví dụ:\n```python\nprint('hello')\n```\nTrên đây là ví dụ",
			expect: "Mã ví dụ:\nprint('hello')\nTrên đây là ví dụ",
		},
		{
			name:   "unwrap LaTeX blocks",
			input:  "Công thức như sau $$E=mc^2$$ trong đó E là năng lượng",
			expect: "Công thức như sau E=mc^2 trong đó E là năng lượng",
		},
		{
			name:   "remove table separator rows and convert data rows",
			input:  "| Tên | Giá trị |\n| --- | --- |\n| A | 1 |",
			expect: "Tên, Giá trị\n\nA, 1",
		},
		{
			name:   "strip heading markers",
			input:  "## Chương II Tổng quan\n### 2.1 Bối cảnh",
			expect: "Chương II Tổng quan\n2.1 Bối cảnh",
		},
		{
			name:   "strip blockquote markers",
			input:  "> Đây là một đoạn trích dẫn\n> Dòng trích dẫn thứ hai",
			expect: "Đây là một đoạn trích dẫn\nDòng trích dẫn thứ hai",
		},
		{
			name:   "unwrap bold and italic",
			input:  "Đây là văn bản **đậm** và *nghiêng* cùng ***đậm nghiêng***",
			expect: "Đây là văn bản đậm và nghiêng cùng đậm nghiêng",
		},
		{
			name:   "strip list markers",
			input:  "- Mục một\n- Mục hai\n1. Thứ tự một\n2. Thứ tự hai",
			expect: "Mục một\nMục hai\nThứ tự một\nThứ tự hai",
		},
		{
			name:   "remove HTML tags",
			input:  "Văn bản<br>xuống dòng<div class=\"test\">nội dung</div>kết thúc",
			expect: "Văn bảnxuống dòngnội dungkết thúc",
		},
		{
			name:   "collapse excessive newlines",
			input:  "Đoạn một\n\n\n\n\nĐoạn hai",
			expect: "Đoạn một\n\nĐoạn hai",
		},
		{
			name: "combined real-world passage",
			input: `## Giới thiệu sản phẩm

Đây là một sản phẩm **quan trọng**. Xem [trang sản phẩm](https://example.com/product).

![ảnh chụp sản phẩm](images/product.png)

> Đánh giá của người dùng: rất dễ dùng

- Tính năng một
- Tính năng hai

` + "```json\n{\"key\": \"value\"}\n```",
			expect: "Giới thiệu sản phẩm\n\nĐây là một sản phẩm quan trọng. Xem trang sản phẩm.\n\nĐánh giá của người dùng: rất dễ dùng\n\nTính năng một\nTính năng hai\n\n{\"key\": \"value\"}",
		},
		{
			name:   "convert table data rows to plain text",
			input:  "| col1 | col2 | col3 |",
			expect: "col1, col2, col3",
		},
		{
			name:   "multi-row table fully converted",
			input:  "| Header1 | Header2 |\n| --- | --- |\n| data1 | data2 |\n| data3 | data4 |",
			expect: "Header1, Header2\n\ndata1, data2\ndata3, data4",
		},
		{
			name:   "table-only passage becomes empty after separator removal",
			input:  "| --- | --- |",
			expect: "",
		},
		{
			name:   "whitespace-only after cleaning",
			input:  "   \n\n   ",
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanPassageForRerank(tt.input)
			if got != tt.expect {
				t.Errorf("cleanPassageForRerank():\ngot:    %q\nexpect: %q", got, tt.expect)
			}
		})
	}
}
