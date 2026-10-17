package doris

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// Các hằng liên quan Stream Load.
const (
	// Giới hạn JSON body của một lô Stream Load (giá trị bảo thủ, nhỏ hơn nhiều so với mặc định streaming_load_max_mb=10240 của Doris).
	// Mục đích chính là kiểm soát độ trễ đuôi của một yêu cầu HTTP; vượt quá thì tự tách lô.
	streamLoadMaxBatchBytes = 1 << 20 // 1 MiB

	// Header HTTP Authorization dùng Basic auth; Stream Load cũng hỗ trợ token,
	// ở đây dùng phương án tên đăng nhập/mật khẩu phổ biến nhất để thống nhất với giao thức MySQL.
	headerAuthorization = "Authorization"
	headerExpect        = "Expect"
	headerContentType   = "Content-Type"
)

// streamLoadResponse là thân kết quả Stream Load do Doris FE/BE trả về.
//
// Trường chính: Status phải là "Success" hoặc "Publish Timeout" (trường hợp sau nghĩa là dữ liệu đã ghi nhưng giao dịch publish quá hạn,
// vẫn coi là thành công). Mọi trạng thái khác coi là thất bại.
type streamLoadResponse struct {
	TxnId                  int64  `json:"TxnId"`
	Label                  string `json:"Label"`
	Status                 string `json:"Status"`
	Message                string `json:"Message"`
	NumberTotalRows        int64  `json:"NumberTotalRows"`
	NumberLoadedRows       int64  `json:"NumberLoadedRows"`
	NumberFilteredRows     int64  `json:"NumberFilteredRows"`
	NumberUnselectedRows   int64  `json:"NumberUnselectedRows"`
	LoadBytes              int64  `json:"LoadBytes"`
	LoadTimeMs             int64  `json:"LoadTimeMs"`
	BeginTxnTimeMs         int64  `json:"BeginTxnTimeMs"`
	StreamLoadPutTimeMs    int64  `json:"StreamLoadPutTimeMs"`
	ReadDataTimeMs         int64  `json:"ReadDataTimeMs"`
	WriteDataTimeMs        int64  `json:"WriteDataTimeMs"`
	CommitAndPublishTimeMs int64  `json:"CommitAndPublishTimeMs"`
	ErrorURL               string `json:"ErrorURL"`
}

// streamLoadURL ghép endpoint HTTP Stream Load của một bảng.
func (r *dorisRepository) streamLoadURL(table string) string {
	return fmt.Sprintf("%s/api/%s/%s/_stream_load",
		r.feHTTPBase, url.PathEscape(r.database), url.PathEscape(table))
}

// newDorisStreamLoadHTTPClient protects both the initial FE request and every
// FE -> BE redirect at connection time. Doris requires Basic auth to survive
// the 307 redirect, so redirect targets on another host must be explicitly
// trusted through the SSRF whitelist before credentials are forwarded.
func newDorisStreamLoadHTTPClient() *http.Client {
	cfg := secutils.DefaultSSRFSafeHTTPClientConfig()
	// Stream Load calls carry their own context deadline. Preserve the previous
	// client behaviour instead of imposing the generic 30-second HTTP timeout.
	cfg.Timeout = 0

	client := secutils.NewSSRFSafeHTTPClient(cfg)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= cfg.MaxRedirects {
			return fmt.Errorf("stopped after %d redirects", cfg.MaxRedirects)
		}
		if len(via) == 0 {
			return nil
		}

		// Stream Load deliberately replays PUT bodies to a BE. Keep this
		// exception local to Doris; generic clients must not replay requests
		// across origins. Every cross-origin destination must be trusted.
		source := via[0].URL
		sameOrigin := strings.EqualFold(source.Scheme, req.URL.Scheme) &&
			strings.EqualFold(source.Host, req.URL.Host)
		if !sameOrigin && !secutils.IsSSRFWhitelisted(req.URL.Hostname()) {
			return fmt.Errorf("%w: stream load target host %q is not trusted to receive credentials",
				secutils.ErrSSRFRedirectBlocked, req.URL.Hostname())
		}
		if strings.EqualFold(source.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
			return fmt.Errorf("%w: stream load HTTPS downgrade is forbidden", secutils.ErrSSRFRedirectBlocked)
		}
		if err := secutils.ValidateURLForSSRF(req.URL.String()); err != nil {
			return fmt.Errorf("%w: %w", secutils.ErrSSRFRedirectBlocked, err)
		}

		// The shared transport still validates each URL and connection. Preserve
		// load options and restore Basic auth only after checking the destination.
		req.Header.Set(headerAuthorization, via[0].Header.Get(headerAuthorization))
		return nil
	}
	return client
}

