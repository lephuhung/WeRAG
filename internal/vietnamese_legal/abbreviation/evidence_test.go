package abbreviation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecideTypo(t *testing.T) {
	v, c, _ := DecideTypo(TypoEvidence{Word: "cascc", Rewrite: "các", RewriteRan: true, Decoded: "các"})
	require.Equal(t, TypoCorrect, v)
	require.Equal(t, "các", c)
	v, c, _ = DecideTypo(TypoEvidence{Word: "phaps", Spellcheck: "pháp", Rewrite: "pháp", RewriteRan: true, Decoded: "pháp"})
	require.Equal(t, TypoCorrect, v)
	require.Equal(t, "pháp", c)
	// ubnf: dictionary neighbour + rewrite agree
	v, c, _ = DecideTypo(TypoEvidence{Word: "ubnf", Spellcheck: "ubn", Rewrite: "UBND", RewriteRan: true, Dictionary: "UBND"})
	require.Equal(t, TypoCorrect, v)
	require.Equal(t, "UBND", c)
	// disagreement: ask with both readings
	v, _, opts := DecideTypo(TypoEvidence{Word: "ddaats", Spellcheck: "đất", Rewrite: "đặt", RewriteRan: true})
	require.Equal(t, TypoAsk, v)
	require.Equal(t, []string{"đặt", "đất"}, opts)
	// one source only: ask
	v, _, opts = DecideTypo(TypoEvidence{Word: "hornor", Rewrite: "honor", RewriteRan: true})
	require.Equal(t, TypoAsk, v)
	require.Equal(t, []string{"honor"}, opts)
	// the chat model spelled it out: abbreviation
	v, _, _ = DecideTypo(TypoEvidence{Word: "vphc", RewriteRan: true, RewriteAbbreviation: true})
	require.Equal(t, TypoAbbreviation, v)
	// a lone spell-check guess the chat model did not share is vetoed
	v, _, _ = DecideTypo(TypoEvidence{Word: "ttcp", Spellcheck: "tập", RewriteRan: true})
	require.Equal(t, TypoNone, v)
	// memory
	v, _, _ = DecideTypo(TypoEvidence{Word: "hornor", Rewrite: "honor", RewriteRan: true, MemoryKeep: true})
	require.Equal(t, TypoKeep, v)
	v, c, _ = DecideTypo(TypoEvidence{Word: "abcx", Memory: "abc"})
	require.Equal(t, TypoCorrect, v)
	require.Equal(t, "abc", c)
	// the user rejected the automatic reading: always ask
	v, _, opts = DecideTypo(TypoEvidence{Word: "cascc", Rewrite: "các", RewriteRan: true, Decoded: "các", Ask: true})
	require.Equal(t, TypoAsk, v)
	require.Equal(t, []string{"các"}, opts)
}

func TestRewriteReadings(t *testing.T) {
	r := RewriteReadings("báo cáo của ubnf tỉnh về vphc năm 2025 cascc",
		"báo cáo của UBND tỉnh về xử phạt vi phạm hành chính năm 2025 các", []string{"ubnf", "vphc", "cascc"})
	require.Equal(t, RewriteReading{Text: "UBND"}, r["ubnf"])
	require.Equal(t, RewriteReading{Abbreviation: true}, r["vphc"])
	require.Equal(t, RewriteReading{Text: "các"}, r["cascc"])
	r = RewriteReadings("tài liệu bmnn lưu trữ", "tài liệu BMNN lưu trữ", []string{"bmnn"})
	require.Equal(t, RewriteReading{}, r["bmnn"])
	r = RewriteReadings("quyddinh về thuế", "quy định về thuế", []string{"quyddinh"})
	require.Equal(t, RewriteReading{Text: "quy định"}, r["quyddinh"])
}

func TestNearShortForm(t *testing.T) {
	sf := []string{"UBND", "HĐND", "BHXH", "TTHC"}
	require.Equal(t, "UBND", NearShortForm("ubnf", sf))
	require.Equal(t, "UBND", NearShortForm("ubdn", sf))
	require.Equal(t, "BHXH", NearShortForm("bhx", sf))
	require.Equal(t, "", NearShortForm("ubnd", sf)) // the word itself
	require.Equal(t, "", NearShortForm("cascc", sf))
}
