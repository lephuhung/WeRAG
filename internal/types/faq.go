package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/textconv"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// FAQChunkMetadata định nghĩa cấu trúc mục FAQ trong Chunk.Metadata
type FAQChunkMetadata struct {
	StandardQuestion  string         `json:"standard_question"`
	SimilarQuestions  []string       `json:"similar_questions,omitempty"`
	NegativeQuestions []string       `json:"negative_questions,omitempty"`
	Answers           []string       `json:"answers,omitempty"`
	AnswerStrategy    AnswerStrategy `json:"answer_strategy,omitempty"`
	Version           int            `json:"version,omitempty"`
	Source            string         `json:"source,omitempty"`
}

// GeneratedQuestion là một câu hỏi do AI sinh
type GeneratedQuestion struct {
	ID              string `json:"id"`                         // Định danh duy nhất, dùng để tạo source_id
	Question        string `json:"question"`                   // Nội dung câu hỏi
	ContentRevision *int   `json:"content_revision,omitempty"` // Phiên bản nội dung chunk tương ứng với câu hỏi này
}

const maxGeneratedQuestionSourceIDLength = 64

// GeneratedQuestionSourceID builds the retrieval source identifier for a
// generated question. PostgreSQL stores source_id as varchar(64), while a
// chunk UUID plus a question UUID would be 73 bytes. Preserve the historical
// representation for short IDs and hash only oversized question IDs so
// existing index rows remain addressable by delete/reindex operations.
func GeneratedQuestionSourceID(chunkID, questionID string) string {
	candidate := chunkID + "-" + questionID
	if len(candidate) <= maxGeneratedQuestionSourceIDLength {
		return candidate
	}
	digest := sha256.Sum256([]byte(questionID))
	// UUID chunk IDs use 36 bytes; "-q" plus 24 hex characters keeps the
	// complete identifier at 62 bytes while retaining ample collision space.
	return chunkID + "-q" + hex.EncodeToString(digest[:12])
}

// DocumentChunkMetadata định nghĩa cấu trúc metadata của chunk tài liệu
// Dùng để lưu thông tin bổ sung như câu hỏi do AI sinh
type DocumentChunkMetadata struct {
	// GeneratedQuestions lưu các câu hỏi liên quan do AI sinh cho chunk này
	// Các câu hỏi này được lập chỉ mục riêng để tăng độ truy hồi
	GeneratedQuestions []GeneratedQuestion `json:"generated_questions,omitempty"`
	// GeneratedQuestionsRevision ties the questions to Chunk.ContentRevision.
	GeneratedQuestionsRevision int `json:"generated_questions_revision,omitempty"`
	// Legal carries Vietnamese legal-document metadata (Điều/Khoản/Điểm
	// markers, heading path) when the chunk was produced by the
	// vietnamese_legal chunking tier. Nested under "legal" so it cannot
	// collide with the generated_questions namespace.
	Legal *vietnamese_legal.LegalMetadata `json:"legal,omitempty"`
}

// IsQuestionCurrent reports whether a generated question was authored for the
// current chunk body. This is advisory metadata for the UI: questions remain
// valid retrieval aliases across chunk edits. Legacy rows fall back to the
// metadata-level revision.
func (m *DocumentChunkMetadata) IsQuestionCurrent(question GeneratedQuestion, chunkRevision int) bool {
	if question.ContentRevision != nil {
		return *question.ContentRevision == chunkRevision
	}
	return m != nil && m.GeneratedQuestionsRevision == chunkRevision
}

// GetQuestionStrings trả về danh sách chuỗi nội dung câu hỏi (tương thích mã cũ)
func (m *DocumentChunkMetadata) GetQuestionStrings() []string {
	if m == nil || len(m.GeneratedQuestions) == 0 {
		return nil
	}
	result := make([]string, len(m.GeneratedQuestions))
	for i, q := range m.GeneratedQuestions {
		result[i] = q.Question
	}
	return result
}

