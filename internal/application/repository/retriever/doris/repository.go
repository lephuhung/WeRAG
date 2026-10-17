package doris

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

const (
	defaultTableBaseName = "weknora_embeddings"
	envDorisTablePrefix  = "DORIS_TABLE_PREFIX"
)

// NewDorisRetrieveEngineRepository tạo kho truy xuất Doris.
//
// Parameters ：
//   - db: thể hiện *sql.DB giao thức MySQL. Bên gọi chịu trách nhiệm các tham số như SetMaxOpenConns.
//   - feHTTPBase: địa chỉ HTTP gốc của FE cho Stream Load (có scheme), ví dụ "http://doris-fe:8030".
//   - username/password: thông tin đăng nhập dùng chung cho MySQL và Stream Load.
//   - database: tên cơ sở dữ liệu đích (dùng cho cả MySQL DSN lẫn đường dẫn URL Stream Load).
//   - indexCfg: có thể nil. Khi nil thì lùi về biến môi trường + giá trị mặc định (đường env).
func NewDorisRetrieveEngineRepository(
	db *sql.DB,
	feHTTPBase, username, password, database string,
	indexCfg *types.IndexConfig,
) interfaces.RetrieveEngineRepository {
	log := logger.GetLogger(context.Background())
	log.Info("[Doris] Initializing Doris retriever engine repository")

	tableBaseName := types.ResolveCollectionName(indexCfg, envDorisTablePrefix, defaultTableBaseName)
	compatMode, invalidCompatMode := resolveConfiguredDorisCompatMode()
	if invalidCompatMode != "" {
		log.Warnf("[Doris] Invalid %s=%q, defaulting to %s", envDorisCompatMode, invalidCompatMode, dorisCompatModeAuto)
	}

	repo := &dorisRepository{
		db:                  db,
		httpClient:          newDorisStreamLoadHTTPClient(),
		feHTTPBase:          strings.TrimRight(feHTTPBase, "/"),
		username:            username,
		password:            password,
		database:            database,
		tableBaseName:       tableBaseName,
		bucketsNum:          indexCfg.GetBucketsNum(0),
		replicationNum:      indexCfg.GetReplicationNum(0),
		compatModeRequested: compatMode,
	}
	log.Infof("[Doris] Repository initialized: db=%s, base=%s, fe_http=%s, compat_mode=%s",
		database, tableBaseName, repo.feHTTPBase, repo.compatModeRequested)
	if os.Getenv(envDorisCompatMode) == "" {
		log.Infof("[Doris] %s not set, defaulting to %s and probing on first use", envDorisCompatMode, dorisCompatModeAuto)
	}
	return repo
}

func (r *dorisRepository) EngineType() types.RetrieverEngineType {
	return types.DorisRetrieverEngineType
}

func (r *dorisRepository) Support() []types.RetrieverType {
	return []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType}
}

// EstimateStorageSize ước tính số byte lưu trữ của danh sách IndexInfo.
//
// Theo thuật toán của Qdrant: độ dài trường payload + byte vector + láng giềng HNSW + metadata.
func (r *dorisRepository) EstimateStorageSize(_ context.Context,
	indexInfoList []*types.IndexInfo, params map[string]any,
) int64 {
	var total int64
	for _, info := range indexInfoList {
		emb := toDorisVectorEmbedding(info, params, dorisCompatModeInnerProductDuplicate)
		total += calculateStorageSize(emb)
	}
	return total
}

// Save ghi một bản ghi vào bảng ứng với số chiều. Vector rỗng bị BatchSave từ chối thống nhất.
func (r *dorisRepository) Save(ctx context.Context,
	info *types.IndexInfo, additionalParams map[string]any,
) error {
	return r.BatchSave(ctx, []*types.IndexInfo{info}, additionalParams)
}