// partialUpdateRows ghi một số hàng trở lại bảng đích qua chế độ partial update của Stream Load.
//
// columns là các cột tham gia partial update lần này (phải gồm cột UNIQUE KEY, tức "id").
// Mỗi phần tử của rows là mảng giá trị trường có độ dài bằng columns.
//
// Điểm chính của hiện thực:
//  1. Body dạng mảng JSON, header thêm strip_outer_array=true.
//  2. Đặt partial_columns=true, merge_type=APPEND để kích hoạt chế độ partial update của Doris
//     (cách chuẩn với Doris 4.1 + bảng UNIQUE KEY MoW).
//  3. Tự tách lô theo streamLoadMaxBatchBytes để một lần không quá lớn.
//  4. Xử lý 307: FE của Doris redirect sang BE, net/http mặc định đi theo;
//     cần đảm bảo GetBody gửi lại được (đã thỏa nhờ dựng Body bằng bytes.NewReader).
func (r *dorisRepository) partialUpdateRows(ctx context.Context,
	table string, columns []string, rows []map[string]any,
) error {
	if len(rows) == 0 {
		return nil
	}
	for _, batch := range chunkRows(rows, streamLoadMaxBatchBytes) {
		if err := r.streamLoadOnce(ctx, table, columns, batch); err != nil {
			return err
		}
	}
	return nil
}

// streamLoadOnce gửi một yêu cầu HTTP Stream Load.
func (r *dorisRepository) streamLoadOnce(ctx context.Context,
	table string, columns []string, rows []map[string]any,
) error {
	log := logger.GetLogger(ctx)
	if len(rows) == 0 {
		return nil
	}

	body, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("marshal stream load body: %w", err)
	}

	url := r.streamLoadURL(table)
	// Validate at the final outbound boundary as well as when user-supplied
	// vector-store configuration is created. This covers stored/env configs and
	// keeps the tainted value from reaching http.Client.Do unchecked.
	if err := secutils.ValidateURLForSSRF(url); err != nil {
		return fmt.Errorf("stream load URL blocked by SSRF validation: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build stream load request: %w", err)
	}

	// GetBody cho phép đọc lại body khi redirect (307 từ FE -> BE cần gửi lại PUT body).
	bodyCopy := append([]byte(nil), body...)
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyCopy)), nil
	}
	req.ContentLength = int64(len(body))

	auth := base64.StdEncoding.EncodeToString([]byte(r.username + ":" + r.password))
	req.Header.Set(headerAuthorization, "Basic "+auth)
	req.Header.Set(headerExpect, "100-continue")
	req.Header.Set(headerContentType, "application/json")
	req.Header.Set("format", "json")
	req.Header.Set("strip_outer_array", "true")
	req.Header.Set("partial_columns", "true")
	req.Header.Set("columns", strings.Join(columns, ","))
	req.Header.Set("merge_type", "APPEND")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("stream load HTTP: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read stream load response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("stream load HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result streamLoadResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decode stream load response: %w (raw=%s)", err, string(respBody))
	}

	switch result.Status {
	case "Success", "Publish Timeout":
		log.Infof("[Doris] Stream load %s OK: rows=%d, loaded=%d, label=%s",
			table, result.NumberTotalRows, result.NumberLoadedRows, result.Label)
		return nil
	default:
		return fmt.Errorf("stream load failed: status=%s msg=%s err_url=%s",
			result.Status, result.Message, result.ErrorURL)
	}
}