// DocumentMetadata phân tích metadata tài liệu trong chunk
func (c *Chunk) DocumentMetadata() (*DocumentChunkMetadata, error) {
	if c == nil || len(c.Metadata) == 0 {
		return nil, nil
	}
	var meta DocumentChunkMetadata
	if err := json.Unmarshal(c.Metadata, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// SetDocumentMetadata đặt metadata tài liệu cho chunk
func (c *Chunk) SetDocumentMetadata(meta *DocumentChunkMetadata) error {
	if c == nil {
		return nil
	}
	if meta == nil {
		c.Metadata = nil
		return nil
	}
	bytes, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	c.Metadata = JSON(bytes)
	return nil
}

// Sanitize làm sạch cơ bản metadata (bỏ khoảng trắng đầu cuối, khử trùng lặp), giữ nguyên nội dung gốc
// Dùng khi lưu DB, không chuẩn hóa ngữ nghĩa
func (m *FAQChunkMetadata) Sanitize() {
	if m == nil {
		return
	}
	m.StandardQuestion = strings.TrimSpace(m.StandardQuestion)
	m.SimilarQuestions = SanitizeStrings(m.SimilarQuestions)
	m.NegativeQuestions = SanitizeStrings(m.NegativeQuestions)
	m.Answers = SanitizeStrings(m.Answers)
	if m.Version <= 0 {
		m.Version = 1
	}
}

// Normalize trả về bản sao đã chuẩn hóa, dùng để tính hash và lập chỉ mục vector
// Dữ liệu gốc không đổi, trả về bản sao chuẩn hóa mới
func (m *FAQChunkMetadata) Normalize() *FAQChunkMetadata {
	if m == nil {
		return nil
	}
	return &FAQChunkMetadata{
		StandardQuestion:  NormalizeQuestion(m.StandardQuestion),
		SimilarQuestions:  normalizeQuestionStrings(m.SimilarQuestions),
		NegativeQuestions: normalizeQuestionStrings(m.NegativeQuestions),
		Answers:           SanitizeStrings(m.Answers), // Câu trả lời chỉ làm sạch cơ bản
		AnswerStrategy:    m.AnswerStrategy,
		Version:           m.Version,
		Source:            m.Source,
	}
}

// SanitizeStrings làm sạch cơ bản danh sách chuỗi (TrimSpace + khử trùng lặp)
func SanitizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	dedup := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		dedup = append(dedup, trimmed)
	}
	if len(dedup) == 0 {
		return nil
	}
	return dedup
}

// FAQMetadata phân tích metadata FAQ trong chunk
// Trả về dữ liệu gốc (chỉ làm sạch cơ bản)
func (c *Chunk) FAQMetadata() (*FAQChunkMetadata, error) {
	if c == nil || len(c.Metadata) == 0 {
		return nil, nil
	}
	var meta FAQChunkMetadata
	if err := json.Unmarshal(c.Metadata, &meta); err != nil {
		return nil, err
	}
	meta.Sanitize() // Chỉ làm sạch cơ bản, giữ nội dung gốc
	return &meta, nil
}

// SetFAQMetadata đặt metadata FAQ cho chunk
// DB lưu dữ liệu gốc, ContentHash tính trên dữ liệu đã chuẩn hóa
func (c *Chunk) SetFAQMetadata(meta *FAQChunkMetadata) error {
	if c == nil {
		return nil
	}
	if meta == nil {
		c.Metadata = nil
		c.ContentHash = ""
		return nil
	}
	// Làm sạch cơ bản rồi lưu DB (giữ nội dung gốc)
	meta.Sanitize()
	bytes, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	c.Metadata = JSON(bytes)
	// ContentHash tính trên dữ liệu đã chuẩn hóa, dùng để so khớp khử trùng lặp
	normalized := meta.Normalize()
	c.ContentHash = CalculateFAQContentHash(normalized)
	return nil
}

