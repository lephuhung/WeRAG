package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWithTyposDropsTypoKeys(t *testing.T) {
	typos := []types.AbbreviationTypo{{Wrong: "cascc", Correct: "các"}}
	det := withTypos(types.AbbreviationDetection{Detector: "clef", Keys: []string{"cascc", "ubnd"}}, "cascc thủ tục UBND", typos)
	require.Equal(t, "clef", det.Detector)
	require.Equal(t, []string{"ubnd"}, det.Keys)
	require.Equal(t, typos, det.Typos)

	// heuristic detection becomes an explicit key set without the typo
	h := withTypos(types.AbbreviationDetection{}, "cascc thủ tục UBND", typos)
	require.Equal(t, types.AbbreviationDetectorHeuristicSpellcheck, h.Detector)
	require.NotContains(t, h.Keys, "cascc")
	require.Contains(t, h.Keys, "ubnd")

	// no typos: detection unchanged
	require.Equal(t, types.AbbreviationDetection{}, withTypos(types.AbbreviationDetection{}, "x", nil))
}
