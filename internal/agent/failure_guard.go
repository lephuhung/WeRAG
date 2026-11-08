package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// The repeated-failure guard, seen on the dev DB (30 days to 2026-10-18):
// discover_mcp_tools called 38 times in 5 turns, 82 % with the same wrong
// server_id; call_mcp_tool retried on "Error executing tool generate_image";
// web_search called while not offered ("tool not found"). The round-boundary
// guard in turn_guards.go counts by the resolved MCP target and refuses
// the exhausted target when it is called again; the model also calls
// never-offered names. This guard sits where calls execute: after
// identicalFailureLimit consecutive failures of one tool name with the same
// normalised error in a turn, further calls are refused without running.
// Neither guard changes the offered tool list: the tool schema is part of
// the prompt prefix providers cache (see exhaustedTools).

// identicalFailureLimit is the number of consecutive identical failures of
// one tool after which it is disabled for the rest of the turn.
const identicalFailureLimit = 3

// toolDisabledMarker is the phrase every disabled-tool text carries; the
// round-boundary note in turn_guards.go is not added on top of it.
const toolDisabledMarker = "It is disabled for the rest of this turn."

// toolDisabledText is the model text of the failure that disabled a tool and
// of every later call to it in the turn.
const toolDisabledText = "Tool %s has failed %d times in this turn with the same error (%s). " +
	toolDisabledMarker + " Do not call it again: answer with what you have, or use a different tool."

// toolErrorExcerptRunes caps the error quoted in toolDisabledText.
const toolErrorExcerptRunes = 200

var (
	failureUUIDPattern  = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	failureHexIDPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8,}\b`)
	failureDigitPattern = regexp.MustCompile(`[0-9]+`)
)

// normalizeToolError is the comparison key of a failure: ids and numbers
// become '#', case and whitespace are folded, so "page 3" and "page 4" of
// the same rejection count as one error.
func normalizeToolError(msg string) string {
	msg = strings.ToLower(strings.TrimSpace(msg))
	msg = failureUUIDPattern.ReplaceAllString(msg, "#")
	msg = failureHexIDPattern.ReplaceAllStringFunc(msg, func(s string) string {
		if strings.ContainsAny(s, "0123456789") {
			return "#"
		}
		return s // a plain word such as "deadbeef" without digits
	})
	msg = failureDigitPattern.ReplaceAllString(msg, "#")
	return strings.Join(strings.Fields(msg), " ")
}

// toolFailureRun is the current run of identical failures of one tool.
type toolFailureRun struct {
	key     string // normalizeToolError of the failures
	excerpt string // the first failure's error, cut to toolErrorExcerptRunes
	count   int
}

// toolFailureGuard holds a turn's failure runs and disabled tools. Like
// searchHistory it is keyed by the assistant message id and starts over when
// that changes, so an engine reused across turns carries nothing over.
// Safe for the parallel tool path.
type toolFailureGuard struct {
	mu        sync.Mutex
	messageID string
	runs      map[string]*toolFailureRun
	disabled  map[string]string // tool name -> toolDisabledText
}

// resetLocked starts a new turn's state when messageID changed.
func (g *toolFailureGuard) resetLocked(messageID string) {
	if g.runs == nil || g.messageID != messageID {
		g.messageID = messageID
		g.runs = map[string]*toolFailureRun{}
		g.disabled = map[string]string{}
	}
}

// disabledText returns the refusal for a tool disabled in this turn.
func (g *toolFailureGuard) disabledText(messageID, name string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resetLocked(messageID)
	text, ok := g.disabled[name]
	return text, ok
}

// record adds a finished call. A success ends the tool's run; a failure
// extends it when its normalised error matches, or starts a new one. It
// returns the disabled text when this failure reached identicalFailureLimit.
func (g *toolFailureGuard) record(messageID, name string, success bool, errMsg string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resetLocked(messageID)
	if success {
		delete(g.runs, name)
		return "", false
	}
	key := normalizeToolError(errMsg)
	run := g.runs[name]
	if run == nil || run.key != key {
		run = &toolFailureRun{key: key, excerpt: truncateRunes(strings.Join(strings.Fields(errMsg), " "), toolErrorExcerptRunes)}
		g.runs[name] = run
	}
	run.count++
	if run.count < identicalFailureLimit {
		return "", false
	}
	text := fmt.Sprintf(toolDisabledText, name, run.count, run.excerpt)
	g.disabled[name] = text
	return text, true
}

// refuseExhaustedTool returns the refused call for a tool exhausted in this
// turn (exhaustedTools), without running it; ok is false when it may run.
// exhaustedTools keys by the executed name, so a call through the MCP proxy
// is matched by its resolved target as well, and the refused call carries
// that target so its ExecutionName reads the same as the calls that ran.
func (e *AgentEngine) refuseExhaustedTool(ctx context.Context, tc types.LLMToolCall, round int) (types.ToolCall, bool) {
	if len(e.exhausted) == 0 {
		return types.ToolCall{}, false
	}
	name := tc.Function.Name
	var target *types.ToolCallTarget
	if e.toolRegistry != nil && len(tc.UnresolvedHandles) == 0 {
		if target = e.toolRegistry.MCPCallTarget(ctx, name, json.RawMessage(tc.Function.Arguments)); target != nil {
			name = target.Name
		}
	}
	text, off := e.exhausted[name]
	if !off {
		return types.ToolCall{}, false
	}
	logger.Infof(ctx, "[Agent][Round-%d] Refusing call to %s: exhausted this turn", round, name)
	refused := unrunToolCall(tc, text)
	refused.Target = target
	refused.Result.Refused = true
	return refused, true
}

// refuseDisabledTool returns the refused call for a tool disabled in this
// turn, without running it; ok is false when the tool may run.
func (e *AgentEngine) refuseDisabledTool(ctx context.Context, tc types.LLMToolCall, round int, assistantMessageID string) (types.ToolCall, bool) {
	text, off := e.failureGuard.disabledText(assistantMessageID, tc.Function.Name)
	if !off {
		return types.ToolCall{}, false
	}
	logger.Infof(ctx, "[Agent][Round-%d] Refusing call to %s: disabled after %d identical failures this turn",
		round, tc.Function.Name, identicalFailureLimit)
	refused := unrunToolCall(tc, text)
	refused.Result.Refused = true
	return refused, true
}

// recordToolOutcome feeds a finished call to the guard and, when it disabled
// the tool, appends the disabled text to the call's error so the model reads
// the stop instruction with the result itself.
func (e *AgentEngine) recordToolOutcome(ctx context.Context, toolCall *types.ToolCall, round int, assistantMessageID string) {
	if toolCall.Result == nil {
		return
	}
	text, disabled := e.failureGuard.record(assistantMessageID, toolCall.Name,
		toolCall.Result.Success, toolCall.Result.Error)
	if !disabled {
		return
	}
	logger.Infof(ctx, "[Agent][Round-%d] Tool %s failed %d times with the same error; disabled for the rest of the turn",
		round, toolCall.Name, identicalFailureLimit)
	if strings.TrimSpace(toolCall.Result.Error) == "" {
		toolCall.Result.Error = text
	} else {
		toolCall.Result.Error += "\n\n[" + text + "]"
	}
}
