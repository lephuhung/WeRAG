package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// A new NĐ30 document drafted from chat for the Word add-in (its contract
// docs/backend-document-template-contract.md). The model writes the content
// from what the user said (trích yếu, the body, nơi nhận, the signer they
// named) as JSON; code fills everything the server knows and the model must
// not choose: the agency, its parent and the place name from the
// workspace's agency profile, the drafting unit from the user's memory, the
// date, the unnumbered số ký hiệu, the signature block built from the signer
// and the form of signing the user stated. Code also refuses what the model
// would invent: a signer, a form of signing, a legal ground, a proposing
// unit or a number the user never wrote. Until all is known the answer is
// needs_input with what is missing; the add-in writes nothing before ready.
// The user may skip the questions ("bỏ qua", "để trống"): the document is
// then built with what is known and a blank (templateBlank) wherever a
// value is missing, for them to fill in; still nothing is invented.

// TemplateUserContext is what is remembered of the person drafting.
type TemplateUserContext struct {
	// Unit is the drafting unit (phòng, bộ phận) and UnitAbbr its
	// abbreviation in số ký hiệu and the "Lưu" line.
	Unit     string `json:"unit,omitempty"`
	UnitAbbr string `json:"unit_abbr,omitempty"`
	// Position is the user's own chức vụ.
	Position string `json:"position,omitempty"`
}

// DocumentTemplateInput is one turn of a template request.
type DocumentTemplateInput struct {
	Request *types.DocumentTemplateRequest
	// Profile is the workspace's agency profile (nil: none saved).
	Profile *types.TenantAgencyProfile
	// User is what memory holds of the person drafting.
	User TemplateUserContext
	// WorkUnit and Province are the user's work context (profile
	// preferences: đơn vị công tác, tỉnh/thành phố), used for the agency and
	// the place name when the workspace has no agency profile, after the
	// user confirms them.
	WorkUnit string
	Province string
	// Inputs are the user's messages of this request, oldest first, the
	// current one last.
	Inputs []string
	Today  time.Time
	Model  chat.Chat
	// Asked are the conflicts with the profile already put to the user
	// (field → the user's value); asked again, the user's value stands.
	Asked map[string]string
	// KeepBlanks: the request drafts again a document the user already had
	// built ("tạo lại công văn"), so what was left blank then stays blank
	// and nothing is asked again.
	KeepBlanks bool
	// Conversation is the earlier conversation the document is drafted
	// from (the agent hand-off), oldest first; nil for a plain request.
	Conversation []TemplateTurn
}

// TemplateTurn is one message of the earlier conversation.
type TemplateTurn struct {
	// Role is "user" or "assistant".
	Role string `json:"role"`
	Text string `json:"text"`
}

