package service

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

// abbreviationDetectTimeout bounds one decision-model call during
// detection; on timeout the heuristic takes over.
const abbreviationDetectTimeout = 5 * time.Second

type abbreviationService struct {
	repo   interfaces.AbbreviationRepository
	models interfaces.ModelService
}

// NewAbbreviationService creates the abbreviation service. models may be
// nil; candidate detection then always uses the heuristic.
func NewAbbreviationService(repo interfaces.AbbreviationRepository, models interfaces.ModelService) interfaces.AbbreviationService {
	return &abbreviationService{repo: repo, models: models}
}

// abbreviationSpellcheckModelID is the spell-check model asked, alongside
// the decision model, which query tokens are keyboard typos; its timeout
// bounds that call, after which detection goes on without it.
const (
	abbreviationSpellcheckModelID = "builtin-llm-spellcheck"
	abbreviationSpellcheckTimeout = 3 * time.Second
)

// DetectCandidates asks the tenant's Decision model (Jev / Clef) which
// tokens are abbreviations. With no Decision model configured, or on any
// model error, it returns the heuristic detection so chat never blocks.
// In parallel the spell-check model looks for keyboard typos (cascc → các):
// a typo token is never a candidate, so it is never looked up or asked
// about, and the effective query reads its correction.
func (s *abbreviationService) DetectCandidates(ctx context.Context, text string) types.AbbreviationDetection {
	if s.models == nil {
		return types.AbbreviationDetection{}
	}
	var typos []types.AbbreviationTypo
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		typos = s.detectTypos(ctx, text)
	}()
	det := s.detectAbbreviations(ctx, text)
	wg.Wait()
	return withTypos(det, text, typos)
}

func (s *abbreviationService) detectAbbreviations(ctx context.Context, text string) types.AbbreviationDetection {
	decider, err := s.models.GetActiveDecisionModel(ctx)
	if err != nil {
		logger.Warnf(ctx, "abbreviation detection: decision model unavailable, using heuristic: %v", err)
		return types.AbbreviationDetection{}
	}
	if decider == nil {
		return types.AbbreviationDetection{}
	}
	detectCtx, cancel := context.WithTimeout(ctx, abbreviationDetectTimeout)
	defer cancel()
	det, err := abbreviation.Detect(detectCtx, abbreviation.NewDecisionDetector(decider), text)
	if err != nil {
		logger.Warnf(ctx, "abbreviation detection: %s failed, using heuristic: %v", decider.Provider(), err)
	}
	return det
}

// detectTypos asks the spell-check model about the query as one paragraph
// and keeps single-word corrections. Words of the active dictionary are
// never typos. Any failure yields no typos.
func (s *abbreviationService) detectTypos(ctx context.Context, text string) []types.AbbreviationTypo {
	if strings.TrimSpace(text) == "" || len(abbreviation.ProposeTokens(text)) == 0 {
		return nil
	}
	model, err := s.models.GetChatModel(ctx, abbreviationSpellcheckModelID)
	if err != nil || model == nil {
		return nil
	}
	spellCtx, cancel := context.WithTimeout(ctx, abbreviationSpellcheckTimeout)
	defer cancel()
	findings, err := tools.ReviewSpelling(spellCtx, model, "", []string{text})
	if err != nil {
		logger.Warnf(ctx, "abbreviation detection: spellcheck skipped: %v", err)
		return nil
	}
	var pairs [][2]string
	for _, f := range findings {
		pairs = append(pairs, abbreviation.TypoWords(f.Wrong, f.Correct)...)
	}
	if len(pairs) == 0 {
		return nil
	}
	known := map[string]struct{}{}
	if active, err := s.repo.ListActive(ctx); err == nil {
		for _, a := range active {
			if a != nil {
				known[strings.ToLower(strings.TrimSpace(a.ShortForm))] = struct{}{}
			}
		}
	}
	var out []types.AbbreviationTypo
	seen := map[string]struct{}{}
	for _, p := range pairs {
		key := strings.ToLower(p[0])
		if _, ok := known[key]; ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, types.AbbreviationTypo{Wrong: p[0], Correct: p[1]})
	}
	return out
}

