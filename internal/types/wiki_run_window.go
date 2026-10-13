package types

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // the window names an IANA zone; containers may lack zoneinfo
)

// WikiRunWindowSettingKey is the system setting that confines wiki
// generation to off-hours. Outside the window the wiki asynq queue is paused,
// so its synthesis LLM calls do not compete with interactive chat; uploads
// keep queueing and are processed when the window opens.
const WikiRunWindowSettingKey = "wiki.run_window"

// WikiRunWindow is the daily time-of-day window in which wiki tasks run.
// Start/End are "HH:MM" in Timezone; End before Start spans midnight
// (19:00–06:00 runs overnight).
type WikiRunWindow struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
}

// DefaultWikiRunWindow is in force until the SuperAdmin saves one: wiki runs
// overnight, Vietnam time, leaving the LLM to chat during working hours.
func DefaultWikiRunWindow() WikiRunWindow {
	return WikiRunWindow{Enabled: true, Start: "19:00", End: "06:00", Timezone: "Asia/Ho_Chi_Minh"}
}

// ResolveWikiRunWindow returns the saved window, or the default when none is
// saved or the stored document is unreadable (writes are validated, so the
// latter only follows a manual DB edit).
func ResolveWikiRunWindow(ctx context.Context, settings interface {
	GetJSON(ctx context.Context, key string) (JSON, bool)
},
) WikiRunWindow {
	if settings == nil {
		return DefaultWikiRunWindow()
	}
	raw, found := settings.GetJSON(ctx, WikiRunWindowSettingKey)
	if !found || len(raw) == 0 {
		return DefaultWikiRunWindow()
	}
	w, err := ParseWikiRunWindow(raw)
	if err != nil {
		return DefaultWikiRunWindow()
	}
	return w
}

// ParseWikiRunWindow strictly decodes and validates a stored window. Fields
// left out of the document keep their defaults.
func ParseWikiRunWindow(raw []byte) (WikiRunWindow, error) {
	w := DefaultWikiRunWindow()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return w, fmt.Errorf("invalid wiki run window: %w", err)
	}
	w.Start = strings.TrimSpace(w.Start)
	w.End = strings.TrimSpace(w.End)
	w.Timezone = strings.TrimSpace(w.Timezone)
	if err := w.validate(); err != nil {
		return w, err
	}
	return w, nil
}

func (w WikiRunWindow) validate() error {
	start, err := parseClock(w.Start)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	end, err := parseClock(w.End)
	if err != nil {
		return fmt.Errorf("end: %w", err)
	}
	if start == end {
		return errors.New("start and end must differ")
	}
	if _, err := time.LoadLocation(w.Timezone); err != nil || w.Timezone == "" {
		return fmt.Errorf("unknown timezone %q", w.Timezone)
	}
	return nil
}

// parseClock returns minutes since midnight for "HH:MM".
func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// Open reports whether wiki tasks may run at now. A disabled or invalid
// window never holds wiki back.
func (w WikiRunWindow) Open(now time.Time) bool {
	if !w.Enabled || w.validate() != nil {
		return true
	}
	loc, _ := time.LoadLocation(w.Timezone)
	start, _ := parseClock(w.Start)
	end, _ := parseClock(w.End)
	local := now.In(loc)
	m := local.Hour()*60 + local.Minute()
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}

// NextOpen is the next time the window opens after now; zero when it is
// already open.
func (w WikiRunWindow) NextOpen(now time.Time) time.Time {
	if w.Open(now) {
		return time.Time{}
	}
	loc, _ := time.LoadLocation(w.Timezone)
	start, _ := parseClock(w.Start)
	local := now.In(loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), start/60, start%60, 0, 0, loc)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
