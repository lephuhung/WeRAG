package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/assets"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler/dto"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/models/asr"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ollama/ollama/api"
)

// DownloadTask download task information
type DownloadTask struct {
	ID        string     `json:"id"`
	ModelName string     `json:"modelName"`
	Status    string     `json:"status"` // pending, downloading, completed, failed
	Progress  float64    `json:"progress"`
	Message   string     `json:"message"`
	StartTime time.Time  `json:"startTime"`
	EndTime   *time.Time `json:"endTime,omitempty"`
}

// Global download task manager
var (
	downloadTasks = make(map[string]*DownloadTask)
	tasksMutex    sync.RWMutex
)

// InitializationHandler handles initialization endpoints
type InitializationHandler struct {
	config           *config.Config
	tenantService    interfaces.TenantService
	modelService     interfaces.ModelService
	kbService        interfaces.KnowledgeBaseService
	kbRepository     interfaces.KnowledgeBaseRepository
	knowledgeService interfaces.KnowledgeService
	ollamaService    *ollama.OllamaService
	documentReader   interfaces.DocumentReader
	pooler           embedding.EmbedderPooler
	storageResolver  interfaces.StorageBackendResolver
}

// NewInitializationHandler creates the initialization handler
func NewInitializationHandler(
	config *config.Config,
	tenantService interfaces.TenantService,
	modelService interfaces.ModelService,
	kbService interfaces.KnowledgeBaseService,
	kbRepository interfaces.KnowledgeBaseRepository,
	knowledgeService interfaces.KnowledgeService,
	ollamaService *ollama.OllamaService,
	documentReader interfaces.DocumentReader,
	pooler embedding.EmbedderPooler,
	storageResolver interfaces.StorageBackendResolver,
) *InitializationHandler {
	return &InitializationHandler{
		config:           config,
		tenantService:    tenantService,
		modelService:     modelService,
		kbService:        kbService,
		kbRepository:     kbRepository,
		knowledgeService: knowledgeService,
		ollamaService:    ollamaService,
		documentReader:   documentReader,
		pooler:           pooler,
		storageResolver:  storageResolver,
	}
}

// KBModelConfigRequest is the knowledge-base model config request (simplified: model IDs only)
type KBModelConfigRequest struct {
	LLMModelID       string           `json:"llmModelId"       binding:"required"`
	EmbeddingModelID string           `json:"embeddingModelId"` // optional when RAG indexing is disabled
	VLMConfig        *types.VLMConfig `json:"vlm_config"`
	ASRConfig        *types.ASRConfig `json:"asr_config"`

	// Document chunking config
	DocumentSplitting struct {
		ChunkSize         int                      `json:"chunkSize"`
		ChunkOverlap      int                      `json:"chunkOverlap"`
		Separators        []string                 `json:"separators"`
		ParserEngineRules []types.ParserEngineRule `json:"parserEngineRules,omitempty"`
		EnableParentChild bool                     `json:"enableParentChild"`
		ParentChunkSize   int                      `json:"parentChunkSize,omitempty"`
		ChildChunkSize    int                      `json:"childChunkSize,omitempty"`
		// Strategy / TokenLimit / Languages use pointer types so the
		// handler can distinguish "field absent in payload" (no change)
		// from "field present with empty/zero value" (clear / disable).
		// Without that distinction, users could set strategy="auto" once
		// but never reset it back to legacy / unset.
		Strategy                  *string   `json:"strategy,omitempty"`
		TokenLimit                *int      `json:"tokenLimit,omitempty"`
		Languages                 *[]string `json:"languages,omitempty"`
		TableMetadataInstructions *string   `json:"tableMetadataInstructions,omitempty"`
	} `json:"documentSplitting"`

	// Multimodal config (model only; the storage engine is set in storageProvider)
	Multimodal struct {
		Enabled bool `json:"enabled"`
	} `json:"multimodal"`

	// Storage engine ("local" | "minio" | "cos") for document uploads and in-document images; parameters come from global settings
	StorageProvider  string `json:"storageProvider"`
	StorageBackendID string `json:"storageBackendId"`

	// Knowledge graph config
	NodeExtract struct {
		Enabled            bool                  `json:"enabled"`
		Text               string                `json:"text"`
		Tags               []string              `json:"tags"`
		Nodes              []types.GraphNode     `json:"nodes"`
		Relations          []types.GraphRelation `json:"relations"`
		CustomInstructions string                `json:"customInstructions"`
	} `json:"nodeExtract"`

	// Question generation config
	QuestionGeneration struct {
		Enabled            bool   `json:"enabled"`
		QuestionCount      int    `json:"questionCount"`
		CustomInstructions string `json:"customInstructions"`
	} `json:"questionGeneration"`
}

// InitializationRequest is the initialization request body
type InitializationRequest struct {
	LLM struct {
		Source    string `json:"source" binding:"required"`
		ModelName string `json:"modelName" binding:"required"`
		BaseURL   string `json:"baseUrl"`
		APIKey    string `json:"apiKey"`
	} `json:"llm" binding:"required"`

	Embedding struct {
		Source    string `json:"source" binding:"required"`
		ModelName string `json:"modelName" binding:"required"`
		BaseURL   string `json:"baseUrl"`
		APIKey    string `json:"apiKey"`
		Dimension int    `json:"dimension"` // embedding dimension
	} `json:"embedding" binding:"required"`

	Rerank struct {
		Enabled   bool   `json:"enabled"`
		ModelName string `json:"modelName"`
		BaseURL   string `json:"baseUrl"`
		APIKey    string `json:"apiKey"`
	} `json:"rerank"`

	Multimodal struct {
		Enabled bool `json:"enabled"`
		VLM     *struct {
			ModelName     string `json:"modelName"`
			BaseURL       string `json:"baseUrl"`
			APIKey        string `json:"apiKey"`
			InterfaceType string `json:"interfaceType"` // "ollama" or "openai"
		} `json:"vlm,omitempty"`
		StorageType string `json:"storageType"`
		COS         *struct {
			SecretID   string `json:"secretId"`
			SecretKey  string `json:"secretKey"`
			Region     string `json:"region"`
			BucketName string `json:"bucketName"`
			AppID      string `json:"appId"`
			PathPrefix string `json:"pathPrefix"`
		} `json:"cos,omitempty"`
		Minio *struct {
			BucketName string `json:"bucketName"`
			PathPrefix string `json:"pathPrefix"`
		} `json:"minio,omitempty"`
	} `json:"multimodal"`

	DocumentSplitting struct {
		ChunkSize    int      `json:"chunkSize" binding:"required,min=100,max=10000"`
		ChunkOverlap int      `json:"chunkOverlap" binding:"min=0"`
		Separators   []string `json:"separators" binding:"required,min=1"`
	} `json:"documentSplitting" binding:"required"`

	NodeExtract struct {
		Enabled bool     `json:"enabled"`
		Text    string   `json:"text"`
		Tags    []string `json:"tags"`
		Nodes   []struct {
			Name       string   `json:"name"`
			Attributes []string `json:"attributes"`
		} `json:"nodes"`
		Relations []struct {
			Node1 string `json:"node1"`
			Node2 string `json:"node2"`
			Type  string `json:"type"`
		} `json:"relations"`
	} `json:"nodeExtract"`

	QuestionGeneration struct {
		Enabled       bool `json:"enabled"`
		QuestionCount int  `json:"questionCount"`
	} `json:"questionGeneration"`
}

