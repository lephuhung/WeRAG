package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// Bounds for LLMChat. The endpoint is a plain completion surface for
// integrations (e.g. the docformat checker labelling document components),
// so requests stay small and text-only: no tools, no images, no streaming.
const (
	llmChatMaxMessages     = 50
	llmChatMaxContentChars = 200_000
	llmChatDefaultMaxToks  = 4096
	llmChatMaxMaxToks      = 16384
)

// LLMChatMessage is one text-only chat turn.
type LLMChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMChatRequest asks a workspace chat model for one completion.
type LLMChatRequest struct {
	// ModelID selects a chat (KnowledgeQA) model of the workspace; empty or
	// "default" uses the workspace default chat model, so integrations follow
	// whatever model the workspace is switched to.
	ModelID     string           `json:"model_id"`
	Messages    []LLMChatMessage `json:"messages"`
	Temperature *float64         `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
	// Thinking defaults to off: integrations want the answer, and reasoning
	// models are several times slower with it on.
	Thinking *bool `json:"thinking"`
	// JSON asks the model for a single JSON object.
	JSON bool `json:"json"`
}

// LLMChatResponse is the completion; credentials and connection settings of
// the model are never part of it.
type LLMChatResponse struct {
	ModelID      string           `json:"model_id"`
	ModelName    string           `json:"model_name"`
	Content      string           `json:"content"`
	FinishReason string           `json:"finish_reason,omitempty"`
	Usage        types.TokenUsage `json:"usage"`
}

func (r *LLMChatRequest) validate() string {
	if len(r.Messages) == 0 || len(r.Messages) > llmChatMaxMessages {
		return "messages must contain 1-50 entries"
	}
	total := 0
	for _, m := range r.Messages {
		switch m.Role {
		case "system", "user", "assistant":
		default:
			return "message role must be system, user or assistant"
		}
		total += len(m.Content)
	}
	if total > llmChatMaxContentChars {
		return "messages are too large"
	}
	if r.Temperature != nil && (*r.Temperature < 0 || *r.Temperature > 2) {
		return "temperature must be between 0 and 2"
	}
	if r.MaxTokens < 0 || r.MaxTokens > llmChatMaxMaxToks {
		return "max_tokens must be between 0 and 16384"
	}
	return ""
}

// selectChatModel returns the active chat model with the given id, or the
// workspace default chat model (first active one when none is flagged
// default) for an empty / "default" id.
func selectChatModel(models []*types.Model, id string) *types.Model {
	byDefault := id == "" || id == "default"
	var selected *types.Model
	for _, m := range models {
		if m == nil || m.Type != types.ModelTypeKnowledgeQA || m.Status != types.ModelStatusActive {
			continue
		}
		if !byDefault {
			if m.ID == id {
				return m
			}
			continue
		}
		if selected == nil || (m.IsDefault && !selected.IsDefault) {
			selected = m
		}
	}
	return selected
}

// LLMChat godoc
// @Summary      Chat completion with a workspace chat model
// @Description  Runs one text completion on the workspace default chat model
// @Description  (or model_id). Model credentials stay server-side.
// @Tags         模型管理
// @Accept       json
// @Produce      json
// @Param        request  body      LLMChatRequest  true  "messages"
// @Success      200      {object}  map[string]interface{}
// @Failure      400      {object}  errors.AppError
// @Failure      503      {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /llm/chat [post]
func (h *ModelHandler) LLMChat(c *gin.Context) {
	ctx := c.Request.Context()
	var req LLMChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequestError("invalid request body"))
		return
	}
	if msg := req.validate(); msg != "" {
		_ = c.Error(errors.NewBadRequestError(msg))
		return
	}

	models, err := h.service.ListModels(ctx)
	if err != nil {
		_ = c.Error(errors.NewInternalServerError("Failed to read chat models"))
		return
	}
	selected := selectChatModel(models, strings.TrimSpace(req.ModelID))
	if selected == nil {
		if req.ModelID == "" || req.ModelID == "default" {
			_ = c.Error(errors.NewBadRequestError("Configure an active chat model for this workspace"))
		} else {
			_ = c.Error(errors.NewBadRequestError("model_id is not an active chat model of this workspace"))
		}
		return
	}
	model, err := h.service.GetChatModel(ctx, selected.ID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"model_id": selected.ID})
		_ = c.Error(errors.NewServiceUnavailableError("Chat model is unavailable"))
		return
	}

	messages := make([]chat.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, chat.Message{Role: m.Role, Content: m.Content})
	}
	thinking := false
	if req.Thinking != nil {
		thinking = *req.Thinking
	}
	opts := &chat.ChatOptions{MaxTokens: req.MaxTokens, Thinking: &thinking}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = llmChatDefaultMaxToks
	}
	if req.Temperature != nil {
		opts.Temperature = *req.Temperature
	}
	if req.JSON {
		opts.Format = json.RawMessage(`{"type":"object"}`)
	}

	result, err := model.Chat(ctx, messages, opts)
	if err != nil || result == nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"model_id": selected.ID})
		_ = c.Error(errors.NewServiceUnavailableError("Chat model call failed; try again"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": LLMChatResponse{
		ModelID:      selected.ID,
		ModelName:    selected.Name,
		Content:      result.Content,
		FinishReason: result.FinishReason,
		Usage:        result.Usage,
	}})
}
