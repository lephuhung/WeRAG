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
	gateDropReported    gateVerdict = "someone else's words, not a statement about the user"
	gateDropPasted      gateVerdict = "pasted text, not a statement about the user"
	gateDropLookup      gateVerdict = "lookup phrase, not a statement"
	gateMarkedInferred  gateVerdict = "profile from a question is a guess"
	gateDemotedToTask   gateVerdict = "fact from a question/command is current work, kept as task"
	gateTaskGivenExpiry gateVerdict = "task without an end date expires in a week"
)

func (v gateVerdict) dropped() bool {
	switch v {
	case gateDropDocument, gateDropPreference, gateDropProfile,
		gateDropReported, gateDropPasted, gateDropLookup:
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
	// Pasted material is checked before the durable cues: an official letter
	// pasted in for rewriting says "chúng tôi" throughout, and a chat log
	// pasted in for advice says "em đang" — neither is the user speaking.
	if isPastedMaterial(source) && !hasAny(text, explicitMemoryCues) {
		return decision, gateDropPasted
	}
	if !durable {
		switch {
		case reportsSomeoneElse(text):
			return decision, gateDropReported
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
		// After the question rule: a short question is still a question and
		// keeps its week as current work.
		case isLookupPhrase(text):
			return decision, gateDropLookup
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

// reportedSpeechCues mark a sentence that passes on what somebody else said
// or did: "chỗ HN nói là chỉ mua USDT", "anh Hùng bảo là hồ sơ bị trả lại".
// The user is asking about a situation; nothing in it describes them, and
// stored as a fact it is recalled into their later, unrelated questions.
var reportedSpeechCues = []string{
	" nói là", " nói rằng", " bảo là", " bảo rằng", " kể là", " kể rằng", " nhắn là", " nhắn rằng",
	" báo là", " báo rằng", " cho biết là", " cho biết rằng", "theo lời", "nghe nói", "người ta nói",
	"họ nói", "họ bảo", " said that", " says that", " told me", "according to",
}

func reportsSomeoneElse(text string) bool {
	return hasAny(text, reportedSpeechCues)
}

// explicitMemoryCues are the few phrasings that ask for something to be kept
// even inside pasted material ("Hãy nhớ: ..." followed by a profile).
var explicitMemoryCues = []string{
	"hãy nhớ", "ghi nhớ", "nhớ là", "nhớ rằng", "nhớ giúp", "từ giờ", "từ nay", " remember ",
}

// pastedMaterialRunes is the length past which a message is material the user
// brought along (a letter, a report, a chat log) rather than something they
// typed about themselves. Real self-descriptions in the eval set and on the
// dev store are all under 300 characters.
const pastedMaterialRunes = 1200

// isPastedMaterial reports whether a message is a long paste or a chat log:
// two or more lines that open with a speaker label ("Chị Lan: ...").
func isPastedMaterial(source string) bool {
	if len([]rune(source)) > pastedMaterialRunes {
		return true
	}
	speakerLines := 0
	for _, line := range strings.Split(source, "\n") {
		if hasSpeakerLabel(strings.TrimSpace(line)) {
			speakerLines++
		}
	}
	return speakerLines >= 2
}

// hasSpeakerLabel matches "Name: text" where Name is one to four words with no
// digits — the shape of a copied chat or meeting transcript. "Bộ phận: Đội 4"
// on a single line is a form field and is never counted alone.
func hasSpeakerLabel(line string) bool {
	colon := strings.Index(line, ":")
	if colon <= 0 || colon == len(line)-1 {
		return false
	}
	label := strings.TrimSpace(line[:colon])
	words := strings.Fields(label)
	if len(words) == 0 || len(words) > 4 {
		return false
	}
	for _, r := range label {
		if !unicode.IsLetter(r) && r != ' ' && r != '.' {
			return false
		}
	}
	return true
}

// statementMarkers make a short sentence say something happened or has to
// happen: "Mai phải nộp báo cáo quý". A short line with none of them and no
// first person is a search phrase: "xử lý vphc trong lĩnh vực giao thông",
// "giá hornor magic 9 pro". Its subject is counted by the topic tracker; the
// phrase itself describes nobody.
var statementMarkers = []string{
	"phải", "cần", "sẽ", "đang", "đã", "vừa", "sắp", "định", "muốn", "được giao", "giao cho",
	"hôm nay", "ngày mai", "mai ", "tuần", "tháng", "năm nay", "sáng nay", "chiều nay", "tối nay",
	"xong", "rồi", "chưa", "hết",
	" will ", " must ", " need ", " have to ", " is ", " are ", " was ", " has ", " had ",
}

// lookupPhraseMaxWords bounds the lookup rule to phrases short enough to be a
// query. Longer statements without a marker are left to the model.
const lookupPhraseMaxWords = 10

func isLookupPhrase(text string) bool {
	if text == "" || strings.Contains(text, ":") || strings.ContainsAny(text, ".!") {
		return false
	}
	if len(strings.Fields(text)) > lookupPhraseMaxWords {
		return false
	}
	padded := " " + text + " "
	for _, marker := range statementMarkers {
		if strings.Contains(padded, " "+strings.TrimSpace(marker)+" ") {
			return false
		}
	}
	return true
}

// changeCues mark a sentence that says something stopped being true or was
// replaced. The model may only retire a note when the user's own words say
// so; a delete without one is a model guess, and an unguarded delete is the
// most damaging mistake the extractor can make — mem0 dropped its
// model-chosen DELETE for the same reason. The same cues let a new fact
// replace an older one under the same topic.
var changeCues = []string{
	"không còn", "không phải", "không làm", "không phụ trách", "không dùng", "không cần",
	"thôi ", "bỏ ", "đã chuyển", "chuyển sang", "chuyển công tác", "chuyển về", "đổi sang", "nghỉ",
	"xong", "hoàn thành", "đã nộp", "nộp rồi", "hết hạn", "hủy", "huỷ", "nhầm", "sai rồi", "quên",
	"không đúng", "giờ là", "nay là", "hiện là", "bây giờ",
	"no longer", "not anymore", "stopped", "moved to", "finished", "done", "cancel", "forget", "wrong",
}

// saysSomethingChanged reports whether a sentence says something stopped
// being true or was replaced.
func saysSomethingChanged(text string) bool {
	return hasAny(normalizeForGate(text), changeCues)
}

// deleteHasCue reports whether the user's own sentence allows a delete.
func deleteHasCue(source string) bool { return saysSomethingChanged(source) }