// UpdateKBConfig godoc
// @Summary      Update Knowledge BaseConfiguration
// @Description  Cập nhật mô hình và cấu hình chia đoạn theo ID kho tri thức
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        kbId     path      string               true  "Knowledge BaseID"
// @Param        request  body      KBModelConfigRequest true  "yêu cầu cấu hình"
// @Success      200      {object}  map[string]interface{}  "cập nhật thành công"
// @Failure      400      {object}  errors.AppError         "tham số không hợp lệ"
// @Failure      404      {object}  errors.AppError         "không tìm thấy kho tri thức"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/config/{kbId} [put]
func (h *InitializationHandler) UpdateKBConfig(c *gin.Context) {
	ctx := c.Request.Context()
	kbIdStr := utils.SanitizeForLog(c.Param("kbId"))

	var req KBModelConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse KB config request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	// Load the knowledge base
	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbIdStr)
	if err != nil || kb == nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"kbId": utils.SanitizeForLog(kbIdStr)})
		c.Error(errors.NewNotFoundError("không tìm thấy kho tri thức"))
		return
	}
	ownWorkspace, err := kbSettingsAccess(c, kb)
	if err != nil {
		_ = c.Error(err)
		return
	}

	// Check whether the embedding model may be changed
	if kb.EmbeddingModelID != "" && req.EmbeddingModelID != "" && kb.EmbeddingModelID != req.EmbeddingModelID {
		// Check whether files already exist
		knowledgeList, err := h.knowledgeService.ListPagedKnowledgeByKnowledgeBaseID(ctx,
			kbIdStr, &types.Pagination{
				Page:     1,
				PageSize: 1,
			}, types.KnowledgeListFilter{})
		if err == nil && knowledgeList != nil && knowledgeList.Total > 0 {
			logger.Error(ctx, "Cannot change embedding model when files exist")
			c.Error(errors.NewBadRequestError("kho tri thức đã có tài liệu, không thể đổi mô hình embedding"))
			return
		}
	}

	// Load model details from DB and validate
	llmModel, err := h.modelService.GetModelByID(ctx, req.LLMModelID)
	if err != nil || llmModel == nil {
		logger.Error(ctx, "LLM model not found")
		c.Error(errors.NewBadRequestError("không tìm thấy mô hình LLM"))
		return
	}

	// Validate the embedding model only when needed (RAG retrieval enabled)
	if req.EmbeddingModelID != "" {
		embeddingModel, err := h.modelService.GetModelByID(ctx, req.EmbeddingModelID)
		if err != nil || embeddingModel == nil {
			logger.Error(ctx, "Embedding model not found")
			c.Error(errors.NewBadRequestError("không tìm thấy mô hình embedding"))
			return
		}
	}

	// Update the knowledge base's model IDs
	kb.SummaryModelID = req.LLMModelID
	if req.EmbeddingModelID != "" {
		kb.EmbeddingModelID = req.EmbeddingModelID
	}

	// Handle multimodal model config
	kb.VLMConfig = types.VLMConfig{}
	if req.VLMConfig != nil && req.Multimodal.Enabled && req.VLMConfig.ModelID != "" {
		vllmModel, err := h.modelService.GetModelByID(ctx, req.VLMConfig.ModelID)
		if err != nil || vllmModel == nil {
			logger.Warn(ctx, "VLM model not found")
		} else {
			kb.VLMConfig.Enabled = req.VLMConfig.Enabled
			kb.VLMConfig.ModelID = req.VLMConfig.ModelID
		}
	}
	if !kb.VLMConfig.Enabled {
		kb.VLMConfig.ModelID = ""
	}

	// Handle ASR speech recognition config
	kb.ASRConfig = types.ASRConfig{}
	if req.ASRConfig != nil && req.ASRConfig.Enabled && req.ASRConfig.ModelID != "" {
		asrModel, err := h.modelService.GetModelByID(ctx, req.ASRConfig.ModelID)
		if err != nil || asrModel == nil {
			logger.Warn(ctx, "ASR model not found")
		} else {
			kb.ASRConfig.Enabled = true
			kb.ASRConfig.ModelID = req.ASRConfig.ModelID
			kb.ASRConfig.Language = req.ASRConfig.Language
		}
	}

	// Update document chunking config
	if req.DocumentSplitting.ChunkSize > 0 {
		kb.ChunkingConfig.ChunkSize = req.DocumentSplitting.ChunkSize
	}
	if req.DocumentSplitting.ChunkOverlap >= 0 {
		kb.ChunkingConfig.ChunkOverlap = req.DocumentSplitting.ChunkOverlap
	}
	if len(req.DocumentSplitting.Separators) > 0 {
		kb.ChunkingConfig.Separators = req.DocumentSplitting.Separators
	}
	kb.ChunkingConfig.ParserEngineRules = req.DocumentSplitting.ParserEngineRules
	kb.ChunkingConfig.EnableParentChild = req.DocumentSplitting.EnableParentChild
	if req.DocumentSplitting.ParentChunkSize > 0 {
		kb.ChunkingConfig.ParentChunkSize = req.DocumentSplitting.ParentChunkSize
	}
	if req.DocumentSplitting.ChildChunkSize > 0 {
		kb.ChunkingConfig.ChildChunkSize = req.DocumentSplitting.ChildChunkSize
	}
	// Pointer-based fields support clearing (empty string / 0 / empty slice
	// is a valid "user picked default again" signal; absent in payload means
	// "no change").
	if req.DocumentSplitting.Strategy != nil {
		kb.ChunkingConfig.Strategy = *req.DocumentSplitting.Strategy
	}
	if req.DocumentSplitting.TokenLimit != nil {
		kb.ChunkingConfig.TokenLimit = *req.DocumentSplitting.TokenLimit
	}
	if req.DocumentSplitting.Languages != nil {
		kb.ChunkingConfig.Languages = *req.DocumentSplitting.Languages
	}
	if req.DocumentSplitting.TableMetadataInstructions != nil {
		kb.ChunkingConfig.TableMetadataInstructions = strings.TrimSpace(*req.DocumentSplitting.TableMetadataInstructions)
	}

	// Update multimodal config
	if req.Multimodal.Enabled {
		// VLM model already set above
	} else {
		kb.VLMConfig.ModelID = ""
	}
	if req.VLMConfig != nil {
		kb.VLMConfig.DescriptionLanguage = strings.TrimSpace(req.VLMConfig.DescriptionLanguage)
		kb.VLMConfig.CustomInstructions = strings.TrimSpace(req.VLMConfig.CustomInstructions)
	}

	// Storage backends resolve per workspace and belong to the owner's
	// infrastructure: another workspace can neither see the owner's backends
	// nor bind the KB to one of its own, so it may only leave them unchanged.
	if ownWorkspace {
		if err := h.applyKBStorageBinding(ctx, kb, kbIdStr, &req); err != nil {
			_ = c.Error(err)
			return
		}
	} else if kbStorageBindingChanged(kb, req.StorageBackendID, req.StorageProvider) {
		_ = c.Error(errors.NewForbiddenError("chỉ đơn vị sở hữu kho tri thức mới được sửa cấu hình lưu trữ"))
		return
	}

	// Update knowledge graph config
	if req.NodeExtract.Enabled {
		// Convert Nodes and Relations to pointer types
		nodes := make([]*types.GraphNode, len(req.NodeExtract.Nodes))
		for i := range req.NodeExtract.Nodes {
			nodes[i] = &req.NodeExtract.Nodes[i]
		}
		relations := make([]*types.GraphRelation, len(req.NodeExtract.Relations))
		for i := range req.NodeExtract.Relations {
			relations[i] = &req.NodeExtract.Relations[i]
		}

		kb.ExtractConfig = &types.ExtractConfig{
			Enabled:            req.NodeExtract.Enabled,
			Text:               req.NodeExtract.Text,
			Tags:               req.NodeExtract.Tags,
			Nodes:              nodes,
			Relations:          relations,
			CustomInstructions: strings.TrimSpace(req.NodeExtract.CustomInstructions),
		}
	} else if kb.ExtractConfig != nil {
		kb.ExtractConfig.Enabled = false
	} else {
		kb.ExtractConfig = &types.ExtractConfig{Enabled: false}
	}
	if err := validateExtractConfig(kb.ExtractConfig); err != nil {
		logger.Error(ctx, "Invalid extract configuration", err)
		c.Error(err)
		return
	}

	// Update question generation config
	if req.QuestionGeneration.Enabled {
		questionCount := req.QuestionGeneration.QuestionCount
		if questionCount <= 0 {
			questionCount = 3
		}
		if questionCount > 10 {
			questionCount = 10
		}
		kb.QuestionGenerationConfig = &types.QuestionGenerationConfig{
			Enabled:            true,
			QuestionCount:      questionCount,
			CustomInstructions: strings.TrimSpace(req.QuestionGeneration.CustomInstructions),
		}
	} else {
		kb.QuestionGenerationConfig = &types.QuestionGenerationConfig{
			Enabled:            false,
			CustomInstructions: strings.TrimSpace(req.QuestionGeneration.CustomInstructions),
		}
	}
	types.NormalizeKnowledgeBasePromptInstructions(kb)
	if err := validateKnowledgeBasePromptInstructions(kb); err != nil {
		c.Error(err)
		return
	}

	// Save the updated knowledge base
	if err := h.kbRepository.UpdateKnowledgeBase(ctx, kb); err != nil {
		logger.Error(ctx, "Failed to update knowledge base", err)
		c.Error(errors.NewInternalServerError("cập nhật kho tri thức thất bại: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "cập nhật cấu hình thành công",
	})
}

// kbSettingsAccess reports whether the caller's workspace owns kb. Another
// workspace may change KB settings only through an admin share: the frontend
// offers KB settings to share admins alone, and KBPermissionEditor edits content,
// not settings. KBAccessWrite on the route also admits share editors, so the
// grant's effective share permission is checked here.
func kbSettingsAccess(c *gin.Context, kb *types.KnowledgeBase) (bool, error) {
	if kb.TenantID == types.CallerFromContext(c.Request.Context()).TenantID {
		return true, nil
	}
	grant, ok := middleware.KBAccessFromContext(c)
	if !ok || grant.KnowledgeBase == nil || grant.KnowledgeBase.ID != kb.ID ||
		!grant.Permission.HasPermission(types.KBPermissionAdmin) {
		return false, errors.NewForbiddenError("sửa cài đặt kho tri thức được chia sẻ cần quyền chia sẻ ở mức quản trị")
	}
	return false, nil
}

// kbStorageBindingChanged reports whether a config request would rebind the
// KB's storage. A backend ID that matches the current one leaves the binding
// alone (its provider is only a projection); without one, an empty current
// provider means the default, which clients echo back as "local".
func kbStorageBindingChanged(kb *types.KnowledgeBase, backendID, provider string) bool {
	backendID = strings.TrimSpace(backendID)
	current := ""
	if kb.StorageBackendID != nil {
		current = *kb.StorageBackendID
	}
	if backendID != "" {
		return backendID != current
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	currentProvider := kb.GetStorageProvider()
	if currentProvider == "" {
		currentProvider = "local"
	}
	return provider != "" && provider != currentProvider
}

// applyKBStorageBinding binds the owner's storage instance to the KB. The
// caller's workspace must own the KB: backends resolve against TenantInfo.
func (h *InitializationHandler) applyKBStorageBinding(
	ctx context.Context, kb *types.KnowledgeBase, kbID string, req *KBModelConfigRequest,
) error {
	// Bind the concrete storage instance. Provider remains a compatibility
	// projection for older clients and historical rows.
	if strings.TrimSpace(req.StorageBackendID) != "" {
		tenant, _ := types.TenantInfoFromContext(ctx)
		backend, resolveErr := h.storageResolver.ResolveBackend(ctx, tenant, req.StorageBackendID, "")
		if resolveErr != nil || backend == nil {
			return errors.NewBadRequestError("Storage backend is unavailable")
		}
		oldID := ""
		if kb.StorageBackendID != nil {
			oldID = *kb.StorageBackendID
		}
		if oldID != "" && oldID != backend.ID {
			knowledgeList, listErr := h.knowledgeService.ListPagedKnowledgeByKnowledgeBaseID(ctx,
				kbID, &types.Pagination{Page: 1, PageSize: 1}, types.KnowledgeListFilter{})
			if listErr == nil && knowledgeList != nil && knowledgeList.Total > 0 {
				return errors.NewBadRequestError(
					"Storage backend cannot be changed while the knowledge base contains files; migrate storage first")
			}
		}
		kb.StorageBackendID = &backend.ID
		req.StorageProvider = backend.Provider
	}
	// Legacy provider projection.
	provider := strings.ToLower(strings.TrimSpace(req.StorageProvider))
	if provider == "" {
		provider = "local"
	}
	if !isStorageProviderAllowed(provider) {
		return errors.NewBadRequestError("Storage provider is not allowed by STORAGE_ALLOW_LIST")
	}
	oldProvider := kb.GetStorageProvider()
	if oldProvider == "" {
		oldProvider = "local"
	}
	if oldProvider != provider {
		knowledgeList, err := h.knowledgeService.ListPagedKnowledgeByKnowledgeBaseID(ctx,
			kbID, &types.Pagination{Page: 1, PageSize: 1}, types.KnowledgeListFilter{})
		if err == nil && knowledgeList != nil && knowledgeList.Total > 0 {
			logger.Warn(ctx, "Storage engine changed with existing files, old files may become inaccessible")
		}
	}
	kb.SetStorageProvider(provider)
	return nil
}

// InitializeByKB godoc
// @Summary      Initialization Knowledge BaseConfiguration
// @Description  Cập nhật toàn bộ cấu hình theo ID kho tri thức
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        kbId     path      string  true  "Knowledge BaseID"
// @Param        request  body      handler.InitializationRequest  true  "yêu cầu khởi tạo"
// @Success      200      {object}  map[string]interface{}  "khởi tạo thành công"
// @Failure      400      {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/initialize/{kbId} [post]
func (h *InitializationHandler) InitializeByKB(c *gin.Context) {
	ctx := c.Request.Context()
	kbIdStr := utils.SanitizeForLog(c.Param("kbId"))

	req, err := h.bindInitializationRequest(ctx, c)
	if err != nil {
		c.Error(err)
		return
	}

	logger.Infof(
		ctx,
		"Starting knowledge base configuration update, kbId: %s, request: %s",
		utils.SanitizeForLog(kbIdStr),
		utils.SanitizeForLog(utils.ToJSON(req)),
	)

	kb, err := h.getKnowledgeBaseForInitialization(ctx, kbIdStr)
	if err != nil {
		c.Error(err)
		return
	}

	if err := h.validateInitializationConfigs(ctx, req); err != nil {
		c.Error(err)
		return
	}

	processedModels, err := h.processInitializationModels(ctx, kb, kbIdStr, req)
	if err != nil {
		c.Error(err)
		return
	}

	h.applyKnowledgeBaseInitialization(kb, req, processedModels)

	if err := h.kbRepository.UpdateKnowledgeBase(ctx, kb); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"kbId": utils.SanitizeForLog(kbIdStr)})
		c.Error(errors.NewInternalServerError("cập nhật cấu hình kho tri thức thất bại: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "cập nhật cấu hình kho tri thức thành công",
		"data": gin.H{
			"models":         processedModels,
			"knowledge_base": kb,
		},
	})
}

func (h *InitializationHandler) bindInitializationRequest(ctx context.Context, c *gin.Context) (*InitializationRequest, error) {
	var req InitializationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse initialization request", err)
		return nil, errors.NewBadRequestError(err.Error())
	}
	return &req, nil
}

