package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// TestPromptSizeReport prints the size of each document-assistant tool's
// model-facing JSON definition (name + description + parameters). It is a
// measuring aid, not an assertion: run it with
//
//	PROMPT_SIZE_REPORT=1 go test ./internal/agent/tools -run TestPromptSizeReport -v
//
// Tokens ≈ chars / 3.8 for this English/Vietnamese mix.
func TestPromptSizeReport(t *testing.T) {
	if os.Getenv("PROMPT_SIZE_REPORT") == "" {
		t.Skip("set PROMPT_SIZE_REPORT=1 to print tool definition sizes")
	}
	checkFormat := NewCheckDocumentFormatToolForWorkspace(nil, nil, "")
	mcpDiscover := BaseTool{name: ToolDiscoverMCPTools, description: mcpDiscoveryDescription, schema: json.RawMessage(mcpDiscoverySchema)}
	list := []BaseTool{
		rewriteParagraphsTool, applyFormatFixesTool, checkFormat.BaseTool, insertParagraphsTool,
		drawDiagramTool, generateIllustrationTool, markPassagesTool, checkSpellingTool,
		mcpDiscover, findRelatedLegalDocumentsTool, findInDocumentsTool, readDocumentOutlineTool,
		searchKnowledgeTool, searchDocumentSectionTool, resolveAbbreviationTool, readDocumentTool,
		checkLegalStatusTool,
	}
	total := 0
	for _, bt := range list {
		def := types.FunctionDefinition{Name: bt.Name(), Description: bt.Description(), Parameters: bt.Parameters()}
		raw, err := json.Marshal(def)
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		total += len(s)
		fmt.Printf("%-30s desc=%5d schema=%5d json_bytes=%5d runes=%5d ~tok=%4d\n", bt.Name(),
			utf8.RuneCountInString(bt.Description()), utf8.RuneCount(bt.Parameters()), len(s), utf8.RuneCountInString(s),
			int(float64(utf8.RuneCountInString(s))/3.8))
	}
	fmt.Printf("TOTAL json_bytes=%d\n", total)
}
