package abbreviation

import (
	"regexp"
	"strings"
)

// mentionRe matches an @-mention as the chat composer writes it into the
// query text: @bao_cao.docx, @KB-Luật, @agent-name.
var mentionRe = regexp.MustCompile(`(^|\s)@[^\s@,;:!?()\[\]{}"'“”]+`)

// StripMentions removes @-mentions from text before abbreviation detection.
// A mentioned file name is not an abbreviation the user has to explain: the
// detector asked "nghĩa đầy đủ của docx" for "@thong_bao.docx".
func StripMentions(text string) string {
	if !strings.Contains(text, "@") {
		return text
	}
	return strings.Join(strings.Fields(mentionRe.ReplaceAllString(text, "$1")), " ")
}
