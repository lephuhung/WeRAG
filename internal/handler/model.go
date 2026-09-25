package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler/dto"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// ModelHandler handles HTTP requests for model-related operations
// It implements the necessary methods to create, retrieve, update, and delete models
type ModelHandler struct {
	service interfaces.ModelService
}

// NewModelHandler creates a new instance of ModelHandler
// It requires a model service implementation that handles business logic
// Parameters:
//   - service: An implementation of the ModelService interface
//
// Returns a pointer to the newly created ModelHandler
func NewModelHandler(service interfaces.ModelService) *ModelHandler {
	return &ModelHandler{service: service}
}

// Per-response redaction/stripping for Model now lives in
// dto.NewModelResponse — handlers must use it for every body that contains a
// model. The previous hideSensitiveInfo helper has been removed.

// CreateModelRequest defines the structure for model creation requests
// Contains all fields required to create a new model in the system
type CreateModelRequest struct {
	Name        string                `json:"name"        binding:"required"`
	DisplayName string                `json:"display_name"`
	Type        types.ModelType       `json:"type"        binding:"required"`
	Source      types.ModelSource     `json:"source"      binding:"required"`
	Description string                `json:"description"`
	Parameters  types.ModelParameters `json:"parameters"  binding:"required"`
}

// CreateModel godoc
// @Summary      Create 模型
// @Description  Create 新的模型Configuration
// @Tags         模型管理
// @Accept       json
// @Produce      json
// @Param        request  body      CreateModelRequest  true  "模型信息"
// @Success      201      {object}  map[string]interface{}  "Create 的模型"
// @Failure      400      {object}  errors.AppError         "请求Parameters 错误"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models [post]
func (h *ModelHandler) CreateModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start creating model")

	var req CreateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		logger.Error(ctx, "Tenant ID is empty")
		c.Error(errors.NewBadRequestError("Workspace ID cannot be empty"))
		return
	}

	logger.Infof(ctx, "Creating model, Tenant ID: %d, Model name: %s, Model type: %s",
		tenantID, secutils.SanitizeForLog(req.Name), secutils.SanitizeForLog(string(req.Type)))

	// SSRF validation for model BaseURL
	if req.Parameters.BaseURL != "" {
		if err := secutils.ValidateURLForSSRF(req.Parameters.BaseURL); err != nil {
			logger.Warnf(ctx, "SSRF validation failed for model BaseURL: %v", err)
			c.Error(errors.NewBadRequestError(secutils.FormatSSRFError("Base URL", req.Parameters.BaseURL, err)))
			return
		}
	}

	model := &types.Model{
		TenantID:    tenantID,
		Name:        secutils.SanitizeForLog(req.Name),
		DisplayName: secutils.SanitizeForLog(req.DisplayName),
		Type:        types.ModelType(secutils.SanitizeForLog(string(req.Type))),
		Source:      req.Source,
		Description: secutils.SanitizeForLog(req.Description),
		Parameters:  req.Parameters,
	}

	if err := h.service.CreateModel(ctx, model); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(
		ctx,
		"Model created successfully, ID: %s, Name: %s",
		secutils.SanitizeForLog(model.ID),
		secutils.SanitizeForLog(model.Name),
	)

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    dto.NewModelResponse(ctx, model),
	})
}

// GetModel godoc
// @Summary      获取模型Details
// @Description  根据ID获取模型Details
// @Tags         模型管理
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "模型ID"
// @Success      200  {object}  map[string]interface{}  "模型Details "
// @Failure      404  {object}  errors.AppError         "模型不存在"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models/{id} [get]
func (h *ModelHandler) GetModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start retrieving model")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Model ID is empty")
		c.Error(errors.NewBadRequestError("Model ID cannot be empty"))
		return
	}

	logger.Infof(ctx, "Retrieving model, ID: %s", id)
	model, err := h.service.GetModelByID(ctx, id)
	if err != nil {
		if err == service.ErrModelNotFound {
			logger.Warnf(ctx, "Model not found, ID: %s", id)
			c.Error(errors.NewNotFoundError("Model not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Retrieved model successfully, ID: %s, Name: %s", model.ID, model.Name)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dto.NewModelResponse(ctx, model),
	})
}