// CalculateFAQContentHash tính giá trị hash của nội dung FAQ
// hash dựa trên: câu hỏi chuẩn + câu hỏi tương tự (đã sắp xếp) + phản ví dụ (đã sắp xếp) + câu trả lời (đã sắp xếp)
// Dùng để so khớp nhanh và khử trùng lặp
func CalculateFAQContentHash(meta *FAQChunkMetadata) string {
	if meta == nil {
		return ""
	}

	// Normalize() returns a new copy; the old code discarded the return value.
	normalized := meta.Normalize()
	if normalized == nil {
		return ""
	}

	// Sắp xếp mảng (đảm bảo nội dung giống nhau cho cùng hash)
	similarQuestions := make([]string, len(normalized.SimilarQuestions))
	copy(similarQuestions, normalized.SimilarQuestions)
	sort.Strings(similarQuestions)

	negativeQuestions := make([]string, len(normalized.NegativeQuestions))
	copy(negativeQuestions, normalized.NegativeQuestions)
	sort.Strings(negativeQuestions)

	answers := make([]string, len(normalized.Answers))
	copy(answers, normalized.Answers)
	sort.Strings(answers)

	// Dựng chuỗi để hash: câu hỏi chuẩn + câu hỏi tương tự + phản ví dụ + câu trả lời
	var builder strings.Builder
	builder.WriteString(normalized.StandardQuestion)
	builder.WriteString("|")
	builder.WriteString(strings.Join(similarQuestions, ","))
	builder.WriteString("|")
	builder.WriteString(strings.Join(negativeQuestions, ","))
	builder.WriteString("|")
	builder.WriteString(strings.Join(answers, ","))

	// Tính hash SHA256
	hash := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(hash[:])
}

// AnswerStrategy định nghĩa chiến lược trả về câu trả lời
type AnswerStrategy string

const (
	// AnswerStrategyAll trả về tất cả câu trả lời
	AnswerStrategyAll AnswerStrategy = "all"
	// AnswerStrategyRandom trả về ngẫu nhiên một câu trả lời
	AnswerStrategyRandom AnswerStrategy = "random"
)

// FAQEntry là mục FAQ trả về cho frontend
type FAQEntry struct {
	ID                int64          `json:"id"`
	ChunkID           string         `json:"chunk_id"`
	KnowledgeID       string         `json:"knowledge_id"`
	KnowledgeBaseID   string         `json:"knowledge_base_id"`
	TagID             int64          `json:"tag_id"`
	TagName           string         `json:"tag_name"`
	IsEnabled         bool           `json:"is_enabled"`
	IsRecommended     bool           `json:"is_recommended"`
	StandardQuestion  string         `json:"standard_question"`
	SimilarQuestions  []string       `json:"similar_questions"`
	NegativeQuestions []string       `json:"negative_questions"`
	Answers           []string       `json:"answers"`
	AnswerStrategy    AnswerStrategy `json:"answer_strategy"`
	IndexMode         FAQIndexMode   `json:"index_mode"`
	UpdatedAt         time.Time      `json:"updated_at"`
	CreatedAt         time.Time      `json:"created_at"`
	Score             float64        `json:"score,omitempty"`
	MatchType         MatchType      `json:"match_type,omitempty"`
	ChunkType         ChunkType      `json:"chunk_type"`
	// MatchedQuestion is the actual question text that was matched in FAQ search
	// Could be the standard question or one of the similar questions
	MatchedQuestion string `json:"matched_question,omitempty"`
}

// FAQExportEntry là mục FAQ xuất ra JSON, tương thích với định dạng nhập của FAQEntryPayload,
// thuận tiện cho vòng "xuất → sửa → nhập lại". Khi thêm trường mới phải giữ omitempty để không phá
// tương thích với các tệp xuất cũ.
type FAQExportEntry struct {
	ID                int64          `json:"id"`
	TagName           string         `json:"tag_name,omitempty"`
	StandardQuestion  string         `json:"standard_question"`
	SimilarQuestions  []string       `json:"similar_questions,omitempty"`
	NegativeQuestions []string       `json:"negative_questions,omitempty"`
	Answers           []string       `json:"answers,omitempty"`
	AnswerStrategy    AnswerStrategy `json:"answer_strategy,omitempty"`
	IsEnabled         bool           `json:"is_enabled"`
	IsRecommended     bool           `json:"is_recommended"`
}

// FAQEntryPayload là payload để tạo/cập nhật mục FAQ
type FAQEntryPayload struct {
	// ID tùy chọn, dùng để chỉ định seq_id khi di chuyển dữ liệu (phải nhỏ hơn giá trị tự tăng khởi đầu 100000000)
	ID                *int64          `json:"id,omitempty"`
	StandardQuestion  string          `json:"standard_question"    binding:"required"`
	SimilarQuestions  []string        `json:"similar_questions"`
	NegativeQuestions []string        `json:"negative_questions"`
	Answers           []string        `json:"answers"`
	AnswerStrategy    *AnswerStrategy `json:"answer_strategy,omitempty"`
	TagID             int64           `json:"tag_id"`
	TagName           string          `json:"tag_name"`
	IsEnabled         *bool           `json:"is_enabled,omitempty"`
	IsRecommended     *bool           `json:"is_recommended,omitempty"`
}

