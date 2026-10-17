package doris

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

// Số bucket / số bản sao mặc định. Doris dùng mặc định của cụm khi PROPERTIES không chỉ định,
// ở đây đặt giá trị bảo thủ thân thiện hơn với máy đơn / cụm nhỏ.
const (
	defaultBucketsNum     = 10
	defaultReplicationNum = 1

	// Thời gian chờ tối đa khi thăm dò chỉ mục ANN sẵn sàng. Chỉ mục chưa sẵn sàng không chặn đường ghi,
	// chỉ chặn chính ensureTable (lần tạo bảng đầu), nên 30s là chấp nhận được.
	annReadyTimeout = 30 * time.Second
	annReadyPoll    = 1 * time.Second
)

// getTableName trả về tên bảng vật lý ứng với một số chiều: <base>_<dim>.
//
// Thống nhất với quy ước đặt tên collection của Qdrant/Milvus/Weaviate,
// nhờ đó dữ liệu của các mô hình embedding khác nhau (khác số chiều) không xung đột.
func (r *dorisRepository) getTableName(dimension int) string {
	return fmt.Sprintf("%s_%d", r.tableBaseName, dimension)
}

// ensureTable đảm bảo bảng ứng với số chiều đích đã tồn tại;
// chưa có thì tạo bằng CREATE TABLE IF NOT EXISTS, rồi thăm dò chỉ mục ANN sẵn sàng sau khi tạo.
//
// Phương thức này được gọi trước mỗi Save / BatchSave, kết quả cache trong initializedTables,
// trong cùng tiến trình mỗi dimension chỉ thực sự chạy SHOW TABLES + DDL một lần.
func (r *dorisRepository) ensureTable(ctx context.Context, dimension int) error {
	if _, ok := r.initializedTables.Load(dimension); ok {
		return nil
	}
	compatMode, err := r.resolveCompatMode(ctx)
	if err != nil {
		return err
	}

	log := logger.GetLogger(ctx)
	tableName := r.getTableName(dimension)

	exists, err := r.tableExists(ctx, tableName)
	if err != nil {
		log.Errorf("[Doris] Failed to check table existence: %v", err)
		return fmt.Errorf("check table existence: %w", err)
	}

	if !exists {
		log.Infof("[Doris] Creating table %s with dimension %d in compat mode %s", tableName, dimension, compatMode)
		if err := r.createTable(ctx, tableName, dimension, compatMode); err != nil {
			log.Errorf("[Doris] Failed to create table: %v", err)
			return fmt.Errorf("create table: %w", err)
		}

		// Chỉ mục ANN được Doris xây bất đồng bộ. Ở đây thăm dò sẵn sàng trong goroutine nền,
		// đường ghi không bị chặn; khi chỉ mục chưa sẵn sàng, truy xuất lùi về brute-force (kết quả đúng, chậm),
		// chấp nhận được hơn là để lô ghi đầu kẹt 30s.
		go func(tn string) {
			// Dùng context riêng (có timeout) để ctx cấp yêu cầu bị hủy không kéo theo thăm dò nền.
			bgCtx, cancel := context.WithTimeout(context.Background(), annReadyTimeout)
			defer cancel()
			if err := r.waitANNReady(bgCtx, tn); err != nil {
				logger.GetLogger(bgCtx).Warnf(
					"[Doris] ANN index for %s not ready within %s: %v "+
						"(queries may fall back to brute force temporarily)",
					tn, annReadyTimeout, err)
				return
			}
			logger.GetLogger(bgCtx).Infof("[Doris] ANN index for %s ready", tn)
		}(tableName)
	}

	r.initializedTables.Store(dimension, true)
	return nil
}

