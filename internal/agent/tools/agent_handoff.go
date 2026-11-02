package tools

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/types"
)

// AgentHandoffType is the card a chat turn of another agent answers with
// when the user wants a Word file: only "Soạn thảo văn bản" writes one, so
// the card offers to switch there; the click opens an empty document and
// drafts it from the conversation (DocumentTemplateRequest.FromConversation).
const AgentHandoffType = "agent_handoff"

var (
	// a verb of making the file followed, within a few words, by Word …
	handoffVerbWordRe = regexp.MustCompile(`(^|[^\p{L}])(tạo|soạn|xuất|đưa|chuyển|làm|lưu|viết|dựng|in ra|tải về|export)\s+(\S+\s+){0,5}?\.?(word|docx)([^\p{L}]|$)`)
	// … or anywhere by a text file
	handoffVerbRe     = regexp.MustCompile(`(^|[^\p{L}])(tạo|soạn|xuất|đưa|chuyển|làm|lưu|viết|dựng|in ra|tải về|export)([^\p{L}]|$)`)
	handoffTextFileRe = regexp.MustCompile(`(file|tệp|tập tin) văn bản`)
	// … or a weaker verb with the file itself ("cho tôi file Word")
	handoffWantRe = regexp.MustCompile(`(^|[^\p{L}])(cần|muốn|cho|gửi)([^\p{L}]|$)`)
	handoffFileRe = regexp.MustCompile(`(file|tệp|tập tin|bản) (word|docx)([^\p{L}]|$)|\.docx`)
	// … unless the user asks how to do it, about another format, or about a
	// Word file they sent (the current agent works on the upload)
	handoffHowToRe  = regexp.MustCompile(`cách|làm sao|làm thế nào|như thế nào|hướng dẫn|là gì|lưu ý|bị lỗi|(trong|dùng|bằng|trên|của) word`)
	handoffOtherRe  = regexp.MustCompile(`(^|[^\p{L}])(pdf|excel|xlsx|powerpoint|pptx|ảnh|hình ảnh)([^\p{L}]|$)`)
	handoffUploadRe = regexp.MustCompile(`đính kèm|tải lên|đã gửi|vừa gửi|tôi gửi|(file|tệp|tập tin|bản) (word|docx) (này|đó|kia|vừa)`)
)

// handoffTemplateWords maps the document types the template flow builds
// to the words that name them, longest first where they overlap.
var handoffTemplateWords = []struct{ id, word string }{
	{"cong-van", "công văn"}, {"ke-hoach", "kế hoạch"}, {"bao-cao", "báo cáo"},
	{"to-trinh", "tờ trình"}, {"thong-bao", "thông báo"}, {"quyet-dinh", "quyết định"},
	{"nghi-quyet", "nghị quyết"}, {"chi-thi", "chỉ thị"}, {"cong-dien", "công điện"},
	{"giay-moi", "giấy mời"}, {"giay-gioi-thieu", "giấy giới thiệu"}, {"bien-ban", "biên bản"},
	{"giay-nghi-phep", "giấy nghỉ phép"},
}

// WantsWordFile tells whether a chat message asks for a Word file to be
// made ("tạo file Word kế hoạch này", "đưa nội dung trên vào file Word",
// "xuất ra docx"), not how to use Word ("soạn trong Word bị lỗi font") or
// what a document says.
func WantsWordFile(query string) bool {
	q := strings.ToLower(norm.NFC.String(query))
	asked := handoffVerbWordRe.MatchString(q) ||
		(handoffTextFileRe.MatchString(q) && handoffVerbRe.MatchString(q)) ||
		(handoffFileRe.MatchString(q) && handoffWantRe.MatchString(q))
	return asked && !handoffHowToRe.MatchString(q) && !handoffOtherRe.MatchString(q) && !handoffUploadRe.MatchString(q)
}

// handoffVerbTypes are the type words that are also everyday verbs
// ("lãnh đạo đã quyết định", "báo cáo lên") or documents a legal chat
// cites: in earlier messages they name the type only after "bản" / "văn
// bản" / "dự thảo".
var handoffVerbTypes = map[string]bool{"quyet-dinh": true, "bao-cao": true, "thong-bao": true,
	// named all day in a legal chat ("Nghị quyết 18-NQ/TW", "lập biên bản vi phạm")
	"chi-thi": true, "nghi-quyet": true, "cong-dien": true, "bien-ban": true}

// HandoffTemplateID guesses the document type of the file: from the
// message, else from the earlier messages of the conversation, latest
// first; "" when none names one. The card still lets the user pick another.
func HandoffTemplateID(query string, earlier []string) string {
	for n, text := range append([]string{query}, earlier...) {
		t := strings.ToLower(norm.NFC.String(text))
		best, at := "", -1
		for _, w := range handoffTemplateWords {
			words := []string{w.word}
			if n > 0 && handoffVerbTypes[w.id] {
				words = []string{"bản " + w.word, "dự thảo " + w.word}
			}
			// the type named last in a message is the one it is about
			for _, word := range words {
				if i := strings.LastIndex(t, word); i > at {
					best, at = w.id, i
				}
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

// AgentHandoffCard is the switch card.
type AgentHandoffCard struct {
	TemplateID string
}

func handoffResend(templateID string) string {
	return "Soạn " + strings.ToLower(types.DocumentTemplateNames[templateID]) + " từ nội dung đã trao đổi"
}

// Message is the answer the card comes with.
func (c *AgentHandoffCard) Message() string {
	what := "văn bản"
	if name := types.DocumentTemplateNames[c.TemplateID]; name != "" {
		what = strings.ToLower(name)
	}
	msg := "Agent đang dùng không tạo được file Word. Để có file .docx, bạn chuyển sang agent **Soạn thảo văn bản**: " +
		"bấm nút bên dưới, trợ lý sẽ mở một văn bản mới và soạn " + what + " từ nội dung chúng ta đã trao đổi."
	if c.TemplateID == "" {
		msg += " Hãy chọn loại văn bản cần soạn."
	}
	return msg
}

// Data is the card payload: the agent to switch to and what the click
// sends (resend, with a template request seeded from the conversation);
// options list the types, the guessed one (template_id) first.
func (c *AgentHandoffCard) Data() map[string]interface{} {
	data := map[string]interface{}{
		"display_type":      AgentHandoffType,
		"target_agent_id":   types.BuiltinDocumentAssistantID,
		"target_agent_name": "Soạn thảo văn bản",
		"template_id":       c.TemplateID,
		"from_conversation": true,
	}
	if c.TemplateID != "" {
		data["template_name"] = types.DocumentTemplateNames[c.TemplateID]
		data["resend"] = handoffResend(c.TemplateID)
	}
	// every type, the guess first, so a wrong guess can be corrected
	opts := make([]map[string]interface{}, 0, len(handoffTemplateWords))
	ordered := append([]struct{ id, word string }(nil), handoffTemplateWords...)
	for i, w := range ordered {
		if w.id == c.TemplateID {
			ordered[0], ordered[i] = ordered[i], ordered[0]
		}
	}
	for _, w := range ordered {
		opts = append(opts, map[string]interface{}{
			"template_id": w.id, "label": types.DocumentTemplateNames[w.id], "resend": handoffResend(w.id),
		})
	}
	data["options"] = opts
	return data
}
