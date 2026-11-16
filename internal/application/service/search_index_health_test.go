package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// searchIndexSettings answers the segment threshold and nothing else.
type searchIndexSettings struct {
	interfaces.SystemSettingService
	threshold int64
	asked     int
}

func (s *searchIndexSettings) GetInt(_ context.Context, key, _ string, def int64) int64 {
	if key == SearchIndexSegmentThresholdKey {
		s.asked++
		return s.threshold
	}
	return def
}

func newSearchIndexTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// On SQLite — which is what the unit tests run on — paradedb.index_info does
// not exist, so the check has to report "not available" rather than let a
// syntax error surface as a broken runtime page.
func TestSearchIndexHealthNonPostgresIsUnavailable(t *testing.T) {
	h := NewSearchIndexHealth(newSearchIndexTestDB(t), &searchIndexSettings{threshold: 40})
	rep := h.Report(context.Background())

	require.False(t, rep.Available)
	require.Equal(t, SearchIndexReasonNotPostgres, rep.Reason)
	require.Equal(t, SearchIndexName, rep.Index)
	require.False(t, rep.NeedsRebuild)
	require.Equal(t, 40, rep.Threshold, "the threshold is still reported so the page can show it")
	require.Empty(t, rep.Segments, "counters are meaningless when unavailable")
	require.False(t, rep.CheckedAt.IsZero())
}

func TestSearchIndexHealthWithoutDatabase(t *testing.T) {
	h := NewSearchIndexHealth(nil, &searchIndexSettings{threshold: 40})
	rep := h.Report(context.Background())

	require.False(t, rep.Available)
	require.Equal(t, SearchIndexReasonNoDatabase, rep.Reason)
}

// A nil check is what a partially wired container holds; asking it must not
// panic, because GetRuntimeQueues calls Report on whatever it was given.
func TestSearchIndexHealthNilReceiver(t *testing.T) {
	var h *SearchIndexHealth
	rep := h.Report(context.Background())

	require.False(t, rep.Available)
	require.Equal(t, SearchIndexName, rep.Index)
	require.Equal(t, SearchIndexReasonNotConfigured, rep.Reason)
	require.NotPanics(t, func() { StartSearchIndexHealth(nil, time.Minute)() })
}

func TestSearchIndexHealthThresholdDefaults(t *testing.T) {
	t.Run("no settings service falls back to the default", func(t *testing.T) {
		h := NewSearchIndexHealth(newSearchIndexTestDB(t), nil)
		require.Equal(t, DefaultSearchIndexSegmentThreshold, h.threshold(context.Background()))
		require.Equal(t, 60, DefaultSearchIndexSegmentThreshold,
			"three times a fresh build (~20) and clear of the 31-38 an ingest simulation settled at")
	})

	t.Run("configured value wins", func(t *testing.T) {
		h := NewSearchIndexHealth(newSearchIndexTestDB(t), &searchIndexSettings{threshold: 12})
		require.Equal(t, 12, h.threshold(context.Background()))
	})
}

// The runtime page polls, so repeated reads inside the TTL must be served
// from the cache rather than re-reading the index every request.
func TestSearchIndexHealthCachesWithinTTL(t *testing.T) {
	settings := &searchIndexSettings{threshold: 40}
	h := NewSearchIndexHealth(newSearchIndexTestDB(t), settings)
	now := time.Now()
	h.now = func() time.Time { return now }

	first := h.Report(context.Background())
	require.Equal(t, 1, settings.asked)

	h.Report(context.Background())
	require.Equal(t, 1, settings.asked, "a read inside the TTL must not hit the database again")

	// Past the TTL the report is refreshed.
	now = now.Add(searchIndexHealthTTL + time.Second)
	second := h.Report(context.Background())
	require.Equal(t, 2, settings.asked)
	require.True(t, second.CheckedAt.After(first.CheckedAt))
}

// NeedsRebuild is the only verdict the page and the warning act on, so pin
// the boundary: strictly above the threshold, and never when it is disabled.
func TestSearchIndexNeedsRebuildVerdict(t *testing.T) {
	tests := []struct {
		name      string
		segments  int
		threshold int
		want      bool
	}{
		{name: "below", segments: 20, threshold: 40, want: false},
		{name: "at the threshold is still fine", segments: 40, threshold: 40, want: false},
		{name: "one above", segments: 41, threshold: 40, want: true},
		{name: "far above", segments: 187, threshold: 40, want: true},
		{name: "threshold zero disables the verdict", segments: 187, threshold: 0, want: false},
		{name: "negative threshold disables the verdict", segments: 187, threshold: -1, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, needsRebuild(tt.segments, tt.threshold))
		})
	}
}

func TestSearchIndexHealthTickOnUnavailableIsQuiet(t *testing.T) {
	h := NewSearchIndexHealth(newSearchIndexTestDB(t), &searchIndexSettings{threshold: 40})
	require.NotPanics(t, func() {
		rep := h.Tick(context.Background())
		require.False(t, rep.Available)
	})
}
