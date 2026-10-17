// Package types defines data structures and types used throughout the system
// These types are shared across different service modules to ensure data consistency
package types

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// ChunkType định nghĩa các loại đoạn (chunk)
type ChunkType = string

const (
	// ChunkTypeText là chunk văn bản thường
	ChunkTypeText ChunkType = "text"
	// ChunkTypeParentText là chunk văn bản cha trong chiến lược cha-con (chỉ dùng làm ngữ cảnh, không đưa vào chỉ mục vector)
	ChunkTypeParentText ChunkType = "parent_text"
	// ChunkTypeImageOCR là chunk văn bản OCR của hình ảnh
	ChunkTypeImageOCR ChunkType = "image_ocr"
	// ChunkTypeImageCaption là chunk mô tả hình ảnh
	ChunkTypeImageCaption ChunkType = "image_caption"
	// ChunkTypeSummary là chunk tóm tắt
	ChunkTypeSummary = "summary"
	// ChunkTypeEntity là chunk thực thể
	ChunkTypeEntity ChunkType = "entity"
	// ChunkTypeRelationship là chunk quan hệ
	ChunkTypeRelationship ChunkType = "relationship"
	// ChunkTypeFAQ là chunk mục FAQ
	ChunkTypeFAQ ChunkType = "faq"
	// ChunkTypeWebSearch là chunk kết quả tìm kiếm web
	ChunkTypeWebSearch ChunkType = "web_search"
	// ChunkTypeTableSummary là chunk tóm tắt bảng dữ liệu
	ChunkTypeTableSummary ChunkType = "table_summary"
	// ChunkTypeTableColumn là chunk mô tả cột của bảng dữ liệu
	ChunkTypeTableColumn ChunkType = "table_column"
	// ChunkTypeWikiPage là chunk đồng bộ từ trang Wiki, để đưa trang wiki vào pipeline truy xuất hiện có
	ChunkTypeWikiPage ChunkType = "wiki_page"
)

// ChunkStatus định nghĩa các trạng thái của chunk
type ChunkStatus int

const (
	ChunkStatusDefault ChunkStatus = 0
	// ChunkStatusStored: chunk đã được lưu
	ChunkStatusStored ChunkStatus = 1
	// ChunkStatusIndexed: chunk đã được lập chỉ mục
	ChunkStatusIndexed ChunkStatus = 2
)

// ChunkFlags định nghĩa các bit cờ của chunk, dùng để quản lý nhiều trạng thái boolean
type ChunkFlags int

const (
	// ChunkFlagRecommended: trạng thái có thể gợi ý (1 << 0 = 1)
	// Khi đặt cờ này, chunk có thể được gợi ý cho người dùng
	ChunkFlagRecommended ChunkFlags = 1 << 0
	// Có thể mở rộng thêm cờ trong tương lai:
	// ChunkFlagPinned ChunkFlags = 1 << 1  // ghim
	// ChunkFlagHot    ChunkFlags = 1 << 2  // nổi bật
)

// HasFlag kiểm tra cờ chỉ định đã được đặt chưa
func (f ChunkFlags) HasFlag(flag ChunkFlags) bool {
	return f&flag != 0
}

// SetFlag đặt cờ chỉ định
func (f ChunkFlags) SetFlag(flag ChunkFlags) ChunkFlags {
	return f | flag
}

// ClearFlag xóa cờ chỉ định
func (f ChunkFlags) ClearFlag(flag ChunkFlags) ChunkFlags {
	return f &^ flag
}

// ToggleFlag đảo cờ chỉ định
func (f ChunkFlags) ToggleFlag(flag ChunkFlags) ChunkFlags {
	return f ^ flag
}

// ImageInfo là thông tin hình ảnh gắn với chunk
type ImageInfo struct {
	// URL hình ảnh (COS)
	URL string `json:"url"          gorm:"type:text"`
	// URL hình ảnh gốc
	OriginalURL string `json:"original_url" gorm:"type:text"`
	// Vị trí bắt đầu của hình ảnh trong văn bản
	StartPos int `json:"start_pos"`
	// Vị trí kết thúc của hình ảnh trong văn bản
	EndPos int `json:"end_pos"`
	// Mô tả hình ảnh
	Caption string `json:"caption"`
	// Văn bản OCR của hình ảnh
	OCRText string `json:"ocr_text"`
}

// VideoInfo là thông tin video gắn với chunk
type VideoInfo struct {
	// URL video
	URL string `json:"url"          gorm:"type:text"`
}

