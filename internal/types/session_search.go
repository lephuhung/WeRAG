package types

import (
	"strings"
	"time"
	"unicode"
)

// SessionContentSearchQuery searches the text of the messages of the
// sessions the session list would show the caller. Source is "" (the web
// list: Word add-in sessions hidden) or SessionSourceWord (the add-in's own).
// Matching is a case-insensitive substring match (ILIKE), accent-sensitive,
// like the search_conversations tool.
type SessionContentSearchQuery struct {
	TenantID uint64
	UserID   string
	Query    string
	Source   string
	Offset   int
	Limit    int
}

// SessionContentHit is one session whose messages match: its newest
// matching message (Content is not sent to clients, only the snippet built
// from it) and how many of its messages match.
type SessionContentHit struct {
	SessionID  string    `json:"session_id"   gorm:"column:session_id"`
	Title      string    `json:"title"        gorm:"column:title"`
	ChatRootID string    `json:"chat_root_id,omitempty" gorm:"column:chat_root_id"`
	MessageID  string    `json:"message_id"   gorm:"column:message_id"`
	Role       string    `json:"role"         gorm:"column:role"`
	MatchedAt  time.Time `json:"matched_at"   gorm:"column:matched_at"`
	MatchCount int       `json:"match_count"  gorm:"column:match_count"`
	Content    string    `json:"-"            gorm:"column:content"`
	Snippet    Snippet   `json:"snippet"      gorm:"-"`
}

// Snippet is a short window of a message around the first match: Before
// and After are the text either side of Match (whitespace collapsed, "…"
// where the message was cut). Match is "" when the match could not be
// located again (then Before holds the start of the message).
type Snippet struct {
	Before string `json:"before"`
	Match  string `json:"match"`
	After  string `json:"after"`
}

// snippetContext is how many runes of context a snippet keeps around the
// match on each side.
const snippetContext = 60

// MakeSnippet finds query in content case-insensitively (rune by rune, so
// Vietnamese letters with diacritics fold like ILIKE folds them: "Đ"/"đ",
// "Ệ"/"ệ") and cuts a window around the first match on rune boundaries.
func MakeSnippet(content, query string) Snippet {
	text := []rune(content)
	needle := []rune(query)
	at := indexFold(text, needle)
	if at < 0 || len(needle) == 0 {
		head := text
		cut := false
		if len(head) > 2*snippetContext {
			head, cut = head[:2*snippetContext], true
		}
		s := collapseSpace(string(head))
		if cut {
			s = trimToWord(s, false) + "…"
		}
		return Snippet{Before: s}
	}
	end := at + len(needle)
	from, to := max(0, at-snippetContext), min(len(text), end+snippetContext)
	before := collapseSpace(string(text[from:at]))
	after := collapseSpace(string(text[end:to]))
	if from > 0 {
		before = "…" + trimToWord(before, true)
	} else {
		before = strings.TrimLeft(before, " ")
	}
	if to < len(text) {
		after = trimToWord(after, false) + "…"
	} else {
		after = strings.TrimRight(after, " ")
	}
	return Snippet{Before: before, Match: collapseSpace(string(text[at:end])), After: after}
}

// indexFold is the rune index of the first case-insensitive instance of
// needle in text, -1 when none.
func indexFold(text, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(text) {
		return -1
	}
	fold := func(r rune) rune { return unicode.ToLower(r) }
outer:
	for i := 0; i+len(needle) <= len(text); i++ {
		for j, n := range needle {
			if fold(text[i+j]) != fold(n) {
				continue outer
			}
		}
		return i
	}
	return -1
}

// collapseSpace turns every run of whitespace (newlines included) into
// one space, keeping a leading or trailing one.
func collapseSpace(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// trimToWord drops the partial word at the cut end of s (the start when
// atStart), unless that would drop most of it.
func trimToWord(s string, atStart bool) string {
	if atStart {
		if i := strings.IndexByte(s, ' '); i >= 0 && i < len(s)/2 {
			return s[i+1:]
		}
		return s
	}
	if i := strings.LastIndexByte(s, ' '); i > len(s)/2 {
		return s[:i]
	}
	return s
}
