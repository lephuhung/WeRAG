package tools

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// The document assistant reviews spelling while the user types: the editor
// sends the paragraphs that are new or changed since its last review (and
// every paragraph once, when a document is first opened), and gets back the
// underlines to apply. A paragraph the user edited first loses its old
// underlines (unmark), so a corrected word does not stay marked. Each
// paragraph's review is cached by its text, so sending one again is cheap.

// LiveParagraph is one paragraph of the open editor to review.
type LiveParagraph struct {
	Text string `json:"text"`
	// Occurrence is the 1-based instance among the paragraphs with the
	// same (normalised) text, for the editor's anchor.
	Occurrence int `json:"occurrence,omitempty"`
	// Edited: the user changed the paragraph since its last review, so its
	// old underlines are cleared before the new ones are applied.
	Edited bool `json:"edited,omitempty"`
}

// Bounds of one live review request. A paragraph longer than the model's
// spellParagraphRunes is reviewed in parts (see liveParts).
const (
	LiveSpellMaxParagraphs = 60
	LiveSpellMaxRunes      = 8000
)

// liveSpellTimeout bounds one live review: the editor waits for it before
// it sends the next pause's paragraphs, and a paragraph the model did not
// answer in time is sent again later.
var liveSpellTimeout = 90 * time.Second

// livePartRunes is the size of one part of a long paragraph, below
// spellParagraphRunes so the model sees each part whole.
const livePartRunes = spellParagraphRunes - 200

// LiveMarks reviews paras and returns the editor ops: per paragraph, an
// unmark when it was edited, then an underline per mistake. missed are
// the positions (in paras) the model did not answer: they get no ops at
// all, so an edited one keeps its old underlines until it is reviewed.
func (t *CheckSpellingTool) LiveMarks(ctx context.Context, paras []LiveParagraph) (ops []DocumentOp, found int, missed []int) {
	ctx, cancel := context.WithTimeout(ctx, liveSpellTimeout)
	defer cancel()
	byPara := make([][]SpellingFinding, len(paras))
	var scoped []spellPara
	var owner []int // scoped position → paras position
	for i, p := range paras {
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		if cached, hit := spellPasses.get(livePassKey(t, p.Text)); hit {
			byPara[i] = cached
			continue
		}
		for _, part := range liveParts(p.Text) {
			// Index is the scoped position: the parts of one paragraph are
			// told apart, and findings map back through owner
			scoped = append(scoped, spellPara{Index: len(scoped), Text: p.Text, Part: part})
			owner = append(owner, i)
		}
	}
	failedPara := map[int]bool{}
	if len(scoped) > 0 {
		findings, _, _, lost := t.reviewTracked(ctx, scoped)
		for _, k := range lost {
			failedPara[owner[k]] = true
		}
		fresh := map[int][]SpellingFinding{}
		for _, f := range findings {
			fresh[owner[f.Paragraph]] = append(fresh[owner[f.Paragraph]], f)
		}
		for k, sp := range scoped {
			i := owner[k]
			if k > 0 && owner[k-1] == i {
				continue // the paragraph's first part did it
			}
			if failedPara[i] {
				continue
			}
			got := fresh[i]
			byPara[i] = got
			cached := make([]SpellingFinding, len(got))
			copy(cached, got)
			for c := range cached {
				cached[c].Paragraph = 0
			}
			spellPasses.put(livePassKey(t, sp.Text), cached)
		}
	}
	// the user's "not a mistake" verdicts, one lookup for the request
	var flagged []string
	for i, p := range paras {
		if len(byPara[i]) > 0 {
			flagged = append(flagged, p.Text)
		}
	}
	dismissed := t.dismissedIn(ctx, flagged)
	for i, p := range paras {
		if failedPara[i] {
			missed = append(missed, i)
			continue
		}
		if len(dismissed) > 0 && len(byPara[i]) > 0 {
			hash, kept := SpellingParagraphHash(p.Text), byPara[i][:0:0]
			for _, f := range byPara[i] {
				if !dismissed[spellingDismissKey(hash, f.Wrong)] {
					kept = append(kept, f)
				}
			}
			byPara[i] = kept
		}
		anchor := &OpAnchor{Text: p.Text}
		if p.Occurrence > 1 {
			anchor.Occurrence = p.Occurrence
		}
		if p.Edited && strings.TrimSpace(p.Text) != "" {
			ops = append(ops, DocumentOp{Op: OpUnmark, Anchor: anchor})
		}
		for _, f := range byPara[i] {
			if m := f.markOps(anchor); len(m) > 0 {
				found++
				ops = append(ops, m...)
			}
		}
	}
	return ops, found, missed
}

// liveParts splits a paragraph longer than livePartRunes into parts of at
// most that size, cut after a sentence end (". ", "; ", "! ", "? ", ": ")
// when there is one in the part's second half, else at a space. A shorter
// paragraph is one part, "" (the whole text).
func liveParts(text string) []string {
	if utf8.RuneCountInString(text) <= livePartRunes {
		return []string{""}
	}
	var parts []string
	rest := []rune(text)
	for len(rest) > livePartRunes {
		cut := 0
		for k := livePartRunes - 1; k >= livePartRunes/2 && cut == 0; k-- {
			if rest[k] == ' ' && strings.ContainsRune(".;!?:", rest[k-1]) {
				cut = k + 1
			}
		}
		for k := livePartRunes - 1; k > 0 && cut == 0; k-- {
			if rest[k] == ' ' {
				cut = k + 1
			}
		}
		if cut == 0 {
			cut = livePartRunes
		}
		if part := strings.TrimSpace(string(rest[:cut])); part != "" {
			parts = append(parts, part)
		}
		rest = rest[cut:]
	}
	if part := strings.TrimSpace(string(rest)); part != "" {
		parts = append(parts, part)
	}
	return parts
}

// livePassKey caches one paragraph's review by its text (as paragraph 0).
func livePassKey(t *CheckSpellingTool, text string) string {
	return "live|" + spellPassKey(t, []spellPara{{Index: 0, Text: text}})
}
