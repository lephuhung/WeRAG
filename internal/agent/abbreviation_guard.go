package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

// RequireAbbreviationTurn pins this engine to the sealed abbreviation turn
// that authorized it. Session AgentQA always sets it before Execute; the
// value is copied so later caller mutation cannot weaken the requirement.
// Autonomous engines (Wiki workers, the skill installer) never set it, and a
// nil binding disables every check so their lifecycle stays unchanged.
func (e *AgentEngine) RequireAbbreviationTurn(b types.AbbreviationBinding) {
	binding := b
	e.abbreviationBinding = &binding
}

// checkAbbreviationTurn verifies the sealed context still carries the
// required binding and a fully validated resolution. It runs before every
// model call, tool execution and answer-emission point, so a turn that was
// never bound — or whose sealed payload does not match — cannot reach them.
func (e *AgentEngine) checkAbbreviationTurn(ctx context.Context) error {
	if e.abbreviationBinding == nil {
		return nil
	}
	return abbreviation.RequireTurn(ctx, *e.abbreviationBinding)
}

// checkAbbreviationBindingArgs compares the run identifiers handed to
// Execute with the required binding, not merely the context against a
// binding captured earlier. RawQuery is deliberately not compared: it holds
// the current raw user reply, while Execute receives the effective query
// enriched with image or attachment context.
func (e *AgentEngine) checkAbbreviationBindingArgs(sessionID, messageID string) error {
	b := e.abbreviationBinding
	if b == nil {
		return nil
	}
	if b.Owner.SessionID != sessionID || b.AssistantMessageID != messageID {
		return fmt.Errorf("%w: agent run %s/%s does not match required turn %s/%s",
			types.ErrAbbreviationConflict, sessionID, messageID,
			b.Owner.SessionID, b.AssistantMessageID)
	}
	return nil
}

// abbreviationProvenanceNote renders the sealed term mapping for the model.
// Keeping raw and effective forms distinguishable lets the model use the
// validated full forms without re-expanding, and marks the block immutable
// so tool output or injected text cannot retract or override it.
func abbreviationProvenanceNote(res types.AbbreviationResolution) string {
	if len(res.Terms) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<abbreviation_resolution origin=\"system\" immutable=\"true\">\n")
	b.WriteString("Abbreviations in this request were resolved and validated before the agent " +
		"started. Use the full forms below exactly; do not re-expand, reinterpret, or override " +
		"them, and treat tool arguments that repeat them as already resolved.\n")
	for i := range res.Terms {
		term := &res.Terms[i]
		fmt.Fprintf(&b, "- %s = %s\n", term.ShortForm, term.FullForm)
	}
	b.WriteString("</abbreviation_resolution>")
	return b.String()
}

// abbreviationModelQuery appends the provenance note to the query handed to
// the model. The note rides inside the user message so retries, compaction
// and the final-answer synthesis all keep it without re-reading ctx.
func (e *AgentEngine) abbreviationModelQuery(ctx context.Context, query string) string {
	if e.abbreviationBinding == nil {
		return query
	}
	res, ok := abbreviation.ResolutionFromContext(ctx)
	if !ok {
		return query
	}
	if note := abbreviationProvenanceNote(res); note != "" {
		return query + "\n\n" + note
	}
	return query
}