// BatchSave gom cùng lô IndexInfo theo số chiều; bảng tương thích ANN của Doris dùng
// DUPLICATE KEY nên ở đây delete + insert tường minh theo id để giữ ngữ nghĩa thay thế cũ.
func (r *dorisRepository) BatchSave(ctx context.Context,
	indexInfoList []*types.IndexInfo, additionalParams map[string]any,
) error {
	log := logger.GetLogger(ctx)
	if len(indexInfoList) == 0 {
		return nil
	}
	compatMode, err := r.resolveCompatMode(ctx)
	if err != nil {
		return err
	}

	groups := make(map[int][]*DorisVectorEmbedding)
	for _, info := range indexInfoList {
		emb := toDorisVectorEmbedding(info, additionalParams, compatMode)
		if len(emb.Embedding) == 0 {
			log.Warnf("[Doris] Skipping empty embedding for chunk %s", info.ChunkID)
			continue
		}
		if err := validateEmbedding(emb.Embedding); err != nil {
			return fmt.Errorf("invalid embedding for chunk %s: %w", info.ChunkID, err)
		}
		// Cấp một khóa chính ổn định. SourceID là "danh tính hàng" có ý nghĩa nhất ở tầng trên,
		// và trong trường hợp một chunk nhiều question thì SourceID đã duy nhất, nên dùng thẳng.
		if emb.ID == "" {
			emb.ID = emb.SourceID
		}
		if emb.ID == "" {
			emb.ID = uuid.New().String()
		}
		dim := len(emb.Embedding)
		groups[dim] = append(groups[dim], emb)
	}

	for dim, rows := range groups {
		if err := r.ensureTable(ctx, dim); err != nil {
			return err
		}
		if compatMode.usesReplaceWrite() {
			err = r.replaceRows(ctx, r.getTableName(dim), rows)
		} else {
			err = r.insertRows(ctx, r.getTableName(dim), rows)
		}
		if err != nil {
			return fmt.Errorf("batch save dim=%d: %w", dim, err)
		}
		log.Infof("[Doris] Saved %d rows to %s", len(rows), r.getTableName(dim))
	}
	return nil
}

// insertRows nối một câu INSERT nhiều VALUES theo thứ tự cột. Cột embedding, do
// go-sql-driver/mysql không hỗ trợ chỗ giữ ARRAY, phải nối dạng literal vào văn bản SQL.
func (r *dorisRepository) insertRows(ctx context.Context,
	table string, rows []*DorisVectorEmbedding,
) error {
	if len(rows) == 0 {
		return nil
	}

	// 9 chỗ giữ thường + 1 literal embedding.
	const perRowPlaceholders = "(?, ?, ?, ?, ?, ?, ?, ?, ?, %s)"

	parts := make([]string, len(rows))
	args := make([]any, 0, len(rows)*9)
	for i, e := range rows {
		parts[i] = fmt.Sprintf(perRowPlaceholders, embeddingLiteral(e.Embedding))
		args = append(args,
			e.ID, e.Content, e.SourceID, e.SourceType,
			e.ChunkID, e.KnowledgeID, e.KnowledgeBaseID, e.TagID,
			e.IsEnabled,
		)
	}

	stmt := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES %s",
		table,
		strings.Join(columns, ", "),
		strings.Join(parts, ", "),
	)
	_, err := r.db.ExecContext(ctx, stmt, args...)
	return err
}

// replaceRows mô phỏng tường minh ngữ nghĩa "ghi đè theo id" trên bảng DUPLICATE KEY.
func (r *dorisRepository) replaceRows(ctx context.Context,
	table string, rows []*DorisVectorEmbedding,
) error {
	rows = dedupeRowsByID(rows)
	if len(rows) == 0 {
		return nil
	}

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if err := r.deleteRowsByID(ctx, table, ids); err != nil {
		return err
	}
	return r.insertRows(ctx, table, rows)
}

func (r *dorisRepository) deleteRowsByID(ctx context.Context,
	table string, ids []string,
) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	stmt := fmt.Sprintf("DELETE FROM `%s` WHERE %s IN (%s)",
		table, fieldID, strings.Join(placeholders, ", "))
	_, err := r.db.ExecContext(ctx, stmt, args...)
	return err
}

func dedupeRowsByID(rows []*DorisVectorEmbedding) []*DorisVectorEmbedding {
	if len(rows) < 2 {
		return rows
	}

	out := make([]*DorisVectorEmbedding, 0, len(rows))
	positions := make(map[string]int, len(rows))
	for _, row := range rows {
		if idx, ok := positions[row.ID]; ok {
			out[idx] = row
			continue
		}
		positions[row.ID] = len(out)
		out = append(out, row)
	}
	return out
}

// DeleteByChunkIDList xóa theo cột chunk_id. dimension dùng để xác định bảng cụ thể.
func (r *dorisRepository) DeleteByChunkIDList(ctx context.Context,
	chunkIDList []string, dimension int, _ string,
) error {
	return r.deleteByField(ctx, fieldChunkID, chunkIDList, dimension)
}

