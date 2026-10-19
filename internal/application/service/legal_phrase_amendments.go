package service

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Current wording for the model: a retrieved provision whose words a newer
// document replaced in place ("Sửa đổi cụm từ “500 triệu đồng” thành “01 tỷ
// đồng” tại Điều 3, Điều 4 …") is shown to the model with the new words and
// the amendment named, so it does not answer from the old figure. The
// reference the user opens keeps the stored text.

const (
	// legalPhraseCandidates bounds the amending chunks read per search.
	legalPhraseCandidates = 200
	// legalPhraseChainHops bounds A → B → C … within one provision.
	legalPhraseChainHops = 5
)

// legalPhraseChange is one phrase amendment of a provision of a retrieved
// chunk: From becomes To (To "" = removed) in khoản Khoan / điểm Diem
// ("" = the whole Điều).
type legalPhraseChange struct {
	From, To    string
	Khoan, Diem string
	Amender     legalHistoryDoc
}

// LegalCurrentWording returns, by chunk id, the content of each retrieved
// chunk with the phrase amendments in force applied (pending ones noted);
// chunks without such amendments are left out.
func LegalCurrentWording(ctx context.Context, db *gorm.DB, kbIDs, chunkIDs []string, now time.Time) map[string]string {
	if db == nil || len(kbIDs) == 0 || len(chunkIDs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, legalHistoryTimeout)
	defer cancel()
	h := &legalHistory{amended: map[string]string{}, ctx: ctx, db: db.WithContext(ctx), kbIDs: kbIDs, now: now}
	chunks, err := h.loadChunks(chunkIDs)
	if err != nil {
		logger.Warnf(ctx, "[LegalWording] load chunks: %v", err)
		return nil
	}
	byChunk := h.phraseChanges(chunks)
	out := map[string]string{}
	for _, c := range chunks {
		if changes := byChunk[c.ID]; len(changes) > 0 {
			if text := applyPhraseChanges(c.Content, changes, now); text != c.Content {
				out[c.ID] = text
			}
		}
	}
	return out
}

// phraseChanges lists, by chunk id, the phrase amendments other documents
// make to each single-Điều chunk, read in one query for all of them.
func (h *legalHistory) phraseChanges(chunks []legalHistoryChunk) map[string][]legalPhraseChange {
	var targets []legalHistoryChunk
	articles := map[string]bool{}
	for _, c := range chunks {
		if len(c.Legal.ArticleNos) == 1 {
			targets = append(targets, c)
			articles[c.Legal.ArticleNos[0]] = true
		}
	}
	if len(targets) == 0 {
		return nil
	}
	var conds []string
	var args []interface{}
	args = append(args, h.kbIDs)
	for a := range articles {
		filter, _ := json.Marshal([]map[string]interface{}{{"article": a, "phrase": true}})
		conds = append(conds, `c.metadata->'legal'->'amends' @> ?::jsonb`)
		args = append(args, string(filter))
	}
	sort.Strings(conds) // stable SQL text
	args = append(args, legalPhraseCandidates)
	var rows []legalHistoryRow
	if err := h.db.Raw(legalHistorySelect+` AND jsonb_exists(c.metadata->'legal', 'amends')
		AND (`+strings.Join(conds, " OR ")+`) LIMIT ?`, args...).Scan(&rows).Error; err != nil {
		logger.Warnf(h.ctx, "[LegalWording] amending chunks: %v", err)
		return nil
	}
	amenders := parseLegalHistoryRows(rows)
	out := map[string][]legalPhraseChange{}
	var numbers []string
	for _, c := range targets {
		article := c.Legal.ArticleNos[0]
		khoans := setOf(c.Legal.KhoanNos)
		seen := map[string]bool{}
		for _, ac := range amenders {
			if ac.KnowledgeID == c.KnowledgeID {
				continue
			}
			for _, a := range ac.Legal.Amends {
				if !a.Phrase || a.From == "" || a.Article != article {
					continue
				}
				if a.Khoan != "" && len(khoans) > 0 && !khoans[a.Khoan] {
					continue
				}
				if h.amendedNumber(ac.Doc, a.Document) != c.Doc.Number {
					continue
				}
				key := ac.Doc.Number + "|" + a.Khoan + "|" + a.Diem + "|" + a.From + "|" + a.To
				if seen[key] {
					continue
				}
				seen[key] = true
				out[c.ID] = append(out[c.ID], legalPhraseChange{From: a.From, To: a.To, Khoan: a.Khoan, Diem: a.Diem, Amender: ac.Doc})
				numbers = append(numbers, ac.Doc.Number)
			}
		}
	}
	// an amender no longer in force changes nothing
	if expired := fullyExpiredDocs(h.ctx, h.db, numbers); len(expired) > 0 {
		for id, list := range out {
			kept := list[:0]
			for _, ch := range list {
				if _, gone := expired[vietnamese_legal.CatalogNumberKey(ch.Amender.Number)]; !gone {
					kept = append(kept, ch)
				}
			}
			out[id] = kept
		}
	}
	return out
}