// ListModels godoc
// @Summary      获取模型List
// @Description  获取当前Tenant workspace的所有模型
// @Tags         模型管理
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "模型List "
// @Failure      400  {object}  errors.AppError         "请求Parameters 错误"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models [get]
func (h *ModelHandler) ListModels(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start retrieving model list")

	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		logger.Error(ctx, "Tenant ID is empty")
		c.Error(errors.NewBadRequestError("Workspace ID cannot be empty"))
		return
	}

	models, err := h.service.ListModels(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Retrieved model list successfully, Tenant ID: %d, Total: %d models", tenantID, len(models))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dto.NewModelResponses(ctx, models),
	})
}

const modelDebugMaxInputBytes = 64 * 1024

// ModelDebugOptions contains the cross-provider parameters exposed by the
// model debugger. Pointer fields preserve explicit zero/false values.
type ModelDebugOptions struct {
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	TopP         *float64 `json:"top_p,omitempty"`
	MaxTokens    *int     `json:"max_tokens,omitempty"`
	Thinking     *bool    `json:"thinking,omitempty"`
}

func parseModelDebugOptions(raw string) (ModelDebugOptions, error) {
	var opts ModelDebugOptions
	if strings.TrimSpace(raw) == "" {
		return opts, nil
	}
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return opts, fmt.Errorf("invalid options: %w", err)
	}
	if opts.MaxTokens != nil && (*opts.MaxTokens < 1 || *opts.MaxTokens > 8192) {
		return opts, fmt.Errorf("max_tokens must be between 1 and 8192")
	}
	if opts.Temperature != nil && (*opts.Temperature < 0 || *opts.Temperature > 2) {
		return opts, fmt.Errorf("temperature must be between 0 and 2")
	}
	if opts.TopP != nil && (*opts.TopP <= 0 || *opts.TopP > 1) {
		return opts, fmt.Errorf("top_p must be greater than 0 and at most 1")
	}
	return opts, nil
}

func redactedDebugConfig(config map[string]string) map[string]string {
	if len(config) == 0 {
		return nil
	}
	out := make(map[string]string, len(config))
	for key, value := range config {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "secret") ||
			strings.Contains(lower, "token") ||
			strings.Contains(lower, "password") ||
			strings.Contains(lower, "api_key") ||
			strings.Contains(lower, "apikey") ||
			strings.Contains(lower, "authorization") {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = value
	}
	return out
}

func modelDebugRequestPreview(model *types.Model, input string, documents []string, opts ModelDebugOptions, fileName string, fileSize int64) gin.H {
	preview := gin.H{
		"model_id":   model.ID,
		"model_name": model.Name,
		"model_type": model.Type,
		"source":     model.Source,
		"provider":   model.Parameters.Provider,
		"input":      input,
		"options":    opts,
	}
	if len(documents) > 0 {
		preview["documents"] = documents
	}
	if fileName != "" {
		preview["file"] = gin.H{"name": fileName, "size": fileSize}
	}
	if model.Parameters.ExtraConfig != nil {
		preview["model_extra_config"] = redactedDebugConfig(model.Parameters.ExtraConfig)
	}
	if len(model.Parameters.CustomHeaders) > 0 {
		headerNames := make([]string, 0, len(model.Parameters.CustomHeaders))
		for name := range model.Parameters.CustomHeaders {
			headerNames = append(headerNames, name)
		}
		preview["custom_header_names"] = headerNames
	}
	return preview
}