func (h *InitializationHandler) getKnowledgeBaseForInitialization(ctx context.Context, kbIdStr string) (*types.KnowledgeBase, error) {
	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbIdStr)
	if err != nil {
		// The repo's not-found sentinel must surface as 404, not 500.
		// Without this, every probe of a stale kb id from the
		// initialization flow burns ops attention with a fake server
		// error. See knowledgebase.go:validateAndGetKnowledgeBase.
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			return nil, errors.NewNotFoundError("không tìm thấy kho tri thức")
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"kbId": utils.SanitizeForLog(kbIdStr)})
		return nil, errors.NewInternalServerError("lấy thông tin kho tri thức thất bại: " + err.Error())
	}
	if kb == nil {
		logger.Error(ctx, "Knowledge base not found")
		return nil, errors.NewNotFoundError("không tìm thấy kho tri thức")
	}
	// Initialization rewrites the models the KB points at, and those rows
	// belong to the KB's workspace. A shared-KB editor passes the route's
	// KBAccessWrite guard (which moves execution into that workspace), so
	// without this check it could repoint the owner's models at its own
	// endpoint and key.
	if kb.TenantID != types.CallerFromContext(ctx).TenantID {
		return nil, errors.NewForbiddenError("chỉ đơn vị sở hữu kho tri thức mới được khởi tạo kho tri thức")
	}
	return kb, nil
}

// canUpdateTenantModels reports whether the caller may author model
// definitions through the legacy inline-initialization payload. Model
// configuration is platform-owned, so this now mirrors the /models write
// guards: system administrators and platform API keys only. Tenants pick
// existing catalog models via PUT /initialization/config/:kbId instead.
func (h *InitializationHandler) canUpdateTenantModels(ctx context.Context) bool {
	return types.CanManageModelConfig(ctx)
}

func (h *InitializationHandler) validateInitializationConfigs(ctx context.Context, req *InitializationRequest) error {
	// SSRF validation for all user-supplied BaseURLs
	urlsToCheck := []struct {
		label string
		url   string
	}{
		{"LLM BaseURL", req.LLM.BaseURL},
		{"Embedding BaseURL", req.Embedding.BaseURL},
		{"Rerank BaseURL", req.Rerank.BaseURL},
	}
	if req.Multimodal.VLM != nil {
		urlsToCheck = append(urlsToCheck, struct {
			label string
			url   string
		}{"VLM BaseURL", req.Multimodal.VLM.BaseURL})
	}
	for _, u := range urlsToCheck {
		if u.url != "" {
			if err := utils.ValidateURLForSSRF(u.url); err != nil {
				logger.Warnf(ctx, "SSRF validation failed for %s: %v", u.label, err)
				return errors.NewBadRequestError(utils.FormatSSRFError(u.label, u.url, err))
			}
		}
	}

	if err := h.validateMultimodalConfig(ctx, req); err != nil {
		return err
	}
	if err := validateRerankConfig(ctx, req); err != nil {
		return err
	}
	return validateNodeExtractConfig(ctx, req)
}

func (h *InitializationHandler) validateMultimodalConfig(ctx context.Context, req *InitializationRequest) error {
	if !req.Multimodal.Enabled {
		return nil
	}

	storageType := strings.ToLower(req.Multimodal.StorageType)
	if req.Multimodal.VLM == nil {
		logger.Error(ctx, "Multimodal enabled but missing VLM configuration")
		return errors.NewBadRequestError("bật đa phương thức cần cấu hình thông tin VLM")
	}
	if req.Multimodal.VLM.InterfaceType == "ollama" {
		req.Multimodal.VLM.BaseURL = os.Getenv("OLLAMA_BASE_URL") + "/v1"
	}
	if req.Multimodal.VLM.ModelName == "" || req.Multimodal.VLM.BaseURL == "" {
		logger.Error(ctx, "VLM configuration incomplete")
		return errors.NewBadRequestError("cấu hình VLM chưa đầy đủ")
	}

	switch storageType {
	case "cos":
		if req.Multimodal.COS == nil || req.Multimodal.COS.SecretID == "" || req.Multimodal.COS.SecretKey == "" ||
			req.Multimodal.COS.Region == "" || req.Multimodal.COS.BucketName == "" ||
			req.Multimodal.COS.AppID == "" {
			logger.Error(ctx, "COS configuration incomplete")
			return errors.NewBadRequestError("cấu hình COS chưa đầy đủ")
		}
	case "minio":
		if req.Multimodal.Minio == nil || req.Multimodal.Minio.BucketName == "" ||
			os.Getenv("MINIO_ACCESS_KEY_ID") == "" || os.Getenv("MINIO_SECRET_ACCESS_KEY") == "" {
			logger.Error(ctx, "MinIO configuration incomplete")
			return errors.NewBadRequestError("cấu hình MinIO chưa đầy đủ")
		}
	}
	return nil
}

func validateRerankConfig(ctx context.Context, req *InitializationRequest) error {
	if !req.Rerank.Enabled {
		return nil
	}
	if req.Rerank.ModelName == "" || req.Rerank.BaseURL == "" {
		logger.Error(ctx, "Rerank configuration incomplete")
		return errors.NewBadRequestError("cấu hình rerank chưa đầy đủ")
	}
	return nil
}

func validateNodeExtractConfig(ctx context.Context, req *InitializationRequest) error {
	if !req.NodeExtract.Enabled {
		return nil
	}
	if strings.ToLower(os.Getenv("NEO4J_ENABLE")) != "true" {
		logger.Error(ctx, "Node Extractor configuration incomplete")
		return errors.NewBadRequestError("hãy cấu hình đúng biến môi trường NEO4J_ENABLE")
	}
	if req.NodeExtract.Text == "" || len(req.NodeExtract.Tags) == 0 {
		logger.Error(ctx, "Node Extractor configuration incomplete")
		return errors.NewBadRequestError("cấu hình Node Extractor chưa đầy đủ")
	}
	if len(req.NodeExtract.Nodes) == 0 || len(req.NodeExtract.Relations) == 0 {
		logger.Error(ctx, "Node Extractor configuration incomplete")
		return errors.NewBadRequestError("hãy trích xuất thực thể và quan hệ trước")
	}
	return nil
}

type modelDescriptor struct {
	modelType     types.ModelType
	name          string
	source        types.ModelSource
	description   string
	baseURL       string
	apiKey        string
	dimension     int
	interfaceType string
}

func buildModelDescriptors(req *InitializationRequest) []modelDescriptor {
	descriptors := []modelDescriptor{
		{
			modelType:   types.ModelTypeKnowledgeQA,
			name:        utils.SanitizeForLog(req.LLM.ModelName),
			source:      types.ModelSource(req.LLM.Source),
			description: "LLM Model for Knowledge QA",
			baseURL:     utils.SanitizeForLog(req.LLM.BaseURL),
			apiKey:      req.LLM.APIKey,
		},
		{
			modelType:   types.ModelTypeEmbedding,
			name:        utils.SanitizeForLog(req.Embedding.ModelName),
			source:      types.ModelSource(req.Embedding.Source),
			description: "Embedding Model",
			baseURL:     utils.SanitizeForLog(req.Embedding.BaseURL),
			apiKey:      req.Embedding.APIKey,
			dimension:   req.Embedding.Dimension,
		},
	}

	if req.Rerank.Enabled {
		descriptors = append(descriptors, modelDescriptor{
			modelType:   types.ModelTypeRerank,
			name:        utils.SanitizeForLog(req.Rerank.ModelName),
			source:      types.ModelSourceRemote,
			description: "Rerank Model",
			baseURL:     utils.SanitizeForLog(req.Rerank.BaseURL),
			apiKey:      req.Rerank.APIKey,
		})
	}

	if req.Multimodal.Enabled && req.Multimodal.VLM != nil {
		descriptors = append(descriptors, modelDescriptor{
			modelType:     types.ModelTypeVLLM,
			name:          utils.SanitizeForLog(req.Multimodal.VLM.ModelName),
			source:        types.ModelSourceRemote,
			description:   "VLM Model",
			baseURL:       utils.SanitizeForLog(req.Multimodal.VLM.BaseURL),
			apiKey:        req.Multimodal.VLM.APIKey,
			interfaceType: req.Multimodal.VLM.InterfaceType,
		})
	}

	return descriptors
}

func (h *InitializationHandler) processInitializationModels(
	ctx context.Context,
	kb *types.KnowledgeBase,
	kbIdStr string,
	req *InitializationRequest,
) ([]*types.Model, error) {
	descriptors := buildModelDescriptors(req)
	var processedModels []*types.Model

	// Inline model definitions author catalog rows — a platform-level
	// write. Non-system-admin callers must bind existing model IDs through
	// PUT /initialization/config/:kbId instead. Checked once up front so a
	// partially-processed request can never mutate some descriptors and
	// then fail on a later one.
	if len(descriptors) > 0 && !h.canUpdateTenantModels(ctx) {
		return nil, errors.NewForbiddenError(
			"inline model configuration requires a system administrator; bind existing models via model IDs instead")
	}

	for _, descriptor := range descriptors {
		model := descriptor.toModel()
		// Stamp the KB's tenant before insert: toModel() carries no tenant and
		// modelRepository.GetByID filters on (tenant_id = ? OR is_builtin), so
		// an unstamped row lands at tenant_id = 0 where no tenant — not even
		// the one that just configured the KB — can ever read it back
		// (issue #3333).
		model.TenantID = kb.TenantID
		existingModelID := h.findExistingModelID(kb, descriptor.modelType)

		var existingModel *types.Model
		if existingModelID != "" {
			var err error
			existingModel, err = h.modelService.GetModelByID(ctx, existingModelID)
			if err != nil {
				logger.Warnf(ctx, "Failed to get existing model %s: %v, will create new one", existingModelID, err)
				existingModel = nil
			}
		}

		if existingModel != nil {
			existingModel.Name = model.Name
			existingModel.Source = model.Source
			existingModel.Description = model.Description
			existingModel.Parameters = model.Parameters
			existingModel.UpdatedAt = time.Now()

			if err := h.modelService.UpdateModel(ctx, existingModel); err != nil {
				logger.ErrorWithFields(ctx, err, map[string]interface{}{
					"model_id": model.ID,
					"kb_id":    kbIdStr,
				})
				return nil, errors.NewInternalServerError("cập nhật mô hình thất bại: " + err.Error())
			}
			processedModels = append(processedModels, existingModel)
			continue
		}

		if err := h.modelService.CreateModel(ctx, model); err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"model_id": model.ID,
				"kb_id":    kbIdStr,
			})
			return nil, errors.NewInternalServerError("tạo mô hình thất bại: " + err.Error())
		}
		processedModels = append(processedModels, model)
	}

	return processedModels, nil
}

func (descriptor modelDescriptor) toModel() *types.Model {
	model := &types.Model{
		Type:        descriptor.modelType,
		Name:        descriptor.name,
		Source:      descriptor.source,
		Description: descriptor.description,
		Parameters: types.ModelParameters{
			BaseURL:       descriptor.baseURL,
			APIKey:        descriptor.apiKey,
			InterfaceType: descriptor.interfaceType,
		},
		IsDefault: false,
		Status:    types.ModelStatusActive,
	}

	if descriptor.modelType == types.ModelTypeEmbedding {
		model.Parameters.EmbeddingParameters = types.EmbeddingParameters{
			Dimension: descriptor.dimension,
		}
	}

	return model
}

func (h *InitializationHandler) findExistingModelID(kb *types.KnowledgeBase, modelType types.ModelType) string {
	switch modelType {
	case types.ModelTypeEmbedding:
		return kb.EmbeddingModelID
	case types.ModelTypeKnowledgeQA:
		return kb.SummaryModelID
	case types.ModelTypeVLLM:
		return kb.VLMConfig.ModelID
	default:
		return ""
	}
}