const (
	FAQBatchModeAppend  = "append"
	FAQBatchModeReplace = "replace"
)

// FAQBatchUpsertPayload nhập hàng loạt mục FAQ
type FAQBatchUpsertPayload struct {
	Entries     []FAQEntryPayload `json:"entries"      binding:"required"`
	Mode        string            `json:"mode"         binding:"oneof=append replace"`
	KnowledgeID string            `json:"knowledge_id"`
	TaskID      string            `json:"task_id"` // Tùy chọn, nếu không gửi sẽ tự sinh UUID
	DryRun      bool              `json:"dry_run"` // Chỉ kiểm tra, không nhập thật
}

// FAQFailedEntry là mục nhập/kiểm tra thất bại
type FAQFailedEntry struct {
	Index             int      `json:"index"`                        // Chỉ số của mục trong lô (bắt đầu từ 0)
	Reason            string   `json:"reason"`                       // Lý do thất bại
	FailureType       string   `json:"failure_type,omitempty"`       // Loại thất bại: pre_validation / post_validation
	IsPartialFailure  bool     `json:"is_partial_failure,omitempty"` // Có phải thất bại một phần (câu hỏi tương tự/phản ví dụ bị loại, nhưng cả mục vẫn nhập được)
	TagName           string   `json:"tag_name,omitempty"`           // Phân loại
	StandardQuestion  string   `json:"standard_question"`            // Câu hỏi chuẩn
	SimilarQuestions  []string `json:"similar_questions,omitempty"`  // Câu hỏi tương tự
	NegativeQuestions []string `json:"negative_questions,omitempty"` // Câu hỏi phản ví dụ
	Answers           []string `json:"answers,omitempty"`            // Câu trả lời
	AnswerAll         bool     `json:"answer_all,omitempty"`         // Trả lời tất cả hay không
	IsDisabled        bool     `json:"is_disabled,omitempty"`        // Đã vô hiệu hóa hay chưa
	// Chi tiết thất bại một phần (khi IsPartialFailure là true)
	RemovedSimilarQuestions  []string `json:"removed_similar_questions,omitempty"`  // Câu hỏi tương tự bị loại và lý do
	RemovedNegativeQuestions []string `json:"removed_negative_questions,omitempty"` // Phản ví dụ bị loại và lý do
}

// FAQMergeDetail là tóm tắt kết quả gộp một FAQ với chunk có sẵn ở chế độ append.
type FAQMergeDetail struct {
	Index            int    `json:"index"`              // Số dòng (chỉ số trong lô)
	StandardQuestion string `json:"standard_question"`  // Câu hỏi chuẩn
	AnswerChanged    bool   `json:"answer_changed"`     // Câu trả lời có thay đổi không
	NewSimilarCount  int    `json:"new_similar_count"`  // Số câu hỏi tương tự thêm mới
	NewNegativeCount int    `json:"new_negative_count"` // Số phản ví dụ thêm mới
}

// FAQSuccessEntry là thông tin tóm tắt của mục nhập thành công
type FAQSuccessEntry struct {
	Index            int    `json:"index"`              // Chỉ số của mục trong lô (bắt đầu từ 0)
	SeqID            int64  `json:"seq_id"`             // ID tuần tự của mục sau khi nhập
	TagID            int64  `json:"tag_id,omitempty"`   // ID phân loại (seq_id)
	TagName          string `json:"tag_name,omitempty"` // Tên phân loại
	StandardQuestion string `json:"standard_question"`  // Câu hỏi chuẩn
}

// FAQDryRunResult là kết quả kiểm tra ở chế độ dry_run
type FAQDryRunResult struct {
	TaskID        string           `json:"task_id,omitempty"` // ID tác vụ bất đồng bộ (trả về ở chế độ bất đồng bộ)
	Total         int              `json:"total"`             // Tổng số mục
	SuccessCount  int              `json:"success_count"`     // Số mục kiểm tra đạt
	FailedCount   int              `json:"failed_count"`      // Số mục kiểm tra không đạt
	FailedEntries []FAQFailedEntry `json:"failed_entries"`    // Chi tiết mục thất bại
}

