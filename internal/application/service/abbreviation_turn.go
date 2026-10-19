package service

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
	// AskWords are asked about even when the sources agree on a reading.
	AskWords []string
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
		reply := abbreviationOptionReply(awaiting.Payload.Resolution, b.RawQuery)
		parsed, err := abbreviation.ParseUserDefinitions(reply, b.UserMessageID, awaiting.Payload.Resolution.UnknownTerms, len(awaiting.Payload.Resolution.UnknownTerms) == 1)
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
	// @-mentions (file names such as @bao_cao.docx) are not abbreviations
	text := abbreviation.StripMentions(b.RawQuery)
	var sources *typoSources
	if c.dictionary != nil {
		// the typo models see only the question and start alongside the
		// detector; tenant data (dictionary, earlier answers) is read once
		// the turn's owner is verified below
		sources = c.startTypoSources(ctx, in, text)
		detection = c.dictionary.DetectCandidates(ctx, text)
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
	var (
		active       []*types.Abbreviation
		listErr      error
		listed       bool
		rewrite      string
		lookupFailed bool
	)
	listActive := func() {
		if !listed {
			listed = true
			active, listErr = c.dictionary.ListActive(ctx)
		}
	}
	// numbers, roman numerals, written words, short forms spelled out in the
	// question and widely known ones are never asked about
	if c.dictionary != nil && len(abbreviation.FindDetectedCandidates(text, detection)) != 0 {
		listActive()
		if listErr == nil {
			detection = abbreviation.DropSettled(text, detection, shortFormSet(active))
			// proper names the user kept as typed (PV01) are settled too
			detection = abbreviation.DropProperNames(text, detection, active)
		}
	}
	if sources != nil {
		listActive()
		detection, rewrite = c.fuseTypos(ctx, in, text, detection, sources, active)
		resolution = abbreviation.InspectDetected(b.RawQuery, nil, detection)
		candidates = candidates[:0]
		for _, term := range resolution.Terms {
			candidates = append(candidates, term.ShortForm)
		}
	}
	if len(candidates) != 0 {
		if c.dictionary == nil {
			lookupFailed = true
		} else if listActive(); listErr == nil {
			resolution = abbreviation.InspectDetected(b.RawQuery, active, detection)
		} else {
			lookupFailed = true
		}
	}
	// a definition given inline for a word that turned out to be a typo
	// has no term left to define
	defs := parsed.Definitions[:0:0]
	for _, d := range parsed.Definitions {
		for _, term := range resolution.Terms {
			if strings.EqualFold(term.ShortForm, d.ShortForm) {
				defs = append(defs, d)
				break
			}
		}
	}
	parsed.Definitions = defs
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
		resolution.Rewrite = offeredRewrite(resolution, rewrite, activeShortForms(active))
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
	reply := abbreviationOptionReply(resolution, b.RawQuery)
	parsed, err := abbreviation.ParseUserDefinitions(reply, b.UserMessageID, resolution.UnknownTerms, len(resolution.UnknownTerms) == 1)
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
	c.rememberProperNames(ctx, asked, parsed.Typos)
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

// abbreviationTypoSources is implemented by the abbreviation service: the
// spell-check model's readings and the chat model's rewritten question.
type abbreviationTypoSources interface {
	SpellcheckReadings(ctx context.Context, text string) map[string]string
	RewriteQuestion(ctx context.Context, modelID, text string, suspects []string) string
}

// typoSources holds the typo models' answers for a question with suspect
// words (not Vietnamese syllables): the spell-check model's readings and
// the chat model's rewrite, running in the background.
type typoSources struct {
	wg       sync.WaitGroup
	suspects []string
	spell    map[string]string
	rewrite  string
}

// startTypoSources starts the typo models when the question has suspect
// words; nil when it has none (most questions) or no source is available.
func (c *AbbreviationTurnCoordinator) startTypoSources(ctx context.Context, in AbbreviationPrepareInput, text string) *typoSources {
	src, ok := c.dictionary.(abbreviationTypoSources)
	suspects := abbreviation.SuspectWords(text)
	if !ok || len(suspects) == 0 {
		return nil
	}
	s := &typoSources{suspects: suspects}
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.spell = src.SpellcheckReadings(ctx, text)
	}()
	go func() {
		defer s.wg.Done()
		modelID := in.ModelID
		if modelID == "" && in.ResolveModelID != nil {
			modelID, _ = in.ResolveModelID(ctx)
		}
		s.rewrite = src.RewriteQuestion(ctx, modelID, text, suspects)
	}()
	return s
}

