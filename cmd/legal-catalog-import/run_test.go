package main

import "testing"

func TestRunControlRequested(t *testing.T) {
	reset := func() {
		*runStart, *runID, *runStep, *runFinish, *runSummary, *runSummaryFile = false, 0, "", "", "", ""
	}
	t.Cleanup(reset)
	cases := []struct {
		name string
		set  func()
		want bool
	}{
		{"plain import", func() {}, false},
		{"import with a run id records its counts", func() { *runID = 7 }, false},
		{"run id 0 from an untracked sync", func() { *runID = 0; *runStep = "import" }, false},
		{"start", func() { *runStart = true }, true},
		{"move", func() { *runID = 7; *runStep = "crawl" }, true},
		{"summary", func() { *runID = 7; *runSummaryFile = "crawl.json" }, true},
		{"finish", func() { *runID = 7; *runFinish = "failed" }, true},
	}
	for _, c := range cases {
		reset()
		c.set()
		if got := runControlRequested(); got != c.want {
			t.Errorf("%s: runControlRequested() = %v, want %v", c.name, got, c.want)
		}
	}
}
