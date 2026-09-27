package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

func (c *AbbreviationTurnCoordinator) persistSuggestions(ctx context.Context, owner types.AbbreviationOwner, row *types.AbbreviationTurnState) error {
	resolution := row.Payload.Resolution
	changed := false
	for i := range resolution.Terms {
		term := &resolution.Terms[i]
		if term.Source != types.AbbreviationSourceUserCurrent || term.Definition == nil ||
			term.SuggestionStatus == types.AbbreviationSuggestionPendingReview || term.SuggestionStatus == types.AbbreviationSuggestionActive {
			continue
		}
		changed = true
		term.SuggestionStatus = types.AbbreviationSuggestionSaveFailed
		if c.dictionary == nil {
			continue
		}
		result, err := c.dictionary.Suggest(ctx, &types.AbbreviationCreateRequest{ShortForm: term.ShortForm, FullForm: term.FullForm})
		if err != nil || result == nil {
			continue
		}
		term.SuggestionID = result.ID
		if result.IsActive {
			term.SuggestionStatus = types.AbbreviationSuggestionActive
		} else {
			term.SuggestionStatus = types.AbbreviationSuggestionPendingReview
		}
	}
	if !changed {
		return nil
	}
	return c.advance(ctx, owner, row, row.State, resolution)
}

func (c *AbbreviationTurnCoordinator) RetrySuggestions(ctx context.Context, owner types.AbbreviationOwner, requestID string) error {
	if c == nil || c.store == nil {
		return types.ErrAbbreviationBadSelection
	}
	row, err := c.store.Get(ctx, owner, requestID)
	if err != nil {
		return err
	}
	return c.persistSuggestions(ctx, owner, row)
}
