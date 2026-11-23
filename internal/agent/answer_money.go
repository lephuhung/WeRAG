package agent

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/logger"
)

// The model sometimes adds its own reading of an amount in brackets and gets
// the scale wrong: a document's "500.000.000 VNĐ" came back as
// "500.000.000 VNĐ (05 tỷ đồng)" (Word add-in, 23/11/2026). Prompt rules do
// not hold Qwen to this, so the answer is checked in code: when an amount is
// followed by a bracket that restates it ("(500 triệu đồng)",
// "(5.000.000.000 VNĐ)") and the two values differ, the bracket is rewritten
// to the value of the amount before it (the figure the model copied from the
// source), or dropped when no short reading exists. Always on: it needs no
// catalogue and changes nothing when the reading is right.

const moneyScaleAlt = `nghìn\s+tỷ|ngàn\s+tỷ|tỷ|triệu|nghìn|ngàn`
const moneyCurrencyAlt = `đồng|vnđ|vnd|đ`

var (
	// amount [scale] [currency] ( [khoảng] amount [scale] [currency] )
	moneyReadingRe = regexp.MustCompile(`(?i)(\d[\d.,]*\d|\d)(\s*(?:` + moneyScaleAlt + `))?(\s*(?:` +
		moneyCurrencyAlt + `))?(\s*)\(\s*((?:khoảng|tương\s+đương|tức|~)\s*)?(\d[\d.,]*\d|\d)(\s*(?:` +
		moneyScaleAlt + `))?(\s*(?:` + moneyCurrencyAlt + `))?\s*\)`)
	// what may still grow into such a match at the end of a stream: an
	// amount, up to three short words, an open bracket without its end
	moneyTailRe      = regexp.MustCompile(`\d[\d.,]*(?:\s*\p{L}{1,6}){0,3}\s*(?:\([^()\n]{0,60})?$`)
	thousandsDotRe   = regexp.MustCompile(`^\d{1,3}(?:\.\d{3})+$`)
	thousandsCommaRe = regexp.MustCompile(`^\d{1,3}(?:,\d{3})+$`)
)

var moneyScales = []struct {
	word  string
	value int64
}{
	{"nghìn tỷ", 1_000_000_000_000}, {"tỷ", 1_000_000_000}, {"triệu", 1_000_000}, {"nghìn", 1_000},
}

func moneyScaleValue(word string) int64 {
	w := strings.Join(strings.Fields(strings.ToLower(word)), " ")
	w = strings.ReplaceAll(w, "ngàn", "nghìn")
	for _, s := range moneyScales {
		if s.word == w {
			return s.value
		}
	}
	return 1
}

// parseMoney is the value in đồng of a figure and its scale word, ok false
// when it is not a whole number of đồng or too large. Without a scale,
// "500.000.000" and "500,000,000" group thousands; with one, a lone
// separator is the decimal mark ("1,5 tỷ", "1.5 tỷ").
func parseMoney(figure, scale string) (int64, bool) {
	mult := int64(1)
	if strings.TrimSpace(scale) != "" {
		mult = moneyScaleValue(scale)
	}
	intPart, frac := figure, ""
	switch {
	case thousandsDotRe.MatchString(figure):
		intPart = strings.ReplaceAll(figure, ".", "")
	case thousandsCommaRe.MatchString(figure) && (mult == 1 || strings.Count(figure, ",") > 1):
		intPart = strings.ReplaceAll(figure, ",", "")
	case mult > 1 && strings.Count(figure, ",")+strings.Count(figure, ".") == 1:
		i := strings.IndexAny(figure, ".,")
		intPart, frac = figure[:i], figure[i+1:]
	case strings.ContainsAny(figure, ".,"):
		return 0, false
	}
	if len(intPart) > 15 || len(frac) > 9 {
		return 0, false
	}
	n, err := strconv.ParseInt(intPart+frac, 10, 64)
	if err != nil {
		return 0, false
	}
	div := int64(1)
	for range frac {
		div *= 10
	}
	if n > (1<<62)/mult || (n*mult)%div != 0 {
		return 0, false
	}
	return n * mult / div, true
}

