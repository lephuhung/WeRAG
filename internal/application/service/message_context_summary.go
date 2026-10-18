package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

// LatestContextSummary returns the summary the quick-answer pipeline starts a
// session's history from: the newest context checkpoint (written by agent
// compaction) and the time of the turn it covers through, or — for a session
// continued from another that has no checkpoint yet — its context seed with a
// zero time (it covers no turn of this session). "" when there is neither.
// The caller must be allowed to read the session.
func (s *messageService) LatestContextSummary(
	ctx context.Context, sessionID string,
) (string, time.Time, error) {
	tenantID, ok := sessionTenantIDForLookup(ctx)
	if !ok {
		return "", time.Time{}, errors.New("organization ID not found in context")
	}
	session, err := loadSessionForRead(ctx, s.sessionRepo, tenantID, sessionUserIDForLookup(ctx), sessionID)
	if err != nil {
		return "", time.Time{}, err
	}
	if checkpoint := loadContextCheckpoint(ctx, s.messageRepo, sessionID); checkpoint != nil {
		return checkpoint.ContextCheckpoint.Summary, checkpoint.CreatedAt, nil
	}
	if seed := strings.TrimSpace(session.ContextSeed); seed != "" {
		logger.Debugf(ctx, "Session %s starts from its context seed (%d chars)", sessionID, len(seed))
		return seed, time.Time{}, nil
	}
	return "", time.Time{}, nil
}