func writeModelDebugResult(c *gin.Context, started time.Time, request gin.H, response any, callErr error, observations gin.H) {
	data := gin.H{
		"ok":           callErr == nil,
		"elapsed_ms":   time.Since(started).Milliseconds(),
		"request":      request,
		"raw_response": response,
		"observations": observations,
	}
	if callErr != nil {
		data["error"] = callErr.Error()
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

type modelDebugChatStreamResponse struct {
	Content          string                 `json:"content"`
	ReasoningContent string                 `json:"reasoning_content,omitempty"`
	ToolCalls        []types.LLMToolCall    `json:"tool_calls,omitempty"`
	FinishReason     string                 `json:"finish_reason,omitempty"`
	Usage            *types.TokenUsage      `json:"usage,omitempty"`
	StreamEvents     []types.StreamResponse `json:"stream_events"`
}

func consumeModelDebugChatStream(stream <-chan types.StreamResponse) (*modelDebugChatStreamResponse, error) {
	result := &modelDebugChatStreamResponse{
		StreamEvents: make([]types.StreamResponse, 0),
	}
	for event := range stream {
		result.StreamEvents = append(result.StreamEvents, event)
		switch event.ResponseType {
		case types.ResponseTypeThinking:
			result.ReasoningContent += event.Content
		case types.ResponseTypeAnswer:
			result.Content += event.Content
			if len(event.ToolCalls) > 0 {
				result.ToolCalls = event.ToolCalls
			}
			if event.FinishReason != "" {
				result.FinishReason = event.FinishReason
			}
			if event.Usage != nil {
				result.Usage = event.Usage
			}
		case types.ResponseTypeToolCall:
			if len(event.ToolCalls) > 0 {
				result.ToolCalls = event.ToolCalls
			}
		case types.ResponseTypeError:
			return result, fmt.Errorf("%s", event.Content)
		}
	}
	return result, nil
}

// DebugModel executes a saved model through the same service constructors used
// by production calls and returns the complete normalized response. Credentials
// stay server-side; the request preview contains only non-secret fields.
func (h *ModelHandler) DebugModel(c *gin.Context) {
	ctx := c.Request.Context()
	started := time.Now()
	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Model ID cannot be empty"))
		return
	}

	model, err := h.service.GetModelByID(ctx, id)
	if err != nil {
		if err == service.ErrModelNotFound {
			c.Error(errors.NewNotFoundError("Model not found"))
			return
		}
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	// The optional file makes this a multipart body, so the cap has to be in
	// place before the first PostForm below — that call is what parses it, and
	// parsing is what buffers the upload to disk. Answer an oversized body here
	// too: the file is read with a `fileErr == nil` guard further down, which
	// would otherwise report a rejected upload as one that was never sent.
	limitUploadBody(c, secutils.GetMaxFileSize())
	if _, formErr := c.MultipartForm(); formErr != nil && isRequestBodyTooLarge(formErr) {
		c.Error(errors.NewBadRequestError(
			fmt.Sprintf("file cannot exceed %d MB", secutils.GetMaxFileSizeMB())))
		return
	}

	input := c.PostForm("input")
	if len(input) > modelDebugMaxInputBytes {
		c.Error(errors.NewBadRequestError("input is too long"))
		return
	}
	opts, err := parseModelDebugOptions(c.PostForm("options"))
	if err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	var documents []string
	if rawDocuments := c.PostForm("documents"); strings.TrimSpace(rawDocuments) != "" {
		if err := json.Unmarshal([]byte(rawDocuments), &documents); err != nil {
			c.Error(errors.NewBadRequestError("documents must be a JSON string array"))
			return
		}
		if len(documents) > 100 {
			c.Error(errors.NewBadRequestError("documents cannot exceed 100 items"))
			return
		}
	}

	var (
		fileBytes []byte
		fileName  string
		fileSize  int64
	)
	if file, header, fileErr := c.Request.FormFile("file"); fileErr == nil {
		defer file.Close()
		fileName = header.Filename
		fileSize = header.Size
		maxFileBytes := secutils.GetMaxFileSize()
		maxFileSizeMB := secutils.GetMaxFileSizeMB()
		if fileSize > maxFileBytes {
			c.Error(errors.NewBadRequestError(fmt.Sprintf("file cannot exceed %d MB", maxFileSizeMB)))
			return
		}
		fileBytes, err = io.ReadAll(io.LimitReader(file, maxFileBytes+1))
		if err != nil {
			c.Error(errors.NewBadRequestError("failed to read uploaded file"))
			return
		}
		if int64(len(fileBytes)) > maxFileBytes {
			c.Error(errors.NewBadRequestError(fmt.Sprintf("file cannot exceed %d MB", maxFileSizeMB)))
			return
		}
		fileSize = int64(len(fileBytes))
	}

	requestPreview := modelDebugRequestPreview(model, input, documents, opts, fileName, fileSize)
	observations := gin.H{}

	switch model.Type {
	case types.ModelTypeKnowledgeQA:
		if strings.TrimSpace(input) == "" {
			c.Error(errors.NewBadRequestError("query cannot be empty"))
			return
		}
		instance, callErr := h.service.GetChatModel(ctx, id)
		if callErr != nil {
			writeModelDebugResult(c, started, requestPreview, nil, callErr, observations)
			return
		}
		messages := make([]chat.Message, 0, 2)
		if strings.TrimSpace(opts.SystemPrompt) != "" {
			messages = append(messages, chat.Message{Role: "system", Content: opts.SystemPrompt})
		}
		messages = append(messages, chat.Message{Role: "user", Content: input})
		chatOpts := &chat.ChatOptions{}
		if opts.Temperature != nil {
			chatOpts.Temperature = *opts.Temperature
		}
		if opts.TopP != nil {
			chatOpts.TopP = *opts.TopP
		}
		if opts.MaxTokens != nil {
			chatOpts.MaxTokens = *opts.MaxTokens
		}
		chatOpts.Thinking = opts.Thinking
		chatConfig := chat.ConfigFromModel(model, "", "")
		thinkingControl := chat.EffectiveThinkingControl(chatConfig)
		observations["stream"] = true
		observations["requested_thinking"] = opts.Thinking != nil && *opts.Thinking
		observations["thinking_control"] = thinkingControl
		observations["thinking_parameter_sent"] = opts.Thinking != nil && thinkingControl != "none"

		stream, callErr := instance.ChatStream(ctx, messages, chatOpts)
		if callErr != nil {
			writeModelDebugResult(c, started, requestPreview, nil, callErr, observations)
			return
		}
		resp, callErr := consumeModelDebugChatStream(stream)
		if resp != nil {
			observations["reasoning_returned"] = strings.TrimSpace(resp.ReasoningContent) != ""
			observations["reasoning_characters"] = len([]rune(resp.ReasoningContent))
			observations["answer_characters"] = len([]rune(resp.Content))
		}
		writeModelDebugResult(c, started, requestPreview, resp, callErr, observations)
	case types.ModelTypeEmbedding:
		if strings.TrimSpace(input) == "" {
			c.Error(errors.NewBadRequestError("input cannot be empty"))
			return
		}
		instance, callErr := h.service.GetEmbeddingModel(ctx, id)
		if callErr != nil {
			writeModelDebugResult(c, started, requestPreview, nil, callErr, observations)
			return
		}
		vector, callErr := instance.Embed(ctx, input)
		observations["dimension"] = len(vector)
		writeModelDebugResult(c, started, requestPreview, vector, callErr, observations)
	case types.ModelTypeRerank:
		if strings.TrimSpace(input) == "" || len(documents) == 0 {
			c.Error(errors.NewBadRequestError("query and documents cannot be empty"))
			return
		}
		instance, callErr := h.service.GetRerankModel(ctx, id)
		if callErr != nil {
			writeModelDebugResult(c, started, requestPreview, nil, callErr, observations)
			return
		}
		results, callErr := instance.Rerank(ctx, input, documents)
		observations["result_count"] = len(results)
		writeModelDebugResult(c, started, requestPreview, results, callErr, observations)
	case types.ModelTypeVLLM:
		if len(fileBytes) == 0 {
			c.Error(errors.NewBadRequestError("image file is required"))
			return
		}
		instance, callErr := h.service.GetVLMModel(ctx, id)
		if callErr != nil {
			writeModelDebugResult(c, started, requestPreview, nil, callErr, observations)
			return
		}
		result, callErr := instance.Predict(ctx, [][]byte{fileBytes}, input)
		observations["answer_characters"] = len([]rune(result))
		writeModelDebugResult(c, started, requestPreview, result, callErr, observations)
	case types.ModelTypeASR:
		if len(fileBytes) == 0 {
			c.Error(errors.NewBadRequestError("audio file is required"))
			return
		}
		instance, callErr := h.service.GetASRModel(ctx, id)
		if callErr != nil {
			writeModelDebugResult(c, started, requestPreview, nil, callErr, observations)
			return
		}
		result, callErr := instance.Transcribe(ctx, fileBytes, fileName)
		if result != nil {
			observations["text_characters"] = len([]rune(result.Text))
			observations["segment_count"] = len(result.Segments)
		}
		writeModelDebugResult(c, started, requestPreview, result, callErr, observations)
	default:
		c.Error(errors.NewBadRequestError("unsupported model type"))
	}
}

// UpdateModelRequest defines the structure for model update requests
// Contains fields that can be updated for an existing model
type UpdateModelRequest struct {
	Name        string                `json:"name"`
	DisplayName *string               `json:"display_name"`
	Description string                `json:"description"`
	Parameters  types.ModelParameters `json:"parameters"`
	Source      types.ModelSource     `json:"source"`
	Type        types.ModelType       `json:"type"`
}

// cloneModelStringMap copies a string map, preserving nil, so merging into
// the copy never mutates the stored parameters on validation errors.
func cloneModelStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// mergeModelParameters overlays only the keys present in paramsRaw onto stored,
// so a partial PUT preserves unspecified fields — including nested maps and
// structs — while still honouring explicit false/0/empty values. Credentials
// (api_key, app_secret) are always preserved from stored; a differing
// non-empty credential in the body is reported via the returned attempt flag
// (the caller logs the deprecation warning) and never applied. Nested maps
// (extra_config, custom_headers) merge key-wise: supplied keys overlay the
// stored entries while unspecified stored keys are preserved. An explicit {}
// clears the whole map, an explicit null resets it to nil, and a per-key
// null (e.g. {"extra_config":{"stale":null}}) deletes that single entry —
// needed because map values are strings and there is otherwise no way to
// express "remove this key" without wiping the map. The stored maps are
// never mutated: the merge works on copies, so a validation error leaves the
// caller's value untouched. Unknown keys are ignored. A JSON type mismatch
// returns an error for a 400 response.
func mergeModelParameters(
	stored types.ModelParameters,
	paramsRaw json.RawMessage,
) (types.ModelParameters, bool, error) {
	merged := stored
	// Deep-copy the nested maps so overlaying below never mutates the
	// caller's stored value — especially when a later key fails validation
	// and we return stored as-is.
	merged.ExtraConfig = cloneModelStringMap(stored.ExtraConfig)
	merged.CustomHeaders = cloneModelStringMap(stored.CustomHeaders)
	if len(paramsRaw) == 0 || strings.TrimSpace(string(paramsRaw)) == "null" {
		return merged, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(paramsRaw, &fields); err != nil {
		return stored, false, fmt.Errorf("invalid parameters: %w", err)
	}
	isNull := func(raw json.RawMessage) bool {
		return strings.TrimSpace(string(raw)) == "null"
	}
	applyString := func(key string, dst *string) error {
		raw, ok := fields[key]
		if !ok {
			return nil
		}
		if isNull(raw) {
			*dst = ""
			return nil
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("invalid %s: %w", key, err)
		}
		*dst = s
		return nil
	}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"base_url", &merged.BaseURL},
		{"provider", &merged.Provider},
		{"interface_type", &merged.InterfaceType},
		{"parameter_size", &merged.ParameterSize},
		{"app_id", &merged.AppID},
	} {
		if err := applyString(f.key, f.dst); err != nil {
			return stored, false, err
		}
	}
	if raw, ok := fields["supports_vision"]; ok {
		if isNull(raw) {
			merged.SupportsVision = false
		} else {
			var b bool
			if err := json.Unmarshal(raw, &b); err != nil {
				return stored, false, fmt.Errorf("invalid supports_vision: %w", err)
			}
			merged.SupportsVision = b
		}
	}
	for _, f := range []struct {
		key string
		dst *int
	}{
		{"context_window", &merged.ContextWindow},
		{"max_output_tokens", &merged.MaxOutputTokens},
		{"max_concurrency", &merged.MaxConcurrency},
	} {
		raw, ok := fields[f.key]
		if !ok {
			continue
		}
		if isNull(raw) {
			*f.dst = 0
			continue
		}
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return stored, false, fmt.Errorf("invalid %s: %w", f.key, err)
		}
		*f.dst = n
	}
	for _, f := range []struct {
		key string
		dst *map[string]string
	}{
		{"extra_config", &merged.ExtraConfig},
		{"custom_headers", &merged.CustomHeaders},
	} {
		raw, ok := fields[f.key]
		if !ok {
			continue
		}
		if isNull(raw) {
			*f.dst = nil
			continue
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return stored, false, fmt.Errorf("invalid %s: %w", f.key, err)
		}
		if len(entries) == 0 {
			*f.dst = map[string]string{}
			continue
		}
		overlay := *f.dst
		if overlay == nil {
			overlay = make(map[string]string, len(entries))
		}
		for k, v := range entries {
			if isNull(v) {
				delete(overlay, k)
				continue
			}
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return stored, false, fmt.Errorf("invalid %s.%s: %w", f.key, k, err)
			}
			overlay[k] = s
		}
		*f.dst = overlay
	}
	if raw, ok := fields["embedding_parameters"]; ok {
		if isNull(raw) {
			merged.EmbeddingParameters = types.EmbeddingParameters{}
		} else {
			var sub map[string]json.RawMessage
			if err := json.Unmarshal(raw, &sub); err != nil {
				return stored, false, fmt.Errorf("invalid embedding_parameters: %w", err)
			}
			if v, ok := sub["dimension"]; ok {
				if isNull(v) {
					merged.EmbeddingParameters.Dimension = 0
				} else if err := json.Unmarshal(v, &merged.EmbeddingParameters.Dimension); err != nil {
					return stored, false, fmt.Errorf("invalid embedding_parameters.dimension: %w", err)
				}
			}
			if v, ok := sub["truncate_prompt_tokens"]; ok {
				if isNull(v) {
					merged.EmbeddingParameters.TruncatePromptTokens = 0
				} else if err := json.Unmarshal(v, &merged.EmbeddingParameters.TruncatePromptTokens); err != nil {
					return stored, false, fmt.Errorf("invalid embedding_parameters.truncate_prompt_tokens: %w", err)
				}
			}
			if v, ok := sub["supports_dimension_override"]; ok {
				if isNull(v) {
					merged.EmbeddingParameters.SupportsDimensionOverride = false
				} else if err := json.Unmarshal(v, &merged.EmbeddingParameters.SupportsDimensionOverride); err != nil {
					return stored, false, fmt.Errorf("invalid embedding_parameters.supports_dimension_override: %w", err)
				}
			}
		}
	}
	credentialAttempt := false
	for _, f := range []struct {
		key    string
		stored string
	}{
		{"api_key", stored.APIKey},
		{"app_secret", stored.AppSecret},
	} {
		raw, ok := fields[f.key]
		if !ok || isNull(raw) {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return stored, false, fmt.Errorf("invalid %s: %w", f.key, err)
		}
		if s != "" && s != f.stored {
			credentialAttempt = true
		}
	}
	// Credentials always stay at their stored values.
	merged.APIKey = stored.APIKey
	merged.AppSecret = stored.AppSecret
	return merged, credentialAttempt, nil
}

