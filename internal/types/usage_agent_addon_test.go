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

func TestMergeSubscriptionAddonNeverShortens(t *testing.T) {
	d := func(days int) *time.Time {
		v := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC).AddDate(0, 0, days)
		return &v
	}
	cases := []struct {
		name               string
		oldEnd, newEnd     *time.Time
		want               *time.Time
		oldStart, newStart *time.Time
		wantStart          *time.Time
	}{
		{"later new end wins", d(10), d(20), d(20), d(0), d(5), d(0)},
		{"earlier new end loses", d(20), d(10), d(20), d(0), d(5), d(0)},
		{"open existing stays open", nil, d(10), nil, d(5), d(0), d(0)},
		{"open new opens it", d(10), nil, nil, d(0), d(0), d(0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MergeSubscriptionAddon(
				&UsageSubscriptionAddon{StartsAt: *c.oldStart, EndsAt: c.oldEnd, PriceVND: 1},
				&UsageSubscriptionAddon{StartsAt: *c.newStart, EndsAt: c.newEnd, PriceVND: 2})
			require.Equal(t, c.want, got.EndsAt)
			require.Equal(t, *c.wantStart, got.StartsAt, "the earlier start is kept")
			require.Equal(t, int64(2), got.PriceVND)
		})
	}
	require.Same(t, (*UsageSubscriptionAddon)(nil), (*UsageSubscriptionAddon)(nil))
	in := &UsageSubscriptionAddon{PriceVND: 3}
	require.Same(t, in, MergeSubscriptionAddon(nil, in), "a new right is written as is")
}
