// Package provider defines the unified interface and registry for multi-vendor model API adapters.
package provider

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

// ProviderName tên nhà cung cấp mô hình
type ProviderName string

const (
	// OpenAI
	ProviderOpenAI ProviderName = "openai"
	// Anthropic Claude
	ProviderAnthropic ProviderName = "anthropic"
	// Alibaba Cloud DashScope
	ProviderAliyun ProviderName = "aliyun"
	// Zhipu AI (dòng GLM)
	ProviderZhipu ProviderName = "zhipu"
	// OpenRouter
	ProviderOpenRouter ProviderName = "openrouter"
	// ProviderLiteLLM is the LiteLLM self-hosted proxy (OpenAI-compatible gateway to 100+ providers).
	ProviderLiteLLM ProviderName = "litellm"
	// Requesty
	ProviderRequesty ProviderName = "requesty"
	// SiliconFlow
	ProviderSiliconFlow ProviderName = "siliconflow"
	// Jina AI (Embedding and Rerank)
	ProviderJina ProviderName = "jina"
	// Generic tương thích OpenAI (tự triển khai)
	ProviderGeneric ProviderName = "generic"
	// DeepSeek
	ProviderDeepSeek ProviderName = "deepseek"
	// Google Gemini
	ProviderGemini ProviderName = "gemini"
	// Volcengine Ark
	ProviderVolcengine ProviderName = "volcengine"
	// Tencent Hunyuan
	ProviderHunyuan ProviderName = "hunyuan"
	// MiniMax
	ProviderMiniMax ProviderName = "minimax"
	// Xiaomi Mimo
	ProviderMimo ProviderName = "mimo"
	// GPUStack (triển khai riêng)
	ProviderGPUStack ProviderName = "gpustack"
	// Moonshot (Kimi)
	ProviderMoonshot ProviderName = "moonshot"
	// ModelScope
	ProviderModelScope ProviderName = "modelscope"
	// Baidu Qianfan
	ProviderQianfan ProviderName = "qianfan"
	// Qiniu Cloud
	ProviderQiniu ProviderName = "qiniu"
	// Meituan LongCat AI
	ProviderLongCat ProviderName = "longcat"
	// Tencent Cloud LKEAP
	ProviderLKEAP ProviderName = "lkeap"
	// NVIDIA
	ProviderNvidia ProviderName = "nvidia"
	// Novita AI
	ProviderNovita ProviderName = "novita"
	// Azure OpenAI
	ProviderAzureOpenAI ProviderName = "azure_openai"
	// TypeSafe Jev (decision model)
	ProviderJev ProviderName = "jev"
	// Cloudflare Clef (Jev-compatible decision model on Workers AI)
	ProviderClef ProviderName = "clef"
)

// AllProviders trả về tên mọi provider đã đăng ký
func AllProviders() []ProviderName {
	return []ProviderName{
		ProviderGeneric,
		ProviderWeKnoraCloud,
		ProviderAliyun,
		ProviderZhipu,
		ProviderVolcengine,
		ProviderHunyuan,
		ProviderSiliconFlow,
		ProviderDeepSeek,
		ProviderMiniMax,
		ProviderMoonshot,
		ProviderModelScope,
		ProviderQianfan,
		ProviderQiniu,
		ProviderOpenAI,
		ProviderAnthropic,
		ProviderGemini,
		ProviderOpenRouter,
		ProviderLiteLLM,
		ProviderRequesty,
		ProviderJina,
		ProviderMimo,
		ProviderLongCat,
		ProviderLKEAP,
		ProviderGPUStack,
		ProviderNvidia,
		ProviderNovita,
		ProviderAzureOpenAI,
		ProviderJev,
		ProviderClef,
	}
}

// ProviderInfo chứa metadata của provider
type ProviderInfo struct {
	Name         ProviderName               // định danh provider
	DisplayName  string                     // tên hiển thị
	Description  string                     // mô tả provider
	DefaultURLs  map[types.ModelType]string // BaseURL mặc định theo loại mô hình
	ModelTypes   []types.ModelType          // các loại mô hình hỗ trợ
	RequiresAuth bool                       // có cần API key không
	ExtraFields  []ExtraFieldConfig         // các trường cấu hình bổ sung
}

// GetDefaultURL lấy URL mặc định cho loại mô hình chỉ định
func (p ProviderInfo) GetDefaultURL(modelType types.ModelType) string {
	if url, ok := p.DefaultURLs[modelType]; ok {
		return url
	}
	// dự phòng về Chat URL
	if url, ok := p.DefaultURLs[types.ModelTypeKnowledgeQA]; ok {
		return url
	}
	return ""
}

