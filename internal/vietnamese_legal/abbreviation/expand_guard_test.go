package abbreviation

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// TestExpandSkipsNilRows is the focused regression for nil dictionary rows:
// a nil entry in actives must be skipped instead of panicking on ShortForm.
func TestExpandSkipsNilRows(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Expand panicked on nil row: %v", r)
		}
	}()
	res := Expand("UBND họp hôm nay", []*types.Abbreviation{
		nil,
		{ID: "m-1", ShortForm: "UBND", FullForm: "Ủy ban nhân dân", IsActive: true},
	})
	if res == nil {
		t.Fatalf("Expand returned nil")
	}
	if res.Expanded != "Ủy ban nhân dân họp hôm nay" {
		t.Fatalf("expanded = %q", res.Expanded)
	}
}
