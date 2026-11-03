package agent

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Qwen with thinking off sometimes ends a round with text that is not an
// answer: it repeats a short message back ("VP" → "VP"), sends only
// punctuation ("..."), or writes its plan in English ("The user wants to
// rewrite mục 2…") in a Vietnamese turn, either as the whole answer or as a
// lead-in paragraph before the Vietnamese one ("Now I have all the data.
// Let me compile the comparison:"). Nothing checked the final answer, so
// those reached the user.
//
// The check runs on the answer stream, ahead of the other answer filters:
//   - a lead-in English paragraph followed by a non-English one is dropped;
//   - an answer that repeats a question without Vietnamese words (a code
//     such as "VP"), has no letters or digits, or is
//     English throughout is held back whole and the round is marked
//     degenerate; the engine reruns it once with thinking on (replaying the
//     "VP" request: echo 10/10 with thinking off, 0/20 with it on), then
//     answers with a fixed Vietnamese sentence (degenerateFallback).
//
// English is judged only when the turn is Vietnamese: the UI language is
// Vietnamese, the question is not itself English and does not ask for
// English (vietnameseTurn).

// degenerateKind is why an answer was held back.
type degenerateKind int

const (
	answerOK degenerateKind = iota
	answerEcho
	answerBlank
	answerEnglish
)

func (k degenerateKind) String() string {
	switch k {
	case answerEcho:
		return "echo"
	case answerBlank:
		return "blank"
	case answerEnglish:
		return "english"
	default:
		return "ok"
	}
}

// leadJudgeRunes is how much of a paragraph is judged for its language.
// The stream holds an unfinished first paragraph until it has this many
// runes, so the stream and the saved answer judge the same text.
const leadJudgeRunes = 200

// englishWords are English function words and the words the model's
// leaked plans are made of; a Vietnamese paragraph has almost none.
var englishWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`the a an is are was were be been being to of and or for with that this these those
		it its i i'm i'll i've you your we our they their them he she his her user user's users wants want wanted asking
		asks asked ask need needs should will would let me my can could do does did not no so then first now about which
		what how there here has have had from into on in at by as but if however also already provide answer information
		document documents tool tools search find found check based looking`) {
		m[w] = true
	}
	return m
}()

// vietnameseLetters are the letters that only Vietnamese writing uses.
const vietnameseLetters = "àáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵđ"

