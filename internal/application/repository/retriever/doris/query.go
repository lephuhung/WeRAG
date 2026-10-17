package doris

import (
	"math"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// Hằng tên cột. Doris là kho SQL, tên cột được dùng lại ở nhiều chỗ SELECT/WHERE/INSERT,
// dùng hằng để thống nhất và tránh gõ sai.
const (
	fieldID              = "id"
	fieldContent         = "content"
	fieldSourceID        = "source_id"
	fieldSourceType      = "source_type"
	fieldChunkID         = "chunk_id"
	fieldKnowledgeID     = "knowledge_id"
	fieldKnowledgeBaseID = "knowledge_base_id"
	fieldTagID           = "tag_id"
	fieldIsEnabled       = "is_enabled"
	fieldEmbedding       = "embedding"
)

// columns là thứ tự cột chuẩn dùng khi INSERT / SELECT.
var columns = []string{
	fieldID, fieldContent, fieldSourceID, fieldSourceType,
	fieldChunkID, fieldKnowledgeID, fieldKnowledgeBaseID, fieldTagID,
	fieldIsEnabled, fieldEmbedding,
}

// columnsForRetrieve là thứ tự cột SELECT khi Retrieve,
// không gồm embedding (kết quả truy vấn không cần trả về vector, tiết kiệm băng thông).
var columnsForRetrieve = []string{
	fieldID, fieldContent, fieldSourceID, fieldSourceType,
	fieldChunkID, fieldKnowledgeID, fieldKnowledgeBaseID, fieldTagID,
	fieldIsEnabled,
}

// columnsForCopy là thứ tự cột dùng khi SELECT phân trang trong CopyIndices,
// nhiều hơn columnsForRetrieve cột embedding vì mục đích sao chép là chuyển chính vector.
var columnsForCopy = []string{
	fieldID, fieldContent, fieldSourceID, fieldSourceType,
	fieldChunkID, fieldKnowledgeID, fieldKnowledgeBaseID, fieldTagID,
	fieldIsEnabled, fieldEmbedding,
}

// whereCond biểu diễn một điều kiện con WHERE: clause là đoạn SQL tham số hóa (có chỗ giữ ?),
// args là các giá trị tham số theo đúng thứ tự. Mọi trường do người dùng nhập (IDs) phải truyền qua args,
// tuyệt đối không nối vào chuỗi clause.
type whereCond struct {
	clause string
	args   []any
}

// whereBuilder dịch các điều kiện lọc trong RetrieveParams thành mệnh đề SQL WHERE.
//
// Mỗi phương thức add* ứng với một toán tử IN / NOT IN / =; cuối cùng build() nối bằng AND.
type whereBuilder struct {
	conds []whereCond
}

// addEqual thêm một điều kiện field = ?.
func (w *whereBuilder) addEqual(field string, value any) {
	w.conds = append(w.conds, whereCond{
		clause: field + " = ?",
		args:   []any{value},
	})
}

// addIn thêm một điều kiện field IN (?, ?, ...). values rỗng thì không thêm gì.
func (w *whereBuilder) addIn(field string, values []string) {
	if len(values) == 0 {
		return
	}
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for i, v := range values {
		placeholders[i] = "?"
		args[i] = v
	}
	w.conds = append(w.conds, whereCond{
		clause: field + " IN (" + strings.Join(placeholders, ", ") + ")",
		args:   args,
	})
}

// addNotIn thêm một điều kiện field NOT IN (?, ?, ...).
func (w *whereBuilder) addNotIn(field string, values []string) {
	if len(values) == 0 {
		return
	}
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for i, v := range values {
		placeholders[i] = "?"
		args[i] = v
	}
	w.conds = append(w.conds, whereCond{
		clause: field + " NOT IN (" + strings.Join(placeholders, ", ") + ")",
		args:   args,
	})
}

// build trả về mệnh đề WHERE (không có tiền tố "WHERE ") và mảng tham số.
// Không có điều kiện nào thì trả về ("1 = 1", nil) để bên gọi nối thẳng.
func (w *whereBuilder) build() (string, []any) {
	if len(w.conds) == 0 {
		return "1 = 1", nil
	}
	parts := make([]string, len(w.conds))
	var args []any
	for i, c := range w.conds {
		parts[i] = c.clause
		args = append(args, c.args...)
	}
	return strings.Join(parts, " AND "), args
}

// buildBaseFilter dịch các điều kiện lọc trong RetrieveParams thành whereBuilder.
// Mặc định thêm is_enabled = TRUE, thống nhất với Qdrant/Milvus/Weaviate:
// chunk bị tắt không tham gia truy xuất.
func buildBaseFilter(params types.RetrieveParams) *whereBuilder {
	w := &whereBuilder{}
	w.addEqual(fieldIsEnabled, true)

	if len(params.KnowledgeBaseIDs) > 0 {
		w.addIn(fieldKnowledgeBaseID, params.KnowledgeBaseIDs)
	}
	if len(params.KnowledgeIDs) > 0 {
		w.addIn(fieldKnowledgeID, params.KnowledgeIDs)
	}
	if len(params.TagIDs) > 0 {
		w.addIn(fieldTagID, params.TagIDs)
	}
	if len(params.ExcludeKnowledgeIDs) > 0 {
		w.addNotIn(fieldKnowledgeID, params.ExcludeKnowledgeIDs)
	}
	if len(params.ExcludeChunkIDs) > 0 {
		w.addNotIn(fieldChunkID, params.ExcludeChunkIDs)
	}
	return w
}

// parseEmbeddingLiteral phân tích chuỗi literal mà Doris ARRAY<FLOAT> trả về
// qua giao thức MySQL (dạng "[1,2,3]") thành []float32.
//
// Đường CopyIndices cần đọc vector từ hàng nguồn rồi ghi lại hàng đích; phân tích ở đây ưu tiên khoan dung:
// không có [] vẫn chấp nhận, mảng rỗng trả về nil.
func parseEmbeddingLiteral(raw []byte) ([]float32, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil, nil
	}
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f, err := strconv.ParseFloat(p, 32)
		if err != nil {
			return nil, err
		}
		out = append(out, float32(f))
	}
	return out, nil
}

