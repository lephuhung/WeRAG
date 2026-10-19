package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Decision 7.3: whole months full price; the leftover ≤ 15 days half,
// > 15 days full; × units (seats).
func TestPriceAddonRemainder(t *testing.T) {
	from := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		until        time.Time
		units        int
		months, days int
		factor       string
		amount       int64
	}{
		{"two whole months", from.AddDate(0, 2, 0), 1, 2, 0, "", 2 * 99_000},
		{"7 days left over: half", from.AddDate(0, 2, 7), 1, 2, 7, UsageAddonLeftoverHalf, 2*99_000 + 49_500},
		{"15 days: still half", from.AddDate(0, 0, 15), 1, 0, 15, UsageAddonLeftoverHalf, 49_500},
		{"16 days: full", from.AddDate(0, 0, 16), 1, 0, 16, UsageAddonLeftoverFull, 99_000},
		{"a started day counts", from.AddDate(0, 0, 15).Add(time.Hour), 1, 0, 16, UsageAddonLeftoverFull, 99_000},
		{"× 4 seats", from.AddDate(0, 1, 10), 4, 1, 10, UsageAddonLeftoverHalf, 4*99_000 + 2*99_000},
		{"nothing left", from, 1, 0, 0, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			months, days, factor, amount := PriceAddonRemainder(99_000, c.units, from, c.until)
			require.Equal(t, c.months, months)
			require.Equal(t, c.days, days)
			require.Equal(t, c.factor, factor)
			require.Equal(t, c.amount, amount)
		})
	}
	// an odd monthly price rounds the half up
	_, _, _, amount := PriceAddonRemainder(49_001, 1, from, from.AddDate(0, 0, 3))
	require.Equal(t, int64(24_501), amount)
}