// Chunk represents a document chunk
// Chunks are meaningful text segments extracted from original documents
// and are the basic units of knowledge base retrieval
// Each chunk contains a portion of the original content
// and maintains its positional relationship with the original text
// Chunks can be independently embedded as vectors and retrieved, supporting precise content localization
type Chunk struct {
	// Unique identifier of the chunk, using UUID format
	ID string `json:"id"                       gorm:"type:varchar(36);primaryKey"`
	// SeqID is an auto-increment integer ID for external API usage (FAQ entries)
	SeqID int64 `json:"seq_id"                   gorm:"type:bigint;uniqueIndex;autoIncrement"`
	// Tenant ID, used for multi-tenant isolation
	TenantID uint64 `json:"tenant_id"`
	// ID of the parent knowledge, associated with the Knowledge model
	KnowledgeID string `json:"knowledge_id"`
	// ID of the knowledge base, for quick location
	KnowledgeBaseID string `json:"knowledge_base_id"`
	// Optional tag ID for categorization within a knowledge base (used for FAQ)
	TagID string `json:"tag_id"                   gorm:"type:varchar(36);index"`
	// Actual text content of the chunk
	Content string `json:"content"`
	// SourceContent is the immutable parser output. Legacy rows are lazily
	// backfilled from Content on the first manual edit.
	SourceContent string `json:"-"`
	// ContentRevision is incremented for every user edit or rollback.
	ContentRevision int `json:"content_revision" gorm:"not null;default:0"`
	// IndexStatus reports whether the current content is reflected in the
	// retrieval stores: ready | processing | failed.
	IndexStatus string `json:"index_status" gorm:"type:varchar(16);not null;default:'ready'"`
	// LastEditorID records the actor that produced the current revision.
	LastEditorID string `json:"last_editor_id" gorm:"type:varchar(64);not null;default:''"`
	// Index position of the chunk in the original document
	ChunkIndex int `json:"chunk_index"`
	// Whether the chunk is enabled, can be used to temporarily disable certain chunks
	IsEnabled bool `json:"is_enabled"               gorm:"default:true"`
	// Flags lưu các bit cờ cho nhiều trạng thái boolean (ví dụ trạng thái gợi ý)
	// Mặc định là ChunkFlagRecommended (1), tức mặc định có thể gợi ý
	Flags ChunkFlags `json:"flags"                    gorm:"default:1"`
	// Status of the chunk
	Status int `json:"status"                   gorm:"default:0"`
	// Starting character position in the original text
	StartAt int `json:"start_at"`
	// Ending character position in the original text
	EndAt int `json:"end_at"`
	// Previous chunk ID
	PreChunkID string `json:"pre_chunk_id"`
	// Next chunk ID
	NextChunkID string `json:"next_chunk_id"`
	// Loại chunk, để phân biệt các loại chunk khác nhau
	ChunkType ChunkType `json:"chunk_type"               gorm:"type:varchar(20);default:'text'"`
	// ID chunk cha, liên kết chunk hình ảnh với chunk văn bản gốc
	ParentChunkID string `json:"parent_chunk_id"          gorm:"type:varchar(36);index"`
	// ID chunk quan hệ, liên kết chunk quan hệ với chunk văn bản gốc
	RelationChunks JSON `json:"relation_chunks"          gorm:"type:json"`
	// ID chunk quan hệ gián tiếp, liên kết chunk quan hệ gián tiếp với chunk văn bản gốc
	IndirectRelationChunks JSON `json:"indirect_relation_chunks" gorm:"type:json"`
	// Metadata lưu thông tin mở rộng ở cấp chunk, ví dụ metadata FAQ
	Metadata JSON `json:"metadata"                 gorm:"type:json"`
	// ContentHash lưu giá trị hash của nội dung, dùng để so khớp nhanh (chủ yếu cho FAQ)
	ContentHash string `json:"content_hash"             gorm:"type:varchar(64)"`
	// Thông tin hình ảnh, lưu dưới dạng JSON
	ImageInfo string `json:"image_info"               gorm:"type:text"`
	// Chunk creation time
	CreatedAt time.Time `json:"created_at"`
	// Chunk last update time
	UpdatedAt time.Time `json:"updated_at"`
	// Soft delete marker, supports data recovery
	DeletedAt gorm.DeletedAt `json:"deleted_at"               gorm:"index"`
	// ContextHeader is a Markdown heading breadcrumb prepended when indexing.
	// It is persisted so a later content edit can rebuild the same index input.
	ContextHeader string `json:"-" gorm:"type:text"`
}

// ChunkRevision is an immutable snapshot of a superseded chunk revision.
// The current content lives on Chunk; this table stores prior versions.
type ChunkRevision struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64    `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	KnowledgeID     string    `json:"knowledge_id" gorm:"type:varchar(36);index"`
	ChunkID         string    `json:"chunk_id" gorm:"type:varchar(36);uniqueIndex:idx_chunk_revision"`
	Revision        int       `json:"revision" gorm:"uniqueIndex:idx_chunk_revision"`
	Content         string    `json:"content" gorm:"type:text"`
	IsEnabled       bool      `json:"is_enabled"`
	EditorID        string    `json:"editor_id" gorm:"type:varchar(64)"`
	EditSource      string    `json:"edit_source" gorm:"type:varchar(16)"`
	EditedAt        time.Time `json:"edited_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// EmbeddingContent returns the chunk content with ContextHeader prepended
// when set. Use this where the embedding model needs section context that
// isn't part of the literal Content. Surrounding whitespace on Content is
// trimmed so leading/trailing newlines from boundary slicing don't dilute
// the embedded vector.
func (c *Chunk) EmbeddingContent() string {
	if c == nil {
		return ""
	}
	body := strings.TrimSpace(c.Content)
	if c.ContextHeader == "" {
		return body
	}
	return c.ContextHeader + "\n\n" + body
}

// AssignChunkSeqIDs assigns sequential SeqIDs to a batch of chunks that have SeqID == 0.
// Must be called before CreateInBatches for SQLite compatibility.
func AssignChunkSeqIDs(tx *gorm.DB, chunks []*Chunk) error {
	needAssign := false
	for _, c := range chunks {
		if c.SeqID == 0 {
			needAssign = true
			break
		}
	}
	if !needAssign {
		return nil
	}

	var maxSeqID *int64
	if err := tx.Unscoped().Model(&Chunk{}).Select("MAX(seq_id)").Scan(&maxSeqID).Error; err != nil {
		return err
	}
	next := int64(1)
	if maxSeqID != nil {
		next = *maxSeqID + 1
	}
	for _, c := range chunks {
		if c.SeqID == 0 {
			c.SeqID = next
			next++
		}
	}
	return nil
}
