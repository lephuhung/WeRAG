package types

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// WebSearchConfig represents the web search configuration for a tenant
type WebSearchConfig struct {
	// Deprecated: Use WebSearchProviderEntity.Parameters.APIKey instead.
	Provider string `json:"provider,omitempty"`
	// Deprecated: Use WebSearchProviderEntity.Parameters.APIKey instead.
	APIKey string `json:"api_key,omitempty"`

	// Per-call Agent filters; never persisted in tenant configuration.
	Filters WebSearchFilters `json:"-"`

	MaxResults        int      `json:"max_results"`        // Số kết quả tìm kiếm tối đa
	IncludeDate       bool     `json:"include_date"`       // Có kèm ngày hay không
	CompressionMethod string   `json:"compression_method"` // Phương pháp nén: none, summary, extract, rag
	Blacklist         []string `json:"blacklist"`          // Danh sách quy tắc chặn
	// Cấu hình liên quan nén RAG
	EmbeddingModelID   string `json:"embedding_model_id,omitempty"`  // ID mô hình embedding (dùng cho nén RAG)
	EmbeddingDimension int    `json:"embedding_dimension,omitempty"` // Số chiều embedding (dùng cho nén RAG)
	RerankModelID      string `json:"rerank_model_id,omitempty"`     // ID mô hình rerank (dùng cho nén RAG)
	DocumentFragments  int    `json:"document_fragments,omitempty"`  // Số đoạn tài liệu (dùng cho nén RAG)
	ProxyURL           string `json:"proxy_url,omitempty"`           // Optional per-request proxy override; normally empty — use WebSearchProviderEntity.Parameters.proxy_url. Merged at call time when set.
}

const (
	DefaultWebSearchMaxResults        = 10
	DefaultWebSearchCompressionMethod = "none"
)

// DefaultWebSearchConfig returns the shared default tenant-level web search configuration.
func DefaultWebSearchConfig() *WebSearchConfig {
	return &WebSearchConfig{
		MaxResults:        DefaultWebSearchMaxResults,
		IncludeDate:       false,
		CompressionMethod: DefaultWebSearchCompressionMethod,
		Blacklist:         []string{},
	}
}

// EffectiveWebSearchConfig normalizes a possibly empty config to the effective runtime config.
func EffectiveWebSearchConfig(cfg *WebSearchConfig) *WebSearchConfig {
	if cfg == nil {
		return DefaultWebSearchConfig()
	}

	normalized := *cfg
	if normalized.MaxResults <= 0 {
		normalized.MaxResults = DefaultWebSearchMaxResults
	}
	if normalized.CompressionMethod == "" {
		normalized.CompressionMethod = DefaultWebSearchCompressionMethod
	}
	if normalized.Blacklist == nil {
		normalized.Blacklist = []string{}
	}

	return &normalized
}

// Value implements driver.Valuer interface for WebSearchConfig
func (c WebSearchConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner interface for WebSearchConfig
func (c *WebSearchConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// WebSearchResult represents a single web search result
type WebSearchResult struct {
	Title   string `json:"title"`   // Tiêu đề kết quả tìm kiếm
	URL     string `json:"url"`     // URL kết quả
	Snippet string `json:"snippet"` // Đoạn trích tóm tắt
	Content string `json:"content"` // Nội dung đầy đủ (tùy chọn, cần tải thêm)
	Source  string `json:"source"`  // Nguồn (ví dụ: duckduckgo)
	// Provider-reported age, without inventing an exact publication date.
	Age string `json:"age,omitempty"`

	PublishedAt *time.Time `json:"published_at,omitempty"` // Thời gian xuất bản (nếu có)
}

// WebSearchProviderInfo represents information about a web search provider
type WebSearchProviderInfo struct {
	ID             string `json:"id"`                // ID nhà cung cấp
	Name           string `json:"name"`              // Tên nhà cung cấp
	Free           bool   `json:"free"`              // Miễn phí hay không
	RequiresAPIKey bool   `json:"requires_api_key"`  // Có cần API key hay không
	Description    string `json:"description"`       // Mô tả
	APIURL         string `json:"api_url,omitempty"` // Địa chỉ API (tùy chọn)
}