// FAQSearchRequest là tham số yêu cầu tìm kiếm FAQ
type FAQSearchRequest struct {
	QueryText            string  `json:"query_text"             binding:"required"`
	VectorThreshold      float64 `json:"vector_threshold"`
	MatchCount           int     `json:"match_count"`
	FirstPriorityTagIDs  []int64 `json:"first_priority_tag_ids"`  // Danh sách ID thẻ ưu tiên 1, giới hạn phạm vi khớp, ưu tiên cao nhất
	SecondPriorityTagIDs []int64 `json:"second_priority_tag_ids"` // Danh sách ID thẻ ưu tiên 2, giới hạn phạm vi khớp, ưu tiên thấp hơn ưu tiên 1
	OnlyRecommended      bool    `json:"only_recommended"`        // Chỉ trả về mục được gợi ý hay không
}

// UntaggedTagName is the default tag name for entries without a tag
const UntaggedTagName = "未分类"

// FAQEntryFieldsUpdate là cập nhật trường của một mục FAQ
type FAQEntryFieldsUpdate struct {
	IsEnabled     *bool  `json:"is_enabled,omitempty"`
	IsRecommended *bool  `json:"is_recommended,omitempty"`
	TagID         *int64 `json:"tag_id,omitempty"`
	// Có thể mở rộng thêm trường sau này
}

// FAQEntryFieldsBatchUpdate là yêu cầu cập nhật hàng loạt trường của mục FAQ
// Hỗ trợ hai chế độ:
// 1. Cập nhật theo ID mục: dùng trường ByID
// 2. Cập nhật theo thẻ: dùng trường ByTag, áp dụng cùng cập nhật cho mọi mục thuộc thẻ
type FAQEntryFieldsBatchUpdate struct {
	// ByID cập nhật theo ID mục, key là ID mục (seq_id)
	ByID map[int64]FAQEntryFieldsUpdate `json:"by_id,omitempty"`
	// ByTag cập nhật hàng loạt theo thẻ, key là ID thẻ (seq_id)
	ByTag map[int64]FAQEntryFieldsUpdate `json:"by_tag,omitempty"`
	// ExcludeIDs là danh sách ID cần loại trừ trong thao tác ByTag (seq_id)
	ExcludeIDs []int64 `json:"exclude_ids,omitempty"`
}

// FAQImportTaskStatus là trạng thái tác vụ nhập
type FAQImportTaskStatus string

const (
	// FAQImportStatusPending represents the pending status of the FAQ import task
	FAQImportStatusPending FAQImportTaskStatus = "pending"
	// FAQImportStatusProcessing represents the processing status of the FAQ import task
	FAQImportStatusProcessing FAQImportTaskStatus = "processing"
	// FAQImportStatusCompleted represents the completed status of the FAQ import task
	FAQImportStatusCompleted FAQImportTaskStatus = "completed"
	// FAQImportStatusFailed represents the failed status of the FAQ import task
	FAQImportStatusFailed FAQImportTaskStatus = "failed"
)

