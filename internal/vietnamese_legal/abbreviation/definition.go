package abbreviation

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"golang.org/x/text/unicode/norm"
)

type DefinitionParseResult struct {
	Definitions []types.AbbreviationDefinition
	// Typos are "X → Y" answers: the word X was a typo of Y (Y equal to X
	// keeps the word as typed). They never become dictionary suggestions.
	Typos                []types.AbbreviationTypo
	IsDefinitionReply    bool
	NeedsExplicitMapping bool
}

var definitionClause = regexp.MustCompile(`^([\pL\pN][\pL\pN_-]*)\s*(?:=|là)\s*(.+)$`)

var typoClause = regexp.MustCompile(`^([\pL\pN][\pL\pN_-]*)\s*(?:→|->)\s*(.+)$`)

// keepNameRe: what a "PV01 là …" answer says when the word is a name to
// keep as typed, not an abbreviation with a meaning.
var keepNameRe = regexp.MustCompile(`^(?:một\s+)?(?:tên riêng|tên đơn vị|tên phòng|tên cơ quan|tên bộ phận|mã đơn vị|mã phòng|ký hiệu đơn vị)(?:[^\pL]|$)|giữ nguyên`)

// keepReplyRe: a whole reply to the card that keeps the asked word as
// typed ("giữ nguyên", "là tên riêng, giữ nguyên", "Đây là tên đơn vị nhé",
// "không phải viết tắt"); anchored, so a question that merely contains such
// words ("quy định về giữ nguyên hiện trạng…") is not one.
var keepReplyRe = regexp.MustCompile(`^(?:(?:đây|nó|đó|từ này|cái này)\s+)?(?:là\s+)?(?:một\s+)?` +
	`(?:tên riêng|tên đơn vị|tên phòng|tên cơ quan|mã đơn vị|ký hiệu đơn vị|giữ nguyên|không phải (?:là )?(?:từ )?viết tắt|không viết tắt)` +
	`(?:\s*[,.;]?\s*(?:là\s+)?(?:tên riêng|tên đơn vị|giữ nguyên|giữ nguyên nhé))*` +
	`(?:\s+(?:nhé|nha|ạ|nhá))?\s*[.!]*$`)

// keepReplyMaxRunes bounds a keep reply: a longer message is a new question.
const keepReplyMaxRunes = 60

func ParseUserDefinitions(text, messageID string, candidates []string, soleReply bool) (DefinitionParseResult, error) {
	var result DefinitionParseResult
	if strings.TrimSpace(text) == "" || len(candidates) == 0 {
		return result, nil
	}
	if strings.Contains(text, "```") {
		result.NeedsExplicitMapping = true
		return result, nil
	}
	allowed := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		allowed[termKey(candidate)] = candidate
	}
	// a short reply to the card about its one word that keeps it as typed
	// ("giữ nguyên", "là tên riêng", "Đây là tên đơn vị"); a clause naming
	// the word itself ("PV01 là tên phòng") is read below
	if soleReply && len(allowed) == 1 && utf8.RuneCountInString(strings.TrimSpace(text)) <= keepReplyMaxRunes &&
		keepReplyRe.MatchString(strings.TrimSpace(strings.ToLower(norm.NFC.String(text)))) && !namesAllowedWord(strings.TrimSpace(text), allowed) {
		for _, short := range allowed {
			result.Typos = append(result.Typos, types.AbbreviationTypo{Wrong: short, Correct: short})
		}
		result.IsDefinitionReply = true
		return result, nil
	}
	seen := make(map[string]bool, len(candidates))
	for start := 0; start < len(text); {
		end := start
		for end < len(text) && text[end] != ';' && text[end] != '\n' {
			end++
		}
		segment := text[start:end]
		trimmed := strings.TrimSpace(segment)
		prefix := len(segment) - len(strings.TrimLeftFunc(segment, unicode.IsSpace))
		if m := typoClause.FindStringSubmatch(trimmed); m != nil {
			key := termKey(m[1])
			canonical, ok := allowed[key]
			correct := strings.TrimSpace(m[2])
			if !ok || correct == "" || strings.ContainsAny(correct, "=;\r\n") ||
				utf8.RuneCountInString(correct) > types.MaxAbbreviationFullFormRunes {
				return DefinitionParseResult{}, types.ErrAbbreviationBadSelection
			}
			if seen[key] {
				return DefinitionParseResult{}, types.ErrAbbreviationConflict
			}
			seen[key] = true
			result.Typos = append(result.Typos, types.AbbreviationTypo{Wrong: canonical, Correct: correct})
			if end == len(text) {
				break
			}
			start = end + 1
			continue
		}
		match := definitionClause.FindStringSubmatchIndex(trimmed)
		if len(match) != 0 {
			short := trimmed[match[2]:match[3]]
			fullRaw := trimmed[match[4]:match[5]]
			full := strings.TrimSpace(fullRaw)
			if validDefinitionPhrase(full) {
				key := termKey(short)
				canonical, ok := allowed[key]
				if !ok || key == "" || !validDefinitionLength(short, full) || strings.EqualFold(short, full) || messageID == "" {
					return DefinitionParseResult{}, types.ErrAbbreviationBadSelection
				}
				if seen[key] {
					return DefinitionParseResult{}, types.ErrAbbreviationConflict
				}
				seen[key] = true
				spanStart := start + prefix + match[4] + len(fullRaw) - len(strings.TrimLeftFunc(fullRaw, unicode.IsSpace))
				result.Definitions = append(result.Definitions, types.AbbreviationDefinition{
					ShortForm: canonical, FullForm: full, SourceMessageID: messageID,
					Start: spanStart, End: spanStart + len(full),
				})
			}
		}
		if end == len(text) {
			break
		}
		start = end + 1
	}
	// "PV01 là tên đơn vị" keeps the word as typed: a name, not a meaning
	defs := result.Definitions[:0]
	for _, d := range result.Definitions {
		if keepNameRe.MatchString(strings.ToLower(norm.NFC.String(d.FullForm))) {
			result.Typos = append(result.Typos, types.AbbreviationTypo{Wrong: d.ShortForm, Correct: d.ShortForm})
			continue
		}
		defs = append(defs, d)
	}
	result.Definitions = defs
	if len(result.Definitions) == 0 && len(result.Typos) == 0 && soleReply && len(allowed) == 1 {
		phrase := strings.TrimSpace(text)
		if validBareDefinitionPhrase(phrase) && utf8.RuneCountInString(phrase) > types.MaxAbbreviationFullFormRunes {
			return DefinitionParseResult{}, types.ErrAbbreviationBadSelection
		}
		for _, short := range allowed {
			if validBareDefinitionPhrase(phrase) && validDefinitionLength(short, phrase) &&
				!strings.EqualFold(short, phrase) && abbreviationInitials(phrase) == normalizedInitials(short) && messageID != "" {
				start := strings.Index(text, phrase)
				result.Definitions = append(result.Definitions, types.AbbreviationDefinition{
					ShortForm: short, FullForm: phrase, SourceMessageID: messageID,
					Start: start, End: start + len(phrase),
				})
			}
		}
	}
	for _, d := range result.Definitions {
		if d.Start < 0 || d.End > len(text) || d.Start >= d.End || text[d.Start:d.End] != d.FullForm || d.SourceMessageID != messageID {
			return DefinitionParseResult{}, types.ErrAbbreviationBadSelection
		}
	}
	result.IsDefinitionReply = len(result.Definitions) != 0 || len(result.Typos) != 0
	result.NeedsExplicitMapping = !result.IsDefinitionReply
	return result, nil
}

