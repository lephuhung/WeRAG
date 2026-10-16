package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// spellingSupport gives the document assistant's spellcheck its verifier
// (the auxiliary model, asked again about findings on real words) and the
// users' "not a mistake" verdicts. The zero value adds neither.
type spellingSupport struct {
	aux      auxiliaryModels
	models   interfaces.ModelService
	feedback interfaces.DocumentSpellingFeedbackRepository
}

// verifier loads the auxiliary model, nil when none is configured or it
// cannot be loaded (the findings are then kept unverified).
func (s spellingSupport) verifier(ctx context.Context) chat.Chat {
	id := s.aux.modelID(ctx)
	if id == "" || s.models == nil {
		return nil
	}
	m, err := s.models.GetChatModel(ctx, id)
	if err != nil || m == nil {
		logger.Warnf(ctx, "spellcheck verifier model %s unavailable: %v", id, err)
		return nil
	}
	return m
}

func (s spellingSupport) apply(ctx context.Context, t *tools.CheckSpellingTool) *tools.CheckSpellingTool {
	return t.WithVerifier(s.verifier(ctx)).WithFeedback(s.feedback)
}

// SetSpellingSupport gives check_spelling its verifier and verdict store.
func (s *agentService) SetSpellingSupport(settings interfaces.SystemSettingService,
	feedback interfaces.DocumentSpellingFeedbackRepository,
) {
	s.spelling = spellingSupport{aux: auxiliaryModels{settings: settings}, models: s.modelService, feedback: feedback}
}

// SetSpellingSupport gives the live spellcheck its verifier and verdict
// store.
func (p *DocumentFormatPrecheck) SetSpellingSupport(settings interfaces.SystemSettingService,
	feedback interfaces.DocumentSpellingFeedbackRepository,
) {
	p.spelling = spellingSupport{aux: auxiliaryModels{settings: settings}, models: p.models, feedback: feedback}
}
