package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
	"github.com/google/uuid"
)

type AbbreviationPrepareInput struct {
	Binding                  types.AbbreviationBinding
	ContinuationID           string
	ExpectedVersion          *uint64
	Snapshot                 types.AbbreviationRequestSnapshot
	ModelID, RelevantHistory string
	ResolveModelID           func(context.Context) (string, error)
}

type AbbreviationPreparer interface {
	Prepare(context.Context, AbbreviationPrepareInput) (types.AbbreviationResolution, error)
}

type AbbreviationTurnCoordinator struct {
	store      interfaces.AbbreviationTurnRepository
	dictionary interfaces.AbbreviationService
	selector   AbbreviationMeaningSelector
	now        func() time.Time
}

func NewAbbreviationTurnCoordinator(store interfaces.AbbreviationTurnRepository, dictionary interfaces.AbbreviationService, selector AbbreviationMeaningSelector) *AbbreviationTurnCoordinator {
	return &AbbreviationTurnCoordinator{store: store, dictionary: dictionary, selector: selector, now: time.Now}
}

func (c *AbbreviationTurnCoordinator) Prepare(ctx context.Context, in AbbreviationPrepareInput) (types.AbbreviationResolution, error) {
	var zero types.AbbreviationResolution
	b := in.Binding
	if c == nil || c.store == nil || b.Owner.TenantID == 0 || b.Owner.SessionID == "" || b.Owner.PrincipalID == "" ||
		b.UserMessageID == "" || b.AssistantMessageID == "" || b.RawQuery == "" {
		return zero, types.ErrAbbreviationBadSelection
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if in.ContinuationID != "" {
		if in.ExpectedVersion == nil {
			return zero, types.ErrAbbreviationConflict
		}
		state, err := c.store.Get(ctx, b.Owner, in.ContinuationID)
		if err != nil {
			return zero, err
		}
		return c.continueTurn(ctx, in, state)
	}
	awaiting, err := c.store.Awaiting(ctx, b.Owner, c.now())
	if err != nil {
		return zero, err
	}
	if awaiting != nil {
		parsed, err := abbreviation.ParseUserDefinitions(b.RawQuery, b.UserMessageID, awaiting.Payload.Resolution.UnknownTerms, len(awaiting.Payload.Resolution.UnknownTerms) == 1)
		if err != nil {
			return zero, err
		}
		if parsed.IsDefinitionReply {
			return c.continueTurn(ctx, in, awaiting)
		}
	}
	return c.beginTurn(ctx, in)
}

func (c *AbbreviationTurnCoordinator) beginTurn(ctx context.Context, in AbbreviationPrepareInput) (types.AbbreviationResolution, error) {
	b := in.Binding
	probe := abbreviation.Inspect(b.RawQuery, nil)
	candidates := make([]string, 0, len(probe.Terms))
	for _, term := range probe.Terms {
		candidates = append(candidates, term.ShortForm)
	}
	parsed, err := abbreviation.ParseUserDefinitions(b.RawQuery, b.UserMessageID, candidates, false)
	if err != nil {
		return types.AbbreviationResolution{}, err
	}
	resolution := probe
	resolution.RequestID = uuid.NewString()
	resolution.RootUserMessageID = b.UserMessageID
	resolution.CurrentUserMessageID = b.UserMessageID
	resolution.ExpiresAt = c.now().Add(24 * time.Hour)
	resolution.Version = 1
	row := &types.AbbreviationTurnState{
		ID: resolution.RequestID, TenantID: b.Owner.TenantID, SessionID: b.Owner.SessionID,
		OwnerID: b.Owner.OwnerID, PrincipalID: b.Owner.PrincipalID, RootUserMessageID: b.UserMessageID,
		State: types.AbbreviationTurnInspecting, Version: 1, ExpiresAt: resolution.ExpiresAt,
		Payload: types.AbbreviationTurnPayload{Snapshot: in.Snapshot, Resolution: resolution},
	}
	if err := c.store.Begin(ctx, b.Owner, row); err != nil {
		return types.AbbreviationResolution{}, err
	}
	if err := c.store.LinkMessages(ctx, b.Owner, row.ID, []types.AbbreviationMessageLink{{MessageID: b.UserMessageID, RequestID: row.ID, Role: types.AbbreviationMessageRoleRoot}}); err != nil {
		return types.AbbreviationResolution{}, err
	}
	lookupFailed := false
	if len(candidates) != 0 {
		if c.dictionary == nil {
			lookupFailed = true
		} else if active, listErr := c.dictionary.ListActive(ctx); listErr == nil {
			resolution = abbreviation.Inspect(b.RawQuery, active)
		} else {
			lookupFailed = true
		}
	}
	resolution, err = abbreviation.ApplyDefinitions(resolution, parsed.Definitions)
	if err != nil {
		return types.AbbreviationResolution{}, err
	}
	resolution.RequestID = row.ID
	resolution.RootUserMessageID = b.UserMessageID
	resolution.CurrentUserMessageID = b.UserMessageID
	resolution.ExpiresAt = row.ExpiresAt
	resolution.Version = row.Version
	if lookupFailed && len(resolution.UnknownTerms) != 0 {
		resolution.Status = types.AbbreviationStatusBlockedError
		resolution.ErrorCode = "dictionary_unavailable"
		resolution.EffectiveQuery = ""
	}
	assistantRole := types.AbbreviationMessageRoleAnswer
	if resolution.Status == types.AbbreviationStatusNeedsDefinition {
		assistantRole = types.AbbreviationMessageRoleClarification
	}
	if err := c.linkAssistant(ctx, b, row, assistantRole); err != nil {
		return types.AbbreviationResolution{}, err
	}
	if resolution.Status == types.AbbreviationStatusNeedsDefinition {
		if err := c.advance(ctx, b.Owner, row, types.AbbreviationTurnAwaitingDefinition, resolution); err != nil {
			return types.AbbreviationResolution{}, err
		}
	} else if resolution.Status == types.AbbreviationStatusBlockedError && resolution.ErrorCode != types.AbbreviationErrorSelectionRequired {
		if err := c.advance(ctx, b.Owner, row, types.AbbreviationTurnBlockedError, resolution); err != nil {
			return types.AbbreviationResolution{}, err
		}
		return row.Payload.Resolution, nil
	} else if err := c.advance(ctx, b.Owner, row, types.AbbreviationTurnInspecting, resolution); err != nil {
		return types.AbbreviationResolution{}, err
	}
	if len(parsed.Definitions) != 0 {
		if err := c.persistSuggestions(ctx, b.Owner, row); err != nil {
			return types.AbbreviationResolution{}, err
		}
	}
	if row.State == types.AbbreviationTurnAwaitingDefinition {
		return row.Payload.Resolution, nil
	}
	return c.finishReady(ctx, in, row)
}

func (c *AbbreviationTurnCoordinator) continueTurn(ctx context.Context, in AbbreviationPrepareInput, row *types.AbbreviationTurnState) (types.AbbreviationResolution, error) {
	var zero types.AbbreviationResolution
	if row == nil {
		return zero, types.ErrAbbreviationNotFound
	}
	if in.ExpectedVersion != nil && *in.ExpectedVersion != row.Version {
		return zero, types.ErrAbbreviationConflict
	}
	if row.State != types.AbbreviationTurnAwaitingDefinition || !c.now().Before(row.ExpiresAt) {
		return zero, types.ErrAbbreviationConflict
	}
	b := in.Binding
	resolution := row.Payload.Resolution
	parsed, err := abbreviation.ParseUserDefinitions(b.RawQuery, b.UserMessageID, resolution.UnknownTerms, len(resolution.UnknownTerms) == 1)
	if err != nil {
		return zero, err
	}
	resolution, err = abbreviation.ApplyDefinitions(resolution, parsed.Definitions)
	if err != nil {
		return zero, err
	}
	resolution.CurrentUserMessageID = b.UserMessageID
	if len(parsed.Definitions) != 0 {
		if err := c.store.LinkMessages(ctx, b.Owner, row.ID, []types.AbbreviationMessageLink{{MessageID: b.UserMessageID, RequestID: row.ID, Role: types.AbbreviationMessageRoleDefinition}}); err != nil {
			return zero, err
		}
	}
	assistantRole := types.AbbreviationMessageRoleAnswer
	if len(resolution.UnknownTerms) != 0 {
		assistantRole = types.AbbreviationMessageRoleClarification
	}
	if err := c.linkAssistant(ctx, b, row, assistantRole); err != nil {
		return zero, err
	}
	if err := c.advance(ctx, b.Owner, row, types.AbbreviationTurnAwaitingDefinition, resolution); err != nil {
		return zero, err
	}
	if len(parsed.Definitions) != 0 {
		if err := c.persistSuggestions(ctx, b.Owner, row); err != nil {
			return zero, err
		}
	}
	if len(row.Payload.Resolution.UnknownTerms) != 0 {
		return row.Payload.Resolution, nil
	}
	return c.finishReady(ctx, in, row)
}

func (c *AbbreviationTurnCoordinator) finishReady(ctx context.Context, in AbbreviationPrepareInput, row *types.AbbreviationTurnState) (types.AbbreviationResolution, error) {
	r := row.Payload.Resolution
	if r.Status == types.AbbreviationStatusBlockedError && r.ErrorCode == types.AbbreviationErrorSelectionRequired {
		if c.selector == nil {
			return c.blockSelector(ctx, in.Binding, row, "selector_unavailable")
		}
		modelID := in.ModelID
		if modelID == "" && in.ResolveModelID != nil {
			var err error
			modelID, err = in.ResolveModelID(ctx)
			if err != nil {
				return c.blockSelector(ctx, in.Binding, row, "selector_unavailable")
			}
		}
		ids, err := c.selector.Select(ctx, modelID, in.RelevantHistory, r)
		if err != nil {
			return c.blockSelector(ctx, in.Binding, row, "selector_unavailable")
		}
		r, err = abbreviation.ApplySelections(r, ids)
		if err != nil {
			return c.blockSelector(ctx, in.Binding, row, "invalid_selection")
		}
	}
	if r.Status != types.AbbreviationStatusReady {
		return types.AbbreviationResolution{}, types.ErrAbbreviationNotReady
	}
	if err := c.advance(ctx, in.Binding.Owner, row, types.AbbreviationTurnReady, r); err != nil {
		return types.AbbreviationResolution{}, err
	}
	return row.Payload.Resolution, nil
}

func (c *AbbreviationTurnCoordinator) blockSelector(ctx context.Context, b types.AbbreviationBinding, row *types.AbbreviationTurnState, code string) (types.AbbreviationResolution, error) {
	r := row.Payload.Resolution
	r.Status = types.AbbreviationStatusBlockedError
	r.ErrorCode = code
	r.EffectiveQuery = ""
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
	}
	if err := c.advance(ctx, b.Owner, row, types.AbbreviationTurnBlockedError, r); err != nil {
		return types.AbbreviationResolution{}, err
	}
	return row.Payload.Resolution, nil
}

func (c *AbbreviationTurnCoordinator) advance(ctx context.Context, owner types.AbbreviationOwner, row *types.AbbreviationTurnState, state string, resolution types.AbbreviationResolution) error {
	resolution.Version = row.Version + 1
	row.State = state
	row.Payload.Resolution = resolution
	ok, err := c.store.CompareAndSwap(ctx, owner, row.Version, row)
	if err != nil {
		return err
	}
	if !ok {
		return types.ErrAbbreviationConflict
	}
	row.Version++
	return nil
}

func (c *AbbreviationTurnCoordinator) linkAssistant(ctx context.Context, b types.AbbreviationBinding, row *types.AbbreviationTurnState, role string) error {
	return c.store.LinkMessages(ctx, b.Owner, row.ID, []types.AbbreviationMessageLink{{MessageID: b.AssistantMessageID, RequestID: row.ID, Role: role}})
}
