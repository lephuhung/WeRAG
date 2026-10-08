package session

import (
	"context"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/stream"
)

// liveRunHeartbeater is implemented by stream managers that can tell a live
// run whose process died from one still generating (Redis). The in-memory
// manager dies with its process and needs neither.
type liveRunHeartbeater interface {
	HeartbeatLiveRun(ctx context.Context, sessionID, assistantMessageID string) (bool, error)
	LiveRunAlive(ctx context.Context, sessionID, assistantMessageID string) (bool, error)
}

// liveRunHeartbeats holds the assistant messages this process is renewing,
// so a follow-up run started from a claim is not renewed twice.
var liveRunHeartbeats sync.Map

// liveRunHeartbeatMax bounds a heartbeat whose run never cleared its marker.
const liveRunHeartbeatMax = 3 * time.Hour

// interruptedTurnContent replaces the empty answer of a turn whose process
// died before it finished.
const interruptedTurnContent = "Lượt trả lời này bị gián đoạn (máy chủ đã khởi động lại hoặc gặp sự cố). Vui lòng gửi lại câu hỏi."

// startLiveRunHeartbeat renews the run's heartbeat until the live-run marker
// stops naming it (the run finished or was handed off) or ctx ends.
func (h *Handler) startLiveRunHeartbeat(ctx context.Context, sessionID, assistantMessageID string) {
	hb, ok := h.streamManager.(liveRunHeartbeater)
	if !ok || sessionID == "" || assistantMessageID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if _, err := hb.HeartbeatLiveRun(ctx, sessionID, assistantMessageID); err != nil {
		logger.Warnf(ctx, "live run heartbeat failed for session %s: %v", sessionID, err)
	}
	if _, running := liveRunHeartbeats.LoadOrStore(assistantMessageID, struct{}{}); running {
		return
	}
	go func() {
		defer liveRunHeartbeats.Delete(assistantMessageID)
		ticker := time.NewTicker(stream.LiveRunHeartbeatInterval)
		defer ticker.Stop()
		deadline := time.Now().Add(liveRunHeartbeatMax)
		for now := range ticker.C {
			if now.After(deadline) {
				return
			}
			live, err := hb.HeartbeatLiveRun(ctx, sessionID, assistantMessageID)
			if err != nil {
				logger.Warnf(ctx, "live run heartbeat failed for session %s: %v", sessionID, err)
				continue
			}
			if !live {
				return
			}
		}
	}()
}

// clearDeadLiveRun drops the session's live-run marker when the process that
// ran that turn is gone (no heartbeat), closes the turn's unfinished answer,
// and reports whether a new turn may start.
func (h *Handler) clearDeadLiveRun(ctx context.Context, sessionID string) bool {
	hb, ok := h.streamManager.(liveRunHeartbeater)
	if !ok {
		return false
	}
	liveID, _, err := h.streamManager.GetLiveRun(ctx, sessionID)
	if err != nil || liveID == "" {
		return false
	}
	alive, err := hb.LiveRunAlive(ctx, sessionID, liveID)
	if err != nil || alive {
		return false
	}
	if err := h.streamManager.ClearLiveRun(ctx, sessionID, liveID); err != nil {
		logger.Warnf(ctx, "clearing dead live run %s of session %s failed: %v", liveID, sessionID, err)
		return false
	}
	logger.Warnf(ctx, "session %s: live run %s had no heartbeat (its process died); cleared", sessionID, liveID)
	if h.messageService != nil {
		if msg, err := h.messageService.GetMessage(ctx, sessionID, liveID); err == nil && msg != nil && !msg.IsCompleted {
			if msg.Content == "" {
				msg.Content = interruptedTurnContent
			}
			msg.IsCompleted = true
			if err := h.messageService.UpdateMessage(ctx, msg); err != nil {
				logger.Warnf(ctx, "closing interrupted message %s failed: %v", liveID, err)
			}
		}
	}
	return true
}