func (h *InitializationHandler) applyKnowledgeBaseInitialization(
	kb *types.KnowledgeBase,
	req *InitializationRequest,
	processedModels []*types.Model,
) {
	embeddingModelID, llmModelID, vlmModelID := extractModelIDs(processedModels)

	kb.SummaryModelID = llmModelID
	kb.EmbeddingModelID = embeddingModelID

	kb.ChunkingConfig = types.ChunkingConfig{
		ChunkSize:    req.DocumentSplitting.ChunkSize,
		ChunkOverlap: req.DocumentSplitting.ChunkOverlap,
		Separators:   req.DocumentSplitting.Separators,
	}

	if req.Multimodal.Enabled {
		kb.VLMConfig = types.VLMConfig{
			Enabled: req.Multimodal.Enabled,
			ModelID: vlmModelID,
		}
		switch req.Multimodal.StorageType {
		case "cos":
			if req.Multimodal.COS != nil {
				kb.SetStorageProvider("cos")
				// Legacy: also write to cos_config for backward compat with old code paths
				kb.StorageConfig = types.StorageConfig{
					Provider:   req.Multimodal.StorageType,
					BucketName: req.Multimodal.COS.BucketName,
					AppID:      req.Multimodal.COS.AppID,
					PathPrefix: req.Multimodal.COS.PathPrefix,
					SecretID:   req.Multimodal.COS.SecretID,
					SecretKey:  req.Multimodal.COS.SecretKey,
					Region:     req.Multimodal.COS.Region,
				}
			}
		case "minio":
			if req.Multimodal.Minio != nil {
				kb.SetStorageProvider("minio")
				// Legacy: also write to cos_config for backward compat with old code paths
				kb.StorageConfig = types.StorageConfig{
					Provider:   req.Multimodal.StorageType,
					BucketName: req.Multimodal.Minio.BucketName,
					PathPrefix: req.Multimodal.Minio.PathPrefix,
					SecretID:   os.Getenv("MINIO_ACCESS_KEY_ID"),
					SecretKey:  os.Getenv("MINIO_SECRET_ACCESS_KEY"),
				}
			}
		}
	} else {
		kb.VLMConfig = types.VLMConfig{}
		kb.SetStorageProvider("")
		kb.StorageConfig = types.StorageConfig{}
	}

	if req.NodeExtract.Enabled {
		kb.ExtractConfig = &types.ExtractConfig{
			Text:      req.NodeExtract.Text,
			Tags:      req.NodeExtract.Tags,
			Nodes:     make([]*types.GraphNode, 0),
			Relations: make([]*types.GraphRelation, 0),
		}
		for _, rnode := range req.NodeExtract.Nodes {
			node := &types.GraphNode{
				Name:       rnode.Name,
				Attributes: rnode.Attributes,
			}
			kb.ExtractConfig.Nodes = append(kb.ExtractConfig.Nodes, node)
		}
		for _, relation := range req.NodeExtract.Relations {
			kb.ExtractConfig.Relations = append(kb.ExtractConfig.Relations, &types.GraphRelation{
				Node1: relation.Node1,
				Node2: relation.Node2,
				Type:  relation.Type,
			})
		}
	}
}

func extractModelIDs(processedModels []*types.Model) (embeddingModelID, llmModelID, vlmModelID string) {
	for _, model := range processedModels {
		if model == nil {
			continue
		}
		switch model.Type {
		case types.ModelTypeEmbedding:
			embeddingModelID = model.ID
		case types.ModelTypeKnowledgeQA:
			llmModelID = model.ID
		case types.ModelTypeVLLM:
			vlmModelID = model.ID
		}
	}
	return
}

// CheckOllamaStatus godoc
// @Summary      Kiểm tra trạng thái dịch vụ Ollama
// @Description  Kiểm tra dịch vụ Ollama có khả dụng hay không
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "trạng thái Ollama"
// @Router       /initialization/ollama/status [get]
func (h *InitializationHandler) CheckOllamaStatus(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking Ollama service status")

	// Determine Ollama base URL for display
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://host.docker.internal:11434"
	}

	// Check whether the Ollama service is available
	err := h.ollamaService.StartService(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"available": false,
				"error":     err.Error(),
				"baseUrl":   baseURL,
			},
		})
		return
	}

	version, err := h.ollamaService.GetVersion(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		version = "unknown"
	}

	logger.Info(ctx, "Ollama service is available")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": h.ollamaService.IsAvailable(),
			"version":   version,
			"baseUrl":   baseURL,
		},
	})
}

// CheckOllamaModels godoc
// @Summary      Kiểm tra trạng thái mô hình Ollama
// @Description  Kiểm tra các mô hình Ollama chỉ định đã được cài hay chưa
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        request  body      object{models=[]string}  true  "danh sách tên mô hình"
// @Success      200      {object}  map[string]interface{}   "trạng thái mô hình"
// @Failure      400      {object}  errors.AppError          "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/ollama/models/check [post]
func (h *InitializationHandler) CheckOllamaModels(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking Ollama models status")

	var req struct {
		Models []string `json:"models" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse models check request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	// Check whether the Ollama service is available
	if !h.ollamaService.IsAvailable() {
		err := h.ollamaService.StartService(ctx)
		if err != nil {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("dịch vụ Ollama không khả dụng: " + err.Error()))
			return
		}
	}

	modelStatus := make(map[string]bool)

	// Check whether each model exists
	for _, modelName := range req.Models {
		available, err := h.ollamaService.IsModelAvailable(ctx, modelName)
		if err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"model_name": modelName,
			})
			modelStatus[modelName] = false
		} else {
			modelStatus[modelName] = available
		}

		logger.Infof(ctx, "Model %s availability: %v", utils.SanitizeForLog(modelName), modelStatus[modelName])
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"models": modelStatus,
		},
	})
}

// DownloadOllamaModel godoc
// @Summary      Tải mô hình Ollama
// @Description  Tải bất đồng bộ mô hình Ollama chỉ định
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        request  body      object{modelName=string}  true  "tên mô hình"
// @Success      200      {object}  map[string]interface{}    "Download task information"
// @Failure      400      {object}  errors.AppError           "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/ollama/models/download [post]
func (h *InitializationHandler) DownloadOllamaModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Starting async Ollama model download")

	var req struct {
		ModelName string `json:"modelName" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse model download request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	// Check whether the Ollama service is available
	if !h.ollamaService.IsAvailable() {
		err := h.ollamaService.StartService(ctx)
		if err != nil {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("dịch vụ Ollama không khả dụng: " + err.Error()))
			return
		}
	}

	// Check whether the model already exists
	available, err := h.ollamaService.IsModelAvailable(ctx, req.ModelName)
	if err != nil {
		c.Error(errors.NewInternalServerError("kiểm tra trạng thái mô hình thất bại: " + err.Error()))
		return
	}

	if available {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "mô hình đã tồn tại",
			"data": gin.H{
				"modelName": req.ModelName,
				"status":    "completed",
				"progress":  100.0,
			},
		})
		return
	}

	// Check for an existing download task for the same model
	tasksMutex.RLock()
	for _, task := range downloadTasks {
		if task.ModelName == req.ModelName && (task.Status == "pending" || task.Status == "downloading") {
			tasksMutex.RUnlock()
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "tác vụ tải mô hình đã tồn tại",
				"data": gin.H{
					"taskId":    task.ID,
					"modelName": task.ModelName,
					"status":    task.Status,
					"progress":  task.Progress,
				},
			})
			return
		}
	}
	tasksMutex.RUnlock()

	// Create the download task
	taskID := uuid.New().String()
	task := &DownloadTask{
		ID:        taskID,
		ModelName: req.ModelName,
		Status:    "pending",
		Progress:  0.0,
		Message:   "chuẩn bị tải",
		StartTime: time.Now(),
	}

	tasksMutex.Lock()
	downloadTasks[taskID] = task
	tasksMutex.Unlock()

	// Start the async download
	newCtx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	go func() {
		defer cancel()
		h.downloadModelAsync(newCtx, taskID, req.ModelName)
	}()

	logger.Infof(ctx, "Created download task for model, task ID: %s", taskID)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "đã tạo tác vụ tải mô hình",
		"data": gin.H{
			"taskId":    taskID,
			"modelName": req.ModelName,
			"status":    "pending",
			"progress":  0.0,
		},
	})
}

// GetDownloadProgress godoc
// @Summary      Lấy tiến độ tải
// @Description  Lấy tiến độ của tác vụ tải mô hình Ollama
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        taskId  path      string  true  "ID tác vụ"
// @Success      200     {object}  map[string]interface{}  "tiến độ tải"
// @Failure      404     {object}  errors.AppError         "không tìm thấy tác vụ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/ollama/download/progress/{taskId} [get]
func (h *InitializationHandler) GetDownloadProgress(c *gin.Context) {
	taskID := c.Param("taskId")

	if taskID == "" {
		c.Error(errors.NewBadRequestError("thiếu tham số taskId"))
		return
	}

	tasksMutex.RLock()
	task, exists := downloadTasks[taskID]
	tasksMutex.RUnlock()

	if !exists {
		c.Error(errors.NewNotFoundError("không tìm thấy tác vụ tải"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    task,
	})
}

// ListDownloadTasks godoc
// @Summary      Liệt kê tác vụ tải
// @Description  Liệt kê mọi tác vụ tải mô hình Ollama
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "danh sách tác vụ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/ollama/download/tasks [get]
func (h *InitializationHandler) ListDownloadTasks(c *gin.Context) {
	tasksMutex.RLock()
	tasks := make([]*DownloadTask, 0, len(downloadTasks))
	for _, task := range downloadTasks {
		tasks = append(tasks, task)
	}
	tasksMutex.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tasks,
	})
}

// ListOllamaModels godoc
// @Summary      Liệt kê mô hình Ollama
// @Description  Liệt kê các mô hình Ollama đã cài
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "danh sách mô hình"
// @Failure      500  {object}  errors.AppError         "lỗi hệ thống"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/ollama/models [get]
func (h *InitializationHandler) ListOllamaModels(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Listing installed Ollama models")

	// Ensure the service is available
	if !h.ollamaService.IsAvailable() {
		if err := h.ollamaService.StartService(ctx); err != nil {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("dịch vụ Ollama không khả dụng: " + err.Error()))
			return
		}
	}

	// Use ListModelsDetailed to get the model list with size and other details
	models, err := h.ollamaService.ListModelsDetailed(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("lấy danh sách mô hình thất bại: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"models": models,
		},
	})
}

// downloadModelAsync downloads a model asynchronously
func (h *InitializationHandler) downloadModelAsync(ctx context.Context,
	taskID, modelName string,
) {
	logger.Infof(ctx, "Starting async download for model, task: %s", taskID)

	// Mark the task as downloading
	h.updateTaskStatus(taskID, "downloading", 0.0, "bắt đầu tải mô hình")

	// Run the download with a progress callback
	err := h.pullModelWithProgress(ctx, modelName, func(progress float64, message string) {
		h.updateTaskStatus(taskID, "downloading", progress, message)
	})
	if err != nil {
		logger.Error(ctx, "Failed to download model", err)
		h.updateTaskStatus(taskID, "failed", 0.0, fmt.Sprintf("tải thất bại: %v", err))
		return
	}

	// Download succeeded
	logger.Infof(ctx, "Model downloaded successfully, task: %s", taskID)
	h.updateTaskStatus(taskID, "completed", 100.0, "tải xong")
}