// groupThousands writes n with dots between thousands (Vietnamese style).
func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// scaledReading is v in the largest scale word that needs at most three
// decimals ("500 triệu", "1,5 tỷ"), "" when none does.
func scaledReading(v int64) string {
	for _, s := range moneyScales {
		step := s.value / 1000
		if s.value == 1_000 {
			step = s.value // "1.234,567 nghìn" reads worse than nothing
		}
		if v < s.value || v%step != 0 {
			continue
		}
		whole, rest := v/s.value, (v%s.value)/(s.value/1000)
		out := groupThousands(whole)
		if rest > 0 {
			out += "," + strings.TrimRight(strconv.FormatInt(rest+1000, 10)[1:], "0")
		}
		return out + " " + s.word
	}
	return ""
}

// fixMoneyReadings rewrites every bracketed restatement of an amount whose
// value differs from the amount before it.
func fixMoneyReadings(ctx context.Context, text string) string {
	if !strings.Contains(text, "(") {
		return text
	}
	matches := moneyReadingRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	group := func(m []int, i int) string {
		if m[2*i] < 0 {
			return ""
		}
		return text[m[2*i]:m[2*i+1]]
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		leftScale, rightScale := group(m, 2), group(m, 7)
		// a reading names a scale on one side at least, a currency on one
		// side at least, and the amount stands alone (not the year of
		// "15/11/2018", not the tail of "38A-29600")
		if strings.TrimSpace(leftScale) == "" && strings.TrimSpace(rightScale) == "" {
			continue
		}
		if strings.TrimSpace(group(m, 3)) == "" && strings.TrimSpace(group(m, 8)) == "" {
			continue
		}
		if r, _ := utf8.DecodeLastRuneInString(text[:m[2]]); m[2] > 0 &&
			(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("/-.,", r)) {
			continue
		}
		left, ok1 := parseMoney(group(m, 1), leftScale)
		right, ok2 := parseMoney(group(m, 6), rightScale)
		if !ok1 || !ok2 || left == right || left < 1000 {
			continue
		}
		currency := strings.TrimSpace(group(m, 8))
		var reading string
		if strings.TrimSpace(rightScale) != "" {
			reading = scaledReading(left)
		} else {
			reading = groupThousands(left)
		}
		// from the end of the amount (and its scale and currency) to the end
		// of the bracket
		start := m[8]
		replacement := ""
		if reading != "" {
			if currency != "" {
				reading += " " + currency
			}
			replacement = group(m, 4) + "(" + reading + ")"
		}
		logger.Warnf(ctx, "[Agent][AnswerMoney] fixed reading %q → %q", text[m[0]:m[1]], replacement)
		common.PipelineWarn(ctx, "Agent", "answer_money_reading_fixed", map[string]interface{}{
			"written": text[m[0]:m[1]], "replacement": replacement,
		})
		b.WriteString(text[last:start])
		b.WriteString(replacement)
		last = m[1]
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

// moneyGuard applies fixMoneyReadings to a streamed answer: it holds back
// only a trailing amount that may still be followed by its bracket, so the
// streamed text equals fixMoneyReadings of the whole answer.
type moneyGuard struct {
	ctx     context.Context
	emit    func(string)
	pending string
}

func newMoneyGuard(ctx context.Context, emit func(string)) *moneyGuard {
	return &moneyGuard{ctx: ctx, emit: emit}
}

// Write adds streamed text and emits what can no longer change.
func (g *moneyGuard) Write(text string) {
	if text == "" {
		return
	}
	g.pending += text
	cut := moneySafeCut(g.pending)
	if cut == 0 {
		return
	}
	region := g.pending[:cut]
	g.pending = g.pending[cut:]
	if out := fixMoneyReadings(g.ctx, region); out != "" {
		g.emit(out)
	}
}

// Flush emits what is still held back (end of the stream).
func (g *moneyGuard) Flush() {
	if g.pending == "" {
		return
	}
	region := g.pending
	g.pending = ""
	if out := fixMoneyReadings(g.ctx, region); out != "" {
		g.emit(out)
	}
}

// moneySafeCut is the length of text's prefix that cannot be part of an
// amount-and-reading still being written: before an incomplete rune and
// before the earliest trailing amount moneyTailRe may grow from. The start
// is moved back over digits and separators so an amount is never split.
func moneySafeCut(text string) int {
	hold := len(text)
	if i := lastRuneStart(text); i < len(text) && !utf8.FullRuneInString(text[i:]) {
		hold = i
	}
	if loc := moneyTailRe.FindStringIndex(text[:hold]); loc != nil {
		hold = loc[0]
		for hold > 0 && strings.IndexByte("0123456789.,", text[hold-1]) >= 0 {
			hold--
		}
	}
	return hold
}