// chunkRows cắt các hàng theo kích thước JSON tích lũy, mỗi phần không quá maxBytes.
//
// Lưu ý: chi phí thực của tuần tự hóa JSON xấp xỉ số byte sau marshal, còn marshal từng hàng
// cộng dấu phẩy + ngoặc mảng xấp xỉ ước tính này. Dùng ước tính thô để khỏi marshal mỗi phần.
func chunkRows(rows []map[string]any, maxBytes int) [][]map[string]any {
	if len(rows) == 0 {
		return nil
	}

	var (
		out    [][]map[string]any
		curr   []map[string]any
		size   int
		header = 2 // "[" + "]"
	)

	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			// marshal lỗi thì tách hàng này thành phần riêng, để streamLoadOnce ở trên marshal lại và báo lỗi.
			if len(curr) > 0 {
				out = append(out, curr)
			}
			out = append(out, []map[string]any{row})
			curr = nil
			size = 0
			continue
		}
		// Cộng chỗ dấu phẩy (trừ hàng đầu).
		need := len(raw)
		if len(curr) > 0 {
			need++
		}
		if size+need+header > maxBytes && len(curr) > 0 {
			out = append(out, curr)
			curr = nil
			size = 0
		}
		curr = append(curr, row)
		size += need
	}
	if len(curr) > 0 {
		out = append(out, curr)
	}
	return out
}

// ---------------------------------------------------------------------------
// Phương thức nghiệp vụ: BatchUpdateChunkEnabledStatus / BatchUpdateChunkTagID
// ---------------------------------------------------------------------------

// BatchUpdateChunkEnabledStatus cập nhật hàng loạt trường is_enabled của chunk.
// Chế độ legacy dùng Stream Load partial update; chế độ inner_product_duplicate đọc cả hàng rồi ghi lại bằng replaceRows.
func (r *dorisRepository) BatchUpdateChunkEnabledStatus(ctx context.Context,
	chunkStatusMap map[string]bool,
) error {
	if len(chunkStatusMap) == 0 {
		return nil
	}
	compatMode, err := r.resolveCompatMode(ctx)
	if err != nil {
		return err
	}
	if !compatMode.usesRewriteChunkUpdates() {
		return r.batchUpdateChunkEnabledStatusLegacy(ctx, chunkStatusMap)
	}

	chunkIDs := make([]string, 0, len(chunkStatusMap))
	for id := range chunkStatusMap {
		chunkIDs = append(chunkIDs, id)
	}

	return r.rewriteChunkRows(ctx, chunkIDs, func(row *DorisVectorEmbedding) bool {
		enabled, ok := chunkStatusMap[row.ChunkID]
		if !ok || row.IsEnabled == enabled {
			return false
		}
		row.IsEnabled = enabled
		return true
	}, "rewrite is_enabled")
}

// BatchUpdateChunkTagID cập nhật hàng loạt trường tag_id của chunk. Logic giống EnabledStatus.
func (r *dorisRepository) BatchUpdateChunkTagID(ctx context.Context,
	chunkTagMap map[string]string,
) error {
	if len(chunkTagMap) == 0 {
		return nil
	}
	compatMode, err := r.resolveCompatMode(ctx)
	if err != nil {
		return err
	}
	if !compatMode.usesRewriteChunkUpdates() {
		return r.batchUpdateChunkTagIDLegacy(ctx, chunkTagMap)
	}

	chunkIDs := make([]string, 0, len(chunkTagMap))
	for id := range chunkTagMap {
		chunkIDs = append(chunkIDs, id)
	}

	return r.rewriteChunkRows(ctx, chunkIDs, func(row *DorisVectorEmbedding) bool {
		tagID, ok := chunkTagMap[row.ChunkID]
		if !ok || row.TagID == tagID {
			return false
		}
		row.TagID = tagID
		return true
	}, "rewrite tag_id")
}

func (r *dorisRepository) rewriteChunkRows(ctx context.Context,
	chunkIDs []string,
	mutate func(*DorisVectorEmbedding) bool,
	action string,
) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	tables, err := r.listEmbeddingTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}

	for _, table := range tables {
		rows, err := r.loadRowsByChunkIDs(ctx, table, chunkIDs)
		if err != nil {
			return fmt.Errorf("load chunk rows from %s: %w", table, err)
		}

		updated := make([]*DorisVectorEmbedding, 0, len(rows))
		for _, row := range rows {
			if !mutate(row) {
				continue
			}
			updated = append(updated, row)
		}
		if len(updated) == 0 {
			continue
		}

		if err := r.replaceRows(ctx, table, updated); err != nil {
			return fmt.Errorf("%s in %s: %w", action, table, err)
		}
	}
	return nil
}

