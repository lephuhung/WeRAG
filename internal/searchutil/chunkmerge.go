package searchutil

import (
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// JoinChunkContent joins two current chunk bodies without relying on parser
// offsets. Exact containment is collapsed, a real suffix/prefix overlap is
// removed, and otherwise both bodies are retained with separator between
// them. The conservative fallback intentionally prefers small duplication
// over silently dropping edited content.
func JoinChunkContent(acc, next, separator string) string {
	if acc == "" {
		return next
	}
	if next == "" {
		return acc
	}
	if ContainsChunkContent(acc, next) {
		return acc
	}
	if ContainsChunkContent(next, acc) {
		return next
	}

	accRunes := []rune(acc)
	nextRunes := []rune(next)
	maxOverlap := minInt(len(accRunes), len(nextRunes))
	// Editable chunks may be much larger than parser-produced chunks. Bound
	// suffix matching so an adversarial 200 KB edit cannot turn retrieval into
	// quadratic work. Parser overlap windows are normally far below this cap;
	// larger unmatched overlap is safely retained as duplication.
	if maxOverlap > defaultSearchSpan {
		maxOverlap = defaultSearchSpan
	}
	for overlap := maxOverlap; overlap >= minOverlapRunes; overlap-- {
		if runeSlicesEqual(accRunes[len(accRunes)-overlap:], nextRunes[:overlap]) {
			return acc + string(nextRunes[overlap:])
		}
	}
	return acc + separator + next
}

// ContainsChunkContent reports whether the complete current body is safely
// represented by another body. Very short substrings are not treated as
// containment because common words and punctuation would create false drops.
func ContainsChunkContent(container, contained string) bool {
	if container == "" || contained == "" {
		return false
	}
	if container == contained {
		return true
	}
	return len([]rune(contained)) >= minOverlapRunes && strings.Contains(container, contained)
}

func runeSlicesEqual(left, right []rune) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// Phần này cài đặt logic chung "ghép nối có chồng lấn" nội dung chunk, dùng chung cho tái dựng tài liệu (reconstructContent),
// gộp nội dung đồ thị tri thức (graph mergeChunkContents), ... Luồng truy xuất trong chat cho phép
// người dùng sửa chunk nên dùng JoinChunkContent ở trên, tránh phụ thuộc toạ độ vị trí trong văn bản gốc.
//
// Trước đây mọi nơi đều cắt chồng lấn bằng công thức "theo vị trí" (offset = len(content) - (EndAt -
// lastEndAt) hoặc tương tự), ngầm định len([]rune(Content)) == EndAt-StartAt. Nhưng có hai loại
// dữ liệu phá vỡ bất biến này, gây ghép lệch, mất chữ hoặc lặp:
//  1. Bộ chia cha-con "bổ sung tiêu đề bảng" cho bảng bị tách, tiêu đề bổ sung có độ rộng bằng 0
//     (start == end), toạ độ vị trí không biểu diễn được, content dài hơn EndAt-StartAt;
//  2. content có thể còn HTML entity (như &#34; / &gt;), số ký tự dài hơn khoảng trong văn bản gốc.
//
// Vì vậy ở đây đổi sang khớp chồng lấn "theo văn bản": tìm trong cửa sổ đầu đoạn kế tiếp vị trí xuất hiện đầu tiên
// của hậu tố văn bản đã gộp, rồi nối từ sau vị trí đó. Thông tin vị trí (StartAt/EndAt) chỉ dùng để ước lượng
// kích thước cửa sổ tìm kiếm, không còn dùng để cắt.

const (
	// minOverlapRunes là độ dài hậu tố ngắn nhất tham gia khớp. Quá ngắn (như dòng phân cách bảng |---|)
	// dễ khớp nhầm nên bỏ qua.
	minOverlapRunes = 12
	// defaultSearchSpan là cận dưới của cửa sổ tìm kiếm, đảm bảo kể cả khi thiếu thông tin vị trí / bằng 0
	// vẫn phát hiện được chồng lấn thật trong một phạm vi nhất định.
	defaultSearchSpan = 400
)

// AppendWithOverlap nối next vào sau acc và bỏ phần chồng lấn giữa hai bên.
//
// positionOverlap là lượng chồng lấn ước lượng từ StartAt/EndAt (lastEnd - curStart), chỉ dùng để
// giới hạn kích thước cửa sổ tìm kiếm; chồng lấn thật khớp theo văn bản, chịu được tiêu đề bảng bổ sung và lệch độ dài do HTML entity.
// Không tìm thấy chồng lấn văn bản thì nối nguyên vẹn (không cắt), thà giữ thừa còn hơn làm hỏng nội dung.
//
// Khi positionOverlap <= 0, hai đoạn kề nhau hoặc rời nhau về vị trí, không có chồng lấn để loại;
// lúc này nếu vẫn khớp văn bản thì cận dưới headSlack 320 có thể khớp nhầm trong cửa sổ đầu next
// với nội dung thật lặp lại của hậu tố acc (như cùng một câu xuất hiện nhiều lần trong tài liệu), coi nhầm
// cả đoạn đầu next là tiêu đề bảng bổ sung rồi xoá, gây mất nội dung không khôi phục được. Nối thẳng, phần
// tiêu đề bảng lặp giao cho bên gọi xử lý sau.
func AppendWithOverlap(acc, next string, positionOverlap int) string {
	if acc == "" {
		return next
	}
	if next == "" {
		return acc
	}
	if positionOverlap <= 0 {
		return acc + next
	}

	accRunes := []rune(acc)
	nextRunes := []rune(next)

	span := positionOverlap

	maxK := minInt(len(accRunes), len(nextRunes))
	if cap := maxInt(span*3, defaultSearchSpan); maxK > cap {
		maxK = cap
	}
	// Số tiền tố tối đa được phép bỏ qua trước phần chồng lấn (tức văn bản tổng hợp như tiêu đề bảng bổ sung).
	headSlack := maxInt(span*2, 320)

	for k := maxK; k >= minOverlapRunes; k-- {
		needle := accRunes[len(accRunes)-k:]
		if pos := indexRunes(nextRunes, needle, headSlack); pos >= 0 {
			return acc + string(nextRunes[pos+k:])
		}
	}
	return acc + next
}

// AppendWithExactOverlap dùng khi bên gọi đã xác nhận toạ độ vị trí đáng tin: nối acc và next theo lượng chồng lấn
// chính xác do toạ độ cho: kiểm tra overlap ký tự cuối của acc và overlap ký tự đầu của next bằng nhau
// từng ký tự, bằng nhau thì cắt chính xác, overlap bằng 0 thì nối thẳng.
//
// Khác với AppendWithOverlap ở chỗ "không đoán": hàm kia để chịu được lệch độ dài do tiêu đề bảng bổ sung, HTML entity
// sẽ tìm hậu tố khớp dài nhất trong cửa sổ, văn bản lặp tuần hoàn (bảng, log) có thể bị coi nhầm là
// chồng lấn và cắt mất nội dung thật. Khi toạ độ đáng tin thì lượng chồng lấn đã biết, không cần tìm.
//
// Kiểm tra không đạt thì trả ok=false, bên gọi quyết định có lui về AppendWithOverlap không.
func AppendWithExactOverlap(acc, next string, overlap int) (string, bool) {
	if acc == "" {
		return next, true
	}
	if next == "" {
		return acc, true
	}
	if overlap < 0 {
		return "", false
	}
	if overlap == 0 {
		return acc + next, true
	}

	accRunes := []rune(acc)
	nextRunes := []rune(next)
	if overlap > len(accRunes) || overlap > len(nextRunes) {
		return "", false
	}
	if !runeSlicesEqual(accRunes[len(accRunes)-overlap:], nextRunes[:overlap]) {
		return "", false
	}
	return acc + string(nextRunes[overlap:]), true
}

// MergeTextChunks sắp xếp theo StartAt (bằng nhau thì theo ChunkIndex), rồi dùng AppendWithOverlap
// tái dựng nội dung nhiều chunk thành văn bản đầy đủ. gapSep là dấu phân cách giữa hai đoạn không kề nhau
// về vị trí (có khoảng trống), ví dụ "\n"; truyền chuỗi rỗng thì nối thẳng.
//
// Bên gọi chịu trách nhiệm lọc loại trước (ví dụ chỉ giữ chunk văn bản); hàm này không biết ChunkType.
func MergeTextChunks(chunks []*types.Chunk, gapSep string) string {
	if len(chunks) == 0 {
		return ""
	}

	sorted := make([]*types.Chunk, len(chunks))
	copy(sorted, chunks)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].StartAt == sorted[j].StartAt {
			return sorted[i].ChunkIndex < sorted[j].ChunkIndex
		}
		return sorted[i].StartAt < sorted[j].StartAt
	})

	merged := ""
	mergedEnd := -1
	for _, c := range sorted {
		if c == nil || c.Content == "" {
			continue
		}
		if merged == "" {
			merged = c.Content
			if c.EndAt > 0 {
				mergedEnd = c.EndAt
			}
			continue
		}

		// Có khoảng trống / thiếu thông tin vị trí (EndAt==0): nối như đoạn độc lập, không cắt chồng lấn.
		if c.StartAt > mergedEnd || c.EndAt == 0 {
			if gapSep != "" {
				merged += gapSep
			}
			merged += c.Content
			if c.EndAt > 0 {
				mergedEnd = c.EndAt
			}
			continue
		}

		// Chồng lấn một phần hoặc nối đuôi: khớp văn bản để bỏ chồng lấn rồi nối.
		if c.EndAt > mergedEnd {
			merged = AppendWithOverlap(merged, c.Content, mergedEnd-c.StartAt)
			mergedEnd = c.EndAt
		}
		// Ngược lại bị đoạn trước bao trùm hoàn toàn, bỏ qua.
	}

	return merged
}

// indexRunes tìm chỉ số rune của lần xuất hiện đầu tiên của needle trong haystack, với vị trí bắt đầu không quá
// maxStart. Không tìm thấy trả về -1.
func indexRunes(haystack, needle []rune, maxStart int) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	limit := len(haystack) - len(needle)
	if maxStart < limit {
		limit = maxStart
	}
	for i := 0; i <= limit; i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
