package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
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
	feedback   interfaces.AbbreviationFeedbackRepository
	now        func() time.Time
}

// SetFeedbackStore records the answers to "unclear word" questions as
// training data; without it they are not kept.
func (c *AbbreviationTurnCoordinator) SetFeedbackStore(f interfaces.AbbreviationFeedbackRepository) {
	c.feedback = f
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
		// the question was left for the rewritten one or for another
		// answer: both are labels for the word that was asked about
		c.recordUnanswered(ctx, b, awaiting)
	}
	return c.beginTurn(ctx, in)
}

func (c *AbbreviationTurnCoordinator) beginTurn(ctx context.Context, in AbbreviationPrepareInput) (types.AbbreviationResolution, error) {
	b := in.Binding
	// The detector (Clef or heuristic) only picks which tokens to look up;
	// meanings come from the dictionary or the user, never the classifier.
	var detection types.AbbreviationDetection
	if c.dictionary != nil {
		// @-mentions (file names such as @bao_cao.docx) are not abbreviations
		text := abbreviation.StripMentions(b.RawQuery)
		// the spell-check model runs alongside the detector; the words it
		// reads as typos are asked about too, its guess offered as a choice
		var typos []types.AbbreviationTypo
		var wg sync.WaitGroup
		if s, ok := c.dictionary.(abbreviationTypoSuggester); ok {
			wg.Add(1)
			go func() {
				defer wg.Done()
				typos = s.SuggestTypos(ctx, text)
			}()
		}
		detection = c.dictionary.DetectCandidates(ctx, text)
		wg.Wait()
		detection = withTypoCandidates(detection, text, typos)
	}
	probe := abbreviation.InspectDetected(b.RawQuery, nil, detection)
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
			resolution = abbreviation.InspectDetected(b.RawQuery, active, detection)
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
	if resolution.Status == types.AbbreviationStatusNeedsDefinition && !lookupFailed {
		resolution.Rewrite = c.rewriteQuestion(ctx, in, resolution)
	}
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
	asked := row.Payload.Resolution
	resolution, err = abbreviation.ApplyTypoCorrections(resolution, c.expandTypoAbbreviations(ctx, parsed.Typos))
	if err != nil {
		return zero, err
	}
	c.recordAnswers(ctx, b, row.ID, asked, parsed)
	resolution.CurrentUserMessageID = b.UserMessageID
	if len(parsed.Definitions) != 0 || len(parsed.Typos) != 0 {
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
		// nothing in the conversation says which meaning is meant: the
		// user picks from the choices rather than the model guessing
		if len(in.RelevantHistory) == 0 {
			if asked, ok, err := c.askSelection(ctx, in.Binding, row); ok || err != nil {
				return asked, err
			}
		}
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

// askSelection hands the choice of meaning to the user: the ambiguous
// terms wait for a definition with their meanings as the choices in the
// chat, and the reply "ABC = <meaning>" continues the turn
// (abbreviation.AskForSelection). ok is false when nothing is ambiguous.
func (c *AbbreviationTurnCoordinator) askSelection(ctx context.Context, b types.AbbreviationBinding, row *types.AbbreviationTurnState) (types.AbbreviationResolution, bool, error) {
	asked := abbreviation.AskForSelection(row.Payload.Resolution)
	if asked.Status != types.AbbreviationStatusNeedsDefinition {
		return types.AbbreviationResolution{}, false, nil
	}
	if err := c.advance(ctx, b.Owner, row, types.AbbreviationTurnAwaitingDefinition, asked); err != nil {
		return types.AbbreviationResolution{}, true, err
	}
	return row.Payload.Resolution, true, nil
}

func (c *AbbreviationTurnCoordinator) blockSelector(ctx context.Context, b types.AbbreviationBinding, row *types.AbbreviationTurnState, code string) (types.AbbreviationResolution, error) {
	r := row.Payload.Resolution
	// the model could not pick a meaning: ask the user instead of failing the turn
	if ctx.Err() == nil {
		if asked, ok, err := c.askSelection(ctx, b, row); ok || err != nil {
			return asked, err
		}
	}
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

// abbreviationTypoSuggester is implemented by the abbreviation service when
// a spell-check model can guess which question words are keyboard typos.
type abbreviationTypoSuggester interface {
	SuggestTypos(ctx context.Context, text string) []types.AbbreviationTypo
}

// withTypoCandidates adds the suspected typo words to the candidate set so
// the gate asks about them (typo, abbreviation, keep as typed, other)
// instead of guessing. A heuristic detection becomes an explicit key set:
// the heuristic's own picks plus the typo words. Typo words beyond the
// term budget are dropped rather than blocking the turn.
func withTypoCandidates(det types.AbbreviationDetection, text string, typos []types.AbbreviationTypo) types.AbbreviationDetection {
	if len(typos) == 0 {
		return det
	}
	out := types.AbbreviationDetection{Detector: det.Detector, Keys: append([]string(nil), det.Keys...)}
	if det.Detector == types.AbbreviationDetectorHeuristic {
		out.Detector = types.AbbreviationDetectorHeuristicSpellcheck
		out.Keys = nil
		for _, tok := range abbreviation.ProposeTokens(text) {
			if abbreviation.IsLikelyAbbreviation(tok) {
				out.Keys = append(out.Keys, strings.ToLower(tok))
			}
		}
	}
	has := make(map[string]bool, len(out.Keys))
	for _, k := range out.Keys {
		has[k] = true
	}
	for _, t := range typos {
		key := strings.ToLower(t.Wrong)
		if !has[key] {
			if len(out.Keys) >= types.MaxAbbreviationTerms {
				continue
			}
			has[key] = true
			out.Keys = append(out.Keys, key)
		}
		out.Typos = append(out.Typos, t)
	}
	return out
}

// expandTypoAbbreviations writes a correction that is a dictionary short
// form with one active meaning as "full form (SHORT)", the way the query
// renders any abbreviation (ubnf → UBND reads "Ủy ban nhân dân (UBND)").
func (c *AbbreviationTurnCoordinator) expandTypoAbbreviations(ctx context.Context, typos []types.AbbreviationTypo) []types.AbbreviationTypo {
	if len(typos) == 0 || c.dictionary == nil {
		return typos
	}
	active, err := c.dictionary.ListActive(ctx)
	if err != nil {
		return typos
	}
	meanings := map[string][]*types.Abbreviation{}
	for _, a := range active {
		if a != nil && a.IsActive {
			k := strings.ToLower(strings.TrimSpace(a.ShortForm))
			meanings[k] = append(meanings[k], a)
		}
	}
	out := make([]types.AbbreviationTypo, len(typos))
	for i, t := range typos {
		out[i] = t
		if m := meanings[strings.ToLower(strings.TrimSpace(t.Correct))]; len(m) == 1 {
			out[i].Correct = m[0].FullForm + " (" + m[0].ShortForm + ")"
		}
	}
	return out
}

// abbreviationQuestionRewriter is implemented by the abbreviation service:
// the chat model rewrites the question with its typing fixed.
type abbreviationQuestionRewriter interface {
	RewriteQuestion(ctx context.Context, modelID, text string, unclear, keep []string) string
}

// rewriteQuestion asks for the question with its unclear words fixed, to
// offer as "did you mean …". Short forms the dictionary resolves or knows
// several meanings of must stay as written. Any failure yields "".
func (c *AbbreviationTurnCoordinator) rewriteQuestion(ctx context.Context, in AbbreviationPrepareInput, r types.AbbreviationResolution) string {
	rw, ok := c.dictionary.(abbreviationQuestionRewriter)
	if !ok {
		return ""
	}
	var unclear, keep []string
	for _, u := range r.UnknownTerms {
		if len(abbreviation.AmbiguousMeanings(r, u)) == 0 {
			unclear = append(unclear, u)
		}
	}
	for _, t := range r.Terms {
		if t.Source != "" || len(t.Meanings) > 1 {
			keep = append(keep, t.ShortForm)
		}
	}
	if len(unclear) == 0 {
		return ""
	}
	modelID := in.ModelID
	if modelID == "" && in.ResolveModelID != nil {
		modelID, _ = in.ResolveModelID(ctx)
	}
	return rw.RewriteQuestion(ctx, modelID, abbreviation.StripMentions(in.Binding.RawQuery), unclear, keep)
}

// firstUnclearWord is the word the gate's question is about: the first
// unknown term without dictionary meanings to choose from.
func firstUnclearWord(r types.AbbreviationResolution) string {
	for _, u := range r.UnknownTerms {
		if len(abbreviation.AmbiguousMeanings(r, u)) == 0 {
			return u
		}
	}
	return ""
}

// recordAnswers keeps each answer to the question as a training row: the
// word, what was offered and what the user chose. Never fails the turn.
func (c *AbbreviationTurnCoordinator) recordAnswers(ctx context.Context, b types.AbbreviationBinding, requestID string, asked types.AbbreviationResolution, parsed abbreviation.DefinitionParseResult) {
	if c.feedback == nil {
		return
	}
	var rows []types.AbbreviationTypoFeedback
	row := func(word, choice, answer string) types.AbbreviationTypoFeedback {
		return types.AbbreviationTypoFeedback{
			TenantID: b.Owner.TenantID, SessionID: b.Owner.SessionID, RequestID: requestID,
			UserMessageID: b.UserMessageID, OriginalQuery: asked.OriginalQuery, Word: word,
			SpellcheckGuess: abbreviation.TypoSuggestionFor(asked, word), Rewrite: asked.Rewrite,
			Choice: choice, Answer: answer, CreatedAt: c.now(),
		}
	}
	for _, t := range parsed.Typos {
		choice := types.AbbreviationFeedbackTypo
		if strings.EqualFold(strings.TrimSpace(t.Correct), t.Wrong) {
			choice = types.AbbreviationFeedbackKeep
		}
		rows = append(rows, row(t.Wrong, choice, t.Correct))
	}
	for _, d := range parsed.Definitions {
		if len(abbreviation.AmbiguousMeanings(asked, d.ShortForm)) == 0 {
			rows = append(rows, row(d.ShortForm, types.AbbreviationFeedbackAbbreviation, d.FullForm))
		}
	}
	if err := c.feedback.Record(ctx, rows); err != nil {
		logger.Warnf(ctx, "abbreviation gate: feedback not recorded: %v", err)
	}
}

// recordUnanswered labels a question left without a definition: the user
// picked the rewritten question, or sent something else.
func (c *AbbreviationTurnCoordinator) recordUnanswered(ctx context.Context, b types.AbbreviationBinding, awaiting *types.AbbreviationTurnState) {
	asked := awaiting.Payload.Resolution
	word := firstUnclearWord(asked)
	if c.feedback == nil || word == "" {
		return
	}
	choice := types.AbbreviationFeedbackOther
	if asked.Rewrite != "" && strings.Join(strings.Fields(b.RawQuery), " ") == strings.Join(strings.Fields(asked.Rewrite), " ") {
		choice = types.AbbreviationFeedbackRewrite
	}
	err := c.feedback.Record(ctx, []types.AbbreviationTypoFeedback{{
		TenantID: b.Owner.TenantID, SessionID: b.Owner.SessionID, RequestID: awaiting.ID,
		UserMessageID: b.UserMessageID, OriginalQuery: asked.OriginalQuery, Word: word,
		SpellcheckGuess: abbreviation.TypoSuggestionFor(asked, word), Rewrite: asked.Rewrite,
		Choice: choice, Answer: b.RawQuery, CreatedAt: c.now(),
	}})
	if err != nil {
		logger.Warnf(ctx, "abbreviation gate: feedback not recorded: %v", err)
	}
}
