package session

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
	"github.com/gin-gonic/gin"
)

// emitAbbreviationPreparationError completes a turn whose abbreviation gate
// failed before the service entry could own completion. It uses the same
// EventError + EventAgentComplete pair executeQA emits for service errors so
// the SSE loop terminates; a user-requested cancellation logs without an
// error toast.
func (h *Handler) emitAbbreviationPreparationError(
	ctx context.Context, bus *event.EventBus, sessionID, messageID string, err error,
) {
	if ctx != nil && ctx.Err() != nil {
		logger.Infof(ctx, "abbreviation preparation cancelled for session %s", sessionID)
		return
	}
	if bus == nil {
		return
	}
	logger.ErrorWithFields(ctx, err, map[string]interface{}{"session_id": sessionID})
	if emitErr := bus.Emit(ctx, event.Event{
		Type:      event.EventError,
		SessionID: sessionID,
		Data: event.ErrorData{
			Error:     err.Error(),
			Stage:     "abbreviation_resolution",
			SessionID: sessionID,
		},
	}); emitErr != nil {
		logger.Warnf(ctx, "abbreviation error event failed for session %s: %v", sessionID, emitErr)
	}
	if emitErr := bus.Emit(ctx, event.Event{
		Type:      event.EventAgentComplete,
		SessionID: sessionID,
		Data: event.AgentCompleteData{
			MessageID: messageID,
		},
	}); emitErr != nil {
		logger.Warnf(ctx, "abbreviation completion event failed for session %s: %v", sessionID, emitErr)
	}
}

// provisionalSessionTitleRunes matches the persisted sessions.title budget
// used by model-generated titles so a provisional raw title never overflows.
const provisionalSessionTitleRunes = 100

// provisionalSessionTitle truncates the raw user query to a safe session
// title without any model call — the abbreviation gate forbids sending
// unresolved abbreviation text to a model.
func provisionalSessionTitle(query string) string {
	title := strings.TrimSpace(query)
	runes := []rune(title)
	if len(runes) <= provisionalSessionTitleRunes {
		return title
	}
	return strings.TrimSpace(string(runes[:provisionalSessionTitleRunes]))
}

// writeProvisionalSessionTitle persists the raw (truncated) query as the
// session title while a clarification turn is waiting, and emits the title
// event so the live stream shows it. Model-based title generation resumes on
// the continuation request once the gate seals a ready turn (see
// generateSessionTitle).
func (h *Handler) writeProvisionalSessionTitle(
	ctx context.Context, bus *event.EventBus, reqCtx *qaRequestContext,
) {
	session := reqCtx.session
	if session == nil || session.Title != "" {
		return
	}
	title := provisionalSessionTitle(reqCtx.query)
	if title == "" {
		return
	}
	session.Title = title
	if err := h.sessionService.UpdateSession(ctx, session); err != nil {
		logger.Warnf(ctx, "provisional session title write failed for session %s: %v", session.ID, err)
	}
	if bus != nil {
		_ = bus.Emit(ctx, event.Event{
			Type:      event.EventSessionTitle,
			SessionID: session.ID,
			Data: event.SessionTitleData{
				SessionID: session.ID,
				Title:     title,
			},
		})
	}
}

// generateSessionTitle starts model-based title generation once the
// abbreviation gate sealed a ready turn. The model is fed the validated
// effective query (expanded forms) rather than the raw text. A stored
// provisional title — exactly the truncated original query of a paused turn —
// is treated as absent so the resumed request still gets a real title.
func (h *Handler) generateSessionTitle(streamCtx *sseStreamContext, reqCtx *qaRequestContext) {
	session := reqCtx.session
	if session == nil {
		return
	}
	titleQuery := reqCtx.query
	provisional := ""
	if res, ok := abbreviation.ResolutionFromContext(streamCtx.asyncCtx); ok {
		if res.EffectiveQuery != "" {
			titleQuery = res.EffectiveQuery
		}
		provisional = provisionalSessionTitle(res.OriginalQuery)
	}
	if session.Title != "" && session.Title != provisional {
		return
	}
	modelID := ""
	if reqCtx.customAgent != nil && reqCtx.customAgent.Config.ModelID != "" {
		modelID = reqCtx.customAgent.Config.ModelID
	}
	// GenerateTitleAsync self-guards on a non-empty title; pass a copy so the
	// provisional provisional title we want replaced does not trip it.
	titleSession := *session
	titleSession.Title = ""
	logger.Infof(reqCtx.ctx, "Starting async title generation, session ID: %s, model: %s",
		reqCtx.sessionID, modelID)
	h.sessionService.GenerateTitleAsync(streamCtx.asyncCtx, &titleSession, titleQuery, modelID, streamCtx.eventBus)
}

// RetryAbbreviationSuggestions godoc
// @Summary      重试缩写建议写入
// @Description  重试处于 save_failed 状态的缩写词典建议写入
// @Tags         问答
// @Accept       json
// @Produce      json
// @Param        session_id  path      string  true   "会话ID"
// @Param        request_id  path      string  true   "缩写解析请求ID"
// @Success      200         {object}  map[string]interface{}  "重试成功"
// @Failure      404         {object}  errors.AppError         "会话或解析请求不存在"
// @Failure      409         {object}  errors.AppError         "解析请求状态不允许重试"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /sessions/{session_id}/abbreviation-clarifications/{request_id}/retry-suggestions [post]
func (h *Handler) RetryAbbreviationSuggestions(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	sessionID := secutils.SanitizeForLog(c.Param("session_id"))
	if sessionID == "" {
		sessionID = secutils.SanitizeForLog(c.Param("id"))
	}
	requestID := secutils.SanitizeForLog(c.Param("request_id"))
	if sessionID == "" || requestID == "" {
		c.Error(errors.NewBadRequestError("session_id and request_id are required"))
		return
	}

	err := h.sessionService.RetryAbbreviationSuggestions(ctx, sessionID, requestID)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"success": true})
	case stderrors.Is(err, types.ErrAbbreviationNotFound):
		c.Error(errors.NewNotFoundError(err.Error()))
	case stderrors.Is(err, types.ErrAbbreviationBadSelection):
		c.Error(errors.NewBadRequestError(err.Error()))
	case stderrors.Is(err, types.ErrAbbreviationConflict),
		stderrors.Is(err, types.ErrAbbreviationNotReady):
		c.Error(errors.NewConflictError(err.Error()))
	default:
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"session_id": sessionID,
			"request_id": requestID,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
	}
}
