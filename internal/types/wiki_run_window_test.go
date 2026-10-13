package types

import (
	"testing"
	"time"
)

func TestWikiRunWindowOvernight(t *testing.T) {
	w := DefaultWikiRunWindow()
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	cases := []struct {
		hh, mm int
		open   bool
	}{
		{18, 59, false}, {19, 0, true}, {23, 30, true}, {0, 0, true}, {5, 59, true}, {6, 0, false}, {12, 0, false},
	}
	for _, c := range cases {
		now := time.Date(2026, 10, 12, c.hh, c.mm, 0, 0, loc)
		if got := w.Open(now); got != c.open {
			t.Errorf("%02d:%02d open=%v, want %v", c.hh, c.mm, got, c.open)
		}
	}
	next := w.NextOpen(time.Date(2026, 10, 12, 9, 0, 0, 0, loc))
	if want := time.Date(2026, 10, 12, 19, 0, 0, 0, loc); !next.Equal(want) {
		t.Errorf("NextOpen = %v, want %v", next, want)
	}
	if !w.NextOpen(time.Date(2026, 10, 12, 20, 0, 0, 0, loc)).IsZero() {
		t.Error("NextOpen inside the window should be zero")
	}
}

func TestWikiRunWindowSameDayAndDisabled(t *testing.T) {
	w := WikiRunWindow{Enabled: true, Start: "12:00", End: "13:30", Timezone: "UTC"}
	if !w.Open(time.Date(2026, 1, 1, 12, 45, 0, 0, time.UTC)) || w.Open(time.Date(2026, 1, 1, 13, 30, 0, 0, time.UTC)) {
		t.Error("same-day window boundaries wrong")
	}
	next := w.NextOpen(time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Errorf("NextOpen = %v, want %v", next, want)
	}
	w.Enabled = false
	if !w.Open(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)) {
		t.Error("disabled window must always be open")
	}
}

func TestParseWikiRunWindow(t *testing.T) {
	w, err := ParseWikiRunWindow([]byte(`{"enabled":false}`))
	if err != nil || w.Enabled || w.Start != "19:00" {
		t.Fatalf("partial doc should keep defaults: %+v %v", w, err)
	}
	for _, bad := range []string{
		`{"start":"25:00"}`, `{"start":"06:00"}`, `{"timezone":"Mars/Base"}`, `{"enable":true}`,
	} {
		if _, err := ParseWikiRunWindow([]byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
