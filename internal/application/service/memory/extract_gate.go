package memory

import (
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
)

// The write gate.
//
// The extraction model is asked to tell a durable statement about a person
// from a passing request, and to mark its own guesses as inferred. With Qwen
// on Vietnamese civil-service traffic it did neither reliably: "Soát lỗi chính
// tả đoạn này" became a standing preference injected into 236 later turns,
// "Vẽ sơ đồ cho nội dung này" a task with no end, and a question about commune
// pensions a confident profile entry. The rules below are what the prompt
// asked for, applied in code where the model cannot skip them. They look only
// at the user's own sentence, never at the model's paraphrase of it.

// gateVerdict says what the gate did with a decision.
type gateVerdict string

const (
	gateKept            gateVerdict = "kept"
	gateDropDocument    gateVerdict = "one-off request about the open document"
	gateDropPreference  gateVerdict = "preference without a standing cue"
	gateDropProfile     gateVerdict = "profile read off a command"
	gateMarkedInferred  gateVerdict = "profile from a question is a guess"
	gateDemotedToTask   gateVerdict = "fact from a question/command is current work, kept as task"
	gateTaskGivenExpiry gateVerdict = "task without an end date expires in a week"
)

func (v gateVerdict) dropped() bool {
	switch v {
	case gateDropDocument, gateDropPreference, gateDropProfile:
		return true
	}
	return false
}

// gateTaskTTL is how long a task the model gave no end date keeps being
// recalled. Current work is current for about a week; after that it is noise
// until the user mentions it again, which writes a fresh one.
const gateTaskTTL = 7 * 24 * time.Hour

// durableCues are phrasings that make a sentence about the person rather than
// about this turn: standing instructions and first-person statements.
var durableCues = []string{
	// standing instructions
	"từ giờ", "từ nay", "từ bây giờ", "sau này", "về sau", "lần sau", "luôn luôn", "luôn ",
	"lúc nào cũng", "mỗi khi", "mỗi lần", "mặc định", "đừng ", "không cần ", "không bao giờ",
	"thói quen", "hãy nhớ", "ghi nhớ", "nhớ là", "nhớ rằng", "nhớ giúp",
	// first person
	"tôi là", "tôi làm", "tôi đang", "tôi công tác", "tôi phụ trách", "tôi thích", "tôi không thích",
	"công tác tại", "làm việc tại", "làm ở", "công tác ở", "thuộc đơn vị", "thuộc phòng", "thuộc sở",
	"đơn vị em", "đơn vị anh", "đơn vị chị", "phòng em", "phòng mình", "em đang", "mình đang",
	"tôi muốn", "tôi cần", "tôi thường", "tôi hay ", "tôi quen", "tôi ở", "tên tôi", "tuổi tôi",
	"mình là", "mình làm", "mình thích", "mình thường", "bên tôi", "bên mình", "bên em", "bên anh", "bên chị",
	"đơn vị tôi", "đơn vị mình", "cơ quan tôi", "cơ quan mình", "phòng tôi", "chúng tôi", "chúng mình",
	"em là", "em làm", "em công tác", "em phụ trách", "em thích", "em muốn", "em cần", "em thường",
	"anh là", "chị là", "anh làm", "chị làm",
	// english (padded: matched against the sentence with a space either side)
	"from now on", " always ", " never ", " remember ", " i am ", " i'm ", " i work ", " i prefer ",
	" i like ", " i usually ", " my name ", " my team ", " my job ", " we use ", " our ",
}

// documentNouns, after a command verb, make the request about a piece of
// writing: "Tóm tắt mục II của văn bản", "Soạn công văn gửi Sở Nội vụ". The
// subject of such a request is counted by the topic tracker; the request
// itself is not a memory.
var documentNouns = []string{
	"văn bản", "tài liệu", "đoạn", "mục ", "bảng", "câu ", "file", "tệp", "nội dung", "sơ đồ",
	"biểu đồ", "hình", "ảnh", "trang ", "phần ", "báo cáo", "công văn", "tờ trình", "kế hoạch",
	"thông báo", "quyết định", "dự thảo", "bài ", "email", "thư ", "chính tả", "thể thức", "tiêu đề",
	"document", "paragraph", "section", "table", "report", "letter", "draft", "email", "text",
}

// documentCues mark a request about whatever is open in front of the user.
// Such a sentence describes the turn, not the person, however it is phrased.
var documentCues = []string{
	"văn bản này", "đoạn này", "đoạn văn này", "tài liệu này", "file này", "tệp này", "câu này",
	"bảng này", "nội dung này", "phần này", "mục này", "trang này", "đoạn trên", "đoạn dưới",
	"ở trên", "bên dưới", "đang mở", "đang xem", "hiện tại", "vừa gửi", "vừa tải", "vừa dán",
	"văn bản đính kèm", "tài liệu đính kèm", "đoạn sau", "đoạn vừa", "cho tôi xem",
	"this document", "this paragraph", "this section", "this file", "the attached",
}

// commandVerbs open a request to do something now. A sentence that starts with
// one is work for this turn unless a durable cue says otherwise.
var commandVerbs = []string{
	"tóm tắt", "vẽ", "viết lại", "viết", "soạn", "soạn thảo", "kiểm tra", "soát", "rà soát", "sửa", "chỉnh",
	"chỉnh sửa", "gửi", "chèn", "thêm", "xóa", "xoá", "bỏ", "đề xuất", "trích xuất", "trích", "dịch",
	"giải thích", "thống kê", "đối chiếu", "so sánh", "liệt kê", "tạo", "lập", "góp ý", "tra cứu", "tìm",
	"nêu", "phân tích", "đánh giá", "chuyển", "định dạng", "in ", "làm", "hãy", "cho tôi", "cho mình",
	"giúp tôi", "giúp mình", "giúp em", "xem", "đọc", "rút gọn", "mở rộng", "bổ sung", "hoàn thiện",
	"điền", "thay", "diễn đạt", "chuẩn hóa", "chuẩn hoá", "kẻ", "tính", "đếm", "xác định", "mô tả",
	"summarize", "summarise", "rewrite", "write", "draw", "check", "fix", "send", "insert", "translate",
	"explain", "compare", "list", "create", "find", "describe", "please",
}

