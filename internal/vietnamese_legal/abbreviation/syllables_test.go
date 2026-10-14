package abbreviation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeTyping(t *testing.T) {
	for in, want := range map[string]string{
		"cacs": "các", "casc": "các", "cascc": "các", "cassc": "các", "mootj": "một",
		"ddang": "đang", "ddaats": "đất", "phaps": "pháp", "thur": "thủ", "tucj": "tục",
		"nghij": "nghị", "thowif": "thời", "ddieeuf": "điều", "trachs": "trách", "dduowcj": "được", "duowcj": "dược",
		"ca1c": "các", "la6p5": "lập", "Cacs": "các",
	} {
		got, ok := DecodeTyping(in)
		require.True(t, ok, in)
		require.Equal(t, want, got, in)
	}
	// abbreviations, names and real syllables are not typing
	for _, in := range []string{"ubnd", "tncn", "vphc", "bmnn", "cccd", "anm", "zalo", "iphone", "các", "pa05"} {
		_, ok := DecodeTyping(in)
		require.False(t, ok, in)
	}
}

func TestSuspectWords(t *testing.T) {
	require.Equal(t, []string{"cascc", "UBND", "la2"}, SuspectWords("số lượng hồ sơ cascc của UBND, số 172/GM-UBND năm 2025 về pa05 la2 gì"))
}
