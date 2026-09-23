package abbreviation

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type countingLister struct {
	actives []*types.Abbreviation
	err     error
	calls   int
}

func (l *countingLister) ListActive(context.Context) ([]*types.Abbreviation, error) {
	l.calls++
	return l.actives, l.err
}

func TestResolveSearchQuery_NoCandidateSkipsDictionary(t *testing.T) {
	lister := &countingLister{actives: []*types.Abbreviation{active("UBND", "Ủy ban nhân dân")}}
	query, res, err := ResolveSearchQuery(context.Background(), "tỉnh họp sáng nay", lister)
	if err != nil || res != nil || query != "tỉnh họp sáng nay" {
		t.Fatalf("query=%q res=%+v err=%v", query, res, err)
	}
	if lister.calls != 0 {
		t.Fatalf("ListActive must not run without candidates, calls=%d", lister.calls)
	}
}

func TestResolveSearchQuery_NilLister(t *testing.T) {
	query, res, err := ResolveSearchQuery(context.Background(), "UBND tỉnh họp", nil)
	if err != nil || res != nil || query != "UBND tỉnh họp" {
		t.Fatalf("query=%q res=%+v err=%v", query, res, err)
	}
}

func TestResolveSearchQuery_SingleMeaningEnriches(t *testing.T) {
	lister := &countingLister{actives: []*types.Abbreviation{active("UBND", "Ủy ban nhân dân")}}
	query, res, err := ResolveSearchQuery(context.Background(), "UBND tỉnh họp", lister)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if query != "Ủy ban nhân dân (UBND) tỉnh họp" {
		t.Fatalf("query=%q", query)
	}
	if lister.calls != 1 {
		t.Fatalf("ListActive calls=%d", lister.calls)
	}
	if res == nil || len(res.Applied) != 1 || res.Applied[0].ShortForm != "UBND" {
		t.Fatalf("applied=%+v", res)
	}
}

func TestResolveSearchQuery_AmbiguousStaysUntouched(t *testing.T) {
	lister := &countingLister{actives: []*types.Abbreviation{
		active("BCH", "Ban chấp hành"),
		active("BCH", "Bệnh viện C Hòa"),
	}}
	query, res, err := ResolveSearchQuery(context.Background(), "BCH đã họp", lister)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if query != "BCH đã họp" {
		t.Fatalf("ambiguous query must be unchanged: %q", query)
	}
	if len(res.Ambiguous["BCH"]) != 2 || len(res.Applied) != 0 {
		t.Fatalf("res=%+v", res)
	}
}

func TestResolveSearchQuery_ListerErrorFailsOpen(t *testing.T) {
	want := errors.New("dictionary unavailable")
	lister := &countingLister{err: want}
	query, res, err := ResolveSearchQuery(context.Background(), "UBND tỉnh họp", lister)
	if err != want || res != nil || query != "UBND tỉnh họp" {
		t.Fatalf("query=%q res=%+v err=%v", query, res, err)
	}
}

func TestEnrichSearchQuery_DedupesCaseVariants(t *testing.T) {
	res := &ExpandResult{
		Original: "UBND tỉnh họp",
		Applied: []AppliedAbbr{
			{ShortForm: "UBND", FullForm: "Ủy ban nhân dân"},
			{ShortForm: "ubnd", FullForm: "không được dùng"},
		},
	}
	got := EnrichSearchQuery(res)
	if got != "Ủy ban nhân dân (UBND) tỉnh họp" {
		t.Fatalf("got=%q", got)
	}
	if EnrichSearchQuery(nil) != "" {
		t.Fatal("nil result must yield empty query")
	}
}