// pullModelWithProgress pulls a model and reports progress
func (h *InitializationHandler) pullModelWithProgress(ctx context.Context,
	modelName string,
	progressCallback func(float64, string),
) error {
	// Check whether the service is available
	if err := h.ollamaService.StartService(ctx); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		return err
	}

	// Check whether the model already exists
	available, err := h.ollamaService.IsModelAvailable(ctx, modelName)
	if err != nil {
		logger.Error(ctx, "Failed to check model availability", err)
		return err
	}
	if available {
		progressCallback(100.0, "mô hình đã tồn tại")
		return nil
	}

	// Build the pull request
	pullReq := &api.PullRequest{
		Name: modelName,
	}

	// Use the Ollama client's Pull with a progress callback
	err = h.ollamaService.GetClient().Pull(ctx, pullReq, func(progress api.ProgressResponse) error {
		progressPercent := 0.0
		message := "đang tải"

		if progress.Total > 0 && progress.Completed > 0 {
			progressPercent = float64(progress.Completed) / float64(progress.Total) * 100
			message = fmt.Sprintf("đang tải: %.1f%% (%s)", progressPercent, progress.Status)
		} else if progress.Status != "" {
			message = progress.Status
		}

		// Invoke the progress callback
		progressCallback(progressPercent, message)

		logger.Infof(ctx,
			"Download progress: %.2f%% - %s", progressPercent, message,
		)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to pull model: %w", err)
	}

	return nil
}

// updateTaskStatus updates a task's status
func (h *InitializationHandler) updateTaskStatus(
	taskID, status string, progress float64, message string,
) {
	tasksMutex.Lock()
	defer tasksMutex.Unlock()

	if task, exists := downloadTasks[taskID]; exists {
		task.Status = status
		task.Progress = progress
		task.Message = message

		if status == "completed" || status == "failed" {
			now := time.Now()
			task.EndTime = &now
		}
	}
}

// GetCurrentConfigByKB godoc
// @Summary      Lấy cấu hình kho tri thức
// @Description  Lấy cấu hình hiện tại theo ID kho tri thức
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        kbId  path      string  true  "Knowledge BaseID"
// @Success      200   {object}  map[string]interface{}  "thông tin cấu hình"
// @Failure      404   {object}  errors.AppError         "không tìm thấy kho tri thức"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/config/{kbId} [get]
func (h *InitializationHandler) GetCurrentConfigByKB(c *gin.Context) {
	ctx := c.Request.Context()
	kbIdStr := utils.SanitizeForLog(c.Param("kbId"))

	logger.Info(ctx, "Getting configuration for knowledge base")

	// Load the specified knowledge base
	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbIdStr)
	if err != nil {
		// Mirror getKnowledgeBaseForInitialization above: missing /
		// cross-tenant kb ids are 404, not 500.
		if stderrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			c.Error(errors.NewNotFoundError("không tìm thấy kho tri thức"))
			return
		}
		logger.Error(ctx, "Failed to get knowledge base", err)
		c.Error(errors.NewInternalServerError("lấy thông tin kho tri thức thất bại: " + err.Error()))
		return
	}

	if kb == nil {
		logger.Error(ctx, "Knowledge base not found")
		c.Error(errors.NewNotFoundError("không tìm thấy kho tri thức"))
		return
	}

	// Load the specific models referenced by the knowledge base
	var models []*types.Model
	modelIDs := []string{
		kb.EmbeddingModelID,
		kb.SummaryModelID,
		kb.VLMConfig.ModelID,
	}

	for _, modelID := range modelIDs {
		if modelID != "" {
			model, err := h.modelService.GetModelByID(ctx, modelID)
			if err != nil {
				logger.Warn(ctx, "Failed to get model", err)
				// If a model is missing or fails to load, continue with the others
				continue
			}
			if model != nil {
				models = append(models, model)
			}
		}
	}

	// Check whether the knowledge base has files
	knowledgeList, err := h.knowledgeService.ListPagedKnowledgeByKnowledgeBaseID(ctx,
		kbIdStr, &types.Pagination{
			Page:     1,
			PageSize: 1,
		}, types.KnowledgeListFilter{})
	hasFiles := err == nil && knowledgeList != nil && knowledgeList.Total > 0

	// Build the config response
	config := h.buildConfigResponse(ctx, models, kb, hasFiles)

	logger.Info(ctx, "Knowledge base configuration retrieved successfully")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    config,
	})
}

// buildConfigResponse builds the config response payload
func (h *InitializationHandler) buildConfigResponse(ctx context.Context, models []*types.Model,
	kb *types.KnowledgeBase, hasFiles bool,
) map[string]interface{} {
	config := map[string]interface{}{
		"hasFiles": hasFiles,
	}
	// Integration details describe the owning workspace's infrastructure. A
	// share receiver — even an admin of its own workspace — only learns
	// whether credentials are configured.
	ownWorkspace := kb != nil && kb.TenantID == types.CallerFromContext(ctx).TenantID
	includeIntegrationDetail := ownWorkspace && dto.CanViewIntegrationSecrets(ctx)

	// Group models by type
	for _, model := range models {
		if model == nil {
			continue
		}
		// Hide sensitive information for builtin models and viewers.
		baseURL := model.Parameters.BaseURL
		if model.IsBuiltin || !includeIntegrationDetail {
			baseURL = ""
		}

		switch model.Type {
		case types.ModelTypeKnowledgeQA:
			config["llm"] = map[string]interface{}{
				"source":    string(model.Source),
				"modelName": model.Name,
				"baseUrl":   baseURL,
				"credentials": map[string]bool{
					"apiKey": model.Parameters.APIKey != "" && !model.IsBuiltin,
				},
			}
		case types.ModelTypeEmbedding:
			config["embedding"] = map[string]interface{}{
				"source":    string(model.Source),
				"modelName": model.Name,
				"baseUrl":   baseURL,
				"dimension": model.Parameters.EmbeddingParameters.Dimension,
				"credentials": map[string]bool{
					"apiKey": model.Parameters.APIKey != "" && !model.IsBuiltin,
				},
			}
		case types.ModelTypeRerank:
			config["rerank"] = map[string]interface{}{
				"enabled":   true,
				"modelName": model.Name,
				"baseUrl":   baseURL,
				"credentials": map[string]bool{
					"apiKey": model.Parameters.APIKey != "" && !model.IsBuiltin,
				},
			}
		case types.ModelTypeVLLM:
			if config["multimodal"] == nil {
				config["multimodal"] = map[string]interface{}{
					"enabled": true,
				}
			}
			multimodal := config["multimodal"].(map[string]interface{})
			multimodal["vlm"] = map[string]interface{}{
				"modelName":     model.Name,
				"baseUrl":       baseURL,
				"interfaceType": model.Parameters.InterfaceType,
				"modelId":       model.ID,
				"credentials": map[string]bool{
					"apiKey": model.Parameters.APIKey != "" && !model.IsBuiltin,
				},
			}
		}
	}

	// Multimodal is enabled when there is a VLM model ID or a storage config (old and new fields)
	storageProvider := kb.GetStorageProvider()
	hasMultimodal := (kb.VLMConfig.IsEnabled() ||
		kb.StorageConfig.SecretID != "" || kb.StorageConfig.BucketName != "" ||
		(storageProvider != "" && storageProvider != "local"))
	if config["multimodal"] == nil {
		config["multimodal"] = map[string]interface{}{
			"enabled": hasMultimodal,
		}
	} else {
		config["multimodal"].(map[string]interface{})["enabled"] = hasMultimodal
	}
	if kb.VLMConfig.DescriptionLanguage != "" || kb.VLMConfig.CustomInstructions != "" {
		if config["multimodal"] == nil {
			config["multimodal"] = map[string]interface{}{
				"enabled": hasMultimodal,
			}
		}
		multimodal := config["multimodal"].(map[string]interface{})
		if kb.VLMConfig.DescriptionLanguage != "" {
			multimodal["descriptionLanguage"] = kb.VLMConfig.DescriptionLanguage
		}
		if kb.VLMConfig.CustomInstructions != "" {
			multimodal["customInstructions"] = kb.VLMConfig.CustomInstructions
		}
	}

	// Without a rerank model, mark rerank as disabled
	if config["rerank"] == nil {
		config["rerank"] = map[string]interface{}{
			"enabled":   false,
			"modelName": "",
			"baseUrl":   "",
			"credentials": map[string]bool{
				"apiKey": false,
			},
		}
	}

	// Add the knowledge base's document splitting config
	if kb != nil {
		ds := map[string]interface{}{
			"chunkSize":    kb.ChunkingConfig.ChunkSize,
			"chunkOverlap": kb.ChunkingConfig.ChunkOverlap,
			"separators":   kb.ChunkingConfig.Separators,
		}
		if kb.ChunkingConfig.Strategy != "" {
			ds["strategy"] = kb.ChunkingConfig.Strategy
		}
		if kb.ChunkingConfig.TokenLimit > 0 {
			ds["tokenLimit"] = kb.ChunkingConfig.TokenLimit
		}
		if len(kb.ChunkingConfig.Languages) > 0 {
			ds["languages"] = kb.ChunkingConfig.Languages
		}
		if kb.ChunkingConfig.TableMetadataInstructions != "" {
			ds["tableMetadataInstructions"] = kb.ChunkingConfig.TableMetadataInstructions
		}
		config["documentSplitting"] = ds

		// Add the multimodal storage config (prefer the new fields, fall back to legacy cos_config)
		effectiveProvider := kb.GetStorageProvider()
		if kb.StorageConfig.SecretID != "" || (effectiveProvider != "" && effectiveProvider != "local") {
			if config["multimodal"] == nil {
				config["multimodal"] = map[string]interface{}{
					"enabled": true,
				}
			}
			multimodal := config["multimodal"].(map[string]interface{})
			multimodal["storageType"] = effectiveProvider
			switch effectiveProvider {
			case "cos":
				multimodal["cos"] = map[string]interface{}{
					"region":     kb.StorageConfig.Region,
					"bucketName": kb.StorageConfig.BucketName,
					"appId":      kb.StorageConfig.AppID,
					"pathPrefix": kb.StorageConfig.PathPrefix,
					"credentials": map[string]bool{
						"secretId":  kb.StorageConfig.SecretID != "",
						"secretKey": kb.StorageConfig.SecretKey != "",
					},
				}
			case "minio":
				multimodal["minio"] = map[string]interface{}{
					"bucketName": kb.StorageConfig.BucketName,
					"pathPrefix": kb.StorageConfig.PathPrefix,
				}
			}
			if !ownWorkspace {
				// Bucket locations are the owner's infrastructure too.
				for _, provider := range []string{"cos", "minio"} {
					if detail, ok := multimodal[provider].(map[string]interface{}); ok {
						for _, field := range []string{"region", "bucketName", "appId", "pathPrefix"} {
							delete(detail, field)
						}
					}
				}
			}
		}
	}

	if kb.ExtractConfig != nil {
		nodeExtract := map[string]interface{}{
			"enabled":   kb.ExtractConfig.Enabled,
			"text":      kb.ExtractConfig.Text,
			"tags":      kb.ExtractConfig.Tags,
			"nodes":     kb.ExtractConfig.Nodes,
			"relations": kb.ExtractConfig.Relations,
		}
		if kb.ExtractConfig.CustomInstructions != "" {
			nodeExtract["customInstructions"] = kb.ExtractConfig.CustomInstructions
		}
		config["nodeExtract"] = nodeExtract
	} else {
		config["nodeExtract"] = map[string]interface{}{
			"enabled": false,
		}
	}

	if kb.QuestionGenerationConfig != nil {
		config["questionGeneration"] = map[string]interface{}{
			"enabled":            kb.QuestionGenerationConfig.Enabled,
			"questionCount":      kb.QuestionGenerationConfig.QuestionCount,
			"customInstructions": kb.QuestionGenerationConfig.CustomInstructions,
		}
	} else {
		config["questionGeneration"] = map[string]interface{}{
			"enabled": false,
		}
	}

	return config
}

