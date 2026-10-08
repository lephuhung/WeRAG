package tools

import (
	"encoding/json"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/google/uuid"
)

// Edit-plan contract between the document tools and the ONLYOFFICE editor
// plugin. A tool returns ToolResult.Data{document_ops, ops_batch_id,
// snapshot_seq}; the plugin applies the ops in order inside the editor, so
// the user sees them as ordinary edits and undoes them with Ctrl+Z.
//
// A paragraph is addressed by an anchor: its full visible text (as docxedit
// Paragraphs returns it, whitespace collapsed) and the 1-based occurrence of
// that text among the paragraphs before it — or {"atStart": true}. Anchors
// are computed against the document as it will be when the op runs: the
// ops before it in the same plan (replaced text, inserted paragraphs) are
// taken into account.

// Op names.
const (
	OpReplaceText      = "replaceText"
	OpReplaceParagraph = "replaceParagraph"
	OpInsertAfter      = "insertAfter"
	OpMark             = "mark"
	OpFormatParagraph  = "formatParagraph"
	OpPageSetup        = "pageSetup"
)

// OpAnchor locates a paragraph for the editor plugin.
type OpAnchor struct {
	Text       string `json:"text"`
	Occurrence int    `json:"occurrence,omitempty"`
	AtStart    bool   `json:"atStart,omitempty"`
}

// MarshalJSON writes {"atStart":true} alone for the start anchor.
func (a OpAnchor) MarshalJSON() ([]byte, error) {
	if a.AtStart {
		return []byte(`{"atStart":true}`), nil
	}
	type plain OpAnchor
	return json.Marshal(plain(a))
}

// DocumentOp is one edit the editor plugin applies.
type DocumentOp struct {
	Op     string    `json:"op"`
	Anchor *OpAnchor `json:"anchor,omitempty"`
	// replaceText / replaceParagraph
	Old *string `json:"old,omitempty"`
	New *string `json:"new,omitempty"`
	// insertAfter: the new paragraph's text; mark: the substring to mark
	// (omitted: the whole paragraph)
	Text string `json:"text,omitempty"`
	// insertAfter: paragraph whose formatting the new one copies
	Like *OpAnchor `json:"like,omitempty"`
	// insertAfter / formatParagraph: left | center | right | both
	Alignment string   `json:"alignment,omitempty"`
	Bold      *bool    `json:"bold,omitempty"`
	Italic    *bool    `json:"italic,omitempty"`
	Font      string   `json:"font,omitempty"`
	SizePt    *float64 `json:"sizePt,omitempty"`
	// mark: underline | highlight | color
	Style string `json:"style,omitempty"`
	// mark: which occurrence of text inside the paragraph to mark (1-based);
	// omitted means the first
	TextOccurrence int `json:"textOccurrence,omitempty"`
	// pageSetup
	MarginsMm map[string]float64 `json:"marginsMm,omitempty"`
	A4        bool               `json:"a4,omitempty"`
}

// opsData is the ToolResult.Data part of an edit plan.
func opsData(ops []DocumentOp, snapshotSeq int) map[string]interface{} {
	if ops == nil {
		ops = []DocumentOp{}
	}
	return map[string]interface{}{
		"document_ops": ops,
		"ops_batch_id": uuid.NewString(),
		"snapshot_seq": snapshotSeq,
	}
}

// editorAppliedNote ends the Output of a tool that returned ops.
const editorAppliedNote = "Các thay đổi trên sẽ được áp dụng trong trình soạn thảo; Ctrl+Z để hoàn tác."

// normalizeDocText is the one normaliser for anchors and the selection
// overlap: zero-width characters (U+200B–U+200D, U+FEFF) dropped, no-break
// spaces made spaces, Latin letters canonically composed (NFC, as
// docxedit.FindParagraph compares) and whitespace runs collapsed. The editor
// plugin must normalise the same way when it resolves an anchor.
func normalizeDocText(s string) string {
	s = zeroWidthReplacer.Replace(s)
	return strings.Join(strings.Fields(docxedit.NFCLatin(s)), " ")
}

var zeroWidthReplacer = strings.NewReplacer(
	"\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "",
	"\u00a0", " ", "\u202f", " ",
)

// anchorText is the normalised paragraph text anchors compare.
func anchorText(s string) string { return normalizeDocText(s) }

// virtualDoc tracks the paragraph texts as the plugin will see them while
// it applies a plan, so each op's anchor is computed on the right state.
type virtualDoc struct {
	texts []string // raw visible text per current position
	ids   []int    // original paragraph index, or -2, -3, … for inserted ones
	next  int      // next id handed to an inserted paragraph
}

func newVirtualDoc(paras []docxedit.Paragraph) *virtualDoc {
	v := &virtualDoc{texts: make([]string, len(paras)), ids: make([]int, len(paras)), next: -2}
	for i, p := range paras {
		v.texts[i], v.ids[i] = p.Text, i
	}
	return v
}

// pos is the current position of the paragraph with id (an original index
// or an id insertAfter returned); -1 when unknown.
func (v *virtualDoc) pos(id int) int {
	for p, x := range v.ids {
		if x == id {
			return p
		}
	}
	return -1
}

// anchor addresses the paragraph at current position p (-1: the start).
func (v *virtualDoc) anchor(p int) *OpAnchor {
	if p < 0 {
		return &OpAnchor{AtStart: true}
	}
	t := anchorText(v.texts[p])
	n := 1
	for i := 0; i < p; i++ {
		if anchorText(v.texts[i]) == t {
			n++
		}
	}
	return &OpAnchor{Text: t, Occurrence: n}
}

// insertAfter records a paragraph inserted after position p (-1: at the
// start) and returns the new paragraph's id.
func (v *virtualDoc) insertAfter(p int, text string) int {
	at := p + 1
	id := v.next
	v.next--
	v.texts = append(v.texts[:at], append([]string{text}, v.texts[at:]...)...)
	v.ids = append(v.ids[:at], append([]int{id}, v.ids[at:]...)...)
	return id
}

// setText records a paragraph's new text.
func (v *virtualDoc) setText(p int, text string) { v.texts[p] = text }

// selectionOverlaps reports whether target (a passage the tool would edit)
// overlaps the user's selection: one contains the other, or they share a
// run of words long enough not to be a coincidence.
func selectionOverlaps(selection, target string) bool {
	s, t := foldForOverlap(selection), foldForOverlap(target)
	if s == "" || t == "" {
		return false
	}
	if strings.Contains(s, t) || strings.Contains(t, s) {
		return true
	}
	sw, tw := strings.Fields(s), strings.Fields(t)
	if len(sw) < 3 {
		// a one- or two-word selection must contain or be contained in the
		// edited text: sharing a word is a coincidence
		return false
	}
	need := 4 // shared words
	if n := min(len(sw), len(tw)); n < need {
		need = n
	}
	return longestCommonWordRun(sw, tw) >= need
}

func foldForOverlap(s string) string {
	return strings.ToLower(strings.Trim(normalizeDocText(s), " .,;:…\"“”'"))
}

// longestCommonWordRun is the length of the longest common run of words.
func longestCommonWordRun(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a)*len(b) > 4_000_000 { // ~8000 runes selection × long paragraph
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	best := 0
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > best {
					best = cur[j]
				}
			} else {
				cur[j] = 0
			}
		}
		prev, cur = cur, prev
	}
	return best
}
