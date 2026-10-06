package abbreviation

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/decision"
)

// scriptedDecider says yes to tokens in yes and counts calls.
type scriptedDecider struct {
	yes   map[string]bool
	calls int
}

func (*scriptedDecider) Provider() string { return "jev" }

func (s *scriptedDecider) Decide(_ context.Context, _ string, qs map[string]decision.Question) (map[string]decision.Answer, error) {
	s.calls++
	if len(qs) > decision.MaxQuestions {
		panic("batch over limit")
	}
	out := map[string]decision.Answer{}
	for id, q := range qs {
		p := 0.1
		for tok := range s.yes {
			if strings.Contains(q.Instructions, `token "`+tok+`"`) {
				p = 0.95
			}
		}
		out[id] = decision.Answer{Type: decision.QuestionNoul, Noul: &p}
	}
	return out, nil
}

func TestDecisionDetectorFlagsAndBatches(t *testing.T) {
	d := &scriptedDecider{yes: map[string]bool{"bhxh": true}}
	tokens := make([]string, 0, 70)
	for i := 0; i < 69; i++ {
		tokens = append(tokens, "w"+strings.Repeat("a", i+1))
	}
	tokens = append(tokens, "bhxh")
	got, err := NewDecisionDetector(d).Classify(context.Background(), "q", tokens)
	if err != nil {
		t.Fatal(err)
	}
	if d.calls != 2 || len(got) != 70 || !got[69] || got[0] {
		t.Fatalf("calls=%d len=%d", d.calls, len(got))
	}
	det, err := Detect(context.Background(), NewDecisionDetector(d), "đóng bhxh ở đâu")
	if err != nil || det.Detector != "jev" || len(det.Keys) != 1 || det.Keys[0] != "bhxh" {
		t.Fatalf("det=%+v err=%v", det, err)
	}
}