// tableExists kiểm tra bảng tồn tại qua information_schema.
//
// Không dùng thẳng SHOW TABLES vì Doris 4.1 phân biệt hoa thường với SHOW TABLES LIKE,
// còn information_schema tương thích MySQL tốt hơn.
func (r *dorisRepository) tableExists(ctx context.Context, tableName string) (bool, error) {
	const q = `SELECT COUNT(1) FROM information_schema.tables
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`
	var n int
	if err := r.db.QueryRowContext(ctx, q, r.database, tableName).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// createTable phát DDL CREATE TABLE. DDL của Doris là đồng bộ (trừ việc xây chỉ mục ANN),
// trả về thành công nghĩa là bảng đã ghi được.
func (r *dorisRepository) createTable(ctx context.Context, tableName string, dimension int, compatMode dorisCompatMode) error {
	buckets := r.bucketsNum
	if buckets <= 0 {
		buckets = defaultBucketsNum
	}
	replication := r.replicationNum
	if replication <= 0 {
		replication = defaultReplicationNum
	}

	ddl := buildCreateTableDDL(tableName, dimension, buckets, replication, compatMode)
	_, err := r.db.ExecContext(ctx, ddl)
	if err != nil && compatMode == dorisCompatModeLegacy {
		return fmt.Errorf(
			"legacy Doris table creation failed: %w. If your Doris build rejects ANN indexes on UNIQUE KEY tables, set %s=%s before creating embedding tables. %s is not interchangeable after %s_* tables are created",
			err,
			envDorisCompatMode,
			dorisCompatModeInnerProductDuplicate,
			envDorisCompatMode,
			r.tableBaseName,
		)
	}
	return err
}

// buildCreateTableDDL sinh DDL CREATE TABLE theo số chiều.
//
// Điểm chính:
//   - DUPLICATE KEY(id): tương thích yêu cầu mô hình bảng cho chỉ mục ANN của Doris/SelectDB hiện tại.
//     WeKnora dùng delete + insert phía Go để giữ ngữ nghĩa ghi thay thế theo id.
//   - Chỉ mục INVERTED phủ mọi trường lọc + chỉ mục toàn văn content với bộ tách từ tiếng Trung.
//   - Chỉ mục ANN dùng HNSW + inner_product; vector được chuẩn hóa đơn vị trước khi ghi/truy vấn Doris,
//     nên tổng thể vẫn giữ ngữ nghĩa cosine similarity như các kho vector khác.
//
// Lưu ý: ba trường số dimension / buckets / replication trong DDL được định dạng và nối phía Go,
// không có rủi ro SQL injection (nguồn đều là IndexConfig int được kiểm soát).
func buildCreateTableDDL(tableName string, dimension, buckets, replication int, compatMode dorisCompatMode) string {
	metricType := "inner_product"
	keyMode := "DUPLICATE KEY(id)"
	properties := fmt.Sprintf("\t\"replication_num\"=\"%d\"", replication)
	if compatMode == dorisCompatModeLegacy {
		metricType = "cosine_distance"
		keyMode = "UNIQUE KEY(id)"
		properties = fmt.Sprintf("\t\"replication_num\"=\"%d\",\n\t\"enable_unique_key_merge_on_write\"=\"true\"", replication)
	}

	const tpl = `CREATE TABLE IF NOT EXISTS ` + "`%s`" + ` (
    id                VARCHAR(64)  NOT NULL,
    chunk_id          VARCHAR(64),
    knowledge_id      VARCHAR(64),
    knowledge_base_id VARCHAR(64),
    source_id         VARCHAR(255),
    source_type       INT,
    tag_id            VARCHAR(64),
    is_enabled        BOOLEAN,
    content           TEXT,
    embedding         ARRAY<FLOAT> NOT NULL,
    INDEX idx_chunk    (chunk_id)          USING INVERTED,
    INDEX idx_kb       (knowledge_base_id) USING INVERTED,
    INDEX idx_kid      (knowledge_id)      USING INVERTED,
    INDEX idx_src      (source_id)         USING INVERTED,
    INDEX idx_tag      (tag_id)            USING INVERTED,
    INDEX idx_enabled  (is_enabled)        USING INVERTED,
    INDEX idx_content  (content)           USING INVERTED PROPERTIES("parser"="chinese","support_phrase"="true"),
    INDEX idx_emb      (embedding)         USING ANN PROPERTIES(
        "index_type"="hnsw",
		"metric_type"="%s",
        "dim"="%d",
        "max_degree"="32",
        "ef_construction"="200"
    )
) ENGINE=OLAP
%s
DISTRIBUTED BY HASH(id) BUCKETS %d
PROPERTIES(
	%s
);`
	return fmt.Sprintf(tpl, tableName, metricType, dimension, keyMode, buckets, properties)
}

// waitANNReady thăm dò SHOW INDEX, chờ chỉ mục ANN vào trạng thái FINISHED.
//
// Chỉ mục ANN của Doris được xây bất đồng bộ sau khi tạo bảng; trong lúc đó truy vấn lùi về brute-force (kết quả đúng, chậm).
// Ở đây chỉ chờ kiểu "cố gắng hết sức": hết giờ chưa sẵn sàng chỉ ghi warning, không chặn ghi.
func (r *dorisRepository) waitANNReady(ctx context.Context, tableName string) error {
	deadline := time.Now().Add(annReadyTimeout)
	for {
		ready, err := r.annIndexReady(ctx, tableName)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ann index not ready within %s", annReadyTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(annReadyPoll):
		}
	}
}

