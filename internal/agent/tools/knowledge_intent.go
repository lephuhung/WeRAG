package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
)

// Questions the open document does not answer. Left to itself the
// document assistant answered them from the model's own memory without one
// search_knowledge call: "xử lý như thế nào theo quy định" cited Luật Bảo vệ
// bí mật nhà nước 2018 while the KB held its replacement, Luật 117/2025, and
// "iPhone 18 Pro" came back as "not released, the latest is iPhone 15". One
// short thinking-off call labels the request; such a turn starts with
// search_knowledge (types.WithFirstToolChoice) and may only be answered
// from what the tools return (types.WithGroundingRequired).

// knowledgeIntentTimeout bounds the labeling call: past it the turn runs
// as before.
const knowledgeIntentTimeout = 20 * time.Second

const knowledgeIntentPrompt = `You label a user's request to an assistant that works on an open Word document (Vietnamese administrative text) and can search a knowledge base (laws, regulations, internal documents) and sometimes the web. Answer with JSON {"lookup": true} or {"lookup": false} and nothing else.

lookup = true when answering needs facts the open document does not contain:
- what laws, decrees, circulars, regulations or official rules say: "theo quy định", "căn cứ pháp lý", "có đúng quy định không", "xử lý như thế nào", "luật nào quy định", "thẩm quyền", "thời hạn", "mức phạt", "còn hiệu lực không";
- any factual question about the world: products, prices, specifications, events, people, organisations, figures, dates, news ("iPhone 18 Pro cấu hình thế nào", "ai là…", "bao nhiêu…").

lookup = false when the open document alone answers it (what the report says, counts, names, summaries of it), or the request edits, rewrites, translates, checks spelling or format, draws a diagram or illustration, or is small talk or a greeting.`

// ClassifyKnowledgeLookup reports whether a request needs facts from
// outside the open document; false when unsure, on error or without a
// model.
func ClassifyKnowledgeLookup(ctx context.Context, model docformat.Completer, query string) bool {
	query = strings.TrimSpace(query)
	if model == nil || query == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, knowledgeIntentTimeout)
	defer cancel()
	reply, err := model.Complete(ctx, []docformat.Message{
		{Role: "system", Content: knowledgeIntentPrompt},
		{Role: "user", Content: "Yêu cầu: " + clipRunes(query, 400)},
	})
	if err != nil {
		logger.Warnf(ctx, "knowledge lookup intent: %v", err)
		return false
	}
	return parseKnowledgeLookup(reply)
}

func parseKnowledgeLookup(reply string) bool {
	var out struct {
		Lookup bool `json:"lookup"`
	}
	reply = strings.TrimSpace(reply)
	if i, j := strings.Index(reply, "{"), strings.LastIndex(reply, "}"); i >= 0 && j > i {
		reply = reply[i : j+1]
	}
	if json.Unmarshal([]byte(reply), &out) != nil {
		return false
	}
	return out.Lookup
}
