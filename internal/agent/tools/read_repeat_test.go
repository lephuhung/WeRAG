package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestIsRepeatReadQuery(t *testing.T) {
	first := repeatQueryTokens("điều kiện năng lực tổ chức thi công xây dựng hạng III")
	cases := []struct {
		query string
		want  bool
	}{
		{"điều kiện năng lực tổ chức hạng III thi công xây dựng", true}, // reordered
		{"Điều kiện năng lực, tổ chức thi công xây dựng hạng III!", true},
		{"điều kiện năng lực tổ chức thi công xây dựng hạng II", false},          // another class
		{"điều kiện năng lực tổ chức thi công xây dựng hạng III nhân sự", false}, // narrower
		{"điều kiện năng lực tổ chức thi công xây dựng", false},                  // broader retry
		{"Điều 18", false},
	}
	for _, c := range cases {
		if got := isRepeatReadQuery(first, repeatQueryTokens(c.query)); got != c.want {
			t.Errorf("isRepeatReadQuery(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

func readQueryArgs(t *testing.T, id, query string, regex bool) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": id, "query": query, "regex": regex})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReadDocumentRepeatGuard(t *testing.T) {
	tool, _ := newReadDocumentFixture(12)

	res, err := tool.Execute(turnCtx("msg-1", 2), readQueryArgs(t, "doc-1", "psionic engine", false))
	if err != nil || !res.Success || res.Data["match_count"] != 2 {
		t.Fatalf("first query: %+v %v", res, err)
	}
	// same round: parallel calls are never compared
	res, _ = tool.Execute(turnCtx("msg-1", 2), readQueryArgs(t, "doc-1", "engine psionic", false))
	if _, repeat := res.Data["repeat_of_round"]; repeat {
		t.Fatal("a call of the same round must run")
	}
	// a later round with the same words is answered without reading
	res, err = tool.Execute(turnCtx("msg-1", 3), readQueryArgs(t, "doc-1", "Engine, psionic", false))
	if err != nil || !res.Success || res.Data["repeat_of_round"] != 2 ||
		!strings.Contains(res.Output, "repeats the read_document query of round 2") ||
		!strings.Contains(res.Output, "2 matching chunk(s)") {
		t.Fatalf("repeat: %+v %v", res, err)
	}
	// a regex is compared as written: punctuation is part of it
	tool.queries.record("msg-1", 2, "doc-1", true, `khoản \d+`, 1)
	res, _ = tool.Execute(turnCtx("msg-1", 3), readQueryArgs(t, "doc-1", `khoản \D+`, true))
	if _, repeat := res.Data["repeat_of_round"]; repeat {
		t.Fatal("another regex must run")
	}
	res, _ = tool.Execute(turnCtx("msg-1", 3), readQueryArgs(t, "doc-1", ` khoản \d+ `, true))
	if res.Data["repeat_of_round"] != 2 {
		t.Fatalf("the same regex is a repeat: %+v", res.Data)
	}
	// narrowing with more words, a regex, a new turn or no agent turn all run
	for name, run := range map[string]func() map[string]any{
		"narrower": func() map[string]any {
			r, _ := tool.Execute(turnCtx("msg-1", 4), readQueryArgs(t, "doc-1", "psionic engine is here", false))
			return r.Data
		},
		"regex": func() map[string]any {
			r, _ := tool.Execute(turnCtx("msg-1", 4), readQueryArgs(t, "doc-1", "psionic engine", true))
			return r.Data
		},
		"next turn": func() map[string]any {
			r, _ := tool.Execute(turnCtx("msg-2", 3), readQueryArgs(t, "doc-1", "psionic engine", false))
			return r.Data
		},
		"mcp": func() map[string]any {
			r, _ := tool.Execute(context.Background(), readQueryArgs(t, "doc-1", "psionic engine", false))
			return r.Data
		},
	} {
		if data := run(); data["repeat_of_round"] != nil || data["match_count"] == nil {
			t.Errorf("%s: must run, data = %+v", name, data)
		}
	}
}