// fuseTypos waits for the typo models and decides every suspect word that
// is not a dictionary short form: read it without asking (two sources
// agree, or the tenant confirmed it before), ask about it, treat it as an
// abbreviation, or keep it. The sources are the spell-check model, the
// chat model's rewrite, the input-method decoder, the dictionary's near
// short forms and the tenant's earlier answers. It returns the detection
// with the verdicts applied and the raw rewrite.
func (c *AbbreviationTurnCoordinator) fuseTypos(ctx context.Context, in AbbreviationPrepareInput, text string,
	det types.AbbreviationDetection, src *typoSources, active []*types.Abbreviation) (types.AbbreviationDetection, string) {
	known := map[string]bool{}
	shortForms := activeShortForms(active)
	for _, sf := range shortForms {
		known[strings.ToLower(strings.TrimSpace(sf))] = true
	}
	var suspects []string
	for _, w := range src.suspects {
		if !known[strings.ToLower(w)] && !abbreviation.SettledWithoutAsking(text, w, known, false) {
			suspects = append(suspects, w)
		}
	}
	var memory map[string]types.AbbreviationTypoFeedback
	if c.feedback != nil && len(suspects) > 0 {
		if m, err := c.feedback.Latest(ctx, in.Binding.Owner.TenantID, suspects); err == nil {
			memory = m
		}
	}
	src.wg.Wait()
	rewrite := src.rewrite
	var readings map[string]abbreviation.RewriteReading
	if rewrite != "" {
		readings = abbreviation.RewriteReadings(text, rewrite, suspects)
	}
	ask := map[string]bool{}
	for _, w := range in.AskWords {
		ask[strings.ToLower(w)] = true
	}
	add, drop := map[string]bool{}, map[string]bool{}
	var typos, corrections []types.AbbreviationTypo
	for _, w := range suspects {
		k := strings.ToLower(w)
		rr := readings[k]
		e := abbreviation.TypoEvidence{
			Word: w, Spellcheck: src.spell[k], Rewrite: rr.Text, RewriteRan: rewrite != "",
			RewriteAbbreviation: rr.Abbreviation, Dictionary: abbreviation.NearShortForm(w, shortForms), Ask: ask[k],
		}
		// another abbreviation in its place (vphc → VPCP) is a reading of
		// an abbreviation, not of a typo
		if rr.Text != "" && abbreviation.NewAbbreviationIn(rr.Text, known) {
			e.Rewrite, e.RewriteAbbreviation = "", true
		}
		e.Decoded, _ = abbreviation.DecodeTyping(w)
		e.Unmarked = abbreviation.UnmarkedReading(w, e.Rewrite, e.Spellcheck)
		if m, ok := memory[k]; ok {
			switch m.Choice {
			case types.AbbreviationFeedbackTypo:
				e.Memory = m.Answer
			case types.AbbreviationFeedbackKeep:
				e.MemoryKeep = true
			case types.AbbreviationFeedbackAbbreviation:
				e.MemoryAbbr = true
			}
		}
		verdict, correction, options := abbreviation.DecideTypo(e)
		switch verdict {
		case abbreviation.TypoCorrect:
			corrections = append(corrections, types.AbbreviationTypo{Wrong: w, Correct: correction})
			drop[k] = true
		case abbreviation.TypoKeep:
			drop[k] = true
		case abbreviation.TypoAsk:
			add[k] = true
			for _, o := range options {
				typos = append(typos, types.AbbreviationTypo{Wrong: w, Correct: o})
			}
		case abbreviation.TypoAbbreviation:
			add[k] = true
		}
	}
	logger.Infof(ctx, "abbreviation gate: suspects=%d corrected=%d asked=%d", len(suspects), len(corrections), len(add))
	return mergeTypoDetection(det, text, add, drop, typos, expandShortForms(corrections, active)), rewrite
}

// shortFormSet is the active dictionary's short forms, lower-cased.
func shortFormSet(active []*types.Abbreviation) map[string]bool {
	out := map[string]bool{}
	for _, sf := range activeShortForms(active) {
		out[strings.ToLower(strings.TrimSpace(sf))] = true
	}
	return out
}

func activeShortForms(active []*types.Abbreviation) []string {
	var out []string
	for _, a := range active {
		if a != nil && a.IsActive {
			out = append(out, a.ShortForm)
		}
	}
	return out
}