// DeleteByKnowledgeIDList xóa theo cột knowledge_id.
func (r *dorisRepository) DeleteByKnowledgeIDList(ctx context.Context,
	knowledgeIDList []string, dimension int, _ string,
) error {
	return r.deleteByField(ctx, fieldKnowledgeID, knowledgeIDList, dimension)
}

// DeleteBySourceIDList xóa theo cột source_id.
func (r *dorisRepository) DeleteBySourceIDList(ctx context.Context,
	sourceIDList []string, dimension int, _ string,
) error {
	return r.deleteByField(ctx, fieldSourceID, sourceIDList, dimension)
}

// deleteByField là hiện thực chung của ba phương thức Delete*:
// DELETE FROM <table> WHERE <field> IN (?, ?, ...).
func (r *dorisRepository) deleteByField(ctx context.Context,
	field string, ids []string, dimension int,
) error {
	log := logger.GetLogger(ctx)
	if len(ids) == 0 {
		return nil
	}

	table := r.getTableName(dimension)
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, v := range ids {
		placeholders[i] = "?"
		args[i] = v
	}
	stmt := fmt.Sprintf("DELETE FROM `%s` WHERE %s IN (%s)",
		table, field, strings.Join(placeholders, ", "))

	if _, err := r.db.ExecContext(ctx, stmt, args...); err != nil {
		log.Errorf("[Doris] Delete by %s failed: %v", field, err)
		return fmt.Errorf("delete by %s: %w", field, err)
	}
	log.Infof("[Doris] Deleted %d rows from %s by %s", len(ids), table, field)
	return nil
}

// Retrieve phân phối theo RetrieverType sang truy xuất vector hoặc từ khóa.
func (r *dorisRepository) Retrieve(ctx context.Context,
	params types.RetrieveParams,
) ([]*types.RetrieveResult, error) {
	switch params.RetrieverType {
	case types.VectorRetrieverType:
		return r.VectorRetrieve(ctx, params)
	case types.KeywordsRetrieverType:
		return r.KeywordsRetrieve(ctx, params)
	}
	return nil, fmt.Errorf("invalid retriever type: %v", params.RetrieverType)
}

// VectorRetrieve chuẩn hóa đơn vị vector truy vấn rồi gọi inner_product_approximate để
// tìm ANN; với vector đơn vị, inner product tương đương cosine similarity,
// nên score vẫn giữ ngữ nghĩa "càng lớn càng giống".
func (r *dorisRepository) VectorRetrieve(ctx context.Context,
	params types.RetrieveParams,
) ([]*types.RetrieveResult, error) {
	log := logger.GetLogger(ctx)
	if err := validateEmbedding(params.Embedding); err != nil {
		return nil, fmt.Errorf("invalid query embedding: %w", err)
	}
	compatMode, err := r.resolveCompatMode(ctx)
	if err != nil {
		return nil, err
	}
	queryEmbedding := append([]float32(nil), params.Embedding...)
	if compatMode.normalizeEmbeddings() {
		queryEmbedding = normalizeEmbedding(queryEmbedding)
	}
	dim := len(params.Embedding)
	table := r.getTableName(dim)

	exists, err := r.tableExists(ctx, table)
	if err != nil {
		return nil, fmt.Errorf("check table %s: %w", table, err)
	}
	if !exists {
		log.Warnf("[Doris] Table %s does not exist, returning empty results", table)
		return buildRetrieveResult(nil, types.VectorRetrieverType), nil
	}

	wb := buildBaseFilter(params)
	whereClause, whereArgs := wb.build()

	// embedding phải là literal; Doris không hỗ trợ chỗ giữ cho LIMIT/OFFSET nên cũng phải nội tuyến literal.
	// Dùng HAVING vì score là bí danh cột SELECT, giai đoạn WHERE chưa thấy.
	scoreExpr := fmt.Sprintf("inner_product_approximate(`%s`, %s)", fieldEmbedding, embeddingLiteral(queryEmbedding))
	if compatMode == dorisCompatModeLegacy {
		scoreExpr = fmt.Sprintf("(1 - cosine_distance_approximate(`%s`, %s))", fieldEmbedding, embeddingLiteral(queryEmbedding))
	}
	stmt := fmt.Sprintf(
		"SELECT %s, %s AS score "+
			"FROM `%s` WHERE %s "+
			"HAVING score >= ? "+
			"ORDER BY score DESC LIMIT %d",
		strings.Join(columnsForRetrieve, ", "),
		scoreExpr,
		table,
		whereClause,
		params.TopK,
	)
	args := append(whereArgs, params.Threshold)

	rows, err := r.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, r.wrapVectorRetrieveError(table, compatMode, err)
	}
	defer rows.Close()

	results, err := scanRetrieveRows(rows, types.MatchTypeEmbedding)
	if err != nil {
		return nil, err
	}
	log.Infof("[Doris] Vector retrieval found %d results in %s", len(results), table)
	return buildRetrieveResult(results, types.VectorRetrieverType), nil
}

