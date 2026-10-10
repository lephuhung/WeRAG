package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Tencent/WeKnora/internal/docformat"
)

// fingerprintKeepParas is how many non-empty paragraphs at each end of the
// document count with their text: the NĐ30 components (quốc hiệu, cơ quan,
// số ký hiệu, trích yếu, chữ ký, nơi nhận) live there, as the labelling
// step assumes too (keepHead/keepTail in docformat).
const fingerprintKeepParas = 45

// formatFingerprint identifies what the format check depends on: page setup,
// every paragraph's formatting and position, and the text of the paragraphs
// at both ends. Rewording the body leaves it unchanged, so such an edit does
// not trigger a new evaluation; changing a font, a margin, the signature or
// the Nơi nhận does.
func formatFingerprint(content []byte) string {
	l := docformat.InspectDocx(content)
	nonEmpty := 0
	for _, p := range l.Paragraphs {
		if strings.TrimSpace(p.Text) != "" {
			nonEmpty++
		}
	}
	paras := make([]docformat.Para, 0, len(l.Paragraphs))
	ord := 0
	for _, p := range l.Paragraphs {
		cp := *p
		cp.Runs = nil
		if strings.TrimSpace(p.Text) != "" {
			if ord >= fingerprintKeepParas && ord < nonEmpty-fingerprintKeepParas {
				cp.Text, cp.LeftText, cp.RightText = "", "", ""
			}
			ord++
		}
		paras = append(paras, cp)
	}
	raw, _ := json.Marshal(struct {
		Sections   []*docformat.Section
		Paragraphs []docformat.Para
		Headers    bool
		Footers    bool
		Errors     []string
	}{l.Sections, paras, l.HasHeaders, l.HasFooters, l.Errors})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// formatCheckIdentity keys a cached format evaluation by what the check
// reads: page setup, every paragraph's formatting, position and full text,
// headers and footers. It leaves out what the check never reads — the file's
// save metadata (dates, rsids: Word changes them on every export) and run
// formatting and paragraph underline (the assistant's red-underline marks;
// no NĐ30 rule checks underline) — so a repeated check of an unchanged
// document is answered from the cache instead of a new minute-long
// evaluation. A file that cannot be read keys by its bytes.
func formatCheckIdentity(content []byte) string {
	l := docformat.InspectDocx(content)
	if len(l.Paragraphs) == 0 {
		sum := sha256.Sum256(content)
		return "bytes:" + hex.EncodeToString(sum[:])
	}
	paras := make([]docformat.Para, 0, len(l.Paragraphs))
	for _, p := range l.Paragraphs {
		cp := *p
		cp.Runs = nil
		cp.Underline = nil
		paras = append(paras, cp)
	}
	raw, _ := json.Marshal(struct {
		Sections   []*docformat.Section
		Paragraphs []docformat.Para
		Headers    bool
		Footers    bool
		Errors     []string
	}{l.Sections, paras, l.HasHeaders, l.HasFooters, l.Errors})
	sum := sha256.Sum256(raw)
	return "fmt:" + hex.EncodeToString(sum[:])
}
