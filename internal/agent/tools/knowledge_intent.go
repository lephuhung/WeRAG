package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"

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

// The same holds for a chat agent with a knowledge base and no open
// document: builtin-smart-reasoning answered 8 of 181 legal questions with
// no tool call, citing decrees from memory, some repealed long ago
// (Nghị định 27/2015/NĐ-CP for a 2024 question).
const chatKnowledgeIntentPrompt = `You label a user's request to an assistant that answers from a knowledge base of Vietnamese laws, regulations and official documents (and sometimes the web). Answer with JSON {"lookup": true} or {"lookup": false} and nothing else.

lookup = true when the answer rests on facts: what laws, decrees, circulars, regulations or official rules say, legal situations ("ông A có vi phạm không", "cơ quan nào có thẩm quyền", "thủ tục gồm những gì", "mức phạt", "thời hạn", "còn hiệu lực không"), or any factual question about organisations, people, products, figures, dates or events.

lookup = false for greetings and small talk, questions about the assistant itself, and requests that work only on text the user supplied in the message (rewrite, translate, summarise, check spelling, draft from the given content), or pure arithmetic.`

// knowledgeIntentAttachedNote extends knowledgeIntentPrompt when the
// request comes with files: "cho tôi thông tin sơ bộ về văn bản" sent with
// a report attached was labeled lookup (2026-10-16) and the turn started
// with a needless search_knowledge.
const knowledgeIntentAttachedNote = `

The user attached files to this request (listed before it). Their text is given to the assistant like the open document's: a request about them (summarise, overview, what they say, counts, names in them) is lookup = false. A question about facts outside them (which law applies, whether a rule or document is valid or current, what a photographed sign or object means under the rules) is still lookup = true.`

// ClassifyKnowledgeLookup reports whether a request needs facts from
// outside the open document and the files attached to it (attached: their
// names); false when unsure, on error or without a model.
func ClassifyKnowledgeLookup(ctx context.Context, model docformat.Completer, query string, attached ...string) bool {
	var names []string
	for _, n := range attached {
		// a file name is untrusted text: one line, quoted
		n = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || r == '"' {
				return ' '
			}
			return r
		}, n))
		if n != "" {
			names = append(names, `"`+clipRunes(n, 120)+`"`)
		}
	}
	if len(names) == 0 {
		return classifyLookup(ctx, model, knowledgeIntentPrompt, query, "")
	}
	return classifyLookup(ctx, model, knowledgeIntentPrompt+knowledgeIntentAttachedNote, query,
		"Tệp đính kèm: "+strings.Join(names, "; ")+"\n")
}

// ClassifyChatKnowledgeLookup is ClassifyKnowledgeLookup for a chat turn
// with no open document.
func ClassifyChatKnowledgeLookup(ctx context.Context, model docformat.Completer, query string) bool {
	return classifyLookup(ctx, model, chatKnowledgeIntentPrompt, query, "")
}

func classifyLookup(ctx context.Context, model docformat.Completer, prompt, query, preface string) bool {
	query = strings.TrimSpace(query)
	if model == nil || query == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, knowledgeIntentTimeout)
	defer cancel()
	reply, err := model.Complete(ctx, []docformat.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: preface + "Yêu cầu: " + clipRunes(query, 400)},
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