// FAQImportProgress represents the progress of an FAQ import task stored in Redis
// When Status is "completed", the result fields (SkippedCount, ImportMode, ImportedAt, DisplayStatus, ProcessingTime) are populated.
type FAQImportProgress struct {
	TaskID             string              `json:"task_id"`                        // UUID for the import task
	KBID               string              `json:"kb_id"`                          // Knowledge Base ID
	KnowledgeID        string              `json:"knowledge_id"`                   // FAQ Knowledge ID
	Status             FAQImportTaskStatus `json:"status"`                         // Task status
	Progress           int                 `json:"progress"`                       // 0-100 percentage
	Total              int                 `json:"total"`                          // Total entries to import
	Processed          int                 `json:"processed"`                      // Entries processed so far
	SuccessCount       int                 `json:"success_count"`                  // Số mục thành công hoàn toàn (không gồm thành công/thất bại một phần)
	FailedCount        int                 `json:"failed_count"`                   // Số mục thất bại
	PartialFailedCount int                 `json:"partial_failed_count,omitempty"` // Số mục thất bại một phần (câu hỏi tương tự/phản ví dụ bị loại)
	SkippedCount       int                 `json:"skipped_count,omitempty"`        // Số mục bỏ qua (ví dụ trùng lặp)
	FailedEntries      []FAQFailedEntry    `json:"failed_entries,omitempty"`       // Chi tiết mục thất bại (trả về trực tiếp khi ít)
	FailedEntriesURL   string              `json:"failed_entries_url,omitempty"`   // URL tải CSV các mục thất bại (trả về URL khi nhiều)
	SuccessEntries     []FAQSuccessEntry   `json:"success_entries,omitempty"`      // Thông tin tóm tắt mục thành công (trả về trực tiếp khi ít)
	ValidEntryIndices  []int               `json:"valid_entry_indices,omitempty"`  // Chỉ số các mục kiểm tra đạt (để bỏ qua kiểm tra khi thử lại)
	MergeEntryIndices  []int               `json:"merge_entry_indices,omitempty"`  // Chỉ số các mục cần gộp (dùng nội bộ, để bỏ qua nhận diện khi thử lại)
	MergedCount        int                 `json:"merged_count,omitempty"`         // Số mục được gộp cập nhật
	AddedCount         int                 `json:"added_count,omitempty"`          // Số mục thêm mới
	MergeDetails       []FAQMergeDetail    `json:"merge_details,omitempty"`        // Chi tiết gộp
	Message            string              `json:"message"`                        // Status message
	Error              string              `json:"error"`                          // Error message if failed
	CreatedAt          int64               `json:"created_at"`                     // Task creation timestamp
	UpdatedAt          int64               `json:"updated_at"`                     // Last update timestamp
	DryRun             bool                `json:"dry_run,omitempty"`              // Có phải chế độ dry run

	// Result fields (populated when Status == "completed")
	ImportMode     string    `json:"import_mode,omitempty"`     // Chế độ nhập: append hoặc replace
	ImportedAt     time.Time `json:"imported_at,omitempty"`     // Thời điểm hoàn tất nhập
	DisplayStatus  string    `json:"display_status,omitempty"`  // Trạng thái hiển thị: open hoặc close
	ProcessingTime int64     `json:"processing_time,omitempty"` // Thời gian xử lý (mili giây)
}

// FAQImportMetadata là thông tin tác vụ nhập FAQ lưu trong Knowledge.Metadata
// Deprecated: Use FAQImportProgress with Redis storage instead
type FAQImportMetadata struct {
	ImportProgress  int `json:"import_progress"` // 0-100
	ImportTotal     int `json:"import_total"`
	ImportProcessed int `json:"import_processed"`
}

// FAQImportResult lưu kết quả thống kê sau khi nhập FAQ xong
// Thông tin này được lưu bền, không theo trạng thái tiến độ, đến lần nhập sau mới bị thay thế
type FAQImportResult struct {
	// Thống kê nhập
	TotalEntries       int `json:"total_entries"`        // Tổng số mục
	SuccessCount       int `json:"success_count"`        // Số mục thành công hoàn toàn (không gồm thành công/thất bại một phần)
	FailedCount        int `json:"failed_count"`         // Số mục thất bại hoàn toàn
	PartialFailedCount int `json:"partial_failed_count"` // Số mục thất bại một phần (câu hỏi tương tự/phản ví dụ bị loại nhưng đã nhập)
	SkippedCount       int `json:"skipped_count"`        // Số mục bỏ qua (ví dụ trùng lặp)
	MergedCount        int `json:"merged_count"`         // Số mục được gộp cập nhật
	AddedCount         int `json:"added_count"`          // Số mục thêm mới

	// Chế độ nhập và thông tin thời gian
	ImportMode string    `json:"import_mode"` // Chế độ nhập: append hoặc replace
	ImportedAt time.Time `json:"imported_at"` // Thời điểm hoàn tất nhập
	TaskID     string    `json:"task_id"`     // ID tác vụ nhập

	// URL chi tiết thất bại (cung cấp liên kết tải khi có nhiều mục thất bại)
	FailedEntriesURL string `json:"failed_entries_url,omitempty"` // URL tải CSV các mục thất bại

	// Điều khiển hiển thị
	DisplayStatus string `json:"display_status"` // Trạng thái hiển thị: open hoặc close

	// Thống kê bổ sung
	ProcessingTime int64 `json:"processing_time"` // Thời gian xử lý (mili giây)
}

