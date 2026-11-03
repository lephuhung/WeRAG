package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// The broad-question gate: a short generic question over the legal corpus
// ("luật đất đai", "quy định về cán bộ") would have retrieval pull
// passages of dozens of documents and the model summarize them all, slowly
// and vaguely. Before retrieval, when the catalogue holds at least
// broadQueryMinMatches documents whose titles carry the topic, the turn is
// answered with a card: the document types and issuers the topic spans,
// the newest documents in force, or "search anyway". When the input has a
// Judge, the model confirms the card first and names the subject
// (query_clarify_judge.go); it can only take the card away.
//
// It never asks when the question carries an anchor (a number, a year, a
// section reference, a quote, an @-mention), when it is about the user or
// the assistant ("về tôi", "của bạn"), when the topic left is only
// function words ("có", "là"), when it is longer than
// broadQueryMaxWords words, when the topic is longer than
// broadQueryMaxTopicWords words, when the user asked to search anyway or
// for everything, or when it already asked for that topic this session.
const (
	BroadQueryClarificationType = "broad_query_clarification"

	broadQueryMaxWords      = 12
	broadQueryMaxTopicWords = 4
	broadQueryMinMatches    = 20
	broadQuerySearchLimit   = 400
	broadQueryTypeOptions   = 3
	broadQueryIssuerOptions = 2
	broadQueryTTL           = 24 * time.Hour
)

// TitleSearcher finds catalogue documents whose title holds every word.
type TitleSearcher interface {
	SearchTitles(ctx context.Context, words []string, limit int) ([]*types.LegalDocument, error)
}

// broadQueryPhrases are the folded request phrases around a topic, longest
// first so "cho toi biet" goes before "cho" and "biet".
var broadQueryPhrases = func() [][]string {
	raw := `cho toi biet|cho minh biet|cho em biet|cho toi hoi|cho minh hoi|cho hoi|toi muon biet|toi muon hoi|toi can biet|
		tim kiem|tra cuu|tim hieu|tim|xem|doc|liet ke|cung cap|thong tin ve|thong tin|noi dung ve|noi dung|
		van ban ve|van ban|quy dinh ve|cac quy dinh ve|cac quy dinh|quy dinh|tai lieu ve|tai lieu|lien quan den|lien quan toi|lien quan|
		ve|cua|la gi|nhu the nao|the nao|ra sao|co gi|co nhung gi|gom nhung gi|co nhung|khong|nhung|cac|nao|hien nay|hien hanh|
		giup toi|giup minh|giup|voi|a|nhe|nha|di|xin|vui long|lam on|toi|minh|em|anh|chi|ban|cho|hoi|biet|muon|gi`
	var out [][]string
	for _, p := range strings.Split(raw, "|") {
		if words := strings.Fields(p); len(words) > 0 {
			out = append(out, words)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}()

var (
	// an anchor: a digit (số hiệu, year, article), a quote, a mention
	broadQueryAnchorRe = regexp.MustCompile(`[0-9"“”'@]`)
	// the user asked for everything or to search anyway
	broadQueryAllRe = regexp.MustCompile(`\b(tim chung|tat ca|toan bo|moi van ban|het)\b`)
	// the question is about a person in the chat, not the corpus: "về tôi",
	// "của bạn", "tôi là ai". Matched with the diacritics kept, since
	// folded "ban", "minh", "to" are also "bản", "minh bạch", "tổ chức".
	broadQueryPersonalRe = regexp.MustCompile(` (về|của) (tôi|mình|tớ|tui|tao|bạn|chúng tôi|chúng mình) | (tôi|mình|bạn) là ai `)
)

// broadQueryFunctionWords never make a topic on their own: a question whose
// topic is only these ("bạn có thông tin gì về tôi" → "có") names nothing to
// look up, and a title search for them matches the whole catalogue. Kept
// with diacritics: folded "do thi", "dau khi", "dang" are also "đô thị",
// "dầu khí", "Đảng".
var broadQueryFunctionWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`có là được bị sẽ đã đang vừa sắp thì mà và hay hoặc như này đó kia ấy
		ai đâu sao bao nhiêu mấy rồi chưa chẳng nữa cũng đều từng mới rất quá lắm hơn nhất
		ở tại khi nếu vì nên để thế vậy đấy`) {
		broadQueryFunctionWords[w] = true
	}
}

