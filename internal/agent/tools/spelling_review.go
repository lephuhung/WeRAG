package tools

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
)

// ReviewSpelling proofreads plain texts outside a document editor (e.g. the
// parsed chunks of a knowledge-base document) with the same prompt, batching
// and validation as check_spelling. Each finding's Paragraph is the index of
// its text in texts; Wrong is copied from that text. An empty prompt uses
// DefaultSpellcheckPrompt. It fails only when every model batch failed.
func ReviewSpelling(ctx context.Context, model chat.Chat, prompt string, texts []string) ([]SpellingFinding, error) {
	if model == nil {
		return nil, errors.New("no spellcheck model")
	}
	t := (&CheckSpellingTool{model: model, prompt: DefaultSpellcheckPrompt}).WithPrompt(prompt)
	paras := make([]spellPara, 0, len(texts))
	for i, text := range texts {
		paras = append(paras, spellPara{Index: i, Text: text})
	}
	findings := strayCharFindings(paras)
	batches, failed := 0, 0
	for start := 0; start < len(paras); start += spellBatchSize {
		batches++
		got, err := t.reviewBatch(ctx, paras[start:min(start+spellBatchSize, len(paras))])
		if err != nil {
			failed++
			logger.Warnf(ctx, "spelling review: batch %d failed: %v", batches, err)
			continue
		}
		findings = append(findings, got...)
	}
	if batches > 0 && failed == batches && len(findings) == 0 {
		return nil, errors.New("the spellcheck model did not answer in the expected format")
	}
	return dedupeFindings(findings), nil
}