// KeywordsRetrieve dùng chỉ mục đảo của Doris + MATCH_ANY để khớp từ khóa.
//
// Không cần tách từ jieba phía client: idx_content đã khai báo chinese parser khi CREATE TABLE.
// Các bảng khác số chiều được gộp lấy topK, như hiện trạng Milvus/Weaviate.
func (r *dorisRepository) KeywordsRetrieve(ctx context.Context,
	params types.RetrieveParams,
) ([]*types.RetrieveResult, error) {
	log := logger.GetLogger(ctx)
	query := strings.TrimSpace(params.Query)
	if query == "" {
		return buildRetrieveResult(nil, types.KeywordsRetrieverType), nil
	}

	tables, err := r.listEmbeddingTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	if len(tables) == 0 {
		return buildRetrieveResult(nil, types.KeywordsRetrieverType), nil
	}

	wb := buildBaseFilter(params)
	whereClause, whereArgs := wb.build()

	var all []*types.IndexWithScore
	for _, table := range tables {
		stmt := fmt.Sprintf(
			"SELECT %s FROM `%s` WHERE %s AND %s MATCH_ANY ? LIMIT %d",
			strings.Join(columnsForRetrieve, ", "),
			table, whereClause, fieldContent,
			params.TopK,
		)
		args := append(append([]any{}, whereArgs...), query)

		rows, err := r.db.QueryContext(ctx, stmt, args...)
		if err != nil {
			log.Warnf("[Doris] Keyword retrieve in %s failed: %v", table, err)
			continue
		}
		// score cố định 1.0 trong KeywordsRetrieve, giống hành vi Qdrant.
		batch, scanErr := scanRetrieveRows(rows, types.MatchTypeKeywords)
		_ = rows.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		all = append(all, batch...)
	}
	if len(all) > params.TopK {
		all = all[:params.TopK]
	}
	log.Infof("[Doris] Keywords retrieval found %d results across %d tables", len(all), len(tables))
	return buildRetrieveResult(all, types.KeywordsRetrieverType), nil
}

// CopyIndices sao chép chunk từ kho tri thức nguồn sang kho đích, tránh tạo lại embedding.
//
// Phản chiếu hoàn toàn hiện thực của Qdrant:
//   - quét bảng nguồn theo trang
//   - dịch chunk_id theo sourceToTargetChunkIDMap
//   - xử lý quy tắc dịch source_id (chunk thường / câu hỏi sinh ra / khác)
//   - ghi hàng đích trở lại cùng bảng
func (r *dorisRepository) CopyIndices(ctx context.Context,
	sourceKnowledgeBaseID string,
	sourceToTargetKBIDMap map[string]string,
	sourceToTargetChunkIDMap map[string]string,
	targetKnowledgeBaseID string,
	dimension int,
	_ string,
) error {
	log := logger.GetLogger(ctx)
	if len(sourceToTargetChunkIDMap) == 0 {
		return nil
	}
	if err := r.ensureTable(ctx, dimension); err != nil {
		return err
	}

	table := r.getTableName(dimension)
	const pageSize = 64
	offset := 0
	totalCopied := 0

	for {
		stmt := fmt.Sprintf(
			"SELECT %s FROM `%s` WHERE %s = ? ORDER BY %s LIMIT %d OFFSET %d",
			strings.Join(columnsForCopy, ", "),
			table, fieldKnowledgeBaseID, fieldID,
			pageSize, offset,
		)
		rows, err := r.db.QueryContext(ctx, stmt, sourceKnowledgeBaseID)
		if err != nil {
			return fmt.Errorf("copy indices scan: %w", err)
		}
		batch, err := scanCopyRows(rows)
		_ = rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}

		var targets []*DorisVectorEmbedding
		for _, src := range batch {
			targetChunkID, ok := sourceToTargetChunkIDMap[src.ChunkID]
			if !ok {
				log.Warnf("[Doris] Source chunk %s not in target mapping", src.ChunkID)
				continue
			}
			targetKnowledgeID, ok := sourceToTargetKBIDMap[src.KnowledgeID]
			if !ok {
				log.Warnf("[Doris] Source knowledge %s not in target mapping", src.KnowledgeID)
				continue
			}

			targetSourceID := translateSourceID(src.SourceID, src.ChunkID, targetChunkID)
			targets = append(targets, &DorisVectorEmbedding{
				ID:              uuid.New().String(),
				Content:         src.Content,
				SourceID:        targetSourceID,
				SourceType:      src.SourceType,
				ChunkID:         targetChunkID,
				KnowledgeID:     targetKnowledgeID,
				KnowledgeBaseID: targetKnowledgeBaseID,
				TagID:           src.TagID,
				IsEnabled:       src.IsEnabled,
				Embedding:       src.Embedding,
			})
		}

		if len(targets) > 0 {
			if err := r.insertRows(ctx, table, targets); err != nil {
				return fmt.Errorf("copy indices insert: %w", err)
			}
			totalCopied += len(targets)
		}

		if len(batch) < pageSize {
			break
		}
		offset += pageSize
	}
	log.Infof("[Doris] CopyIndices done, dim=%d, copied=%d", dimension, totalCopied)
	return nil
}

