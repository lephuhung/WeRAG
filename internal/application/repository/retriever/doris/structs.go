package doris

import (
	"database/sql"
	"net/http"
	"sync"
)

// dorisRepository là hiện thực kho truy xuất cho Apache Doris 4.1.
//
// Kênh giao tiếp:
//   - Đường đọc/ghi chính: giao thức MySQL (database/sql + go-sql-driver/mysql), FE mặc định cổng 9030.
//   - Stream Load: HTTP (FE mặc định cổng 8030), chế độ legacy dùng cho partial update.
//
// Bảng được tách theo số chiều: <tableBaseName>_<dim>.
// Chế độ tương thích do DORIS_COMPAT_MODE quyết định:
//   - legacy：UNIQUE KEY(id) + cosine_distance ANN + Stream Load partial update
//   - inner_product_duplicate：DUPLICATE KEY(id) + normalized inner product + delete/insert rewrite
//
// Thiết lập này không đổi qua lại được sau khi bảng embedding đã tạo; đổi chế độ cần tạo lại các bảng.
//
// Như Qdrant/Milvus/Weaviate, initializedTables cache các số chiều "đã đảm bảo tồn tại",
// tránh chạy SHOW TABLES mỗi lần ghi.
type dorisRepository struct {
	db *sql.DB

	httpClient *http.Client
	// fe HTTP base, ví dụ "http://doris-fe:8030". Đường dẫn Stream Load
	// do streamLoadURL(table) nối: <feHTTPBase>/api/<database>/<table>/_stream_load.
	feHTTPBase string

	username string
	password string
	database string

	tableBaseName       string
	bucketsNum          int // 0 -> default 10
	replicationNum      int // 0 -> default 1
	compatModeRequested dorisCompatMode
	compatModeResolved  dorisCompatMode
	compatResolveOnce   sync.Once
	compatResolveErr    error

	// Tập số chiều đã qua ensureTable: dim -> true.
	initializedTables sync.Map
}

// DorisVectorEmbedding là mô hình miền của một hàng ghi vào bảng Doris.
//
// Thứ tự trường thống nhất với thứ tự cột INSERT trong schema.go,
// khi điều chỉnh cần cập nhật đồng thời createInsert và columns.
type DorisVectorEmbedding struct {
	ID              string
	Content         string
	SourceID        string
	SourceType      int
	ChunkID         string
	KnowledgeID     string
	KnowledgeBaseID string
	TagID           string
	IsEnabled       bool
	Embedding       []float32
}

// DorisVectorEmbeddingWithScore là mô hình miền của kết quả truy xuất,
// Score tính theo compat mode hiện tại khi truy xuất vector, gán đồng loạt 1.0 khi truy xuất từ khóa.
type DorisVectorEmbeddingWithScore struct {
	DorisVectorEmbedding
	Score float64
}