// conversationUserText is what the user wrote in the earlier conversation.
func conversationUserText(turns []TemplateTurn) string {
	var b strings.Builder
	for _, t := range turns {
		if t.Role == "user" {
			b.WriteString(t.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// selfInputs are the user's messages that may say who the user is: the
// first-person ones ("tôi là chuyên viên Văn phòng"), the earlier
// conversation's included, and the replies of this request after its first
// message, which answer the questions about the user ("Bộ phận: Đội 4",
// "VP" right under "Phòng/bộ phận soạn thảo của bạn …?") unless they speak
// of the signer.
func selfInputs(in DocumentTemplateInput) []string {
	var out []string
	for _, t := range in.Conversation {
		if t.Role == "user" && selfRe.MatchString(strings.ToLower(t.Text)) {
			out = append(out, t.Text)
		}
	}
	for i, m := range in.Inputs {
		lower := strings.ToLower(m)
		if selfRe.MatchString(lower) || (i > 0 && !signerTalkRe.MatchString(lower)) {
			out = append(out, m)
		}
	}
	return out
}

// signerTalkRe marks a reply about the signer, whose unit and position are
// not the user's.
var signerTalkRe = regexp.MustCompile(`người ký|(^|[^\p{L}])ký([^\p{L}]|$)`)

// conversationAssistantText is what the other agent wrote there (drafts the
// user asked to put into the document).
func conversationAssistantText(turns []TemplateTurn) string {
	var b strings.Builder
	for _, t := range turns {
		if t.Role == "assistant" {
			b.WriteString(t.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// DocumentTemplateOutcome is the answer to one turn.
type DocumentTemplateOutcome struct {
	// Data is the prepare_document_template payload.
	Data map[string]interface{}
	// Answer is the chat text of the turn.
	Answer string
	Ready  bool
	// Learned is the drafting context the user stated in this request, to
	// remember (nil when none).
	Learned *TemplateUserContext
	// Asked is Input.Asked with the conflicts asked in this turn.
	Asked map[string]string
}

// templateDraft is the model's JSON.
type templateDraft struct {
	AgencyName       string `json:"agency_name"`
	ParentAgency     string `json:"parent_agency"`
	AgencyAbbr       string `json:"agency_abbr"`
	Locality         string `json:"locality"`
	DraftingUnit     string `json:"drafting_unit"`
	DraftingUnitAbbr string `json:"drafting_unit_abbr"`
	UserPosition     string `json:"user_position"`
	Subject          string `json:"subject"`
	Addressee        string `json:"addressee"`
	Paragraphs       []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"paragraphs"`
	Recipients   []string `json:"recipients"`
	Grounds      []string `json:"grounds"`
	RequestBasis string   `json:"request_basis"`
	Signer       struct {
		Name       string `json:"name"`
		Position   string `json:"position"`
		Authority  string `json:"authority"`
		OnBehalfOf string `json:"on_behalf_of"`
	} `json:"signer"`
	MissingContent []string `json:"missing_content"`
	// Form holds the facts of a form type (templateForms), by key.
	Form map[string]formValue `json:"form"`
	// AgencyInSentence is the agency's name as a sentence writes it ("Sở
	// Nội vụ"), for the forms that name it in their body.
	AgencyInSentence string `json:"agency_in_sentence"`
}

// templateParagraph is one paragraph of the body in the add-in's payload.
type templateParagraph struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Limits of the add-in's payload.
const (
	templateTextMax       = 2000
	templateParagraphMax  = 20000
	templateParagraphsMax = 300
	templateRecipientsMax = 100
	templateGroundsMax    = 50
	templateMissingMax    = 30
	templateMaxTokens     = 6000
	// templateContentAsksMax caps the content facts the model may ask for.
	templateContentAsksMax = 3
	// templateBlank stands for a value the user chose to fill in later.
	templateBlank = "……"
)

// skipAsksRe: the user's message asks to build the document now and leave
// what is missing blank.
var skipAsksRe = regexp.MustCompile(`bỏ qua|để trống|điền sau|bổ sung sau|tự điền|cứ tạo|tạo luôn|tạo ngay|khỏi hỏi|không cần hỏi|không cần (thêm )?thông tin|\bskip\b|leave (it |them )?blanks?`)

// templatePromptRe is the composer's prefill of a picked type
// (frontend templatePrompt): sent as it is, it says nothing yet.
var templatePromptRe = regexp.MustCompile(`tạo cho tôi văn bản [\p{L} ]+ theo nđ ?30,? thông tin tôi cần là:?`)

// emptyAskWords are the words that remain of a skip once its phrase is
// gone ("bỏ qua các thông tin còn thiếu, tạo văn bản và để trống …").
var emptyAskWords = map[string]bool{
	"các": true, "thông": true, "tin": true, "còn": true, "thiếu": true, "tạo": true, "văn": true, "bản": true,
	"và": true, "để": true, "tôi": true, "mình": true, "nhé": true, "nha": true, "ạ": true, "đi": true,
	"luôn": true, "cho": true, "giúp": true, "hết": true, "tất": true, "cả": true, "phần": true, "này": true,
	"thôi": true, "ok": true, "vâng": true, "được": true, "rồi": true, "trống": true, "trắng": true, "mới": true,
}

// keepBlankRe: a message that turns the template down ("bỏ qua", "không
// cần mẫu", "văn bản trắng"); buildNowRe: one that asks for the document
// now ("cứ tạo luôn", "tạo văn bản và để trống …", the skip button of a
// needs_input), which builds it with blanks.
var (
	keepBlankRe = regexp.MustCompile(`bỏ qua|\bskip\b|không cần mẫu|không dùng mẫu|(?:giữ |để )?văn bản trắng`)
	buildNowRe  = regexp.MustCompile(`cứ tạo|tạo luôn|tạo ngay|tạo văn bản|khỏi hỏi|không cần hỏi|\bcreate\b`)
)

// saidNothing: no message of the request holds anything but the prefill
// and a refusal ("bỏ qua" at once): the user wants the empty document as it
// is. Asking for the document now is no refusal.
func saidNothing(inputs []string) bool {
	if len(inputs) == 0 || !keepBlankRe.MatchString(fold(inputs[len(inputs)-1])) {
		return false
	}
	for _, m := range inputs {
		f := fold(m)
		if buildNowRe.MatchString(f) {
			return false
		}
		t := keepBlankRe.ReplaceAllString(skipAsksRe.ReplaceAllString(templatePromptRe.ReplaceAllString(f, " "), " "), " ")
		for _, w := range strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if !emptyAskWords[w] {
				return false
			}
		}
	}
	return true
}

// wantsBlanks: the current message (the last input) skips the questions.
func wantsBlanks(inputs []string) bool {
	return len(inputs) > 0 && skipAsksRe.MatchString(fold(inputs[len(inputs)-1]))
}

// blankedLabels are the questions of a skipped needs_input as what was
// left blank: a conflict with the agency profile kept the saved value (not
// blank), an unconfirmed work context left the agency and the place blank.
func blankedLabels(missing []string) []string {
	var out []string
	for _, m := range missing {
		switch {
		case strings.HasPrefix(m, "Xác nhận các số liệu"):
			out = append(out, "Các số liệu, thời hạn bạn chưa nêu trong nội dung")
		case strings.HasPrefix(m, "Xác nhận thông tin lấy từ hồ sơ công tác"):
			out = append(out, "Tên cơ quan ban hành và địa danh")
		case strings.HasPrefix(m, "Xác nhận "):
			// the profile's value was used
		default:
			out = append(out, m)
		}
	}
	return out
}

// blankInvented replaces the numbers and placeholders the user never wrote
// (draftViolations) with templateBlank.
func blankInvented(s string, invented []string) string {
	for _, v := range invented {
		if ph := strings.TrimPrefix(v, "placeholder "); ph != v {
			s = strings.ReplaceAll(s, ph, templateBlank)
			continue
		}
		n := regexp.QuoteMeta(strings.TrimPrefix(v, "the number "))
		s = regexp.MustCompile(`(^|\D)`+n+`(\D|$)`).ReplaceAllString(s, "${1}"+templateBlank+"${2}")
	}
	return s
}

// templateTypeCodes are the số ký hiệu codes of the types (NĐ30 Phụ lục
// III); công văn has none (<cơ quan>-<đơn vị soạn thảo>).
var templateTypeCodes = map[string]string{
	"ke-hoach": "KH", "bao-cao": "BC", "to-trinh": "TTr", "thong-bao": "TB", "quyet-dinh": "QĐ",
	"chi-thi": "CT", "nghi-quyet": "NQ", "cong-dien": "CĐ", "giay-moi": "GM", "giay-gioi-thieu": "GGT",
	"bien-ban": "BB", "giay-nghi-phep": "GNP",
}

// decisionTemplate: the types issued "Căn cứ …" by an authority line, the
// body after "QUYẾT ĐỊNH:" / "QUYẾT NGHỊ:".
func decisionTemplate(templateID string) bool {
	return templateID == "quyet-dinh" || templateID == "nghi-quyet"
}

// untitledSubject: the forms printed without a trích yếu line (Mẫu 1.8,
// 1.10); their subject only names the document in the chat.
func untitledSubject(templateID string) bool {
	return templateID == "giay-gioi-thieu" || templateID == "giay-nghi-phep"
}

// templateOutlines tell the model the usual parts of each type.
var templateOutlines = map[string]string{
	"cong-van":        `Công văn: no title line. paragraphs = the body as "body" paragraphs (context, request, deadline as the user gave it), the last one ending with "./.". addressee = who it is sent to (without "Kính gửi:").`,
	"ke-hoach":        `Kế hoạch: "heading" paragraphs "I. MỤC ĐÍCH, YÊU CẦU", "II. NỘI DUNG", "III. TỔ CHỨC THỰC HIỆN" (sub-headings "1. …" also "heading"), each followed by "body" paragraphs.`,
	"bao-cao":         `Báo cáo: "heading" paragraphs "I. TÌNH HÌNH, KẾT QUẢ", "II. ĐÁNH GIÁ", "III. KIẾN NGHỊ, ĐỀ XUẤT", each followed by "body" paragraphs.`,
	"to-trinh":        `Tờ trình: "heading" paragraphs "I. SỰ CẦN THIẾT", "II. NỘI DUNG ĐỀ NGHỊ", "III. KIẾN NGHỊ", each followed by "body" paragraphs; addressee = the authority asked to approve (without "Kính gửi:").`,
	"thong-bao":       `Thông báo: "body" paragraphs stating what is announced, to whom and from when, as the user gave it; the last one ending with "./.".`,
	"quyet-dinh":      `Quyết định cá biệt: paragraphs are the articles: an "article" paragraph "Điều 1. <short title>" followed by "body" paragraphs, then "Điều 2." …; the last article is about hiệu lực and trách nhiệm thi hành. grounds = the "Căn cứ …;" lines the user gave (each starting "Căn cứ", ending ";"); request_basis = "Theo đề nghị của <the proposing unit the user named>.".`,
	"nghi-quyet":      `Nghị quyết cá biệt (the system writes "QUYẾT NGHỊ:"): paragraphs are the articles: an "article" paragraph "Điều 1. <short title>" followed by "body" paragraphs, then "Điều 2." …; the last article is about tổ chức thực hiện and hiệu lực. grounds = the "Căn cứ …;" lines the user gave (each starting "Căn cứ", ending ";"); request_basis = the "Xét …" / "Theo đề nghị của …" line only when the user named that proposal, else "".`,
	"chi-thi":         `Chỉ thị: no "Kính gửi", no "Căn cứ". "body" paragraphs stating the situation and why the directive is issued, then the tasks as "heading" paragraphs "1. …", "2. …" each followed by "body" paragraphs assigning them to the units the user named; the last paragraph asks for strict implementation and ends with "./.".`,
	"cong-dien":       `Công điện: subject = the trích yếu. paragraphs = the message as "body" paragraphs (the situation, then what each receiver must do), the last one ending with "./.". Put the receivers in form.receivers, not in paragraphs.`,
	"giay-moi":        `Giấy mời: subject = the trích yếu nội dung cuộc họp. paragraphs = only the "các vấn đề cần lưu ý" the user gave (documents to bring, confirmation), else []. The invitation lines are written from form.`,
	"giay-gioi-thieu": `Giấy giới thiệu: subject = "". paragraphs = []. Everything is in form.`,
	"giay-nghi-phep":  `Giấy nghỉ phép: subject = "". paragraphs = []. Everything is in form.`,
	"bien-ban":        `Biên bản: subject = the name of the meeting (hội nghị, hội thảo). paragraphs = the proceedings (diễn biến, ý kiến, kết luận) as "body" paragraphs, written only from what the user gave. signer = the chair (chủ tọa); the secretary goes in form.`,
}

const templateSystemPrompt = `You draft a Vietnamese administrative document (văn bản hành chính, Nghị định 30/2020/NĐ-CP) from what the user wrote. Answer with ONE JSON object only, no prose, no code fence:
{"agency_name":"","parent_agency":"","agency_abbr":"","locality":"","drafting_unit":"","drafting_unit_abbr":"","user_position":"","subject":"","addressee":"","paragraphs":[{"kind":"heading|body|article","text":""}],"recipients":["- …;"],"grounds":[],"request_basis":"","signer":{"name":"","position":"","authority":"","on_behalf_of":""},"missing_content":[]}

Rules:
- Write in Vietnamese, formal administrative style. Use ONLY facts the user wrote: never invent names, numbers, dates, deadlines, amounts, legal grounds (căn cứ), document numbers, a signer or a form of signing. Leave a field "" (or []) when the user did not give it.
- agency_name, parent_agency, agency_abbr, locality: only when the user wrote them in their messages (they override the unit profile); else "".
- drafting_unit / drafting_unit_abbr: the user's phòng / bộ phận soạn thảo and its abbreviation, only when the user wrote them; user_position: the user's own chức vụ, only when written.
- subject: the trích yếu (what the document is about), without the type name and without "V/v".
- paragraphs: the body written from the user's information. Do not repeat the header, the title, "Kính gửi", the signature or "Nơi nhận". No placeholders in square brackets: where a fact the body needs is missing, list it in missing_content instead.
- recipients: the "Nơi nhận" lines the user named, each "- <recipient>;" ("- Như trên;" for a công văn / tờ trình). Do NOT add the "Lưu" line.
- signer: only the person the user said signs: name, position (their chức vụ), authority = "truc_tiep" (signs in their own name), "TM", "KT", "TL", "TUQ" or "Q" exactly as the user stated it, "" if they did not say; on_behalf_of = the collective (after TM.) or the head's position (after KT./TL./TUQ./Q.) when the user wrote it. Never deduce the authority from a title.
- The system itself writes: the agency header, quốc hiệu and tiêu ngữ, the type name and trích yếu lines, số ký hiệu, place and date, the issuing authority line, the grounds and "Theo đề nghị" lines (from grounds / request_basis), "Kính gửi", "QUYẾT ĐỊNH:", the signature block and "Nơi nhận". Never put them in paragraphs and never ask for them in missing_content.
- missing_content: at most 3 short Vietnamese labels of facts the body cannot be written without at all (e.g. "Nội dung đề nghị", "Đối tượng áp dụng"). Do not ask for details a draft can leave general (budget, venue, detailed schedule, legal grounds of a plan or report): write general sentences that follow from the request, without adding facts. [] when the body can be written.`

// PrepareDocumentTemplate answers one turn of a template request.
func PrepareDocumentTemplate(ctx context.Context, in DocumentTemplateInput) (*DocumentTemplateOutcome, error) {
	if in.Request == nil || in.Model == nil {
		return nil, fmt.Errorf("document template: no request or model")
	}
	// "bỏ qua" before saying anything: the document stays empty, nothing is
	// drafted (no model call); the client closes the request and stops
	// offering the templates for this document
	if len(in.Conversation) == 0 && !in.KeepBlanks && saidNothing(in.Inputs) {
		r := in.Request
		return &DocumentTemplateOutcome{
			Data: map[string]interface{}{
				"schema_version": 1, "request_id": r.RequestID, "document_id": r.DocumentID, "template_id": r.TemplateID,
				"status": "skipped",
			},
			Answer: "Đã giữ văn bản trắng, không tạo mẫu. Bạn có thể soạn trực tiếp trong văn bản; khi cần mẫu NĐ30, chọn \"Tạo văn bản mới\" để mở một văn bản trống khác.",
		}, nil
	}
	// what the user wrote, here and in the conversation the request is
	// drafted from: the signer, the agency, the form of signing must be in
	// it. Facts of the body may also come from what the other agent drafted
	// there, since the user asked for that content in the document.
	userText := conversationUserText(in.Conversation) + strings.Join(in.Inputs, "\n")
	factText := userText + "\n" + conversationAssistantText(in.Conversation)
	draft, err := draftTemplate(ctx, in, "")
	if err != nil {
		return nil, err
	}
	bad := draftViolations(draft, factText, in.Today)
	if len(bad) > 0 {
		// once more, told what was invented
		retry, err := draftTemplate(ctx, in, "Your previous answer used facts the user never wrote: "+strings.Join(bad, "; ")+
			". Remove them (or list what is needed in missing_content) and answer again.")
		if err == nil {
			draft = retry
			bad = draftViolations(draft, factText, in.Today)
		}
	}
	out := assembleTemplate(in, draft, userText, bad)
	return out, nil
}

// draftTemplate asks the model for the draft.
func draftTemplate(ctx context.Context, in DocumentTemplateInput, correction string) (*templateDraft, error) {
	var b strings.Builder
	name := types.DocumentTemplateNames[in.Request.TemplateID]
	fmt.Fprintf(&b, "Document type: %s.\n%s%s\n\n", name, templateOutlines[in.Request.TemplateID], formOutline(in.Request.TemplateID))
	b.WriteString("Unit profile (known, do not repeat in the body):\n")
	if p := in.Profile; p != nil {
		fmt.Fprintf(&b, "- cơ quan ban hành: %s\n- cơ quan chủ quản: %s\n- địa danh: %s\n", p.AgencyName, p.ParentAgency, p.Locality)
	} else {
		b.WriteString("- (none saved)\n")
	}
	if in.WorkUnit != "" || in.Province != "" {
		fmt.Fprintf(&b, "- the user's profile: work unit %q, province/city %q\n", in.WorkUnit, in.Province)
	}
	if u := in.User; u.Unit != "" || u.Position != "" {
		fmt.Fprintf(&b, "- the user's drafting unit: %s (%s); the user's position: %s\n", u.Unit, u.UnitAbbr, u.Position)
	}
	if len(in.Conversation) > 0 {
		b.WriteString("\nThe earlier conversation, oldest first. The user discussed the document there with another assistant and then asked for it as a Word file: " +
			"the facts the user settled there count as written by the user; a text the assistant drafted there is the content to put in the body " +
			"(keep its substance, wording and figures; drop its own header, signature and \"Nơi nhận\" lines, its placeholders in square brackets and its notes to the user). " +
			"Names, the signer and the form of signing count only when the USER wrote them.\n")
		for _, t := range in.Conversation {
			fmt.Fprintf(&b, "[%s] %s\n", t.Role, t.Text)
		}
	}
	b.WriteString("\nThe user's messages, oldest first:\n")
	for i, m := range in.Inputs {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, m)
	}
	msgs := []chat.Message{{Role: "system", Content: templateSystemPrompt + formSystemRule(in.Request.TemplateID)}, {Role: "user", Content: b.String()}}
	if correction != "" {
		msgs = append(msgs, chat.Message{Role: "user", Content: correction})
	}
	thinking := false
	resp, err := in.Model.Chat(ctx, msgs, &chat.ChatOptions{Temperature: 0.2, MaxTokens: templateMaxTokens, Thinking: &thinking})
	if err != nil {
		return nil, fmt.Errorf("document template: %w", err)
	}
	return parseTemplateDraft(resp.Content)
}

func parseTemplateDraft(reply string) (*templateDraft, error) {
	s := strings.TrimSpace(reply)
	if i := strings.Index(s, "</think>"); i >= 0 {
		s = s[i+len("</think>"):]
	}
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("document template: the model answered no JSON")
	}
	var d templateDraft
	if err := json.Unmarshal([]byte(RepairJSON(s[start:end+1])), &d); err != nil {
		return nil, fmt.Errorf("document template: unreadable JSON: %w", err)
	}
	return &d, nil
}

var (
	digitsRe      = regexp.MustCompile(`\d+`)
	placeholderRe = regexp.MustCompile(`\[[^\]]*\]`)
	// numbering at the start of a heading or article: "Điều 2.", "1.2."
	leadingNumberRe = regexp.MustCompile(`^\s*(?:Điều\s+\d+[.:]?|\d+(?:\.\d+)*[.)])\s*`)
)

// draftViolations lists what the draft states that the user never wrote:
// numbers (dates, deadlines, amounts, document numbers) and placeholders.
// Numbering of headings and articles and the current year are allowed.
func draftViolations(d *templateDraft, userText string, today time.Time) []string {
	known := map[string]bool{strconv.Itoa(today.Year()): true}
	for _, n := range digitsRe.FindAllString(userText, -1) {
		known[strings.TrimLeft(n, "0")] = true
	}
	var bad []string
	seen := map[string]bool{}
	check := func(s string) {
		for _, ph := range placeholderRe.FindAllString(s, -1) {
			if !seen[ph] {
				seen[ph] = true
				bad = append(bad, "placeholder "+ph)
			}
		}
		for _, n := range digitsRe.FindAllString(s, -1) {
			k := strings.TrimLeft(n, "0")
			if k == "" || known[k] || seen[n] {
				continue
			}
			seen[n] = true
			bad = append(bad, "the number "+n)
		}
	}
	check(d.Subject)
	check(d.Addressee)
	check(d.RequestBasis)
	for _, p := range d.Paragraphs {
		for _, line := range strings.Split(p.Text, "\n") {
			check(leadingNumberRe.ReplaceAllString(line, ""))
		}
	}
	for _, g := range d.Grounds {
		check(g)
	}
	for _, r := range d.Recipients {
		check(r)
	}
	for _, v := range d.Form {
		check(string(v))
	}
	return bad
}

// fold is the comparison form of a text: lower case, single spaces.
func fold(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// said reports whether the user wrote s (folded, ignoring end punctuation).
func said(userText, s string) bool {
	s = strings.Trim(fold(s), " .;:,")
	return s != "" && strings.Contains(fold(userText), s)
}

// subjectPrefixRe is a "V/v" / "về việc" the trích yếu must not start
// with (the add-in writes "V/v" before a công văn's).
var subjectPrefixRe = regexp.MustCompile(`(?i)^(v/v|về việc)\s*:?\s*`)

// selfRe marks a message where the user says who they are ("tôi là…",
// "tôi công tác ở…", "phòng của tôi…"); "Tạo cho tôi…" is not one.
var selfRe = regexp.MustCompile(`(^|[^\p{L}])(tôi|mình)\s+(là|làm|đang là|đang làm|công tác|thuộc|ở)([^\p{L}]|$)|của (tôi|mình)([^\p{L}]|$)`)

// saidOfSelf: one of the messages that may say who the user is
// (selfInputs) contains s.
func saidOfSelf(inputs []string, s string) bool {
	for _, m := range inputs {
		if said(m, s) {
			return true
		}
	}
	return false
}

// signing forms: the words the user must have written for each.
var authorityWords = map[string]*regexp.Regexp{
	"truc_tiep": regexp.MustCompile(`trực tiếp ký|ký trực tiếp|tự ký|đứng tên ký`),
	"TM":        regexp.MustCompile(`(^|[^\p{L}])tm\.|thay mặt`),
	"KT":        regexp.MustCompile(`(^|[^\p{L}])kt\.|ký thay`),
	"TL":        regexp.MustCompile(`(^|[^\p{L}])tl\.|thừa lệnh`),
	"TUQ":       regexp.MustCompile(`(^|[^\p{L}])tuq\.|thừa (ủy|uỷ) quyền`),
	"Q":         regexp.MustCompile(`(^|[^\p{L}])q\.\s|(^|[^\p{L}])quyền (giám đốc|chủ tịch|trưởng|chánh|viện trưởng|hiệu trưởng|cục trưởng|vụ trưởng|tổng)`),
}

// assembleTemplate checks the draft and builds the payload.
// invented are the facts the draft still states that the user never wrote
// (draftViolations): they hold it back as needs_input.
func assembleTemplate(in DocumentTemplateInput, d *templateDraft, userText string, invented []string) *DocumentTemplateOutcome {
	req := in.Request
	out := &DocumentTemplateOutcome{Asked: map[string]string{}}
	for k, v := range in.Asked {
		out.Asked[k] = v
	}
	var missing []string
	need := func(label string) { missing = append(missing, label) }

	// the agency: the profile, unless the user wrote another (asked once)
	profile := in.Profile
	if profile == nil {
		profile = &types.TenantAgencyProfile{}
	}
	pick := func(field, label, saved, stated string) string {
		stated = strings.TrimSpace(stated)
		if stated == "" || !said(userText, stated) {
			return saved
		}
		if saved == "" || fold(saved) == fold(stated) || fold(out.Asked[field]) == fold(stated) {
			return stated
		}
		// part of the saved name ("UBND tỉnh" of "UBND tỉnh Hà Tĩnh"): no
		// other agency, only less of it
		if strings.Contains(fold(saved), fold(stated)) {
			return saved
		}
		out.Asked[field] = stated
		need(fmt.Sprintf("Xác nhận %s: hồ sơ tổ chức ghi «%s», bạn ghi «%s» (gửi lại «%s» nếu vẫn dùng)", label, saved, stated, stated))
		return saved
	}
	agency := pick("agency_name", "cơ quan ban hành", profile.AgencyName, d.AgencyName)
	parent := pick("parent_agency", "cơ quan chủ quản", profile.ParentAgency, d.ParentAgency)
	locality := pick("locality", "địa danh", profile.Locality, d.Locality)
	abbr := pick("agency_abbr", "viết tắt tên cơ quan", profile.AgencyAbbr, d.AgencyAbbr)
	// no agency profile: the user's work context, once they confirm it
	guessAgency, guessLocality := "", ""
	if agency == "" {
		guessAgency = workUnitAgency(in.WorkUnit, in.Province)
	}
	if locality == "" {
		guessLocality = in.Province
	}
	if guessAgency != "" || guessLocality != "" {
		key := guessAgency + "|" + guessLocality
		if out.Asked["work_context"] == key {
			if guessAgency != "" {
				agency = guessAgency
			}
			if guessLocality != "" {
				locality = guessLocality
			}
		} else {
			out.Asked["work_context"] = key
			var parts []string
			if guessAgency != "" {
				parts = append(parts, "cơ quan ban hành «"+strings.ToUpper(guessAgency)+"»")
			}
			if guessLocality != "" {
				parts = append(parts, "địa danh «"+guessLocality+"»")
			}
			need("Xác nhận thông tin lấy từ hồ sơ công tác của bạn: " + strings.Join(parts, ", ") +
				" (trả lời «đúng», hoặc ghi lại cho đúng)")
		}
	}
	// a value being confirmed is not asked for again
	if agency == "" && guessAgency == "" {
		need("Tên cơ quan ban hành (tổ chức chưa có hồ sơ: quản trị tổ chức cập nhật tại Cài đặt › Hồ sơ tổ chức)")
	}
	if abbr == "" {
		need("Tên viết tắt của cơ quan (dùng trong số ký hiệu, ví dụ SNV)")
	}
	if locality == "" && guessLocality == "" {
		need("Địa danh ghi trước ngày tháng (ví dụ Huế)")
	}

	// the drafting unit: memory, or what the user wrote now about themself
	// (remembered): a unit with its abbreviation, a position, each in a
	// message in the first person — "chuyên viên Văn phòng" of someone the
	// document is about is not the user's
	user := in.User
	learned := TemplateUserContext{}
	if u, a := strings.TrimSpace(d.DraftingUnit), strings.TrimSpace(d.DraftingUnitAbbr); u != "" && a != "" &&
		saidOfSelf(selfInputs(in), u) && saidOfSelf(selfInputs(in), a) {
		learned.Unit, learned.UnitAbbr = u, a
	}
	if p := strings.TrimSpace(d.UserPosition); p != "" && saidOfSelf(selfInputs(in), p) {
		learned.Position = p
	}
	if learned.Unit != "" {
		user.Unit, user.UnitAbbr = learned.Unit, learned.UnitAbbr
	}
	if learned.Position != "" {
		user.Position = learned.Position
	}
	if learned != (TemplateUserContext{}) {
		out.Learned = &learned
	}
	// a biên bản's số ký hiệu and "Lưu" line do not name the unit
	if user.UnitAbbr == "" && req.TemplateID != "bien-ban" {
		label := "Phòng/bộ phận soạn thảo của bạn và tên viết tắt (ví dụ Văn phòng, VP)"
		if user.Position == "" {
			label += ", kèm chức vụ của bạn"
		}
		need(label)
	}

	// the content
	subject := strings.TrimSuffix(strings.TrimSpace(subjectPrefixRe.ReplaceAllString(strings.TrimSpace(d.Subject), "")), ".")
	form, formMissing := formValues(req.TemplateID, d, userText)
	switch {
	case untitledSubject(req.TemplateID):
		// no trích yếu line: the chat names it by the person
		subject = ""
		if form["person"] != "" {
			subject = "ông (bà) " + form["person"]
		}
	case subject == "":
		need("Trích yếu nội dung văn bản")
	default:
		subject = subjectCase(req.TemplateID, subject)
	}
	for _, m := range formMissing {
		need(m)
	}
	paragraphs := templateBody(d, req.TemplateID, agency, parent)
	asked := 0
	for _, m := range d.MissingContent {
		if m = strings.TrimSpace(m); m != "" && asked < templateContentAsksMax {
			asked++
			need(fitRunes(m, 200))
		}
	}
	// a giấy mời's notes are optional, the giấy giới thiệu and nghỉ phép
	// have only their form
	if len(paragraphs) == 0 && asked == 0 && req.TemplateID != "giay-moi" && !untitledSubject(req.TemplateID) {
		if req.TemplateID == "bien-ban" {
			need("Nội dung (diễn biến, kết luận) của cuộc họp")
		} else {
			need("Nội dung chính của văn bản")
		}
	}
	if len(invented) > 0 {
		var nums []string
		for _, v := range invented {
			nums = append(nums, strings.TrimPrefix(strings.TrimPrefix(v, "the number "), "placeholder "))
		}
		need("Xác nhận các số liệu, thời hạn trong nội dung (bạn chưa nêu): " + strings.Join(nums, ", "))
	}
	addressee := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d.Addressee), "Kính gửi:"))
	// the forms, a chỉ thị and a decision have no "Kính gửi"
	if templateForms[req.TemplateID] != nil || req.TemplateID == "chi-thi" || decisionTemplate(req.TemplateID) {
		addressee = ""
	}
	if (req.TemplateID == "cong-van" || req.TemplateID == "to-trinh") && addressee == "" {
		need("Nơi gửi (Kính gửi)")
	}
	var grounds []string
	requestBasis, decisionAuthority := "", ""
	if decisionTemplate(req.TemplateID) {
		for _, g := range d.Grounds {
			if g = strings.TrimSpace(g); g != "" && groundSaid(userText, g) {
				grounds = append(grounds, endWith(g, ";"))
			}
		}
		if len(grounds) > templateGroundsMax {
			grounds = grounds[:templateGroundsMax]
		}
		if len(grounds) == 0 || len(grounds) < len(d.Grounds) {
			need("Căn cứ ban hành: văn bản quy định thẩm quyền và căn cứ pháp lý (ghi rõ tên, số hiệu)")
			grounds = nil
		} else {
			grounds[len(grounds)-1] = endWith(strings.TrimSuffix(grounds[len(grounds)-1], ";"), ";")
		}
		basis := strings.TrimSpace(d.RequestBasis)
		if req.TemplateID == "nghi-quyet" {
			// a resolution's proposal line ("Xét Tờ trình số …") is
			// optional, kept only as the user wrote it
			if core := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(basis, "Xét"), "Theo đề nghị của")), "."); core != "" && said(userText, core) {
				requestBasis = endWith(basis, ".")
			}
			// the collective that resolves: set with the signer (TM. …)
			decisionAuthority = strings.ToUpper(agency)
		} else {
			unit := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(basis, "Theo đề nghị của")), ".")
			if unit == "" || !said(userText, unit) {
				need("Đơn vị đề nghị (Theo đề nghị của …)")
			} else {
				requestBasis = "Theo đề nghị của " + unit + "."
			}
			if profile.HeadTitle != "" && agency != "" {
				decisionAuthority = strings.ToUpper(profile.HeadTitle + " " + agency)
			} else {
				need("Chức vụ người đứng đầu cơ quan (thẩm quyền ban hành quyết định)")
			}
		}
	}

	// the signer, as the user stated them; a biên bản is signed by its
	// chair and secretary (the form's), with no form of signing
	s := d.Signer
	signerName, signerPos := strings.TrimSpace(s.Name), strings.TrimSpace(s.Position)
	signing := strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(s.Authority), "."))
	onBehalf := strings.TrimSpace(s.OnBehalfOf)
	minutes := req.TemplateID == "bien-ban"
	if minutes {
		signerName, signerPos, signing, onBehalf = form["chair_name"], "CHỦ TỌA", "truc_tiep", ""
	}
	if !minutes && (signerName == "" || signerPos == "" || !said(userText, signerName) || !said(userText, signerPos)) {
		need("Người ký: họ tên và chức vụ")
		signerName, signerPos = "", ""
	}
	if signing == "TRUC_TIEP" {
		signing = "truc_tiep"
	}
	if re, ok := authorityWords[signing]; !minutes && (!ok || !re.MatchString(fold(userText))) {
		need("Hình thức ký: ký trực tiếp, hay TM./KT./TL./TUQ./Q. (ghi rõ)")
		signing = ""
	}
	if onBehalf != "" && !said(userText, onBehalf) {
		onBehalf = ""
	}
	if onBehalf == "" {
		switch signing {
		case "TM":
			onBehalf = profile.CollectiveName
		case "KT", "TL", "TUQ", "Q":
			onBehalf = profile.HeadTitle
		}
	}
	if signing != "" && signing != "truc_tiep" && onBehalf == "" {
		if signing == "TM" {
			need("Tên tập thể lãnh đạo ghi sau TM.")
		} else {
			need("Chức vụ người đứng đầu ghi sau " + signing + ".")
		}
	}

	// a resolution's collective is the one signing for it ("TM. Ban Giám
	// đốc" → "BAN GIÁM ĐỐC SỞ NỘI VỤ"), with the agency unless named in it
	if req.TemplateID == "nghi-quyet" && signing == "TM" && onBehalf != "" && agency != "" {
		decisionAuthority = strings.ToUpper(onBehalf)
		if !strings.Contains(expandAgencyAbbr(onBehalf), expandAgencyAbbr(agency)) {
			decisionAuthority += " " + strings.ToUpper(agency)
		}
	}

	base := map[string]interface{}{
		"schema_version": 1, "request_id": req.RequestID, "document_id": req.DocumentID, "template_id": req.TemplateID,
	}
	name := types.DocumentTemplateNames[req.TemplateID]
	if len(missing) > templateMissingMax {
		missing = missing[:templateMissingMax]
	}
	blanks := len(missing) > 0 && (in.KeepBlanks || wantsBlanks(in.Inputs))
	if len(missing) > 0 && !blanks {
		base["status"] = "needs_input"
		base["missing_fields"] = missing
		out.Data = base
		var b strings.Builder
		fmt.Fprintf(&b, "Để soạn %s, mình cần thêm:\n", strings.ToLower(name))
		for _, m := range missing {
			b.WriteString("- " + m + "\n")
		}
		b.WriteString("Bạn trả lời trong khung chat; hoặc trả lời «bỏ qua» để tạo ngay, các phần còn thiếu để trống (" +
			templateBlank + ") cho bạn tự điền.")
		out.Answer = b.String()
		return out
	}
	if blanks {
		// the user fills these in later: what is known stays, the rest is
		// a blank — a work-context guess they did not confirm is not used
		blank := func(v *string) {
			if strings.TrimSpace(*v) == "" {
				*v = templateBlank
			}
		}
		for _, v := range []*string{&agency, &abbr, &locality, &user.UnitAbbr, &subject} {
			blank(v)
		}
		if len(invented) > 0 {
			subject = blankInvented(subject, invented)
			addressee = blankInvented(addressee, invented)
			for i := range paragraphs {
				paragraphs[i].Text = blankInvented(paragraphs[i].Text, invented)
			}
		}
		if len(paragraphs) == 0 && req.TemplateID != "giay-moi" {
			paragraphs = []templateParagraph{{Kind: "body", Text: templateBlank}}
		}
		if req.TemplateID == "cong-van" || req.TemplateID == "to-trinh" {
			blank(&addressee)
		}
		if decisionTemplate(req.TemplateID) {
			if len(grounds) == 0 {
				grounds = []string{"Căn cứ " + templateBlank + ";"}
			}
			if requestBasis == "" && req.TemplateID == "quyet-dinh" {
				requestBasis = "Theo đề nghị của " + templateBlank + "."
			}
			blank(&decisionAuthority)
		}
		blankForm(req.TemplateID, form)
		for k, v := range form {
			form[k] = blankInvented(v, invented)
		}
		blank(&signerName)
		blank(&signerPos)
		if signing == "" {
			signing = "truc_tiep"
		}
		blank(&onBehalf)
	}
	if templateForms[req.TemplateID] != nil {
		var extra []templateParagraph
		if req.TemplateID != "giay-gioi-thieu" && req.TemplateID != "giay-nghi-phep" {
			extra = paragraphs
		}
		paragraphs = formBody(req.TemplateID, form, agencyInSentence(agency, d.AgencyInSentence), subject, extra)
	}

	var signature []string
	switch signing {
	case "truc_tiep":
		signature = []string{signerPos}
	case "Q":
		signature = []string{"Q. " + onBehalf}
	default:
		signature = []string{signing + ". " + onBehalf, signerPos}
	}
	for i := range signature {
		signature[i] = fitRunes(strings.ToUpper(signature[i]), templateTextMax)
	}
	fields := map[string]interface{}{
		"agency":          fitRunes(strings.ToUpper(agency), templateTextMax),
		"locality":        fitRunes(locality, templateTextMax),
		"date":            nd30Date(in.Today),
		"number_symbol":   numberSymbol(req.TemplateID, abbr, user.UnitAbbr),
		"subject":         fitRunes(subject, templateTextMax),
		"paragraphs":      paragraphs,
		"recipients":      templateRecipients(req.TemplateID, recipientLines(req.TemplateID, d.Recipients, form), profile.DefaultRecipients, user.UnitAbbr, paragraphs),
		"signature_lines": signature,
		"signer_name":     fitRunes(signerName, templateTextMax),
	}
	if parent != "" {
		fields["parent_agency"] = fitRunes(strings.ToUpper(parent), templateTextMax)
	}
	if addressee != "" {
		fields["addressee"] = fitRunes(addressee, templateTextMax)
	}
	if decisionTemplate(req.TemplateID) {
		fields["grounds"] = grounds
		if requestBasis != "" {
			fields["request_basis"] = fitRunes(requestBasis, templateTextMax)
		}
		fields["decision_authority"] = fitRunes(decisionAuthority, templateTextMax)
	}
	switch req.TemplateID {
	case "bien-ban":
		fields["secretary_name"] = fitRunes(form["secretary_name"], templateTextMax)
	case "cong-dien":
		sender := form["sender"]
		if sender == "" {
			sender = agency
		}
		fields["issuer_line"] = fitRunes(strings.ToUpper(sender), templateTextMax-6) + " điện:"
		lines := telegramReceivers(form["receivers"])
		if len(lines) == 0 {
			lines = []string{"- " + templateBlank + ";"}
		}
		fields["receiver_lines"] = lines
	}
	base["status"] = "ready"
	base["fields"] = fields
	out.Data, out.Ready = base, true
	if blanks {
		var b strings.Builder
		fmt.Fprintf(&b, "Đã tạo %s «%s» theo Nghị định 30/2020/NĐ-CP; các phần bạn chưa cung cấp được để trống (%s) để bạn tự điền:\n",
			strings.ToLower(name), subject, templateBlank)
		for _, m := range blankedLabels(missing) {
			b.WriteString("- " + m + "\n")
		}
		b.WriteString("Số văn bản để trống chờ cấp số. Kiểm tra và điền đủ trước khi trình ký.")
		out.Answer = b.String()
		return out
	}
	out.Answer = fmt.Sprintf("Đã chuẩn bị %s «%s» theo Nghị định 30/2020/NĐ-CP với thông tin của %s; số văn bản để trống chờ cấp số. Kiểm tra lại nội dung trước khi trình ký.",
		strings.ToLower(name), subject, strings.ToUpper(agency))
	return out
}

// groundSaid: a legal ground the user wrote — its numbers, or its words
// after "Căn cứ".
func groundSaid(userText, g string) bool {
	core := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(g), "Căn cứ"))
	if nums := digitsRe.FindAllString(core, -1); len(nums) > 0 {
		for _, n := range nums {
			if !strings.Contains(userText, n) {
				return false
			}
		}
		return true
	}
	return said(userText, core)
}

func endWith(s, end string) string {
	s = strings.TrimRight(strings.TrimSpace(s), ".;,")
	return s + end
}

// nd30Date writes "ngày … tháng … năm …": days below 10 and months 1–2
// with a leading zero (NĐ30 Phụ lục I).
func nd30Date(t time.Time) string {
	day := strconv.Itoa(t.Day())
	if t.Day() < 10 {
		day = "0" + day
	}
	month := strconv.Itoa(int(t.Month()))
	if t.Month() <= 2 {
		month = "0" + month
	}
	return fmt.Sprintf("ngày %s tháng %s năm %d", day, month, t.Year())
}

// numberSymbol is the unnumbered số ký hiệu: the number is left blank for
// the văn thư to give ("Số:      /KH-SNV").
func numberSymbol(templateID, agencyAbbr, unitAbbr string) string {
	sym := agencyAbbr
	if code := templateTypeCodes[templateID]; code != "" {
		sym = code + "-" + agencyAbbr
	} else if unitAbbr != "" {
		sym = agencyAbbr + "-" + unitAbbr
	}
	return "      /" + sym
}

// templateRecipients are the Nơi nhận lines: the user's (else the
// profile's usual ones), each "- …;", then "- Lưu: VT, <đơn vị>.". "Như
// trên" refers to a "Kính gửi": a decision's is "Như Điều <last article>",
// a type without one has none.
func templateRecipients(templateID string, lines, defaults []string, unitAbbr string, body []templateParagraph) []string {
	lastArticle := ""
	for _, p := range body {
		if m := articleNumberRe.FindStringSubmatch(p.Text); m != nil {
			lastArticle = m[1]
		}
	}
	clean := func(lines []string) []string {
		var kept []string
		for _, l := range lines {
			l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-–•+ "))
			if strings.HasPrefix(fold(l), "như trên") {
				switch {
				case templateID == "cong-van" || templateID == "to-trinh" || templateID == "cong-dien" || templateID == "giay-gioi-thieu" || templateID == "giay-moi":
				case decisionTemplate(templateID) && lastArticle != "":
					l = "Như Điều " + lastArticle
				default:
					continue
				}
			}
			kept = append(kept, l)
		}
		return kept
	}
	lines = clean(lines)
	// minutes are kept on file (Mẫu 1.9), not sent to the usual recipients
	if len(lines) == 0 && templateID != "bien-ban" {
		lines = clean(defaults)
	}
	var out []string
	seen := map[string]bool{}
	for _, l := range lines {
		if l == "" || strings.HasPrefix(fold(l), "lưu") || seen[fold(l)] {
			continue
		}
		seen[fold(l)] = true
		out = append(out, fitRunes("- "+endWith(l, ";"), templateTextMax))
		if len(out) == templateRecipientsMax-1 {
			break
		}
	}
	luu := "- Lưu: VT"
	switch {
	case templateID == "bien-ban":
		// Mẫu 1.9: the minutes go to the meeting's file
		luu += ", Hồ sơ"
	case templateID == "giay-gioi-thieu":
		// Mẫu 1.8 keeps it at the văn thư
	case unitAbbr != "":
		luu += ", " + unitAbbr
	}
	return append(out, luu+".")
}

// recipientLines are the Nơi nhận lines the user named, else the ones the
// form itself gives: "Như trên" for whom a giấy giới thiệu or công điện is
// addressed to, the person on leave for a giấy nghỉ phép.
func recipientLines(templateID string, lines []string, form map[string]string) []string {
	if templateID == "cong-dien" {
		// the receivers are "Như trên"; the others the user named follow
		receivers := map[string]bool{}
		for _, r := range telegramReceivers(form["receivers"]) {
			receivers[recipientKey(r)] = true
		}
		out := []string{"Như trên"}
		for _, l := range lines {
			if !receivers[recipientKey(l)] {
				out = append(out, l)
			}
		}
		return out
	}
	for _, l := range lines {
		if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-–•+ ")); l != "" && !strings.HasPrefix(fold(l), "lưu") {
			return lines
		}
	}
	switch templateID {
	case "giay-gioi-thieu", "giay-moi":
		return []string{"Như trên"}
	case "giay-nghi-phep":
		if p := form["person"]; p != "" {
			return []string{"Ông (bà) " + p}
		}
	}
	return lines
}

// recipientKey compares Nơi nhận lines: folded, without the dash and the
// end mark.
func recipientKey(l string) string {
	return strings.Trim(fold(strings.TrimLeft(strings.TrimSpace(l), "-–•+ ")), " .;,")
}

// fitRunes cuts s to at most n runes (the add-in rejects longer fields).
func fitRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// workUnitAgency reads the issuing agency from the user's work unit: a name
// ending on its level ("Công an tỉnh", "UBND huyện") gets the province.
func workUnitAgency(workUnit, province string) string {
	u := strings.Join(strings.Fields(workUnit), " ")
	if u == "" {
		return ""
	}
	if province != "" {
		last := strings.ToLower(u[strings.LastIndex(u, " ")+1:])
		if last == "tỉnh" || last == "phố" || last == "thành" {
			u += " " + province
		}
	}
	return u
}

var (
	articleRe = regexp.MustCompile(`^Điều\s+\d+\s*[.:]`)
	// subHeadingRe is a numbered sub-heading line: "2. Quy mô và hình thức",
	// without an ending sentence mark.
	subHeadingRe    = regexp.MustCompile(`^(\d+(\.\d+)*\.|[a-zđ]\))\s+[^.;:!?]+$`)
	articleNumberRe = regexp.MustCompile(`^Điều\s+(\d+)`)
	// frameLineRe matches lines of the document's frame the add-in writes
	// itself (quốc hiệu, tiêu ngữ, a rule, the decision's "QUYẾT ĐỊNH:").
	frameLineRe = regexp.MustCompile(`(?i)^(cộng hòa xã hội chủ nghĩa việt nam|cộng hoà xã hội chủ nghĩa việt nam|độc lập\s*-\s*tự do\s*-\s*hạnh phúc|[-_=–—.\s]+|quyết định\s*:?|quyết nghị\s*:?|kính gửi\s*:.*|nơi nhận\s*:.*)$`)
)

// templateBody is the model's body cleaned for the add-in: one paragraph
// per line, the frame lines the add-in writes itself dropped (the model
// copies the header, the title, the authority, the grounds into it), a
// paragraph "Điều N. …" an article, repeats dropped; a decision's
// grounds and proposal lines go (they have their own fields).
func templateBody(d *templateDraft, templateID, agency, parent string) []templateParagraph {
	frame := map[string]bool{}
	for _, v := range []string{agency, parent, d.Subject, d.RequestBasis, "quyết định", "quyết nghị"} {
		if f := strings.Trim(fold(v), " .;:"); f != "" {
			frame[f] = true
		}
	}
	for _, g := range d.Grounds {
		frame[strings.Trim(fold(g), " .;:")] = true
	}
	var out []templateParagraph
	for _, p := range d.Paragraphs {
		for _, line := range strings.Split(p.Text, "\n") {
			text := strings.TrimSpace(line)
			f := strings.Trim(fold(text), " .;:")
			if text == "" || frameLineRe.MatchString(text) || frame[f] || frame[strings.Trim(subjectPrefixRe.ReplaceAllString(f, ""), " .;:")] ||
				// a decision's grounds and proposal have their own fields
				(decisionTemplate(templateID) && (strings.HasPrefix(f, "căn cứ ") || strings.HasPrefix(f, "theo đề nghị") || strings.HasPrefix(f, "xét "))) ||
				(agency != "" && strings.HasSuffix(f, fold(agency)) && strings.ToUpper(text) == text) {
				continue
			}
			kind := p.Kind
			switch {
			case articleRe.MatchString(text):
				kind = "article"
			case subHeadingRe.MatchString(text) && utf8.RuneCountInString(text) <= 100:
				// "1. Đối tượng tham gia": a heading of a plan or report
				kind = "heading"
			case kind != "heading":
				// an "article" the model marked that is no "Điều N." line
				kind = "body"
			}
			if n := len(out); n > 0 && fold(out[n-1].Text) == fold(text) {
				continue
			}
			out = append(out, templateParagraph{Kind: kind, Text: fitRunes(text, templateParagraphMax)})
			if len(out) == templateParagraphsMax {
				return out
			}
		}
	}
	return out
}

// subjectCase starts the trích yếu as NĐ30 prints it: after "V/v" in lower
// case ("V/v triển khai …"), else as a sentence ("Cử cán bộ …"). A word in
// capitals (UBND) is kept.
func subjectCase(templateID, subject string) string {
	r, n := utf8.DecodeRuneInString(subject)
	if templateID != "cong-van" {
		return string(unicode.ToUpper(r)) + subject[n:]
	}
	if next, _ := utf8.DecodeRuneInString(subject[n:]); unicode.IsUpper(next) {
		return subject
	}
	return string(unicode.ToLower(r)) + subject[n:]
}