// mergeTypoDetection applies the typo verdicts to the detector's candidate
// keys: asked words join them, read and kept words leave them. A heuristic
// detection becomes an explicit key set (the heuristic's own picks) first.
// Words beyond the term budget are not added rather than blocking the turn.
func mergeTypoDetection(det types.AbbreviationDetection, text string, add, drop map[string]bool,
	typos, corrections []types.AbbreviationTypo) types.AbbreviationDetection {
	if len(add) == 0 && len(drop) == 0 {
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
	var keys []string
	has := map[string]bool{}
	for _, k := range out.Keys {
		if !drop[k] && !has[k] {
			keys = append(keys, k)
			has[k] = true
		}
	}
	for _, w := range abbreviation.SuspectWords(text) {
		k := strings.ToLower(w)
		if add[k] && !has[k] && len(keys) < types.MaxAbbreviationTerms {
			keys = append(keys, k)
			has[k] = true
		}
	}
	out.Keys = keys
	for _, t := range typos {
		if has[strings.ToLower(t.Wrong)] {
			out.Typos = append(out.Typos, t)
		}
	}
	out.Corrections = corrections
	return out
}

// expandShortForms writes a correction that is a dictionary short form
// with one active meaning as "full form (SHORT)", the way the query
// renders any abbreviation (ubnf → "Ủy ban nhân dân (UBND)").
func expandShortForms(typos []types.AbbreviationTypo, active []*types.Abbreviation) []types.AbbreviationTypo {
	meanings := map[string][]*types.Abbreviation{}
	for _, a := range active {
		// a proper name has no meaning to read the word as (PV01 stays PV01)
		if a != nil && a.IsActive && !a.IsProperName {
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

// offeredRewrite is the chat model's rewrite when it may be offered as
// "did you mean …" for the words still asked about: a pure typing fix that
// keeps the resolved and ambiguous short forms (abbreviation.ValidRewrite).
func offeredRewrite(r types.AbbreviationResolution, rewrite string, shortForms []string) string {
	if rewrite == "" {
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
	if len(unclear) == 0 || !abbreviation.ValidRewrite(r.OriginalQuery, rewrite, unclear, keep, shortForms) {
		return ""
	}
	return rewrite
}

// expandTypoAbbreviations expands the user's confirmed corrections that
// are dictionary short forms (see expandShortForms).
func (c *AbbreviationTurnCoordinator) expandTypoAbbreviations(ctx context.Context, typos []types.AbbreviationTypo) []types.AbbreviationTypo {
	if len(typos) == 0 || c.dictionary == nil {
		return typos
	}
	active, err := c.dictionary.ListActive(ctx)
	if err != nil {
		return typos
	}
	return expandShortForms(typos, active)
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

// abbreviationOptionReply reads a bare option number typed under the card
// ("3") as what clicking that option sends ("PV01 → PV01", a meaning, the
// rewritten question); an option the user fills in has nothing to send and
// needs text of its own. Anything else is returned as typed.
func abbreviationOptionReply(r types.AbbreviationResolution, raw string) string {
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if err != nil || n < 1 {
		return raw
	}
	_, data := abbreviationChoice(r, false)
	opts, _ := data["options"].([]map[string]interface{})
	if n > len(opts) {
		return raw
	}
	if resend, _ := opts[n-1]["resend"].(string); strings.TrimSpace(resend) != "" {
		return resend
	}
	return raw
}

// properNameKeeper is the dictionary's "keep as typed" store
// (abbreviationService.RememberProperName).
type properNameKeeper interface {
	RememberProperName(ctx context.Context, word string) (*types.Abbreviation, error)
}

// rememberProperNames adds to the dictionary, active, the words the user
// kept as typed that have the shape of a unit code (PV01, PA05): asked once,
// never again. Any other word kept as typed ("cascc", "UBND") stays a
// feedback label only. Never fails the turn.
func (c *AbbreviationTurnCoordinator) rememberProperNames(ctx context.Context, asked types.AbbreviationResolution, typos []types.AbbreviationTypo) {
	keeper, ok := c.dictionary.(properNameKeeper)
	if !ok {
		return
	}
	// A proper name is written platform-wide and active at once: a Guest
	// (public portal, anyone who signs up) must not add to it. Their
	// "keep as typed" still answers the turn as typed.
	if types.InPublicPortal(ctx) {
		return
	}
	for _, t := range typos {
		if !strings.EqualFold(strings.TrimSpace(t.Correct), t.Wrong) || !looksLikeProperName(asked.OriginalQuery, t.Wrong) {
			continue
		}
		if _, err := keeper.RememberProperName(ctx, surfaceForm(asked.OriginalQuery, t.Wrong)); err != nil {
			logger.Warnf(ctx, "abbreviation gate: proper name %q not kept: %v", t.Wrong, err)
		}
	}
}

// surfaceForm is word as the user wrote it in text (the case kept).
func surfaceForm(text, word string) string {
	for _, f := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' }) {
		if strings.EqualFold(f, word) {
			return f
		}
	}
	return word
}

// unitCodeRe is the shape of a unit code as written: capitals then digits,
// optionally a suffix ("PV01", "PA05", "PA05-Đ4", "CSGT01"). An all-caps
// word without digits (UBND, BHXH) may be a real abbreviation still without
// a meaning: it is not made a proper name for everyone.
var unitCodeRe = regexp.MustCompile(`^\p{Lu}{1,6}[-_.]?\d{1,4}(?:[-_.][\p{L}\d]{1,6})*$`)

// looksLikeProperName: the word as written has the shape of a unit code.
func looksLikeProperName(text, word string) bool {
	return unitCodeRe.MatchString(surfaceForm(text, word))
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