func setOf(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, v := range list {
		m[v] = true
	}
	return m
}

// phraseEffect is when a change applies: its effective date, else its
// issue date; known is false when the amender gives neither.
func phraseEffect(d legalHistoryDoc, loc *time.Location) (t time.Time, known bool) {
	for _, s := range []string{d.EffFrom, d.IssueDate} {
		if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// phraseSpan is one occurrence of an amended phrase in the chunk.
type phraseSpan struct {
	start, end int
	repl       string
}

// applyPhraseChanges rewrites content in one pass. For each occurrence of a
// changed phrase, the changes whose provision holds it are followed from
// the stored words: the newest change in force of those words, then the
// newest in force of its new words, and so on (A → B → C). The occurrence
// then reads "<current> [thay cho “<stored>” theo <amenders>]"; a change
// not yet in force, or without any date, is only noted after the words.
// Notes are never rewritten again.
func applyPhraseChanges(content string, changes []legalPhraseChange, now time.Time) string {
	froms := map[string]bool{}
	for _, ch := range changes {
		froms[ch.From] = true
	}
	// longer phrases first, so "500 triệu đồng" wins over "500 triệu"
	order := make([]string, 0, len(froms))
	for f := range froms {
		order = append(order, f)
	}
	sort.Slice(order, func(i, j int) bool {
		if len(order[i]) != len(order[j]) {
			return len(order[i]) > len(order[j])
		}
		return order[i] < order[j]
	})
	scope := newProvisionScope(content)
	var spans []phraseSpan
	taken := func(s, e int) bool {
		for _, sp := range spans {
			if s < sp.end && sp.start < e {
				return true
			}
		}
		return false
	}
	for _, from := range order {
		for _, loc := range findPhrase(content, from) {
			if taken(loc[0], loc[1]) {
				continue
			}
			khoan, diem := scope.at(loc[0])
			var here []legalPhraseChange
			for _, ch := range changes {
				if (ch.Khoan == "" || ch.Khoan == khoan) && (ch.Diem == "" || ch.Diem == diem) {
					here = append(here, ch)
				}
			}
			if repl, ok := phraseReplacement(content[loc[0]:loc[1]], here, now); ok {
				spans = append(spans, phraseSpan{loc[0], loc[1], repl})
			}
		}
	}
	if len(spans) == 0 {
		return content
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(content[last:sp.start])
		b.WriteString(sp.repl)
		last = sp.end
	}
	b.WriteString(content[last:])
	return b.String()
}

// phraseReplacement follows the changes of the stored words; ok is false
// when none applies.
func phraseReplacement(stored string, changes []legalPhraseChange, now time.Time) (string, bool) {
	current := stored
	var since time.Time
	var via []string
	removed := false
	for hop := 0; hop < legalPhraseChainHops && !removed; hop++ {
		best, found := -1, false
		var bestAt time.Time
		for i, ch := range changes {
			if !samePhrase(ch.From, current) {
				continue
			}
			at, known := phraseEffect(ch.Amender, now.Location())
			if !known || at.After(now) || at.Before(since) {
				continue
			}
			if !found || at.After(bestAt) || (at.Equal(bestAt) && ch.Amender.Number > changes[best].Amender.Number) {
				best, bestAt, found = i, at, true
			}
		}
		if !found {
			break
		}
		ch := changes[best]
		via = append(via, ch.Amender.label()+", từ "+bestAt.Format("02/01/2006"))
		since = bestAt
		if ch.To == "" {
			removed = true
		} else {
			current = ch.To
		}
	}
	// a change of the current words not (yet) applicable is only noted
	var pending []string
	if !removed {
		for _, ch := range changes {
			if !samePhrase(ch.From, current) {
				continue
			}
			at, known := phraseEffect(ch.Amender, now.Location())
			switch {
			case !known:
				pending = append(pending, phraseChangeText(ch)+" theo "+ch.Amender.label()+", chưa rõ ngày hiệu lực")
			case at.After(now):
				pending = append(pending, "sẽ "+phraseChangeText(ch)+" theo "+ch.Amender.label()+", từ "+at.Format("02/01/2006"))
			}
		}
	}
	var repl string
	switch {
	case removed:
		repl = "[cụm từ “" + stored + "” đã bị bãi bỏ theo " + strings.Join(via, "; ") + "]"
	case len(via) > 0:
		repl = current + " [thay cho “" + stored + "” theo " + strings.Join(via, "; ") + "]"
	default:
		repl = stored
	}
	if len(pending) > 0 {
		repl += " [" + strings.Join(pending, "; ") + "]"
	}
	return repl, len(via) > 0 || len(pending) > 0
}

func phraseChangeText(ch legalPhraseChange) string {
	if ch.To == "" {
		return "bị bãi bỏ"
	}
	return "được thay bằng “" + ch.To + "”"
}

// samePhrase compares two phrases ignoring case and spacing.
func samePhrase(a, b string) bool {
	return foldSpaces(a) == foldSpaces(b)
}

// findPhrase returns the byte ranges of phrase in content, ignoring case
// and spacing, not glued to a longer word or number ("500 triệu đồng" is
// not in "1.500 triệu đồng", "25 ngày" not in "125 ngày").
func findPhrase(content, phrase string) [][2]int {
	words := strings.Fields(phrase)
	if len(words) == 0 {
		return nil
	}
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	re, err := regexp.Compile(`(?i)` + strings.Join(words, `\s+`))
	if err != nil {
		return nil
	}
	var out [][2]int
	for _, m := range re.FindAllStringIndex(content, -1) {
		if !gluedAt(content, m[0], m[1]) {
			out = append(out, [2]int{m[0], m[1]})
		}
	}
	return out
}

// gluedAt reports whether content[start:end] runs on into a word or number
// on either side; "." or "," between digits count as part of the number.
func gluedAt(content string, start, end int) bool {
	alnum := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	first, _ := utf8.DecodeRuneInString(content[start:])
	lastR, _ := utf8.DecodeLastRuneInString(content[:end])
	if start > 0 {
		before, n := utf8.DecodeLastRuneInString(content[:start])
		if alnum(before) && alnum(first) {
			return true
		}
		if (before == '.' || before == ',') && unicode.IsDigit(first) {
			if prev, _ := utf8.DecodeLastRuneInString(content[:start-n]); unicode.IsDigit(prev) {
				return true
			}
		}
	}
	if end < len(content) {
		after, n := utf8.DecodeRuneInString(content[end:])
		if alnum(after) && alnum(lastR) {
			return true
		}
		if (after == '.' || after == ',') && unicode.IsDigit(lastR) {
			if next, _ := utf8.DecodeRuneInString(content[end+n:]); unicode.IsDigit(next) {
				return true
			}
		}
	}
	return false
}

var diemStartRe = regexp.MustCompile(`(?m)^\s*(?:> )?([a-zđ])\)\s`)

// provisionScope tells which khoản and điểm of an article chunk a byte
// offset falls in ("" before the first one).
type provisionScope struct {
	khoans, diems []scopeMark
}

type scopeMark struct {
	at    int
	label string
}

func newProvisionScope(content string) provisionScope {
	var s provisionScope
	for _, m := range khoanStartRe.FindAllStringSubmatchIndex(content, -1) {
		s.khoans = append(s.khoans, scopeMark{m[0], content[m[2]:m[3]]})
	}
	for _, m := range diemStartRe.FindAllStringSubmatchIndex(content, -1) {
		s.diems = append(s.diems, scopeMark{m[0], content[m[2]:m[3]]})
	}
	return s
}

func (s provisionScope) at(pos int) (khoan, diem string) {
	khoanAt := -1
	for _, k := range s.khoans {
		if k.at <= pos {
			khoan, khoanAt = k.label, k.at
		}
	}
	for _, d := range s.diems {
		if d.at <= pos && d.at > khoanAt {
			diem = d.label
		}
	}
	return khoan, diem
}
