package tools

import (
	"fmt"
	"strings"
	"sync"
)

// readQueryEntry is one completed read_document query call of the turn.
type readQueryEntry struct {
	docID   string
	regex   bool
	query   string // trimmed raw text, compared as is for regex queries
	tokens  map[string]struct{}
	round   int
	matches int
}

// readQueryHistory holds the turn's read_document query calls, keyed like
// searchHistory by the assistant message id so a reused tool instance
// starts over on the next turn.
//
// The pack eval of 2026-11-08 found the same in-document query asked round
// after round ("điều kiện năng lực tổ chức thi công xây dựng hạng III" in
// word orders that return the same 3 chunks; the same "Điều 82 … Điều 86"
// query in rounds 5 and 6).
type readQueryHistory struct {
	mu        sync.Mutex
	messageID string
	entries   []readQueryEntry
}

// isRepeatReadQuery reports whether two in-document queries are the same
// search: the same words in any order, case and punctuation aside. Any other
// change is a new search: every word must match, so a changed, added or
// dropped word changes the result ("hạng III" → "hạng II", or fewer words
// after a partial match).
func isRepeatReadQuery(prev, cur map[string]struct{}) bool {
	if len(prev) == 0 || len(prev) != len(cur) {
		return false
	}
	for tok := range cur {
		if _, ok := prev[tok]; !ok {
			return false
		}
	}
	return true
}

// find returns the earlier call of the turn cur repeats, from a round before
// the current one (calls of one round run in parallel).
func (h *readQueryHistory) find(messageID string, round int, docID string, regex bool, query string) (readQueryEntry, bool) {
	if h == nil {
		return readQueryEntry{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.messageID != messageID {
		return readQueryEntry{}, false
	}
	raw := strings.TrimSpace(query)
	tokens := repeatQueryTokens(query)
	for _, e := range h.entries {
		if e.round >= round || e.docID != docID || e.regex != regex {
			continue
		}
		if regex && e.query == raw || !regex && isRepeatReadQuery(e.tokens, tokens) {
			return e, true
		}
	}
	return readQueryEntry{}, false
}

// record adds a completed call; a new message id starts a new history.
func (h *readQueryHistory) record(messageID string, round int, docID string, regex bool, query string, matches int) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.messageID != messageID {
		h.messageID = messageID
		h.entries = nil
	}
	h.entries = append(h.entries, readQueryEntry{
		docID: docID, regex: regex, query: strings.TrimSpace(query), tokens: repeatQueryTokens(query),
		round: round, matches: matches,
	})
}

// readRepeatOutput is the model text for a repeated in-document query.
func readRepeatOutput(query string, prev readQueryEntry) string {
	return fmt.Sprintf("This query repeats the read_document query of round %d on the same document (%d matching "+
		"chunk(s)) with the same words, so it would return the same chunks and was not run. The result of round "+
		"%d still applies. Answer with what you have, read a chunk you already found with id=cN and context, or "+
		"search with clearly different words. Query: %q.", prev.round, prev.matches, prev.round, query)
}
