package abbreviation

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Tencent/WeKnora/internal/models/decision"
)

// DecisionThreshold is the minimum yes-probability for a token to count as
// an abbreviation.
const DecisionThreshold = 0.5

const decisionInstruction = `Is the token %q used in this Vietnamese text as an abbreviation or acronym, ` +
	`i.e. a short form standing for a longer phrase (for example "UBND" for "Ủy ban nhân dân", ` +
	`"bhxh" for "bảo hiểm xã hội", "GPLX" for "giấy phép lái xe")? ` +
	`Answer no if it is an ordinary Vietnamese word or syllable, a person or place name, ` +
	`a number, or a measurement unit.`

// decisionDetector adapts a Jev-API decision model to Detector: one noul
// question per proposed token. The model only scores the tokens it is
// asked about; it has no channel to add tokens or meanings.
type decisionDetector struct {
	d decision.Decider
}

// NewDecisionDetector wraps a decision model (Jev or Clef) as a Detector.
func NewDecisionDetector(d decision.Decider) Detector {
	if d == nil {
		return nil
	}
	return &decisionDetector{d: d}
}

func (x *decisionDetector) Name() string { return x.d.Provider() }

func (x *decisionDetector) Classify(ctx context.Context, text string, tokens []string) ([]bool, error) {
	scores, err := ScoreTokens(ctx, x.d, text, tokens)
	if err != nil {
		return nil, err
	}
	out := make([]bool, len(scores))
	for i, sc := range scores {
		out[i] = sc.Abbreviation
	}
	return out, nil
}

// TokenScore is the decision model's verdict for one token.
type TokenScore struct {
	Token        string  `json:"token"`
	Probability  float64 `json:"probability"`
	Abbreviation bool    `json:"abbreviation"`
}

// ScoreTokens asks d one noul question per token (batched by
// decision.MaxQuestions) and returns the yes-probabilities in token order.
func ScoreTokens(ctx context.Context, d decision.Decider, text string, tokens []string) ([]TokenScore, error) {
	out := make([]TokenScore, 0, len(tokens))
	for start := 0; start < len(tokens); start += decision.MaxQuestions {
		batch := tokens[start:min(start+decision.MaxQuestions, len(tokens))]
		questions := make(map[string]decision.Question, len(batch))
		for i, tok := range batch {
			questions[questionID(i)] = decision.Question{
				Type:         decision.QuestionNoul,
				Instructions: fmt.Sprintf(decisionInstruction, tok),
			}
		}
		answers, err := d.Decide(ctx, text, questions)
		if err != nil {
			return nil, err
		}
		for i, tok := range batch {
			a := answers[questionID(i)]
			if a.Noul == nil {
				return nil, fmt.Errorf("%s: no noul probability for token %q", d.Provider(), tok)
			}
			out = append(out, TokenScore{Token: tok, Probability: *a.Noul, Abbreviation: *a.Noul >= DecisionThreshold})
		}
	}
	return out, nil
}

func questionID(i int) string { return "t" + strconv.Itoa(i) }
