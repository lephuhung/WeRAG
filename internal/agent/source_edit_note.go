package agent

import (
	"context"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// An edit asked of a source document (a chat upload, looked up only) can
// only be answered with a proposal in the chat. The answer then ends with a
// fixed note saying so — code-appended like the amendment history, since
// Qwen ignores the prompt rule and told users to highlight text in a file
// that has no editor (2026-10-16).

// SourceEditNoteFunc returns the note for a turn whose @-mentions are all
// source documents and whose query asks for a change, "" otherwise.
type SourceEditNoteFunc func(ctx context.Context, query string) string

// SetSourceEditNote installs the source-edit note.
func (e *AgentEngine) SetSourceEditNote(fn SourceEditNoteFunc) {
	e.sourceEditNote = fn
}

// sourceEditTools are the tools whose success means the edit did land on a
// document (a target), so no note is due.
var sourceEditTools = map[string]bool{
	"rewrite_paragraphs": true,
	"insert_paragraphs":  true,
	"apply_format_fixes": true,
}

// editToolSucceeded reports whether an edit tool succeeded in the turn.
func editToolSucceeded(steps []types.AgentStep) bool {
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if call.Result != nil && call.Result.Success && sourceEditTools[call.ExecutionName()] {
				return true
			}
		}
	}
	return false
}

// appendSourceEditNote streams the note after the answer, on the answer's
// own event so the stored message carries it too.
func (e *AgentEngine) appendSourceEditNote(ctx context.Context, state *types.AgentState, sessionID, answerID, query string) {
	if e.sourceEditNote == nil || e.eventBus == nil || answerID == "" || state == nil || state.FinalAnswer == "" {
		return
	}
	if editToolSucceeded(state.RoundSteps) {
		return
	}
	note := e.sourceEditNote(ctx, query)
	if note == "" {
		return
	}
	block := "\n\n" + note + "\n"
	logger.Infof(ctx, "[Agent] Appending source-edit note")
	_ = e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Content: block},
	})
	state.FinalAnswer += block
}
