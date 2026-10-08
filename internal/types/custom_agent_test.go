package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCustomAgentConfigResolveChatParserEngine(t *testing.T) {
	config := &CustomAgentConfig{ChatParserEngineRules: []ParserEngineRule{
		{FileTypes: []string{"pdf", ".pptx"}, Engine: "mineru"},
		{FileTypes: []string{"png", "jpg"}, Engine: "paddleocr_vl"},
	}}
	for input, expected := range map[string]string{
		"PDF": "mineru", ".pptx": "mineru", "png": "paddleocr_vl",
		"txt": "", "ppt": "markitdown",
	} {
		if actual := config.ResolveChatParserEngine(input); actual != expected {
			t.Fatalf("ResolveChatParserEngine(%q) = %q, want %q", input, actual, expected)
		}
	}
	var nilConfig *CustomAgentConfig
	if actual := nilConfig.ResolveChatParserEngine("pdf"); actual != "" {
		t.Fatalf("nil config resolved %q", actual)
	}
	if actual := nilConfig.ResolveChatParserEngine("pptx"); actual != "markitdown" {
		t.Fatalf("nil config pptx resolved %q, want markitdown", actual)
	}
}

func TestEnsureDefaults_ThinkingExplicitFalse(t *testing.T) {
	agent := &CustomAgent{Config: CustomAgentConfig{}}
	agent.EnsureDefaults()
	if agent.Config.Thinking == nil {
		t.Fatal("EnsureDefaults should set Thinking to explicit false when unset")
	}
	if *agent.Config.Thinking {
		t.Fatal("default Thinking should be false")
	}
}

func TestEnsureDefaults_ThinkingPreservesTrue(t *testing.T) {
	enabled := true
	agent := &CustomAgent{Config: CustomAgentConfig{Thinking: &enabled}}
	agent.EnsureDefaults()
	if agent.Config.Thinking == nil || !*agent.Config.Thinking {
		t.Fatal("EnsureDefaults must not overwrite an explicit Thinking=true")
	}
}

func TestEnsureDefaults_MaxCompletionTokensByMode(t *testing.T) {
	qa := &CustomAgent{Config: CustomAgentConfig{AgentMode: AgentModeQuickAnswer}}
	qa.EnsureDefaults()
	if qa.Config.MaxCompletionTokens != 0 {
		t.Fatalf("unset max_completion_tokens must stay 0 (follow default at call time), got %d",
			qa.Config.MaxCompletionTokens)
	}

	sr := &CustomAgent{Config: CustomAgentConfig{AgentMode: AgentModeSmartReasoning}}
	sr.EnsureDefaults()
	if sr.Config.MaxCompletionTokens != 0 {
		t.Fatalf("unset smart-reasoning max_completion_tokens must stay 0, got %d",
			sr.Config.MaxCompletionTokens)
	}

	explicit := &CustomAgent{Config: CustomAgentConfig{
		AgentMode:           AgentModeSmartReasoning,
		MaxCompletionTokens: 64000,
	}}
	explicit.EnsureDefaults()
	if explicit.Config.MaxCompletionTokens != 64000 {
		t.Fatalf("EnsureDefaults must preserve explicit MaxCompletionTokens, got %d",
			explicit.Config.MaxCompletionTokens)
	}
}

func TestEnsureDefaults_MaxIterationsUnlimited(t *testing.T) {
	unset := &CustomAgent{Config: CustomAgentConfig{}}
	unset.EnsureDefaults()
	if unset.Config.MaxIterations != 10 {
		t.Fatalf("unset max_iterations should default to 10, got %d", unset.Config.MaxIterations)
	}

	unlimited := &CustomAgent{Config: CustomAgentConfig{MaxIterations: -1}}
	unlimited.EnsureDefaults()
	if unlimited.Config.MaxIterations != UnlimitedMaxIterations {
		t.Fatalf("EnsureDefaults must preserve unlimited max_iterations, got %d",
			unlimited.Config.MaxIterations)
	}

	oddNegative := &CustomAgent{Config: CustomAgentConfig{MaxIterations: -7}}
	oddNegative.EnsureDefaults()
	if oddNegative.Config.MaxIterations != UnlimitedMaxIterations {
		t.Fatalf("any negative max_iterations should normalize to %d, got %d",
			UnlimitedMaxIterations, oddNegative.Config.MaxIterations)
	}
}

func TestEnsureDefaults_CitationsDefaultEnabledAndPreserveFalse(t *testing.T) {
	legacy := &CustomAgent{Config: CustomAgentConfig{}}
	legacy.EnsureDefaults()
	if legacy.Config.CitationEnabled == nil || !*legacy.Config.CitationEnabled {
		t.Fatal("legacy agents must default citation output to enabled")
	}

	disabled := false
	explicit := &CustomAgent{Config: CustomAgentConfig{CitationEnabled: &disabled}}
	explicit.EnsureDefaults()
	if explicit.Config.CitationEnabled == nil || *explicit.Config.CitationEnabled {
		t.Fatal("EnsureDefaults must preserve explicit citation_enabled=false")
	}
}

func TestCustomAgentDocumentAssistantSettings(t *testing.T) {
	var nilCfg *CustomAgentConfig
	if !nilCfg.FormatCheckOnOpenEnabled() || nilCfg.OpenDocumentMaxRunes() != 0 || nilCfg.FormatCheckModel() != "" {
		t.Fatal("nil config defaults")
	}
	off := false
	cfg := &CustomAgentConfig{ModelID: "chat", FormatCheckModelID: " thinker ", FormatCheckOnOpen: &off, OpenDocumentMaxRunesLimit: 9000}
	if cfg.FormatCheckOnOpenEnabled() || cfg.FormatCheckModel() != "thinker" || cfg.SpellcheckModel() != "chat" || cfg.OpenDocumentMaxRunes() != 9000 {
		t.Fatalf("config: %+v", cfg)
	}
	if err := cfg.ValidateDocumentAssistant(); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{-1, MaxOpenDocumentRunes + 1} {
		if (&CustomAgentConfig{OpenDocumentMaxRunesLimit: n}).ValidateDocumentAssistant() == nil {
			t.Errorf("open_document_max_runes=%d accepted", n)
		}
	}
	// JSON names
	b, _ := json.Marshal(CustomAgentConfig{FormatCheckModelID: "a", SpellcheckModelID: "b", FormatCheckOnOpen: &off,
		WebSearchDefaultOn: true, OpenDocumentMaxRunesLimit: 7})
	for _, key := range []string{`"format_check_model_id":"a"`, `"spellcheck_model_id":"b"`, `"format_check_on_open":false`,
		`"web_search_default_on":true`, `"open_document_max_runes":7`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("JSON lacks %s: %s", key, b)
		}
	}
}