// namesAllowedWord: the reply is a clause about one of the asked words
// ("PV01 = …", "PV01 là …", "PV01 → …").
func namesAllowedWord(text string, allowed map[string]string) bool {
	for _, re := range []*regexp.Regexp{definitionClause, typoClause} {
		if m := re.FindStringSubmatch(text); m != nil && allowed[termKey(m[1])] != "" {
			return true
		}
	}
	return false
}

func validDefinitionLength(short, full string) bool {
	return utf8.RuneCountInString(short) <= types.MaxAbbreviationShortFormRunes &&
		utf8.RuneCountInString(full) <= types.MaxAbbreviationFullFormRunes
}

func validDefinitionPhrase(phrase string) bool {
	if phrase == "" || strings.ContainsAny(phrase, "?\"'`\r\n;=") || strings.Contains(phrase, ". ") {
		return false
	}
	lower := strings.ToLower(phrase)
	if lower == "gì" || lower == "nào" || lower == "ai" || lower == "ở đâu" || strings.HasSuffix(lower, " là gì") {
		return false
	}
	return !strings.HasPrefix(lower, "không phải") && !strings.HasPrefix(lower, "chưa phải") &&
		!strings.HasPrefix(lower, "chẳng phải") && !strings.HasPrefix(lower, "assistant nói") &&
		!strings.HasSuffix(lower, " đúng không") && !strings.HasSuffix(lower, " phải không") &&
		!strings.HasSuffix(lower, " hay không")
}

func validBareDefinitionPhrase(phrase string) bool {
	if !validDefinitionPhrase(phrase) {
		return false
	}
	words := strings.Fields(strings.ToLower(phrase))
	if len(words) == 0 || len(words) > 40 {
		return false
	}
	for _, word := range words {
		switch word {
		case "tôi", "mình", "biết", "cần", "muốn", "hỏi", "cho", "hãy", "xin", "gì", "nào", "ngày", "mai", "assistant":
			return false
		}
	}
	return true
}

func normalizedInitials(text string) string {
	var out strings.Builder
	for _, r := range norm.NFD.String(strings.ToUpper(text)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == 'Đ' {
			r = 'D'
		}
		if unicode.IsLetter(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func abbreviationInitials(phrase string) string {
	var out strings.Builder
	for _, word := range strings.Fields(phrase) {
		r, _ := utf8.DecodeRuneInString(word)
		if !unicode.IsLetter(r) {
			return ""
		}
		for _, part := range word {
			if !unicode.IsLetter(part) && !unicode.Is(unicode.Mn, part) {
				return ""
			}
		}
		out.WriteString(normalizedInitials(string(r)))
	}
	return out.String()
}
