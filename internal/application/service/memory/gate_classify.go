package memory

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// What kind of message a user sentence is, which is all the write gate needs
// to know about it. The word-list rules (ruleClass) needed a patch for every
// new phrasing; the platform's small model (llm.auxiliary_model_id, Gemma
// E4B) answers the same question by meaning. Measured on 261 labelled dev
// and eval messages plus 94 held out, it let 3 and 1 non-statements through
// where the rules let 30 and 18, at ~120 ms a call. The rules stay as the
// fallback when the model is off, slow or answers outside the labels, and the
// structural checks (pasted material, explicit "hãy nhớ", delete cues) stay
// in code whatever the model says. Rows the model let through are marked
// (MemoryItem.GateSource) so the daily rules-only re-check (regateStored)
// does not undo the model's call; the model is not run there because that
// pass archives existing rows every day and must not flip with the model.
//
// The model is set in the platform memory configuration (gate_model_id);
// blank uses the platform auxiliary model, "off" keeps the rules.
type sentenceClass string

const (
	classSelf     sentenceClass = "self"
	classReported sentenceClass = "reported"
	classPasted   sentenceClass = "pasted"
	classDocument sentenceClass = "document"
	classCommand  sentenceClass = "command"
	classQuestion sentenceClass = "question"
	classLookup   sentenceClass = "lookup"
	classOther    sentenceClass = "other"
)

var sentenceClasses = []sentenceClass{
	classSelf, classReported, classPasted, classDocument, classCommand, classQuestion, classLookup, classOther,
}

func parseSentenceClass(s string) (sentenceClass, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, c := range sentenceClasses {
		if string(c) == s {
			return c, true
		}
	}
	return "", false
}

// ruleClass is the class the word-list rules give a message, in the order the
// gate always applied them. Anything they do not recognise is let through as
// a statement (self), which is how the gate behaved before the model.
func ruleClass(source string) sentenceClass {
	text := normalizeForGate(source)
	if isPastedMaterial(source) && !hasAny(text, explicitMemoryCues) {
		return classPasted
	}
	if hasAny(text, durableCues) {
		return classSelf
	}
	switch {
	case reportsSomeoneElse(text):
		return classReported
	case mentionsOpenDocument(text), startsWithCommand(text) && hasAny(text, documentNouns):
		return classDocument
	case startsWithCommand(text):
		return classCommand
	case looksLikeQuestion(text):
		return classQuestion
	case isLookupPhrase(text):
		return classLookup
	}
	return classSelf
}

const (
	gateClassifyTimeout = 3 * time.Second
	// gateClassifySlow is an answer slow enough to count against the run;
	// gateClassifyMaxSlow such answers switch the rest of the run to rules,
	// so a struggling model cannot stretch an extraction run by minutes.
	gateClassifySlow     = 1500 * time.Millisecond
	gateClassifyMaxSlow  = 2
	gateClassifyMaxRunes = 1500
	// gateClassifierEnv turns the model off ("off") and leaves the rules.
	gateClassifierEnv = "WEKNORA_MEMORY_GATE_CLASSIFIER"
)

var gateClassifySchema = json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","enum":["self","reported","pasted","document","command","question","lookup","other"]}},"required":["label"]}`)

const gateClassifyPrompt = `Bạn phân loại MỘT tin nhắn người dùng gửi cho trợ lý AI của cán bộ, công chức Việt Nam (tra cứu pháp luật, soạn văn bản, có văn bản Word đang mở bên cạnh). Mục đích: biết tin nhắn có KHẲNG ĐỊNH điều gì về chính người dùng để ghi nhớ hay không.

Nhãn (chọn đúng một):
- self: người dùng khẳng định điều về bản thân họ: công việc, đơn vị, chức vụ, công cụ, thói quen, sở thích; việc họ phải làm, hạn nộp, tiến độ, việc đã xong hoặc đã thay đổi; hoặc nhờ ghi nhớ / quên. Câu không có chủ ngữ vẫn là self khi nói việc của người dùng. Ví dụ: "Tôi công tác ở phòng Tư pháp huyện", "Bên em đang dùng phần mềm QLVB của tỉnh", "Thứ 6 phải gửi kế hoạch cho Văn phòng UBND", "Tờ trình xong rồi, đã trình ký sáng nay", "Họ tên: Minh / Chức vụ: Phó trưởng phòng". Yêu cầu LÂU DÀI về cách trả lời (từ giờ, luôn, mỗi khi, lần sau, mặc định) cũng là self: "Từ giờ trả lời bằng gạch đầu dòng", "Mỗi lần tóm tắt thì để số liệu lên đầu". Nếu vừa khẳng định về bản thân vừa hỏi, vẫn là self.
- reported: thuật lại lời nói, ý kiến, lời khai, việc làm của NGƯỜI KHÁC mà không phải việc của người dùng. Ví dụ: "chị Mai bảo là sở chưa nhận hồ sơ", "bên nhà thầu báo là chậm 2 tuần", "đối tượng khai đã bán xe". KHÔNG phải reported: việc người dùng phải làm hoặc được giao, kể cả khi nhắc người giao hay nơi nhận ("trưởng phòng giao tôi rà soát hồ sơ", "phải nộp cho Sở Tài chính", "gửi UBND huyện trước thứ 6") — đó là self.
- pasted: tài liệu dán vào: công văn, đoạn trích dài, nhật ký chat, biên bản.
- document: yêu cầu một lần về văn bản đang mở, tệp hoặc ảnh đính kèm, hoặc hỏi về nội dung trong đó. Ví dụ: "rút gọn đoạn 2", "ảnh này là gì", "kiểm tra lỗi chính tả văn bản", "văn bản này có mấy phụ lục".
- command: yêu cầu làm một việc một lần, không về văn bản đang mở. Ví dụ: "soạn tờ trình xin kinh phí", "tóm tắt Luật Đất đai 2024", "kiểm tra lại câu trả lời trên".
- question: câu hỏi tìm thông tin, kể cả hỏi về chính người dùng ("tôi làm ở đâu?", "bạn nhớ gì về tôi?") và câu có từ hỏi (gì, nào, sao, bao nhiêu, như thế nào, có … không, ở đâu, ai). Ví dụ: "thời hiệu xử phạt hành chính là bao lâu?", "điều kiện cấp giấy phép lái xe như thế nào".
- lookup: cụm từ tìm kiếm trơ trọi, KHÔNG có từ hỏi, không có động từ yêu cầu, không phải lời kể; kể cả một dãy số (CCCD, điện thoại) đứng một mình. Ví dụ: "mức phạt vượt đèn đỏ xe máy", "thông tư 15/2023", "Số CMND 012345678".
- other: chào hỏi, cảm ơn, yêu cầu về cách trả lời CHỈ cho câu này (không có từ giờ, luôn, mỗi khi), lựa chọn ngắn ("2", "ok", "đồng ý"), trả lời giải nghĩa từ viết tắt ("UBND = Ủy ban nhân dân", "là PV01 nhé"), chuỗi thử.

Trả lời đúng một đối tượng JSON {"label": "<nhãn>"}.`

