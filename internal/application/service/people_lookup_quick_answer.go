package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/people"
	"github.com/google/uuid"
)

type quickPeopleLookupService interface {
	Enabled() bool
	SearchByCCCD(context.Context, string) *people.SearchResult
	SearchByBHXH(context.Context, string) *people.SearchResult
	SearchByPhone(context.Context, string) *people.SearchResult
	SearchByName(context.Context, string, int64) *people.SearchResult
}

func canUsePeopleLookup(ctx context.Context) bool {
	return types.IsSystemAdminFromContext(ctx)
}

func (s *sessionService) tryQuickAnswerPeopleLookup(
	ctx context.Context, req *types.QARequest, eventBus *event.EventBus,
) (bool, error) {
	if eventBus == nil || s.peopleService == nil || !s.peopleService.Enabled() || !canUsePeopleLookup(ctx) {
		return false, nil
	}
	request, ok := people.DetectLookupRequest(req.Query)
	if !ok {
		return false, nil
	}
	sessionID := req.Session.ID

	toolCallID := uuid.New().String()
	args := map[string]any{
		"lookup_type": request.LookupType,
		"query":       request.Query,
	}
	if request.LookupType == "name" {
		args["limit"] = request.Limit
	}
	if err := eventBus.Emit(ctx, event.Event{
		Type:      event.EventAgentToolCall,
		SessionID: sessionID,
		Data: event.AgentToolCallData{
			ToolCallID: toolCallID,
			ToolName:   tools.ToolPeopleLookup,
			Arguments:  args,
		},
	}); err != nil {
		return true, fmt.Errorf("emit people_lookup tool call: %w", err)
	}

	start := time.Now()
	result := s.executeQuickPeopleLookup(ctx, request)
	duration := time.Since(start).Milliseconds()

	if result == nil {
		err := stderrors.New("people lookup returned no result")
		if emitErr := eventBus.Emit(ctx, event.Event{
			Type:      event.EventAgentToolResult,
			SessionID: sessionID,
			Data: event.AgentToolResultData{
				ToolCallID: toolCallID,
				ToolName:   tools.ToolPeopleLookup,
				Error:      err.Error(),
				Success:    false,
				Duration:   duration,
			},
		}); emitErr != nil {
			return true, fmt.Errorf("emit failed people_lookup result: %w", emitErr)
		}
		return true, err
	}

	output := strings.TrimSpace(result.Display)
	if output == "" {
		output = "Không tìm thấy dữ liệu phù hợp."
	}
	if err := eventBus.Emit(ctx, event.Event{
		Type:      event.EventAgentToolResult,
		SessionID: sessionID,
		Data: event.AgentToolResultData{
			ToolCallID: toolCallID,
			ToolName:   tools.ToolPeopleLookup,
			Output:     output,
			Success:    true,
			Duration:   duration,
			Data: map[string]interface{}{
				"found":       result.Found,
				"persons":     result.Persons,
				"schemas":     result.Schemas,
				"lookup_type": result.LookupType,
				"unavailable": result.Unavailable,
			},
		},
	}); err != nil {
		return true, fmt.Errorf("emit people_lookup result: %w", err)
	}
	if request.ContinuePipeline {
		req.QuotedContext = appendPeopleLookupContext(req.QuotedContext, output, result.Unavailable)
		return false, nil
	}
	// The structured persons payload renders as person cards in the UI — when
	// the lookup found records, the text answer is just the "found N people"
	// headline; streaming the whole profile dump would duplicate the cards.
	answer := output
	if result.Found {
		if line, _, ok := strings.Cut(output, "\n"); ok && strings.TrimSpace(line) != "" {
			answer = strings.TrimSpace(line)
		}
	}
	if err := eventBus.Emit(ctx, event.Event{
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Content: answer},
	}); err != nil {
		return true, fmt.Errorf("emit people_lookup answer: %w", err)
	}
	if err := eventBus.Emit(ctx, event.Event{
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data:      event.AgentFinalAnswerData{Done: true},
	}); err != nil {
		return true, fmt.Errorf("complete people_lookup answer: %w", err)
	}
	return true, nil
}

func appendPeopleLookupContext(existing, output string, unavailable bool) string {
	var b strings.Builder
	if strings.TrimSpace(existing) != "" {
		b.WriteString(existing)
		b.WriteString("\n\n")
	}
	b.WriteString("<people_lookup>\n")
	b.WriteString(output)
	b.WriteString("\n</people_lookup>")
	if unavailable {
		b.WriteString("\nHệ thống tra cứu đang không khả dụng; không được diễn giải kết quả này thành không tìm thấy người.")
	}
	return b.String()
}

func (s *sessionService) executeQuickPeopleLookup(
	ctx context.Context, request people.LookupRequest,
) *people.SearchResult {
	switch request.LookupType {
	case "cccd":
		return s.peopleService.SearchByCCCD(ctx, request.Query)
	case "bhxh":
		return s.peopleService.SearchByBHXH(ctx, request.Query)
	case "phone":
		return s.peopleService.SearchByPhone(ctx, request.Query)
	case "name":
		return s.peopleService.SearchByName(ctx, request.Query, request.Limit)
	default:
		return nil
	}
}
