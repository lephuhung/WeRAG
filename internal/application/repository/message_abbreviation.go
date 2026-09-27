package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// messageWithSessionMessages flattens the joined results to their embedded
// Message so hydration treats every read path identically.
func messageWithSessionMessages(results []*types.MessageWithSession) []*types.Message {
	messages := make([]*types.Message, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		messages = append(messages, &result.Message)
	}
	return messages
}

// abbreviationPublicStateForMessage projects a persisted turn row to the
// public state attached to its linked messages. The projection follows the
// live row rather than the payload's stale resolution status: a paused turn
// reads needs_definition so the reply form renders, an expired wait reads
// expired so the old form can no longer be submitted, and terminal/executing
// states surface verbatim. Private fields (owner, principal, snapshot,
// definition spans) never leave AbbreviationPublicState.
func abbreviationPublicStateForMessage(
	state *types.AbbreviationTurnState, now time.Time,
) *types.AbbreviationPublicState {
	if state == nil {
		return nil
	}
	public := state.Payload.Resolution.PublicState()
	public.RequestID = state.ID
	public.RootUserMessageID = state.RootUserMessageID
	public.Version = state.Version
	public.ExpiresAt = state.ExpiresAt
	if state.State == types.AbbreviationTurnAwaitingDefinition {
		if now.Before(state.ExpiresAt) {
			public.Status = types.AbbreviationStatusNeedsDefinition
		} else {
			public.Status = types.AbbreviationTurnExpired
		}
		return &public
	}
	public.Status = state.State
	return &public
}

// hydrateAbbreviationState attaches the public abbreviation state to every
// message linked to a turn row, on every read path. Links join live state —
// nothing is copied onto the message row, so a stale answer never reports a
// turn as waiting. A link whose turn belongs to a different session or tenant
// than the message is dropped rather than projected. Infrastructure failures
// abort the read: a load error must never present as "no clarification".
func (r *messageRepository) hydrateAbbreviationState(
	ctx context.Context, messages []*types.Message,
) error {
	if len(messages) == 0 {
		return nil
	}
	ids := make([]string, 0, len(messages))
	sessionIDs := map[string]struct{}{}
	for _, message := range messages {
		if message == nil || message.ID == "" {
			continue
		}
		ids = append(ids, message.ID)
		sessionIDs[message.SessionID] = struct{}{}
	}
	if len(ids) == 0 {
		return nil
	}

	var links []types.AbbreviationMessageLink
	if err := r.db.WithContext(ctx).
		Where("message_id IN ?", ids).Find(&links).Error; err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}

	requestIDs := make([]string, 0, len(links))
	seenRequests := map[string]struct{}{}
	for _, link := range links {
		if _, seen := seenRequests[link.RequestID]; seen {
			continue
		}
		seenRequests[link.RequestID] = struct{}{}
		requestIDs = append(requestIDs, link.RequestID)
	}
	var states []types.AbbreviationTurnState
	if err := r.db.WithContext(ctx).
		Where("id IN ?", requestIDs).Find(&states).Error; err != nil {
		return err
	}
	if len(states) == 0 {
		return nil
	}

	// Sessions carry the tenant a linked turn must match; a state that names
	// a different session than the message, or whose tenant disagrees with
	// the session row, is not this conversation's turn.
	sessionList := make([]string, 0, len(sessionIDs))
	for id := range sessionIDs {
		sessionList = append(sessionList, id)
	}
	var sessionRows []struct {
		ID       string `gorm:"column:id"`
		TenantID uint64 `gorm:"column:tenant_id"`
	}
	if err := r.db.WithContext(ctx).
		Table("sessions").Select("id", "tenant_id").
		Where("id IN ?", sessionList).Find(&sessionRows).Error; err != nil {
		return err
	}
	sessionTenants := make(map[string]uint64, len(sessionRows))
	for _, row := range sessionRows {
		sessionTenants[row.ID] = row.TenantID
	}

	statesByID := make(map[string]*types.AbbreviationTurnState, len(states))
	for i := range states {
		statesByID[states[i].ID] = &states[i]
	}
	linksByMessage := map[string]string{}
	for _, link := range links {
		linksByMessage[link.MessageID] = link.RequestID
	}

	now := time.Now()
	for _, message := range messages {
		if message == nil {
			continue
		}
		requestID, ok := linksByMessage[message.ID]
		if !ok {
			continue
		}
		state, ok := statesByID[requestID]
		if !ok || state.SessionID != message.SessionID {
			continue
		}
		if tenant, exists := sessionTenants[message.SessionID]; !exists || tenant != state.TenantID {
			continue
		}
		message.Abbreviation = abbreviationPublicStateForMessage(state, now)
	}
	return nil
}
