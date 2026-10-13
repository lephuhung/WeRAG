package agent

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// The document assistant's cards carry the only buttons there are: a
// rewrite proposal ("Thay vào văn bản"), an illustration ("Chèn vào văn
// bản") and a format-fix plan ("Áp dụng các sửa thể thức này"). The model
// kept inviting the user to press one after a format report or a refused
// edit, where no card exists; prompt rules and tool-result notes did not
// stop it. So for a turn marked types.WithAnswerButtonGuard the answer text
// is filtered sentence by sentence as it streams: a sentence naming a
// button whose card this turn did not produce is dropped (and logged). The
// same filter cleans the content kept as the final answer, so the saved
// message matches what was shown.

// turnButtons is which cards the steps of the turn produced so far.
type turnButtons struct {
	proposal     bool
	illustration bool
	fixPlan      bool
}

// buttonRule maps a button phrase (lower case, with diacritics) to the card
// that must exist for a sentence to keep it.
type buttonRule struct {
	phrase string
	card   string
	has    func(turnButtons) bool
}

var buttonRules = []buttonRule{
	{"thay vào văn bản", "rewrite proposal", func(b turnButtons) bool { return b.proposal }},
	{"chèn vào văn bản", "illustration", func(b turnButtons) bool { return b.illustration }},
	// the button's exact label: "tôi sẽ áp dụng các sửa thể thức" is a
	// sentence about the next step, not a button
	{"áp dụng các sửa thể thức này", "format-fix plan", func(b turnButtons) bool { return b.fixPlan }},
}

// collectTurnButtons scans the tool results of the turn for the cards.
func collectTurnButtons(steps []types.AgentStep) turnButtons {
	var b turnButtons
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			r := tc.Result
			if r == nil || !r.Success || r.Data == nil {
				continue
			}
			if v, _ := r.Data["proposal"].(bool); v {
				b.proposal = true
			}
			if r.Data["illustration"] != nil {
				b.illustration = true
			}
			if tc.ExecutionName() == "apply_format_fixes" {
				review, _ := r.Data["review"].(bool)
				if review && len(dataList(r.Data["applied"])) > 0 {
					b.fixPlan = true
				}
			}
		}
	}
	return b
}

// dataList is a tool Data value as a list; tool Data is built in Go, so
// the value may be a typed slice ([]AppliedFormatFix) rather than
// []interface{}.
func dataList(v interface{}) []interface{} {
	return typedSliceAsList(v)
}

type turnButtonsKey struct{}

// withTurnButtons stores the turn's cards in ctx for the round's emitters;
// a no-op when the guard is off.
func withTurnButtons(ctx context.Context, steps []types.AgentStep) context.Context {
	if !types.AnswerButtonGuardFromContext(ctx) {
		return ctx
	}
	return context.WithValue(ctx, turnButtonsKey{}, collectTurnButtons(steps))
}

// answerGuardFromContext is the guard for this round, nil when off.
func answerGuardFromContext(ctx context.Context) *turnButtons {
	if !types.AnswerButtonGuardFromContext(ctx) {
		return nil
	}
	b, ok := ctx.Value(turnButtonsKey{}).(turnButtons)
	if !ok {
		b = turnButtons{}
	}
	return &b
}

// phantomButton is the rule a unit of text breaks, nil when none.
func phantomButton(unit string, b turnButtons) *buttonRule {
	l := strings.ToLower(unit)
	for i := range buttonRules {
		r := &buttonRules[i]
		if strings.Contains(l, r.phrase) && !r.has(b) {
			return r
		}
	}
	return nil
}

// splitUnits cuts text into sentence-ish units: a line, or a sentence ended
// by ". ", "! ", "? " (the terminator stays with its unit). The last unit
// is complete only when text ends with a line break; incomplete reports
// that, so a streaming caller can hold it back.
func splitUnits(text string) (units []string, incomplete bool) {
	start := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '\n' {
			units = append(units, text[start:i+1])
			start = i + 1
			continue
		}
		if (c == '.' || c == '!' || c == '?') && i+1 < len(text) && text[i+1] == ' ' {
			units = append(units, text[start:i+2])
			start = i + 2
			i++
		}
	}
	if start < len(text) {
		units = append(units, text[start:])
		return units, true
	}
	return units, false
}

// cleanAnswer drops every unit of text naming a button the turn has no
// card for; dropped is how many.
func cleanAnswer(ctx context.Context, text string, b turnButtons) (string, int) {
	units, _ := splitUnits(text)
	var out strings.Builder
	dropped := 0
	for _, u := range units {
		if r := phantomButton(u, b); r != nil {
			dropped++
			logger.Infof(ctx, "[Agent][AnswerGuard] dropped a sentence inviting %q: no %s this turn: %q",
				r.phrase, r.card, strings.TrimSpace(u))
			// keep the line break of a dropped line so lists stay lists
			if strings.HasSuffix(u, "\n") && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
				out.WriteString("\n")
			}
			continue
		}
		out.WriteString(u)
	}
	return out.String(), dropped
}

// cleanAnswerInContext applies cleanAnswer when the guard is on.
func cleanAnswerInContext(ctx context.Context, text string) string {
	b := answerGuardFromContext(ctx)
	if b == nil || text == "" {
		return text
	}
	out, _ := cleanAnswer(ctx, text, *b)
	return out
}

// answerGuard filters a streamed answer: text is held back until a unit is
// complete, so a dropped sentence never reaches the client in part.
type answerGuard struct {
	ctx     context.Context
	buttons turnButtons
	emit    func(string)
	pending string
	dropped int
}

// newAnswerGuard returns a guard writing to emit, or nil when the guard is
// off (callers then emit directly).
func newAnswerGuard(ctx context.Context, emit func(string)) *answerGuard {
	b := answerGuardFromContext(ctx)
	if b == nil {
		return nil
	}
	return &answerGuard{ctx: ctx, buttons: *b, emit: emit}
}

// Write adds streamed text and emits the complete units.
func (g *answerGuard) Write(text string) {
	if text == "" {
		return
	}
	g.pending += text
	units, incomplete := splitUnits(g.pending)
	if incomplete {
		g.pending = units[len(units)-1]
		units = units[:len(units)-1]
	} else {
		g.pending = ""
	}
	for _, u := range units {
		g.emitUnit(u)
	}
}

// Flush emits what is still held back (end of the stream).
func (g *answerGuard) Flush() {
	if g.pending == "" {
		return
	}
	u := g.pending
	g.pending = ""
	g.emitUnit(u)
}

func (g *answerGuard) emitUnit(u string) {
	if r := phantomButton(u, g.buttons); r != nil {
		g.dropped++
		logger.Infof(g.ctx, "[Agent][AnswerGuard] dropped a streamed sentence inviting %q: no %s this turn: %q",
			r.phrase, r.card, strings.TrimSpace(u))
		if strings.HasSuffix(u, "\n") {
			g.emit("\n")
		}
		return
	}
	g.emit(u)
}
