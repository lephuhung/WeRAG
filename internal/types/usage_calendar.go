package types

import "time"

// Billing calendar (user decision 19/10/2026, review item 7): time is
// counted, but a day ends at 24:00 Vietnam time (GMT+7), not at the hour of
// purchase; months are added with the day clamped to the month's end.
//
// - Subscription and add-on ends snap to the end of their end date
//   (EndOfDayVN): 24:00 VN = 00:00 of the next day VN, stored in UTC.
// - Months are added on the Vietnamese calendar (AddMonthsVN): 31/1 + 1
//   month = 28/2 (29/2 in a leap year), not 3/3.
// - Leftover days of the 2-tier rule count Vietnamese calendar days
//   (CalendarDaysVN): every date the span touches counts once.

// EndOfDayVN returns 24:00 Vietnam time of t's Vietnamese date — t itself
// when it already is a Vietnamese midnight. In UTC.
func EndOfDayVN(t time.Time) time.Time {
	local := t.In(usageLocation)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, usageLocation)
	if local.Equal(midnight) {
		return midnight.UTC()
	}
	return midnight.AddDate(0, 0, 1).UTC()
}

// AddMonthsVN adds months on the Vietnamese calendar, keeping the clock
// time and clamping the day to the target month's last day. In UTC.
func AddMonthsVN(t time.Time, months int) time.Time {
	local := t.In(usageLocation)
	y, m, d := local.Date()
	first := time.Date(y, m+time.Month(months), 1, local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), usageLocation)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), usageLocation).UTC()
}

// SubscriptionEndVN is the end of a period of `months` starting at from:
// the clamped month step, snapped to 24:00 VN of that date. A `from` that is
// itself a stored end (a Vietnamese midnight = 24:00 of the previous date)
// steps from that previous date, the last one covered: end of 30/10 + 1
// month = end of 30/11 (not of 29/11), end of 31/1 + 1 month = end of 28/2.
func SubscriptionEndVN(from time.Time, months int) time.Time {
	if isMidnightVN(from) {
		from = from.Add(-time.Nanosecond)
	}
	return EndOfDayVN(AddMonthsVN(from, months))
}

// isMidnightVN reports whether t is 00:00 Vietnam time.
func isMidnightVN(t time.Time) bool {
	local := t.In(usageLocation)
	return local.Hour() == 0 && local.Minute() == 0 && local.Second() == 0 && local.Nanosecond() == 0
}

// CalendarDaysVN counts the Vietnamese dates the span [from, until)
// touches: a day that has started counts as a whole day, and a span ending
// at a Vietnamese midnight does not touch the next date. 0 when empty.
func CalendarDaysVN(from, until time.Time) int {
	if !until.After(from) {
		return 0
	}
	first := from.In(usageLocation)
	last := until.Add(-time.Nanosecond).In(usageLocation)
	d0 := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.UTC)
	d1 := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC)
	return int(d1.Sub(d0).Hours()/24) + 1
}