// BatchUpdateChunkEnabledStatus / BatchUpdateChunkTagID hiện thực thật nằm ở streamload.go,
// chọn partial update hoặc rewrite rows theo compat mode.

// ---------------------------------------------------------------------------
// Hàm phụ trợ riêng
// ---------------------------------------------------------------------------

// toDorisVectorEmbedding chuyển IndexInfo + ánh xạ embedding tầng trên truyền vào thành
// mô hình hàng Doris. Embedding lấy theo SourceID từ map[string][]float32
// trong additionalParams[fieldEmbedding], giống hệt Qdrant/Milvus.
// Chế độ inner_product_duplicate chuẩn hóa đơn vị trước, chế độ legacy giữ vector gốc.
func toDorisVectorEmbedding(
	info *types.IndexInfo,
	additionalParams map[string]any,
	compatMode dorisCompatMode,
) *DorisVectorEmbedding {
	emb := &DorisVectorEmbedding{
		ID:              info.ID,
		Content:         info.Content,
		SourceID:        info.SourceID,
		SourceType:      int(info.SourceType),
		ChunkID:         info.ChunkID,
		KnowledgeID:     info.KnowledgeID,
		KnowledgeBaseID: info.KnowledgeBaseID,
		TagID:           info.TagID,
		IsEnabled:       info.IsEnabled,
	}
	if additionalParams != nil {
		if v, ok := additionalParams[fieldEmbedding]; ok {
			if m, ok := v.(map[string][]float32); ok {
				emb.Embedding = append([]float32(nil), m[info.SourceID]...)
				if compatMode.normalizeEmbeddings() {
					emb.Embedding = normalizeEmbedding(emb.Embedding)
				}
			}
		}
	}
	return emb
}

func (r *dorisRepository) wrapVectorRetrieveError(table string, compatMode dorisCompatMode, err error) error {
	if compatMode == dorisCompatModeLegacy {
		return fmt.Errorf(
			"vector retrieve %s in Doris compat mode %s: %w. If your Doris build does not support cosine_distance_approximate or ANN on UNIQUE KEY tables, set %s=%s before creating embedding tables. %s is not interchangeable after %s_* tables are created",
			table,
			compatMode,
			err,
			envDorisCompatMode,
			dorisCompatModeInnerProductDuplicate,
			envDorisCompatMode,
			r.tableBaseName,
		)
	}
	return fmt.Errorf("vector retrieve %s in Doris compat mode %s: %w", table, compatMode, err)
}

// translateSourceID dịch SourceID nguồn sang SourceID đích, phản chiếu hiện thực Qdrant:
//   - chunk thường: SourceID == ChunkID  -> dùng targetChunkID
//   - câu hỏi sinh ra: SourceID == "<chunkID>-<questionID>" -> "<targetChunkID>-<questionID>"
//   - trường hợp khác: sinh UUID mới (giữ tính duy nhất)
func translateSourceID(originalSourceID, sourceChunkID, targetChunkID string) string {
	switch {
	case originalSourceID == sourceChunkID:
		return targetChunkID
	case strings.HasPrefix(originalSourceID, sourceChunkID+"-"):
		questionID := strings.TrimPrefix(originalSourceID, sourceChunkID+"-")
		return fmt.Sprintf("%s-%s", targetChunkID, questionID)
	default:
		return uuid.New().String()
	}
}

