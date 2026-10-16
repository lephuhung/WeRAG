package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