// validateEmbedding kiểm tra mọi phần tử vector đều hữu hạn.
//
// strconv.FormatFloat với NaN/±Inf sẽ xuất "NaN"/"+Inf"/"-Inf",
// các literal này làm SQL nối cho Doris bị lỗi cú pháp (hoặc cho kết quả không xác định ở một số phiên bản).
// Thượng nguồn (mô hình embedding) bình thường không xuất giá trị vô hạn, nhưng GPU OOM, bug thượng nguồn,
// stub kiểm thử đều có thể gây ra; fail-fast ở đây an toàn hơn âm thầm ghi dữ liệu bẩn.
func validateEmbedding(vec []float32) error {
	for i, v := range vec {
		if f := float64(v); math.IsNaN(f) || math.IsInf(f, 0) {
			return errInvalidEmbedding{index: i, value: v}
		}
	}
	return nil
}

// normalizeEmbedding returns a unit-length copy of vec so Doris inner-product
// ANN search can preserve cosine-style similarity semantics.
func normalizeEmbedding(vec []float32) []float32 {
	if len(vec) == 0 {
		return nil
	}
	var sumSquares float64
	for _, value := range vec {
		f := float64(value)
		sumSquares += f * f
	}
	if sumSquares == 0 {
		return append([]float32(nil), vec...)
	}
	norm := float32(math.Sqrt(sumSquares))
	normalized := make([]float32, len(vec))
	for i, value := range vec {
		normalized[i] = value / norm
	}
	return normalized
}

// errInvalidEmbedding mô tả chỉ số nào chứa giá trị vô hạn; dùng struct thay vì fmt.Errorf
// để tầng trên lấy được chỉ số trong log mà định vị lỗi.
type errInvalidEmbedding struct {
	index int
	value float32
}

func (e errInvalidEmbedding) Error() string {
	return "doris: embedding[" + strconv.Itoa(e.index) +
		"] is not finite: " + strconv.FormatFloat(float64(e.value), 'g', -1, 32)
}

// embeddingLiteral chuyển []float32 thành chuỗi literal Doris ARRAY<FLOAT>:
// "[1.23,4.56,...]".
//
// Vì sao không dùng chỗ giữ: go-sql-driver/mysql không hỗ trợ bind tham số kiểu ARRAY,
// phía Doris cũng chỉ nhận dạng literal. Ở đây dùng strconv.FormatFloat ('g' + bitSize=32)
// thay vì fmt.Sprintf("%f", v), vì hai lý do:
//  1. fmt ở một số locale dùng dấu phân cách hàng nghìn, phá cú pháp SQL;
//  2. 'g' ngắn hơn 'f' và không mất độ chính xác.
//
// Rủi ro injection: phần tử []float32 là số thực hữu hạn do mô hình embedding xuất, sau tuần tự hóa chỉ có thể
// chứa ký tự [0-9eE+-.\s], không thoát khỏi ngữ cảnh literal.
func embeddingLiteral(vec []float32) string {
	if len(vec) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.Grow(len(vec) * 12)
	sb.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}
