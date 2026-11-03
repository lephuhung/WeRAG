package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/types"
)

// fakeTitles answers every search with n documents spread over types and
// issuers, and records the words it was asked for.
type fakeTitles struct {
	n     int
	words []string
}

func (f *fakeTitles) SearchTitles(_ context.Context, words []string, limit int) ([]*types.LegalDocument, error) {
	f.words = words
	types_ := []string{"Luật", "Nghị định", "Thông tư", "Quyết định"}
	issuers := []string{"Quốc hội", "Chính phủ", "Bộ Tài nguyên và Môi trường"}
	var out []*types.LegalDocument
	for i := 0; i < f.n && i < limit; i++ {
		out = append(out, &types.LegalDocument{
			DocumentNumber: fmt.Sprintf("%d/2020", i), Title: "Về đất đai " + fmt.Sprint(i),
			DocType: types_[i%len(types_)], Issuer: issuers[i%len(issuers)],
		})
	}
	return out, nil
}

func TestBroadQueryTopicStripsTheRequestPhrases(t *testing.T) {
	cases := map[string]string{
		"Luật đất đai là gì":                    "Luật đất đai",
		"cho tôi biết quy định về đất đai":      "đất đai",
		"Tìm các văn bản liên quan đến cán bộ":  "cán bộ",
		"đất đai có những quy định gì không":    "đất đai",
		"quy định về thuế thu nhập cá nhân nhé": "thuế thu nhập cá nhân",
	}
	for q, want := range cases {
		if got := strings.Join(broadQueryTopic(strings.Fields(q)), " "); got != want {
			t.Errorf("%q → %q, want %q", q, got, want)
		}
	}
}

func TestStripLeadingDocType(t *testing.T) {
	if got := strings.Join(stripLeadingDocType(strings.Fields("Luật đất đai")), " "); got != "đất đai" {
		t.Fatalf("got %q", got)
	}
	if got := strings.Join(stripLeadingDocType(strings.Fields("Nghị định cán bộ")), " "); got != "cán bộ" {
		t.Fatalf("got %q", got)
	}
	if got := strings.Join(stripLeadingDocType(strings.Fields("Luật")), " "); got != "Luật" {
		t.Fatalf("a bare type stays: got %q", got)
	}
}

func TestBroadQueryAsksOverAWideTopic(t *testing.T) {
	freshSessionStores(t)
	cat := &fakeTitles{n: 60}
	card := BroadQueryClarification(context.Background(), cat, "s-wide", BroadQueryInput{Query: "cho tôi biết quy định về luật đất đai"})
	if card == nil {
		t.Fatal("a topic over 60 catalogue documents must be asked about")
	}
	if card.Topic != "đất đai" || card.Total != 60 {
		t.Fatalf("card %+v", card)
	}
	if len(cat.words) != 2 {
		t.Fatalf("searched words %v", cat.words)
	}
	labels := make([]string, 0, len(card.Options))
	for _, o := range card.Options {
		labels = append(labels, o.Label)
	}
	joined := strings.Join(labels, " | ")
	for _, want := range []string{"Luật về đất đai", "Văn bản của Chính phủ về đất đai", "Văn bản mới nhất còn hiệu lực về đất đai", "Vẫn tìm chung về đất đai"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("options %s lack %q", joined, want)
		}
	}
	if card.Options[len(card.Options)-1].Resend != "cho tôi biết quy định về luật đất đai (tìm chung)" {
		t.Fatalf("search-anyway resend %q", card.Options[len(card.Options)-1].Resend)
	}
	if !strings.Contains(card.Message(), "60 văn bản") || !strings.Contains(card.Data()["display_type"].(string), BroadQueryClarificationType) {
		t.Fatalf("message %q", card.Message())
	}
	// the gate asks once per topic and session
	MarkBroadQueryAsked(context.Background(), "s-wide", card)
	if BroadQueryClarification(context.Background(), cat, "s-wide", BroadQueryInput{Query: "đất đai"}) != nil {
		t.Fatal("asked again for the same topic in the same session")
	}
	if BroadQueryClarification(context.Background(), cat, "s-other", BroadQueryInput{Query: "đất đai"}) == nil {
		t.Fatal("another session must be asked")
	}
}

func TestBroadQueryLeavesAnchoredAndNarrowQuestionsAlone(t *testing.T) {
	cat := &fakeTitles{n: 60}
	for _, q := range []string{
		"Nghị định 53/2020 về đất đai", // a number
		"đất đai năm 2024",             // a year
		"Điều 5 luật đất đai",          // a section
		"tất cả văn bản về đất đai",    // everything
		"đất đai (tìm chung)",          // the search-anyway answer
		"thủ tục cấp giấy chứng nhận quyền sử dụng đất cho hộ gia đình", // long topic
		"",
	} {
		if card := BroadQueryClarification(context.Background(), cat, "s1", BroadQueryInput{Query: q}); card != nil {
			t.Errorf("%q asked: %+v", q, card)
		}
	}
	few := &fakeTitles{n: 5}
	if card := BroadQueryClarification(context.Background(), few, "s1", BroadQueryInput{Query: "đất đai"}); card != nil {
		t.Fatalf("5 matches is not broad: %+v", card)
	}
	if card := BroadQueryClarification(context.Background(), cat, "s1", BroadQueryInput{Query: "đất đai", Disabled: true}); card != nil {
		t.Fatal("the setting turns the gate off")
	}
}

