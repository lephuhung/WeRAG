package types

import "testing"

func TestParserEngineConfigResolveChatParserEngine(t *testing.T) {
	config := &ParserEngineConfig{ChatParserEngineRules: []ParserEngineRule{
		{FileTypes: []string{"pdf", ".docx"}, Engine: "vietnamese_legal"},
		{FileTypes: []string{"xlsx"}, Engine: "markitdown"},
	}}
	for input, expected := range map[string]string{
		"PDF": "vietnamese_legal", ".docx": "vietnamese_legal", "xlsx": "markitdown",
		"txt": "", "pptx": "markitdown", ".PPT": "markitdown",
	} {
		if actual := config.ResolveChatParserEngine(input); actual != expected {
			t.Fatalf("ResolveChatParserEngine(%q) = %q, want %q", input, actual, expected)
		}
	}
	var nilConfig *ParserEngineConfig
	if actual := nilConfig.ResolveChatParserEngine("pdf"); actual != "vietnamese_legal" {
		t.Fatalf("nil config pdf resolved %q, want vietnamese_legal", actual)
	}
	if actual := nilConfig.ResolveChatParserEngine("pptx"); actual != "markitdown" {
		t.Fatalf("nil config pptx resolved %q, want markitdown", actual)
	}
}