// scanRetrieveRows giải tuần tự rows của giai đoạn Retrieve thành danh sách IndexWithScore.
//
// Hai nhánh:
//   - số cột == columnsForRetrieve+1: cột N+1 là score (đường truy xuất vector)
//   - số cột == columnsForRetrieve: score gán đồng loạt 1.0 (đường truy xuất từ khóa)
func scanRetrieveRows(rows *sql.Rows, matchType types.MatchType) ([]*types.IndexWithScore, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	withScore := len(cols) == len(columnsForRetrieve)+1

	var out []*types.IndexWithScore
	for rows.Next() {
		var (
			id, content, sourceID, chunkID      string
			knowledgeID, knowledgeBaseID, tagID string
			sourceType                          int
			isEnabled                           bool
			score                               float64
			err                                 error
		)
		if withScore {
			err = rows.Scan(&id, &content, &sourceID, &sourceType,
				&chunkID, &knowledgeID, &knowledgeBaseID, &tagID, &isEnabled, &score)
		} else {
			err = rows.Scan(&id, &content, &sourceID, &sourceType,
				&chunkID, &knowledgeID, &knowledgeBaseID, &tagID, &isEnabled)
			score = 1.0
		}
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		out = append(out, &types.IndexWithScore{
			ID:              id,
			Content:         content,
			SourceID:        sourceID,
			SourceType:      types.SourceType(sourceType),
			ChunkID:         chunkID,
			KnowledgeID:     knowledgeID,
			KnowledgeBaseID: knowledgeBaseID,
			TagID:           tagID,
			Score:           score,
			MatchType:       matchType,
		})
	}
	return out, rows.Err()
}

// scanCopyRows giải tuần tự kết quả truy vấn phân trang của CopyIndices.
//
// Khác scanRetrieveRows, ở đây cần trường embedding (để sao chép vector gốc).
// ARRAY<FLOAT> của Doris qua giao thức mysql trả về literal chuỗi "[1,2,3]".
func scanCopyRows(rows *sql.Rows) ([]*DorisVectorEmbedding, error) {
	var out []*DorisVectorEmbedding
	for rows.Next() {
		var (
			id, content, sourceID, chunkID      string
			knowledgeID, knowledgeBaseID, tagID string
			sourceType                          int
			isEnabled                           bool
			embeddingRaw                        sql.RawBytes
		)
		if err := rows.Scan(&id, &content, &sourceID, &sourceType,
			&chunkID, &knowledgeID, &knowledgeBaseID, &tagID, &isEnabled, &embeddingRaw); err != nil {
			return nil, fmt.Errorf("scan copy row: %w", err)
		}
		vec, err := parseEmbeddingLiteral(embeddingRaw)
		if err != nil {
			return nil, fmt.Errorf("parse embedding: %w", err)
		}
		out = append(out, &DorisVectorEmbedding{
			ID:              id,
			Content:         content,
			SourceID:        sourceID,
			SourceType:      sourceType,
			ChunkID:         chunkID,
			KnowledgeID:     knowledgeID,
			KnowledgeBaseID: knowledgeBaseID,
			TagID:           tagID,
			IsEnabled:       isEnabled,
			Embedding:       vec,
		})
	}
	return out, rows.Err()
}

// buildRetrieveResult bọc danh sách IndexWithScore thành RetrieveResult.
func buildRetrieveResult(results []*types.IndexWithScore, retrieverType types.RetrieverType) []*types.RetrieveResult {
	return []*types.RetrieveResult{{
		Results:             results,
		RetrieverEngineType: types.DorisRetrieverEngineType,
		RetrieverType:       retrieverType,
		Error:               nil,
	}}
}

// calculateStorageSize ước tính chi phí lưu trữ một hàng.
//
// Giống Qdrant: byte chuỗi payload + vector (dim*4) + HNSW M*2*8 + metadata 24.
func calculateStorageSize(emb *DorisVectorEmbedding) int64 {
	var payload int64
	payload += int64(len(emb.Content))
	payload += int64(len(emb.SourceID))
	payload += int64(len(emb.ChunkID))
	payload += int64(len(emb.KnowledgeID))
	payload += int64(len(emb.KnowledgeBaseID))
	payload += int64(len(emb.TagID))
	payload += 8 // source_type int

	var vec int64
	var hnsw int64
	if len(emb.Embedding) > 0 {
		vec = int64(len(emb.Embedding)) * 4
		const hnswM = 32
		hnsw = hnswM * 2 * 8
	}
	const metaBytes int64 = 24
	return payload + vec + hnsw + metaBytes
}