func (r *dorisRepository) batchUpdateChunkEnabledStatusLegacy(ctx context.Context,
	chunkStatusMap map[string]bool,
) error {
	chunkIDs := make([]string, 0, len(chunkStatusMap))
	for id := range chunkStatusMap {
		chunkIDs = append(chunkIDs, id)
	}

	mapping, err := r.lookupChunkRowKeys(ctx, chunkIDs)
	if err != nil {
		return err
	}

	byTable := make(map[string][]map[string]any)
	for chunkID, locations := range mapping {
		enabled, ok := chunkStatusMap[chunkID]
		if !ok {
			continue
		}
		for _, loc := range locations {
			byTable[loc.table] = append(byTable[loc.table], map[string]any{
				fieldID:        loc.id,
				fieldIsEnabled: enabled,
			})
		}
	}
	for table, rows := range byTable {
		if err := r.partialUpdateRows(ctx, table, []string{fieldID, fieldIsEnabled}, rows); err != nil {
			return fmt.Errorf("partial update is_enabled in %s: %w", table, err)
		}
	}
	return nil
}

func (r *dorisRepository) batchUpdateChunkTagIDLegacy(ctx context.Context,
	chunkTagMap map[string]string,
) error {
	chunkIDs := make([]string, 0, len(chunkTagMap))
	for id := range chunkTagMap {
		chunkIDs = append(chunkIDs, id)
	}

	mapping, err := r.lookupChunkRowKeys(ctx, chunkIDs)
	if err != nil {
		return err
	}

	byTable := make(map[string][]map[string]any)
	for chunkID, locations := range mapping {
		tagID, ok := chunkTagMap[chunkID]
		if !ok {
			continue
		}
		for _, loc := range locations {
			byTable[loc.table] = append(byTable[loc.table], map[string]any{
				fieldID:    loc.id,
				fieldTagID: tagID,
			})
		}
	}
	for table, rows := range byTable {
		if err := r.partialUpdateRows(ctx, table, []string{fieldID, fieldTagID}, rows); err != nil {
			return fmt.Errorf("partial update tag_id in %s: %w", table, err)
		}
	}
	return nil
}

func (r *dorisRepository) loadRowsByChunkIDs(ctx context.Context,
	table string, chunkIDs []string,
) ([]*DorisVectorEmbedding, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, v := range chunkIDs {
		placeholders[i] = "?"
		args[i] = v
	}

	stmt := fmt.Sprintf(
		"SELECT %s FROM `%s` WHERE %s IN (%s)",
		strings.Join(columnsForCopy, ", "),
		table,
		fieldChunkID,
		strings.Join(placeholders, ", "),
	)
	rows, err := r.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	batch, err := scanCopyRows(rows)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	return batch, nil
}

// rowLocation cho biết một hàng nằm ở bảng nào và khóa chính id là gì.
type rowLocation struct {
	table string
	id    string
}

// lookupChunkRowKeys truy vấn vị trí vật lý của các chunkIDs đã cho trong mọi bảng <base>_<dim>:
//   - key: chunk_id
//   - value: [(table, id), ...], vì cùng một chunk có thể có bản sao ở bảng của nhiều số chiều.
//
// Truy vấn xuyên bảng dùng mọi bảng khớp do listEmbeddingTables liệt kê; mỗi bảng chạy một lần
// SELECT id, chunk_id FROM <table> WHERE chunk_id IN (?, ?, ...).
func (r *dorisRepository) lookupChunkRowKeys(ctx context.Context,
	chunkIDs []string,
) (map[string][]rowLocation, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}
	tables, err := r.listEmbeddingTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	if len(tables) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, v := range chunkIDs {
		placeholders[i] = "?"
		args[i] = v
	}

	out := make(map[string][]rowLocation)
	for _, table := range tables {
		stmt := fmt.Sprintf(
			"SELECT %s, %s FROM `%s` WHERE %s IN (%s)",
			fieldID, fieldChunkID, table, fieldChunkID, strings.Join(placeholders, ", "),
		)
		rows, err := r.db.QueryContext(ctx, stmt, args...)
		if err != nil {
			return nil, fmt.Errorf("lookup chunk row keys in %s: %w", table, err)
		}
		for rows.Next() {
			var id, chunkID string
			if err := rows.Scan(&id, &chunkID); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan row keys: %w", err)
			}
			out[chunkID] = append(out[chunkID], rowLocation{table: table, id: id})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}
