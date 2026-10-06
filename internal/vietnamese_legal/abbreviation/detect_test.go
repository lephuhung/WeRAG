package abbreviation

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type fakeDetector struct {
	yes  map[string]bool
	err  error
	seen []string
}

func (*fakeDetector) Name() string { return "clef" }

func (f *fakeDetector) Classify(_ context.Context, _ string, tokens []string) ([]bool, error) {
	f.seen = append([]string(nil), tokens...)
	if f.err != nil {
		return nil, f.err
	}
	out := make([]bool, len(tokens))
	for i, tok := range tokens {
		out[i] = f.yes[termKey(tok)]
	}
	return out, nil
}

func TestProposeTokensSkipsStopWordsCodesAndNumbers(t *testing.T) {
	got := ProposeTokens("Theo văn bản 172/GM-UBND thì CAND và cand của 2024 là gì")
	want := []string{"văn", "bản", "CAND"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDetectFlagsOnlyProposedTokens(t *testing.T) {
	d := &fakeDetector{yes: map[string]bool{"cand": true, "xyz": true}}
	det, err := Detect(context.Background(), d, "tra cứu cand")
	if err != nil {
		t.Fatal(err)
	}
	// xyz is not in the text, so the detector cannot add it.
	if det.Detector != "clef" || !reflect.DeepEqual(det.Keys, []string{"cand"}) {
		t.Fatalf("detection = %+v", det)
	}
}

func TestDetectFallsBackToHeuristicOnError(t *testing.T) {
	det, err := Detect(context.Background(), &fakeDetector{err: errors.New("down")}, "tra cứu UBND")
	if err == nil || det.Detector != types.AbbreviationDetectorHeuristic || det.Keys != nil {
		t.Fatalf("det=%+v err=%v", det, err)
	}
	if det, err := Detect(context.Background(), nil, "UBND"); err != nil || det.Detector != "" {
		t.Fatalf("nil detector: det=%+v err=%v", det, err)
	}
}

func TestInspectDetectedUsesClassifierNotHeuristic(t *testing.T) {
	active := []*types.Abbreviation{
		{ID: "1", ShortForm: "CAND", FullForm: "Công an nhân dân", IsActive: true},
		{ID: "2", ShortForm: "ng", FullForm: "người", IsActive: true},
	}
	// Heuristic misses "cand" (vowel ratio 0.25) and wrongly takes "ng".
	heur := Inspect("tra cứu cand ng", active)
	if len(heur.Terms) != 1 || heur.Terms[0].Key != "ng" {
		t.Fatalf("heuristic terms = %+v", heur.Terms)
	}
	det := types.AbbreviationDetection{Detector: "clef", Keys: []string{"cand"}}
	r := InspectDetected("tra cứu cand ng", active, det)
	if r.Status != types.AbbreviationStatusReady || len(r.Terms) != 1 || r.Terms[0].Key != "cand" {
		t.Fatalf("resolution = %+v", r)
	}
	if r.EffectiveQuery != "tra cứu Công an nhân dân (cand) ng" {
		t.Fatalf("effective = %q", r.EffectiveQuery)
	}
	// Coverage re-validation uses the recorded detection, not the heuristic.
	if _, err := RenderResolvedQuery(r); err != nil {
		t.Fatalf("render: %v", err)
	}
	stripped := r.Clone()
	stripped.Detector, stripped.DetectedKeys = "", nil
	if _, err := RenderResolvedQuery(stripped); err == nil {
		t.Fatal("heuristic coverage must reject clef terms")
	}
}

func TestInspectDetectedUnknownStillNeedsDefinition(t *testing.T) {
	det := types.AbbreviationDetection{Detector: "clef", Keys: []string{"qlda"}}
	r := InspectDetected("quy trình qlda", nil, det)
	if r.Status != types.AbbreviationStatusNeedsDefinition || !reflect.DeepEqual(r.UnknownTerms, []string{"qlda"}) {
		t.Fatalf("resolution = %+v", r)
	}
}