// onlyFunctionWords reports whether every topic word is a function word.
func onlyFunctionWords(words []string) bool {
	for _, w := range words {
		if !broadQueryFunctionWords[strings.Trim(strings.ToLower(norm.NFC.String(w)), ".,;:!?()")] {
			return false
		}
	}
	return true
}

// BroadQueryOption is one way to narrow the question.
type BroadQueryOption struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Resend string `json:"resend"`
}

// BroadQueryCard is the payload of the gate's question.
type BroadQueryCard struct {
	Query   string
	Topic   string
	Total   int
	Options []BroadQueryOption
	// topics as folded words, for the session memory: the rules' and,
	// when the judge named another, the judged one, so a repeat or a click
	// on an option is not asked about again whatever the judge says then
	keys []string
}

// Data is the card payload (display_type broad_query_clarification).
func (c *BroadQueryCard) Data() map[string]interface{} {
	opts := make([]map[string]interface{}, 0, len(c.Options))
	for _, o := range c.Options {
		opts = append(opts, map[string]interface{}{"label": o.Label, "detail": o.Detail, "resend": o.Resend})
	}
	return map[string]interface{}{
		"display_type": BroadQueryClarificationType,
		"query":        c.Query,
		"topic":        c.Topic,
		"total":        c.Total,
		"options":      opts,
	}
}

