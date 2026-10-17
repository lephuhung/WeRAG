package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Vision-model extra_config keys (models.parameters.extra_config) that tune
// how the OCR paths talk to a model.
const (
	// ModelExtraOCRPrompt replaces the built-in OCR prompt. OCR-only models
	// are trained on one short prompt (senocr-vi: "OCR:"); given the long
	// instruction prompt they recite it, or loop, on an image without text.
	ModelExtraOCRPrompt = "ocr_prompt"
	// ModelExtraOCROnly ("true") marks a model that only reads text: no
	// caption or description is ever asked of it, since the answer would be
	// more text, invented.
	ModelExtraOCROnly = "ocr_only"
)

// ocrModelProfile is what the OCR paths adapt to a vision model.
type ocrModelProfile struct {
	// Prompt is the OCR prompt to send instead of the built-in one; "" keeps it.
	Prompt string
	// OCROnly skips caption/description calls.
	OCROnly bool
}

// loadOCRModelProfile reads the model's OCR settings; a model that cannot
// be loaded or has none yields the zero profile (built-in behaviour).
func loadOCRModelProfile(ctx context.Context, models interfaces.ModelService, modelID string) ocrModelProfile {
	if models == nil || strings.TrimSpace(modelID) == "" {
		return ocrModelProfile{}
	}
	model, err := models.GetModelByID(ctx, modelID)
	if err != nil || model == nil {
		return ocrModelProfile{}
	}
	extra := model.Parameters.ExtraConfig
	return ocrModelProfile{
		Prompt:  strings.TrimSpace(extra[ModelExtraOCRPrompt]),
		OCROnly: isTrueString(extra[ModelExtraOCROnly]),
	}
}

// ocrPrompt returns the model's prompt when it has one, else fallback.
func (p ocrModelProfile) ocrPrompt(fallback string) string {
	if p.Prompt != "" {
		return p.Prompt
	}
	return fallback
}

// isTrueString reads a boolean written into extra_config ("true", "1", "yes").
func isTrueString(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}