// questionCues mark a sentence as asking something. Vietnamese questions do
// not need a question mark, so the usual particles are checked too.
var questionCues = []string{
	"?", " là gì", " gì", "bao nhiêu", "như thế nào", "thế nào", "ra sao", "khi nào", "bao lâu",
	"ở đâu", "tại sao", "vì sao", " không", "hay không", "được không", "có được", "phải không",
	"ai ký", " mấy ", "bao giờ", "nào",
	"what ", "when ", "how ", "why ", "where ", "which ", "who ", "is there", "are there", "can i", "do i",
}

// topicActionPrefixes open a topic label that names what the user asked the
// assistant to DO rather than a field they work in. Extraction is asked for
// subject areas, but a document assistant mostly receives commands, and with
// no area to name the model labels the action: "tóm tắt văn bản", "kiểm tra
// lỗi chính tả", "hỏi nội dung ảnh". Counting those promotes a feature of the
// tool into a "long-term focus" of the person.
var topicActionPrefixes = append([]string{
	"hỏi", "yêu cầu", "nhờ", "cần", "muốn", "đề nghị", "trả lời", "tra ", "xem ", "đọc ",
	"ask", "request", "answer",
}, commandVerbs...)

// topicIsAnAction reports whether a topic label describes a request rather
// than a subject, so it must not be counted towards an interest.
func topicIsAnAction(label string) bool {
	text := normalizeForGate(label)
	if text == "" {
		return false
	}
	for _, prefix := range topicActionPrefixes {
		p := strings.TrimSpace(prefix)
		if text == p || strings.HasPrefix(text, p+" ") {
			return true
		}
	}
	return false
}

func hasAny(text string, cues []string) bool {
	padded := " " + text + " "
	for _, cue := range cues {
		if strings.Contains(padded, cue) {
			return true
		}
	}
	return false
}

func normalizeForGate(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	var b strings.Builder
	lastSpace := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteRune(' ')
			}
			lastSpace = true
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

func startsWithCommand(text string) bool {
	// Politeness and address prefixes do not change what the sentence is.
	for _, prefix := range []string{"hãy ", "vui lòng ", "xin ", "nhờ ", "bạn ", "em ", "anh ", "chị ", "ad ", "bot ", "please "} {
		text = strings.TrimPrefix(text, prefix)
	}
	for _, verb := range commandVerbs {
		if strings.HasPrefix(text, verb+" ") || strings.HasPrefix(text, verb+",") || text == verb {
			return true
		}
	}
	return false
}

func mentionsOpenDocument(text string) bool {
	if hasAny(text, documentCues) {
		return true
	}
	// "@ke_hoach_nguon.docx" style references name an attachment of this
	// session; nothing about the person follows from them.
	if at := strings.Index(text, "@"); at >= 0 {
		rest := text[at:]
		for _, ext := range []string{".docx", ".doc", ".pdf", ".xlsx", ".xls", ".pptx", ".txt", ".md"} {
			if strings.Contains(rest, ext) {
				return true
			}
		}
	}
	return false
}

func looksLikeQuestion(text string) bool {
	if strings.HasSuffix(text, "?") {
		return true
	}
	padded := " " + text + " "
	for _, cue := range questionCues {
		if cue == "?" {
			continue
		}
		if strings.HasSuffix(text, strings.TrimSpace(cue)) || strings.Contains(padded, cue) {
			return true
		}
	}
	return false
}

// gateDecision applies the write rules to one add/update decision. It returns
// the (possibly adjusted) decision and what was done to it. The source is the
// user's own sentence the decision points at.
func gateDecision(decision extractionDecision, source string) (extractionDecision, gateVerdict) {
	text := normalizeForGate(source)
	kind := strings.ToLower(strings.TrimSpace(decision.Kind))
	durable := hasAny(text, durableCues)

	verdict := gateKept
	if !durable {
		switch {
		case mentionsOpenDocument(text):
			return decision, gateDropDocument
		case startsWithCommand(text) && hasAny(text, documentNouns):
			return decision, gateDropDocument
		case startsWithCommand(text):
			switch kind {
			case types.MemoryKindPreference:
				return decision, gateDropPreference
			case types.MemoryKindProfile:
				return decision, gateDropProfile
			case types.MemoryKindFact:
				decision.Kind = types.MemoryKindTask
				verdict = gateDemotedToTask
			}
		case looksLikeQuestion(text):
			switch kind {
			case types.MemoryKindPreference:
				return decision, gateDropPreference
			case types.MemoryKindProfile:
				if !decision.Inferred {
					decision.Inferred = true
					verdict = gateMarkedInferred
				}
			case types.MemoryKindFact:
				decision.Kind = types.MemoryKindTask
				verdict = gateDemotedToTask
			}
		}
	}

	if strings.EqualFold(decision.Kind, types.MemoryKindTask) && parseExpiry(decision.ExpiresAt) == nil {
		decision.ExpiresAt = time.Now().Add(gateTaskTTL).Format("2006-01-02")
		if verdict == gateKept {
			verdict = gateTaskGivenExpiry
		}
	}
	return decision, verdict
}
