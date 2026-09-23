package searchutil

import (
	"crypto/md5"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
)

// BuildContentSignature creates a normalized MD5 signature for content to detect duplicates.
// It normalizes the content by lowercasing, trimming whitespace, and collapsing multiple spaces.
func BuildContentSignature(content string) string {
	c := strings.ToLower(strings.TrimSpace(content))
	if c == "" {
		return ""
	}
	// Normalize whitespace
	c = strings.Join(strings.Fields(c), " ")
	// Use MD5 hash of full content
	hash := md5.Sum([]byte(c))
	return hex.EncodeToString(hash[:])
}

// containsChinese checks whether text contains any CJK unified ideographs.
func containsChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// TokenizeSimple tokenizes text into a set of unique tokens.
// For text containing Chinese characters, it uses jieba segmentation for accurate word boundaries.
// For pure non-Chinese text, it falls back to whitespace-based splitting.
// Returns a map where keys are lowercase tokens with rune length > 1.
func TokenizeSimple(text string) map[string]struct{} {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}

	var words []string
	if containsChinese(text) {
		// Use jieba for Chinese text segmentation (search mode for finer granularity)
		words = types.Jieba.CutForSearch(text, true)
	} else {
		words = strings.Fields(text)
	}

	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		// Filter out single-rune tokens and pure punctuation/whitespace
		if len([]rune(w)) > 1 && !isAllPunct(w) {
			set[w] = struct{}{}
		}
	}
	return set
}

// isAllPunct checks if a string consists entirely of punctuation or whitespace.
func isAllPunct(s string) bool {
	for _, r := range s {
		if !unicode.IsPunct(r) && !unicode.IsSpace(r) && !unicode.IsSymbol(r) {
			return false
		}
	}
	return true
}

// Jaccard calculates Jaccard similarity between two token sets.
// Returns a value between 0 and 1, where 1 means identical sets.
func Jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}

	// small set drives large set
	if len(a) > len(b) {
		return Jaccard(b, a)
	}

	// Calculate intersection
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}

	// Calculate union
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}

	return float64(inter) / float64(union)
}

// NormalizeContent returns a lowercased, whitespace-collapsed version of s
// suitable for containment and overlap checks.
func NormalizeContent(s string) string {
	c := strings.ToLower(strings.TrimSpace(s))
	if c == "" {
		return ""
	}
	return strings.Join(strings.Fields(c), " ")
}

// IsContentContained reports whether the normalized form of short is a
// substring of the normalized form of long. Both inputs must already be
// normalized via NormalizeContent.
func IsContentContained(normalizedShort, normalizedLong string) bool {
	if normalizedShort == "" || normalizedLong == "" {
		return false
	}
	if len(normalizedShort) > len(normalizedLong) {
		return false
	}
	return strings.Contains(normalizedLong, normalizedShort)
}

// ContentOverlapRatio estimates how much of a's content overlaps with b by
// comparing their token sets (Jaccard-like but using overlap coefficient:
// |intersection| / |smaller set|). Both inputs should be raw content strings.
// Returns a value in [0, 1] where 1 means the smaller set is fully contained
// in the larger set.
func ContentOverlapRatio(a, b string) float64 {
	tokA := TokenizeSimple(a)
	tokB := TokenizeSimple(b)
	if len(tokA) == 0 || len(tokB) == 0 {
		return 0
	}

	small, large := tokA, tokB
	if len(tokA) > len(tokB) {
		small, large = tokB, tokA
	}

	inter := 0
	for k := range small {
		if _, ok := large[k]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(small))
}

// degenerateItemBoundary splits OCR output into candidate repeat items:
// a numbered-list marker ("12. " / "12) ") attached to preceding whitespace
// (or at the very start), or a run of newlines. Degenerate VLM loops usually
// take the numbered-list form ("1. X. 2. X."), while bare newlines cover
// plain repeated lines. The numbered alternative is tried first so a marker
// following a newline is consumed together with that newline.
var degenerateItemBoundary = regexp.MustCompile(`(?:^|\s)\d{1,4}[.)]\s+|\n+`)

// degenerateLeadRe strips a leading bullet or residual enumeration from a
// segment so "12. X" and "13. X" normalize to the same key — a marker can
// survive inside a segment when it follows a blank line ("\n\n12. X").
var degenerateLeadRe = regexp.MustCompile(`^\s*(?:[-*•·‣◦]+\s*|\d{1,4}[.)]\s*)`)

const (
	// degenerateMinRun is the consecutive identical-item count that marks a
	// tail as degenerate. Real documents almost never end with five truly
	// identical list items back-to-back.
	degenerateMinRun = 5
	// degenerateMinSegmentLen guards against truncating legitimate short
	// repeats (signature marks, "N/A" rows): only longer sentences count.
	degenerateMinSegmentLen = 10 // runes
)

func degenerateNorm(seg string) string {
	seg = degenerateLeadRe.ReplaceAllString(seg, "")
	seg = strings.ToLower(strings.Join(strings.Fields(seg), " "))
	return strings.TrimRight(seg, " .,;:!?…")
}

// CollapseDegenerateTail truncates a degenerate LLM output tail — a run of
// identical items (numbered-list entries or whole lines) repeated at the
// end of the text. VLM OCR occasionally loops a single sentence until the
// token limit; cutting at the first repeated item keeps whatever real
// content preceded it. Returns "" when the entire output is one repeated
// item.
func CollapseDegenerateTail(text string) string {
	bounds := degenerateItemBoundary.FindAllStringIndex(text, -1)

	// Segments are the spans between boundaries; each remembers the start
	// offset of the boundary in front of it so a cut removes the dangling
	// delimiter ("... 15. ") as well.
	type segment struct {
		norm       string
		boundStart int
	}
	segs := make([]segment, 0, len(bounds)+1)
	prevEnd, prevBound := 0, 0
	for _, b := range bounds {
		segs = append(segs, segment{degenerateNorm(text[prevEnd:b[0]]), prevBound})
		prevEnd, prevBound = b[1], b[0]
	}
	segs = append(segs, segment{degenerateNorm(text[prevEnd:]), prevBound})

	// Trailing blank segments carry no text and must not mask the real tail.
	for len(segs) > 0 && segs[len(segs)-1].norm == "" {
		segs = segs[:len(segs)-1]
	}
	if len(segs) == 0 {
		return text
	}

	last := segs[len(segs)-1].norm
	cutStart := -1
	if last != "" {
		// Identical-item run ending at (and including) the last segment.
		i := len(segs) - 1
		for i-1 >= 0 && segs[i-1].norm == last {
			i--
		}
		if len(segs)-i >= degenerateMinRun && len([]rune(last)) >= degenerateMinSegmentLen {
			cutStart = segs[i].boundStart
		} else if len(segs) >= 2 {
			// The final item may be a truncation of the repeated one (the
			// model ran out of max_tokens mid-sentence): it still counts
			// when it is a prefix of the identical items in front of it.
			canon := segs[len(segs)-2].norm
			if canon != "" && strings.HasPrefix(canon, last) {
				j := len(segs) - 2
				for j-1 >= 0 && segs[j-1].norm == canon {
					j--
				}
				if len(segs)-j >= degenerateMinRun && len([]rune(canon)) >= degenerateMinSegmentLen {
					cutStart = segs[j].boundStart
				}
			}
		}
	}
	if cutStart >= 0 {
		return strings.TrimRight(text[:cutStart], " \t\n")
	}
	return text
}

// ClampFloat clamps a float value to the specified range [minV, maxV].
func ClampFloat(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
