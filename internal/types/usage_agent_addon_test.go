package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// vn is a Vietnamese wall-clock time, returned in UTC.
func vn(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.FixedZone("ICT", 7*3600)).UTC()
}

// Decision 7.3 with the 19/10/2026 calendar rule: whole months full price;
// the leftover in Vietnamese calendar days (a day ends at 24:00 GMT+7),
// ≤ 15 days half, > 15 full; × units (seats).
func TestPriceAddonRemainder(t *testing.T) {
	from := vn(2026, 10, 13, 17, 0) // bought 17:00 on 13/10
	cases := []struct {
		name         string
		until        time.Time
		units        int
		months, days int
		factor       string
		amount       int64
	}{
		{"to 24:00 of 13/12: 2 whole months (as a 2-month subscription)", vn(2026, 12, 14, 0, 0), 1, 2, 0, "", 2 * 99_000},
		{"to 24:00 of 20/12: 2 months + 7 dates → half", vn(2026, 12, 21, 0, 0), 1, 2, 7, UsageAddonLeftoverHalf, 2*99_000 + 49_500},
		{"to 17:00 of 13/12: 1 month + 30 dates → full", vn(2026, 12, 13, 17, 0), 1, 1, 30, UsageAddonLeftoverFull, 2 * 99_000},
		{"13/10 → end of 27/10: 15 dates → half", vn(2026, 10, 28, 0, 0), 1, 0, 15, UsageAddonLeftoverHalf, 49_500},
		{"13/10 → end of 28/10: 16 dates → full", vn(2026, 10, 29, 0, 0), 1, 0, 16, UsageAddonLeftoverFull, 99_000},
		{"one minute past midnight touches one more date", vn(2026, 10, 28, 0, 1), 1, 0, 16, UsageAddonLeftoverFull, 99_000},
		{"× 4 seats, 1 month + 10 dates", vn(2026, 11, 24, 0, 0), 4, 1, 10, UsageAddonLeftoverHalf, 4*99_000 + 2*99_000},
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
	_, _, _, amount := PriceAddonRemainder(49_001, 1, from, vn(2026, 10, 16, 0, 0))
	require.Equal(t, int64(24_501), amount)
}

func TestBillingCalendarVN(t *testing.T) {
	// a day ends at 24:00 VN, stored in UTC
	require.Equal(t, vn(2026, 10, 20, 0, 0), EndOfDayVN(vn(2026, 10, 19, 23, 59)))
	require.Equal(t, vn(2026, 10, 20, 0, 0), EndOfDayVN(vn(2026, 10, 19, 0, 1)))
	require.Equal(t, vn(2026, 10, 19, 0, 0), EndOfDayVN(vn(2026, 10, 19, 0, 0)), "a VN midnight stays")
	// 23:30 UTC on 18/10 is 06:30 VN on 19/10: the VN date counts
	require.Equal(t, vn(2026, 10, 20, 0, 0), EndOfDayVN(time.Date(2026, 10, 18, 23, 30, 0, 0, time.UTC)))

	// months clamp to the month's last day
	require.Equal(t, vn(2027, 2, 28, 9, 0), AddMonthsVN(vn(2027, 1, 31, 9, 0), 1))
	require.Equal(t, vn(2028, 2, 29, 9, 0), AddMonthsVN(vn(2028, 1, 31, 9, 0), 1), "leap year")
	require.Equal(t, vn(2027, 3, 31, 9, 0), AddMonthsVN(vn(2027, 1, 31, 9, 0), 2), "from the original date")
	require.Equal(t, vn(2027, 1, 31, 9, 0), AddMonthsVN(vn(2026, 10, 31, 9, 0), 3))
	require.Equal(t, vn(2027, 3, 1, 0, 0), SubscriptionEndVN(vn(2027, 1, 31, 9, 0), 1), "ends at 24:00 of 28/2")

	// calendar days
	require.Equal(t, 1, CalendarDaysVN(vn(2026, 10, 19, 23, 0), vn(2026, 10, 20, 0, 0)))
	require.Equal(t, 2, CalendarDaysVN(vn(2026, 10, 19, 23, 0), vn(2026, 10, 20, 0, 30)))
	require.Equal(t, 0, CalendarDaysVN(vn(2026, 10, 20, 0, 0), vn(2026, 10, 20, 0, 0)))

	// 31/1 → end of 16/3: 1 month (to the end of 28/2) + 16 dates (1/3..16/3) → full
	months, days := AddonPeriodSplit(vn(2027, 1, 31, 9, 0), vn(2027, 3, 17, 0, 0))
	require.Equal(t, 1, months)
	require.Equal(t, 16, days)
	// 31/1 → end of 31/3: 2 whole months (31/1 → 28/2 → 31/3, clamped)
	months, days = AddonPeriodSplit(vn(2027, 1, 31, 9, 0), vn(2027, 4, 1, 0, 0))
	require.Equal(t, 2, months)
	require.Equal(t, 0, days)
	// review item 7 example: bought 01:00 on 20/10, ends 00:30 on 4/11 → 16
	// dates (20/10..4/11) → full month
	_, days = AddonPeriodSplit(vn(2026, 10, 20, 1, 0), vn(2026, 11, 4, 0, 30))
	require.Equal(t, 16, days)
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

// Review R1: months added from a stored end (a VN midnight = 24:00 of the
// previous date) step from that previous date.
func TestSubscriptionEndFromAStoredEnd(t *testing.T) {
	endOf := func(y int, m time.Month, d int) time.Time { return vn(y, m, d+1, 0, 0) }
	cases := []struct {
		name   string
		from   time.Time
		months int
		want   time.Time
	}{
		{"end of 30/10 + 1 month = end of 30/11", endOf(2026, 10, 30), 1, endOf(2026, 11, 30)},
		{"end of 31/10 + 1 month = end of 30/11 (clamped)", endOf(2026, 10, 31), 1, endOf(2026, 11, 30)},
		{"end of 31/1 + 1 month = end of 28/2", endOf(2027, 1, 31), 1, endOf(2027, 2, 28)},
		{"end of 31/1 + 1 month = end of 29/2 in a leap year", endOf(2028, 1, 31), 1, endOf(2028, 2, 29)},
		{"end of 28/2 + 1 month = end of 28/3", endOf(2027, 2, 28), 1, endOf(2027, 3, 28)},
		{"end of 30/10 + 0 months = itself", endOf(2026, 10, 30), 0, endOf(2026, 10, 30)},
		{"bought 10:00 on 30/9 + 1 month = end of 30/10", vn(2026, 9, 30, 10, 0), 1, endOf(2026, 10, 30)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, SubscriptionEndVN(c.from, c.months))
		})
	}
	// a right ending at the end of 30/10, extended to the end of 30/11, is
	// exactly one month (not 1 month + 1 day → 1.5 months)
	months, days := AddonPeriodSplit(endOf(2026, 10, 30), endOf(2026, 11, 30))
	require.Equal(t, 1, months)
	require.Equal(t, 0, days)
	months, days = AddonPeriodSplit(endOf(2027, 1, 31), endOf(2027, 2, 28))
	require.Equal(t, 1, months)
	require.Equal(t, 0, days)
	_, _, factor, amount := PriceAddonRemainder(99_000, 1, endOf(2026, 10, 30), endOf(2026, 11, 30))
	require.Equal(t, "", factor)
	require.Equal(t, int64(99_000), amount)
}