// Message is the question shown above the options, listing them in text
// too so a client without the card (Word add-in, IM) can answer by number.
func (c *BroadQueryCard) Message() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Câu hỏi về “%s” khá rộng: danh mục có %d văn bản về chủ đề này, trả lời chung sẽ chậm và thiếu trọng tâm. Bạn muốn xem nhóm nào?\n", c.Topic, c.Total)
	for i, o := range c.Options {
		fmt.Fprintf(&b, "%d. %s", i+1, o.Label)
		if o.Detail != "" {
			b.WriteString(" — " + o.Detail)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// BroadQueryInput is what the gate looks at.
type BroadQueryInput struct {
	Query string
	// Disabled turns the gate off (the setting).
	Disabled bool
	// Judge, when set, confirms the card the rules would show: "not
	// broad", an error or a timeout leaves the turn to run as usual.
	Judge docformat.Completer
}

// BroadQueryClarification runs the gate for a turn; nil means go on.
func BroadQueryClarification(ctx context.Context, catalog TitleSearcher, sessionID string, in BroadQueryInput) *BroadQueryCard {
	query := strings.TrimSpace(in.Query)
	if in.Disabled || catalog == nil || query == "" {
		return nil
	}
	folded := foldSearch(query)
	if broadQueryAnchorRe.MatchString(query) || searchReferenceRe.MatchString(folded) || broadQueryAllRe.MatchString(folded) ||
		broadQueryPersonalRe.MatchString(" "+strings.Join(strings.Fields(strings.Map(foldPunctToSpace, strings.ToLower(norm.NFC.String(query)))), " ")+" ") {
		return nil
	}
	words := strings.Fields(query)
	if len(words) == 0 || len(words) > broadQueryMaxWords {
		return nil
	}
	topicWords := broadQueryTopic(words)
	// "luật đất đai", "nghị định về cán bộ": the document type named up
	// front is one of the choices the card offers, so the topic is the rest
	topicWords = stripLeadingDocType(topicWords)
	if len(topicWords) == 0 || len(topicWords) > broadQueryMaxTopicWords || onlyFunctionWords(topicWords) {
		return nil
	}
	foldedTopic := make([]string, 0, len(topicWords))
	for _, w := range topicWords {
		foldedTopic = append(foldedTopic, foldSearch(w))
	}
	key := strings.Join(foldedTopic, " ")
	if broadQueryAsked(ctx, sessionID, key) {
		return nil
	}
	keys := []string{key}
	docs, err := catalog.SearchTitles(ctx, topicWords, broadQuerySearchLimit)
	if err != nil {
		logger.Warnf(ctx, "[BroadQuery] session=%s title search: %v", sessionID, err)
		return nil
	}
	if len(docs) < broadQueryMinMatches {
		return nil
	}
	if in.Judge != nil {
		broad, judged, err := judgeBroadQuery(ctx, in.Judge, query)
		if err != nil {
			logger.Warnf(ctx, "[BroadQuery] session=%s topic=%q: judge failed, no card: %v", sessionID, strings.Join(topicWords, " "), err)
			return nil
		}
		if !broad {
			logger.Infof(ctx, "[BroadQuery] session=%s topic=%q matches %d titles but the judge says not broad: no card", sessionID, strings.Join(topicWords, " "), len(docs))
			return nil
		}
		// the subject as the user wrote it: the rules' word stripping can
		// cut a content word ("bảo hiểm xã hội" → "bảo hiểm xã")
		if words := judgedTopicWords(query, judged); words != nil && !sameFoldedWords(words, foldedTopic) {
			topicWords = words
			foldedTopic = foldedTopic[:0]
			for _, w := range topicWords {
				foldedTopic = append(foldedTopic, foldSearch(w))
			}
			key = strings.Join(foldedTopic, " ")
			if broadQueryAsked(ctx, sessionID, key) {
				return nil
			}
			keys = append(keys, key)
			if docs, err = catalog.SearchTitles(ctx, topicWords, broadQuerySearchLimit); err != nil {
				logger.Warnf(ctx, "[BroadQuery] session=%s title search: %v", sessionID, err)
				return nil
			}
			if len(docs) < broadQueryMinMatches {
				return nil
			}
		}
	}
	topic := strings.Join(topicWords, " ")
	card := &BroadQueryCard{Query: query, Topic: topic, Total: len(docs), keys: keys}
	for _, g := range topGroups(docs, func(d *types.LegalDocument) string { return d.DocType }, broadQueryTypeOptions) {
		card.Options = append(card.Options, BroadQueryOption{
			Label:  g.name + " về " + topic,
			Detail: fmt.Sprintf("%d văn bản", g.n),
			Resend: g.name + " về " + topic,
		})
	}
	for _, g := range topGroups(docs, func(d *types.LegalDocument) string { return d.Issuer }, broadQueryIssuerOptions) {
		card.Options = append(card.Options, BroadQueryOption{
			Label:  "Văn bản của " + g.name + " về " + topic,
			Detail: fmt.Sprintf("%d văn bản", g.n),
			Resend: "Văn bản của " + g.name + " về " + topic,
		})
	}
	card.Options = append(card.Options,
		BroadQueryOption{Label: "Văn bản mới nhất còn hiệu lực về " + topic, Resend: "Văn bản mới nhất còn hiệu lực về " + topic},
		BroadQueryOption{Label: "Vẫn tìm chung về " + topic, Detail: "trả lời tổng hợp, có thể chậm", Resend: query + " (tìm chung)"},
	)
	logger.Infof(ctx, "[BroadQuery] session=%s topic=%q matches %d catalogue documents: asking", sessionID, topic, len(docs))
	return card
}

// sameFoldedWords reports whether words fold to the folded words want.
func sameFoldedWords(words, want []string) bool {
	if len(words) != len(want) {
		return false
	}
	for i, w := range words {
		if foldSearch(w) != want[i] {
			return false
		}
	}
	return true
}

// foldPunctToSpace turns punctuation into spaces so the personal pattern
// sees "về tôi?" as words.
func foldPunctToSpace(r rune) rune {
	if strings.ContainsRune(".,;:!?()", r) {
		return ' '
	}
	return r
}

// broadQueryTopic strips the request phrases around the topic and returns
// the topic words as written ("Luật đất đai là gì" → "Luật đất đai").
func broadQueryTopic(words []string) []string {
	folded := make([]string, len(words))
	for i, w := range words {
		folded[i] = strings.Trim(foldSearch(w), ".,;:!?()")
	}
	keep := make([]bool, len(words))
	for i := range keep {
		keep[i] = true
	}
	for _, phrase := range broadQueryPhrases {
		for i := 0; i+len(phrase) <= len(folded); i++ {
			match := true
			for j, pw := range phrase {
				if !keep[i+j] || folded[i+j] != pw {
					match = false
					break
				}
			}
			if match {
				for j := range phrase {
					keep[i+j] = false
				}
			}
		}
	}
	var out []string
	for i, w := range words {
		if keep[i] && folded[i] != "" {
			out = append(out, strings.Trim(w, ".,;:!?()"))
		}
	}
	return out
}

// broadQueryDocTypes are the folded document types a question may start
// with ("luật", "nghị định"), longest first.
var broadQueryDocTypes = [][]string{
	{"nghi", "dinh"}, {"thong", "tu"}, {"quyet", "dinh"}, {"chi", "thi"}, {"nghi", "quyet"}, {"phap", "lenh"},
	{"thong", "bao"}, {"cong", "van"}, {"ke", "hoach"}, {"luat"},
}

// stripLeadingDocType drops a document type at the start of the topic
// when something is left after it.
func stripLeadingDocType(words []string) []string {
	for _, dt := range broadQueryDocTypes {
		if len(words) <= len(dt) {
			continue
		}
		match := true
		for i, w := range dt {
			if foldSearch(words[i]) != w {
				match = false
				break
			}
		}
		if match {
			return words[len(dt):]
		}
	}
	return words
}

type docGroup struct {
	name string
	n    int
}

// topGroups counts the documents per key and returns the largest limit
// groups, largest first; empty keys are skipped.
func topGroups(docs []*types.LegalDocument, key func(*types.LegalDocument) string, limit int) []docGroup {
	counts := map[string]int{}
	for _, d := range docs {
		if k := strings.TrimSpace(key(d)); k != "" {
			counts[k]++
		}
	}
	groups := make([]docGroup, 0, len(counts))
	for k, n := range counts {
		groups = append(groups, docGroup{name: k, n: n})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].n != groups[j].n {
			return groups[i].n > groups[j].n
		}
		return groups[i].name < groups[j].name
	})
	if len(groups) > limit {
		groups = groups[:limit]
	}
	return groups
}

