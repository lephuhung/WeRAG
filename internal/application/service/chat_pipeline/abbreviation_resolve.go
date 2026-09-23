package chatpipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
	"github.com/google/uuid"
)

const abbreviationResolveTool = "resolve_abbreviation"

type PluginAbbreviationResolve struct {
	abbreviationService interfaces.AbbreviationService
}

func NewPluginAbbreviationResolve(
	eventManager *EventManager,
	abbreviationService interfaces.AbbreviationService,
) *PluginAbbreviationResolve {
	res := &PluginAbbreviationResolve{abbreviationService: abbreviationService}
	eventManager.Register(res)
	return res
}

func (p *PluginAbbreviationResolve) ActivationEvents() []types.EventType {
	return []types.EventType{types.ABBREVIATION_RESOLVE}
}

func (p *PluginAbbreviationResolve) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	input := strings.TrimSpace(chatManage.Query)
	if input == "" {
		input = strings.TrimSpace(chatManage.RewriteQuery)
	}
	resolved, result, err := resolveAbbreviationForTurn(
		ctx, p.abbreviationService, chatManage.RewriteQuery, chatManage.Query,
	)
	if err != nil {
		pipelineWarn(ctx, "AbbreviationResolve", "abbreviation_resolution_error", map[string]interface{}{
			"error": err.Error(),
		})
		return next()
	}
	if input == "" {
		input = resolved
	}
	if result != nil {
		emitAbbreviationResolution(ctx, chatManage, input, result)
		chatManage.RewriteQuery = resolved
	}
	return next()
}

func resolveAbbreviationForTurn(
	ctx context.Context,
	svc interfaces.AbbreviationService,
	effective, original string,
) (string, *abbreviation.ExpandResult, error) {
	effective = strings.TrimSpace(effective)
	original = strings.TrimSpace(original)
	if effective == "" {
		effective = original
	}
	resolved, result, err := abbreviation.ResolveSearchQuery(ctx, effective, svc)
	if err != nil {
		return effective, nil, err
	}
	if original == "" || original == effective {
		return resolved, result, nil
	}
	_, originalResult, origErr := abbreviation.ResolveSearchQuery(ctx, original, svc)
	if origErr != nil {
		return resolved, result, origErr
	}
	if originalResult == nil {
		return resolved, result, nil
	}
	if len(originalResult.Potential) == 0 && len(originalResult.Ambiguous) == 0 {
		return resolved, result, nil
	}
	if result == nil {
		result = &abbreviation.ExpandResult{Original: effective, Expanded: resolved}
	}
	seenPotential := make(map[string]struct{}, len(result.Potential))
	for _, candidate := range result.Potential {
		seenPotential[strings.ToLower(candidate)] = struct{}{}
	}
	for _, candidate := range originalResult.Potential {
		key := strings.ToLower(candidate)
		if _, ok := seenPotential[key]; ok {
			continue
		}
		seenPotential[key] = struct{}{}
		result.Potential = append(result.Potential, candidate)
	}
	for shortForm, meanings := range originalResult.Ambiguous {
		exists := false
		for existing := range result.Ambiguous {
			if strings.EqualFold(existing, shortForm) {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if result.Ambiguous == nil {
			result.Ambiguous = make(map[string][]abbreviation.Meaning)
		}
		result.Ambiguous[shortForm] = meanings
	}
	return resolved, result, nil
}

func emitAbbreviationToolCall(
	ctx context.Context, cm *types.ChatManage, args map[string]any,
) string {
	if cm == nil || cm.EventBus == nil {
		return ""
	}
	callID := uuid.NewString()
	_ = cm.EventBus.Emit(ctx, types.Event{
		Type:      types.EventType(event.EventAgentToolCall),
		SessionID: cm.SessionID,
		Data: event.AgentToolCallData{
			ToolCallID: callID,
			ToolName:   abbreviationResolveTool,
			Arguments:  args,
		},
	})
	return callID
}

func emitAbbreviationToolResult(
	ctx context.Context, cm *types.ChatManage, callID string,
	success bool, output, errMsg string, data map[string]interface{},
) {
	if cm == nil || cm.EventBus == nil || callID == "" {
		return
	}
	_ = cm.EventBus.Emit(ctx, types.Event{
		Type:      types.EventType(event.EventAgentToolResult),
		SessionID: cm.SessionID,
		Data: event.AgentToolResultData{
			ToolCallID: callID,
			ToolName:   abbreviationResolveTool,
			Output:     output,
			Error:      errMsg,
			Success:    success,
			Data:       data,
		},
	})
}

func emitAbbreviationResolution(
	ctx context.Context, cm *types.ChatManage, input string, result *abbreviation.ExpandResult,
) {
	if result == nil || (len(result.Potential) == 0 && len(result.Ambiguous) == 0) {
		return
	}
	callID := emitAbbreviationToolCall(ctx, cm, map[string]any{
		"action": "expand",
		"text":   input,
	})
	var b strings.Builder
	for _, candidate := range result.Potential {
		fmt.Fprintf(&b, "unknown_candidate: %s\n", candidate)
	}
	ambiguousKeys := make([]string, 0, len(result.Ambiguous))
	for shortForm := range result.Ambiguous {
		ambiguousKeys = append(ambiguousKeys, shortForm)
	}
	sort.Strings(ambiguousKeys)
	for _, shortForm := range ambiguousKeys {
		fmt.Fprintf(&b, "ambiguous: %s could mean", shortForm)
		for _, meaning := range result.Ambiguous[shortForm] {
			fmt.Fprintf(&b, " \"%s\"", meaning.FullForm)
		}
		b.WriteString("\n")
	}
	emitAbbreviationToolResult(ctx, cm, callID, true, b.String(), "",
		map[string]interface{}{"result": result})
}
