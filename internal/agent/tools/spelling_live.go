package tools

import (
	"context"
	"strings"
)

// The document assistant reviews spelling while the user types: the editor
// sends the paragraphs that are new or changed since its last review (and
// every paragraph once, when a document is first opened), and gets back the
// red underlines to apply. A paragraph the user edited first loses its old
// red underlines (unmark), so a corrected word does not stay marked. Each
// paragraph's review is cached by its text, so sending one again is cheap.

// LiveParagraph is one paragraph of the open editor to review.
type LiveParagraph struct {
	Text string `json:"text"`
	// Occurrence is the 1-based instance among the paragraphs with the
	// same (normalised) text, for the editor's anchor.
	Occurrence int `json:"occurrence,omitempty"`
	// Edited: the user changed the paragraph since its last review, so its
	// old red underlines are cleared before the new ones are applied.
	Edited bool `json:"edited,omitempty"`
}

// Bounds of one live review request.
const (
	LiveSpellMaxParagraphs = 60
	LiveSpellMaxRunes      = 4000
)

// LiveMarks reviews paras and returns the editor ops: per paragraph, an
// unmark when it was edited, then a red underline per mistake. ok is false
// when the model answered none of the paragraphs that needed it.
func (t *CheckSpellingTool) LiveMarks(ctx context.Context, paras []LiveParagraph) (ops []DocumentOp, found int, ok bool) {
	byPara := make([][]SpellingFinding, len(paras))
	var scoped []spellPara
	for i, p := range paras {
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		if cached, hit := spellPasses.get(livePassKey(t, p.Text)); hit {
			byPara[i] = cached
			continue
		}
		scoped = append(scoped, spellPara{Index: i, Text: p.Text})
	}
	ok = true
	if len(scoped) > 0 {
		findings, batches, failed := t.review(ctx, scoped)
		if batches > 0 && failed == batches && len(findings) == 0 {
			ok = false
		}
		fresh := make(map[int][]SpellingFinding, len(scoped))
		for _, f := range findings {
			fresh[f.Paragraph] = append(fresh[f.Paragraph], f)
		}
		for _, sp := range scoped {
			got := fresh[sp.Index]
			byPara[sp.Index] = got
			// a complete review only: a failed batch may have hidden mistakes
			if failed == 0 {
				cached := make([]SpellingFinding, len(got))
				copy(cached, got)
				for k := range cached {
					cached[k].Paragraph = 0
				}
				spellPasses.put(livePassKey(t, sp.Text), cached)
			}
		}
	}
	for i, p := range paras {
		anchor := &OpAnchor{Text: p.Text}
		if p.Occurrence > 1 {
			anchor.Occurrence = p.Occurrence
		}
		if p.Edited && strings.TrimSpace(p.Text) != "" {
			ops = append(ops, DocumentOp{Op: OpUnmark, Anchor: anchor})
		}
		for _, f := range byPara[i] {
			if f.raw == "" {
				continue
			}
			found++
			op := DocumentOp{Op: OpMark, Anchor: anchor, Text: f.raw, Style: "underline"}
			if f.occurrence > 1 {
				op.TextOccurrence = f.occurrence
			}
			ops = append(ops, op)
		}
	}
	return ops, found, ok
}

// livePassKey caches one paragraph's review by its text (as paragraph 0).
func livePassKey(t *CheckSpellingTool, text string) string {
	return "live|" + spellPassKey(t, []spellPara{{Index: 0, Text: text}})
}
