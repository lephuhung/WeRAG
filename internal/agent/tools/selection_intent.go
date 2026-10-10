package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
)

// Requests about a highlighted passage. The agent model alone often
// answered "viết lại đoạn này" in the chat instead of calling
// rewrite_paragraphs — then there is no proposal and no "Thay vào văn bản"
// button — and listed spelling mistakes without underlining them. One
// short thinking-off call labels the request, and the turn starts with the
// tool it needs (types.WithFirstToolChoice).
const (
	SelectionIntentRewrite  = "rewrite"
	SelectionIntentSpelling = "spelling"
	SelectionIntentOther    = "other"
)

// selectionIntentTimeout bounds the labeling call: past it the turn runs
// as before.
const selectionIntentTimeout = 20 * time.Second

const selectionIntentPrompt = `You label a user's request about a passage they highlighted in a Word document (Vietnamese administrative text). Answer with JSON {"intent": "<label>"} and nothing else.

Labels:
- rewrite: the user wants new wording for the passage — rewrite, reword, shorten, correct, polish, make it more formal, or suggest/propose how to write it ("viết lại", "gợi ý viết lại", "đề xuất cách viết", "sửa câu này", "sửa lại", "rút gọn", "cho mượt hơn", "văn phong hành chính", "viết hay hơn").
- spelling: the user wants the spelling, typos or diacritics of the passage checked ("kiểm tra chính tả", "soát lỗi chính tả", "sai dấu", "lỗi đánh máy").
- other: anything else — what it means, explain, summarize, translate, compare with another document, look up the law, check the format, a question about it.`

// ClassifySelectionIntent labels a request about a highlighted passage;
// SelectionIntentOther when unsure, on error or without a model.
func ClassifySelectionIntent(ctx context.Context, model docformat.Completer, query, selection string) string {
	query = strings.TrimSpace(query)
	if model == nil || query == "" {
		return SelectionIntentOther
	}
	ctx, cancel := context.WithTimeout(ctx, selectionIntentTimeout)
	defer cancel()
	reply, err := model.Complete(ctx, []docformat.Message{
		{Role: "system", Content: selectionIntentPrompt},
		{Role: "user", Content: "Yêu cầu: " + clipRunes(query, 400) + "\nĐoạn đã bôi đen: " + clipRunes(strings.TrimSpace(selection), 300)},
	})
	if err != nil {
		logger.Warnf(ctx, "selection intent: %v", err)
		return SelectionIntentOther
	}
	return parseSelectionIntent(reply)
}

func parseSelectionIntent(reply string) string {
	var out struct {
		Intent string `json:"intent"`
	}
	reply = strings.TrimSpace(reply)
	if i, j := strings.Index(reply, "{"), strings.LastIndex(reply, "}"); i >= 0 && j > i {
		reply = reply[i : j+1]
	}
	if json.Unmarshal([]byte(reply), &out) != nil {
		return SelectionIntentOther
	}
	switch intent := strings.ToLower(strings.TrimSpace(out.Intent)); intent {
	case SelectionIntentRewrite, SelectionIntentSpelling:
		return intent
	}
	return SelectionIntentOther
}

// FirstToolForSelection is the tool a labeled request starts with, "" for
// SelectionIntentOther.
func FirstToolForSelection(intent string) string {
	switch intent {
	case SelectionIntentRewrite:
		return ToolRewriteParagraphs
	case SelectionIntentSpelling:
		return ToolCheckSpelling
	}
	return ""
}