// ExtraFieldConfig định nghĩa trường cấu hình bổ sung của provider
type ExtraFieldConfig struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"` // "string", "number", "boolean", "select"
	Required    bool   `json:"required"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder"`
	Options     []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"options,omitempty"`
}

// Config là cấu hình của provider mô hình
type Config struct {
	Provider  ProviderName   `json:"provider"`
	BaseURL   string         `json:"base_url"`
	APIKey    string         `json:"api_key"`
	ModelName string         `json:"model_name"`
	ModelID   string         `json:"model_id"`
	Extra     map[string]any `json:"extra,omitempty"`
}

type Provider interface {
	// Info trả về metadata của nhà cung cấp
	Info() ProviderInfo

	// ValidateConfig kiểm tra cấu hình của nhà cung cấp
	ValidateConfig(config *Config) error
}

// registry lưu mọi provider đã đăng ký
var (
	registryMu sync.RWMutex
	registry   = make(map[ProviderName]Provider)
)

// Register thêm một provider vào registry toàn cục
func Register(p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.Info().Name] = p
}

// Get lấy provider từ registry theo tên
func Get(name ProviderName) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[name]
	return p, ok
}

// GetOrDefault lấy provider theo tên, không có thì trả về provider mặc định
func GetOrDefault(name ProviderName) Provider {
	p, ok := Get(name)
	if ok {
		return p
	}
	// không tìm thấy thì trả về provider mặc định
	p, _ = Get(ProviderGeneric)
	return p
}

// List trả về mọi provider đã đăng ký (theo thứ tự AllProviders)
func List() []ProviderInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()

	result := make([]ProviderInfo, 0, len(registry))
	for _, name := range AllProviders() {
		if p, ok := registry[name]; ok {
			result = append(result, p.Info())
		}
	}
	return result
}

// ListByModelType trả về các provider hỗ trợ loại mô hình chỉ định (theo thứ tự AllProviders)
func ListByModelType(modelType types.ModelType) []ProviderInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()

	result := make([]ProviderInfo, 0)
	for _, name := range AllProviders() {
		if p, ok := registry[name]; ok {
			info := p.Info()
			for _, t := range info.ModelTypes {
				if t == modelType {
					result = append(result, info)
					break
				}
			}
		}
	}
	return result
}

// DetectProvider nhận diện nhà cung cấp qua BaseURL
func DetectProvider(baseURL string) ProviderName {
	switch {
	case containsAny(baseURL, "dashscope.aliyuncs.com"):
		return ProviderAliyun
	case containsAny(baseURL, "open.bigmodel.cn", "zhipu"):
		return ProviderZhipu
	case containsAny(baseURL, "openrouter.ai"):
		return ProviderOpenRouter
	// Hostname/path containing "litellm" (including the catalog placeholder
	// your_litellm_proxy). Loopback URLs such as localhost:4000 stay generic
	// because they are SSRF-blocked unless explicitly whitelisted.
	case containsAny(baseURL, "litellm"):
		return ProviderLiteLLM
	case containsAny(baseURL, "router.requesty.ai", "requesty.ai"):
		return ProviderRequesty
	case containsAny(baseURL, "siliconflow.cn"):
		return ProviderSiliconFlow
	case containsAny(baseURL, "api.jina.ai"):
		return ProviderJina
	case containsAny(baseURL, "openai.azure.com"):
		return ProviderAzureOpenAI
	case containsAny(baseURL, "api.openai.com"):
		return ProviderOpenAI
	case containsAny(baseURL, "api.anthropic.com"):
		return ProviderAnthropic
	case containsAny(baseURL, "api.deepseek.com"):
		return ProviderDeepSeek
	case containsAny(baseURL, "generativelanguage.googleapis.com"):
		return ProviderGemini
	case containsAny(baseURL, "volces.com", "volcengine"):
		return ProviderVolcengine
	case containsAny(baseURL, "hunyuan.cloud.tencent.com"):
		return ProviderHunyuan
	case containsAny(baseURL, "minimax.io", "minimaxi.com"):
		return ProviderMiniMax
	case containsAny(baseURL, "xiaomimimo.com"):
		return ProviderMimo
	case containsAny(baseURL, "gpustack"):
		return ProviderGPUStack
	case containsAny(baseURL, "modelscope.cn"):
		return ProviderModelScope
	case containsAny(baseURL, "qiniuapi.com", "qiniu"):
		return ProviderQiniu
	case containsAny(baseURL, "moonshot.ai"):
		return ProviderMoonshot
	case containsAny(baseURL, "qianfan.baidubce.com", "baidubce.com"):
		return ProviderQianfan
	case containsAny(baseURL, "longcat.chat"):
		return ProviderLongCat
	case containsAny(baseURL, "lkeap.cloud.tencent.com", "api.lkeap", "lkeap.tencentcloudapi.com"):
		return ProviderLKEAP
	case containsAny(baseURL, "nvidia.com"):
		return ProviderNvidia
	case containsAny(baseURL, "api.novita.ai", "novita.ai"):
		return ProviderNovita
	case containsAny(baseURL, "weknora.weixin.qq.com"):
		return ProviderWeKnoraCloud
	default:
		return ProviderGeneric
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func NewConfigFromModel(model *types.Model) (*Config, error) {
	if model == nil {
		return nil, fmt.Errorf("model is nil")
	}

	providerName := ProviderName(model.Parameters.Provider)
	if providerName == "" {
		providerName = DetectProvider(model.Parameters.BaseURL)
	}

	return &Config{
		Provider:  providerName,
		BaseURL:   model.Parameters.BaseURL,
		APIKey:    model.Parameters.APIKey,
		ModelName: model.Name,
		ModelID:   model.ID,
	}, nil
}
