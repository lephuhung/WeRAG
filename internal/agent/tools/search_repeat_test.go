package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const (
	repeatFirstQuery  = "Nghị định 30/2020/NĐ-CP Phụ lục I phông chữ Times New Roman"
	repeatSecondQuery = "Nghị định 30/2020/NĐ-CP Phụ lục I phông chữ Times New Roman cỡ chữ 13"
)

func turnCtx(messageID string, round int) context.Context {
	return WithToolExecContext(context.Background(), &ToolExecContext{AssistantMessageID: messageID, Round: round})
}

func searchArgs(t *testing.T, query, mode string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"query": query, "mode": mode, "limit": 20})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestIsRepeatQuery(t *testing.T) {
	first := repeatQueryTokens(repeatFirstQuery)
	cases := []struct {
		query string
		want  bool
	}{
		{repeatFirstQuery, true},
		{"  nghị định 30/2020/nđ-cp, PHỤ LỤC I: phông chữ Times New Roman!  ", true},
		{repeatSecondQuery, true}, // adds "cỡ" and "13"
		{repeatFirstQuery + " cỡ chữ 13 dòng kẻ", false},
		{"Thông tư 01/2011/TT-BNV thể thức văn bản hành chính", false},
	}
	for _, c := range cases {
		if got := isRepeatQuery(first, repeatQueryTokens(c.query)); got != c.want {
			t.Errorf("isRepeatQuery(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

// The second near-identical query of a turn is answered without retrieval;
// a different query or mode still goes on to the abbreviation gate and
// retrieval (which fail here: the tool has no services).
func TestSearchKnowledgeRepeatGuard(t *testing.T) {
	tool := &SearchKnowledgeTool{BaseTool: searchKnowledgeTool}
	tool.history.record("msg-1", 7, SearchModeHybrid, repeatFirstQuery, 15)

	res, err := tool.Execute(turnCtx("msg-1", 8), searchArgs(t, repeatSecondQuery, ""))
	if err != nil || res == nil || !res.Success {
		t.Fatalf("repeat should succeed without retrieval, got %+v, %v", res, err)
	}
	if res.Data["repeat_of_round"] != 7 || res.Data["query"] != repeatSecondQuery {
		t.Fatalf("repeat data = %+v", res.Data)
	}
	if _, hasRows := res.Data["results"]; hasRows {
		t.Fatal("a repeat carries no result rows")
	}
	for _, want := range []string{"round 7", "still apply", "different query", "read_document"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output lacks %q: %s", want, res.Output)
		}
	}

	notRun := func(name string, ctx context.Context, query, mode string) {
		t.Helper()
		res, _ := tool.Execute(ctx, searchArgs(t, query, mode))
		if res == nil || res.Success || res.Data["repeat_of_round"] != nil {
			t.Errorf("%s: should not be a repeat, got %+v", name, res)
		}
	}
	notRun("different query", turnCtx("msg-1", 8), "Thông tư 01/2011/TT-BNV thể thức văn bản", "")
	notRun("different mode", turnCtx("msg-1", 8), repeatSecondQuery, SearchModeKeyword)
	notRun("same round", turnCtx("msg-1", 7), repeatSecondQuery, "")
	notRun("next turn", turnCtx("msg-2", 1), repeatSecondQuery, "")
	notRun("outside a turn", context.Background(), repeatSecondQuery, "")

	res, _ = tool.Execute(turnCtx("msg-1", 8), json.RawMessage(
		`{"query":"`+repeatSecondQuery+`","abbreviation_meaning_ids":{"NĐ":"m-1"}}`))
	if res != nil && res.Data["repeat_of_round"] != nil {
		t.Error("another abbreviation meaning selection is not a repeat")
	}
}

func TestSearchHistoryStartsOverOnNewMessage(t *testing.T) {
	var h searchHistory
	h.record("msg-1", 1, SearchModeHybrid, repeatFirstQuery, 3)
	if _, ok := h.findRepeat("msg-1", 2, SearchModeHybrid, repeatFirstQuery); !ok {
		t.Fatal("same turn should repeat")
	}
	h.record("msg-2", 1, SearchModeHybrid, "khác", 0)
	if _, ok := h.findRepeat("msg-2", 2, SearchModeHybrid, repeatFirstQuery); ok {
		t.Fatal("a new turn must not see the previous turn's searches")
	}
	// an empty result is a repeat too
	if prev, ok := h.findRepeat("msg-2", 2, SearchModeHybrid, "khác"); !ok || prev.count != 0 {
		t.Fatalf("repeat of an empty result = %+v, %v", prev, ok)
	}
}
