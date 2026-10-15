package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsagePlanFeaturesEffectiveMaxIterations(t *testing.T) {
	none := UsagePlanFeatures{}
	require.Equal(t, 30, none.EffectiveMaxIterations(30))
	require.Equal(t, -1, none.EffectiveMaxIterations(-1))

	capped := UsagePlanFeatures{MaxIterations: 12}
	require.Equal(t, 8, capped.EffectiveMaxIterations(8))
	require.Equal(t, 12, capped.EffectiveMaxIterations(46))
	require.Equal(t, 12, capped.EffectiveMaxIterations(UnlimitedMaxIterations))
	require.Equal(t, 12, capped.EffectiveMaxIterations(0))
}

func TestUsagePlanFeaturesEffectiveHistoryTokens(t *testing.T) {
	require.Equal(t, 250000, UsagePlanFeatures{}.EffectiveHistoryTokens(250000))
	require.Equal(t, 65536, UsagePlanFeatures{HistoryTokens: 65536}.EffectiveHistoryTokens(250000))
	require.Equal(t, MinHistoryTokens, UsagePlanFeatures{HistoryTokens: 1000}.EffectiveHistoryTokens(250000))
	require.Equal(t, 128000, UsagePlanFeatures{HistoryTokens: 500000}.EffectiveHistoryTokens(128000))
}

func TestParseUsagePlanFeatures(t *testing.T) {
	f, err := ParseUsagePlanFeatures(JSON(`{"session.max_tokens": 2000000, "session.max_turns": null}`))
	require.NoError(t, err)
	require.Equal(t, UsagePlanFeatures{SessionMaxTokens: 2000000}, f)
	f, err = ParseUsagePlanFeatures(nil)
	require.NoError(t, err)
	require.True(t, f.IsZero())
	_, err = ParseUsagePlanFeatures(JSON(`{"agent.turn_token_cap": true}`))
	require.Error(t, err)
}