// classifySentence asks a chat model for the class of one user message.
func classifySentence(ctx context.Context, model chat.Chat, text string) (sentenceClass, error) {
	if r := []rune(text); len(r) > gateClassifyMaxRunes {
		text = string(r[:gateClassifyMaxRunes]) + " …"
	}
	ctx, cancel := context.WithTimeout(ctx, gateClassifyTimeout)
	defer cancel()
	thinking := false
	resp, err := model.Chat(ctx, []chat.Message{
		{Role: "system", Content: gateClassifyPrompt},
		{Role: "user", Content: "Tin nhắn:\n" + text},
	}, &chat.ChatOptions{Temperature: 0, MaxCompletionTokens: 32, Thinking: &thinking, Format: gateClassifySchema})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errGateClassifier("empty response")
	}
	content := strings.TrimSpace(resp.Content)
	var out struct {
		Label string `json:"label"`
	}
	if start, end := strings.Index(content, "{"), strings.LastIndex(content, "}"); start >= 0 && end > start {
		if json.Unmarshal([]byte(content[start:end+1]), &out) == nil {
			content = out.Label
		}
	}
	class, ok := parseSentenceClass(strings.Trim(content, "\"' `."))
	if !ok {
		return "", errGateClassifier("label outside the set: " + truncateForLog(resp.Content, 60))
	}
	return class, nil
}

type errGateClassifier string

func (e errGateClassifier) Error() string { return "gate classifier: " + string(e) }

func truncateForLog(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// gateClassifier classifies the source sentences of one extraction run. A
// model failure switches the rest of the run to the rules, so a down model
// costs one timeout, not one per decision.
type gateClassifier struct {
	model   chat.Chat
	modelID string
	cache   map[string]sentenceClass
	slow    int
}

// gateModelID is the classifier model the memory configuration names: its
// gate_model_id, else the platform auxiliary model; "" means rules only.
func (s *Service) gateModelID(ctx context.Context, cfg *types.MemoryConfig) string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(gateClassifierEnv)), types.MemoryGateModelOff) {
		return ""
	}
	if cfg != nil && cfg.GateModelID != "" {
		if strings.EqualFold(cfg.GateModelID, types.MemoryGateModelOff) {
			return ""
		}
		return cfg.GateModelID
	}
	if s.systemSettings == nil {
		return ""
	}
	return strings.TrimSpace(s.systemSettings.GetString(ctx, types.AuxiliaryModelSettingKey,
		"WEKNORA_AUXILIARY_MODEL_ID", types.DefaultAuxiliaryModelID))
}

// newGateClassifier resolves the classifier model; without one (turned off,
// unset, tests without settings) the classifier is the rules alone.
func (s *Service) newGateClassifier(ctx context.Context, cfg *types.MemoryConfig) *gateClassifier {
	g := &gateClassifier{cache: map[string]sentenceClass{}}
	if s.modelService == nil {
		return g
	}
	modelID := s.gateModelID(ctx, cfg)
	if modelID == "" {
		return g
	}
	model, err := s.modelService.GetChatModel(ctx, modelID)
	if err != nil || model == nil {
		logger.Warnf(ctx, "memory: gate classifier model %s unavailable, using rules: %v", modelID, err)
		return g
	}
	g.model, g.modelID = model, modelID
	return g
}

// classOf is the class of a source sentence and whether the model gave it.
func (g *gateClassifier) classOf(ctx context.Context, source string) (sentenceClass, bool) {
	if g == nil || g.model == nil || strings.TrimSpace(source) == "" {
		return ruleClass(source), false
	}
	if c, ok := g.cache[source]; ok {
		return c, true
	}
	start := time.Now()
	c, err := classifySentence(ctx, g.model, source)
	if took := time.Since(start); err == nil && took > gateClassifySlow {
		if g.slow++; g.slow >= gateClassifyMaxSlow {
			logger.Warnf(ctx, "memory: gate classifier %s answered slowly %d times (last %v), using rules for the rest of the run",
				g.modelID, g.slow, took.Round(time.Millisecond))
			g.model = nil
		}
	}
	if err != nil {
		logger.Warnf(ctx, "memory: gate classifier %s failed, using rules for the rest of the run: %v", g.modelID, err)
		g.model = nil
		return ruleClass(source), false
	}
	g.cache[source] = c
	return c, true
}