// letterWords splits text into lower-case words of letters (an apostrophe
// inside a word stays: "user's").
func letterWords(text string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, strings.Trim(cur.String(), "'"))
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(norm.NFC.String(text)) {
		if unicode.IsLetter(r) || (r == '\'' && cur.Len() > 0) || r == '’' && cur.Len() > 0 {
			if r == '’' {
				r = '\''
			}
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return words
}

func anyVietnameseWord(text string) bool {
	for _, w := range letterWords(text) {
		if hasVietnameseLetter(w) {
			return true
		}
	}
	return false
}

func hasVietnameseLetter(word string) bool {
	return strings.ContainsAny(word, vietnameseLetters)
}

// withoutQuotes is text with its quoted and code spans removed: an English
// plan quotes the Vietnamese it is about ("mục 2", `Nghị định 336`), and
// the quotes say nothing of the language it is written in.
func withoutQuotes(text string) string {
	var b strings.Builder
	var closer rune
	fenced := false
	lines := strings.SplitAfter(text, "\n")
	text = ""
	for _, line := range lines {
		// a fenced code block is code, not prose
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			text += line
		}
	}
	for _, r := range text {
		if closer != 0 {
			if r == closer || r == '\n' {
				closer = 0
			}
			continue
		}
		switch r {
		case '`':
			closer = '`'
		case '"':
			closer = '"'
		case '“':
			closer = '”'
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// englishText reports whether text, outside its quotes, reads as English: at least 6 words, at
// least 30 % of them English function words and at most 35 % with a
// Vietnamese letter.
func englishText(text string) bool {
	words := letterWords(withoutQuotes(text))
	if len(words) < 6 {
		return false
	}
	en, vi := 0, 0
	for _, w := range words {
		if englishWords[w] {
			en++
		}
		if hasVietnameseLetter(w) {
			vi++
		}
	}
	return en*10 >= len(words)*3 && vi*100 <= len(words)*35
}

// englishParagraph judges a paragraph on its first leadJudgeRunes runes.
func englishParagraph(p string) bool {
	return englishText(firstRunes(p, leadJudgeRunes))
}

func firstRunes(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// englishRequests are the phrases of a question asking for English text.
var englishRequests = []string{"tiếng anh", "english", "translate", "dịch sang", "dịch ra", "bản dịch", "anh ngữ",
	"song ngữ", "bilingual", "nước ngoài"}

// vietnameseTurn reports whether an English answer is wrong for this turn:
// the UI language is Vietnamese and the question is neither English nor a
// request for English.
func vietnameseTurn(ctx context.Context, query string) bool {
	if !strings.HasPrefix(strings.ToLower(types.LanguageFromContextOrDefault(ctx)), "vi") {
		return false
	}
	l := strings.ToLower(norm.NFC.String(query))
	for _, p := range englishRequests {
		if strings.Contains(l, p) {
			return false
		}
	}
	words := letterWords(query)
	en, vi := 0, 0
	for _, w := range words {
		if englishWords[w] {
			en++
		}
		if hasVietnameseLetter(w) {
			vi++
		}
	}
	return !(vi == 0 && en >= 2)
}

// foldForEcho is text lower-cased with everything but letters and digits
// removed, words joined by one space.
func foldForEcho(text string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(norm.NFC.String(text)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
			continue
		}
		space = true
	}
	return b.String()
}

// paraClass is what a paragraph says about the language of the answer.
type paraClass int

const (
	// paraNeutral: a rule, a heading of a few words without Vietnamese
	// letters, a list of codes; it decides nothing
	paraNeutral paraClass = iota
	paraEnglish
	// paraOther: Vietnamese or any text that is not English
	paraOther
)

// structuredParagraph is a paragraph the model wrote as content, never as
// a plan: a quote, a table, a list, a heading or code. Such text in English
// (a quoted English clause, the English half of a bilingual notice, a code
// answer) is shown; the leaked plans are plain prose.
func structuredParagraph(p string) bool {
	t := strings.TrimLeft(p, " \t\n")
	if t == "" {
		return false
	}
	switch t[0] {
	case '>', '|', '#', '-', '+':
		return !separatorParagraph(t)
	case '*':
		// "* item" is a list; "**Summary**" a heading
		return len(t) > 1 && (t[1] == ' ' || t[1] == '*') && !separatorParagraph(t)
	}
	if strings.HasPrefix(t, "```") {
		return true
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i < len(t) && (t[i] == '.' || t[i] == ')')
}

// separatorParagraph is a rule ("---", "***").
func separatorParagraph(p string) bool {
	t := strings.TrimSpace(p)
	return t != "" && strings.Trim(t, "-*_ ") == ""
}

// classifyParagraph judges a paragraph on its first leadJudgeRunes runes,
// so a stream holding that much of an unfinished paragraph judges what the
// saved answer will.
func classifyParagraph(p string) paraClass {
	if structuredParagraph(p) {
		return paraOther
	}
	head := firstRunes(p, leadJudgeRunes)
	if englishText(head) {
		return paraEnglish
	}
	outside := withoutQuotes(head)
	if len(letterWords(outside)) >= 6 || anyVietnameseWord(outside) {
		return paraOther
	}
	return paraNeutral
}

// scanLead reads text paragraph by paragraph (separated by a blank line)
// to its first paragraph that is not English or neutral. decided reports
// that it got there, or that text is final; cut is where the answer then
// starts: after the English lead-in when there was one, else 0.
// allEnglish reports a final text with English and neutral paragraphs only.
// Without a Vietnamese turn nothing is English and the first paragraph
// that is not neutral decides at cut 0.
//
// A text that is not final ends on a paragraph that may still grow; it is
// judged once it has leadJudgeRunes runes.
func scanLead(text string, final, vietnamese bool) (cut int, decided, allEnglish bool) {
	pos := 0
	sawEnglish := false
	for pos < len(text) {
		end := strings.Index(text[pos:], "\n\n")
		para := text[pos:]
		next := len(text)
		if end >= 0 {
			para = text[pos : pos+end]
			next = pos + end + 2
		} else if !final && utf8.RuneCountInString(para) < leadJudgeRunes {
			return 0, false, false
		}
		class := classifyParagraph(para)
		if class == paraEnglish && !vietnamese {
			class = paraOther
		}
		switch class {
		case paraEnglish:
			sawEnglish = true
		case paraOther:
			if sawEnglish {
				return pos, true, false
			}
			return 0, true, false
		}
		if end < 0 && !final {
			// a long unfinished English or neutral paragraph: wait for more
			return 0, false, false
		}
		pos = next
	}
	if !final {
		return 0, false, false
	}
	return 0, true, sawEnglish
}

// stripEnglishLeadIn drops the leading English paragraphs of a complete
// text (and the neutral ones among them) when a paragraph in another
// language follows. allEnglish reports text that is English throughout
// (nothing is dropped then).
func stripEnglishLeadIn(text string, vietnamese bool) (rest string, dropped bool, allEnglish bool) {
	cut, _, all := scanLead(text, true, vietnamese)
	return text[cut:], cut > 0, all
}

// judgeAnswer is why a complete answer must not be shown, answerOK when it
// can be. text is the answer after stripEnglishLeadIn.
func judgeAnswer(query, text string, vietnamese bool) degenerateKind {
	folded := foldForEcho(text)
	if folded == "" {
		return answerBlank
	}
	// a code repeated back ("VP" → "VP") is a model that did not know what
	// to do; a greeting mirrored ("Xin chào" → "Xin chào") is a reply, so
	// only a question without a Vietnamese word counts
	if q := foldForEcho(query); q != "" && folded == q && !anyVietnameseWord(query) {
		return answerEcho
	}
	if _, _, all := stripEnglishLeadIn(text, vietnamese); all {
		return answerEnglish
	}
	return answerOK
}

// cleanFinalAnswer is the saved form of a streamed or non-streamed answer:
// its English lead-in dropped, and why it must be held back.
func cleanFinalAnswer(ctx context.Context, query, text string) (string, degenerateKind) {
	vietnamese := vietnameseTurn(ctx, query)
	rest, _, _ := stripEnglishLeadIn(text, vietnamese)
	// the stream drops the line breaks an answer opens with (leadGuard)
	rest = strings.TrimLeft(rest, " \t\r\n")
	return rest, judgeAnswer(query, rest, vietnamese)
}

// leadGuard is the head of the answer stream (see the file comment). It
// holds text until scanLead decides: the English lead-in is dropped and
// the rest released; a short answer is held to its end and judged whole.
type leadGuard struct {
	ctx        context.Context
	query      string
	vietnamese bool
	emit       func(string)
	held       string
	passing    bool
	// preamble: the round called tools and the text went out unchecked
	preamble bool
	emitted  bool
	// kind is set at Flush when the whole answer was held back
	kind    degenerateKind
	flushed bool
}

func newLeadGuard(ctx context.Context, query string, emit func(string)) *leadGuard {
	return &leadGuard{ctx: ctx, query: query, vietnamese: vietnameseTurn(ctx, query), emit: emit}
}

// Write adds streamed answer text.
func (g *leadGuard) Write(text string) {
	if text == "" {
		return
	}
	if g.passing {
		g.emit(text)
		return
	}
	g.held += text
	if cut, decided, _ := scanLead(g.held, false, g.vietnamese); decided {
		g.releaseFrom(cut)
	}
}

// releaseFrom sends the held text from cut on and passes the rest through.
func (g *leadGuard) releaseFrom(cut int) {
	if cut > 0 {
		logger.Infof(g.ctx, "[Agent][AnswerCheck] dropped an English lead-in: %q", firstRunes(g.held[:cut], 120))
	}
	g.passing = true
	rest := g.held[cut:]
	if !g.emitted {
		// the stray line breaks Qwen sends first were dropped downstream
		// before the answer started; held with the text they no longer are
		rest = strings.TrimLeft(rest, " \t\r\n")
	}
	if rest != "" {
		g.emitted = true
		g.emit(rest)
	}
	g.held = ""
}

// Release sends what is held and passes the rest through: the round calls
// tools, so its text is a preamble, not the answer.
func (g *leadGuard) Release() {
	if !g.passing && !g.flushed {
		g.preamble = true
		g.releaseFrom(0)
	}
}

// Flush ends the stream: held text is judged whole and sent, or kept back
// with kind set.
func (g *leadGuard) Flush() {
	if g.passing || g.flushed {
		return
	}
	g.flushed = true
	if g.held == "" {
		return
	}
	cut, _, _ := scanLead(g.held, true, g.vietnamese)
	if k := judgeAnswer(g.query, g.held[cut:], g.vietnamese); k != answerOK {
		g.kind = k
		logger.Warnf(g.ctx, "[Agent][AnswerCheck] held back a %s answer: %q", k, firstRunes(g.held, 120))
		g.held = ""
		return
	}
	g.releaseFrom(cut)
}

// Preamble reports that the text went out as a tool-call preamble,
// unchecked: the saved content must not be cleaned then either.
func (g *leadGuard) Preamble() bool {
	return g.preamble
}

// Kind is why the streamed answer was held back, answerOK when it was not.
func (g *leadGuard) Kind() degenerateKind {
	return g.kind
}

type answerCheckQueryKey struct{}

// withAnswerCheckQuery stores the turn's question for the round's lead
// check.
func withAnswerCheckQuery(ctx context.Context, query string) context.Context {
	return context.WithValue(ctx, answerCheckQueryKey{}, query)
}

func answerCheckQuery(ctx context.Context) string {
	q, _ := ctx.Value(answerCheckQueryKey{}).(string)
	return q
}

// degenerateFallback is the answer when the rerun also failed.
func degenerateFallback(query string, kind degenerateKind) string {
	q := strings.TrimSpace(query)
	if kind == answerEcho && q != "" && utf8.RuneCountInString(q) <= 40 && !strings.ContainsAny(q, "*_[]<>`\\#|") {
		return "Tôi chưa hiểu rõ ý bạn qua “" + q + "”. Bạn nói rõ hơn giúp tôi bạn cần tra cứu hoặc hỗ trợ điều gì nhé."
	}
	return "Xin lỗi, tôi chưa soạn được câu trả lời cho câu này. Bạn diễn đạt lại hoặc nói rõ hơn giúp tôi nhé."
}

// heldBackKind parses types.ChatResponse.AnswerHeldBack.
func heldBackKind(name string) degenerateKind {
	switch name {
	case "echo":
		return answerEcho
	case "blank":
		return answerBlank
	case "english":
		return answerEnglish
	default:
		return answerOK
	}
}

// emitFixedAnswer sends text as the turn's whole answer: the chunk and the
// Done marker on one event id.
func (e *AgentEngine) emitFixedAnswer(ctx context.Context, sessionID, text string) {
	answerID := generateEventID("answer")
	_ = e.eventBus.Emit(ctx, event.Event{
		ID: answerID, Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Content: text},
	})
	_ = e.eventBus.Emit(ctx, event.Event{
		ID: answerID, Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Done: true},
	})
}