// ToJSON converts the metadata to JSON type.
func (m *FAQImportMetadata) ToJSON() (JSON, error) {
	if m == nil {
		return nil, nil
	}
	bytes, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return JSON(bytes), nil
}

// ToJSON converts the import result to JSON type.
func (r *FAQImportResult) ToJSON() (JSON, error) {
	if r == nil {
		return nil, nil
	}
	bytes, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return JSON(bytes), nil
}

// ParseFAQImportMetadata parses FAQ import metadata from Knowledge.
func ParseFAQImportMetadata(k *Knowledge) (*FAQImportMetadata, error) {
	if k == nil || len(k.Metadata) == 0 {
		return nil, nil
	}
	var metadata FAQImportMetadata
	if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

// normalizeQuestionStrings chuẩn hóa danh sách câu hỏi
// Gồm chuyển full-width sang half-width, bỏ dấu câu cuối, gộp khoảng trắng... và khử trùng lặp
func normalizeQuestionStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	dedup := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		normalized := NormalizeQuestion(v)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		dedup = append(dedup, normalized)
	}
	if len(dedup) == 0 {
		return nil
	}
	return dedup
}

// multiSpaceRegex khớp nhiều ký tự trắng liên tiếp
var multiSpaceRegex = regexp.MustCompile(`\s+`)

// urlRegex khớp URL
var urlRegex = regexp.MustCompile(`https?://[^\s]+`)

// URLNormMode định nghĩa chế độ chuẩn hóa URL
type URLNormMode int

const (
	// URLRemove xóa hoàn toàn URL
	URLRemove URLNormMode = iota
	// URLPlaceholder thay bằng placeholder <URL>
	URLPlaceholder
	// URLKeepDomain chỉ giữ tên miền
	URLKeepDomain
	// URLKeepDomainAndPath giữ tên miền và đường dẫn
	URLKeepDomainAndPath
)

// NormalizeQuestion chuẩn hóa văn bản câu hỏi để tăng tỷ lệ khớp vector
// Thứ tự xử lý tham khảo: query = convert_st(trim_url(query.lower().strip().strip("？。，；、：""！?.,;!:'\"")), 1)
// 1. Bỏ khoảng trắng đầu cuối
// 2. Xóa URL
// 3. Chuyển chữ thường
// 4. Bỏ dấu câu đầu cuối
// 5. Chuyển chữ Hán phồn thể sang giản thể
// 6. Chuyển ký hiệu full-width sang half-width
// 7. Xử lý khoảng trắng thông minh (bỏ giữa chữ Hán, giữ giữa chữ Latin/số)
func NormalizeQuestion(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}

	// 1. Xóa URL
	q = trimURL(q)

	// 2. Chuyển chữ thường (có tác dụng với chữ Latin)
	q = strings.ToLower(q)

	// 3. Bỏ dấu câu đầu cuối
	q = strings.Trim(q, `？。，；、：""！?.,;!:'""`)

	// 4. Chuyển phồn thể sang giản thể
	q = toSimplified(q)

	// 5. Chuyển ký tự full-width sang half-width
	q = toHalfWidth(q)

	// 6. Xử lý khoảng trắng thông minh: bỏ giữa chữ Hán, giữ giữa chữ Latin/số
	q = normalizeSpaces(q)

	return strings.TrimSpace(q)
}