// withTypos removes typo tokens from the candidate set. A heuristic
// detection becomes an explicit key set (the heuristic's own picks minus
// the typos) so later coverage checks re-derive the same candidates.
func withTypos(det types.AbbreviationDetection, text string, typos []types.AbbreviationTypo) types.AbbreviationDetection {
	if len(typos) == 0 {
		return det
	}
	drop := make(map[string]struct{}, len(typos))
	for _, t := range typos {
		drop[strings.ToLower(t.Wrong)] = struct{}{}
	}
	out := types.AbbreviationDetection{Detector: det.Detector, Typos: typos}
	keys := det.Keys
	if det.Detector == types.AbbreviationDetectorHeuristic {
		out.Detector = types.AbbreviationDetectorHeuristicSpellcheck
		keys = nil
		for _, tok := range abbreviation.ProposeTokens(text) {
			if abbreviation.IsLikelyAbbreviation(tok) {
				keys = append(keys, strings.ToLower(tok))
			}
		}
	}
	for _, k := range keys {
		if _, typo := drop[k]; !typo {
			out.Keys = append(out.Keys, k)
		}
	}
	return out
}

// Suggest creates an inactive abbreviation suggestion attributed to the
// caller in ctx. Activation is an admin-only step done through Update.
func (s *abbreviationService) Suggest(
	ctx context.Context, req *types.AbbreviationCreateRequest,
) (*types.Abbreviation, error) {
	short, full, err := normalizeAbbreviationCreate(req)
	if err != nil {
		return nil, err
	}
	userID, _ := types.UserIDFromContext(ctx)
	return s.repo.SuggestUnique(ctx, &types.Abbreviation{
		ShortForm: short, FullForm: full, Description: req.Description, SuggestedBy: userID,
	})
}

// Create inserts a row with an explicit active flag.
func (s *abbreviationService) Create(
	ctx context.Context, req *types.AbbreviationCreateRequest, active bool,
) (*types.Abbreviation, error) {
	short, full, err := normalizeAbbreviationCreate(req)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, short, full, req.Description, active)
}

func normalizeAbbreviationCreate(req *types.AbbreviationCreateRequest) (string, string, error) {
	if req == nil {
		return "", "", werrors.NewBadRequestError("abbreviation request is required")
	}
	short := strings.TrimSpace(req.ShortForm)
	full := strings.TrimSpace(req.FullForm)
	if short == "" {
		return "", "", werrors.NewBadRequestError("short_form is required")
	}
	if utf8.RuneCountInString(short) > 50 {
		return "", "", werrors.NewBadRequestError("short_form must be at most 50 characters")
	}
	if full == "" {
		return "", "", werrors.NewBadRequestError("full_form is required")
	}
	if utf8.RuneCountInString(full) > 255 {
		return "", "", werrors.NewBadRequestError("full_form must be at most 255 characters")
	}
	return short, full, nil
}

func (s *abbreviationService) create(
	ctx context.Context, short, full, description string, active bool,
) (*types.Abbreviation, error) {
	userID, _ := types.UserIDFromContext(ctx)
	abbr := &types.Abbreviation{
		ShortForm:   short,
		FullForm:    full,
		Description: description,
		IsActive:    active,
		SuggestedBy: userID,
	}
	if err := s.repo.Create(ctx, abbr); err != nil {
		return nil, err
	}
	return abbr, nil
}

func (s *abbreviationService) Get(ctx context.Context, id string) (*types.Abbreviation, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *abbreviationService) List(
	ctx context.Context, search string, isActive *bool, page, perPage int,
) ([]*types.Abbreviation, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	return s.repo.List(ctx, search, isActive, (page-1)*perPage, perPage)
}

func (s *abbreviationService) Update(
	ctx context.Context, id string, req *types.AbbreviationUpdateRequest,
) (*types.Abbreviation, error) {
	abbr, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if abbr == nil {
		return nil, nil
	}
	if req.ShortForm != nil {
		abbr.ShortForm = strings.TrimSpace(*req.ShortForm)
	}
	if req.FullForm != nil {
		abbr.FullForm = strings.TrimSpace(*req.FullForm)
	}
	if req.Description != nil {
		abbr.Description = *req.Description
	}
	if req.IsActive != nil {
		abbr.IsActive = *req.IsActive
	}
	if err := s.repo.Update(ctx, abbr); err != nil {
		return nil, err
	}
	return abbr, nil
}

func (s *abbreviationService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *abbreviationService) ListActive(ctx context.Context) ([]*types.Abbreviation, error) {
	return s.repo.ListActive(ctx)
}

func (s *abbreviationService) ListByShortForm(
	ctx context.Context, shortForm string,
) ([]*types.Abbreviation, error) {
	return s.repo.ListByShortForm(ctx, shortForm)
}
