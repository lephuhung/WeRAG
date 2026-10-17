package types

import (
	"context"
	"testing"
)

func TestApplyBuiltinAgentLocalizationOverlaysYAMLLocale(t *testing.T) {
	restore := OverrideBuiltinAgentEntriesForTest(map[string]*BuiltinAgentEntry{
		BuiltinQuickAnswerID: {
			ID:     BuiltinQuickAnswerID,
			Avatar: "quick.png",
			I18n: map[string]BuiltinAgentI18n{
				"default": {Name: "Quick Answer", Description: "Knowledge base RAG Q&A"},
				"vi":      {Name: "Hỏi đáp nhanh", Description: "Hỏi đáp RAG trên kho tri thức"},
			},
		},
	})
	t.Cleanup(restore)

	agent := &CustomAgent{
		ID:          BuiltinQuickAnswerID,
		Name:        "Quick Answer",
		Description: "Knowledge base RAG Q&A",
		Avatar:      "",
		IsBuiltin:   true,
		TenantID:    1,
	}
	ctx := context.WithValue(context.Background(), LanguageContextKey, "vi")
	ApplyBuiltinAgentLocalization(ctx, agent)

	if agent.Name != "Hỏi đáp nhanh" {
		t.Fatalf("Name = %q, want Hỏi đáp nhanh", agent.Name)
	}
	if agent.Description != "Hỏi đáp RAG trên kho tri thức" {
		t.Fatalf("Description = %q, want Vietnamese copy", agent.Description)
	}
	if agent.Avatar != "quick.png" {
		t.Fatalf("Avatar = %q, want quick.png", agent.Avatar)
	}
}

func TestApplyBuiltinAgentLocalizationNilSafe(t *testing.T) {
	ApplyBuiltinAgentLocalization(context.Background(), nil)
}

func TestApplyBuiltinAgentLocalizationLeavesUnknownAgents(t *testing.T) {
	agent := &CustomAgent{ID: "custom-agent-1", Name: "Mine", Description: "keep me"}
	ctx := context.WithValue(context.Background(), LanguageContextKey, "en-US")
	ApplyBuiltinAgentLocalization(ctx, agent)
	if agent.Name != "Mine" || agent.Description != "keep me" {
		t.Fatalf("custom agent was rewritten: %+v", agent)
	}
}

func TestApplyBuiltinAgentLocalizationPreservesCustomizedNameAndDesc(t *testing.T) {
	restore := OverrideBuiltinAgentEntriesForTest(map[string]*BuiltinAgentEntry{
		BuiltinQuickAnswerID: {
			ID:     BuiltinQuickAnswerID,
			Avatar: "quick.png",
			I18n: map[string]BuiltinAgentI18n{
				"default": {Name: "Quick Answer", Description: "Knowledge base RAG Q&A"},
				"vi":      {Name: "Hỏi đáp nhanh", Description: "Hỏi đáp RAG trên kho tri thức"},
			},
		},
	})
	t.Cleanup(restore)

	agent := &CustomAgent{
		ID:          BuiltinQuickAnswerID,
		Name:        "My Custom Quick Bot",
		Description: "Customized description for my workspace",
		IsBuiltin:   true,
		TenantID:    1,
	}
	ctx := context.WithValue(context.Background(), LanguageContextKey, "vi")
	ApplyBuiltinAgentLocalization(ctx, agent)

	if agent.Name != "My Custom Quick Bot" {
		t.Fatalf("Name = %q, want My Custom Quick Bot", agent.Name)
	}
	if agent.Description != "Customized description for my workspace" {
		t.Fatalf("Description = %q, want Customized description for my workspace", agent.Description)
	}
}