// UpdateModel godoc
// @Summary      Update 模型
// @Description  Update 模型Configuration 信息
// @Tags         模型管理
// @Accept       json
// @Produce      json
// @Param        id       path      string              true  "模型ID"
// @Param        request  body      UpdateModelRequest  true  "Update 信息"
// @Success      200      {object}  map[string]interface{}  "Update 后的模型"
// @Failure      404      {object}  errors.AppError         "模型不存在"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models/{id} [put]
func (h *ModelHandler) UpdateModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start updating model")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Model ID is empty")
		c.Error(errors.NewBadRequestError("Model ID cannot be empty"))
		return
	}

	// Read the raw body so field presence can be distinguished from zero
	// values: an omitted "parameters"/"description" key must preserve the
	// stored value, while an explicit false/0/""/{} applies.
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Error(ctx, "Failed to read request body", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	var req UpdateModelRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	var rawBody map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawBody); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	logger.Infof(ctx, "Retrieving model information, ID: %s", id)
	model, err := h.service.GetModelByID(ctx, id)
	if err != nil {
		if err == service.ErrModelNotFound {
			logger.Warnf(ctx, "Model not found, ID: %s", id)
			c.Error(errors.NewNotFoundError("Model not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	// Update model fields if they are provided in the request
	if req.Name != "" {
		model.Name = req.Name
	}
	if req.DisplayName != nil {
		model.DisplayName = secutils.SanitizeForLog(*req.DisplayName)
	}
	if _, ok := rawBody["description"]; ok {
		model.Description = req.Description
	}

	// Presence-aware parameter merge: only keys present in the body overlay
	// the stored parameters. Credentials (api_key, app_secret) NEVER flow
	// through this endpoint — they live behind the /credentials subresource
	// and mergeModelParameters always preserves the stored values, so even
	// a misbehaving caller cannot clobber them. Log a warning to spot
	// stale callers.
	storedParams := model.Parameters
	if paramsRaw, hasParams := rawBody["parameters"]; hasParams {
		merged, credentialAttempt, err := mergeModelParameters(storedParams, paramsRaw)
		if err != nil {
			logger.Error(ctx, "Failed to parse model parameters", err)
			c.Error(errors.NewBadRequestError(err.Error()))
			return
		}
		if credentialAttempt {
			logger.Warnf(ctx,
				"deprecated: api_key/app_secret in PUT /models/%s body is ignored; use PUT /credentials instead", id)
		}
		// SSRF validation runs against the effective BaseURL, and only
		// when it actually changed.
		if merged.BaseURL != storedParams.BaseURL && merged.BaseURL != "" {
			if err := secutils.ValidateURLForSSRF(merged.BaseURL); err != nil {
				logger.Warnf(ctx, "SSRF validation failed for model BaseURL: %v", err)
				c.Error(errors.NewBadRequestError(secutils.FormatSSRFError("Base URL", merged.BaseURL, err)))
				return
			}
		}
		model.Parameters = merged
	}

	// Source/Type are presence-gated like the other preserved fields: an
	// update that omits them must not wipe the stored values (empty type
	// breaks agent resolution and model selectors downstream).
	if req.Source != "" {
		model.Source = req.Source
	}
	if req.Type != "" {
		if !req.Type.Valid() {
			c.Error(errors.NewBadRequestError(fmt.Sprintf("invalid model type %q", req.Type)))
			return
		}
		model.Type = req.Type
	}

	logger.Infof(ctx, "Updating model, ID: %s, Name: %s", id, model.Name)
	if err := h.service.UpdateModel(ctx, model); err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Model updated successfully, ID: %s", id)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dto.NewModelResponse(ctx, model),
	})
}

// DeleteModel godoc
// @Summary      Delete 模型
// @Description  Delete 指定的模型
// @Tags         模型管理
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "模型ID"
// @Success      200  {object}  map[string]interface{}  "Delete 成功"
// @Failure      400  {object}  errors.AppError         "模型仍被Knowledge Base、智能体或长期记忆引用"
// @Failure      404  {object}  errors.AppError         "模型不存在"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models/{id} [delete]
func (h *ModelHandler) DeleteModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start deleting model")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Model ID is empty")
		c.Error(errors.NewBadRequestError("Model ID cannot be empty"))
		return
	}

	logger.Infof(ctx, "Deleting model, ID: %s", id)
	if err := h.service.DeleteModel(ctx, id); err != nil {
		if err == service.ErrModelNotFound {
			logger.Warnf(ctx, "Model not found, ID: %s", id)
			c.Error(errors.NewNotFoundError("Model not found"))
			return
		}
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Model deleted successfully, ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Model deleted",
	})
}