// annIndexReady kiểm tra State của chỉ mục ANN có phải FINISHED không.
//
// SHOW INDEX FROM <table> trên Doris trả về nhiều cột; thứ tự cột hơi khác giữa các phiên bản nhỏ,
// nên ở đây khớp theo tên cột (information_schema.statistics + view tùy chỉnh không khả thi,
// dùng thẳng SHOW INDEX rồi quét là đủ).
//
// Chiến lược tương thích: nếu SHOW INDEX không có hàng idx_emb (phiên bản rất cũ) thì coi là đã sẵn sàng,
// tránh kẹt khởi động do khác biệt đầu ra giữa các phiên bản Doris.
func (r *dorisRepository) annIndexReady(ctx context.Context, tableName string) (bool, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf("SHOW INDEX FROM `%s`", tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return false, err
	}
	keyNameIdx, stateIdx := -1, -1
	for i, c := range cols {
		switch strings.ToLower(c) {
		case "key_name":
			keyNameIdx = i
		case "state", "index_state":
			stateIdx = i
		}
	}

	for rows.Next() {
		// Nhận bằng sql.RawBytes để tương thích các kiểu cột khác nhau.
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return false, err
		}

		var keyName, state string
		if keyNameIdx >= 0 {
			keyName = bytesToString(raw[keyNameIdx])
		}
		if stateIdx >= 0 {
			state = bytesToString(raw[stateIdx])
		}

		if keyName != "idx_emb" {
			continue
		}
		if stateIdx < 0 {
			// Phiên bản cũ không lộ cột state, lạc quan coi là đã sẵn sàng.
			return true, nil
		}
		if !strings.EqualFold(state, "FINISHED") &&
			!strings.EqualFold(state, "NORMAL") {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	// Tới đây có hai trường hợp:
	//   1. Tìm thấy hàng idx_emb và state đã là FINISHED/NORMAL (hoặc phiên bản cũ với stateIdx<0);
	//   2. Không tìm thấy hàng idx_emb (Doris rất cũ không lộ tên chỉ mục này);
	// đều coi là sẵn sàng, không chặn. Nhánh chưa sẵn sàng đã return false sớm trong vòng lặp.
	return true, nil
}

// listEmbeddingTables trả về mọi bảng tên <base>_% trong database hiện tại,
// dùng cho truy xuất từ khóa / BatchUpdate xuyên số chiều.
func (r *dorisRepository) listEmbeddingTables(ctx context.Context) ([]string, error) {
	const q = `SELECT TABLE_NAME FROM information_schema.tables
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME LIKE ?`
	rows, err := r.db.QueryContext(ctx, q, r.database, r.tableBaseName+"\\_%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// bytesToString chuyển raw any mà SHOW INDEX trả về (thường là []byte hoặc string)
// thành chuỗi một cách an toàn.
func bytesToString(v any) string {
	switch s := v.(type) {
	case []byte:
		return string(s)
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", s)
	}
}
