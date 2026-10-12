package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// legalCatalogQueryMaxNumbers caps the số hiệu looked up for one question.
const legalCatalogQueryMaxNumbers = 3

// legalCatalogPrompt looks up the documents a question names by số hiệu in
// the legal catalogue, so the RAG answer knows them even when no knowledge
// base holds their text (most local documents) — the agent has
// check_legal_status for this, the RAG pipeline has no tools. Same report
// as the tool; "" when the question names no document.
func (s *sessionService) legalCatalogPrompt(ctx context.Context, query string) string {
	if s.legalCatalog == nil {
		return ""
	}
	var numbers []string
	seen := map[string]bool{}
	for _, n := range vietnamese_legal.DocNumberPattern.FindAllString(query, -1) {
		key := vietnamese_legal.CatalogNumberKey(n)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		numbers = append(numbers, n)
		if len(numbers) == legalCatalogQueryMaxNumbers {
			break
		}
	}
	if len(numbers) == 0 {
		return ""
	}
	tool := tools.NewCheckLegalStatusTool(s.legalCatalog)
	var parts []string
	for _, n := range numbers {
		args, _ := json.Marshal(map[string]string{"document_number": n})
		res, err := tool.Execute(ctx, args)
		if err != nil || res == nil {
			logger.Warnf(ctx, "legal catalogue lookup of %q failed: %v", n, err)
			continue
		}
		parts = append(parts, "### "+n+"\n"+strings.TrimSpace(res.Output))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n## Legal catalogue\nThe platform's legal catalogue (metadata from vbpl.vn) on the documents " +
		"the question names. It is metadata, not the documents' text: use it for type, issuer, dates, " +
		"validity and related documents, and never invent their contents. When several provinces share " +
		"a number, answer for the user's province and mention the others.\n\n" + strings.Join(parts, "\n\n")
}