// ModelTestRequest is the unified "test connection" request body.
//
// The test endpoints of all four model kinds (chat/embedding/rerank/asr) share this struct so that:
//   - the frontend maintains a single form -> backend mapping;
//   - the backend converts the request straight into a *types.Model and calls each package's ConfigFromModel,
//     following exactly the same assembly path as production (service.modelService.GetXxxModel),
//     removing the boilerplate where every test endpoint hand-built its Config.
//
// All provider/model common fields are declared here; adding a field later (like the current
// custom_headers) needs one change that applies to both the production and test paths.
type ModelTestRequest struct {
	Source                    string            `json:"source"` // defaults to "remote" when empty
	ModelName                 string            `json:"modelName" binding:"required"`
	BaseURL                   string            `json:"baseUrl"`
	APIKey                    string            `json:"apiKey"`
	Provider                  string            `json:"provider"`
	InterfaceType             string            `json:"interfaceType,omitempty"`
	Dimension                 int               `json:"dimension,omitempty"`
	SupportsDimensionOverride bool              `json:"supportsDimensionOverride,omitempty"`
	CustomHeaders             map[string]string `json:"customHeaders,omitempty"`
	ExtraConfig               map[string]string `json:"extraConfig,omitempty"`
	// AppSecret is for providers needing a second secret, e.g. LKEAP / Volcengine Rerank (maps to model Parameters.AppSecret).
	AppSecret string `json:"appSecret,omitempty"`
	// ModelID, when set, instructs the handler to substitute any missing
	// secrets (APIKey, AppSecret via ExtraConfig) from the stored model
	// record before assembling the test client. This lets the "Test
	// connection" button work on existing models without making the
	// frontend reload — and ship — the plaintext API key. Other fields
	// (BaseURL, ModelName, etc.) on this request still override the
	// stored values, so a user can validate a new endpoint against the
	// existing credentials in one click.
	ModelID string `json:"modelId,omitempty"`
}

// fillSecretsFromStoredModel mutates req in place: if req.ModelID is set
// and a secret field on the request is empty, the corresponding value from
// the stored (and decrypted) model is copied in. The stored ExtraConfig is
// filled in as well when the request does not carry one — provider-specific
// settings (thinking_control, api_version, remote_model_name, ...) must
// apply to the connection test exactly as they apply to real traffic, and
// the frontend only sends extraConfig when the user actively edits it.
// Non-empty request values are always preferred — they represent the user
// actively typing a new key they want to verify. Missing or inaccessible
// model is treated as a no-op (the connection test will fail downstream
// with a clearer "missing apiKey" error than we could produce here).
func (h *InitializationHandler) fillSecretsFromStoredModel(ctx context.Context, req *ModelTestRequest) {
	if req == nil || req.ModelID == "" {
		return
	}
	if req.APIKey != "" && req.AppSecret != "" && req.ExtraConfig != nil {
		return
	}
	stored, err := h.modelService.GetModelByID(ctx, req.ModelID)
	if err != nil || stored == nil {
		logger.Warnf(ctx, "test-connection: stored model %s not found, leaving secrets empty: %v",
			utils.SanitizeForLog(req.ModelID), err)
		return
	}
	if req.APIKey == "" {
		req.APIKey = stored.Parameters.APIKey
	}
	if req.AppSecret == "" {
		req.AppSecret = stored.Parameters.AppSecret
	}
	if req.ExtraConfig == nil {
		req.ExtraConfig = stored.Parameters.ExtraConfig
	}
}

// RemoteModelCheckRequest keeps the old swagger definition compatible.
//
// Deprecated: kept so generated API docs do not break; new code should use ModelTestRequest.
type RemoteModelCheckRequest = ModelTestRequest

// decryptModelAppSecret decrypts the AppSecret in model Parameters (same behavior as modelService).
func decryptModelAppSecret(encrypted string) string {
	if encrypted == "" {
		return encrypted
	}
	if key := utils.GetAESKey(); key != nil {
		if plain, err := utils.DecryptAESGCM(encrypted, key); err == nil {
			return plain
		}
	}
	return encrypted
}

// buildTestModel converts a test-connection request into a temporary *types.Model (not persisted)
// for ConfigFromModel. An empty source falls back to defaultSource (chat/rerank/asr
// default to remote; embedding follows the source sent by the frontend).
func (h *InitializationHandler) buildTestModel(
	req *ModelTestRequest, modelType types.ModelType, defaultSource types.ModelSource,
) *types.Model {
	source := types.ModelSource(strings.ToLower(req.Source))
	if source == "" {
		source = defaultSource
	}
	return &types.Model{
		Name:   req.ModelName,
		Type:   modelType,
		Source: source,
		Parameters: types.ModelParameters{
			BaseURL:       req.BaseURL,
			APIKey:        req.APIKey,
			AppSecret:     req.AppSecret,
			Provider:      req.Provider,
			InterfaceType: req.InterfaceType,
			ExtraConfig:   req.ExtraConfig,
			CustomHeaders: req.CustomHeaders,
			EmbeddingParameters: types.EmbeddingParameters{
				Dimension:                 req.Dimension,
				TruncatePromptTokens:      256,
				SupportsDimensionOverride: req.SupportsDimensionOverride,
			},
		},
	}
}

// resolveTenantWeKnoraCloudCreds reads the WeKnoraCloud credentials from the current tenant context
// so test-connection endpoints can fill appID/appSecret. Mirrors service.resolveWeKnoraCloudCredentials,
// but since the handler has no injected tenantService yet (historical), it reads from
// TenantInfoFromContext, which is equivalent.
func (h *InitializationHandler) resolveTenantWeKnoraCloudCreds(ctx context.Context) (string, string, bool) {
	tenantInfo, ok := types.TenantInfoFromContext(ctx)
	if !ok {
		return "", "", false
	}
	creds := tenantInfo.Credentials.GetWeKnoraCloud()
	if creds == nil {
		return "", "", true
	}
	return creds.AppID, creds.AppSecret, true
}

// CheckRemoteModel godoc
// @Summary      Kiểm tra mô hình từ xa
// @Description  Kiểm tra kết nối tới mô hình API từ xa
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        request  body      RemoteModelCheckRequest  true  "yêu cầu kiểm tra mô hình"
// @Success      200      {object}  map[string]interface{}   "kết quả kiểm tra"
// @Failure      400      {object}  errors.AppError          "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/remote/check [post]
func (h *InitializationHandler) CheckRemoteModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking remote model connection")

	var req ModelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse remote model check request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	h.fillSecretsFromStoredModel(ctx, &req)

	if req.ModelName == "" || req.BaseURL == "" {
		logger.Error(ctx, "Model name and base URL are required")
		c.Error(errors.NewBadRequestError("thiếu tên mô hình hoặc Base URL"))
		return
	}

	if err := utils.ValidateURLForSSRF(req.BaseURL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for remote model BaseURL: %v", err)
		c.Error(errors.NewBadRequestError(utils.FormatSSRFError("Base URL", req.BaseURL, err)))
		return
	}
	appID, appSecret, ok := h.resolveTenantWeKnoraCloudCreds(ctx)
	if !ok {
		logger.Error(ctx, "Tenant info not found")
		c.Error(errors.NewBadRequestError("không tìm thấy thông tin đơn vị"))
		return
	}

	model := h.buildTestModel(&req, types.ModelTypeKnowledgeQA, types.ModelSourceRemote)
	available, message := h.checkChatModelConnection(ctx, model, appID, appSecret)

	logger.Infof(ctx, "Remote model check completed, available: %v, message: %s", available, message)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": available,
			"message":   message,
		},
	})
}

// TestEmbeddingModel godoc
// @Summary      Kiểm tra mô hình embedding
// @Description  Kiểm tra API embedding có hoạt động không và trả về số chiều vector
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        request  body      handler.ModelTestRequest  true  "yêu cầu kiểm tra embedding"
// @Success      200      {object}  map[string]interface{}  "kết quả kiểm tra"
// @Failure      400      {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/embedding/test [post]
func (h *InitializationHandler) TestEmbeddingModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Testing embedding model connectivity and functionality")

	var req ModelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse embedding test request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	h.fillSecretsFromStoredModel(ctx, &req)
	if req.Source == "" {
		req.Source = string(types.ModelSourceRemote)
	}

	if req.BaseURL != "" {
		if err := utils.ValidateURLForSSRF(req.BaseURL); err != nil {
			logger.Warnf(ctx, "SSRF validation failed for embedding BaseURL: %v", err)
			c.Error(errors.NewBadRequestError(utils.FormatSSRFError("Base URL", req.BaseURL, err)))
			return
		}
	}

	// Alibaba Cloud multimodal embedding models are not supported yet
	if strings.ToLower(req.Provider) == "aliyun" {
		modelNameLower := strings.ToLower(req.ModelName)
		if strings.Contains(modelNameLower, "vision") || strings.Contains(modelNameLower, "multimodal") {
			logger.Infof(ctx, "Aliyun multimodal embedding model not supported: %s", req.ModelName)
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data": gin.H{
					"available": false,
					"message":   "chưa hỗ trợ mô hình embedding đa phương thức của Alibaba Cloud, hãy dùng mô hình embedding thuần văn bản (ví dụ text-embedding-v4)",
					"dimension": 0,
				},
			})
			return
		}
	}

	appID, appSecret, ok := h.resolveTenantWeKnoraCloudCreds(ctx)
	if !ok {
		logger.Error(ctx, "Tenant info not found")
		c.Error(errors.NewBadRequestError("không tìm thấy thông tin đơn vị"))
		return
	}

	model := h.buildTestModel(&req, types.ModelTypeEmbedding, types.ModelSourceRemote)
	emb, err := embedding.NewEmbedder(embedding.ConfigFromModel(model, appID, appSecret), h.pooler, h.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"model": utils.SanitizeForLog(req.ModelName)})
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{`available`: false, `message`: fmt.Sprintf("tạo embedder thất bại: %v", err), `dimension`: 0},
		})
		return
	}

	vec, err := emb.Embed(ctx, "hello")
	if err != nil {
		logger.Error(ctx, "Failed to call embedder", err)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{`available`: false, `message`: fmt.Sprintf("gọi embedding thất bại: %v", err), `dimension`: 0},
		})
		return
	}

	logger.Infof(ctx, "Embedding test succeeded, dimension: %d", len(vec))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{`available`: true, `message`: fmt.Sprintf("kiểm tra thành công, số chiều vector=%d", len(vec)), `dimension`: len(vec)},
	})
}

// classifyConnectionError maps an upstream error string to a short
// human-readable hint in Chinese. Callers should always combine the hint
// with the raw error message (e.g. fmt.Sprintf("%s：%v", hint, err)) so
// the operator can still see what URL / response body the SDK actually
// got — the hint is for "where to start looking", the raw error is for
// "what actually happened".
func classifyConnectionError(errMsg string) string {
	switch {
	case strings.Contains(errMsg, "401") || strings.Contains(errMsg, "unauthorized"):
		return "xác thực thất bại, hãy kiểm tra API Key"
	case strings.Contains(errMsg, "403") || strings.Contains(errMsg, "forbidden"):
		return "không đủ quyền, hãy kiểm tra quyền của API Key"
	case strings.Contains(errMsg, "404") || strings.Contains(errMsg, "not found"):
		return "endpoint API không tồn tại, hãy kiểm tra Base URL"
	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "context deadline exceeded"):
		return "kết nối quá thời gian, hãy kiểm tra mạng"
	case strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "no such host") || strings.Contains(errMsg, "dial tcp"):
		return "không kết nối được máy chủ, hãy kiểm tra Base URL"
	default:
		return "kết nối thất bại"
	}
}

