package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// auxiliaryModels resolves the platform's small model for low-risk side
// tasks (types.AuxiliaryModelSettingKey). The zero value resolves nothing,
// so those tasks keep using the answering model.
type auxiliaryModels struct {
	settings interfaces.SystemSettingService
}

// modelID is the auxiliary model, or "" when none is configured.
func (a auxiliaryModels) modelID(ctx context.Context) string {
	if a.settings == nil {
		return ""
	}
	return strings.TrimSpace(a.settings.GetString(ctx, types.AuxiliaryModelSettingKey,
		"WEKNORA_AUXILIARY_MODEL_ID", types.DefaultAuxiliaryModelID))
}

// SetAuxiliaryModels lets session titles use the auxiliary model.
func (s *sessionService) SetAuxiliaryModels(settings interfaces.SystemSettingService) {
	s.aux = auxiliaryModels{settings: settings}
}

// SetAuxiliaryModels lets follow-up suggestions use the auxiliary model.
func (s *messageSuggestionService) SetAuxiliaryModels(settings interfaces.SystemSettingService) {
	s.aux = auxiliaryModels{settings: settings}
}
