package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/docformat"
)

// The rules of the broad-question gate only see words: "hi", "bỏ qua",
// "tìm kiếm trong văn bản", "VP" all left a topic whose words sit in
// dozens of catalogue titles, and the turn got the narrowing card instead
// of an answer (8 of the 11 cards the rules would show on 798 dev
// questions). Before the card is shown one short labeling call asks
// whether the message really is a general question about a subject area,
// and for the subject as the user wrote it. The model can only take a card
// away: "not broad", an error or a timeout means the turn runs as usual,
// and a subject that does not match broadQueryMinMatches titles shows no
// card either.

// broadQueryJudgeTimeout bounds the call; the rules only reach it on the
// few turns they would answer with the card. It is shorter than the intent
// model's own budget (intentPrimaryTimeout): a stalled intent model means
// no card, not a second try with the answering model.
const broadQueryJudgeTimeout = 4 * time.Second

const broadQueryJudgePrompt = `You check one message a user sent to an assistant that answers from a catalogue of Vietnamese laws and official documents. A word rule would answer this message with a card asking the user to narrow the subject (by document type, issuer or newest documents) instead of answering. Decide whether that card fits.

broad = true only when the message asks in general about a whole subject area of law or administration and names nothing more specific: "luật đất đai", "quy định về cán bộ", "văn bản về phòng cháy chữa cháy", "bảo hiểm xã hội", "cho tôi biết về đấu thầu", "nghiên cứu khoa học". Naming a document type without its number ("nghị định về …", "thông tư về …", "luật …") keeps it broad: the card offers the types to pick from.

broad = false for everything else:
- greetings, thanks, small talk, test messages ("chào anh", "cảm ơn", "test 123");
- short replies and choices ("ừ", "đồng ý", "phương án 2", "làm lại đi");
- requests about the conversation, the open document or attached files ("rà soát đoạn này", "file vừa gửi nói gì", "ý chính của tài liệu");
- questions about the user or the assistant ("bạn biết gì về tôi", "bạn là ai");
- a bare abbreviation or code ("PC06", "HĐND");
- a specific question: a situation, a procedure, a penalty, a deadline, a definition, a comparison;
- commands to write, draft, create or translate something ("lập kế hoạch", "dịch đoạn này").

topic: when broad, the subject copied word for word from the message, without the request words ("cho tôi biết", "quy định về", "văn bản về", "luật"): "đất đai", "bảo hiểm xã hội". Empty when not broad.

Answer with JSON {"broad": true, "topic": "..."} or {"broad": false, "topic": ""} and nothing else.`

var errBroadQueryJudge = errors.New("broad-question judge: unreadable reply")

// judgeBroadQuery asks the model whether query is a general question about
// a subject area; topic is the subject it named ("" when none or not broad).
func judgeBroadQuery(ctx context.Context, model docformat.Completer, query string) (broad bool, topic string, err error) {
	ctx, cancel := context.WithTimeout(ctx, broadQueryJudgeTimeout)
	defer cancel()
	reply, err := model.Complete(ctx, []docformat.Message{
		{Role: "system", Content: broadQueryJudgePrompt},
		{Role: "user", Content: "Tin nhắn: " + clipRunes(query, 400)},
	})
	if err != nil {
		return false, "", err
	}
	var out struct {
		Broad *bool  `json:"broad"`
		Topic string `json:"topic"`
	}
	reply = strings.TrimSpace(reply)
	if i, j := strings.Index(reply, "{"), strings.LastIndex(reply, "}"); i >= 0 && j > i {
		reply = reply[i : j+1]
	}
	if json.Unmarshal([]byte(reply), &out) != nil || out.Broad == nil {
		return false, "", errBroadQueryJudge
	}
	if !*out.Broad {
		return false, "", nil
	}
	return true, strings.TrimSpace(out.Topic), nil
}

// judgedTopicWords keeps the model's subject only when it is a run of
// consecutive words of the query, in order (it may not invent or reorder a
// subject), and something besides a document type and function words is
// left; nil otherwise. Words compare lower-cased, NFC, without punctuation.
func judgedTopicWords(query, topic string) []string {
	words := strings.Fields(strings.Map(foldPunctToSpace, norm.NFC.String(topic)))
	if len(words) == 0 || len(words) > broadQueryMaxTopicWords+2 {
		return nil
	}
	queryWords := strings.Fields(strings.Map(foldPunctToSpace, norm.NFC.String(query)))
	if !containsRun(queryWords, words) {
		return nil
	}
	words = stripLeadingDocType(words)
	if len(words) > broadQueryMaxTopicWords || onlyFunctionWords(words) {
		return nil
	}
	return words
}

// containsRun reports whether run occurs in words as consecutive words,
// compared case-insensitively.
func containsRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		match := true
		for j, w := range run {
			if strings.ToLower(words[i+j]) != strings.ToLower(w) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