// checkChatModelConnection makes a minimal call through the chat module to test connectivity and auth.
// It follows the same ConfigFromModel -> NewChat path as production, so CustomHeaders,
// ExtraConfig, Provider and the other fields are passed through correctly.
func (h *InitializationHandler) checkChatModelConnection(
	ctx context.Context, model *types.Model, appID, appSecret string,
) (bool, string) {
	chatInstance, err := chat.NewChat(chat.ConfigFromModel(model, appID, appSecret), h.ollamaService)
	if err != nil {
		return false, fmt.Sprintf("tạo phiên chat thất bại: %v", err)
	}

	testMessages := []chat.Message{{Role: "user", Content: "test"}}
	testOptions := &chat.ChatOptions{
		MaxTokens: 1,
		Thinking:  &[]bool{false}[0], // for dashscope.aliyuncs qwen3-32b
	}

	_, err = chatInstance.Chat(ctx, testMessages, testOptions)
	if err != nil {
		errMsg := err.Error()
		// 400 = endpoint reachable + auth ok, just a parameter mismatch
		// (e.g. max_tokens vs max_completion_tokens). Treat as success.
		if strings.Contains(errMsg, "status code: 400") {
			return true, "kết nối bình thường, mô hình sẵn sàng"
		}
		// For every other failure mode we surface a human-readable hint
		// AND the upstream error verbatim. Swallowing the underlying
		// message used to hide things like the actual URL the SDK
		// tried, response body, etc. — making remote debugging nearly
		// impossible. Format: "<hint>：<raw err>".
		return false, fmt.Sprintf("%s：%v", classifyConnectionError(errMsg), err)
	}

	// Connected; the model is usable
	return true, "kết nối bình thường, mô hình sẵn sàng"
}

// checkRerankModelConnection makes a minimal call through the rerank module to test connectivity and auth.
// It shares ConfigFromModel with production, so all fields (CustomHeaders etc.) are passed through.
func (h *InitializationHandler) checkRerankModelConnection(
	ctx context.Context, model *types.Model, appID, appSecret string,
) (bool, string) {
	reranker, err := rerank.NewReranker(rerank.ConfigFromModel(model, appID, appSecret))
	if err != nil {
		return false, fmt.Sprintf("tạo reranker thất bại: %v", err)
	}

	results, err := reranker.Rerank(ctx, "ping", []string{"pong"})
	if err != nil {
		return false, fmt.Sprintf("kiểm tra rerank thất bại: %v", err)
	}
	if len(results) > 0 {
		return true, fmt.Sprintf("rerank hoạt động bình thường, trả về %d kết quả", len(results))
	}
	return false, "kết nối API rerank thành công nhưng không trả về kết quả"
}

// CheckRerankModel godoc
// @Summary      Kiểm tra mô hình rerank
// @Description  Kiểm tra kết nối và chức năng của mô hình rerank
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        request  body      handler.ModelTestRequest  true  "yêu cầu kiểm tra rerank"
// @Success      200      {object}  map[string]interface{}  "kết quả kiểm tra"
// @Failure      400      {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/rerank/check [post]
func (h *InitializationHandler) CheckRerankModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking rerank model connection and functionality")

	var req ModelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse rerank model check request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	h.fillSecretsFromStoredModel(ctx, &req)

	if req.ModelName == "" || req.BaseURL == "" {
		logger.Error(ctx, "Model name and base URL are required")
		c.Error(errors.NewBadRequestError("thiếu tên mô hình hoặc Base URL"))
		return
	}

	if err := utils.ValidateURLForSSRF(req.BaseURL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for rerank BaseURL: %v", err)
		c.Error(errors.NewBadRequestError(utils.FormatSSRFError("Base URL", req.BaseURL, err)))
		return
	}

	appID, appSecret, ok := h.resolveTenantWeKnoraCloudCreds(ctx)
	if !ok {
		logger.Error(ctx, "Tenant info not found")
		c.Error(errors.NewBadRequestError("không tìm thấy thông tin đơn vị"))
		return
	}

	model := h.buildTestModel(&req, types.ModelTypeRerank, types.ModelSourceRemote)
	if providerName := provider.ProviderName(model.Parameters.Provider); providerName == provider.ProviderLKEAP || providerName == provider.ProviderVolcengine {
		appID = ""
		appSecret = decryptModelAppSecret(model.Parameters.AppSecret)
	}
	available, message := h.checkRerankModelConnection(ctx, model, appID, appSecret)

	logger.Infof(ctx, "Rerank model check completed, available: %v, message: %s", available, message)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": available,
			"message":   message,
		},
	})
}

// CheckASRModel godoc
// @Summary      Kiểm tra mô hình ASR
// @Description  Kiểm tra kết nối mô hình ASR (nhận dạng giọng nói) bằng cách gửi một đoạn âm thanh im lặng tới endpoint /v1/audio/transcriptions
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        request  body      handler.ModelTestRequest  true  "yêu cầu kiểm tra ASR"
// @Success      200      {object}  map[string]interface{}  "kết quả kiểm tra"
// @Failure      400      {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/asr/check [post]
func (h *InitializationHandler) CheckASRModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking ASR model connection")

	var req ModelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse ASR model check request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	h.fillSecretsFromStoredModel(ctx, &req)

	if req.ModelName == "" || req.BaseURL == "" {
		logger.Error(ctx, "Model name and base URL are required for ASR check")
		c.Error(errors.NewBadRequestError("thiếu tên mô hình hoặc Base URL"))
		return
	}

	if err := utils.ValidateURLForSSRF(req.BaseURL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for ASR BaseURL: %v", err)
		c.Error(errors.NewBadRequestError(utils.FormatSSRFError("Base URL", req.BaseURL, err)))
		return
	}

	// Build a test *types.Model with the shared constructor (ASR needs no WeKnoraCloud credentials),
	// then send a very short silent WAV to verify /v1/audio/transcriptions is reachable.
	model := h.buildTestModel(&req, types.ModelTypeASR, types.ModelSourceRemote)
	asrInstance, err := asr.NewASR(asr.ConfigFromModel(model))
	if err != nil {
		logger.Errorf(ctx, "Failed to create ASR instance for check: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"available": false,
				"message":   fmt.Sprintf("tạo phiên ASR thất bại: %v", err),
			},
		})
		return
	}

	res, err := asrInstance.Transcribe(ctx, assets.ASRTestWAV, "asr_test.wav")
	var text string
	if res != nil {
		text = res.Text
	}
	available := true
	message := "kết nối ASR thành công"

	if err != nil {
		errMsg := err.Error()
		// Always include the raw upstream error after the hint — see
		// classifyConnectionError comment for rationale.
		switch {
		case strings.Contains(errMsg, "401") || strings.Contains(errMsg, "Unauthorized") || strings.Contains(errMsg, "authentication"):
			available = false
			message = fmt.Sprintf("xác thực thất bại, hãy kiểm tra API Key: %s", errMsg)
		case strings.Contains(errMsg, "404") || strings.Contains(errMsg, "Not Found"):
			available = false
			message = fmt.Sprintf("endpoint API không tồn tại, hãy kiểm tra Base URL: %s", errMsg)
		case strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "no such host") || strings.Contains(errMsg, "dial tcp"):
			available = false
			message = fmt.Sprintf("không kết nối được máy chủ, hãy kiểm tra Base URL: %s", errMsg)
		case strings.Contains(errMsg, "model") && strings.Contains(errMsg, "not found"):
			available = false
			message = fmt.Sprintf("mô hình không tồn tại, hãy kiểm tra tên mô hình: %s", errMsg)
		default:
			logger.Infof(ctx, "ASR check got non-fatal error (endpoint reachable): %v", err)
			available = true
			message = fmt.Sprintf("endpoint ASR truy cập được (lỗi không nghiêm trọng: %s)", errMsg)
		}
	} else if text != "" {
		message = fmt.Sprintf("kết nối ASR thành công, kết quả chuyển văn bản: %s", text)
	}

	logger.Infof(ctx, "ASR model check completed, available: %v, message: %s", available, message)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": available,
			"message":   message,
		},
	})
}

// Parse form data into a struct
type testMultimodalForm struct {
	VLMModel         string `form:"vlm_model"`
	VLMBaseURL       string `form:"vlm_base_url"`
	VLMAPIKey        string `form:"vlm_api_key"`
	VLMInterfaceType string `form:"vlm_interface_type"`

	StorageType string `form:"storage_type"`

	// COS Configuration
	COSSecretID   string `form:"cos_secret_id"`
	COSSecretKey  string `form:"cos_secret_key"`
	COSRegion     string `form:"cos_region"`
	COSBucketName string `form:"cos_bucket_name"`
	COSAppID      string `form:"cos_app_id"`
	COSPathPrefix string `form:"cos_path_prefix"`

	// MinIO config (when storage is minio)
	MinioBucketName string `form:"minio_bucket_name"`
	MinioPathPrefix string `form:"minio_path_prefix"`

	// Document splitting config (strings parsed later to avoid binding failures)
	ChunkSize     string `form:"chunk_size"`
	ChunkOverlap  string `form:"chunk_overlap"`
	SeparatorsRaw string `form:"separators"`
}