// ModelProviderDTO 模型厂商信息 DTO
type ModelProviderDTO struct {
	Value       string            `json:"value"`       // provider 标识符
	Label       string            `json:"label"`       // 显示名称
	Description string            `json:"description"` // 描述
	DefaultURLs map[string]string `json:"defaultUrls"` // 按模型类型区分的默认 URL
	ModelTypes  []string          `json:"modelTypes"`  // 支持的模型类型
}

// modelTypeToFrontend 将后端 ModelType 转换为前端兼容的字符串
// KnowledgeQA -> chat, Embedding -> embedding, Rerank -> rerank, VLLM -> vllm
func modelTypeToFrontend(mt types.ModelType) string {
	switch mt {
	case types.ModelTypeKnowledgeQA:
		return "chat"
	case types.ModelTypeEmbedding:
		return "embedding"
	case types.ModelTypeRerank:
		return "rerank"
	case types.ModelTypeVLLM:
		return "vllm"
	case types.ModelTypeASR:
		return "asr"
	default:
		return string(mt)
	}
}

// ListModelProviders godoc
// @Summary      获取模型厂商List
// @Description  根据模型类型获取支持的厂商List 及Configuration 信息
// @Tags         模型管理
// @Accept       json
// @Produce      json
// @Param        model_type  query     string  false  "模型类型 (chat, embedding, rerank, vllm)"
// @Success      200         {object}  map[string]interface{}  "厂商List "
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /models/providers [get]
func (h *ModelHandler) ListModelProviders(c *gin.Context) {
	ctx := c.Request.Context()

	modelType := c.Query("model_type")
	logger.Infof(ctx, "Listing model providers for type: %s", secutils.SanitizeForLog(modelType))

	// 将前端类型映射到后端类型
	// 前端: chat, embedding, rerank, vllm
	// 后端: KnowledgeQA, Embedding, Rerank, VLLM
	var backendModelType types.ModelType
	switch modelType {
	case "chat":
		backendModelType = types.ModelTypeKnowledgeQA
	case "embedding":
		backendModelType = types.ModelTypeEmbedding
	case "rerank":
		backendModelType = types.ModelTypeRerank
	case "vllm":
		backendModelType = types.ModelTypeVLLM
	case "asr":
		backendModelType = types.ModelTypeASR
	default:
		backendModelType = types.ModelType(modelType)
	}

	var providers []provider.ProviderInfo
	if modelType != "" {
		// 按模型类型过滤
		providers = provider.ListByModelType(backendModelType)
	} else {
		// 返回所有 provider
		providers = provider.List()
	}

	// 转换为 DTO
	result := make([]ModelProviderDTO, 0, len(providers))
	for _, p := range providers {
		// 转换 DefaultURLs map[types.ModelType]string -> map[string]string
		// 使用前端兼容的 key (chat 而不是 KnowledgeQA)
		defaultURLs := make(map[string]string)
		for mt, url := range p.DefaultURLs {
			frontendType := modelTypeToFrontend(mt)
			defaultURLs[frontendType] = url
		}

		// 转换 ModelTypes 为前端兼容格式
		modelTypes := make([]string, 0, len(p.ModelTypes))
		for _, mt := range p.ModelTypes {
			modelTypes = append(modelTypes, modelTypeToFrontend(mt))
		}

		result = append(result, ModelProviderDTO{
			Value:       string(p.Name),
			Label:       p.DisplayName,
			Description: p.Description,
			DefaultURLs: defaultURLs,
			ModelTypes:  modelTypes,
		})
	}

	logger.Infof(ctx, "Retrieved %d providers", len(result))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
