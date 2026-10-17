package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// continuationRecentTurns bounds how many turns after the old session's
	// checkpoint the seed quotes; the checkpoint covers everything before.
	continuationRecentTurns = 8
	// continuationTurnRunes caps each quoted answer, and continuationSeedRunes
	// the whole seed (about 8k tokens): the seed is replayed every turn of the
	// new session until its first checkpoint folds it in.
	continuationTurnRunes = 2000
	continuationSeedRunes = 24000
	// continuationPageSize is the newest rows read for the recent turns.
	continuationPageSize = 4 * continuationRecentTurns
)

// ContinueSession opens a new session that carries on from old once old
// reached its plan's per-session cap (or the user chose to): same owner,
// workspace and input-bar state, and a context seed the new session's first
// turn starts from (types.Session.ContextSeed). The seed is old's latest
// context checkpoint — or its own seed when it never compacted — followed by
// short excerpts of the turns the checkpoint does not cover. It is built from
// stored text alone, no model call, so continuing is instant and cannot fail
// on a summarizer.
func (s *sessionService) ContinueSession(ctx context.Context, old *types.Session) (*types.Session, error) {
	return s.continueSession(ctx, old, "")
}

// OpenSubChat opens a new sub-chat of from's document chat: a continued
// session (summary seed) that works on the root chat's documents.
func (s *sessionService) OpenSubChat(ctx context.Context, from *types.Session) (*types.Session, error) {
	if from == nil || from.ID == "" {
		return nil, fmt.Errorf("open sub-chat: no session")
	}
	root := from.ChatRootID
	if root == "" {
		root = from.ID
	}
	return s.continueSession(ctx, from, root)
}

// ListSubChats returns the root of s's document chat and its sub-chats.
func (s *sessionService) ListSubChats(ctx context.Context, sess *types.Session) (string, []*types.Session, error) {
	root := sess.ChatRootID
	if root == "" {
		root = sess.ID
	}
	chats, err := s.sessionRepo.ListChatFamily(ctx, sess.TenantID, sess.UserID, root)
	return root, chats, err
}

func (s *sessionService) continueSession(ctx context.Context, old *types.Session, chatRoot string) (*types.Session, error) {
	if old == nil || old.ID == "" {
		return nil, fmt.Errorf("continue session: no session")
	}
	next := &types.Session{
		TenantID:         old.TenantID,
		UserID:           old.UserID,
		Title:            continuedSessionTitle(old.Title),
		Description:      old.Description,
		LastRequestState: old.LastRequestState,
		ContextSeed:      s.continuationSeed(ctx, old),
		ChatRootID:       chatRoot,
	}
	created, err := s.CreateSession(ctx, next)
	if err != nil {
		return nil, fmt.Errorf("continue session %s: %w", old.ID, err)
	}
	logger.Infof(ctx, "Session %s continues session %s (seed %d runes)",
		created.ID, old.ID, utf8.RuneCountInString(next.ContextSeed))
	return created, nil
}

// continuedSessionTitle marks a continued session in the list.
func continuedSessionTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" || strings.HasSuffix(title, " (tiếp)") {
		return title
	}
	return title + " (tiếp)"
}

// continuationSeed is what the continued session starts from: old's summary
// and excerpts of the newer turns it does not cover. A failed read degrades
// to whatever was read; an empty seed is a fresh start.
func (s *sessionService) continuationSeed(ctx context.Context, old *types.Session) string {
	summary := strings.TrimSpace(old.ContextSeed)
	var checkpoint *types.Message
	if s.messageRepo != nil {
		checkpoint = loadContextCheckpoint(ctx, s.messageRepo, old.ID)
	}
	if checkpoint != nil {
		summary = strings.TrimSpace(checkpoint.ContextCheckpoint.Summary)
	}
	var sb strings.Builder
	if summary != "" {
		sb.WriteString(summary)
	}
	if recent := s.recentTurnExcerpts(ctx, old.ID, checkpoint); recent != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## Most recent turns of the previous session (excerpts)\n\n")
		sb.WriteString(recent)
	}
	return truncateRunesKeepHead(sb.String(), continuationSeedRunes)
}

// recentTurnExcerpts quotes the newest completed turns after checkpoint
// (all newest turns when there is none), oldest first.
func (s *sessionService) recentTurnExcerpts(ctx context.Context, sessionID string, checkpoint *types.Message) string {
	if s.messageRepo == nil {
		return ""
	}
	rows, err := s.messageRepo.ListMessagesBySessionBeforeCursor(ctx, sessionID, time.Time{}, "", continuationPageSize)
	if err != nil {
		logger.Warnf(ctx, "Continue session %s: recent turns unavailable: %v", sessionID, err)
		return ""
	}
	turns := completeHistoryTurns(rows, len(rows) < continuationPageSize)
	turns = turnsAfterCheckpoint(turns, checkpoint)
	if len(turns) > continuationRecentTurns {
		turns = turns[len(turns)-continuationRecentTurns:]
	}
	var sb strings.Builder
	for _, t := range turns {
		question := strings.TrimSpace(t.users[0].Content)
		answer := strings.TrimSpace(agentHistoryThinkTagRegex.ReplaceAllString(t.assistant.Content, ""))
		if question == "" && answer == "" {
			continue
		}
		fmt.Fprintf(&sb, "User: %s\nAssistant: %s\n\n",
			truncateRunesKeepHead(question, continuationTurnRunes),
			truncateRunesKeepHead(answer, continuationTurnRunes))
	}
	return strings.TrimSpace(sb.String())
}

// truncateRunesKeepHead cuts s to n runes, marking the cut.
func truncateRunesKeepHead(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + " […]"
}
