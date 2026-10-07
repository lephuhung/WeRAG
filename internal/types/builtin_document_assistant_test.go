package types

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuiltinDocumentAssistantLoadsFromYAML(t *testing.T) {
	configDir := filepath.Join("..", "..", "config")
	if err := LoadBuiltinAgentsConfig(configDir); err != nil {
		t.Fatal(err)
	}
	if !IsBuiltinAgentID(BuiltinDocumentAssistantID) {
		t.Fatal("builtin-document-assistant is not registered")
	}

	for _, locale := range []string{"vi", "vi-VN"} {
		ctx := context.WithValue(context.Background(), LanguageContextKey, locale)
		agent := GetBuiltinAgentWithContext(ctx, BuiltinDocumentAssistantID, 1)
		if agent == nil {
			t.Fatalf("%s: agent not built", locale)
		}
		if agent.Name != "Soạn thảo văn bản" || !strings.Contains(agent.Description, "Nghị định 30/2020") {
			t.Fatalf("%s: name=%q description=%q", locale, agent.Name, agent.Description)
		}
	}
	en := GetBuiltinAgentWithContext(context.WithValue(context.Background(), LanguageContextKey, "en-US"),
		BuiltinDocumentAssistantID, 1)
	if en.Name != "Document Assistant" || en.Avatar != "📝" {
		t.Fatalf("default locale: name=%q avatar=%q", en.Name, en.Avatar)
	}

	cfg := en.Config
	wantTools := []string{"check_document_format", "read_document_outline", "apply_format_fixes", "rewrite_paragraphs",
		"search_knowledge", "read_document", "query_knowledge_graph", "resolve_abbreviation"}
	if !reflect.DeepEqual(cfg.AllowedTools, wantTools) {
		t.Fatalf("allowed_tools = %v", cfg.AllowedTools)
	}
	if !reflect.DeepEqual(cfg.SupportedFileTypes, []string{"docx", "doc"}) {
		t.Fatalf("supported_file_types = %v", cfg.SupportedFileTypes)
	}
	if cfg.SystemPromptID != "document_assistant" || cfg.AgentMode != "smart-reasoning" ||
		cfg.AgentType != AgentTypeCustom || cfg.Temperature != 0.2 || cfg.MaxIterations != 30 {
		t.Fatalf("config = %+v", cfg)
	}

	// the system prompt it references exists
	data, err := os.ReadFile(filepath.Join(configDir, "prompt_templates", "agent_system_prompt.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var prompts struct {
		Templates []struct {
			ID      string `yaml:"id"`
			Mode    string `yaml:"mode"`
			Content string `yaml:"content"`
		} `yaml:"templates"`
	}
	if err := yaml.Unmarshal(data, &prompts); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range prompts.Templates {
		if p.ID == "document_assistant" {
			found = true
			for _, want := range []string{"apply_format_fixes", "rewrite_paragraphs", "<document_selection>", "tracked change"} {
				if !strings.Contains(p.Content, want) {
					t.Errorf("document_assistant prompt lacks %q", want)
				}
			}
		}
	}
	if !found {
		t.Fatal("prompt template document_assistant missing")
	}
}
