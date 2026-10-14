package config

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Tencent/WeKnora/internal/types"
)

// The default rewrite prompt runs on every chat turn. Its examples were all
// Chinese; for a Vietnamese deployment they have to stay Vietnamese.
func TestDefaultRewritePromptCarriesNoChinese(t *testing.T) {
	raw, err := os.ReadFile("../../config/prompt_templates/rewrite.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Templates []struct {
			ID      string `yaml:"id"`
			Content string `yaml:"content"`
			User    string `yaml:"user"`
		} `yaml:"templates"`
	}
	if err := yaml.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tpl := range file.Templates {
		if tpl.ID != "default_rewrite" {
			continue
		}
		found = true
		if types.ContainsHan(tpl.Content) || types.ContainsHan(tpl.User) {
			t.Error("default_rewrite contains Chinese")
		}
	}
	if !found {
		t.Fatal("default_rewrite template not found")
	}
}
