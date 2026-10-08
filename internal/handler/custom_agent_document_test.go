package handler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type docAgentModels struct {
	interfaces.ModelService
	models map[string]*types.Model
}

func (m docAgentModels) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	if model, ok := m.models[id]; ok {
		return model, nil
	}
	return nil, errors.New("not found")
}

func TestValidateDocumentAssistantConfig(t *testing.T) {
	h := &CustomAgentHandler{models: docAgentModels{models: map[string]*types.Model{
		"chat":   {ID: "chat", Type: types.ModelTypeKnowledgeQA},
		"embed":  {ID: "embed", Type: types.ModelTypeEmbedding},
		"rerank": {ID: "rerank", Type: types.ModelTypeRerank},
	}}}
	ctx := context.Background()
	for name, c := range map[string]struct {
		cfg  types.CustomAgentConfig
		want string // "" = valid
	}{
		"empty":            {types.CustomAgentConfig{}, ""},
		"chat models":      {types.CustomAgentConfig{FormatCheckModelID: "chat", SpellcheckModelID: "chat"}, ""},
		"embedding":        {types.CustomAgentConfig{FormatCheckModelID: "embed"}, "format_check_model_id"},
		"rerank":           {types.CustomAgentConfig{SpellcheckModelID: "rerank"}, "spellcheck_model_id"},
		"unknown":          {types.CustomAgentConfig{SpellcheckModelID: "nope"}, "not found"},
		"runes too large":  {types.CustomAgentConfig{OpenDocumentMaxRunesLimit: types.MaxOpenDocumentRunes + 1}, "open_document_max_runes"},
		"runes negative":   {types.CustomAgentConfig{OpenDocumentMaxRunesLimit: -5}, "open_document_max_runes"},
		"runes in range":   {types.CustomAgentConfig{OpenDocumentMaxRunesLimit: 12000}, ""},
		"blank id ignored": {types.CustomAgentConfig{FormatCheckModelID: "  "}, ""},
	} {
		err := h.validateDocumentAssistantConfig(ctx, c.cfg)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
	// without a model service only the static checks run
	if err := (&CustomAgentHandler{}).validateDocumentAssistantConfig(ctx, types.CustomAgentConfig{FormatCheckModelID: "x"}); err != nil {
		t.Fatal(err)
	}
}