// normalizeSpaces xử lý khoảng trắng thông minh
// Quy tắc:
// - Bỏ khoảng trắng thừa trong ngữ cảnh chữ Hán
// - Giữ khoảng trắng cần thiết giữa chữ Latin/số
// Ví dụ:
// - "怎么 绑定 手机" → "怎么绑定手机"
// - "iphone 15 怎么 激活" → "iphone 15怎么激活"
func normalizeSpaces(s string) string {
	// Gộp nhiều khoảng trắng liên tiếp thành một trước
	s = multiSpaceRegex.ReplaceAllString(s, " ")

	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(s))

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Nếu không phải khoảng trắng, ghi thẳng
		if r != ' ' {
			builder.WriteRune(r)
			continue
		}

		// Xử lý khoảng trắng: xem ký tự trước/sau để quyết định giữ hay không
		// Lấy ký tự không trắng liền trước
		var prevRune rune
		if i > 0 {
			prevRune = runes[i-1]
		}

		// Lấy ký tự không trắng liền sau
		var nextRune rune
		for j := i + 1; j < len(runes); j++ {
			if runes[j] != ' ' {
				nextRune = runes[j]
				break
			}
		}

		// Chỉ giữ khoảng trắng khi cả trước và sau đều là chữ Latin hoặc số
		// Các trường hợp khác (gồm chữ Hán) đều bỏ khoảng trắng
		if isASCIIAlphaNum(prevRune) && isASCIIAlphaNum(nextRune) {
			builder.WriteRune(' ')
		}
		// Ngược lại bỏ qua khoảng trắng
	}

	return builder.String()
}

// isASCIIAlphaNum kiểm tra có phải chữ cái hoặc số ASCII
func isASCIIAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// trimURL xóa URL trong chuỗi (dùng chế độ URLRemove mặc định)
func trimURL(s string) string {
	return NormalizeURL(s, URLRemove)
}

// NormalizeURL xử lý URL trong văn bản theo chế độ chỉ định
func NormalizeURL(text string, mode URLNormMode) string {
	return urlRegex.ReplaceAllStringFunc(text, func(raw string) string {
		switch mode {
		case URLRemove:
			return ""
		case URLPlaceholder:
			return "<URL>"
		case URLKeepDomain:
			domain, _ := parseURL(raw)
			if domain != "" {
				return domain
			}
			return "<URL>"
		case URLKeepDomainAndPath:
			domain, path := parseURL(raw)
			if domain != "" {
				return domain + path
			}
			return "<URL>"
		default:
			return "<URL>"
		}
	})
}

// parseURL phân tích URL, trả về tên miền và đường dẫn
func parseURL(raw string) (domain, path string) {
	// Bỏ tiền tố giao thức
	u := raw
	if strings.HasPrefix(u, "https://") {
		u = u[8:]
	} else if strings.HasPrefix(u, "http://") {
		u = u[7:]
	}

	// Tách tên miền và đường dẫn
	slashIdx := strings.Index(u, "/")
	if slashIdx == -1 {
		// Không có đường dẫn, toàn bộ là tên miền (có thể kèm query parameters)
		queryIdx := strings.Index(u, "?")
		if queryIdx != -1 {
			domain = u[:queryIdx]
		} else {
			domain = u
		}
		return domain, ""
	}

	domain = u[:slashIdx]
	path = u[slashIdx:]

	// Bỏ query parameters và fragment
	if queryIdx := strings.Index(path, "?"); queryIdx != -1 {
		path = path[:queryIdx]
	}
	if fragIdx := strings.Index(path, "#"); fragIdx != -1 {
		path = path[:fragIdx]
	}

	return domain, path
}

// toSimplified chuyển chữ Hán phồn thể sang giản thể
func toSimplified(s string) string {
	return textconv.ToSimplified(s)
}

// toHalfWidth chuyển ký tự full-width sang half-width
// Chủ yếu xử lý: khoảng trắng full-width, ký tự ASCII full-width (gồm dấu câu)
func toHalfWidth(s string) string {
	var builder strings.Builder
	builder.Grow(len(s))

	for _, r := range s {
		switch {
		// Khoảng trắng full-width -> half-width
		case r == '\u3000':
			builder.WriteRune(' ')
		// Ký tự ASCII full-width (khoảng 0xFF01-0xFF5E) -> half-width (0x0021-0x007E)
		case r >= 0xFF01 && r <= 0xFF5E:
			builder.WriteRune(r - 0xFF01 + 0x21)
		// Ký tự khác giữ nguyên
		default:
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

// NormalizeQueryText chuẩn hóa văn bản truy vấn tìm kiếm
// Cùng logic với NormalizeQuestion, dùng để chuẩn hóa văn bản truy vấn khi tìm kiếm
func NormalizeQueryText(q string) string {
	return NormalizeQuestion(q)
}

// IsChineseChar kiểm tra có phải chữ Hán
func IsChineseChar(r rune) bool {
	return unicode.Is(unicode.Han, r)
}
