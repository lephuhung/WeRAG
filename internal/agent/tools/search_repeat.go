package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// searchRepeatJaccard is the token-set similarity from which two queries of
// one turn count as the same search.
const searchRepeatJaccard = 0.8

// searchRepeatMaxAddedTokens is how many tokens a query may add to an earlier
// one of the turn and still count as its repeat: "... phông chữ Times New
// Roman" then "... phông chữ Times New Roman cỡ chữ 13" retrieves the same
// chunks again.
const searchRepeatMaxAddedTokens = 2

// searchRepeatKey is what besides the query two calls must share to repeat
// each other: the mode and the abbreviation meaning selection, which changes
// the effective query.
func searchRepeatKey(mode string, meaningIDs map[string]string, literal bool) string {
	key := mode
	if literal {
		key += "|literal"
	}
	forms := make([]string, 0, len(meaningIDs))
	for form := range meaningIDs {
		forms = append(forms, form)
	}
	sort.Strings(forms)
	for _, form := range forms {
		key += "|" + form + "=" + meaningIDs[form]
	}
	return key
}

// searchHistoryEntry is one completed search_knowledge call of the turn.
type searchHistoryEntry struct {
	key    string // searchRepeatKey
	tokens map[string]struct{}
	round  int
	count  int
}

// searchHistory holds the turn's completed searches. The tool instance is
// built per engine, and the engine per turn (CreateAgentEngine), so the
// history already lives for one turn; it is also keyed by the assistant
// message id from the ToolExecContext and starts over when that changes, so
// a tool instance reused across turns never carries one turn's searches into
// the next.
type searchHistory struct {
	mu        sync.Mutex
	messageID string
	entries   []searchHistoryEntry
}

// normalizeSearchQuery lower-cases the query, turns punctuation and symbols
// into spaces and collapses whitespace.
func normalizeSearchQuery(q string) string {
	q = strings.ToLower(q)
	q = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return ' '
		}
		return r
	}, q)
	return strings.Join(strings.Fields(q), " ")
}

// repeatQueryTokens is the token set of a normalized query.
func repeatQueryTokens(q string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, tok := range strings.Fields(normalizeSearchQuery(q)) {
		set[tok] = struct{}{}
	}
	return set
}

// isRepeatQuery reports whether cur searches for what prev already did:
// equal after normalization, token-set Jaccard >= searchRepeatJaccard, or
// prev plus at most searchRepeatMaxAddedTokens new tokens.
func isRepeatQuery(prev, cur map[string]struct{}) bool {
	if len(prev) == 0 || len(cur) == 0 {
		return false
	}
	inter := 0
	for tok := range cur {
		if _, ok := prev[tok]; ok {
			inter++
		}
	}
	union := len(prev) + len(cur) - inter
	if inter == union {
		return true
	}
	if float64(inter)/float64(union) >= searchRepeatJaccard {
		return true
	}
	// cur is a superset of prev adding a few tokens
	return inter == len(prev) && len(cur)-len(prev) <= searchRepeatMaxAddedTokens
}

// findRepeat returns the earlier call cur repeats, from a round before the
// current one. Calls of the same round run in parallel and are never
// compared with each other.
func (h *searchHistory) findRepeat(messageID string, round int, key, query string) (searchHistoryEntry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.messageID != messageID {
		return searchHistoryEntry{}, false
	}
	tokens := repeatQueryTokens(query)
	for _, e := range h.entries {
		if e.round < round && e.key == key && isRepeatQuery(e.tokens, tokens) {
			return e, true
		}
	}
	return searchHistoryEntry{}, false
}

// record adds a completed call; a new message id starts a new history.
func (h *searchHistory) record(messageID string, round int, key, query string, count int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.messageID != messageID {
		h.messageID = messageID
		h.entries = nil
	}
	h.entries = append(h.entries, searchHistoryEntry{
		key: key, tokens: repeatQueryTokens(query), round: round, count: count,
	})
}

// searchTurnPosition returns the turn and round of an agent tool call; ok is
// false outside an agent turn (MCP endpoint, tests without an exec context),
// where the repeat guard does not apply.
func searchTurnPosition(ctx context.Context) (messageID string, round int, ok bool) {
	meta, found := ToolExecFromContext(ctx)
	if !found || meta == nil || meta.Round <= 0 {
		return "", 0, false
	}
	return meta.AssistantMessageID, meta.Round, true
}

// searchRepeatOutput is the model text for a repeated search.
func searchRepeatOutput(query, mode string, prev searchHistoryEntry) string {
	return fmt.Sprintf("This query repeats the search_knowledge call of round %d (mode=%s, %d result(s)); "+
		"running it again would return the same chunks, so it was not run. The results of round %d still "+
		"apply. Either answer now with the evidence you already have, saying what was not found, or try a "+
		"clearly different query (other terms, another mode) or read_document on a document you already found. "+
		"Query: %q.", prev.round, mode, prev.count, prev.round, query)
}
