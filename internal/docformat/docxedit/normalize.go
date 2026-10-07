package docxedit

import (
	"sort"
	"strings"
)

// nfcLatin canonically composes Latin letters with combining marks (the
// subset of NFC that matters for Vietnamese: base + horn/breve/circumflex +
// tone mark, in any canonical order). Characters outside the table pass
// through unchanged.
func nfcLatin(s string) string {
	hasMark := false
	for _, r := range s {
		if _, ok := combiningClass[r]; ok {
			hasMark = true
			break
		}
	}
	if !hasMark {
		return s
	}
	rs := []rune(s)
	// canonical reordering of each run of combining marks
	for i := 0; i < len(rs); {
		if _, ok := combiningClass[rs[i]]; !ok {
			i++
			continue
		}
		j := i
		for j < len(rs) {
			if _, ok := combiningClass[rs[j]]; !ok {
				break
			}
			j++
		}
		marks := rs[i:j]
		sort.SliceStable(marks, func(a, b int) bool {
			return combiningClass[marks[a]] < combiningClass[marks[b]]
		})
		i = j
	}
	out := make([]rune, 0, len(rs))
	starter := -1 // index in out of the last starter
	lastCC := uint8(0)
	for _, r := range rs {
		cc, isMark := combiningClass[r]
		if isMark && starter >= 0 {
			// blocked when an uncomposed mark of the same or higher class
			// sits between the starter and r
			if lastCC == 0 || lastCC < cc {
				if c, ok := latinCompose[[2]rune{out[starter], r}]; ok {
					out[starter] = c
					continue
				}
			}
			out = append(out, r)
			lastCC = cc
			continue
		}
		out = append(out, r)
		if isMark {
			lastCC = cc
			continue
		}
		starter = len(out) - 1
		lastCC = 0
	}
	return string(out)
}

// normalizeText is the FindParagraph key: composed, whitespace collapsed,
// lower-cased (diacritics kept).
func normalizeText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(nfcLatin(s)), " "))
}

// NFCLatin is the canonical composition FindParagraph uses (see nfcLatin),
// exported so callers can compare paragraph texts the same way.
func NFCLatin(s string) string { return nfcLatin(s) }