// A question about a person in the chat, or one whose leftover topic is only
// function words, names nothing in the corpus: no card.
func TestBroadQueryLeavesPersonalAndEmptyTopicsAlone(t *testing.T) {
	cat := &fakeTitles{n: 400}
	for _, q := range []string{
		"Bạn có thông tin gì về tôi nào",
		"bạn biết gì về tôi?",
		"thông tin của tôi",
		"bạn nhớ gì về tôi",
		"bạn lưu gì về tôi",
		"to\u0302i la\u0300 ai", // NFD input
		"cho tôi biết bạn có thông tin gì về tôi",
		"tôi là ai",
		"bạn là ai vậy",
		"có không",
		"là gì vậy",
	} {
		if card := BroadQueryClarification(context.Background(), cat, "s-personal", BroadQueryInput{Query: q}); card != nil {
			t.Errorf("%q asked about %q", q, card.Topic)
		}
	}
	freshSessionStores(t)
	for _, q := range []string{"quy định về thuế", "có những quy định gì về đất đai", "thông tin về cán bộ",
		"quy định về tổ chức", "văn bản về minh bạch tài chính", "quyết định của ban chỉ huy", "quy định về bán hàng",
		"quy định về đô thị", "luật dầu khí", "quy định về Đảng", "quy chế làm việc"} {
		if card := BroadQueryClarification(context.Background(), cat, "s-topic-"+q, BroadQueryInput{Query: q}); card == nil {
			t.Errorf("%q must still be asked about", q)
		}
	}
}

// replyJudge answers every labeling call with reply (or err) and counts them.
type replyJudge struct {
	reply string
	err   error
	calls int
}

func (j *replyJudge) Complete(context.Context, []docformat.Message) (string, error) {
	j.calls++
	return j.reply, j.err
}

func TestBroadQueryJudgeTakesTheCardAway(t *testing.T) {
	cat := &fakeTitles{n: 60}
	for name, j := range map[string]*replyJudge{
		"not broad":  {reply: `{"broad": false, "topic": ""}`},
		"error":      {err: fmt.Errorf("timeout")},
		"unreadable": {reply: `maybe`},
		"no field":   {reply: `{"topic": "đất đai"}`},
	} {
		if card := BroadQueryClarification(context.Background(), cat, "s-judge-"+name, BroadQueryInput{Query: "luật đất đai", Judge: j}); card != nil {
			t.Errorf("%s: card shown", name)
		}
		if j.calls != 1 {
			t.Errorf("%s: %d judge calls", name, j.calls)
		}
	}
}

func TestBroadQueryJudgeIsNotAskedWhenTheRulesShowNoCard(t *testing.T) {
	j := &replyJudge{reply: `{"broad": true, "topic": "đất đai"}`}
	if card := BroadQueryClarification(context.Background(), &fakeTitles{n: 5}, "s-few", BroadQueryInput{Query: "luật đất đai", Judge: j}); card != nil {
		t.Fatal("the judge cannot add a card the catalogue does not back")
	}
	if j.calls != 0 {
		t.Fatalf("%d judge calls below the match threshold", j.calls)
	}
}

func TestBroadQueryJudgeNamesTheSubject(t *testing.T) {
	cat := &fakeTitles{n: 60}
	// the rules strip "hội" (folded "hoi", a request word): the judge's
	// subject replaces the cut one and the titles are searched for it
	j := &replyJudge{reply: `{"broad": true, "topic": "bảo hiểm xã hội"}`}
	card := BroadQueryClarification(context.Background(), cat, "s-subject", BroadQueryInput{Query: "bảo hiểm xã hội", Judge: j})
	if card == nil || card.Topic != "bảo hiểm xã hội" {
		t.Fatalf("card %+v", card)
	}
	if strings.Join(cat.words, " ") != "bảo hiểm xã hội" {
		t.Fatalf("searched %q", cat.words)
	}
	// a subject that is not in the message is ignored: the rules' topic stays
	cat = &fakeTitles{n: 60}
	j = &replyJudge{reply: `{"broad": true, "topic": "thuế"}`}
	card = BroadQueryClarification(context.Background(), cat, "s-invented", BroadQueryInput{Query: "quy định về đất đai", Judge: j})
	if card == nil || card.Topic != "đất đai" {
		t.Fatalf("card %+v", card)
	}
	// "luật đất đai" from the judge loses its document type like the rules'
	j = &replyJudge{reply: `{"broad": true, "topic": "luật đất đai"}`}
	if card = BroadQueryClarification(context.Background(), &fakeTitles{n: 60}, "s-type", BroadQueryInput{Query: "luật đất đai", Judge: j}); card == nil || card.Topic != "đất đai" {
		t.Fatalf("card %+v", card)
	}
}

func TestBroadQueryJudgedSubjectNeedsEnoughTitles(t *testing.T) {
	cat := &shrinkingTitles{first: 60, then: 3}
	j := &replyJudge{reply: `{"broad": true, "topic": "bảo hiểm xã hội"}`}
	if card := BroadQueryClarification(context.Background(), cat, "s-shrink", BroadQueryInput{Query: "bảo hiểm xã hội", Judge: j}); card != nil {
		t.Fatalf("card over %d titles", card.Total)
	}
}

// shrinkingTitles matches first documents on the first search, then then.
type shrinkingTitles struct {
	first, then, calls int
}

func (s *shrinkingTitles) SearchTitles(ctx context.Context, words []string, limit int) ([]*types.LegalDocument, error) {
	n := s.first
	if s.calls > 0 {
		n = s.then
	}
	s.calls++
	return (&fakeTitles{n: n}).SearchTitles(ctx, words, limit)
}
