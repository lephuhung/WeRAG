package searchutil

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestAppendWithOverlap_ContiguousNoTrim(t *testing.T) {
	// Hai đoạn nối tiếp nhau (không chồng lấn), và next chứa thực thể HTML nên số ký tự > EndAt-StartAt.
	// Công thức cắt theo vị trí cũ sẽ cắt mất phần đầu; ở đây phải giữ nguyên cả đoạn.
	acc := "## Mục 2\n\n"
	next := "| Cột A | Cột B |\n| Giá trị 1 | Nội dung có thực thể &#34;dấu ngoặc&#34; |\n"
	got := AppendWithOverlap(acc, next, 0)
	want := acc + next
	if got != want {
		t.Fatalf("contiguous merge mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestAppendWithOverlap_PrependedTableHeaderSkipped(t *testing.T) {
	// Mô phỏng chunker bổ sung tiêu đề cho bảng bị tách: đầu next có thêm một tiêu đề (độ rộng 0, không thấy theo vị trí),
	// các dòng chồng lấn thật nằm sau tiêu đề. Cắt theo vị trí sẽ lệch; khớp theo văn bản phải khử trùng lặp đúng.
	header := "| Cột 1 | Cột 2 | Cột 3 |\n|:---|:---|:---|\n"
	overlapRows := "| Dòng 5 | Nội dung 5A | Nội dung 5B |\n| Dòng 6 | Nội dung 6A | Nội dung 6B |\n"
	accTail := "| Dòng 4 | Nội dung 4A | Nội dung 4B |\n" + overlapRows
	acc := header + "| Dòng 1 | x | — |\n" + accTail

	newRows := "| Dòng 7 | Nội dung 7A | Nội dung 7B |\n| Dòng 8 | Nội dung 8A | Nội dung 8B |\n"
	next := header + overlapRows + newRows

	// Lượng chồng lấn theo vị trí xấp xỉ độ dài hai dòng (giá trị gần đúng là đủ, chỉ dùng để ước lượng cửa sổ).
	got := AppendWithOverlap(acc, next, len([]rune(overlapRows)))
	want := acc + newRows
	if got != want {
		t.Fatalf("prepended-header merge mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestAppendWithOverlap_PlainOverlap(t *testing.T) {
	acc := "abcdefghijklmnopqrstuvwxyz0123"
	// next chồng lấn với đuôi acc "klmnopqrstuvwxyz0123", rồi nối nội dung mới
	next := "klmnopqrstuvwxyz0123ABCDEFG"
	got := AppendWithOverlap(acc, next, 20)
	want := "abcdefghijklmnopqrstuvwxyz0123ABCDEFG"
	if got != want {
		t.Fatalf("plain overlap mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestAppendWithOverlap_NoOverlap(t *testing.T) {
	acc := "hello world"
	next := "completely different"
	got := AppendWithOverlap(acc, next, 0)
	want := acc + next
	if got != want {
		t.Fatalf("no overlap mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestAppendWithExactOverlap_TrimsKnownOverlap(t *testing.T) {
	overlap := "shared boundary text"
	got, ok := AppendWithExactOverlap("before "+overlap, overlap+" after", len([]rune(overlap)))
	if !ok {
		t.Fatal("exact overlap should be accepted")
	}
	want := "before " + overlap + " after"
	if got != want {
		t.Fatalf("exact overlap mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestAppendWithExactOverlap_ZeroOverlapConcatenatesRepeatedText(t *testing.T) {
	// Hai đoạn nối tiếp nhau đều gồm cùng một văn bản lặp tuần hoàn (dòng bảng / dòng log). Khi lượng chồng lấn là 0
	// phải nối nguyên văn, không được tìm khớp hậu tố, nếu không sẽ nuốt mất các dòng lặp ở đầu next.
	row := "| cell | cell |\n"
	acc := "Lời nói đầu\n" + row + row
	next := row + row + row + "Kết thúc\n"
	got, ok := AppendWithExactOverlap(acc, next, 0)
	if !ok {
		t.Fatal("zero overlap should be accepted")
	}
	if got != acc+next {
		t.Fatalf("zero overlap must concatenate verbatim:\n got=%q\nwant=%q", got, acc+next)
	}
}

func TestAppendWithExactOverlap_RejectsMismatchedOverlap(t *testing.T) {
	// Bất biến độ dài đúng nhưng văn bản đã khác (thực thể HTML, tiêu đề bổ sung...), phải từ chối,
	// để bên gọi quay về khớp theo văn bản.
	if _, ok := AppendWithExactOverlap("abcdefghijkl", "XYZdefghijkl", 6); ok {
		t.Fatal("mismatched overlap should be rejected")
	}
}

func TestAppendWithExactOverlap_RejectsOverlapLongerThanBodies(t *testing.T) {
	if _, ok := AppendWithExactOverlap("short", "shorter", 99); ok {
		t.Fatal("overlap exceeding body length should be rejected")
	}
	if _, ok := AppendWithExactOverlap("short", "shorter", -1); ok {
		t.Fatal("negative overlap should be rejected")
	}
}

func TestJoinChunkContentUsesCurrentTextInsteadOfSourceOffsets(t *testing.T) {
	first := "first edited body with no original overlap"
	second := "second independently edited body"
	got := JoinChunkContent(first, second, "\n\n")
	want := first + "\n\n" + second
	if got != want {
		t.Fatalf("edited join mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestJoinChunkContentRemovesRealBoundaryOverlap(t *testing.T) {
	overlap := "shared boundary text"
	got := JoinChunkContent("before "+overlap, overlap+" after", "\n\n")
	want := "before " + overlap + " after"
	if got != want {
		t.Fatalf("overlap join mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestJoinChunkContentCollapsesExactContainment(t *testing.T) {
	outer := "prefix complete current body suffix"
	if got := JoinChunkContent(outer, "complete current body", "\n\n"); got != outer {
		t.Fatalf("contained body should be collapsed: %q", got)
	}
}

func TestMergeTextChunks_OrdersFiltersAndStitches(t *testing.T) {
	header := "| a | b |\n|:--|:--|\n"
	chunks := []*types.Chunk{
		{
			Content: header + "| r1 | x |\n| r2 | y |\n", ChunkType: types.ChunkTypeText,
			StartAt: 0, EndAt: 20, ChunkIndex: 0,
		},
		{
			// tiêu đề bổ sung + chồng lấn r2 với đoạn trước + dòng mới r3
			Content: header + "| r2 | y |\n| r3 | z |\n", ChunkType: types.ChunkTypeText,
			StartAt: 10, EndAt: 40, ChunkIndex: 1,
		},
	}
	got := MergeTextChunks(chunks, "\n")
	want := header + "| r1 | x |\n| r2 | y |\n" + "| r3 | z |\n"
	if got != want {
		t.Fatalf("MergeTextChunks mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestMergeTextChunks_GapSeparator(t *testing.T) {
	chunks := []*types.Chunk{
		{Content: "first", ChunkType: types.ChunkTypeText, StartAt: 0, EndAt: 5, ChunkIndex: 0},
		{Content: "second", ChunkType: types.ChunkTypeText, StartAt: 100, EndAt: 106, ChunkIndex: 1},
	}
	got := MergeTextChunks(chunks, "\n")
	want := "first\nsecond"
	if got != want {
		t.Fatalf("gap separator mismatch:\n got=%q\nwant=%q", got, want)
	}
}

// TestAppendWithOverlap_ContiguousRealContentRepeat is a regression test:
// two chunks are strictly contiguous (positionOverlap==0), but a boilerplate
// sentence at the tail of acc reappears inside the head window of next as
// real content (the same sentence is written multiple times in the document).
// The old algorithm would enter text matching and, due to the headSlack
// floor of 320, mistake the head of next for a prepended table header and
// delete it, causing irreversible content loss. After the fix it should
// concatenate directly without trimming.
func TestAppendWithOverlap_ContiguousRealContentRepeat(t *testing.T) {
	repeat := "The system shall maintain a complete audit trail of all transactions."
	acc := "3.2 Logging Requirements\n\n" + repeat
	next := "\n\n5.1 Security Controls\n\n* Role-based access\n* Encryption at rest\n\n5.2 Compliance\n\n" + repeat + " This satisfies SOC 2."
	got := AppendWithOverlap(acc, next, 0)
	want := acc + next
	if got != want {
		t.Fatalf("contiguous real-content repeat must not trim:\n got.len=%d\nwant.len=%d\n got.tail=%q\nwant.tail=%q",
			len(got), len(want), got[len(acc)-50:], want[len(acc)-50:])
	}
}