// The topics asked about per session, so the gate asks once: in Redis
// (werag:broadq:<session>) when available, else in process memory.
const broadQueryKeyPrefix = "werag:broadq:"

type storedBroadQueries struct {
	topics  map[string]bool
	expires time.Time
}

var (
	broadQueries   sync.Map // session ID → storedBroadQueries
	broadQueriesMu sync.Mutex
)

func loadBroadQueries(ctx context.Context, sessionID string) map[string]bool {
	if formatChecks.rdb != nil {
		var topics map[string]bool
		if formatChecks.redisGet(ctx, broadQueryKeyPrefix+sessionID, &topics) && topics != nil {
			return topics
		}
		return map[string]bool{}
	}
	v, ok := broadQueries.Load(sessionID)
	if !ok {
		return map[string]bool{}
	}
	st := v.(storedBroadQueries)
	if time.Now().After(st.expires) {
		broadQueries.Delete(sessionID)
		return map[string]bool{}
	}
	out := make(map[string]bool, len(st.topics))
	for k := range st.topics {
		out[k] = true
	}
	return out
}

func broadQueryAsked(ctx context.Context, sessionID, key string) bool {
	return sessionID != "" && loadBroadQueries(ctx, sessionID)[key]
}

// MarkBroadQueryAsked records that the card was shown for the topic of
// the card in this session, so the same topic is not asked about again.
func MarkBroadQueryAsked(ctx context.Context, sessionID string, card *BroadQueryCard) {
	if sessionID == "" || card == nil || len(card.keys) == 0 {
		return
	}
	broadQueriesMu.Lock()
	defer broadQueriesMu.Unlock()
	topics := loadBroadQueries(ctx, sessionID)
	for _, k := range card.keys {
		topics[k] = true
	}
	if formatChecks.rdb == nil {
		broadQueries.Store(sessionID, storedBroadQueries{topics: topics, expires: time.Now().Add(broadQueryTTL)})
		return
	}
	formatChecks.redisSet(ctx, broadQueryKeyPrefix+sessionID, topics)
}