// TestMultimodalFunction godoc
// @Summary      Kiểm tra chức năng đa phương thức
// @Description  Tải ảnh lên để kiểm tra xử lý đa phương thức
// @Tags         Khởi tạo
// @Accept       multipart/form-data
// @Produce      json
// @Param        image             formData  file    true   "ảnh kiểm tra"
// @Param        vlm_model         formData  string  true   "tên mô hình VLM"
// @Param        vlm_base_url      formData  string  true   "VLM Base URL"
// @Param        vlm_api_key       formData  string  false  "VLM API Key"
// @Param        vlm_interface_type formData string  false  "loại giao diện VLM"
// @Param        storage_type      formData  string  true   "loại lưu trữ (cos/minio)"
// @Success      200               {object}  map[string]interface{}  "kết quả kiểm tra"
// @Failure      400               {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/multimodal/test [post]
func (h *InitializationHandler) TestMultimodalFunction(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Testing multimodal functionality")

	var req testMultimodalForm
	if err := c.ShouldBind(&req); err != nil {
		logger.Error(ctx, "Failed to parse form data", err)
		c.Error(errors.NewBadRequestError("phân tích tham số biểu mẫu thất bại"))
		return
	}
	// Build the base url automatically for ollama
	if req.VLMInterfaceType == "ollama" {
		req.VLMBaseURL = os.Getenv("OLLAMA_BASE_URL") + "/v1"
	}

	req.StorageType = strings.ToLower(req.StorageType)

	if req.VLMModel == "" || req.VLMBaseURL == "" {
		logger.Error(ctx, "VLM model name and base URL are required")
		c.Error(errors.NewBadRequestError("thiếu tên mô hình VLM hoặc Base URL"))
		return
	}

	// SSRF validation for VLM BaseURL
	if err := utils.ValidateURLForSSRF(req.VLMBaseURL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for VLM BaseURL: %v", err)
		c.Error(errors.NewBadRequestError(utils.FormatSSRFError("VLM Base URL", req.VLMBaseURL, err)))
		return
	}

	switch req.StorageType {
	case "cos":
		// Required: SecretID/SecretKey/Region/BucketName/AppID; PathPrefix optional
		if req.COSSecretID == "" || req.COSSecretKey == "" ||
			req.COSRegion == "" || req.COSBucketName == "" ||
			req.COSAppID == "" {
			logger.Error(ctx, "COS configuration is required")
			c.Error(errors.NewBadRequestError("thiếu thông tin cấu hình COS"))
			return
		}
	case "minio":
		if req.MinioBucketName == "" {
			logger.Error(ctx, "MinIO configuration is required")
			c.Error(errors.NewBadRequestError("thiếu thông tin cấu hình MinIO"))
			return
		}
	default:
		logger.Error(ctx, "Invalid storage type")
		c.Error(errors.NewBadRequestError("loại lưu trữ không hợp lệ"))
		return
	}

	// Validate file size: MAX_FILE_SIZE_MB env (default 50MB).
	// See the note in utils/filesize.go: deliberately a deploy-time env, not a runtime setting.
	maxSizeMB := utils.GetMaxFileSizeMB()
	maxSize := maxSizeMB * 1024 * 1024
	// The limit must apply before multipart parsing: FormFile buffers the whole body first
	// (spilling to temp files beyond memory), so the later header.Size check sees a fully received upload.
	// nginx location /api/ still enforces MAX_FILE_SIZE; this is the same limit when hitting the app directly.
	limitUploadBody(c, maxSize)

	// Get the uploaded image file
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		if isRequestBodyTooLarge(err) {
			logger.Error(ctx, "File size too large")
			c.Error(errors.NewBadRequestError(fmt.Sprintf("kích thước ảnh không được vượt quá %dMB", maxSizeMB)))
			return
		}
		logger.Error(ctx, "Failed to get uploaded image", err)
		c.Error(errors.NewBadRequestError("không đọc được ảnh tải lên"))
		return
	}
	defer file.Close()

	// Validate file type
	if !strings.HasPrefix(header.Header.Get("Content-Type"), "image/") {
		logger.Error(ctx, "Invalid file type, only images are allowed")
		c.Error(errors.NewBadRequestError("chỉ cho phép tải lên tệp ảnh"))
		return
	}

	if header.Size > maxSize {
		logger.Error(ctx, "File size too large")
		c.Error(errors.NewBadRequestError(fmt.Sprintf("kích thước ảnh không được vượt quá %dMB", maxSizeMB)))
		return
	}
	logger.Infof(ctx, "Processing image: %s", utils.SanitizeForLog(header.Filename))

	// Parse the document splitting config
	chunkSizeInt32, err := strconv.ParseInt(req.ChunkSize, 10, 32)
	if err != nil {
		logger.Error(ctx, "Failed to parse chunk size", err)
		c.Error(errors.NewBadRequestError("Failed to parse chunk size"))
		return
	}
	chunkSize := int32(chunkSizeInt32)
	if chunkSize < 100 || chunkSize > 10000 {
		chunkSize = 1000
	}

	chunkOverlapInt32, err := strconv.ParseInt(req.ChunkOverlap, 10, 32)
	if err != nil {
		logger.Error(ctx, "Failed to parse chunk overlap", err)
		c.Error(errors.NewBadRequestError("Failed to parse chunk overlap"))
		return
	}
	chunkOverlap := int32(chunkOverlapInt32)
	if chunkOverlap < 0 || chunkOverlap >= chunkSize {
		chunkOverlap = 200
	}

	var separators []string
	if req.SeparatorsRaw != "" {
		if err := json.Unmarshal([]byte(req.SeparatorsRaw), &separators); err != nil {
			separators = []string{"\n\n", "\n", "。", "！", "？", ";", "；"}
		}
	} else {
		separators = []string{"\n\n", "\n", "。", "！", "？", ";", "；"}
	}

	// Read the image file content
	imageContent, err := io.ReadAll(file)
	if err != nil {
		logger.Error(ctx, "Failed to read image file", err)
		c.Error(errors.NewBadRequestError("đọc tệp ảnh thất bại"))
		return
	}

	// Run the multimodal test
	startTime := time.Now()
	result, err := h.testMultimodalWithDocReader(
		ctx,
		imageContent, header.Filename,
		chunkSize, chunkOverlap, separators, &req,
	)
	processingTime := time.Since(startTime).Milliseconds()

	if err != nil {
		logger.Error(ctx, "Failed to test multimodal", err)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"success":         false,
				"message":         err.Error(),
				"processing_time": processingTime,
			},
		})
		return
	}

	logger.Infof(ctx, "Multimodal test completed successfully in %dms", processingTime)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"success":         true,
			"caption":         result["caption"],
			"ocr":             result["ocr"],
			"processing_time": processingTime,
		},
	})
}

// testMultimodalWithDocReader uses DocumentReader.Read for document reading,
// then returns basic information about the result.
func (h *InitializationHandler) testMultimodalWithDocReader(
	ctx context.Context,
	imageContent []byte, filename string,
	chunkSize, chunkOverlap int32, separators []string,
	req *testMultimodalForm,
) (map[string]string, error) {
	fileExt := ""
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		fileExt = strings.ToLower(filename[idx+1:])
	}

	if h.documentReader == nil {
		return nil, fmt.Errorf("DocReader service not configured")
	}

	requestID, _ := types.RequestIDFromContext(ctx)

	readResult, err := h.documentReader.Read(ctx, &types.ReadRequest{
		FileContent: imageContent,
		FileName:    filename,
		FileType:    fileExt,
		RequestID:   requestID,
	})
	if err != nil {
		return nil, fmt.Errorf("gọi dịch vụ DocReader thất bại: %v", err)
	}
	if readResult.Error != "" {
		return nil, fmt.Errorf("dịch vụ DocReader trả về lỗi: %s", readResult.Error)
	}

	result := map[string]string{
		"markdown": readResult.MarkdownContent,
		"caption":  "",
		"ocr":      "",
	}
	return result, nil
}

// TextRelationExtractionRequest is the text relation extraction request body
type TextRelationExtractionRequest struct {
	Text    string   `json:"text"     binding:"required"`
	Tags    []string `json:"tags"     binding:"required"`
	ModelID string   `json:"model_id" binding:"required"`
}

// TextRelationExtractionResponse is the text relation extraction response body
type TextRelationExtractionResponse struct {
	Nodes     []*types.GraphNode     `json:"nodes"`
	Relations []*types.GraphRelation `json:"relations"`
}

// ExtractTextRelations godoc
// @Summary      Trích xuất quan hệ từ văn bản
// @Description  Trích xuất thực thể và quan hệ từ văn bản
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        request  body      TextRelationExtractionRequest  true  "yêu cầu trích xuất"
// @Success      200      {object}  map[string]interface{}         "kết quả trích xuất"
// @Failure      400      {object}  errors.AppError                "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/extract/text-relation [post]
func (h *InitializationHandler) ExtractTextRelations(c *gin.Context) {
	ctx := c.Request.Context()

	var req TextRelationExtractionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "invalid text relation extraction request")
		c.Error(errors.NewBadRequestError("tham số yêu cầu trích xuất quan hệ không hợp lệ"))
		return
	}

	// Validate the text
	if len(req.Text) == 0 {
		c.Error(errors.NewBadRequestError("nội dung văn bản không được để trống"))
		return
	}

	if len(req.Text) > 5000 {
		c.Error(errors.NewBadRequestError("nội dung văn bản không được vượt quá 5000 ký tự"))
		return
	}

	// Validate the tags
	if len(req.Tags) == 0 {
		c.Error(errors.NewBadRequestError("cần chọn ít nhất một nhãn quan hệ"))
		return
	}

	// Load the chat model by ID
	chatModel, err := h.modelService.GetChatModel(ctx, req.ModelID)
	if err != nil {
		logger.Error(ctx, "failed to get model", err)
		c.Error(errors.NewBadRequestError("lấy mô hình thất bại: " + err.Error()))
		return
	}

	// Call the model to extract text relations
	result, err := h.extractRelationsFromText(ctx, req.Text, req.Tags, chatModel)
	if err != nil {
		logger.Error(ctx, "text relation extraction failed", err)
		c.Error(errors.NewInternalServerError("trích xuất quan hệ văn bản thất bại: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// extractRelationsFromText extracts relations from text
func (h *InitializationHandler) extractRelationsFromText(
	ctx context.Context,
	text string,
	tags []string,
	chatModel chat.Chat,
) (*TextRelationExtractionResponse, error) {
	template := &types.PromptTemplateStructured{
		Description: h.config.ExtractManager.ExtractGraph.Description,
		Tags:        tags,
		Examples:    h.config.ExtractManager.ExtractGraph.Examples,
	}

	extractor := chatpipeline.NewExtractor(chatModel, template)
	graph, err := extractor.Extract(ctx, text)
	if err != nil {
		logger.Error(ctx, "text relation extraction failed", err)
		return nil, err
	}
	extractor.RemoveUnknownRelation(ctx, graph)

	result := &TextRelationExtractionResponse{
		Nodes:     graph.Node,
		Relations: graph.Relation,
	}

	return result, nil
}

// FabriTextRequest is a request for generating example text
type FabriTextRequest struct {
	Tags    []string `json:"tags"`
	ModelID string   `json:"model_id" binding:"required"`
}

// FabriTextResponse is a response for generating example text
type FabriTextResponse struct {
	Text string `json:"text"`
}

// FabriText godoc
// @Summary      Sinh văn bản mẫu
// @Description  Sinh văn bản mẫu theo nhãn
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Param        request  body      FabriTextRequest  true  "yêu cầu sinh"
// @Success      200      {object}  map[string]interface{}  "văn bản đã sinh"
// @Failure      400      {object}  errors.AppError         "tham số không hợp lệ"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /initialization/extract/fabri-text [post]
func (h *InitializationHandler) FabriText(c *gin.Context) {
	ctx := c.Request.Context()

	var req FabriTextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "failed to parse fabri text request")
		c.Error(errors.NewBadRequestError("invalid fabri text request parameters"))
		return
	}

	chatModel, err := h.modelService.GetChatModel(ctx, req.ModelID)
	if err != nil {
		logger.Error(ctx, "failed to get model", err)
		c.Error(errors.NewBadRequestError("lấy mô hình thất bại: " + err.Error()))
		return
	}

	result, err := h.fabriText(ctx, req.Tags, chatModel)
	if err != nil {
		logger.Error(ctx, "failed to generate fabri text", err)
		c.Error(errors.NewInternalServerError("failed to generate fabri text: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    FabriTextResponse{Text: result},
	})
}

// fabriText generates example text
func (h *InitializationHandler) fabriText(ctx context.Context, tags []string, chatModel chat.Chat) (string, error) {
	content := h.config.ExtractManager.FabriText.WithNoTag
	if len(tags) > 0 {
		tagStr, _ := json.Marshal(tags)
		content = fmt.Sprintf(h.config.ExtractManager.FabriText.WithTag, string(tagStr))
	}

	think := false
	result, err := chatModel.Chat(ctx, []chat.Message{
		{Role: "user", Content: content},
	}, &chat.ChatOptions{
		Temperature: 0.3,
		MaxTokens:   4096,
		Thinking:    &think,
	})
	if err != nil {
		logger.Error(ctx, "failed to generate sample text", err)
		return "", err
	}
	return result.Content, nil
}

// FabriTagRequest is a request for generating tags
type FabriTagRequest struct{}

// FabriTagResponse is a response for generating tags
type FabriTagResponse struct {
	Tags []string `json:"tags"`
}

var tagOptions = []string{
	"Content", "Culture", "Person", "Event", "Time", "Location",
	"Work", "Author", "Relation", "Attribute",
}

// FabriTag godoc
// @Summary      Sinh nhãn ngẫu nhiên
// @Description  Sinh ngẫu nhiên một bộ nhãn
// @Tags         Khởi tạo
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "nhãn đã sinh"
// @Router       /initialization/extract/fabri-tag [post]
func (h *InitializationHandler) FabriTag(c *gin.Context) {
	tagRandom := RandomSelect(tagOptions, rand.Intn(len(tagOptions)-1)+1)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    FabriTagResponse{Tags: tagRandom},
	})
}

// RandomSelect selects random strings
func RandomSelect(strs []string, n int) []string {
	if n <= 0 {
		return []string{}
	}
	result := make([]string, len(strs))
	copy(result, strs)
	rand.Shuffle(len(result), func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})

	if n > len(strs) {
		n = len(strs)
	}
	return result[:n]
}
